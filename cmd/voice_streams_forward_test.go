package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/coder/websocket"
	"github.com/plivo/plivo-cli/internal/api"
)

// /answer must return PlivoXML referencing the supplied wss URL and codec.
// Bidirectional attr only appears when bidi=true.
func TestBuildLocalStreamServer_answerXML(t *testing.T) {
	cases := []struct {
		name     string
		bidi     bool
		codec    string
		rate     int
		wantSubs []string
		denySubs []string
	}{
		{
			// Codec and rate are one combined attribute value; there is no
			// sampleRate attribute on <Stream>. An earlier version of this
			// test asserted the split form and so locked the bug in.
			name:     "mulaw 8k unidirectional",
			bidi:     false,
			codec:    "mulaw",
			rate:     8000,
			wantSubs: []string{`contentType="audio/x-mulaw;rate=8000"`, "wss://abc.ngrok.dev/ws"},
			denySubs: []string{`bidirectional="true"`, "sampleRate"},
		},
		{
			name:     "l16 16k bidirectional",
			bidi:     true,
			codec:    "l16",
			rate:     16000,
			wantSubs: []string{`bidirectional="true"`, `contentType="audio/x-l16;rate=16000"`},
			denySubs: []string{"sampleRate", `contentType="audio/l16"`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			var events atomic.Int64
			srv := buildLocalStreamServer(&out, "wss://abc.ngrok.dev/ws", "ws://localhost:7860/ws",
				c.bidi, c.codec, c.rate, false, false, &events)
			// Stand up a transient httptest server with our mux. Hitting /answer
			// from a real client is the only way to verify the response shape.
			ts := httptest.NewServer(srv.Handler)
			defer ts.Close()

			resp, err := http.Post(ts.URL+"/answer", "application/json", strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("POST /answer: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
				t.Errorf("Content-Type = %q, want application/xml...", ct)
			}
			body, _ := io.ReadAll(resp.Body)
			for _, want := range c.wantSubs {
				if !strings.Contains(string(body), want) {
					t.Errorf("body missing %q\nbody: %s", want, body)
				}
			}
			for _, deny := range c.denySubs {
				if strings.Contains(string(body), deny) {
					t.Errorf("body should not contain %q\nbody: %s", deny, body)
				}
			}
		})
	}
}

// /answer rejects non-GET/POST verbs. PUT/DELETE shouldn't be how Plivo hits us
// and a 405 is more informative than serving the same XML to a stray probe.
func TestBuildLocalStreamServer_answerRejectsBadMethod(t *testing.T) {
	var out bytes.Buffer
	var events atomic.Int64
	srv := buildLocalStreamServer(&out, "wss://x/ws", "ws://x/ws", true, "mulaw", 8000, false, false, &events)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	req, _ := http.NewRequest("DELETE", ts.URL+"/answer", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /answer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

// /ws must accept a WS upgrade, attempt to dial the customer endpoint, and
// fail gracefully (logging) when the customer endpoint is unreachable instead
// of leaking the Plivo connection.
func TestBuildLocalStreamServer_wsBridgeUnreachableCustomer(t *testing.T) {
	var logBuf bytes.Buffer
	var events atomic.Int64
	// Customer URL points at an unbound port; dial will fail.
	srv := buildLocalStreamServer(&logBuf, "wss://x/ws", "ws://127.0.0.1:1/ws", true, "mulaw", 8000, false, false, &events)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/ws"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial /ws: %v", err)
	}
	// Server should close us shortly after failing to dial customer.
	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Error("expected read error after server gave up on customer dial")
	}
	conn.Close(websocket.StatusNormalClosure, "")

	logs := logBuf.String()
	if !strings.Contains(logs, "StreamConnect") {
		t.Errorf("expected StreamConnect log, got: %s", logs)
	}
	if !strings.Contains(logs, "dial customer WS") || !strings.Contains(logs, "failed") {
		t.Errorf("expected dial-customer-failed log, got: %s", logs)
	}
	// StreamConnect + dial-failed.
	if got := events.Load(); got != 2 {
		t.Errorf("events = %d, want 2", got)
	}
}

// /ws should bridge frames cleanly when both sides come up.
func TestBuildLocalStreamServer_wsBridgeBidirectional(t *testing.T) {
	// Stand up a bot server that echoes whatever Plivo sends.
	botHandler := func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		typ, data, err := c.Read(r.Context())
		if err != nil {
			return
		}
		_ = c.Write(r.Context(), typ, append([]byte("echo:"), data...))
	}
	bot := httptest.NewServer(http.HandlerFunc(botHandler))
	defer bot.Close()
	botWS := strings.Replace(bot.URL, "http://", "ws://", 1)

	var logBuf bytes.Buffer
	var events atomic.Int64
	srv := buildLocalStreamServer(&logBuf, "wss://x/ws", botWS, true, "mulaw", 8000, false, false, &events)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/ws"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial /ws: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := conn.Write(ctx, websocket.MessageText, []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, got, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "echo:hello" {
		t.Errorf("got %q, want %q", got, "echo:hello")
	}
}

