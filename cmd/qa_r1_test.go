package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/feedback"
	"github.com/plivo/plivo-cli/internal/tunnel"
	"golang.org/x/term"
)

// r1Cases are the findings of the first QA round, on v1.2.0.
func r1Cases() []qaCase {
	return []qaCase{
		{
			id: "r1/C1", title: "an upper-case URI id skipped the in-use check: uris delete --yes deleted a URI a trunk used, and the API the trunk",
			args:    []string{"sip", "uris", "delete", strings.ToUpper(r1URI), "--yes"},
			replies: map[string]string{"GET Zentrunk/Trunk/*": r1Page(r1Trunk(r1TrunkIn, "enabled"))}, // the API reads an id in any case
			exit:    5,
			sent: []string{
				"GET Zentrunk/Trunk/?limit=20&offset=0&primary_uri_uuid=" + r1URI,
				"GET Zentrunk/Trunk/?fallback_uri_uuid=" + r1URI + "&limit=20&offset=0",
			},
			stderr: []string{"In use by 1 trunk(s)", `"code": "DESTRUCTIVE_REFUSED"`},
		},
		{
			id: "r1/C2", title: "a --query that failed on the answer exited 1 after the SMS was sent, inviting a second send",
			args:    []string{"messaging", "sms", "send", "--src", "+14155550100", "--dst", "+14155550101", "--text", "qa", "--yes", "--query", "abs(data.message)"},
			replies: map[string]string{"POST Message/": `202 {"api_id": "qa", "message": "message(s) queued", "message_uuid": ["qa-msg-1"]}`},
			sent:    []string{"POST Message/"},
			stdout:  []string{`"message_uuid"`},
			stderr:  []string{"The request was sent: do not run it again"},
		},
		{
			id: "r1/C3", title: "trunks update --name dropped the trunk's fallback URI: the API clears one an update leaves out",
			args:    qaArgs("sip trunks update " + r1TrunkIn + " --name renamed -o json"),
			replies: map[string]string{"GET Zentrunk/Trunk/{}/": r1Trunk(r1TrunkIn, "enabled")},
			sent:    []string{"GET Zentrunk/Trunk/" + r1TrunkIn + "/", "POST Zentrunk/Trunk/" + r1TrunkIn + "/"},
			stdout:  []string{`"updated": true`},
			check:   r1Carried("POST Zentrunk/Trunk/"+r1TrunkIn+"/", `"fallback_uri_uuid":"`+r1Fallback+`"`),
		},
		{
			id: "r1/C4", title: `diagnose -o json exited 3 on a good analysis: a null timeline, confidence "High (90%)", a command block after the result`,
			args: []string{"voice", "calls", "diagnose", r1Call, "-o", "json"},
			replies: map[string]string{
				"GET Call/{}/": r1CallRecord,
				r1Chat: sseTurn("final", payload(map[string]any{"latency_ms": 1, "answer": "The caller hung up after 42 seconds.\n\n```json\n" +
					`{"what_happened": "The caller hung up after 42 seconds.", "likely_cause": "", "timeline": null, ` +
					`"next_steps": ["Nothing to fix."], "confidence": "High (90%)"}` +
					"\n```\n\nTo read the record yourself:\n```\nplivo voice calls get " + r1Call + "\n```"})),
			},
			sent:   []string{"GET Call/" + r1Call + "/", "GET ", r1Chat}, // the record, the balance, the assistant
			stdout: []string{`"what_happened": "The caller hung up after 42 seconds."`, `"confidence": "high"`},
		},
		{
			id: "r1/C5", title: "skill install overwrote a SKILL.md that held local edits",
			args:   qaArgs("skill install cli"),
			setup:  r1EditedSkill,
			exit:   5,
			stderr: []string{`"code": "DESTRUCTIVE_REFUSED"`, "matches no released version", "--force overwrites it"},
			check:  r1SkillKept,
		},
		{
			id: "r1/H6", title: "streams forward never noticed its tunnel stop: the app stayed on the dead URL until a Ctrl-C",
			// The tunnel is stubbed: its process exiting is a closed Done. ngrok,
			// ssh and auto's fallback to localhost.run are left to internal/tunnel.
			args:    []string{"voice", "streams", "forward", "--number", "+14155550100", "--app", r1App, "--to", "ws://localhost:7860/ws", "--yes", "-o", "table"},
			replies: map[string]string{"GET Application/{}/": `{"api_id": "qa", "app_id": "` + r1App + `", "app_name": "qa-app", "answer_url": "` + r1AnswerURL + `", "answer_method": "POST"}`},
			setup:   r1DeadTunnel,
			exit:    3,
			sent:    []string{"GET Application/" + r1App + "/", "POST Application/" + r1App + "/", "POST Application/" + r1App + "/"},
			stdout:  []string{"✗ the tunnel stopped", `Restoring answer_url on "qa-app"... done.`},
			stderr:  []string{`"code": "NETWORK_ERROR"`, `"restored": true`},
			check:   r1Carried("POST Application/"+r1App+"/", `"answer_url":"`+r1AnswerURL+`"`),
		},
		{
			id: "r1/H7", title: "messaging sms list --from sent a filter the Message API does not have, so every number's messages came back",
			args:   qaArgs("messaging sms list --from +14155550100 -o json"),
			exit:   1,
			stderr: []string{`"code": "BAD_FLAG"`, "cannot filter by number", "--query"},
		},
		{
			id: "r1/H8", title: "a request that got no answer was USER_ERROR, exit 1",
			args:    []string{"voice", "calls", "get", r1Call, "-o", "json"},
			handler: r1NoAnswer,
			exit:    3,
			sent:    []string{"GET Call/" + r1Call + "/"},
			stderr:  []string{`"code": "NETWORK_ERROR"`, `"retryable": true`},
		},
		{
			id: "r1/H9", title: `a 404 echoing a URI id with "2fa" in it came out as AUTH_2FA_REQUIRED, exit 2`,
			args:    []string{"sip", "uris", "get", r1URI2FA, "-o", "json"},
			replies: map[string]string{"GET Zentrunk/URI/{}/": `404 {"api_id": "qa", "error": "URI ` + r1URI2FA + ` does not exist"}`},
			exit:    1,
			sent:    []string{"GET Zentrunk/URI/" + r1URI2FA + "/"},
			stderr:  []string{`"code": "RESOURCE_NOT_FOUND"`},
		},
		{
			id: "r1/H10", title: "sip calls list --since alone searched only the 30 days after the start",
			args:   qaArgs("sip calls list --since 2026-09-01 -o json"),
			setup:  r1Clock,
			sent:   []string{"GET Zentrunk/Call/?end_time__gte=2026-09-01+00%3A00%3A00&end_time__lte=2026-10-16+03%3A00%3A00&limit=20&offset=0"},
			stdout: []string{`"objects"`},
		},
		{
			id: "r1/H11", title: "streams test said ready after the endpoint hung up on the start frame, and its frames were not Plivo's",
			args:   qaArgs("voice streams test --to ws://" + r1Endpoint + "/ws --duration 1 -o json"),
			setup:  r1StreamEndpoint,
			exit:   3,
			stderr: []string{`"code": "NETWORK_ERROR"`, `code 1011, reason \"bot crashed\"`},
		},
		{
			id: "r1/H12", title: "ask hung on a stream that went silent: --timeout did nothing",
			args:    qaArgs("ask hello --timeout 1 -o json"),
			handler: r1SilentStream,
			exit:    3,
			sent:    []string{"GET ", r1Chat},
			stderr:  []string{`"code": "UPSTREAM_TIMEOUT"`, "had not finished after --timeout 1s"},
		},
		{
			id: "r1/H13", title: "sip test probed the host and said nothing of the SIP SRV record that names where SIP is served",
			// DNS is stubbed: the host has no address of its own, only a SIP SRV record.
			args:   []string{"sip", "test", "--uri", "sip.example.com;transport=tcp", "-o", "json"},
			setup:  r1SRVOnly,
			exit:   3,
			stdout: []string{`"warnings"`, "points to pbx.example.net:5060"},
		},
		{
			id: "r1/H14", title: "feedback --bug left phone numbers, a card, an IP and SIP credentials in the report",
			args:   []string{"feedback", "--bug", "--message", r1PII, "--dry-run"},
			check:  qaShows("Bug report, as it will be sent:", "Dry run: nothing sent."),
			hidden: []string{"4155550123", "7946 0958", "4111 1111 1111 1111", "203.0.113.77", "qauser01", "s3cret"},
		},
		{
			id: "r1/H15", title: "a SIP URI with a password in it was accepted, stored and printed back",
			args:   []string{"sip", "uris", "create", "--name", "qa", "--uri", "sip:alice:s3cret@sip.example.com", "-o", "json"},
			exit:   1,
			stderr: []string{`"code": "BAD_FLAG"`, "has a password in its user part"},
			hidden: []string{"s3cret"},
		},
		{
			id: "r1/H16", title: "--schema showed hand-checked required flags as optional",
			args:  qaArgs("sip credentials create --schema -o json"),
			check: r1Required("username", "password-stdin"),
		},
		// r1/H17, upgrade sending install.sh's binary in /opt/homebrew/bin to
		// brew, is TestHomebrewInstallHint's install.sh-in-brew-bin row.
		{
			id: "r1/M-a", title: `uris create without --name sent "name": "", which the API refuses`,
			args:   qaArgs("sip uris create --uri sip.example.com -o json"),
			exit:   1,
			stderr: []string{`required flag(s) \"name\" not set`},
		},
		{
			id: "r1/M-b", title: "uris update --authentication-needed --password-stdin --dry-run failed: the stored username was never read",
			args:    []string{"sip", "uris", "update", r1URI, "--authentication-needed=true", "--password-stdin", "--dry-run"},
			stdin:   "QaPass1!\n",
			replies: map[string]string{"GET Zentrunk/URI/{}/": `{"api_id": "qa", "uri_uuid": "` + r1URI + `", "name": "qa", "uri": "sip.example.com;transport=tcp", "authentication_needed": true, "username": "storeduser1"}`},
			sent:    []string{"GET Zentrunk/URI/" + r1URI + "/"}, // the read runs under --dry-run; the POST is previewed
			stderr:  []string{"[dry-run] POST", `"username": "storeduser1"`},
			hidden:  []string{"QaPass1!"},
		},
		{
			id: "r1/M-c", title: "trunks update --credential on an inbound trunk said Updated and changed nothing",
			args:    qaArgs("sip trunks update " + r1TrunkIn + " --credential " + r1CredUUID),
			replies: map[string]string{"GET Zentrunk/Trunk/{}/": r1Trunk(r1TrunkIn, "enabled")},
			exit:    1,
			sent:    []string{"GET Zentrunk/Trunk/" + r1TrunkIn + "/"},
			stderr:  []string{`"code": "BAD_FLAG"`, "is for an outbound trunk, and this one is inbound"},
		},
		{
			id: "r1/M-d", title: "--platform elevenlabs turned a sips: URI into port 5060 over TCP",
			args:   qaArgs("sip uris create --name qa --platform elevenlabs --uri sips:sip.rtc.elevenlabs.io -o json"),
			sent:   []string{"POST Zentrunk/URI/"},
			stdout: []string{`"data"`},
			check:  r1Carried("POST Zentrunk/URI/", `"uri":"sip.rtc.elevenlabs.io:5061;transport=tls"`),
		},
		{
			id: "r1/M-e", title: "numbers update --trunk-id routed a number to a disabled trunk without a word",
			args:    qaArgs("numbers update 14155550100 --trunk-id " + r1TrunkOff),
			replies: map[string]string{"GET Zentrunk/Trunk/{}/": r1Trunk(r1TrunkOff, "disabled")},
			sent:    []string{"GET Zentrunk/Trunk/" + r1TrunkOff + "/", "POST Number/14155550100/"},
			stderr:  []string{"Warning: trunk " + r1TrunkOff + " is disabled"},
		},
		{
			id: "r1/M-f", title: "ip-acl create sent an IPv6 address the API refuses",
			args:   qaArgs("sip ip-acl create --name qa --ip 2606:4700:4700::1111"),
			exit:   1,
			stderr: []string{`"code": "BAD_FLAG"`, "is IPv6, which Plivo does not accept"},
		},
		{
			id: "r1/M-g", title: "credentials create sent a password the API refuses (5 to 20 characters, with a symbol)",
			args:   qaArgs("sip credentials create --name qa --username qauser01 --password-stdin"),
			stdin:  "abc\n",
			exit:   1,
			stderr: []string{`"code": "BAD_INPUT"`, "must be 5 to 20 letters"},
		},
		{
			id: "r1/M-h", title: "voice calls hangup -o json printed nothing on stdout",
			args:    []string{"voice", "calls", "hangup", r1Call, "--yes", "-o", "json"},
			replies: map[string]string{"DELETE Call/{}/": "204 {}"},
			sent:    []string{"DELETE Call/" + r1Call + "/"},
			stdout:  []string{`"call_uuid": "` + r1Call + `"`, `"hung_up": true`},
		},
		{
			id: "r1/M-i", title: "numbers compliance list -o jsonl wrote the whole page as one line",
			args:    qaArgs("numbers compliance list -o jsonl"),
			replies: map[string]string{"GET PhoneNumber/Compliance/*": r1Compliances},
			sent:    []string{"GET PhoneNumber/Compliance/?limit=20&offset=0"},
			check:   r1Lines(`"compliance_id":"qa-comp-0"`, `"compliance_id":"qa-comp-1"`),
		},
		{
			id: "r1/M-j", title: "powerpacks list failed on a number_priority sent as an array",
			args:    qaArgs("messaging sms powerpacks list -o json"),
			replies: map[string]string{"GET Powerpack/*": r1Page(r1Powerpack)},
			sent:    []string{"GET Powerpack/?limit=20&offset=0"},
			stdout:  []string{`"number_priority"`, `"priority1": "shortcode"`},
		},
		{
			id: "r1/M-k", title: "-o csv let a spreadsheet run =/@/+ cells as formulas, and --query sorted the columns",
			args: qaArgs("numbers list -o csv --query data.objects"),
			replies: map[string]string{"GET Number/*": r1Page(
				`{"number": "14155550100", "alias": "=1+2", "type": "local"}`,
				`{"number": "14155550101", "alias": "+14155550199", "type": "local"}`)},
			sent:   []string{"GET Number/?limit=20&offset=0"},
			stdout: []string{"number,alias,type\n14155550100,'=1+2,local\n14155550101,+14155550199,local\n"}, // an E.164 number is no formula
		},
		{
			id: "r1/M-l", title: "no way to read a call in progress: voice calls get read only the record written at the end",
			args:    []string{"voice", "calls", "get", r1Call, "--live", "-o", "json"},
			replies: map[string]string{"GET Call/{}/?status=live": `{"api_id": "qa", "call_uuid": "` + r1Call + `", "call_status": "in-progress", "direction": "outbound", "from": "+14155550100", "to": "+14155550101"}`},
			sent:    []string{"GET Call/" + r1Call + "/?status=live"},
			stdout:  []string{`"call_status": "in-progress"`},
		},
		{
			id: "r1/M-m", title: "ask -o json is JSON Lines, one object per event, not one document: by design, and the help must say so",
			args:    qaArgs("ask hello -o json"),
			replies: map[string]string{r1Chat: sseTurn("start", `{}`, "token", `{"text": "Hi."}`, "final", `{"answer": "Hi.", "latency_ms": 1}`, "done", `{}`)},
			sent:    []string{"GET ", r1Chat},
			check:   r1JSONLinesDocumented,
		},
		{
			id: "r1/M-n", title: `sip calls diagnose called a call it could not read "not found on this account"`,
			args:    []string{"sip", "calls", "diagnose", r1Call, "-o", "json"},
			handler: r1NoAnswerOn("GET Zentrunk/Call/"),
			replies: map[string]string{"GET Call/{}/": `404 {"api_id": "qa", "error": "not found"}`, r1Chat: r1GoodTurn},
			sent:    []string{"GET Zentrunk/Call/" + r1Call + "/", "GET ", r1Chat}, // the read that got no answer, the balance, the assistant
			stdout:  []string{`"what_happened": "The callee hung up."`},
			hidden:  []string{"not found on this account"},
		},
		{
			id: "r1/M-o", title: "the rating pop-up waited for Enter under `plivo ... | jq`, and PLIVO_FEEDBACK_PROMPT=off did not turn it off",
			// Execute asks after a command that succeeded; the check asks as it does.
			args:  qaArgs("numbers list -o json"),
			setup: r1PromptDue,
			sent:  []string{"GET Number/?limit=20&offset=0"},
			check: r1PromptGates,
		},
	}
}

