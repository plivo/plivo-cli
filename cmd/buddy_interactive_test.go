package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
)

// These tests cover the pure helpers behind `ask -i` (input classification and
// the session's history bookkeeping). They run without a server: the REPL loop
// and SSE streaming are exercised separately by the renderer tests.

func TestClassifyREPLInput(t *testing.T) {
	cases := []struct {
		name       string
		line       string
		wantAction replAction
		wantArg    string
	}{
		{"empty", "", replEmpty, ""},
		{"whitespace only", "   \t ", replEmpty, ""},

		{"exit", "/exit", replExit, ""},
		{"quit", "/quit", replExit, ""},
		{"q short", "/q", replExit, ""},
		{"exit uppercase", "/EXIT", replExit, ""},
		{"exit with trailing space", "  /exit  ", replExit, ""},

		{"reset", "/reset", replReset, ""},
		{"new", "/new", replReset, ""},
		{"reset mixed case", "/Reset", replReset, ""},

		{"help", "/help", replHelp, ""},
		{"help question", "/?", replHelp, ""},

		{"unknown command", "/bogus", replUnknown, "/bogus"},
		{"unknown command with args", "/foo bar", replUnknown, "/foo bar"},

		{"plain message", "what is error 30007", replMessage, "what is error 30007"},
		{"message gets trimmed", "  hello there  ", replMessage, "hello there"},
		{"message that mentions a slash mid-text", "send to /v1/foo", replMessage, "send to /v1/foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotAction, gotArg := classifyREPLInput(tc.line)
			if gotAction != tc.wantAction {
				t.Errorf("action = %d, want %d", gotAction, tc.wantAction)
			}
			if gotArg != tc.wantArg {
				t.Errorf("arg = %q, want %q", gotArg, tc.wantArg)
			}
		})
	}
}

func TestBuddySession_historyForRequest_capsToMax(t *testing.T) {
	s := &buddySession{}
	// Push more than the cap so trimming is exercised.
	total := maxHistoryTurns + 7
	for i := 0; i < total; i++ {
		s.record("user", fmt.Sprintf("turn-%d", i))
	}

	got := s.historyForRequest()
	if len(got) != maxHistoryTurns {
		t.Fatalf("historyForRequest len = %d, want %d", len(got), maxHistoryTurns)
	}
	// It must keep the most recent turns, in order.
	wantFirst := fmt.Sprintf("turn-%d", total-maxHistoryTurns)
	if got[0].Text != wantFirst {
		t.Errorf("first kept turn = %q, want %q", got[0].Text, wantFirst)
	}
	wantLast := fmt.Sprintf("turn-%d", total-1)
	if got[len(got)-1].Text != wantLast {
		t.Errorf("last kept turn = %q, want %q", got[len(got)-1].Text, wantLast)
	}
}

func TestBuddySession_historyForRequest_underCap_returnsAll(t *testing.T) {
	s := &buddySession{}
	s.record("user", "a")
	s.record("assistant", "b")

	got := s.historyForRequest()
	if len(got) != 2 {
		t.Fatalf("historyForRequest len = %d, want 2", len(got))
	}
	if got[0].Text != "a" || got[1].Text != "b" {
		t.Errorf("history order wrong: %+v", got)
	}
}

func TestBuddySession_historyForRequest_empty(t *testing.T) {
	s := &buddySession{}
	if got := s.historyForRequest(); len(got) != 0 {
		t.Errorf("empty session history len = %d, want 0", len(got))
	}
}

func TestBuddySession_record(t *testing.T) {
	s := &buddySession{}
	s.record("user", "question")
	s.record("assistant", "answer")

	want := []api.BuddyTurn{
		{Role: "user", Text: "question"},
		{Role: "assistant", Text: "answer"},
	}
	if len(s.history) != len(want) {
		t.Fatalf("history len = %d, want %d", len(s.history), len(want))
	}
	for i, w := range want {
		if s.history[i].Role != w.Role || s.history[i].Text != w.Text {
			t.Errorf("history[%d] = %+v, want %+v", i, s.history[i], w)
		}
	}
}

