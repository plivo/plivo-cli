package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

// wsAcceptServer is a minimal WS endpoint that accepts the upgrade and
// discards whatever the client sends — enough for `streams test` to dial
// and stream synthetic frames into.
func wsAcceptServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		for {
			if _, _, err := c.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// -o json must emit a single parseable summary object on stdout instead of
// the human progress lines.
func TestVoiceStreamsTest_jsonSummary(t *testing.T) {
	setFakeCreds(t)
	wsURL := strings.Replace(wsAcceptServer(t).URL, "http://", "ws://", 1)

	err, stdout, _ := execCmd(t, "voice", "streams", "test", "--to", wsURL, "--duration", "1", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env struct {
		Data streamsTestResult `json:"data"`
	}
	if jsonErr := json.Unmarshal([]byte(stdout), &env); jsonErr != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", jsonErr, stdout)
	}
	if !env.Data.Connected {
		t.Error("connected should be true")
	}
	if !env.Data.HandshakeSent {
		t.Error("handshake_sent should be true")
	}
	if env.Data.FramesSent == 0 {
		t.Error("frames_sent should be > 0")
	}
	if env.Data.Codec != "mulaw" {
		t.Errorf("codec = %q, want mulaw", env.Data.Codec)
	}
	if env.Data.Rate != 8000 {
		t.Errorf("rate = %d, want 8000", env.Data.Rate)
	}
	if env.Data.Errors != 0 {
		t.Errorf("errors = %d, want 0", env.Data.Errors)
	}
	if strings.Contains(stdout, "✓") || strings.Contains(stdout, "Endpoint is ready") {
		t.Errorf("stdout should contain only the JSON summary, got: %q", stdout)
	}
}

// -o table (the human path) must be unaffected by the -o json changes.
func TestVoiceStreamsTest_humanOutputUnchangedWithTableFormat(t *testing.T) {
	setFakeCreds(t)
	wsURL := strings.Replace(wsAcceptServer(t).URL, "http://", "ws://", 1)

	err, stdout, _ := execCmd(t, "-o", "table", "voice", "streams", "test", "--to", wsURL, "--duration", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "Endpoint is ready to receive Plivo audio streams") {
		t.Errorf("expected human progress text with -o table, got: %q", stdout)
	}
}
