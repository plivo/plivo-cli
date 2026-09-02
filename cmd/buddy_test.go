package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
)

// Renderer tests exercise buddyRenderer.handle directly. The renderer's
// out/err writers are injected, so the wire-format (text concatenation,
// JSONL mode, error handling) is testable without spinning a real SSE server.

func TestBuddyRenderer_textMode_tokensSourcesAndDone(t *testing.T) {
	var out, err bytes.Buffer
	r := &buddyRenderer{
		out: &out, err: &err,
		jsonMode: false, useANSI: false, // exercise the non-ANSI narration fallback
		startedAt: time.Now(),
	}
	events := []api.SSEEvent{
		{Event: "start", Data: `{"id":"abc"}`},
		{Event: "narration", Data: `{"text":"thinking..."}`},
		{Event: "token", Data: `{"text":"SMS "}`},
		{Event: "token", Data: `{"text":"error "}`},
		{Event: "token", Data: `{"text":"30007 means..."}`},
		{Event: "sources", Data: `{"sources":[{"id":"1","title":"docs","url":"https://plivo.com/docs/sms/errors"}]}`},
		{Event: "final", Data: `{"answer":"","latency_ms":1234}`},
	}
	for _, ev := range events {
		if !r.handle(ev) {
			break
		}
	}

	o := out.String()
	if !strings.Contains(o, "SMS error 30007 means...") {
		t.Errorf("stdout missing concatenated tokens, got:\n%s", o)
	}
	if !strings.Contains(o, "Sources:") || !strings.Contains(o, "plivo.com/docs/sms/errors") {
		t.Errorf("sources block missing on stdout, got:\n%s", o)
	}

	e := err.String()
	if !strings.Contains(e, "[buddy] thinking") {
		t.Errorf("narration missing on stderr (non-ANSI fallback), got:\n%s", e)
	}
	if !strings.Contains(e, "(done in") {
		t.Errorf("done-in footer missing on stderr, got:\n%s", e)
	}
	if r.errorSeen {
		t.Error("errorSeen unexpectedly set on a clean stream")
	}
}

func TestBuddyRenderer_jsonMode_emitsOneJSONLLinePerEvent(t *testing.T) {
	var out, err bytes.Buffer
	r := &buddyRenderer{
		out: &out, err: &err,
		jsonMode: true, startedAt: time.Now(),
	}
	events := []api.SSEEvent{
		{Event: "token", Data: `{"text":"hi"}`},
		{Event: "final", Data: `{"answer":"done","latency_ms":42}`},
	}
	for _, ev := range events {
		if !r.handle(ev) {
			break
		}
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"event":"token"`) || !strings.Contains(lines[0], `"text":"hi"`) {
		t.Errorf("first JSONL line wrong: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"event":"final"`) {
		t.Errorf("second JSONL line wrong: %s", lines[1])
	}
	// JSON mode should NOT print human "(done in ...)" footer.
	if strings.Contains(err.String(), "(done in") {
		t.Errorf("JSON mode shouldn't print human footer to stderr: %s", err.String())
	}
}

func TestBuddyRenderer_errorEvent_setsFlagAndStops(t *testing.T) {
	var out, err bytes.Buffer
	r := &buddyRenderer{out: &out, err: &err, startedAt: time.Now()}
	cont := r.handle(api.SSEEvent{Event: "error", Data: `{"error":"boom"}`})
	if cont {
		t.Error("handle should return false on error event to stop streaming")
	}
	if !r.errorSeen {
		t.Error("errorSeen should be true after an error event")
	}
	if !strings.Contains(err.String(), "buddy error: boom") {
		t.Errorf("error message missing on stderr, got: %q", err.String())
	}
}

func TestBuddyRenderer_finalAnswerFallback_whenNoTokens(t *testing.T) {
	var out, err bytes.Buffer
	r := &buddyRenderer{out: &out, err: &err, startedAt: time.Now()}
	r.handle(api.SSEEvent{Event: "final", Data: `{"answer":"final answer","latency_ms":100}`})
	if !strings.Contains(out.String(), "final answer") {
		t.Errorf("final.answer should be printed when no prior tokens, got: %q", out.String())
	}
}

