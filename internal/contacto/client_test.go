//go:build internal

package contacto

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/config"
)

// rewritingTransport forwards every request to a fixed target host so that
// production-shaped URLs like https://dev-us-auth-api.contactodev.com/... can
// be tested against an httptest server.
type rewritingTransport struct {
	target string
}

func (rt *rewritingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := url.Parse(rt.target)
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

// newClientPointedAt returns a *Client whose HTTP transport rewrites any
// outgoing URL host to the given test-server URL.
func newClientPointedAt(srv *httptest.Server, prof *config.ContactoProfile) *Client {
	c := New(prof)
	c.HTTP = &http.Client{Transport: &rewritingTransport{target: srv.URL}}
	return c
}

func makeProfile() *config.ContactoProfile {
	return &config.ContactoProfile{
		Email:            "you@plivo.com",
		AuthToken:        "session-jwt-xyz",
		AomUUID:          "aom-uuid-123",
		Region:           "us",
		Environment:      "dev",
		BrowserSessionID: "browser-456",
		OrgName:          "test-org",
	}
}

// ─── New ─────────────────────────────────────────────────────────────────────

func TestNew_setsProfileAndTimeout(t *testing.T) {
	p := makeProfile()
	c := New(p)
	if c.Profile != p {
		t.Error("Profile not stored")
	}
	if c.HTTP == nil {
		t.Fatal("HTTP client nil")
	}
	if c.HTTP.Timeout == 0 {
		t.Errorf("Timeout = 0, want non-zero default for regular requests")
	}
}

// ─── Do — happy path + headers ───────────────────────────────────────────────

func TestDo_setsAllStandardHeaders(t *testing.T) {
	var (
		seenAuth, seenAom, seenRegion, seenClient, seenBrowser string
		seenContentType, seenAccept, seenUA                    string
		seenURLPath                                            string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenAom = r.Header.Get("Aom_uuid")
		seenRegion = r.Header.Get("Region")
		seenClient = r.Header.Get("Client-Type")
		seenBrowser = r.Header.Get("Browser-Session-Id")
		seenContentType = r.Header.Get("Content-Type")
		seenAccept = r.Header.Get("Accept")
		seenUA = r.Header.Get("User-Agent")
		seenURLPath = r.URL.Path
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newClientPointedAt(srv, makeProfile())
	_, err := c.Do(context.Background(), "POST", "/v1/contacto-core/contacto-config/phlo", map[string]string{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if seenAuth != "Token session-jwt-xyz" {
		t.Errorf("Authorization = %q, want 'Token session-jwt-xyz' (Contacto uses Token, not Bearer)", seenAuth)
	}
	if seenAom != "aom-uuid-123" {
		t.Errorf("Aom_uuid = %q", seenAom)
	}
	if seenRegion != "us" {
		t.Errorf("Region = %q", seenRegion)
	}
	if seenClient != "web_app" {
		t.Errorf("Client-Type = %q, want web_app", seenClient)
	}
	if seenBrowser != "browser-456" {
		t.Errorf("Browser-Session-Id = %q", seenBrowser)
	}
	if seenContentType != "application/json" {
		t.Errorf("Content-Type = %q", seenContentType)
	}
	if seenAccept != "application/json" {
		t.Errorf("Accept = %q (non-SSE path should be application/json)", seenAccept)
	}
	if !strings.HasPrefix(seenUA, "Plivo-CLI/") {
		t.Errorf("User-Agent = %q, want Plivo-CLI/ prefix", seenUA)
	}
	if seenURLPath != "/v1/contacto-core/contacto-config/phlo" {
		t.Errorf("URL path = %q", seenURLPath)
	}
}

func TestDo_emptyRegion_returnsError(t *testing.T) {
	p := makeProfile()
	p.Region = ""
	c := New(p)
	_, err := c.Do(context.Background(), "GET", "/v1/x", nil)
	if err == nil {
		t.Fatal("expected error when Region is empty")
	}
	if !strings.Contains(err.Error(), "no region") {
		t.Errorf("error should mention region: %v", err)
	}
}

func TestDo_noBody_skipsContentType(t *testing.T) {
	var seenContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenContentType = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newClientPointedAt(srv, makeProfile())
	_, err := c.Do(context.Background(), "GET", "/v1/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if seenContentType != "" {
		t.Errorf("GET with no body should not set Content-Type, got %q", seenContentType)
	}
}

func TestDo_marshalsBody(t *testing.T) {
	var bodyBytes []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newClientPointedAt(srv, makeProfile())
	_, _ = c.Do(context.Background(), "POST", "/v1/x", map[string]any{"name": "acme", "n": 42})

	var parsed map[string]any
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		t.Fatalf("body not JSON: %v\n%s", err, bodyBytes)
	}
	if parsed["name"] != "acme" {
		t.Errorf("body.name = %v", parsed["name"])
	}
}

func TestDo_returnsResponseStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "yes")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()

	c := newClientPointedAt(srv, makeProfile())
	resp, err := c.Do(context.Background(), "POST", "/v1/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 201 {
		t.Errorf("Status = %d", resp.Status)
	}
	if string(resp.Body) != `{"id":"abc"}` {
		t.Errorf("Body = %q", resp.Body)
	}
	if resp.Header.Get("X-Custom") != "yes" {
		t.Errorf("Header X-Custom not forwarded")
	}
}

func TestDo_4xx_returnsResponseNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	c := newClientPointedAt(srv, makeProfile())
	resp, err := c.Do(context.Background(), "GET", "/v1/x", nil)
	if err != nil {
		t.Fatalf("transport err should be nil for HTTP 401: %v", err)
	}
	if resp.Status != 401 {
		t.Errorf("Status = %d, want 401", resp.Status)
	}
	// 4xx classification is the caller's job (via DecodeJSON or clierr.FromHTTP).
}

