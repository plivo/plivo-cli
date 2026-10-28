package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/plivosig"
)

// simulateJSON runs simulate under -o json. A pass returns the result; a
// failure returns the error, whose context holds the checks, and must leave
// stdout empty.
func simulateJSON(t *testing.T, args ...string) (simulation, *clierr.Error) {
	t.Helper()
	err, stdout, _ := execCmd(t, append([]string{"voice", "streams", "simulate", "--duration", "1", "-o", "json"}, args...)...)
	if err != nil {
		var ce *clierr.Error
		if !errors.As(err, &ce) || stdout != "" {
			t.Fatalf("err = %v, stdout = %q; want a CLI error and nothing on stdout", err, stdout)
		}
		return simulation{}, ce
	}
	var env struct {
		Data simulation `json:"data"`
	}
	if jerr := json.Unmarshal([]byte(stdout), &env); jerr != nil {
		t.Fatalf("stdout is not the JSON result: %v\n%s", jerr, stdout)
	}
	return env.Data, nil
}

// statusOf is the status of the first check called name, "" when none is.
func statusOf(checks []streamsCheck, name string) string {
	for _, c := range checks {
		if c.Name == name {
			return c.Status
		}
	}
	return ""
}

// simulate requests the answer URL as Plivo does, with either method: the
// documented fields and a V3 signature the server can check. Then it streams
// to the <Stream>, with the same CallUUID in the start frame.
func TestVoiceStreamsSimulate_actsAsPlivo(t *testing.T) {
	for _, tc := range []struct {
		method      string
		echo, fails bool // the voice agent answers; --expect-audio then fails
	}{{"POST", true, false}, {"GET", true, false}, {"POST", false, true}} {
		t.Run(fmt.Sprintf("%s echo=%v", tc.method, tc.echo), func(t *testing.T) {
			setFakeCreds(t)
			reply := echoPlayAudio
			if !tc.echo {
				reply = nil
			}
			ws := newStreamServer(t, reply)
			var mu sync.Mutex
			var fields map[string]string
			var signed bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				body := map[string]string{}
				for k := range r.PostForm {
					body[k] = r.PostForm.Get(k)
				}
				mu.Lock()
				fields = map[string]string{"From": r.Form.Get("From"), "CallUUID": r.Form.Get("CallUUID"), "Direction": r.Form.Get("Direction")}
				signed = r.Method == tc.method && plivosig.Validate("ci-only-not-a-real-token", "http://"+r.Host+r.URL.RequestURI(),
					r.Method, r.Header.Get(plivosig.HeaderNonce), r.Header.Get(plivosig.HeaderSignature), body)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/xml; charset=utf-8")
				fmt.Fprintf(w, `<Response><Speak>Hi</Speak><Stream bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000"
					statusCallbackUrl="https://agent.example.com/status">%s</Stream></Response>`, ws.wsURL())
			}))
			t.Cleanup(srv.Close)

			res, ce := simulateJSON(t, "--answer-url", srv.URL+"/answer?agent=1", "--answer-method", tc.method, "--expect-audio")
			ws.assertStopThenClose(t)
			mu.Lock()
			defer mu.Unlock()
			if !signed || fields["From"] != sampleCaller || fields["Direction"] != "inbound" {
				t.Errorf("signature valid = %v, fields = %v; want a valid %s with Plivo's fields", signed, fields, tc.method)
			}
			if ws.callID == "" || ws.callID != fields["CallUUID"] {
				t.Errorf("start frame callId = %q, answer URL got CallUUID %q; want the same call", ws.callID, fields["CallUUID"])
			}
			if tc.fails {
				if ce == nil || ce.ExitCode() != ExitUserError || !strings.Contains(ce.Message, "no playAudio") {
					t.Fatalf("err = %v, want exit 1 for no playAudio", ce)
				}
				return
			}
			if ce != nil || res.Result != "pass" || !res.Signed || res.Stream == nil || res.Stream.PlayAudioFrames == 0 {
				t.Fatalf("err = %v, result = %+v; want a signed pass with playAudio back", ce, res)
			}
			for _, c := range res.Checks {
				if c.Status != "pass" {
					t.Errorf("check %+v, want every check to pass", c)
				}
			}
		})
	}
}