// Ids, as the API writes them: lower case.
const (
	r1URI      = "5f0c8a9e-1b2d-4c3e-9f4a-6b7c8d9e0a1b"
	r1Fallback = "6a1d9b0f-2c3e-4d4f-8a5b-7c8d9e0f1a2c"
	r1CredUUID = "8c3f1d2b-4e5a-4f6b-8c7d-9e0f1a2b3c4e"
	r1URI2FA   = "0e2fa000-0000-4000-8000-000000000000" // holds "2fa"
	r1Call     = "d1e2f3a4-0000-4000-8000-000000000001"
	r1TrunkIn  = "21000000000001"
	r1TrunkOff = "21000000000003"
	r1App      = "31000000000000001"
)

const (
	r1AnswerURL = "https://qa.example.com/answer"
	r1Chat      = "POST /v1/cli/cx/v1/aiassist/buddy-ext/chat"
	r1Endpoint  = "stream.example.test" // a WebSocket endpoint r1Route serves
	r1PII       = "call 4155550123 or +44 20 7946 0958, card 4111 1111 1111 1111, ip 203.0.113.77, " +
		"trunk sip:qauser01:s3cret@pbx.example.com"
	r1CallRecord = `{"api_id": "qa", "call_uuid": "` + r1Call + `", "from_number": "+14155550100", "to_number": "+14155550101", ` +
		`"call_direction": "outbound", "hangup_cause_name": "Normal Hangup", "hangup_cause_code": 4000, "hangup_source": "Caller", "bill_duration": 42}`
	r1Compliances = `{"api_id": "qa", "meta": {"limit": 20, "offset": 0, "total_count": 2, "next": null, "previous": null}, "compliances": [` +
		`{"compliance_id": "qa-comp-0", "alias": "comp 0", "status": "accepted"}, {"compliance_id": "qa-comp-1", "alias": "comp 1", "status": "accepted"}]}`
	r1Powerpack = `{"uuid": "d0d0d0d0-0000-4000-8000-000000000001", "name": "qa-pp", "sticky_sender": true, ` +
		`"number_priority": [{"country_iso": "US", "priority": {"priority1": "shortcode", "priority2": "longcode", "priority3": "tollfree"}}]}`
)

