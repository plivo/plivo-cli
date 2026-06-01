package cmd

import (
	"bytes"
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
