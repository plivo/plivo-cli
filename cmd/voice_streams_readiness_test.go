package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
)

const (
	streamApp = `{"app_id":"00000000000000000000","app_name":"agent","answer_url":"https://agent.example.com/answer",` +
		`"answer_method":"POST","hangup_url":"https://agent.example.com/hangup"}`
	usAppPath    = "/Application/00000000000000000000/"
	usNumberPath = "/Number/14155551234/"
	accountPath  = "/CIFAKEPLACEHOLDER001/"
	fundedPaid   = `{"cash_credits":"5.00","billing_mode":"prepaid"}`
)

type apiReply struct {
	status int
	body   string
}

// readinessAPI fakes the Plivo API: a request whose path ends with a key gets
// that reply, any other a 404. It returns the requests made, as "METHOD path".
func readinessAPI(t *testing.T, routes map[string]apiReply) func() []string {
	t.Helper()
	var mu sync.Mutex
	var reqs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqs = append(reqs, r.Method+" "+r.URL.Path)
		mu.Unlock()
		reply := apiReply{http.StatusNotFound, `{"error":"not found"}`}
		for suffix, rep := range routes {
			if strings.HasSuffix(r.URL.Path, suffix) {
				reply = rep
			}
		}
		w.WriteHeader(reply.status)
		_, _ = w.Write([]byte(reply.body))
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), reqs...)
	}
}

func ok(body string) apiReply { return apiReply{http.StatusOK, body} }

// readinessRun runs readiness under -o json and returns its checks (from
// stdout, or from the error's context), the simulation, the result and the
// exit code.
func readinessRun(t *testing.T, args ...string) (checks []streamsCheck, sim *simulation, result string, exit int) {
	t.Helper()
	err, stdout, _ := execCmd(t, append([]string{"voice", "streams", "readiness", "-o", "json"}, args...)...)
	if err != nil {
		var ce *clierr.Error
		if !errors.As(err, &ce) || stdout != "" {
			t.Fatalf("err = %v, stdout = %q; want a CLI error and nothing on stdout", err, stdout)
		}
		checks, _ = ce.Context["checks"].([]streamsCheck)
		sim, _ = ce.Context["simulate"].(*simulation)
		result, _ = ce.Context["result"].(string)
		return checks, sim, result, ce.ExitCode()
	}
	var env struct {
		Data readiness `json:"data"`
	}
	if jerr := json.Unmarshal([]byte(stdout), &env); jerr != nil {
		t.Fatalf("stdout is not the JSON result: %v\n%s", jerr, stdout)
	}
	return env.Data.Checks, env.Data.Simulate, env.Data.Result, 0
}

