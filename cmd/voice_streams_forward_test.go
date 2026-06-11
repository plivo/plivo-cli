package cmd

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

// codecMime maps the human flag to the Plivo <Stream> contentType. Two values
// are valid; everything else falls back to mulaw (the call-path default).
func TestCodecMime(t *testing.T) {
	cases := map[string]string{
		"mulaw":  "audio/x-mulaw",
		"MULAW":  "audio/x-mulaw",
		"l16":    "audio/l16",
		"L16":    "audio/l16",
		"":       "audio/x-mulaw",
		"opus":   "audio/x-mulaw",
		"random": "audio/x-mulaw",
	}
	for in, want := range cases {
		if got := codecMime(in); got != want {
			t.Errorf("codecMime(%q) = %q, want %q", in, got, want)
		}
	}
}

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
			name:     "mulaw 8k unidirectional",
			bidi:     false,
			codec:    "mulaw",
			rate:     8000,
			wantSubs: []string{`contentType="audio/x-mulaw"`, `sampleRate="8000"`, "wss://abc.ngrok.dev/ws"},
			denySubs: []string{`bidirectional="true"`},
		},
		{
			name:     "l16 16k bidirectional",
			bidi:     true,
			codec:    "l16",
			rate:     16000,
			wantSubs: []string{`bidirectional="true"`, `contentType="audio/l16"`, `sampleRate="16000"`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			srv := buildLocalStreamServer(&out, "wss://abc.ngrok.dev/ws", "ws://localhost:7860/ws",
				c.bidi, c.codec, c.rate, false)
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
	srv := buildLocalStreamServer(&out, "wss://x/ws", "ws://x/ws", true, "mulaw", 8000, false)
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
	// Customer URL points at an unbound port; dial will fail.
	srv := buildLocalStreamServer(&logBuf, "wss://x/ws", "ws://127.0.0.1:1/ws", true, "mulaw", 8000, false)
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
	srv := buildLocalStreamServer(&logBuf, "wss://x/ws", botWS, true, "mulaw", 8000, false)
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