func TestBuddySession_reset(t *testing.T) {
	s := &buddySession{}
	s.record("user", "a")
	s.record("assistant", "b")
	s.reset()
	if len(s.history) != 0 {
		t.Errorf("history after reset len = %d, want 0", len(s.history))
	}
	// Recording after a reset should start fresh.
	s.record("user", "c")
	if len(s.history) != 1 || s.history[0].Text != "c" {
		t.Errorf("history after reset+record = %+v, want one turn 'c'", s.history)
	}
}

// /reset starts a new conversation, so it must rotate the session id too —
// reusing the old one would thread the "new" conversation into the old one
// in analytics.
func TestBuddySession_reset_rotatesSessionID(t *testing.T) {
	s := &buddySession{sessionID: newBuddySessionID()}
	before := s.sessionID
	s.reset()
	if s.sessionID == "" {
		t.Error("reset should mint a fresh session id, not clear it to empty")
	}
	if s.sessionID == before {
		t.Errorf("reset should rotate the session id, still %q", before)
	}
}

func TestNewBuddySessionID_nonEmpty_unique_within64Chars(t *testing.T) {
	a, b := newBuddySessionID(), newBuddySessionID()
	if a == "" || b == "" {
		t.Fatal("newBuddySessionID must not return an empty string")
	}
	if a == b {
		t.Errorf("two mints returned the same id: %q", a)
	}
	if len(a) > 64 || len(b) > 64 {
		t.Errorf("session id exceeds the server's 64-char limit: %q (%d), %q (%d)", a, len(a), b, len(b))
	}
}

// chatSessionIDServer replies to POST .../chat with a minimal `final` event
// and records the session_id each request body carried, in order — the
// harness for asserting how newBuddySessionID's value threads across turns.
func chatSessionIDServer(t *testing.T) (srv *httptest.Server, sessionIDs func() []string) {
	t.Helper()
	var mu sync.Mutex
	var ids []string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/chat") {
			_, _ = w.Write([]byte(`{}`)) // e.g. the account-balance lookup
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req api.BuddyChatRequest
		_ = json.Unmarshal(body, &req)
		mu.Lock()
		ids = append(ids, req.SessionID)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: final\ndata: {\"answer\":\"ok\",\"latency_ms\":1}\n\n"))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(ids))
		copy(out, ids)
		return out
	}
}

// (a) a fresh conversation mints an id and sends it, (b) a second turn in
// the same conversation reuses it, (e) it stays within the server's 64-char
// limit.
func TestRunInteractiveAsk_sessionID_mintedAndSharedAcrossTurns(t *testing.T) {
	srv, sessionIDs := chatSessionIDServer(t)
	client := &api.Client{BaseURL: srv.URL, BuddyBaseURL: srv.URL, HTTP: &http.Client{}}

	stdinTokenFn(t, "first turn\nsecond turn\n", func() {
		_ = runInteractiveAsk(client, srv.URL+"/chat", "")
	})

	ids := sessionIDs()
	if len(ids) != 2 {
		t.Fatalf("got %d chat requests, want 2: %+v", len(ids), ids)
	}
	if ids[0] == "" {
		t.Error("the first turn should mint and send a non-empty session_id")
	}
	if len(ids[0]) > 64 {
		t.Errorf("session_id is %d chars, server max is 64", len(ids[0]))
	}
	if ids[0] != ids[1] {
		t.Errorf("a second turn in the same conversation should reuse the id: %q != %q", ids[0], ids[1])
	}
}