// r1GoodTurn is an assistant answer that ends with a valid result block.
var r1GoodTurn = sseTurn("final", payload(map[string]any{"latency_ms": 1, "answer": "Nothing failed.\n\n```json\n" +
	`{"what_happened": "The callee hung up.", "likely_cause": "A normal hangup.", "timeline": [], "next_steps": [], "confidence": "high"}` + "\n```"}))

// r1Trunk is an inbound trunk with a primary and a fallback URI.
func r1Trunk(id, status string) string {
	return fmt.Sprintf(`{"api_id": "qa", "trunk_id": %q, "name": "qa-in", "trunk_direction": "inbound", "trunk_status": %q, `+
		`"primary_uri_uuid": %q, "fallback_uri_uuid": %q, "credential_uuid": "", "ipacl_uuid": ""}`, id, status, r1URI, r1Fallback)
}

// r1Page is one page of a list holding rows.
func r1Page(rows ...string) string {
	return fmt.Sprintf(`{"api_id": "qa", "meta": {"limit": 20, "offset": 0, "total_count": %d, "next": null, "previous": null}, "objects": [%s]}`,
		len(rows), strings.Join(rows, ", "))
}

// r1Carried is a check: the last request named req carried part in its body.
func r1Carried(req, part string) func(*qaResult) []string {
	return func(r *qaResult) []string {
		for i := len(r.reqs) - 1; i >= 0; i-- {
			if q := r.reqs[i]; q.String() == req {
				return qaExpect(strings.Contains(q.Raw, part), "%s sent %s, want %s in it", req, q.Raw, part)
			}
		}
		return []string{req + " was not sent"}
	}
}