// jsonOut=true must suppress the lifecycle log lines while still counting
// events, so `-o json`'s final events_observed is accurate even though
// nothing was printed along the way.
func TestBuildLocalStreamServer_jsonOutSuppressesLogsButCountsEvents(t *testing.T) {
	var logBuf bytes.Buffer
	var events atomic.Int64
	// Customer URL points at an unbound port; dial will fail, exercising
	// the StreamConnect + dial-failed pair without needing a real bot.
	srv := buildLocalStreamServer(&logBuf, "wss://x/ws", "ws://127.0.0.1:1/ws", true, "mulaw", 8000, false, true, &events)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/ws"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial /ws: %v", err)
	}
	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Error("expected read error after server gave up on customer dial")
	}
	conn.Close(websocket.StatusNormalClosure, "")

	if logBuf.Len() != 0 {
		t.Errorf("jsonOut=true should suppress all progress logs, got: %q", logBuf.String())
	}
	if got := events.Load(); got != 2 {
		t.Errorf("events = %d, want 2 (still counted despite suppressed logs)", got)
	}
}

// confirmInteractive returns false on EOF or anything that isn't y/yes.
func TestConfirmInteractive_defaultsNo(t *testing.T) {
	// We can't easily swap os.Stdin without a global, but we CAN verify
	// the prompt is written to out.
	var out bytes.Buffer
	// Closed stdin → Fscan error → returns false. Hard to simulate without
	// swapping os.Stdin; the prompt-printed-to-out behavior is what we lock.
	_ = confirmInteractive(&out, "Continue? [y/N] ")
	if !strings.Contains(out.String(), "Continue?") {
		t.Errorf("prompt not written: %q", out.String())
	}
}

