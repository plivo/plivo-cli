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
	    {"public_url":"http://abc.ngrok-free.dev","proto":"http","config":{"addr":"http://localhost:8080"}},
	    {"public_url":"https://abc.ngrok-free.dev","proto":"https","config":{"addr":"http://localhost:8080"}}
	  ]
	}`)
	url, ok := extractHTTPSURL(body, 8080)
	if !ok {
		t.Fatal("expected ok")
	}
	if url != "https://abc.ngrok-free.dev" {
		t.Errorf("got %q, want https://abc.ngrok-free.dev", url)
	}
}

func TestExtractHTTPSURL_emptyTunnelList(t *testing.T) {
	body := strings.NewReader(`{"tunnels":[]}`)
	if _, ok := extractHTTPSURL(body, 8080); ok {
		t.Error("expected !ok on empty tunnel list")
	}
}

func TestExtractHTTPSURL_httpOnly(t *testing.T) {
	body := strings.NewReader(`{"tunnels":[{"public_url":"http://abc.ngrok.io"}]}`)
	if _, ok := extractHTTPSURL(body, 8080); ok {
		t.Error("expected !ok when only http tunnel is listed (we want https)")
	}
}

func TestExtractHTTPSURL_garbageJSON(t *testing.T) {
	body := strings.NewReader(`not json at all`)
	if _, ok := extractHTTPSURL(body, 8080); ok {
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

// TestExtractHTTPSURL_ignoresForeignTunnel is SA-04. Discovery returned the
// first HTTPS tunnel advertised on 127.0.0.1:4040, but that port belongs to
// whichever ngrok started first. An unrelated instance — a colleague's, a
// leftover from an earlier run, another tool — could hand us its URL, which
// the caller then writes into the Plivo application's answer_url, routing the
// account's calls to a tunnel we do not own.
func TestExtractHTTPSURL_ignoresForeignTunnel(t *testing.T) {
	body := strings.NewReader(`{
	  "tunnels": [
	    {"public_url":"https://someone-else.ngrok-free.dev","proto":"https","config":{"addr":"http://localhost:9999"}}
	  ]
	}`)
	if url, ok := extractHTTPSURL(body, 8080); ok {
		t.Errorf("adopted a tunnel forwarding to another port: %s", url)
	}
}

// With both present, ours must be chosen regardless of ordering.
func TestExtractHTTPSURL_picksOursAmongMany(t *testing.T) {
	body := strings.NewReader(`{
	  "tunnels": [
	    {"public_url":"https://foreign-a.ngrok-free.dev","proto":"https","config":{"addr":"http://localhost:9999"}},
	    {"public_url":"https://ours.ngrok-free.dev","proto":"https","config":{"addr":"http://localhost:8080"}},
	    {"public_url":"https://foreign-b.ngrok-free.dev","proto":"https","config":{"addr":"http://localhost:7777"}}
	  ]
	}`)
	url, ok := extractHTTPSURL(body, 8080)
	if !ok {
		t.Fatal("did not find our own tunnel")
	}
	if url != "https://ours.ngrok-free.dev" {
		t.Errorf("got %q, want ours.ngrok-free.dev", url)
	}
}

// A tunnel with no addr at all cannot be shown to be ours, so it must not be
// adopted.
func TestExtractHTTPSURL_rejectsMissingAddr(t *testing.T) {
	body := strings.NewReader(`{"tunnels":[{"public_url":"https://x.ngrok-free.dev","proto":"https"}]}`)
	if _, ok := extractHTTPSURL(body, 8080); ok {
		t.Error("adopted a tunnel with no configured address")
	}
}

func TestForwardsToPort(t *testing.T) {
	cases := []struct {
		addr string
		port int
		want bool
	}{
		{"http://localhost:8080", 8080, true},
		{"localhost:8080", 8080, true},
		{"127.0.0.1:8080", 8080, true},
		{":8080", 8080, true},
		{"https://localhost:8080/", 8080, true},
		{"http://localhost:9999", 8080, false},
		{"http://localhost:80801", 8080, false}, // not a prefix match
		{"http://localhost:808", 8080, false},
		{"localhost", 8080, false},
		{"", 8080, false},
	}
	for _, c := range cases {
		if got := forwardsToPort(c.addr, c.port); got != c.want {
			t.Errorf("forwardsToPort(%q, %d) = %v, want %v", c.addr, c.port, got, c.want)
		}
	}
}