// r1Lines is a check: stdout is one line per want, each holding it.
func r1Lines(want ...string) func(*qaResult) []string {
	return func(r *qaResult) []string {
		lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
		if len(lines) != len(want) {
			return []string{fmt.Sprintf("%d line(s), want %d:\n%s", len(lines), len(want), qaClip(r.stdout))}
		}
		var p []string
		for i, w := range want {
			p = append(p, qaExpect(strings.Contains(lines[i], w), "line %d lacks %s: %s", i+1, w, lines[i])...)
		}
		return p
	}
}

// r1NoAnswer drops the connection without an answer.
func r1NoAnswer(w http.ResponseWriter, _ *http.Request) bool {
	if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
		_ = conn.Close()
	}
	return true
}

// r1NoAnswerOn is r1NoAnswer for the requests whose "METHOD path" starts with
// prefix; the replies answer the rest.
func r1NoAnswerOn(prefix string) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		path := strings.TrimPrefix(r.URL.Path, "/v1/Account/"+qaAuthID+"/")
		return strings.HasPrefix(r.Method+" "+path, prefix) && r1NoAnswer(w, r)
	}
}

// r1SilentStream starts the assistant's stream and then sends nothing until
// the client hangs up, or for 3s, so a build that ignores --timeout ends.
func r1SilentStream(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasSuffix(r.URL.Path, "/chat") {
		return false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, sseTurn("start", `{}`))
	w.(http.Flusher).Flush()
	select {
	case <-r.Context().Done():
	case <-time.After(3 * time.Second):
	}
	return true
}

