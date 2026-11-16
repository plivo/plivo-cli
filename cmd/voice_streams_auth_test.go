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
	auth := newStreamAuth(authTestToken, "https://tunnel.example", false)
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

// dialWS upgrades /ws on ts with the given signature headers.
func dialWS(t *testing.T, ts *httptest.Server, h http.Header) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: h})
	if err == nil {
		conn.Close(websocket.StatusNormalClosure, "")
	}
	return err
}

// Plivo signs the upgrade over the <Stream> URL as written,
// wss://tunnel.example/ws. Before that it signed http://tunnel.example/ws, and
// both must pass during the transition or every real call is refused.
func TestWebSocketSignedAsPlivoSignsIsAccepted(t *testing.T) {
	ts, _, _ := authFixture(t)
	nonce := "12345678901234567890"
	sign := func(token, uri string) string {
		sig, err := plivosig.Compute(token, uri, http.MethodGet, nonce, map[string]string{})
		if err != nil {
			t.Fatal(err)
		}
		return sig
	}
	newSig := sign(authTestToken, "wss://tunnel.example/ws")
	legacySig := sign(authTestToken, "http://tunnel.example/ws")
	otherSig := sign("other-auth-token", "wss://tunnel.example/ws")

	cases := []struct {
		name   string
		header string
		sig    string
	}{
		{"wss form", plivosig.HeaderSignature, newSig},
		{"legacy http form", plivosig.HeaderSignature, legacySig},
		{"wss form in Ma-V3 only", plivosig.HeaderSignatureMA, newSig},
		{"legacy form in Ma-V3 only", plivosig.HeaderSignatureMA, legacySig},
		{"wss form second of two tokens", plivosig.HeaderSignature, otherSig + "," + newSig},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := dialWS(t, ts, http.Header{c.header: {c.sig}, plivosig.HeaderNonce: {nonce}})
			if err != nil {
				t.Fatalf("upgrade signed the way Plivo signs it was refused: %v", err)
			}
		})
	}
}

// Only the stream URL, in either form, signed with this account's token passes.
func TestWebSocketWrongSignatureIsRejected(t *testing.T) {
	ts, _, _ := authFixture(t)
	nonce := "12345678901234567890"
	cases := []struct{ name, token, uri, nonce string }{
		{"wrong token", "other-auth-token", "wss://tunnel.example/ws", nonce},
		{"https form", authTestToken, "https://tunnel.example/ws", nonce},
		{"ws form", authTestToken, "ws://tunnel.example/ws", nonce},
		{"other host", authTestToken, "wss://evil.example/ws", nonce},
		{"answer URL", authTestToken, "https://tunnel.example/answer", nonce},
		{"nonce mismatch", authTestToken, "wss://tunnel.example/ws", "99999999999999999999"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sig, err := plivosig.Compute(c.token, c.uri, http.MethodGet, c.nonce, map[string]string{})
			if err != nil {
				t.Fatal(err)
			}
			if dialWS(t, ts, http.Header{plivosig.HeaderSignature: {sig}, plivosig.HeaderNonce: {nonce}}) == nil {
				t.Fatal("upgrade with a wrong signature succeeded; the bridge is open")
			}
		})
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