// The reply and its XML are checked as Plivo reads them. Each case names the
// check it is about, that check's status, and the exit code.
func TestVoiceStreamsSimulate_checksTheReply(t *testing.T) {
	const good = `bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://agent.example.com/s"`
	stream := func(attrs string) string {
		return `<Response><Stream ` + attrs + `>wss://agent.example.com/ws</Stream></Response>`
	}
	cases := []struct {
		name, ctype, body string
		status            int
		check, want       string
		exit              int
	}{
		{"declared charset", "application/xml", `<?xml version="1.0" encoding="ISO-8859-1"?>` + stream(good), 200, "xml", "pass", 0},
		{"text/plain", "text/plain", stream(good), 200, "content_type", "warn", 0},
		{"listening only", "text/xml", stream(`keepCallAlive="true" contentType="audio/x-l16;rate=16000"`), 200, "bidirectional", "warn", 0},
		{"no callback", "text/xml", stream(`bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000"`), 200, "statusCallbackUrl", "warn", 0},
		{"no contentType", "text/xml", stream(`bidirectional="true" keepCallAlive="true"`), 200, "contentType", "warn", 0},
		{"something follows", "text/xml", `<Response><Stream>wss://agent.example.com/ws</Stream><Wait length="60"/></Response>`, 200, "keepCallAlive", "", 0},
		{"not found", "text/html", "nope", 404, "answer_url", "fail", 1},
		{"redirect", "", "", 302, "answer_url", "fail", 1},
		{"empty", "application/xml", " ", 200, "xml", "fail", 1},
		{"JSON", "application/json", `{"flow":"x"}`, 200, "xml", "fail", 1},
		{"HTML", "text/html", "<html><body>hi</body></html>", 200, "xml", "fail", 1},
		{"two roots", "application/xml", stream(good) + "<Response/>", 200, "xml", "fail", 1},
		{"no stream", "application/xml", "<Response><Speak>Hi</Speak></Response>", 200, "stream", "fail", 1},
		{"not a WebSocket URL", "application/xml", `<Response><Stream keepCallAlive="true">https://agent.example.com/ws</Stream></Response>`, 200, "stream", "fail", 1},
		{"call ends at once", "application/xml", stream(`bidirectional="true" contentType="audio/x-mulaw;rate=8000"`), 200, "keepCallAlive", "fail", 1},
		{"both tracks two-way", "application/xml", stream(good + ` audioTrack="both"`), 200, "audioTrack", "fail", 1},
		{"undocumented codec", "application/xml", stream(`bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=16000"`), 200, "contentType", "fail", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status == http.StatusFound {
					http.Redirect(w, r, "/elsewhere", http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", tc.ctype)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			res, ce := simulateJSON(t, "--answer-url", srv.URL, "--xml-only")
			checks := res.Checks
			if ce != nil {
				checks, _ = ce.Context["checks"].([]streamsCheck)
			}
			if got := statusOf(checks, tc.check); got != tc.want {
				t.Errorf("check %s = %q, want %q (%+v)", tc.check, got, tc.want, checks)
			}
			if code := clierr.Wrap(ce).ExitCode(); code != tc.exit {
				t.Errorf("exit = %d, want %d (%v)", code, tc.exit, ce)
			}
			if tc.exit == 0 && res.Stream != nil {
				t.Error("--xml-only opened the WebSocket")
			}
		})
	}
}

// An answer URL nobody answers exits 3, as for any unreachable host.
func TestVoiceStreamsSimulate_unreachableExits3(t *testing.T) {
	setFakeCreds(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	if _, ce := simulateJSON(t, "--answer-url", srv.URL); ce == nil || ce.Code != clierr.CodeNetworkError || ce.ExitCode() != ExitAPIError {
		t.Fatalf("err = %v, want NETWORK_ERROR, exit 3", ce)
	}
}

// --dry-run prints the request it would send and sends nothing; bad flags
// are refused before anything is sent.
func TestVoiceStreamsSimulate_dryRunAndFlagsSendNothing(t *testing.T) {
	setFakeCreds(t)
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)

	err, stdout, stderr := execCmd(t, "voice", "streams", "simulate", "--answer-url", srv.URL, "--dry-run")
	if err != nil || stdout != "" || !strings.Contains(stderr, "[dry-run] POST "+srv.URL) || !strings.Contains(stderr, "CallUUID=") {
		t.Errorf("err = %v, stdout = %q, stderr = %q; want the plan on stderr only", err, stdout, stderr)
	}
	for flag, args := range map[string][]string{
		"answer-url":    {"--answer-url", "ftp://" + strings.TrimPrefix(srv.URL, "http://")},
		"answer-method": {"--answer-url", srv.URL, "--answer-method", "PUT"},
		"expect-audio":  {"--answer-url", srv.URL, "--xml-only", "--expect-audio"},
		"duration":      {"--answer-url", srv.URL, "--duration", "31"},
		"direction":     {"--answer-url", srv.URL, "--direction", "sideways"},
	} {
		if _, ce := simulateJSON(t, args...); ce == nil || ce.Code != clierr.CodeBadFlag || ce.Context["flag"] != flag {
			t.Errorf("%v: err = %v, want BAD_FLAG --%s", args, ce, flag)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("the answer URL got %d requests, want none", n)
	}
}

// plivoHosted matches Plivo's hosts and, on any host, a flow runner's path.
func TestPlivoHosted(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://plivo.com/answer":                             true,
		"https://Any-Host.Plivo.COM./answer":                   true,
		"https://runner.example.net//V2/Account/x/Flow/f/node": true,
		"https://agent.example.com/answer":                     false,
		"https://plivo.com.example.com/answer":                 false,
		"https://myplivo.com/answer":                           false,
		"https://agent.example.com/app/v1/account/x/phlo/f":    false,
		"https://agent.example.com/v1/account/x/phloem":        false,
	} {
		if u, _ := url.Parse(raw); plivoHosted(u) != want {
			t.Errorf("plivoHosted(%s) = %v, want %v", raw, !want, want)
		}
	}
}