// r1EditedSkill puts a CLI skill with local edits where install writes it,
// and runs from outside any repository.
func r1EditedSkill(t *testing.T) {
	t.Chdir(t.TempDir())
	writeSkillFile(t, os.Getenv("HOME"), "plivo-cli", "my own notes\n")
}

// r1SkillKept is a check: the edited SKILL.md is as it was.
func r1SkillKept(*qaResult) []string {
	dir, _ := resolveSkillDir("", "plivo-cli") // where install writes the CLI skill
	got, err := os.ReadFile(filepath.Join(dir, skillFileName))
	return qaExpect(err == nil && string(got) == "my own notes\n", "the edited SKILL.md now holds %.40q (%v)", got, err)
}

// r1DeadTunnel stands in a tunnel whose process has exited. A forward that
// misses it ends on the context's deadline instead of hanging.
func r1DeadTunnel(t *testing.T) {
	dead := make(chan struct{})
	close(dead)
	startTunnel = func(context.Context, int, string) (*tunnel.Tunnel, error) {
		return &tunnel.Tunnel{PublicURL: "https://qa-tunnel.example.test", Done: dead}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	voiceStreamsForwardCmd.SetContext(ctx)
	t.Cleanup(func() {
		cancel()
		startTunnel = tunnel.Start
		voiceStreamsForwardCmd.SetContext(context.Background())
	})
}

// r1Clock sets the lists' clock to 2026-10-16 03:00 UTC.
func r1Clock(t *testing.T) {
	listTimeNow = func() time.Time { return time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { listTimeNow = time.Now })
}

// r1StreamEndpoint serves ws://r1Endpoint/ws: it reads the start frame and
// the first media frame, then hangs up, as a bot that crashed does. Frames
// that are not Plivo's documented ones get another close reason.
func r1StreamEndpoint(t *testing.T) {
	r1Route(t, r1Endpoint, func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_, start, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		_, media, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		if !r1PlivoFrame(start, false) || !r1PlivoFrame(media, true) {
			_ = conn.Close(websocket.StatusPolicyViolation, "frames are not Plivo's")
			return
		}
		_ = conn.Close(websocket.StatusInternalError, "bot crashed")
	})
}

