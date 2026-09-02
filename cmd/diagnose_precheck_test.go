package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
)

// diagnoseServer records which paths were hit and replies with the given status
// for the resource lookup. The chat path always 200s, so a test can tell whether
// the turn reached the assistant.
func diagnoseServer(t *testing.T, lookupStatus int) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch {
		case strings.Contains(r.URL.Path, "/chat"):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: final\ndata: {\"answer\":\"ok\",\"latency_ms\":1}\n\n"))
		case strings.Contains(r.URL.Path, "/Call/") || strings.Contains(r.URL.Path, "/Message/"):
			w.WriteHeader(lookupStatus)
			if lookupStatus == http.StatusNotFound {
				_, _ = w.Write([]byte(`{"error":"CDR for call uuid not found"}`))
			} else {
				_, _ = w.Write([]byte(`{"call_uuid":"abc"}`))
			}
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{
		BaseURL: srv.URL, BuddyBaseURL: srv.URL,
		AuthID: "MAFAKEFORTEST", AuthToken: "tok", HTTP: &http.Client{},
	}
	t.Cleanup(func() { clientForTest = nil })
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(paths))
		copy(out, paths)
		return out
	}
}

func hitChat(paths []string) bool {
	for _, p := range paths {
		if strings.Contains(p, "/chat") {
			return true
		}
	}
	return false
}

// The whole point: a typo'd id must never reach the assistant, because the
// assistant cannot tell "does not exist" from "lookup failed" and escalates,
// turning every typo into a support ticket.
func TestDiagnoseVoice_unknownCallUUID_refusesBeforeReachingAssistant(t *testing.T) {
	setFakeCreds(t)
	_, paths := diagnoseServer(t, http.StatusNotFound)

	err, _, _ := execCmd(t, "voice", "calls", "diagnose", "00000000-0000-4000-8000-000000000000")
	if err == nil {
		t.Fatal("expected a not-found error")
	}
	if !strings.Contains(err.Error(), "RESOURCE_NOT_FOUND") {
		t.Errorf("expected RESOURCE_NOT_FOUND, got: %v", err)
	}
	if hitChat(paths()) {
		t.Error("the assistant must not be reached for an unknown call uuid")
	}
}

func TestDiagnoseVoice_knownCallUUID_reachesAssistant(t *testing.T) {
	setFakeCreds(t)
	_, paths := diagnoseServer(t, http.StatusOK)

	if err, _, _ := execCmd(t, "voice", "calls", "diagnose", "abc-123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hitChat(paths()) {
		t.Error("a known call uuid should still be diagnosed")
	}
}

// A lookup that fails for any reason other than 404 must not block the
// diagnose. Losing the feature to a flaky pre-flight read would be worse than
// the ticket it prevents.
func TestDiagnoseVoice_lookupServerError_doesNotBlock(t *testing.T) {
	setFakeCreds(t)
	_, paths := diagnoseServer(t, http.StatusInternalServerError)

	if err, _, _ := execCmd(t, "voice", "calls", "diagnose", "abc-123"); err != nil {
		t.Fatalf("a 500 on the pre-check must not fail the command: %v", err)
	}
	if !hitChat(paths()) {
		t.Error("should have fallen through to the assistant")
	}
}

// --dry-run sends nothing, so it must not spend a lookup either.
func TestDiagnoseVoice_dryRun_skipsPrecheck(t *testing.T) {
	setFakeCreds(t)
	_, paths := diagnoseServer(t, http.StatusNotFound)

	if err, _, _ := execCmd(t, "voice", "calls", "diagnose", "whatever", "--dry-run"); err != nil {
		t.Fatalf("dry-run should not fail: %v", err)
	}
	for _, p := range paths() {
		if strings.Contains(p, "/Call/whatever") {
			t.Error("dry-run should not perform the existence pre-check")
		}
	}
}

// All three messaging channels share the same guard.
func TestDiagnoseMessaging_unknownUUID_refusesForEveryChannel(t *testing.T) {
	for _, channel := range []string{"sms", "whatsapp", "mms"} {
		t.Run(channel, func(t *testing.T) {
			setFakeCreds(t)
			_, paths := diagnoseServer(t, http.StatusNotFound)

			err, _, _ := execCmd(t, "messaging", channel, "diagnose", "00000000-0000-0000-0000-000000000000")
			if err == nil || !strings.Contains(err.Error(), "RESOURCE_NOT_FOUND") {
				t.Errorf("expected RESOURCE_NOT_FOUND, got: %v", err)
			}
			if hitChat(paths()) {
				t.Error("the assistant must not be reached for an unknown message uuid")
			}
		})
	}
}

// TestDiagnose_unknownID_neverFilesATicket is a dedicated regression guard,
// separate from the hitChat() checks above: it points the client's whole
// assistant surface at a counting-only server and requires the hit count to
// be exactly 0, so a future refactor of the shared diagnoseServer/hitChat
// helpers can't silently let an unknown id reach the assistant (which
// auto-files a support ticket) without this test catching it.
func TestDiagnose_unknownID_neverFilesATicket(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"voice", []string{"voice", "calls", "diagnose", "00000000-0000-4000-8000-000000000000"}},
		{"sms", []string{"messaging", "sms", "diagnose", "00000000-0000-0000-0000-000000000000"}},
		{"whatsapp", []string{"messaging", "whatsapp", "diagnose", "00000000-0000-0000-0000-000000000000"}},
		{"mms", []string{"messaging", "mms", "diagnose", "00000000-0000-0000-0000-000000000000"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)

			// Resource pre-check: always 404s (the id is unknown).
			lookupSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"not found"}`))
			}))
			t.Cleanup(lookupSrv.Close)

			// Assistant surface: counts every request it receives. Any hit
			// here is a ticket-creating call, so the count must stay 0.
			var chatHits atomic.Int64
			chatSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				chatHits.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("event: final\ndata: {\"answer\":\"ok\"}\n\n"))
			}))
			t.Cleanup(chatSrv.Close)

			clientForTest = &api.Client{
				BaseURL: lookupSrv.URL, BuddyBaseURL: chatSrv.URL,
				AuthID: "MAFAKEFORTEST", AuthToken: "tok", HTTP: &http.Client{},
			}
			t.Cleanup(func() { clientForTest = nil })

			err, _, _ := execCmd(t, tc.args...)
			if err == nil || !strings.Contains(err.Error(), "RESOURCE_NOT_FOUND") {
				t.Fatalf("expected RESOURCE_NOT_FOUND for an unknown id, got: %v", err)
			}
			if got := chatHits.Load(); got != 0 {
				t.Errorf("assistant endpoint received %d request(s) for an unknown id, want 0 — this would auto-file a support ticket", got)
			}
		})
	}
}
