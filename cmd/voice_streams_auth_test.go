package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/plivo/plivo-cli/internal/plivosig"
)

const authTestToken = "test-auth-token"

func authFixture(t *testing.T) (*httptest.Server, *streamAuth, *atomic.Int64) {
	t.Helper()
	var events atomic.Int64
	// Public URLs the CLI would have set on the application.
	auth := &streamAuth{
		authToken: authTestToken,
		answerURL: "https://tunnel.example/answer",
		wsURL:     "wss://tunnel.example/ws",
	}
	srv := buildLocalStreamServer(&strings.Builder{}, "wss://tunnel.example/ws",
		"ws://127.0.0.1:1/ws", true, "mulaw", 8000, false, true, &events, auth)
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts, auth, &events
}

// TestUnsignedAnswerIsRejected is SA-01. The audit reproduced HTTP 200 from an
// unsigned /answer; anyone who learned the tunnel URL could drive the handler.
func TestUnsignedAnswerIsRejected(t *testing.T) {
	ts, _, _ := authFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			req, _ := http.NewRequest(method, ts.URL+"/answer", nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("unsigned /answer returned %d, want 403", resp.StatusCode)
			}
		})
	}
}

// A correctly signed request must still be served, or this breaks the feature
// for every real user. The signature is computed the way Plivo would.
func TestSignedAnswerIsServed(t *testing.T) {
	ts, auth, _ := authFixture(t)
	nonce := "12345678901234567890"
	sig, err := plivosig.Compute(authTestToken, auth.answerURL, http.MethodGet, nonce, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/answer", nil)
	req.Header.Set(plivosig.HeaderSignature, sig)
	req.Header.Set(plivosig.HeaderNonce, nonce)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("correctly signed /answer returned %d, want 200", resp.StatusCode)
	}
}

// The audit sent a marker through an unsigned /ws and got the local handler's
// reply back. The upgrade must now be refused before any bridging happens.
func TestUnsignedWebSocketIsRejected(t *testing.T) {
	ts, _, _ := authFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		conn.Close(websocket.StatusNormalClosure, "")
		t.Fatal("unsigned WebSocket upgrade succeeded; the bridge is still open")
	}
}

// dialSignedWS opens /ws with a handshake signed over signedURL, as Plivo signs
// one, and reports whether the upgrade was accepted.
func dialSignedWS(t *testing.T, ts *httptest.Server, signedURL string) bool {
	t.Helper()
	nonce := "12345678901234567890"
	sig, err := plivosig.Compute(authTestToken, signedURL, http.MethodGet, nonce, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	h := http.Header{}
	h.Set(plivosig.HeaderSignature, sig)
	h.Set(plivosig.HeaderNonce, nonce)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		return false
	}
	conn.Close(websocket.StatusNormalClosure, "")
	return true
}

// Plivo signs the stream handshake with an http(s) scheme, not the wss:// in
// <Stream>. Checking only the wss:// form refused every real stream with a 403.
func TestWebSocketSignedOverHTTPIsAccepted(t *testing.T) {
	ts, _, _ := authFixture(t)
	if !dialSignedWS(t, ts, "http://tunnel.example/ws") {
		t.Fatal("handshake signed over http://tunnel.example/ws was refused")
	}
}

// A server that terminates TLS itself sees the handshake as https://.
func TestWebSocketSignedOverHTTPSIsAccepted(t *testing.T) {
	ts, _, _ := authFixture(t)
	if !dialSignedWS(t, ts, "https://tunnel.example/ws") {
		t.Fatal("handshake signed over https://tunnel.example/ws was refused")
	}
}

func TestWebSocketSignedOverWSSIsStillAccepted(t *testing.T) {
	ts, _, _ := authFixture(t)
	if !dialSignedWS(t, ts, "wss://tunnel.example/ws") {
		t.Fatal("handshake signed over wss://tunnel.example/ws was refused")
	}
}

// Any scheme is accepted, but only for the tunnel URL: not for the local
// listener the request actually arrived on, and not for another host.
func TestWebSocketSignedForAnotherURLIsRejected(t *testing.T) {
	ts, _, _ := authFixture(t)
	for _, u := range []string{
		"http://" + ts.Listener.Addr().String() + "/ws",
		"http://other.example/ws",
		"http://tunnel.example/answer",
	} {
		if dialSignedWS(t, ts, u) {
			t.Errorf("handshake signed over %s was accepted", u)
		}
	}
}

// The escape hatch has to work, or a signature-scheme mismatch in the field
// leaves no way to run the command at all.
func TestSkipSignatureFlagAllowsUnsigned(t *testing.T) {
	var events atomic.Int64
	auth := &streamAuth{skip: true, answerURL: "https://tunnel.example/answer"}
	srv := buildLocalStreamServer(&strings.Builder{}, "wss://tunnel.example/ws",
		"ws://127.0.0.1:1/ws", true, "mulaw", 8000, false, true, &events, auth)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/answer")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("--insecure-skip-signature should serve unsigned requests, got %d", resp.StatusCode)
	}
}