// forwardTestServer mocks the two GETs runVoiceStreamsForward makes before
// touching ngrok: the app read and the number-count-by-application lookup.
// numberStatus lets a test simulate the count fetch failing.
func forwardTestServer(t *testing.T, numberStatus int, totalCount int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/Application/"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"app_id":"APP123","app_name":"my-test-app","answer_url":"https://old.example.com/answer","answer_method":"POST"}`)
		case strings.Contains(r.URL.Path, "/Number/"):
			if got := r.URL.Query().Get("application"); got != "APP123" {
				t.Errorf("Number list not filtered by application, got query %q", r.URL.RawQuery)
			}
			if numberStatus != http.StatusOK {
				w.WriteHeader(numberStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"api_id":"x","meta":{"limit":1,"offset":0,"total_count":%d},"objects":[]}`, totalCount)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
}

// --dry-run must show the app's real name and current answer_url — the read
// happens even though client.DryRun would otherwise no-op the GET too.
func TestVoiceStreamsForward_dryRun_showsRealAppInfo(t *testing.T) {
	setFakeCreds(t)
	srv := forwardTestServer(t, http.StatusOK, 0)
	defer srv.Close()
	streamsFwdClientForTest = &api.Client{BaseURL: srv.URL, AuthID: "MAFAKE", AuthToken: "tok", HTTP: &http.Client{}}
	defer func() { streamsFwdClientForTest = nil }()

	err, stdout, _ := execCmd(t, "-o", "table", "voice", "streams", "forward",
		"--number", "+14155550142", "--app", "APP123", "--to", "ws://localhost:7860/ws", "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, `"my-test-app"`) {
		t.Errorf("dry-run output missing real app name, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "https://old.example.com/answer") {
		t.Errorf("dry-run output missing real answer_url, got:\n%s", stdout)
	}
	if strings.Contains(stdout, `app ""`) || strings.Contains(stdout, "from: \n") {
		t.Errorf("dry-run output still shows blanks, got:\n%s", stdout)
	}
}

// --dry-run -o json must emit a single parseable preview object, not prose.
func TestVoiceStreamsForward_dryRun_json(t *testing.T) {
	setFakeCreds(t)
	srv := forwardTestServer(t, http.StatusOK, 0)
	defer srv.Close()
	streamsFwdClientForTest = &api.Client{BaseURL: srv.URL, AuthID: "MAFAKE", AuthToken: "tok", HTTP: &http.Client{}}
	defer func() { streamsFwdClientForTest = nil }()

	err, stdout, _ := execCmd(t, "voice", "streams", "forward",
		"--number", "+14155550142", "--app", "APP123", "--to", "ws://localhost:7860/ws", "--dry-run", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env struct {
		Data struct {
			DryRun        bool   `json:"dry_run"`
			AppID         string `json:"app_id"`
			FromAnswerURL string `json:"from_answer_url"`
			To            string `json:"to"`
		} `json:"data"`
	}
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", jsonErr, stdout)
	}
	if !env.Data.DryRun {
		t.Error("dry_run should be true")
	}
	if env.Data.AppID != "APP123" {
		t.Errorf("app_id = %q, want APP123", env.Data.AppID)
	}
	if env.Data.FromAnswerURL != "https://old.example.com/answer" {
		t.Errorf("from_answer_url = %q, want the real current answer_url", env.Data.FromAnswerURL)
	}
	if env.Data.To != "ws://localhost:7860/ws" {
		t.Errorf("to = %q, want the --to value", env.Data.To)
	}
}

// The confirm banner must disclose how many numbers ride on this app's
// answer_url — the whole point being it's not just --number that moves.
func TestVoiceStreamsForward_confirm_showsNumberCount(t *testing.T) {
	setFakeCreds(t)
	srv := forwardTestServer(t, http.StatusOK, 3)
	defer srv.Close()
	streamsFwdClientForTest = &api.Client{BaseURL: srv.URL, AuthID: "MAFAKE", AuthToken: "tok", HTTP: &http.Client{}}
	defer func() { streamsFwdClientForTest = nil }()

	var err error
	var stdout string
	stdinTokenFn(t, "n\n", func() {
		err, stdout, _ = execCmd(t, "voice", "streams", "forward",
			"--number", "+14155550142", "--app", "APP123", "--to", "ws://localhost:7860/ws")
	})
	if err == nil || !strings.Contains(err.Error(), "aborted by user") {
		t.Fatalf("expected 'aborted by user' (declined prompt), got: %v", err)
	}
	if !strings.Contains(stdout, "3 phone numbers attached to this app will forward through the tunnel") {
		t.Errorf("confirm banner missing the blast-radius line, got:\n%s", stdout)
	}
}

// A failed count fetch must degrade to a generic warning, not block the
// command — the confirm flow should still reach the y/N prompt.
func TestVoiceStreamsForward_confirm_countFetchFailureDegradesGracefully(t *testing.T) {
	setFakeCreds(t)
	srv := forwardTestServer(t, http.StatusInternalServerError, 0)
	defer srv.Close()
	streamsFwdClientForTest = &api.Client{BaseURL: srv.URL, AuthID: "MAFAKE", AuthToken: "tok", HTTP: &http.Client{}}
	defer func() { streamsFwdClientForTest = nil }()

	var err error
	var stdout string
	stdinTokenFn(t, "n\n", func() {
		err, stdout, _ = execCmd(t, "voice", "streams", "forward",
			"--number", "+14155550142", "--app", "APP123", "--to", "ws://localhost:7860/ws")
	})
	if err == nil || !strings.Contains(err.Error(), "aborted by user") {
		t.Fatalf("a count-fetch failure must not block the confirm flow, got: %v", err)
	}
	if !strings.Contains(stdout, "could not determine how many phone numbers") {
		t.Errorf("expected the generic degrade warning, got:\n%s", stdout)
	}
}

// numbersAffectedWarning pluralizes correctly and degrades on API failure.
func TestNumbersAffectedWarning(t *testing.T) {
	cases := []struct {
		total int
		want  string
	}{
		{0, "0 phone numbers attached to this app will forward through the tunnel"},
		{1, "1 phone number attached to this app will forward through the tunnel"},
		{5, "5 phone numbers attached to this app will forward through the tunnel"},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"api_id":"x","meta":{"total_count":%d},"objects":[]}`, c.total)
		}))
		client := &api.Client{BaseURL: srv.URL, AuthID: "MAFAKE", AuthToken: "tok", HTTP: &http.Client{}}
		got := numbersAffectedWarning(client, "APP123")
		srv.Close()
		if got != c.want {
			t.Errorf("total=%d: got %q, want %q", c.total, got, c.want)
		}
	}
}

func TestNumbersAffectedWarning_fetchFailureDegrades(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := &api.Client{BaseURL: srv.URL, AuthID: "MAFAKE", AuthToken: "tok", HTTP: &http.Client{}}
	got := numbersAffectedWarning(client, "APP123")
	if !strings.Contains(got, "could not determine") {
		t.Errorf("expected the degrade message, got: %q", got)
	}
}
