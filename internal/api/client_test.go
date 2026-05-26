package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// captureStderr swaps os.Stderr for a pipe, runs fn, then returns whatever
// was written. Used to verify dry-run output without an external dependency.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()
	_ = w.Close()
	return <-done
}

// captured holds the parts of an inbound HTTP request we care about asserting on.
type captured struct {
	method      string
	path        string
	query       url.Values
	authHeader  string
	contentType string
	accept      string
	userAgent   string
	body        string
}

// newCapturingServer returns a test server that records the request and replies
// with the given response (status + JSON body).
func newCapturingServer(t *testing.T, status int, respBody string, extraHeaders map[string]string) (*httptest.Server, *captured) {
	t.Helper()
	c := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.method = r.Method
		c.path = r.URL.Path
		c.query = r.URL.Query()
		c.authHeader = r.Header.Get("Authorization")
		c.contentType = r.Header.Get("Content-Type")
		c.accept = r.Header.Get("Accept")
		c.userAgent = r.Header.Get("User-Agent")
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			c.body = string(b)
		}
		for k, v := range extraHeaders {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

func TestDo_basicAuth_onRegularToken(t *testing.T) {
	srv, cap := newCapturingServer(t, 200, `{"ok":true}`, nil)
	c := New("MAabc", "regular-token", time.Second)
	c.BaseURL = srv.URL

	_, err := c.Do("GET", srv.URL+"/v1/Account/MAabc/", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Basic auth header = "Basic " + base64("MAabc:regular-token")
	if !strings.HasPrefix(cap.authHeader, "Basic ") {
		t.Errorf("Authorization = %q, want Basic prefix", cap.authHeader)
	}
}

func TestDo_bearerAuth_onScopedToken(t *testing.T) {
	srv, cap := newCapturingServer(t, 200, `{}`, nil)
	c := New("MAabc", "stk_scoped123", time.Second)
	c.BaseURL = srv.URL

	_, err := c.Do("GET", srv.URL+"/v1/agent/token", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cap.authHeader != "Bearer stk_scoped123" {
		t.Errorf("Authorization = %q, want Bearer stk_scoped123", cap.authHeader)
	}
}

func TestDo_standardHeaders(t *testing.T) {
	srv, cap := newCapturingServer(t, 200, `{}`, nil)
	c := New("MAabc", "tok", time.Second)
	c.BaseURL = srv.URL

	_, err := c.Do("POST", srv.URL+"/x", map[string]string{"k": "v"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cap.contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", cap.contentType)
	}
	if cap.accept != "application/json" {
		t.Errorf("Accept = %q", cap.accept)
	}
	if !strings.HasPrefix(cap.userAgent, "plivo-cli/") {
		t.Errorf("User-Agent = %q, want plivo-cli/ prefix", cap.userAgent)
	}
}

func TestDo_noContentTypeWhenNoBody(t *testing.T) {
	srv, cap := newCapturingServer(t, 200, `{}`, nil)
	c := New("MAabc", "tok", time.Second)
	c.BaseURL = srv.URL

	_, err := c.Do("GET", srv.URL+"/x", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cap.contentType != "" {
		t.Errorf("Content-Type should be empty for GET without body, got %q", cap.contentType)
	}
}

func TestDo_marshalsBody(t *testing.T) {
	srv, cap := newCapturingServer(t, 200, `{}`, nil)
	c := New("MAabc", "tok", time.Second)
	c.BaseURL = srv.URL

	body := map[string]any{"src": "+1", "dst": "+2", "text": "hi"}
	_, err := c.Do("POST", srv.URL+"/Message", body, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(cap.body), &parsed); err != nil {
		t.Fatalf("body not valid JSON: %v\n%s", err, cap.body)
	}
	if parsed["src"] != "+1" || parsed["text"] != "hi" {
		t.Errorf("body fields wrong: %v", parsed)
	}
}

func TestDo_appendsQueryParams(t *testing.T) {
	srv, cap := newCapturingServer(t, 200, `{}`, nil)
	c := New("MAabc", "tok", time.Second)
	c.BaseURL = srv.URL

	q := url.Values{}
	q.Set("limit", "5")
	q.Set("offset", "10")
	_, err := c.Do("GET", srv.URL+"/Number", nil, q, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cap.query.Get("limit") != "5" || cap.query.Get("offset") != "10" {
		t.Errorf("query params not forwarded: %v", cap.query)
	}
}

func TestDo_appendsQueryWithAmpersandWhenURLAlreadyHasQuery(t *testing.T) {
	srv, cap := newCapturingServer(t, 200, `{}`, nil)
	c := New("MAabc", "tok", time.Second)
	c.BaseURL = srv.URL

	q := url.Values{}
	q.Set("limit", "5")
	_, err := c.Do("GET", srv.URL+"/Lookup?type=carrier", nil, q, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cap.query.Get("type") != "carrier" || cap.query.Get("limit") != "5" {
		t.Errorf("both query params should be present: %v", cap.query)
	}
}

func TestDo_unmarshalsResponseIntoOut(t *testing.T) {
	srv, _ := newCapturingServer(t, 200, `{"api_id":"x","name":"acme"}`, nil)
	c := New("MAabc", "tok", time.Second)
	c.BaseURL = srv.URL

	var out struct {
		APIID string `json:"api_id"`
		Name  string `json:"name"`
	}
	apiErr, err := c.Do("GET", srv.URL+"/x", nil, nil, &out)
	if err != nil || apiErr != nil {
		t.Fatalf("unexpected: err=%v apiErr=%v", err, apiErr)
	}
	if out.APIID != "x" || out.Name != "acme" {
		t.Errorf("decoded body wrong: %+v", out)
	}
}

func TestDo_returnsAPIErrorOn4xx(t *testing.T) {
	srv, _ := newCapturingServer(t, 401, `{"error":"auth invalid"}`, map[string]string{"X-Request-ID": "rid-123"})
	c := New("MAabc", "wrong-tok", time.Second)
	c.BaseURL = srv.URL

	apiErr, err := c.Do("GET", srv.URL+"/x", nil, nil, nil)
	if err != nil {
		t.Fatalf("transport error should be nil for HTTP 401: %v", err)
	}
	if apiErr == nil {
		t.Fatal("expected *APIError for 401")
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if apiErr.RequestID != "rid-123" {
		t.Errorf("RequestID = %q, want rid-123", apiErr.RequestID)
	}
}

func TestDo_returnsAPIErrorOn5xx(t *testing.T) {
	srv, _ := newCapturingServer(t, 503, `{"error":"upstream down"}`, nil)
	c := New("MAabc", "tok", time.Second)
	c.BaseURL = srv.URL

	apiErr, err := c.Do("GET", srv.URL+"/x", nil, nil, nil)
	if err != nil {
		t.Fatalf("transport err should be nil: %v", err)
	}
	if apiErr == nil || apiErr.StatusCode != 503 {
		t.Errorf("expected APIError with status 503, got %v", apiErr)
	}
	if !apiErr.Retryable {
		t.Error("5xx should be marked retryable")
	}
}

func TestDo_204NoContent_returnsNil(t *testing.T) {
	srv, _ := newCapturingServer(t, 204, "", nil)
	c := New("MAabc", "tok", time.Second)
	c.BaseURL = srv.URL

	var out struct{ X string }
	apiErr, err := c.Do("DELETE", srv.URL+"/x", nil, nil, &out)
	if apiErr != nil || err != nil {
		t.Errorf("204 should return nil/nil; got apiErr=%v err=%v", apiErr, err)
	}
}

func TestDo_emptyBody200_returnsNil(t *testing.T) {
	srv, _ := newCapturingServer(t, 200, "", nil)
	c := New("MAabc", "tok", time.Second)
	c.BaseURL = srv.URL

	apiErr, err := c.Do("GET", srv.URL+"/x", nil, nil, nil)
	if apiErr != nil || err != nil {
		t.Errorf("empty 200 should return nil/nil; got apiErr=%v err=%v", apiErr, err)
	}
}

func TestDo_dryRun_skipsHTTPAndPrintsURL(t *testing.T) {
	srv, _ := newCapturingServer(t, 200, `{}`, nil)
	c := New("MAabc", "tok", time.Second)
	c.BaseURL = srv.URL
	c.DryRun = true

	out := captureStderr(t, func() {
		_, err := c.Do("POST", srv.URL+"/x", map[string]string{"k": "v"}, nil, nil)
		if err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "[dry-run] POST") {
		t.Errorf("dry-run output missing prefix: %q", out)
	}
	if !strings.Contains(out, srv.URL+"/x") {
		t.Errorf("dry-run output missing URL: %q", out)
	}
	if !strings.Contains(out, `"k": "v"`) && !strings.Contains(out, `"k":"v"`) {
		t.Errorf("dry-run output missing body: %q", out)
	}
}

func TestDo_dryRun_doesNotCallServer(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	c := New("MAabc", "tok", time.Second)
	c.BaseURL = srv.URL
	c.DryRun = true

	captureStderr(t, func() {
		_, _ = c.Do("GET", srv.URL+"/x", nil, nil, nil)
	})
	if called {
		t.Error("dry-run should not hit the HTTP server")
	}
}

func TestDo_logRequestCallbackFires(t *testing.T) {
	srv, _ := newCapturingServer(t, 200, `{}`, nil)
	c := New("MAabc", "tok", time.Second)
	c.BaseURL = srv.URL

	var loggedMethod, loggedURL string
	var loggedBody []byte
	c.LogRequest = func(method, url string, body []byte) {
		loggedMethod = method
		loggedURL = url
		loggedBody = body
	}

	body := map[string]string{"k": "v"}
	_, err := c.Do("POST", srv.URL+"/x", body, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if loggedMethod != "POST" {
		t.Errorf("LogRequest method = %q", loggedMethod)
	}
	if !strings.HasSuffix(loggedURL, "/x") {
		t.Errorf("LogRequest url = %q", loggedURL)
	}
	if !strings.Contains(string(loggedBody), `"k"`) {
		t.Errorf("LogRequest body missing payload: %s", loggedBody)
	}
}

func TestDo_decodeError_includesBodyInError(t *testing.T) {
	srv, _ := newCapturingServer(t, 200, `not valid json {`, nil)
	c := New("MAabc", "tok", time.Second)
	c.BaseURL = srv.URL

	var out struct{ X string }
	_, err := c.Do("GET", srv.URL+"/x", nil, nil, &out)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Errorf("error doesn't mention decode: %v", err)
	}
	if !strings.Contains(err.Error(), "not valid json") {
		t.Errorf("error doesn't include body for debug: %v", err)
	}
}

func TestDo_transportError(t *testing.T) {
	c := New("MAabc", "tok", time.Second)
	// Unroutable URL → DNS/connection error.
	c.BaseURL = "http://nonexistent.invalid"

	_, err := c.Do("GET", "http://nonexistent.invalid/x", nil, nil, nil)
	if err == nil {
		t.Fatal("expected transport error")
	}
	if !strings.Contains(err.Error(), "http:") {
		t.Errorf("transport error should be wrapped: %v", err)
	}
}

func TestDo_marshalError_unmarshallableBody(t *testing.T) {
	c := New("MAabc", "tok", time.Second)
	c.BaseURL = "http://127.0.0.1:1"

	// channels can't be JSON-marshaled.
	body := map[string]any{"ch": make(chan int)}
	_, err := c.Do("POST", "http://127.0.0.1:1/x", body, nil, nil)
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if !strings.Contains(err.Error(), "marshal body") {
		t.Errorf("error should mention marshal: %v", err)
	}
}
