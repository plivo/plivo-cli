package tunnel

import (
	"strings"
	"testing"
)

// Most of tunnel.go is subprocess-bound — we can't unit-test the spawn
// without a real ngrok. extractHTTPSURL is the pure-function piece worth
// covering directly.

func TestExtractHTTPSURL_picksHTTPSTunnel(t *testing.T) {
	body := strings.NewReader(`{
	  "tunnels": [
	    {"public_url":"http://abc.ngrok-free.dev","proto":"http"},
	    {"public_url":"https://abc.ngrok-free.dev","proto":"https"}
	  ]
	}`)
	url, ok := extractHTTPSURL(body)
	if !ok {
		t.Fatal("expected ok")
	}
	if url != "https://abc.ngrok-free.dev" {
		t.Errorf("got %q, want https://abc.ngrok-free.dev", url)
	}
}

func TestExtractHTTPSURL_emptyTunnelList(t *testing.T) {
	body := strings.NewReader(`{"tunnels":[]}`)
	if _, ok := extractHTTPSURL(body); ok {
		t.Error("expected !ok on empty tunnel list")
	}
}

func TestExtractHTTPSURL_httpOnly(t *testing.T) {
	body := strings.NewReader(`{"tunnels":[{"public_url":"http://abc.ngrok.io"}]}`)
	if _, ok := extractHTTPSURL(body); ok {
		t.Error("expected !ok when only http tunnel is listed (we want https)")
	}
}

func TestExtractHTTPSURL_garbageJSON(t *testing.T) {
	body := strings.NewReader(`not json at all`)
	if _, ok := extractHTTPSURL(body); ok {
		t.Error("expected !ok on garbage")
	}
}

func TestFindNgrok_returnsHintWhenMissing(t *testing.T) {
	// Hard to test the negative path without dirty-ing PATH. We at least
	// verify findNgrok returns a string containing the install hint when
	// ngrok genuinely isn't installed. Skip if ngrok IS installed locally.
	_, err := findNgrok()
	if err == nil {
		t.Skip("ngrok is installed locally; skipping missing-binary check")
	}
	if !strings.Contains(err.Error(), "ngrok") || !strings.Contains(err.Error(), "https://ngrok.com") {
		t.Errorf("error should mention ngrok + install URL, got: %v", err)
	}
}
