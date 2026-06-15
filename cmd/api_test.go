package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
)

// ─── Pure helpers ────────────────────────────────────────────────────────────

func TestResolveAPIMethodAndPath(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		methodFlag  string
		wantMethod  string
		wantPath    string
		wantErrFrag string // substring of err message; "" means no error expected
	}{
		{"positional both", []string{"GET", "/Account/"}, "", "GET", "/Account/", ""},
		{"lowercase normalises", []string{"get", "/Account/"}, "", "GET", "/Account/", ""},
		{"mixed case normalises", []string{"Post", "/Message/"}, "", "POST", "/Message/", ""},
		{"flag method + positional path", []string{"/Message/"}, "POST", "POST", "/Message/", ""},
		{"flag method lower", []string{"/Message/"}, "patch", "PATCH", "/Message/", ""},
		{"matching positional + flag", []string{"DELETE", "/Foo/"}, "DELETE", "DELETE", "/Foo/", ""},
		{"conflicting positional + flag", []string{"DELETE", "/Foo/"}, "POST", "", "", "conflicts"},
		{"missing both", nil, "", "", "", "missing"},
		{"missing path with method only positional", []string{"GET"}, "", "", "", "missing path"},
		{"unknown method", []string{"FOO", "/Bar/"}, "", "", "", "unknown HTTP method"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMethod, gotPath, err := resolveAPIMethodAndPath(tc.args, tc.methodFlag)
			if tc.wantErrFrag != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrFrag)
				}
				if !strings.Contains(err.Error(), tc.wantErrFrag) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.wantErrFrag)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotMethod != tc.wantMethod {
				t.Errorf("method = %q, want %q", gotMethod, tc.wantMethod)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tc.wantPath)
			}
		})
	}
}

func TestValidateAPIPath(t *testing.T) {
	cases := []struct {
		path    string
		wantErr bool
	}{
		{"/Account/", false},
		{"/v1/Account/MAxxx/Message/", false},
		{"https://api.plivo.com/v1/Account/", true},
		{"http://evil.example.com/", true},
		{"ftp://host/path", true},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			err := validateAPIPath(tc.path)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateAPIPath(%q) err = %v, wantErr = %v", tc.path, err, tc.wantErr)
			}
		})
	}
}

