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

func atkFixture(t *testing.T) *httptest.Server {
	t.Helper()
	var ev atomic.Int64
	auth := &streamAuth{
		authToken: "test-auth-token",
		answerURL: "https://tunnel.example/answer",
		wsURL:     "wss://tunnel.example/ws",
	}
	srv := buildLocalStreamServer(&strings.Builder{}, "wss://tunnel.example/ws",
		"ws://127.0.0.1:1/ws", true, "mulaw", 8000, false, true, &ev, auth)
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts
}

// Every one of these is an attempt to get past the signature check.
func TestAttackAnswerBypass(t *testing.T) {
	ts := atkFixture(t)
	valid, _ := plivosig.Compute("test-auth-token", "https://tunnel.example/answer",
		http.MethodGet, "12345678901234567890", map[string]string{})

	attacks := []struct{ name, sig, nonce string }{
		{"no headers at all", "", ""},
		{"nonce but no signature", "", "12345678901234567890"},
		{"signature but no nonce", valid, ""},
		{"empty-string signature", "", "n"},
		{"comma soup", ",,,,", "12345678901234567890"},
		{"valid sig, wrong nonce", valid, "99999999999999999999"},
		{"whitespace-only sig", "    ", "12345678901234567890"},
		{"truncated valid sig", valid[:len(valid)-4], "12345678901234567890"},
		{"sig with trailing junk", valid + "AAAA", "12345678901234567890"},
		{"base64 of empty", "", "12345678901234567890"},
		{"null byte injected", valid + "\x00", "12345678901234567890"},
	}
	for _, a := range attacks {
		t.Run(a.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/answer", nil)
			if a.sig != "" {
				req.Header.Set(plivosig.HeaderSignature, a.sig)
			}
			if a.nonce != "" {
				req.Header.Set(plivosig.HeaderNonce, a.nonce)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return // rejected at transport level, fine
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				t.Errorf("BYPASS: %s returned 200", a.name)
			}
		})
	}
}

// The Ma header is a legitimate fallback; it must not be a free pass.
func TestAttackMaHeaderIsNotAFreePass(t *testing.T) {
	ts := atkFixture(t)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/answer", nil)
	req.Header.Set(plivosig.HeaderSignatureMA, "AAAAnotarealsignature=")
	req.Header.Set(plivosig.HeaderNonce, "12345678901234567890")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("BYPASS: a bogus Ma signature was accepted")
	}
}

// Signing the LOCAL url instead of the public one must not work, or an
// attacker who guesses the listener address gets in.
func TestAttackSigningLocalURL(t *testing.T) {
	ts := atkFixture(t)
	nonce := "12345678901234567890"
	localSig, _ := plivosig.Compute("test-auth-token", ts.URL+"/answer",
		http.MethodGet, nonce, map[string]string{})
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/answer", nil)
	req.Header.Set(plivosig.HeaderSignature, localSig)
	req.Header.Set(plivosig.HeaderNonce, nonce)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("BYPASS: a signature over the LOCAL url was accepted")
	}
}

// /ws must refuse before the upgrade under every unsigned variation.
func TestAttackWebSocketBypass(t *testing.T) {
	ts := atkFixture(t)
	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/ws"
	for _, h := range []http.Header{
		{},
		{plivosig.HeaderNonce: []string{"12345678901234567890"}},
		{plivosig.HeaderSignature: []string{"AAAA="}, plivosig.HeaderNonce: []string{"1"}},
		{"Origin": []string{"https://tunnel.example"}},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: h})
		cancel()
		if err == nil {
			conn.Close(websocket.StatusNormalClosure, "")
			t.Errorf("BYPASS: unsigned /ws upgrade accepted with headers %v", h)
		}
	}
}