// r1PlivoFrame reports whether data has the fields of Plivo's documented start
// or media frame that v1.2.0 left out; a chunk sent as a string fails it.
func r1PlivoFrame(data []byte, media bool) bool {
	var f struct {
		SequenceNumber *int    `json:"sequenceNumber"`
		ExtraHeaders   *string `json:"extra_headers"`
		StreamID       string  `json:"streamId"`
		Start          struct {
			Tracks []string `json:"tracks"`
		} `json:"start"`
		Media struct {
			Chunk *int `json:"chunk"`
		} `json:"media"`
	}
	if json.Unmarshal(data, &f) != nil || f.SequenceNumber == nil || f.ExtraHeaders == nil {
		return false
	}
	if media {
		return f.StreamID != "" && f.Media.Chunk != nil
	}
	return len(f.Start.Tracks) > 0
}

// r1Route serves host, on port 80, from h for the rest of the test: the
// commands dial h's server in its place, and every other address through the
// network guard.
func r1Route(t *testing.T, host string, h http.HandlerFunc) {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	guard := http.DefaultTransport.(*http.Transport)
	tr := guard.Clone()
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == host+":80" {
			addr = srv.Listener.Addr().String()
		}
		return guard.DialContext(ctx, network, addr)
	}
	http.DefaultTransport = tr
	t.Cleanup(tr.CloseIdleConnections)
}

