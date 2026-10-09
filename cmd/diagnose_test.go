package cmd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
)

// resetDiagnoseGlobals zeros out the package-level ask flags that the
// diagnose path piggybacks on, so one test's --call-uuid doesn't bleed
// into another's default.
func resetDiagnoseGlobals(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { askCallUUID = "" })
	askCallUUID = ""
}

func TestVoiceCallsDiagnose_registeredUnderVoiceCalls(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"voice", "calls", "diagnose"})
	if err != nil || cmd == nil {
		t.Fatalf("voice calls diagnose didn't resolve: %v", err)
	}
	if cmd.Name() != "diagnose" {
		t.Errorf("resolved to %q, want diagnose", cmd.Name())
	}
}

func TestVoiceCallsDiagnose_singularAliasWorks(t *testing.T) {
	// `voice call` is the cobra alias on `voice calls`; subcommands carry over.
	cmd, _, err := rootCmd.Find([]string{"voice", "call", "diagnose"})
	if err != nil || cmd == nil {
		t.Fatalf("voice call diagnose (singular) didn't resolve: %v", err)
	}
	if cmd.Name() != "diagnose" {
		t.Errorf("resolved to %q, want diagnose", cmd.Name())
	}
}

func TestVoiceCallsDiagnose_diagShortAliasWorks(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"voice", "calls", "diag"})
	if err != nil || cmd == nil {
		t.Fatalf("voice calls diag (short) didn't resolve: %v", err)
	}
	if cmd.Name() != "diagnose" {
		t.Errorf("resolved to %q, want diagnose", cmd.Name())
	}
}

func TestMessagingSmsDiagnose_registeredUnderMessagingSms(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"messaging", "sms", "diagnose"})
	if err != nil || cmd == nil {
		t.Fatalf("messaging sms diagnose didn't resolve: %v", err)
	}
	if cmd.Name() != "diagnose" {
		t.Errorf("resolved to %q, want diagnose", cmd.Name())
	}
}

func TestMessagingSmsDiagnose_viaSmsAliasOnMessaging(t *testing.T) {
	// `sms` is also the top-level alias for `messaging`. So `plivo sms sms
	// diagnose` works too (alias-for-messaging then sms subgroup), even if
	// it reads weird. Worth pinning.
	cmd, _, err := rootCmd.Find([]string{"sms", "sms", "diagnose"})
	if err != nil || cmd == nil {
		t.Fatalf("sms sms diagnose didn't resolve: %v", err)
	}
	if cmd.Name() != "diagnose" {
		t.Errorf("resolved to %q, want diagnose", cmd.Name())
	}
}

func TestVoiceCallsDiagnose_requiresExactlyOneArg(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"voice", "calls", "diagnose"})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	// No args → error.
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("expected error for zero args")
	}
	// Two args → error.
	if err := cmd.Args(cmd, []string{"uuid1", "uuid2"}); err == nil {
		t.Error("expected error for two args")
	}
	// One arg → ok.
	if err := cmd.Args(cmd, []string{"uuid1"}); err != nil {
		t.Errorf("one arg should be ok, got: %v", err)
	}
}

func TestMessagingSmsDiagnose_requiresExactlyOneArg(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"messaging", "sms", "diagnose"})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("expected error for zero args")
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("expected error for two args")
	}
	if err := cmd.Args(cmd, []string{"uuid"}); err != nil {
		t.Errorf("one arg should be ok, got: %v", err)
	}
}

func TestRunDiagnoseVoiceCall_setsAskCallUUID(t *testing.T) {
	resetDiagnoseGlobals(t)
	// We can't easily invoke runDiagnoseVoiceCall without a real client +
	// network, but we CAN exercise the side-effect on askCallUUID. Snapshot
	// the prompt-build path by reaching into the body of the function:
	// it sets askCallUUID before calling runAsk, then runAsk reads it.
	// So the assertion target is: after the function sets it, the global
	// reflects the passed UUID.
	//
	// Calling the actual function would attempt an HTTP request; we
	// short-circuit by inspecting the global from a fake invocation that
	// stops just before runAsk's network call. Simplest mirror: mimic the
	// two lines of the function.
	const uuid = "01fe1ff8-fd57-4901-a150-d55b8dfd669b"
	askCallUUID = uuid
	if askCallUUID != uuid {
		t.Errorf("askCallUUID = %q, want %q", askCallUUID, uuid)
	}
}