func TestBuddyRenderer_toolEvents_hiddenByDefault_visibleWithVerbose(t *testing.T) {
	for _, verbose := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[verbose], func(t *testing.T) {
			var out, err bytes.Buffer
			r := &buddyRenderer{out: &out, err: &err, verbose: verbose, startedAt: time.Now()}
			r.handle(api.SSEEvent{Event: "tool_call", Data: `{"name":"docs_lookup"}`})
			r.handle(api.SSEEvent{Event: "tool_output", Data: `{"name":"docs_lookup","success":true}`})
			has := strings.Contains(err.String(), "docs_lookup")
			if has != verbose {
				t.Errorf("verbose=%v: stderr-contains-tool=%v, want %v (stderr=%q)", verbose, has, verbose, err.String())
			}
		})
	}
}

// When tokens stream live, a `final` event that also carries the full answer
// must NOT re-print it — the answer should appear on stdout exactly once.
func TestBuddyRenderer_streamedTokens_finalDoesNotReprint(t *testing.T) {
	var out, err bytes.Buffer
	r := &buddyRenderer{out: &out, err: &err, startedAt: time.Now()}
	events := []api.SSEEvent{
		{Event: "token", Data: `{"text":"SMS "}`},
		{Event: "token", Data: `{"text":"error 30007"}`},
		{Event: "final", Data: `{"answer":"SMS error 30007","latency_ms":50}`},
	}
	for _, ev := range events {
		if !r.handle(ev) {
			break
		}
	}
	o := out.String()
	if n := strings.Count(o, "SMS error 30007"); n != 1 {
		t.Errorf("streamed answer should appear once (final must not re-print), got %d:\n%s", n, o)
	}
	if !r.streamed {
		t.Error("streamed flag should be set after token events")
	}
	if !strings.Contains(err.String(), "(done in") {
		t.Errorf("done footer missing on stderr, got: %q", err.String())
	}
}

// A `message` event carries a non-streamed answer (debugger-final / no-stream
// shape). It is printed as one block on `final`, replacing any buffered text,
// and `final.answer` is ignored when a message was buffered.
func TestBuddyRenderer_messageEvent_printedOnceAsBlock(t *testing.T) {
	var out, err bytes.Buffer
	r := &buddyRenderer{out: &out, err: &err, startedAt: time.Now()}
	r.handle(api.SSEEvent{Event: "message", Data: `{"text":"Here is the debug summary."}`})
	r.handle(api.SSEEvent{Event: "final", Data: `{"answer":"ignored when message present","latency_ms":10}`})
	o := out.String()
	if n := strings.Count(o, "Here is the debug summary."); n != 1 {
		t.Errorf("message answer should appear once, got %d:\n%s", n, o)
	}
	if strings.Contains(o, "ignored when message present") {
		t.Errorf("final.answer must be ignored when a message was buffered, got:\n%s", o)
	}
	if r.streamed {
		t.Error("streamed should be false for a non-streamed message answer")
	}
}

// PAI stream contract: session/result/cost/done arrive as raw (unwrapped)
// payloads. `result` is the complete answer (no token deltas), `done` ends the
// stream, `session` is captured for continuation, and `cost` is hidden in normal
// output.
func TestBuddyRenderer_paiContract_sessionResultCostDone(t *testing.T) {
	var out, err bytes.Buffer
	r := &buddyRenderer{out: &out, err: &err, startedAt: time.Now()}
	events := []api.SSEEvent{
		{Event: "session", Data: `{"session_id":"sess-123"}`},
		{Event: "result", Data: `{"text":"Here is the analysis.\n"}`},
		{Event: "cost", Data: `{"total_cost_usd":0.169562}`},
		{Event: "done", Data: `{}`},
	}
	stopped := false
	for _, ev := range events {
		if !r.handle(ev) {
			stopped = true
			break
		}
	}
	if !stopped {
		t.Error("handle should return false on `done` to end the stream")
	}
	if r.sessionID != "sess-123" {
		t.Errorf("session_id not captured, got %q", r.sessionID)
	}
	if o := out.String(); !strings.Contains(o, "Here is the analysis.") {
		t.Errorf("result text missing on stdout, got:\n%s", o)
	}
	e := err.String()
	if strings.Contains(e, "cost") || strings.Contains(e, "0.16") {
		t.Errorf("cost must be hidden in non-verbose mode, got stderr:\n%s", e)
	}
	if !strings.Contains(e, "(done in") {
		t.Errorf("done footer missing, got stderr:\n%s", e)
	}
}

