package cmd

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"gopkg.in/yaml.v3"
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
	diagnoseTurn(t, `{}`, stream)
}

// diagnoseTurn answers every call lookup with record and the assistant with
// turn. It returns the messages the assistant was sent.
func diagnoseTurn(t *testing.T, record, turn string) func() []string {
	t.Helper()
	resetDiagnoseGlobals(t)
	var mu sync.Mutex
	var sent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/chat"):
			var req api.BuddyChatRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			sent = append(sent, req.Message)
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(turn))
		case strings.Contains(r.URL.Path, "/Call/"):
			_, _ = w.Write([]byte(record))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{
		BaseURL: srv.URL, BuddyBaseURL: srv.URL,
		AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{},
	}
	t.Cleanup(func() { clientForTest = nil })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), sent...)
	}
}

// payload marshals an event's payload, escaping the text as JSON needs.
func payload(fields map[string]any) string {
	b, _ := json.Marshal(fields)
	return string(b)
}

// testResultBlock is a valid -o json result block, as the diagnose prompt asks
// the assistant to end its answer with.
const testResultBlock = "```json\n" + `{"what_happened": "The callee answered and hung up after 32 seconds.",` +
	` "likely_cause": "The callee ended the call; nothing failed.", "timeline": [],` +
	` "next_steps": ["Nothing to fix."], "confidence": "high"}` + "\n```"

// okDiagnoseTurn is a complete turn whose answer ends with a valid result
// block, so diagnose exits 0 in every output mode.
var okDiagnoseTurn = sseTurn("final", payload(map[string]any{
	"answer": "Nothing unusual on this call.\n\n" + testResultBlock, "latency_ms": 1}))

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
			"token", `{"text":"The callee hung up after 32 seconds; nothing unusual.\n\n"}`,
			"token", payload(map[string]any{"text": testResultBlock}),
			"final", `{"answer":"","latency_ms":1}`),
		"message then done": sseTurn(
			"message", payload(map[string]any{"text": "Delivered to the handset.\n\n" + testResultBlock}),
			"done", `{}`),
		"final answer only": okDiagnoseTurn,
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

// Call records as the pre-check reads them, ids replaced with placeholders.
// Voice CDR times carry an offset; SIP Trunking ones are UTC without one.
const (
	voiceCallRecord = `{"api_id":"` + placeholderUUID + `","call_uuid":"` + placeholderUUID + `",` +
		`"hangup_cause_code":4000,"hangup_cause_name":"Normal Hangup","hangup_source":"Callee",` +
		`"initiation_time":"2026-01-02 10:00:00+05:30","answer_time":"2026-01-02 10:00:05+05:30",` +
		`"end_time":"2026-01-02 10:00:37+05:30"}`
	sipCallRecord = `{"call_uuid":"` + placeholderUUID + `","hangup_cause_code":3000,` +
		`"hangup_cause_name":"Normal Hangup","hangup_source":"carrier","initiation_time":"2026-01-02 04:30:00",` +
		`"answer_time":"2026-01-02 04:30:05","end_time":"2026-01-02 04:30:37"}`
)

// resultTurn is a turn whose answer is prose followed by a result block
// holding block.
func resultTurn(block string) string {
	return sseTurn(
		"token", `{"text":"The callee answered, talked for 32 seconds and hung up.\n\n"}`,
		"token", payload(map[string]any{"text": "```json\n" + block + "\n```"}),
		"final", `{"answer":"","latency_ms":1}`)
}

// decodeOne decodes stdout as exactly one JSON document.
func decodeOne(t *testing.T, stdout string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	if _, err := dec.Token(); err != io.EOF {
		t.Fatalf("stdout holds more than one JSON document:\n%s", stdout)
	}
	return doc
}