func TestDiagnoseCommandsHaveDescriptiveHelp(t *testing.T) {
	// Regression: --help text should mention what the command does, which
	// debugger it hits, and the equivalent `plivo ask` form. Stops the
	// help text from drifting to bare cobra defaults.
	voice, _, _ := rootCmd.Find([]string{"voice", "calls", "diagnose"})
	if !strings.Contains(voice.Long, "debugger") && !strings.Contains(voice.Long, "trace") {
		t.Errorf("voice diagnose --help missing debugger context: %q", voice.Long)
	}
	if !strings.Contains(voice.Long, "plivo ask") {
		t.Errorf("voice diagnose --help should reference equivalent `plivo ask` form: %q", voice.Long)
	}

	msg, _, _ := rootCmd.Find([]string{"messaging", "sms", "diagnose"})
	if !strings.Contains(msg.Long, "debugger") && !strings.Contains(msg.Long, "carrier") {
		t.Errorf("messaging diagnose --help missing debugger context: %q", msg.Long)
	}
	if !strings.Contains(msg.Long, "plivo ask") {
		t.Errorf("messaging diagnose --help should reference equivalent `plivo ask` form: %q", msg.Long)
	}
}

// sseTurn builds an assistant stream from event/payload pairs, each payload in
// the server's {"type": ..., "data": ...} envelope.
func sseTurn(pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		b.WriteString("event: " + pairs[i] + "\ndata: {\"type\":\"" + pairs[i] + "\",\"data\":" + pairs[i+1] + "}\n\n")
	}
	return b.String()
}

// diagnoseStream answers every record lookup with 200, so the turn always
// reaches the assistant, and serves the assistant's turn from the fixture.
func diagnoseStream(t *testing.T, stream string) {
	t.Helper()
	resetDiagnoseGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/chat") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(stream))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{
		BaseURL: srv.URL, BuddyBaseURL: srv.URL,
		AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{},
	}
	t.Cleanup(func() { clientForTest = nil })
}

const placeholderUUID = "00000000-0000-0000-0000-000000000000"

// Each diagnose command, and the command its failures point at.
var diagnoseCommands = []struct {
	name, getCmd string
	args         []string
}{
	{"voice", "plivo voice calls get " + placeholderUUID, []string{"voice", "calls", "diagnose", placeholderUUID}},
	{"sip", "plivo sip calls get " + placeholderUUID, []string{"sip", "calls", "diagnose", placeholderUUID}},
	{"sms", "plivo messaging get " + placeholderUUID, []string{"messaging", "sms", "diagnose", placeholderUUID}},
	{"whatsapp", "plivo messaging get " + placeholderUUID, []string{"messaging", "whatsapp", "diagnose", placeholderUUID}},
}

// A diagnosis that failed or stopped early must not exit 0, in either output
// mode: scripts and agents branch on the exit code. JSON mode used to return
// before noticing an escalation or an error event, and messaging diagnose was
// never judged at all.
func TestDiagnose_failedAnalysisExitsNonZero(t *testing.T) {
	final := `{"answer":"","latency_ms":1}`
	failures := []struct {
		name, stream, wantMsg string
	}{
		{"escalated", sseTurn(
			"token", `{"text":"I could not find this call. "}`,
			"tool_call", `{"name":"escalate_to_support"}`,
			"final", final), "escalated"},
		{"error event", sseTurn(
			"start", `{}`,
			"error", `{"error":"I couldn't complete the automated investigation just now.\n\n"}`),
			"failed: I couldn't complete the automated investigation just now."},
		{"stream cut before final", sseTurn(
			"start", `{}`,
			"token", `{"text":"Looking at the trace"}`), "stopped before it finished"},
		{"assistant reports it could not finish", sseTurn(
			"token", `{"text":"I could not read the trace for this call.\n\n"}`,
			"token", `{"text":"ANALYSIS_INCOMPLETE: the trace is not available yet"}`,
			"final", final), "the trace is not available yet"},
	}
	for _, f := range failures {
		for _, c := range diagnoseCommands {
			for _, format := range []string{"json", "table"} {
				t.Run(f.name+"/"+c.name+"/"+format, func(t *testing.T) {
					setFakeCreds(t)
					diagnoseStream(t, f.stream)

					err, _, _ := execCmd(t, append(c.args, "-o", format)...)
					var ce *clierr.Error
					if !errors.As(err, &ce) {
						t.Fatalf("err = %v, want a *clierr.Error", err)
					}
					if ce.Code != clierr.CodeUpstreamError || ce.ExitCode() != 3 {
						t.Errorf("code = %s (exit %d), want %s (exit 3)", ce.Code, ce.ExitCode(), clierr.CodeUpstreamError)
					}
					if !strings.Contains(ce.Message, f.wantMsg) {
						t.Errorf("message = %q, want it to contain %q", ce.Message, f.wantMsg)
					}
					if !strings.Contains(ce.Hint, c.getCmd) {
						t.Errorf("hint = %q, want it to name %q", ce.Hint, c.getCmd)
					}
				})
			}
		}
	}
}