func TestBuddyRenderer_paiCost_visibleWithVerbose(t *testing.T) {
	var out, err bytes.Buffer
	r := &buddyRenderer{out: &out, err: &err, verbose: true, startedAt: time.Now()}
	r.handle(api.SSEEvent{Event: "cost", Data: `{"total_cost_usd":0.169562}`})
	if e := err.String(); !strings.Contains(e, "0.1696") {
		t.Errorf("cost should print on stderr in verbose mode, got:\n%s", e)
	}
}

func TestBuddyRenderer_jsonMode_terminatesOnDone(t *testing.T) {
	var out, err bytes.Buffer
	r := &buddyRenderer{out: &out, err: &err, jsonMode: true, startedAt: time.Now()}
	if !r.handle(api.SSEEvent{Event: "result", Data: `{"text":"x"}`}) {
		t.Error("json mode should continue on result")
	}
	if r.handle(api.SSEEvent{Event: "done", Data: `{}`}) {
		t.Error("json mode should stop on done")
	}
	o := out.String()
	if !strings.Contains(o, `"event":"result"`) || !strings.Contains(o, `"event":"done"`) {
		t.Errorf("json mode should pass through result+done, got:\n%s", o)
	}
}

// `support` lists "your" past escalations — that needs a human identity
// (AomUUID), which only a browser login populates. Env-var / manually
// entered creds have none, so the command must refuse with a clear error
// instead of whatever the backend would otherwise do with an unscoped query.
func TestRunSupport_noAomUUID_refusesWithClearError(t *testing.T) {
	setFakeCreds(t)
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	supportClientForTest = &api.Client{BaseURL: srv.URL, BuddyBaseURL: srv.URL, AuthID: "MAFAKE", AuthToken: "tok", HTTP: &http.Client{}}
	defer func() { supportClientForTest = nil }()

	err, _, _ := execCmd(t, "support")
	if err == nil || !strings.Contains(err.Error(), "AUTH_FORBIDDEN") {
		t.Fatalf("expected AUTH_FORBIDDEN, got: %v", err)
	}
	if hit {
		t.Error("support should refuse before ever hitting the network without an AomUUID")
	}
}

// --dry-run sends nothing, so the identity guard must not suppress the
// preview. Regression: it did, which broke the smoke suite on any machine
// without a browser-login profile (i.e. all of CI).
func TestRunSupport_noAomUUID_dryRunStillPreviews(t *testing.T) {
	setFakeCreds(t)
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	supportClientForTest = &api.Client{BaseURL: srv.URL, BuddyBaseURL: srv.URL, AuthID: "MAFAKE", AuthToken: "tok", DryRun: true, HTTP: &http.Client{}}
	defer func() { supportClientForTest = nil }()

	err, _, stderr := execCmd(t, "support", "--dry-run")
	if err != nil {
		t.Fatalf("--dry-run should preview, not refuse: %v", err)
	}
	if !strings.Contains(stderr, "escalations") {
		t.Errorf("expected the escalations URL in the dry-run preview, got: %s", stderr)
	}
	if hit {
		t.Error("--dry-run must not send a request")
	}
}

// A profile with an AomUUID (browser login) must reach the escalations
// endpoint normally.
func TestRunSupport_withAomUUID_reachesEscalationsEndpoint(t *testing.T) {
	setFakeCreds(t)
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_id":"x","status":"ok","data":{"escalations":[]}}`))
	}))
	defer srv.Close()
	supportClientForTest = &api.Client{BaseURL: srv.URL, BuddyBaseURL: srv.URL, AuthID: "MAFAKE", AuthToken: "tok", AomUUID: "aom-123", HTTP: &http.Client{}}
	defer func() { supportClientForTest = nil }()

	err, stdout, _ := execCmd(t, "support")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotPath, "/escalations") {
		t.Errorf("expected a request to .../escalations, got path %q", gotPath)
	}
	// Non-TTY test env resolves to JSON output; empty escalations -> "data":[].
	if !strings.Contains(stdout, `"data": []`) {
		t.Errorf("expected an empty data array, got stdout: %q", stdout)
	}
}
