package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/plivo/plivo-cli/internal/clierr"
)

// streamServer is a WebSocket endpoint for `streams test`. It records the
// event of every frame the CLI sends and the status the CLI closed with, and
// answers each media frame with reply(payload) when reply is set; with a nil
// reply it is a silent sink.
type streamServer struct {
	*httptest.Server
	mu     sync.Mutex
	events []string
	status websocket.StatusCode
	done   chan struct{}
}

func newStreamServer(t *testing.T, reply func(payload string) string) *streamServer {
	t.Helper()
	s := &streamServer{done: make(chan struct{})}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer close(s.done)
		defer c.CloseNow()
		for {
			_, data, err := c.Read(r.Context())
			if err != nil {
				s.mu.Lock()
				s.status = websocket.CloseStatus(err)
				s.mu.Unlock()
				return
			}
			var ev struct {
				Event string `json:"event"`
				Media struct {
					Payload string `json:"payload"`
				} `json:"media"`
			}
			_ = json.Unmarshal(data, &ev)
			s.mu.Lock()
			s.events = append(s.events, ev.Event)
			s.mu.Unlock()
			if reply != nil && ev.Event == "media" {
				if c.Write(r.Context(), websocket.MessageText, []byte(reply(ev.Media.Payload))) != nil {
					return
				}
			}
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *streamServer) wsURL() string { return "ws" + strings.TrimPrefix(s.URL, "http") }

// seen waits for the CLI to hang up, then returns the events it sent and the
// status it closed with.
func (s *streamServer) seen(t *testing.T) ([]string, websocket.StatusCode) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the CLI never closed the WebSocket")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.events, s.status
}

// assertStopThenClose checks the endpoint got a stop frame last and then a
// normal close, rather than a dropped connection.
func (s *streamServer) assertStopThenClose(t *testing.T) {
	t.Helper()
	events, status := s.seen(t)
	if len(events) == 0 || events[len(events)-1] != "stop" || status != websocket.StatusNormalClosure {
		last := ""
		if len(events) > 0 {
			last = events[len(events)-1]
		}
		t.Errorf("endpoint saw %d frames ending with %q, then close status %v; want stop, then a normal close",
			len(events), last, status)
	}
}

// streamsTestJSON runs `voice streams test --duration 1 -o json` with args
// and decodes the summary on stdout, which is printed even when the command
// then fails.
func streamsTestJSON(t *testing.T, args ...string) (streamsTestResult, string, error) {
	t.Helper()
	err, stdout, _ := execCmd(t, append([]string{"voice", "streams", "test", "--duration", "1", "-o", "json"}, args...)...)
	var env struct {
		Data streamsTestResult `json:"data"`
	}
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("stdout is not one JSON summary: %v (command error: %v)\nstdout: %q", jsonErr, err, stdout)
	}
	return env.Data, stdout, err
}

// -o json must emit a single parseable summary object on stdout instead of
// the human progress lines.
func TestVoiceStreamsTest_jsonSummary(t *testing.T) {
	setFakeCreds(t)
	srv := newStreamServer(t, nil)

	res, stdout, err := streamsTestJSON(t, "--to", srv.wsURL())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Connected {
		t.Error("connected should be true")
	}
	if !res.HandshakeSent {
		t.Error("handshake_sent should be true")
	}
	if res.FramesSent == 0 {
		t.Error("frames_sent should be > 0")
	}
	if res.Codec != "mulaw" {
		t.Errorf("codec = %q, want mulaw", res.Codec)
	}
	if res.Rate != 8000 {
		t.Errorf("rate = %d, want 8000", res.Rate)
	}
	if res.Errors != 0 {
		t.Errorf("errors = %d, want 0", res.Errors)
	}
	if strings.Contains(stdout, "✓") || strings.Contains(stdout, "Endpoint is ready") {
		t.Errorf("stdout should contain only the JSON summary, got: %q", stdout)
	}
}

// -o table (the human path) must be unaffected by the -o json changes.
func TestVoiceStreamsTest_humanOutputUnchangedWithTableFormat(t *testing.T) {
	setFakeCreds(t)
	srv := newStreamServer(t, nil)

	err, stdout, _ := execCmd(t, "-o", "table", "voice", "streams", "test", "--to", srv.wsURL(), "--duration", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "Endpoint is ready to receive Plivo audio streams") {
		t.Errorf("expected human progress text with -o table, got: %q", stdout)
	}
}

// With --bidirectional the endpoint gets stop and then a normal close: the
// read timeout used to drop the socket before stop went out.
func TestVoiceStreamsTest_bidirectionalSilentStopsBeforeClose(t *testing.T) {
	setFakeCreds(t)
	srv := newStreamServer(t, nil)

	res, _, err := streamsTestJSON(t, "--to", srv.wsURL(), "--bidirectional")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Bidirectional || res.FramesReadBack != 0 {
		t.Errorf("bidirectional = %v, frames_read_back = %d; want true and nothing back", res.Bidirectional, res.FramesReadBack)
	}
	srv.assertStopThenClose(t)
}

// When the endpoint hangs up mid-stream the error carries its close code and
// reason, instead of writes retried on a dead socket.
func TestVoiceStreamsTest_reportsEndpointClose(t *testing.T) {
	setFakeCreds(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		if _, _, err := c.Read(r.Context()); err != nil { // the start frame
			return
		}
		_ = c.Close(websocket.StatusPolicyViolation, "signature required")
	}))
	t.Cleanup(srv.Close)

	err, _, _ := execCmd(t, "voice", "streams", "test", "--to", "ws"+strings.TrimPrefix(srv.URL, "http"),
		"--duration", "1", "--bidirectional", "-o", "json")
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Code != clierr.CodeNetworkError {
		t.Fatalf("err = %v, want NETWORK_ERROR", err)
	}
	if !strings.Contains(ce.Message, "1008") || !strings.Contains(ce.Message, "signature required") {
		t.Errorf("message = %q, want the endpoint's close code and reason", ce.Message)
	}
}
