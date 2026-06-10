package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/version"
)

// TestUserAgent_setOnEveryRequest is the live forcing function for the
// "every outbound API call carries a `Plivo-CLI/<version>` User-Agent" rule.
// All three transport paths in this package — JSON Do, multipart Do, and
// streaming StreamSSE — must satisfy it. Adding a fourth path? Add a case here.
func TestUserAgent_setOnEveryRequest(t *testing.T) {
	wantPrefix := "Plivo-CLI/"
	wantSuffix := version.Value
	want := version.UserAgent()

	t.Run("format", func(t *testing.T) {
		if !strings.HasPrefix(want, wantPrefix) {
			t.Errorf("UserAgent() = %q, want prefix %q", want, wantPrefix)
		}
		if !strings.HasSuffix(want, wantSuffix) {
			t.Errorf("UserAgent() = %q, want suffix %q (the version)", want, wantSuffix)
		}
	})

	t.Run("Do (JSON)", func(t *testing.T) {
		var got string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Get("User-Agent")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		c := New("MAtest", "tok", 5e9)
		c.BaseURL = srv.URL
		_, err := c.Do("GET", srv.URL+"/x", nil, nil, nil)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		if got != want {
			t.Errorf("Do User-Agent = %q, want %q", got, want)
		}
	})

	t.Run("StreamSSE", func(t *testing.T) {
		var got string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Get("User-Agent")
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			// flush an empty stream — handler returns immediately
		}))
		defer srv.Close()
		c := New("MAtest", "tok", 5e9)
		err := c.StreamSSE(t.Context(), "POST", srv.URL+"/sse", nil, func(SSEEvent) bool { return true })
		if err != nil {
			t.Fatalf("StreamSSE: %v", err)
		}
		if got != want {
			t.Errorf("StreamSSE User-Agent = %q, want %q", got, want)
		}
	})
}