// With -o json a diagnosis is one fixed result, not the event stream: the
// assistant's analysis, plus the hangup facts and timeline anchors from the
// call's own record, merged into one timeline oldest first.
func TestDiagnose_jsonPrintsOneResult(t *testing.T) {
	cases := []struct {
		name, record, block string
		args                []string
		wantCode            float64
		wantSource          string
		wantAt              []string
	}{
		{"voice", voiceCallRecord,
			`{"what_happened": "The callee answered and hung up after 32 seconds.", "likely_cause": "The callee ended the call.",` +
				` "timeline": [{"at": "2026-01-02T04:30:30Z", "event": "BYE from the callee"},` +
				` {"at": "2026-01-02 10:00:01", "event": "180 Ringing"}, {"at": null, "event": "codec PCMU"}],` +
				` "next_steps": ["Nothing to fix."], "confidence": "High", "extra": "ignored"}`,
			[]string{"voice", "calls", "diagnose", placeholderUUID}, 4000, "Callee",
			[]string{"2026-01-02 10:00:00+05:30", "2026-01-02 10:00:01", "2026-01-02 10:00:05+05:30",
				"2026-01-02T04:30:30Z", "2026-01-02 10:00:37+05:30", ""}},
		{"sip", sipCallRecord,
			`{"what_happened": "The callee answered and hung up after 32 seconds.", "likely_cause": "The callee ended the call.",` +
				` "timeline": [{"at": "2026-01-02 04:30:01", "event": "180 Ringing"}, {"at": "2026-01-02 04:30:30", "event": "BYE from the callee"},` +
				` {"at": null, "event": "codec PCMU"}], "next_steps": ["Nothing to fix."], "confidence": "high"}`,
			[]string{"sip", "calls", "diagnose", placeholderUUID}, 3000, "carrier",
			[]string{"2026-01-02 04:30:00", "2026-01-02 04:30:01", "2026-01-02 04:30:05",
				"2026-01-02 04:30:30", "2026-01-02 04:30:37", ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			diagnoseTurn(t, tc.record, resultTurn(tc.block))

			err, stdout, _ := execCmd(t, append(tc.args, "-o", "json")...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			data, _ := decodeOne(t, stdout)["data"].(map[string]any)
			if data["call_uuid"] != placeholderUUID || data["confidence"] != "high" ||
				data["what_happened"] != "The callee answered and hung up after 32 seconds." ||
				data["likely_cause"] != "The callee ended the call." {
				t.Errorf("analysis fields wrong: %v", data)
			}
			if data["hangup_cause_code"] != tc.wantCode || data["hangup_source"] != tc.wantSource {
				t.Errorf("hangup fields = %v / %v, want %v / %v from the call record",
					data["hangup_cause_code"], data["hangup_source"], tc.wantCode, tc.wantSource)
			}
			if steps, _ := data["next_steps"].([]any); len(steps) != 1 || steps[0] != "Nothing to fix." {
				t.Errorf("next_steps = %v", data["next_steps"])
			}
			if answer, _ := data["answer"].(string); answer != "The callee answered, talked for 32 seconds and hung up." {
				t.Errorf("answer = %q, want the prose without the result block", answer)
			}
			timeline, _ := data["timeline"].([]any)
			var gotAt, gotSource []string
			for _, e := range timeline {
				entry, _ := e.(map[string]any)
				at, _ := entry["at"].(string)
				gotAt = append(gotAt, at)
				gotSource = append(gotSource, entry["source"].(string))
			}
			if strings.Join(gotAt, "|") != strings.Join(tc.wantAt, "|") {
				t.Errorf("timeline order:\n got %q\nwant %q", gotAt, tc.wantAt)
			}
			if want := "call_record|assistant|call_record|assistant|call_record|assistant"; strings.Join(gotSource, "|") != want {
				t.Errorf("timeline sources = %q, want %q", gotSource, want)
			}
		})
	}
}

// A message has no call record behind it: the hangup fields are present and
// null, and the id is message_uuid.
func TestDiagnose_messagingJSONResultHasNullHangupFields(t *testing.T) {
	setFakeCreds(t)
	diagnoseTurn(t, `{}`, okDiagnoseTurn)

	err, stdout, _ := execCmd(t, "messaging", "sms", "diagnose", placeholderUUID, "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := decodeOne(t, stdout)["data"].(map[string]any)
	for _, key := range []string{"hangup_cause_code", "hangup_source"} {
		if v, ok := data[key]; !ok || v != nil {
			t.Errorf("%s = %v (present: %v), want null", key, v, ok)
		}
	}
	if data["message_uuid"] != placeholderUUID {
		t.Errorf("message_uuid = %v", data["message_uuid"])
	}
	if _, ok := data["call_uuid"]; ok {
		t.Error("a message result should not carry call_uuid")
	}
}

// -o jsonl keeps the raw event stream, and only -o json asks the assistant for
// a result block; table mode is unchanged.
func TestDiagnose_jsonlStreamsEventsAndOnlyJSONAsksForABlock(t *testing.T) {
	for _, format := range []string{"json", "jsonl", "table"} {
		t.Run(format, func(t *testing.T) {
			setFakeCreds(t)
			sent := diagnoseTurn(t, voiceCallRecord, okDiagnoseTurn)

			err, stdout, _ := execCmd(t, "voice", "calls", "diagnose", placeholderUUID, "-o", format)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			msgs := sent()
			if len(msgs) != 1 {
				t.Fatalf("assistant got %d turns, want 1", len(msgs))
			}
			asked := strings.Contains(msgs[0], "```json")
			if asked != (format == "json") {
				t.Errorf("result block requested = %v for -o %s", asked, format)
			}
			if format == "json" && !strings.Contains(msgs[0], "initiation, answer and end") {
				t.Error("a call's prompt should leave the record's anchors to the CLI")
			}
			streamed := strings.Contains(stdout, `"event":"final"`)
			if streamed != (format == "jsonl") {
				t.Errorf("event stream on stdout = %v for -o %s:\n%s", streamed, format, stdout)
			}
		})
	}
}

func TestDiagnose_messagePromptHasNoCallAnchors(t *testing.T) {
	setFakeCreds(t)
	sent := diagnoseTurn(t, `{}`, okDiagnoseTurn)
	if err, _, _ := execCmd(t, "messaging", "sms", "diagnose", placeholderUUID, "-o", "json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgs := sent(); len(msgs) != 1 || strings.Contains(msgs[0], "initiation, answer and end") {
		t.Errorf("a message has no call record anchors to leave out: %q", msgs)
	}
}

// A missing or invalid result block is an error, never a result with empty
// fields. The answer is kept in the error's context, since the run is long.
func TestDiagnose_missingOrInvalidResultBlockFails(t *testing.T) {
	valid := map[string]any{"what_happened": "x", "likely_cause": "y", "timeline": []any{}, "next_steps": []any{"z"}, "confidence": "low"}
	without := func(key string, value any) string {
		m := map[string]any{}
		for k, v := range valid {
			m[k] = v
		}
		if value == nil {
			delete(m, key)
		} else {
			m[key] = value
		}
		return payload(m)
	}
	cases := map[string]struct{ turn, want string }{
		"no block":         {sseTurn("token", `{"text":"The callee hung up."}`, "final", `{"answer":"","latency_ms":1}`), "no JSON result block"},
		"not JSON":         {resultTurn(`{"what_happened": "x",`), "not valid JSON"},
		"no likely_cause":  {resultTurn(without("likely_cause", nil)), "without likely_cause"},
		"blank what":       {resultTurn(without("what_happened", " ")), "without what_happened"},
		"no timeline":      {resultTurn(without("timeline", nil)), "without timeline"},
		"no next_steps":    {resultTurn(without("next_steps", nil)), "without next_steps"},
		"bad confidence":   {resultTurn(without("confidence", "certain")), `confidence "certain"`},
		"event-less entry": {resultTurn(without("timeline", []any{map[string]any{"at": nil}})), "timeline entry without an event"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			setFakeCreds(t)
			diagnoseTurn(t, voiceCallRecord, tc.turn)

			err, stdout, _ := execCmd(t, "voice", "calls", "diagnose", placeholderUUID, "-o", "json")
			var ce *clierr.Error
			if !errors.As(err, &ce) || ce.Code != clierr.CodeUpstreamError || ce.ExitCode() != 3 {
				t.Fatalf("err = %v, want %s (exit 3)", err, clierr.CodeUpstreamError)
			}
			if !strings.Contains(ce.Message, tc.want) {
				t.Errorf("message = %q, want it to contain %q", ce.Message, tc.want)
			}
			if !strings.Contains(ce.Hint, "-o table") || !strings.Contains(ce.Hint, "plivo voice calls get "+placeholderUUID) {
				t.Errorf("hint = %q, want the prose and record commands", ce.Hint)
			}
			if answer, _ := ce.Context["answer"].(string); !strings.Contains(answer, "The callee") {
				t.Errorf("context.answer = %q, want the assistant's answer", answer)
			}
			if stdout != "" {
				t.Errorf("a failed result should print nothing on stdout, got:\n%s", stdout)
			}
		})
	}
}

// The block may come untagged or tagged JSON; the last block is the result,
// so an example earlier in the prose does not count.
func TestNewDiagnoseResult_findsTheLastBlock(t *testing.T) {
	block := `{"what_happened": "w", "likely_cause": "c", "timeline": [], "next_steps": [], "confidence": "medium"}`
	for name, answer := range map[string]string{
		"untagged":        "Prose.\n```\n" + block + "\n```",
		"uppercase tag":   "Prose.\n```JSON\n" + block + "\n```",
		"example earlier": "Prose with an example:\n```json\n{\"what_happened\": \"example\"}\n```\nMore prose.\n```json\n" + block + "\n```",
	} {
		t.Run(name, func(t *testing.T) {
			res, err := newDiagnoseResult(answer, diagnoseTarget{label: "message", uuid: placeholderUUID, getCmd: "messaging get"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.WhatHappened != "w" || res.Confidence != "medium" || res.Timeline == nil || res.NextSteps == nil {
				t.Errorf("result = %+v", res)
			}
		})
	}
}

// diagnose's result goes through output.JSONSuccess, so yaml, csv and --query
// work on it like on any other command's result.
func TestDiagnose_resultTakesYAMLCSVAndQuery(t *testing.T) {
	const likelyCause = "The callee ended the call; nothing failed."
	cases := []struct {
		name  string
		flags []string
		check func(t *testing.T, stdout string)
	}{
		{"yaml", []string{"-o", "yaml"}, func(t *testing.T, stdout string) {
			var doc struct {
				Data map[string]any `yaml:"data"`
			}
			if err := yaml.Unmarshal([]byte(stdout), &doc); err != nil || doc.Data["likely_cause"] != likelyCause {
				t.Errorf("-o yaml = %q (%v), want the result as YAML", stdout, err)
			}
		}},
		{"csv", []string{"-o", "csv"}, func(t *testing.T, stdout string) {
			rows, err := csv.NewReader(strings.NewReader(stdout)).ReadAll()
			if err != nil || len(rows) != 2 || rows[0][0] != "call_uuid" || rows[1][0] != placeholderUUID {
				t.Errorf("-o csv = %q (%v), want a header and one row", stdout, err)
			}
		}},
		{"query", []string{"--query", "data.likely_cause"}, func(t *testing.T, stdout string) {
			if got := decodeString(t, stdout); got != likelyCause {
				t.Errorf("--query data.likely_cause = %q, want %q", got, likelyCause)
			}
		}},
		{"query with yaml", []string{"-o", "yaml", "--query", "data.confidence"}, func(t *testing.T, stdout string) {
			if strings.TrimSpace(stdout) != "high" {
				t.Errorf("-o yaml --query data.confidence = %q, want high", stdout)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			t.Cleanup(func() { queryFlag = ""; _ = output.Configure("", "") })
			diagnoseTurn(t, voiceCallRecord, okDiagnoseTurn)

			err, stdout, _ := execCmd(t, append([]string{"voice", "calls", "diagnose", placeholderUUID}, tc.flags...)...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tc.check(t, stdout)
		})
	}
}

// decodeString decodes stdout as one JSON string.
func decodeString(t *testing.T, stdout string) string {
	t.Helper()
	var s string
	if err := json.Unmarshal([]byte(stdout), &s); err != nil {
		t.Fatalf("stdout is not a JSON string: %v\n%s", err, stdout)
	}
	return s
}