func TestExpandAPIPath(t *testing.T) {
	c := &api.Client{
		BaseURL: "https://api.example.com/v1/cli/api",
		AuthID:  "MAFAKE001",
	}
	cases := []struct {
		path string
		want string
	}{
		{"/Message/", "https://api.example.com/v1/cli/api/v1/Account/MAFAKE001/Message/"},
		{"Application/", "https://api.example.com/v1/cli/api/v1/Account/MAFAKE001/Application/"},
		{"/v1/Account/MAOTHER/Message/", "https://api.example.com/v1/cli/api/v1/Account/MAOTHER/Message/"},
		{"/v1/Lookup/Number/+14155551234", "https://api.example.com/v1/cli/api/v1/Lookup/Number/+14155551234"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := expandAPIPath(c, tc.path)
			if got != tc.want {
				t.Errorf("expandAPIPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestParseAPIQueryFlags(t *testing.T) {
	cases := []struct {
		name    string
		flags   []string
		want    url.Values
		wantErr bool
	}{
		{"single", []string{"limit=10"}, url.Values{"limit": {"10"}}, false},
		{"repeated key", []string{"k=v1", "k=v2"}, url.Values{"k": {"v1", "v2"}}, false},
		{"value with =", []string{"filter=a=b"}, url.Values{"filter": {"a=b"}}, false},
		{"empty", nil, url.Values{}, false},
		{"missing equals", []string{"badflag"}, nil, true},
		{"empty key", []string{"=value"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseAPIQueryFlags(tc.flags)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if err == nil && !equalValues(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func equalValues(a, b url.Values) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || len(va) != len(vb) {
			return false
		}
		for i := range va {
			if va[i] != vb[i] {
				return false
			}
		}
	}
	return true
}

func TestParseAPIHeaderFlags(t *testing.T) {
	cases := []struct {
		name    string
		flags   []string
		want    http.Header
		wantErr bool
	}{
		{"single", []string{"X-Foo: bar"}, http.Header{"X-Foo": {"bar"}}, false},
		{"trims whitespace", []string{"X-Foo:bar"}, http.Header{"X-Foo": {"bar"}}, false},
		{"repeated", []string{"X-Foo: a", "X-Foo: b"}, http.Header{"X-Foo": {"a", "b"}}, false},
		{"empty value allowed", []string{"X-Empty:"}, http.Header{"X-Empty": {""}}, false},
		{"no colon", []string{"X-Foo bar"}, nil, true},
		{"empty key", []string{": value"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseAPIHeaderFlags(tc.flags)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len(got) = %d, want %d (%v vs %v)", len(got), len(tc.want), got, tc.want)
			}
			for k, vs := range tc.want {
				gv := got[http.CanonicalHeaderKey(k)]
				if len(gv) != len(vs) {
					t.Errorf("header %s: got %v, want %v", k, gv, vs)
					continue
				}
				for i := range vs {
					if gv[i] != vs[i] {
						t.Errorf("header %s[%d]: got %q, want %q", k, i, gv[i], vs[i])
					}
				}
			}
		})
	}
}

func TestReadAPIBody(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got, err := readAPIBody("", strings.NewReader(""))
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("empty body should be nil, got %q", got)
		}
	})

	t.Run("literal", func(t *testing.T) {
		got, err := readAPIBody(`{"k":"v"}`, strings.NewReader(""))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != `{"k":"v"}` {
			t.Errorf("got %q", got)
		}
	})

	t.Run("file", func(t *testing.T) {
		dir := t.TempDir()
		fp := filepath.Join(dir, "body.json")
		_ = os.WriteFile(fp, []byte(`{"from":"file"}`), 0o644)
		got, err := readAPIBody("@"+fp, strings.NewReader(""))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != `{"from":"file"}` {
			t.Errorf("got %q", got)
		}
	})

	t.Run("stdin", func(t *testing.T) {
		got, err := readAPIBody("@-", strings.NewReader(`{"from":"stdin"}`))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != `{"from":"stdin"}` {
			t.Errorf("got %q", got)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := readAPIBody("@/nope/missing.json", strings.NewReader(""))
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})
}

func TestIsMutatingMethod(t *testing.T) {
	cases := map[string]bool{
		"GET":     false,
		"HEAD":    false,
		"OPTIONS": false,
		"POST":    true,
		"PUT":     true,
		"PATCH":   true,
		"DELETE":  true,
	}
	for m, want := range cases {
		if got := isMutatingMethod(m); got != want {
			t.Errorf("isMutatingMethod(%q) = %v, want %v", m, got, want)
		}
	}
}

func TestLooksLikeJSON(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{`{"k":"v"}`, true},
		{`[1,2,3]`, true},
		{`  {"k":"v"}`, true},
		{"\n\t[]", true},
		{"hello", false},
		{"", false},
		{"   ", false},
	}
	for _, tc := range cases {
		if got := looksLikeJSON([]byte(tc.in)); got != tc.want {
			t.Errorf("looksLikeJSON(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// ─── End-to-end via execCmd + httptest ───────────────────────────────────────

// startAPIServer returns an httptest server that records all requests
// (method, path, query, body, headers) and replies with the given body +
// status + Content-Type. Mutex-guarded for safe access from the handler
// goroutine.
func startAPIServer(t *testing.T, status int, contentType, body string) (*httptest.Server, func() []*http.Request) {
	t.Helper()
	var mu sync.Mutex
	reqs := []*http.Request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read + restore the body so the test can inspect it after the request.
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		r.Body = io.NopCloser(strings.NewReader(string(b)))
		// Stash the body as a request header so we can read it later without race.
		r.Header.Set("X-Test-Captured-Body", string(b))
		mu.Lock()
		reqs = append(reqs, r.Clone(r.Context()))
		mu.Unlock()
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	getter := func() []*http.Request {
		mu.Lock()
		defer mu.Unlock()
		out := make([]*http.Request, len(reqs))
		copy(out, reqs)
		return out
	}
	return srv, getter
}

// pointAPIAtTestServer installs an api.Client into the apiClientForTest hook
// so `plivo api` reaches the httptest server with realistic auth + BaseURL
// behaviour. The hook is automatically cleared on test cleanup so later tests
// fall back to the normal getClient path.
func pointAPIAtTestServer(t *testing.T, srv *httptest.Server) {
	t.Helper()
	c := &api.Client{
		BaseURL:   srv.URL,
		AuthID:    "MAFAKEFORTEST",
		AuthToken: "fake-token",
		HTTP:      &http.Client{},
	}
	apiClientForTest = c
	t.Cleanup(func() { apiClientForTest = nil })
}

func TestAPICmd_GET_passthroughJSON(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startAPIServer(t, 200, "application/json", `{"name":"acme"}`)
	pointAPIAtTestServer(t, srv)

	err, stdout, _ := execCmd(t, "api", "GET", "/Account/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := hits()
	if len(got) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(got))
	}
	if got[0].Method != "GET" {
		t.Errorf("method = %q, want GET", got[0].Method)
	}
	// Default JSON output (non-TTY) wraps the upstream body in {"data": ...}
	if !strings.Contains(stdout, `"name"`) || !strings.Contains(stdout, `"acme"`) {
		t.Errorf("stdout missing upstream payload, got: %s", stdout)
	}
}

func TestAPICmd_POST_refusesWithoutYes(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startAPIServer(t, 200, "application/json", `{}`)
	pointAPIAtTestServer(t, srv)

	err, _, _ := execCmd(t, "api", "POST", "/Message/", "--body", `{"src":"+1","dst":"+1","text":"hi"}`)
	if err == nil {
		t.Fatal("expected DESTRUCTIVE_REFUSED, got nil")
	}
	if !strings.Contains(err.Error(), "DESTRUCTIVE_REFUSED") {
		t.Errorf("expected DESTRUCTIVE_REFUSED in error, got: %v", err)
	}
	if len(hits()) != 0 {
		t.Errorf("mutating verb hit the network without --yes (hits=%d)", len(hits()))
	}
}

func TestAPICmd_DELETE_refusesWithoutYes(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startAPIServer(t, 204, "", "")
	pointAPIAtTestServer(t, srv)

	err, _, _ := execCmd(t, "api", "DELETE", "/Application/APP-123/")
	if err == nil {
		t.Fatal("expected DESTRUCTIVE_REFUSED, got nil")
	}
	if !strings.Contains(err.Error(), "DESTRUCTIVE_REFUSED") {
		t.Errorf("expected DESTRUCTIVE_REFUSED in error, got: %v", err)
	}
	if len(hits()) != 0 {
		t.Errorf("DELETE hit the network without --yes")
	}
}

func TestAPICmd_POST_dryRunSkipsNetwork(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startAPIServer(t, 200, "application/json", `{}`)
	pointAPIAtTestServer(t, srv)

	err, _, stderr := execCmd(t, "api", "POST", "/Message/", "--body", `{"src":"+1","dst":"+1"}`, "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hits()) != 0 {
		t.Errorf("dry-run hit the network (hits=%d)", len(hits()))
	}
	if !strings.Contains(stderr, "[dry-run]") || !strings.Contains(stderr, "POST") {
		t.Errorf("stderr should mention dry-run + method, got: %s", stderr)
	}
}

func TestAPICmd_POST_withYes_callsUpstream(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startAPIServer(t, 201, "application/json", `{"api_id":"abc","message_uuid":["uuid-1"]}`)
	pointAPIAtTestServer(t, srv)

	err, stdout, _ := execCmd(t, "api", "POST", "/Message/",
		"--body", `{"src":"+1","dst":"+1","text":"hi"}`,
		"--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := hits()
	if len(got) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(got))
	}
	if got[0].Method != "POST" {
		t.Errorf("method = %q, want POST", got[0].Method)
	}
	// Body was forwarded
	bodyCaptured := got[0].Header.Get("X-Test-Captured-Body")
	if !strings.Contains(bodyCaptured, `"src"`) || !strings.Contains(bodyCaptured, `"+1"`) {
		t.Errorf("body not forwarded, got: %s", bodyCaptured)
	}
	// JSON Content-Type was auto-set
	if !strings.Contains(got[0].Header.Get("Content-Type"), "application/json") {
		t.Errorf("Content-Type not set, got: %s", got[0].Header.Get("Content-Type"))
	}
	if !strings.Contains(stdout, `"api_id"`) {
		t.Errorf("stdout missing upstream payload, got: %s", stdout)
	}
}

func TestAPICmd_4xxEmitsUpstreamErrorEnvelope(t *testing.T) {
	setFakeCreds(t)
	srv, _ := startAPIServer(t, 422, "application/json", `{"error":"validation failed","field":"src"}`)
	pointAPIAtTestServer(t, srv)

	err, _, stderr := execCmd(t, "api", "GET", "/Message/", "--query", "limit=10")
	if err == nil {
		t.Fatal("expected error on 422, got nil")
	}
	// The renderer prints the structured envelope to stderr; the err itself
	// carries the code so we can assert on both.
	if !strings.Contains(err.Error(), "UPSTREAM_ERROR") {
		t.Errorf("expected UPSTREAM_ERROR code in err: %v", err)
	}
	// Default JSON output for non-TTY → error envelope should contain
	// "upstream" context including the body. Stderr (JSON envelope) is what
	// the renderer writes; the err itself is the underlying *clierr.Error.
	_ = stderr
}

func TestAPICmd_5xxEmitsRetryableEnvelope(t *testing.T) {
	setFakeCreds(t)
	srv, _ := startAPIServer(t, 503, "application/json", `{"error":"upstream unavailable"}`)
	pointAPIAtTestServer(t, srv)

	err, _, _ := execCmd(t, "api", "GET", "/Account/")
	if err == nil {
		t.Fatal("expected error on 503, got nil")
	}
	if !strings.Contains(err.Error(), "UPSTREAM_ERROR") {
		t.Errorf("expected UPSTREAM_ERROR code, got: %v", err)
	}
}

func TestAPICmd_QueryParamsForwarded(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startAPIServer(t, 200, "application/json", `{}`)
	pointAPIAtTestServer(t, srv)

	err, _, _ := execCmd(t, "api", "GET", "/Message/",
		"--query", "limit=10",
		"--query", "offset=5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := hits()
	if len(got) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(got))
	}
	// Proxied through; the path the server sees includes the scheme/host
	// because the proxy is applied at the transport level. Inspect URL.RawQuery.
	if !strings.Contains(got[0].URL.RawQuery, "limit=10") || !strings.Contains(got[0].URL.RawQuery, "offset=5") {
		t.Errorf("query params not forwarded: %s", got[0].URL.RawQuery)
	}
}

func TestAPICmd_HeadersForwarded(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startAPIServer(t, 200, "application/json", `{}`)
	pointAPIAtTestServer(t, srv)

	err, _, _ := execCmd(t, "api", "GET", "/Account/", "--header", "X-Trace-ID: abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := hits()
	if len(got) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(got))
	}
	if got[0].Header.Get("X-Trace-Id") != "abc123" {
		t.Errorf("custom header not forwarded: %v", got[0].Header)
	}
}

func TestAPICmd_BodyFromFile(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startAPIServer(t, 200, "application/json", `{}`)
	pointAPIAtTestServer(t, srv)

	dir := t.TempDir()
	bodyPath := filepath.Join(dir, "msg.json")
	if err := os.WriteFile(bodyPath, []byte(`{"src":"+1","dst":"+2","text":"hi"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err, _, _ := execCmd(t, "api", "POST", "/Message/", "--body", "@"+bodyPath, "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := hits()
	if len(got) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(got))
	}
	body := got[0].Header.Get("X-Test-Captured-Body")
	if !strings.Contains(body, `"src"`) {
		t.Errorf("file body not forwarded: %s", body)
	}
}

func TestAPICmd_AbsolutePathPreserved(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startAPIServer(t, 200, "application/json", `{}`)
	pointAPIAtTestServer(t, srv)

	err, _, _ := execCmd(t, "api", "GET", "/v1/Account/MAOTHER999/Message/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := hits()
	if len(got) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(got))
	}
	if !strings.Contains(got[0].URL.Path, "/v1/Account/MAOTHER999/Message/") {
		t.Errorf("absolute path not preserved: %s", got[0].URL.Path)
	}
}

func TestAPICmd_RejectsFullURL(t *testing.T) {
	setFakeCreds(t)
	err, _, _ := execCmd(t, "api", "GET", "https://evil.example.com/something")
	if err == nil {
		t.Fatal("expected error when path contains scheme, got nil")
	}
	// BAD_INPUT envelope
	if !strings.Contains(err.Error(), "BAD_INPUT") && !strings.Contains(err.Error(), "scheme") {
		t.Errorf("expected BAD_INPUT (scheme rejection) in err: %v", err)
	}
}

func TestAPICmd_PipedBodyFromStdin(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startAPIServer(t, 200, "application/json", `{}`)
	pointAPIAtTestServer(t, srv)

	stdinTokenFn(t, `{"piped":true}`, func() {
		err, _, _ := execCmd(t, "api", "--method", "POST", "/Message/", "--body", "@-", "--yes")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	got := hits()
	if len(got) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(got))
	}
	body := got[0].Header.Get("X-Test-Captured-Body")
	if !strings.Contains(body, `"piped"`) {
		t.Errorf("stdin body not forwarded: %s", body)
	}
}

func TestAPICmd_UnknownMethodFails(t *testing.T) {
	setFakeCreds(t)
	err, _, _ := execCmd(t, "api", "FOOBAR", "/Account/")
	if err == nil {
		t.Fatal("expected error for unknown method, got nil")
	}
	if !strings.Contains(err.Error(), "method") {
		t.Errorf("expected method-related error, got: %v", err)
	}
}

// Smoke: response renders even when upstream returns non-JSON content.
func TestAPICmd_NonJSONUpstreamPassesThrough(t *testing.T) {
	setFakeCreds(t)
	srv, _ := startAPIServer(t, 200, "text/plain", "plain text response\n")
	pointAPIAtTestServer(t, srv)

	err, stdout, _ := execCmd(t, "api", "GET", "/Account/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default format is JSON for non-TTY → wrapped in {"data": "..."}
	if !strings.Contains(stdout, "plain text response") {
		t.Errorf("body not forwarded to stdout: %s", stdout)
	}
}

// Sanity: upstream JSON error body shows up in the envelope's context.upstream.body
func TestAPICmd_UpstreamErrorBodyPreservedInEnvelope(t *testing.T) {
	// Directly exercise the helper so we can inspect the Context map without
	// going through the full execCmd→renderer round-trip (which strips Context
	// down to whatever the JSON envelope writer chose to keep).
	gotErr := apiErrorFromUpstream(400, "req-1", []byte(`{"error":"missing src"}`), true)
	cerr, ok := gotErr.(*clierr.Error)
	if !ok {
		t.Fatalf("expected *clierr.Error, got %T", gotErr)
	}
	if cerr.Code != clierr.CodeUpstreamError {
		t.Errorf("code = %s, want UPSTREAM_ERROR", cerr.Code)
	}
	if cerr.StatusCode != 400 {
		t.Errorf("status_code = %d, want 400", cerr.StatusCode)
	}
	// The upstream body should be parsed and tucked into context.upstream.body.
	js, _ := json.Marshal(cerr)
	if !strings.Contains(string(js), "missing src") {
		t.Errorf("envelope JSON missing upstream body: %s", string(js))
	}
	if !strings.Contains(string(js), "upstream") {
		t.Errorf("envelope JSON missing upstream context: %s", string(js))
	}
}