func TestDo_marshalError(t *testing.T) {
	p := makeProfile()
	c := New(p)
	_, err := c.Do(context.Background(), "POST", "/v1/x", map[string]any{"ch": make(chan int)})
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if !strings.Contains(err.Error(), "marshal body") {
		t.Errorf("error should mention marshal: %v", err)
	}
}

// ─── DecodeJSON ──────────────────────────────────────────────────────────────

func TestDecodeJSON_success(t *testing.T) {
	c := New(makeProfile())
	r := &Response{Status: 200, Body: []byte(`{"name":"acme"}`)}
	var out struct{ Name string }
	if err := c.DecodeJSON(r, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "acme" {
		t.Errorf("Name = %q", out.Name)
	}
}

func TestDecodeJSON_nilOut_returnsNil(t *testing.T) {
	c := New(makeProfile())
	r := &Response{Status: 200, Body: []byte(`{"x":1}`)}
	if err := c.DecodeJSON(r, nil); err != nil {
		t.Errorf("nil out should be no-op, got %v", err)
	}
}

func TestDecodeJSON_emptyBody_returnsNil(t *testing.T) {
	c := New(makeProfile())
	r := &Response{Status: 204, Body: nil}
	var out struct{ X string }
	if err := c.DecodeJSON(r, &out); err != nil {
		t.Errorf("empty body should be no-op, got %v", err)
	}
}

func TestDecodeJSON_non2xx_returnsError(t *testing.T) {
	c := New(makeProfile())
	r := &Response{Status: 401, Body: []byte(`{"error":"unauthorized"}`)}
	err := c.DecodeJSON(r, nil)
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status: %v", err)
	}
	if !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("error should include body: %v", err)
	}
}

func TestDecodeJSON_malformedJSON(t *testing.T) {
	c := New(makeProfile())
	r := &Response{Status: 200, Body: []byte(`not json {`)}
	var out struct{ X string }
	err := c.DecodeJSON(r, &out)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode JSON") {
		t.Errorf("error wrong: %v", err)
	}
}

// ─── applyHeaders edge cases ─────────────────────────────────────────────────

func TestApplyHeaders_omitsBrowserSessionWhenEmpty(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Browser-Session-Id")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	p := makeProfile()
	p.BrowserSessionID = ""
	c := newClientPointedAt(srv, p)
	_, _ = c.Do(context.Background(), "GET", "/v1/x", nil)
	if seen != "" {
		t.Errorf("Browser-Session-Id should be omitted when empty in profile, got %q", seen)
	}
}

// ─── SSE empty-region guard ─────────────────────────────────────────────────

func TestSSE_emptyRegion_returnsError(t *testing.T) {
	p := makeProfile()
	p.Region = ""
	c := New(p)
	err := c.SSE(context.Background(), "/v1/stream", nil, func(SSEEvent) bool { return true })
	if err == nil {
		t.Fatal("expected error when Region is empty")
	}
	if !strings.Contains(err.Error(), "no region") {
		t.Errorf("error should mention region: %v", err)
	}
}

func TestSSE_marshalError(t *testing.T) {
	c := New(makeProfile())
	err := c.SSE(context.Background(), "/v1/stream", map[string]any{"ch": make(chan int)}, func(SSEEvent) bool { return true })
	if err == nil {
		t.Fatal("expected marshal error")
	}
}