// r1SRVOnly stubs DNS: sip.example.com has no address, and its _sip._tcp
// record points to pbx.example.net:5060.
func r1SRVOnly(t *testing.T) {
	lookup, lookupSRV := sipLookup, sipLookupSRV
	t.Cleanup(func() { sipLookup, sipLookupSRV = lookup, lookupSRV })
	sipLookup = func(_ context.Context, host string) ([]net.IPAddr, error) {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	sipLookupSRV = func(_ context.Context, service, proto, name string) (string, []*net.SRV, error) {
		if service != "sip" || proto != "tcp" || name != "sip.example.com" {
			return "", nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
		}
		return "_sip._tcp.sip.example.com.", []*net.SRV{{Target: "pbx.example.net.", Port: 5060, Priority: 10, Weight: 10}}, nil
	}
}

// r1Required is a check: --schema lists each flag as required.
func r1Required(flags ...string) func(*qaResult) []string {
	return func(r *qaResult) []string {
		var doc struct {
			Data struct {
				Flags []struct {
					Name     string `json:"name"`
					Required bool   `json:"required"`
				} `json:"flags"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
			return []string{fmt.Sprintf("--schema is not JSON (%v):\n%s", err, qaClip(r.stdout))}
		}
		required := map[string]bool{}
		for _, f := range doc.Data.Flags {
			required[f.Name] = f.Required
		}
		var p []string
		for _, name := range flags {
			p = append(p, qaExpect(required[name], "--%s is missing or shows required: false", name)...)
		}
		return p
	}
}

// r1JSONLinesDocumented is a check: every stdout line is a JSON object, and
// ask's help says -o json prints JSON Lines.
func r1JSONLinesDocumented(r *qaResult) []string {
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	p := qaExpect(len(lines) > 1, "want a JSON line per event, got:\n%s", qaClip(r.stdout))
	for _, l := range lines {
		p = append(p, qaExpect(json.Valid([]byte(l)), "not a JSON line: %s", l)...)
	}
	return append(p, qaExpect(strings.Contains(askCmd.Long, "JSON Lines"), "ask --help no longer says -o json prints JSON Lines")...)
}

// r1StdoutTTY is what the stubbed isTerminal says of stdout.
var r1StdoutTTY bool

// r1PromptDue stands in terminals for stdin and stderr (stdout per
// r1StdoutTTY) and clears every off switch.
func r1PromptDue(t *testing.T) {
	for _, k := range []string{"CI", feedback.PromptOptOutEnvVar, feedback.TelemetryOptOutEnvVar, config.TelemetryEnvVar} {
		t.Setenv(k, "")
	}
	isTerminal = func(fd int) bool { return r1StdoutTTY || fd != int(os.Stdout.Fd()) }
	t.Cleanup(func() { isTerminal = term.IsTerminal })
}

// r1PromptGates is a check: the prompt asks on a terminal, and never with
// stdout piped or with PLIVO_FEEDBACK_PROMPT=off.
func r1PromptGates(*qaResult) []string {
	p := qaExpect(r1Prompted(true, ""), "not asked on a terminal: the stand-in terminal does not work")
	p = append(p, qaExpect(!r1Prompted(false, ""), "asked with stdout piped, where it waits for an Enter that never comes")...)
	return append(p, qaExpect(!r1Prompted(true, "off"), "asked with PLIVO_FEEDBACK_PROMPT=off")...)
}

// r1Prompted runs the prompt, due, as Execute does after `numbers list`,
// answers no, and reports whether it asked.
func r1Prompted(stdoutTTY bool, promptEnv string) bool {
	r1StdoutTTY = stdoutTTY
	_ = os.Setenv(feedback.PromptOptOutEnvVar, promptEnv) // put back by r1PromptDue's t.Setenv
	_ = feedback.SaveState(&feedback.State{SuccessCount: feedback.MinSuccessfulBeforeFirstPrompt})
	in, w, err := os.Pipe()
	if err != nil {
		return false
	}
	_, _ = io.WriteString(w, "n\n")
	_ = w.Close()
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	stdin, stderr := os.Stdin, os.Stderr
	os.Stdin, os.Stderr = in, null
	maybePromptFeedback("numbers", []string{"numbers", "list", "-o", "json"})
	os.Stdin, os.Stderr = stdin, stderr
	_, _ = in.Close(), null.Close()
	s, err := feedback.LoadState()
	return err == nil && s.PromptCount == 1
}