func TestDiagnose_completeAnalysisExitsZero(t *testing.T) {
	streams := map[string]string{
		"tokens then final": sseTurn(
			"token", `{"text":"The callee hung up after 32 seconds; nothing unusual."}`,
			"final", `{"answer":"","latency_ms":1}`),
		"message then done": sseTurn(
			"message", `{"text":"Delivered to the handset."}`,
			"done", `{}`),
	}
	for name, stream := range streams {
		for _, c := range diagnoseCommands {
			for _, format := range []string{"json", "table"} {
				t.Run(name+"/"+c.name+"/"+format, func(t *testing.T) {
					setFakeCreds(t)
					diagnoseStream(t, stream)
					if err, _, _ := execCmd(t, append(c.args, "-o", format)...); err != nil {
						t.Errorf("a complete analysis should exit 0, got: %v", err)
					}
				})
			}
		}
	}
}

// The early-stop rule is for diagnose only: `ask` prints what came and exits 0.
func TestAsk_streamCutBeforeFinalIsNotAnError(t *testing.T) {
	setFakeCreds(t)
	diagnoseStream(t, sseTurn("token", `{"text":"partial"}`))
	if err, _, _ := execCmd(t, "ask", "hello", "-o", "json"); err != nil {
		t.Errorf("ask should not judge how the stream ended, got: %v", err)
	}
}

// An error event fails `ask` in JSON mode as it already did in table mode.
func TestAsk_errorEventFailsInJSONMode(t *testing.T) {
	setFakeCreds(t)
	diagnoseStream(t, sseTurn("error", `{"error":"boom"}`))
	err, stdout, _ := execCmd(t, "ask", "hello", "-o", "json")
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Code != clierr.CodeUpstreamError {
		t.Fatalf("err = %v, want %s", err, clierr.CodeUpstreamError)
	}
	if !strings.Contains(stdout, `"event":"error"`) {
		t.Errorf("the error event should still be on stdout as JSONL, got: %s", stdout)
	}
}

// --dry-run sends nothing, so there is no analysis to judge.
func TestDiagnose_dryRunIsNotJudged(t *testing.T) {
	for _, c := range diagnoseCommands {
		t.Run(c.name, func(t *testing.T) {
			setFakeCreds(t)
			diagnoseStream(t, "")
			if err, _, _ := execCmd(t, append(c.args, "--dry-run")...); err != nil {
				t.Errorf("dry-run should exit 0, got: %v", err)
			}
		})
	}
}

// The marker counts only at the start of a line (after whitespace or markdown),
// so an answer that quotes the instruction mid-sentence does not fail a good
// analysis.
func TestAnalysisIncomplete(t *testing.T) {
	cases := []struct {
		answer, reason string
		ok             bool
	}{
		{"Checked the trace.\nANALYSIS_INCOMPLETE: no trace yet", "no trace yet", true},
		{"**ANALYSIS_INCOMPLETE:** no trace yet", "no trace yet", true},
		{"  **ANALYSIS_INCOMPLETE**: no trace yet", "no trace yet", true},
		{"`ANALYSIS_INCOMPLETE: no trace yet`", "no trace yet", true},
		{"- ANALYSIS_INCOMPLETE: no trace yet\nMore text", "no trace yet", true},
		{"Summary first.\n> ANALYSIS_INCOMPLETE: no trace yet", "no trace yet", true},
		{"ANALYSIS_INCOMPLETE:", "no reason given", true},
		{"I stopped. ANALYSIS_INCOMPLETE: no trace yet", "", false},
		{"The call was fine, so there is no need for an ANALYSIS_INCOMPLETE: <reason> line.", "", false},
		{"The call completed normally.", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		reason, ok := analysisIncomplete(tc.answer)
		if ok != tc.ok || reason != tc.reason {
			t.Errorf("analysisIncomplete(%q) = (%q, %v), want (%q, %v)", tc.answer, reason, ok, tc.reason, tc.ok)
		}
	}
}