// (c) resetting the conversation changes the id.
func TestRunInteractiveAsk_reset_rotatesSessionID(t *testing.T) {
	srv, sessionIDs := chatSessionIDServer(t)
	client := &api.Client{BaseURL: srv.URL, BuddyBaseURL: srv.URL, HTTP: &http.Client{}}

	stdinTokenFn(t, "first turn\n/reset\nsecond turn\n", func() {
		_ = runInteractiveAsk(client, srv.URL+"/chat", "")
	})

	ids := sessionIDs()
	if len(ids) != 2 {
		t.Fatalf("got %d chat requests, want 2 (/reset itself sends none): %+v", len(ids), ids)
	}
	if ids[0] == ids[1] {
		t.Errorf("/reset should rotate the session_id, both turns got %q", ids[0])
	}
	if ids[1] == "" {
		t.Error("the turn after /reset should still send a non-empty session_id")
	}
}

// A one-shot `ask` (no -i) is its own single-turn conversation — it must
// still mint and send a session_id even though it carries no history.
func TestRunAsk_oneShot_sendsSessionID(t *testing.T) {
	setFakeCreds(t) // isolates HOME so applyBuddyURL's config.Load() can't pick up a real ~/.plivo/config.toml
	srv, sessionIDs := chatSessionIDServer(t)
	clientForTest = &api.Client{BaseURL: srv.URL, BuddyBaseURL: srv.URL, AuthID: "MAFAKE1", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })

	err, _, _ := execCmd(t, "ask", "what does error 30007 mean?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := sessionIDs()
	if len(ids) != 1 || ids[0] == "" {
		t.Fatalf("expected exactly one request with a non-empty session_id, got %+v", ids)
	}
	if len(ids[0]) > 64 {
		t.Errorf("session_id is %d chars, server max is 64", len(ids[0]))
	}
}

// (d) a different profile gets a different id. There's no cross-process
// persistence backing this (buddySession's history doesn't survive past its
// own -i REPL either — see newBuddySessionID), so this holds unconditionally:
// every invocation, on any profile, mints its own fresh id.
func TestRunAsk_differentClients_getDifferentSessionIDs(t *testing.T) {
	setFakeCreds(t)
	srv, sessionIDs := chatSessionIDServer(t)
	t.Cleanup(func() { clientForTest = nil })

	clientForTest = &api.Client{BaseURL: srv.URL, BuddyBaseURL: srv.URL, AuthID: "MAFAKE1", AuthToken: "tok", HTTP: &http.Client{}}
	if err, _, _ := execCmd(t, "ask", "question one"); err != nil {
		t.Fatalf("first ask: %v", err)
	}
	clientForTest = &api.Client{BaseURL: srv.URL, BuddyBaseURL: srv.URL, AuthID: "MAFAKE2", AuthToken: "tok", HTTP: &http.Client{}}
	if err, _, _ := execCmd(t, "ask", "question two"); err != nil {
		t.Fatalf("second ask (different profile): %v", err)
	}

	ids := sessionIDs()
	if len(ids) != 2 {
		t.Fatalf("got %d chat requests, want 2: %+v", len(ids), ids)
	}
	if ids[0] == ids[1] {
		t.Errorf("different profiles/invocations must not share a session_id, both got %q", ids[0])
	}
}

// The non-TTY default (no explicit -o) must not block -i — only an EXPLICIT
// -o json should, since the renderer always prints plain chat text anyway.
func TestRunInteractiveAsk_nonTTYDefault_notRejected(t *testing.T) {
	orig := outputFormat
	outputFormat = ""
	defer func() { outputFormat = orig }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	client := &api.Client{BaseURL: srv.URL, HTTP: &http.Client{}}

	var err error
	stdinTokenFn(t, "", func() { // immediate EOF, like Ctrl-D
		err = runInteractiveAsk(client, srv.URL+"/chat", "")
	})
	if err != nil {
		t.Errorf("expected nil (clean EOF exit), got: %v", err)
	}
}

func TestRunInteractiveAsk_explicitJSON_stillRejected(t *testing.T) {
	orig := outputFormat
	outputFormat = "json"
	defer func() { outputFormat = orig }()

	err := runInteractiveAsk(&api.Client{}, "http://example.invalid/chat", "")
	if err == nil || !strings.Contains(err.Error(), "can't be combined with -o json") {
		t.Errorf("expected the -o json rejection, got: %v", err)
	}
}
