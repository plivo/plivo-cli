package tunnel

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStart_unknownProviderIsAnError(t *testing.T) {
	_, err := Start(context.Background(), 1234, "carrier-pigeon")
	if err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
	// The message must list the real options, or the user has to read source.
	for _, p := range Providers {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error should mention %q, got: %v", p, err)
		}
	}
}

// auto must never hard-fail for want of ngrok; that was the whole complaint.
func TestDescribe_autoNamesAProviderEitherWay(t *testing.T) {
	got := Describe("auto")
	if got == "" {
		t.Fatal("Describe(auto) returned empty")
	}
	if _, err := findNgrok(); err == nil {
		if !strings.Contains(got, "ngrok") {
			t.Errorf("ngrok is installed, so auto should pick it; got %q", got)
		}
	} else if !strings.Contains(got, "localhost.run") {
		t.Errorf("ngrok absent, so auto should fall back to localhost.run; got %q", got)
	}
}

func TestDescribe_explicitProviderIsEchoed(t *testing.T) {
	if got := Describe(ProviderLocalhostRun); got != ProviderLocalhostRun {
		t.Errorf("got %q", got)
	}
}

func TestLhrURL_matchesTheAnnouncementLine(t *testing.T) {
	line := "83dc5fcbd06c3b.lhr.life tunneled with tls termination, https://83dc5fcbd06c3b.lhr.life"
	if got := lhrURL.FindString(line); got != "https://83dc5fcbd06c3b.lhr.life" {
		t.Errorf("got %q", got)
	}
	if lhrURL.FindString("no url here") != "" {
		t.Error("matched a line with no URL")
	}
}

// End-to-end against the real service. Opt-in: CI should not depend on a
// third-party tunnel being up.
//
//	PLIVO_TUNNEL_E2E=1 go test ./internal/tunnel/ -run E2E -v
func TestLocalhostRun_E2E_carriesTraffic(t *testing.T) {
	if os.Getenv("PLIVO_TUNNEL_E2E") != "1" {
		t.Skip("set PLIVO_TUNNEL_E2E=1 to run against the real localhost.run")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "ok path=%s", r.URL.Path)
	})}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	tn, err := startLocalhostRun(ctx, port)
	if err != nil {
		t.Fatalf("tunnel did not start: %v", err)
	}
	defer func() { _ = tn.Close() }()
	if !strings.HasPrefix(tn.PublicURL, "https://") {
		t.Fatalf("expected an https URL, got %q", tn.PublicURL)
	}

	var body string
	for i := 0; i < 10; i++ {
		time.Sleep(2 * time.Second)
		resp, err := http.Get(tn.PublicURL + "/answer")
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		body = string(b)
		if resp.StatusCode == 200 && strings.Contains(body, "ok path=/answer") {
			return
		}
	}
	t.Fatalf("tunnel never carried traffic; last body: %q", body)
}