// Each account check reads the API and says what it found; --dry-run makes
// those reads (and only reads) but sends nothing to the answer URL, so the
// result stays unknown. Each case names one check, its status and the exit.
func TestVoiceStreamsReadiness_accountChecks(t *testing.T) {
	noCaller := apiReply{http.StatusBadRequest, `{"error":"Phone Number not found"}`} // seen live
	cases := []struct {
		name        string
		args        []string
		routes      map[string]apiReply
		check, want string
		exit        int
		detail      string // in the check's detail, when set
	}{
		{"inbound, US", []string{"+14155551234"}, map[string]apiReply{usNumberPath: ok(usNumberRecord), usAppPath: ok(streamApp)}, "fallback_answer_url", "warn", 0, ""},
		{"inbound, says no country check applies", []string{"14155551234"}, map[string]apiReply{usNumberPath: ok(usNumberRecord), usAppPath: ok(streamApp)}, "country", "info", 0, ""},
		{"inbound, not on this account", []string{"+14155551234"}, nil, "number", "fail", 1, ""},
		{"inbound, voice off", []string{"+14155551234"}, map[string]apiReply{usNumberPath: ok(strings.Replace(usNumberRecord, `"voice_enabled":true`, `"voice_enabled":false`, 1)), usAppPath: ok(streamApp)}, "voice", "fail", 1, ""},
		{"inbound, on a SIP trunk", []string{"+14155551234"}, map[string]apiReply{usNumberPath: ok(strings.Replace(usNumberRecord, "/Application/", "/Zentrunk/Trunk/", 1))}, "application", "fail", 1, ""},
		{"inbound, India KYC rejected", []string{"+910000000000"}, map[string]apiReply{"/Number/910000000000/": ok(indiaNumberWithApp), "/PhoneNumber/Compliance/" + placeholderComplianceID + "/": ok(appRejected)}, "compliance", "fail", 1, ""},
		{"outbound, unknown caller ID", []string{"--from", "+14155551234", "--to", "+14155555678", "--answer-url", "https://agent.example.com/answer"}, map[string]apiReply{"/VerifiedCallerId/+14155551234/": noCaller, accountPath: ok(fundedPaid)}, "caller_id", "fail", 1, "3030"},
		{"outbound, verified caller ID", []string{"--from", "+14155551234", "--to", "+14155555678", "--answer-url", "https://agent.example.com/answer"}, map[string]apiReply{"/VerifiedCallerId/+14155551234/": ok(`{"phone_number":"+14155551234"}`), accountPath: ok(fundedPaid)}, "caller_id", "pass", 0, ""},
		{"outbound, geo permissions have no API", []string{"--from", "+14155551234", "--to", "+14155555678", "--answer-url", "https://agent.example.com/answer"}, map[string]apiReply{usNumberPath: ok(usNumberRecord), accountPath: ok(fundedPaid)}, "geo_permissions", "unknown", 0, ""},
		{"outbound, India calls only India", []string{"--from", "+910000000000", "--to", "+14155555678", "--answer-url", "https://agent.example.com/answer"}, map[string]apiReply{"/Number/910000000000/": ok(indiaNumberWithApp), "/PhoneNumber/Compliance/" + placeholderComplianceID + "/": ok(`{"compliance":{"status":"accepted"}}`), accountPath: ok(fundedPaid)}, "country", "fail", 1, ""},
		{"outbound, Indian caller ID not on this account", []string{"--from", "+910000000000", "--to", "+910000000001", "--answer-url", "https://agent.example.com/answer"}, map[string]apiReply{accountPath: ok(fundedPaid)}, "compliance", "", 1, ""},
		{"outbound, no credit", []string{"--from", "+14155551234", "--to", "+14155555678", "--answer-url", "https://agent.example.com/answer"}, map[string]apiReply{usNumberPath: ok(usNumberRecord), accountPath: ok(`{"cash_credits":"0.00000","billing_mode":"prepaid"}`)}, "balance", "fail", 1, ""},
		{"outbound, balance unreadable", []string{"--from", "+14155551234", "--to", "+14155555678", "--answer-url", "https://agent.example.com/answer"}, map[string]apiReply{usNumberPath: ok(usNumberRecord)}, "balance", "unknown", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			reqs := readinessAPI(t, tc.routes)
			checks, sim, result, exit := readinessRun(t, append(tc.args, "--dry-run")...)
			if got := statusOf(checks, tc.check); got != tc.want || exit != tc.exit || !strings.Contains(fmt.Sprint(checks), tc.detail) {
				t.Errorf("check %s = %q, exit %d; want %q, exit %d, %q in its detail (%+v)", tc.check, got, exit, tc.want, tc.exit, tc.detail, checks)
			}
			if wantResult := map[int]string{0: "unknown", 1: "not_ready"}[tc.exit]; sim != nil || result != wantResult {
				t.Errorf("simulate = %v, result = %q; want no simulation under --dry-run, and %q", sim, result, wantResult)
			}
			for _, c := range checks {
				if strings.Contains(c.Detail, "refusing to route") {
					t.Errorf("check %s keeps the routing wording: %q", c.Name, c.Detail)
				}
			}
			for _, r := range reqs() {
				if !strings.HasPrefix(r, "GET ") {
					t.Errorf("readiness must only read, but sent %s", r)
				}
			}
		})
	}
}

// Without --dry-run the number's answer URL is simulated, but a host Plivo
// cannot reach fails before anything is sent to it.
func TestVoiceStreamsReadiness_localAnswerURLFails(t *testing.T) {
	setFakeCreds(t)
	var hits atomic.Int64
	agent := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(agent.Close)
	readinessAPI(t, map[string]apiReply{usNumberPath: ok(usNumberRecord),
		usAppPath: ok(strings.Replace(streamApp, "https://agent.example.com/answer", agent.URL+"/answer", 1))})

	_, sim, result, exit := readinessRun(t, "+14155551234")
	if sim == nil || statusOf(sim.Checks, "answer_url") != "fail" || result != "not_ready" || exit != ExitUserError {
		t.Fatalf("simulate = %+v, result = %q, exit = %d; want the answer URL refused as local, exit 1", sim, result, exit)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("the local answer URL got %d requests, want none", n)
	}
}

// Inbound and outbound do not mix, and a malformed number is refused before
// any request.
func TestVoiceStreamsReadiness_refusesBadInput(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"+14155551234", "--from", "+14155555678"},
		{"+1 415 555 1234"},
		{"--from", "caller", "--to", "+14155555678", "--answer-url", "https://agent.example.com/answer"},
		{"--from", "+14155551234", "--to", "+14155555678"},
		{"+14155551234", "--xml-only", "--expect-audio"},
	} {
		setFakeCreds(t)
		reqs := readinessAPI(t, nil)
		err, _, _ := execCmd(t, append([]string{"voice", "streams", "readiness"}, args...)...)
		var ce *clierr.Error
		if !errors.As(err, &ce) || (ce.Code != clierr.CodeBadInput && ce.Code != clierr.CodeBadFlag) || len(reqs()) != 0 {
			t.Errorf("%v: err = %v, requests %v; want BAD_INPUT or BAD_FLAG and none", args, err, reqs())
		}
	}
}
