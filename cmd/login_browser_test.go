package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/zalando/go-keyring"
)

// TestCLITokenEnvelope_unmarshalsResponse locks in the wire shape we
// expect from the auth server's /v1/accounts/cli/token: the standard
// {api_id, data} envelope. If the response wrapper drifts (data → result,
// or no envelope at all), this fails loudly before the silent empty-bundle
// path in runLoginBrowser surfaces it as 'token redemption returned an empty bundle'.
func TestCLITokenEnvelope_unmarshalsResponse(t *testing.T) {
	// Wire shape emitted today by the auth server's standard envelope helper.
	raw := `{
		"api_id": "abc-123",
		"data": {
			"plivo_auth_id":   "MA_TEST_FIXTURE",
			"plivo_auth_token": "tok-xyz",
			"aom_uuid":         "aom-uuid-1",
			"region":           "us-east-1"
		}
	}`

	var got cliTokenEnvelope
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if got.Data.PlivoAuthID != "MA_TEST_FIXTURE" {
		t.Errorf("PlivoAuthID = %q", got.Data.PlivoAuthID)
	}
	if got.Data.PlivoAuthToken != "tok-xyz" {
		t.Errorf("PlivoAuthToken = %q", got.Data.PlivoAuthToken)
	}
	if got.Data.AomUUID != "aom-uuid-1" {
		t.Errorf("AomUUID = %q", got.Data.AomUUID)
	}
	if got.Data.Region != "us-east-1" {
		t.Errorf("Region = %q", got.Data.Region)
	}
}

// TestCLITokenEnvelope_emptyDataYieldsBlankBundle guards the "data wrapper
// is missing or empty" failure mode — runLoginBrowser must NOT silently
// store a blank profile. The empty-bundle guard in runLoginBrowser handles
// this; this test just locks the unmarshal half of the contract.
func TestCLITokenEnvelope_emptyDataYieldsBlankBundle(t *testing.T) {
	var got cliTokenEnvelope
	if err := json.Unmarshal([]byte(`{"api_id":"x","data":{}}`), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Data.PlivoAuthID != "" || got.Data.PlivoAuthToken != "" {
		t.Errorf("expected blank fields on empty data block, got: %+v", got)
	}
}

// TestPkcePair_satisfiesS256_relation: the verifier returned must hash
// (via SHA256 → base64url no-pad) to exactly the challenge. This is what
// the auth server's PKCE check on /v1/cli/token enforces — if this test
// fails, no CLI invocation can ever succeed.
func TestPkcePair_satisfiesS256_relation(t *testing.T) {
	for i := 0; i < 20; i++ {
		verifier, challenge, err := pkcePair()
		if err != nil {
			t.Fatalf("pkcePair: %v", err)
		}
		if len(verifier) != 43 {
			t.Errorf("verifier len = %d, want 43 (32 bytes base64url no-pad)", len(verifier))
		}
		h := sha256.Sum256([]byte(verifier))
		got := base64.RawURLEncoding.EncodeToString(h[:])
		if got != challenge {
			t.Errorf("challenge mismatch: SHA256(verifier)=%s but challenge=%s", got, challenge)
		}
	}
}

func TestRandomURLToken_uniqueAndLength(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		tok, err := randomURLToken(32)
		if err != nil {
			t.Fatalf("randomURLToken: %v", err)
		}
		if len(tok) != 43 {
			t.Errorf("len = %d, want 43", len(tok))
		}
		if seen[tok] {
			t.Errorf("duplicate token: %s", tok)
		}
		seen[tok] = true
	}
}

func TestBuildAuthorizeURL_hasRequiredParams(t *testing.T) {
	got := buildAuthorizeURL(
		"https://api.example.com/",
		"http://127.0.0.1:54321/",
		"my-state",
		"my-challenge",
		"Mac MacBook-Pro",
	)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Host != "api.example.com" {
		t.Errorf("host = %q", u.Host)
	}
	if u.Path != "/v1/accounts/cli/authorize" {
		t.Errorf("path = %q", u.Path)
	}
	q := u.Query()
	for _, k := range []string{"cb", "state", "code_challenge", "code_challenge_method", "device"} {
		if q.Get(k) == "" {
			t.Errorf("missing query param: %s", k)
		}
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	if got := q.Get("device"); got != "Mac MacBook-Pro" {
		t.Errorf("device = %q, want %q", got, "Mac MacBook-Pro")
	}
	// cb should be the exact loopback URL passed in.
	if q.Get("cb") != "http://127.0.0.1:54321/" {
		t.Errorf("cb = %q", q.Get("cb"))
	}
}

// Empty device hint must not surface as `?device=` in the URL — the
// server's fallback copy ("your machine") relies on the param being
// absent, not empty-but-present.
func TestBuildAuthorizeURL_omitsEmptyDevice(t *testing.T) {
	got := buildAuthorizeURL(
		"https://api.example.com/",
		"http://127.0.0.1:54321/",
		"my-state",
		"my-challenge",
		"",
	)
	if strings.Contains(got, "device=") {
		t.Errorf("URL must omit device when empty, got %q", got)
	}
}

// TestAwaitLoopbackCallback_happyPath drives a real ephemeral listener +
// fires a fake browser GET at it. The callback should return the code
// when state matches.
func TestAwaitLoopbackCallback_happyPath(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan struct {
		code string
		err  error
	}, 1)
	go func() {
		c, e := awaitLoopbackCallback(ctx, listener, "expected-state-value")
		done <- struct {
			code string
			err  error
		}{c, e}
	}()

	// Give the listener a beat to be accept-ready.
	time.Sleep(50 * time.Millisecond)
	resp, err := http.Get("http://127.0.0.1:" + itoa(port) + "/?state=expected-state-value&code=test-code-123")
	if err != nil {
		t.Fatalf("client get: %v", err)
	}
	_ = resp.Body.Close()

	r := <-done
	if r.err != nil {
		t.Fatalf("callback returned err: %v", r.err)
	}
	if r.code != "test-code-123" {
		t.Errorf("code = %q, want test-code-123", r.code)
	}
}

func TestAwaitLoopbackCallback_rejectsStateMismatch(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, e := awaitLoopbackCallback(ctx, listener, "expected-state")
		done <- e
	}()

	time.Sleep(50 * time.Millisecond)
	resp, _ := http.Get("http://127.0.0.1:" + itoa(port) + "/?state=wrong-state&code=x")
	if resp != nil {
		_ = resp.Body.Close()
	}

	err = <-done
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Errorf("want state-mismatch err, got: %v", err)
	}
}

func TestAwaitLoopbackCallback_timesOutAfterContextDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = awaitLoopbackCallback(ctx, listener, "state")
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("want timeout err, got: %v", err)
	}
	if elapsed > 1*time.Second {
		t.Errorf("returned slow (%s) — context deadline didn't propagate", elapsed)
	}
}

// itoa is a small helper to avoid the import bloat of strconv just for
// one int → string conversion in the tests.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [10]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

// ─── End-to-end coverage for redeemAndPersist ────────────────────────────────
//
// These tests exercise the second half of `plivo login --browser`: POST to
// the auth server's /v1/accounts/cli/token, validate the envelope, and
// persist the bundle to ~/.plivo/config.toml + the OS keychain. They use a
// real httptest.Server as the auth-server mock and an in-memory keyring +
// temp HOME so the developer's real config / Keychain never gets touched.
//
// The browser + loopback half is already covered by
// TestAwaitLoopbackCallback_* above; CSRF state-mismatch lives there.

// tokenServerMock returns an httptest.Server that mimics the auth server's
// /v1/accounts/cli/token endpoint. It records each POST body (so a test can
// assert on the PKCE verifier round-trip) and replies with status / body
// supplied by the caller.
type tokenServerMock struct {
	srv      *httptest.Server
	mu       sync.Mutex
	hits     []map[string]string // each entry is the decoded JSON body of one POST
	respCode int
	respBody string
}

func newTokenServerMock(t *testing.T, respCode int, respBody string) *tokenServerMock {
	t.Helper()
	m := &tokenServerMock{respCode: respCode, respBody: respBody}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/accounts/cli/token" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		var got map[string]string
		_ = json.Unmarshal(raw, &got)
		m.mu.Lock()
		m.hits = append(m.hits, got)
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(m.respCode)
		_, _ = w.Write([]byte(m.respBody))
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *tokenServerMock) snapshot() []map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]map[string]string, len(m.hits))
	copy(out, m.hits)
	return out
}

// setupBrowserLoginTestEnv wires up the bits redeemAndPersist needs:
//   - in-memory keychain so SetToken doesn't pop a real macOS Keychain prompt
//   - temp HOME so config.Save writes to t.TempDir() instead of ~/.plivo
//   - an api.Client pointed at the supplied mock auth server
//
// Returns the client + a deterministic profile name to use in assertions.
func setupBrowserLoginTestEnv(t *testing.T, mockURL string) (*api.Client, string) {
	t.Helper()
	keyring.MockInit()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	// Make sure no stray env-var-creds short-circuit Resolve elsewhere.
	t.Setenv("PLIVO_AUTH_ID", "")
	t.Setenv("PLIVO_AUTH_TOKEN", "")

	client := api.New("", "", 5*time.Second)
	client.BuddyBaseURL = mockURL
	return client, "browser-login-test"
}

// TestRedeemAndPersist_happyPath drives redeemAndPersist against a mock
// auth server that returns the success envelope, then asserts the bundle
// landed in config + keychain AND that the POST body carried the same
// code_verifier the test handed in (the PKCE round-trip the auth server
// enforces).
func TestRedeemAndPersist_happyPath(t *testing.T) {
	const (
		state    = "state-abc"
		code     = "code-xyz"
		verifier = "verifier-deadbeef"
		authID   = "MA_FAKE_ID"
		token    = "fake_token_for_testing"
		aomUUID  = "aom-uuid-7"
		region   = "us-east-1"
	)

	mock := newTokenServerMock(t, http.StatusOK, `{
		"api_id": "req-id-1",
		"data": {
			"plivo_auth_id":   "`+authID+`",
			"plivo_auth_token": "`+token+`",
			"aom_uuid":         "`+aomUUID+`",
			"region":           "`+region+`"
		},
		"errors": null,
		"message": ""
	}`)
	client, profileName := setupBrowserLoginTestEnv(t, mock.srv.URL)

	if err := redeemAndPersist(client, state, code, verifier, profileName, ""); err != nil {
		t.Fatalf("redeemAndPersist: %v", err)
	}

	// 1. The request hit the auth server exactly once with the right
	//    (state, code, code_verifier) triple. The PKCE verifier round-trip
	//    is the security invariant the auth server checks before issuing
	//    the bundle.
	hits := mock.snapshot()
	if len(hits) != 1 {
		t.Fatalf("auth-server hits = %d, want 1", len(hits))
	}
	if hits[0]["state"] != state {
		t.Errorf("state on wire = %q, want %q", hits[0]["state"], state)
	}
	if hits[0]["code"] != code {
		t.Errorf("code on wire = %q, want %q", hits[0]["code"], code)
	}
	if hits[0]["code_verifier"] != verifier {
		t.Errorf("code_verifier on wire = %q, want %q (PKCE verifier never leaves the CLI between authorize + token phases)", hits[0]["code_verifier"], verifier)
	}

	// 2. The profile landed in config.toml with the right auth_id + region
	//    and became the active profile (first-profile-wins).
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	prof, ok := cfg.Profiles[profileName]
	if !ok {
		t.Fatalf("profile %q not saved to config", profileName)
	}
	if prof.AuthID != authID {
		t.Errorf("Profile.AuthID = %q, want %q", prof.AuthID, authID)
	}
	if prof.Region != region {
		t.Errorf("Profile.Region = %q, want %q", prof.Region, region)
	}
	if prof.Env != "" {
		t.Errorf("Profile.Env = %q, want \"\" for prod default", prof.Env)
	}
	if cfg.Active != profileName {
		t.Errorf("cfg.Active = %q, want %q (first profile wins)", cfg.Active, profileName)
	}

	// 3. The auth_token went into the OS keychain (the mock one), NOT
	//    inline in config.toml. This is the security promise — losing it
	//    would silently regress to plaintext-token storage.
	if prof.AuthToken != "" {
		t.Errorf("Profile.AuthToken = %q on disk, want empty (token should be in keychain only)", prof.AuthToken)
	}
	got, err := config.GetToken(profileName)
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if got != token {
		t.Errorf("GetToken = %q, want %q", got, token)
	}
}

// TestRedeemAndPersist_brokenEnvelopeShape_returnsEmptyBundle is a regression
// test for the auth-server envelope-shape bug: the success payload landed
// under `errors` instead of `data`. The CLI must NOT silently persist a
// blank profile in that case — it must surface "empty bundle". If a future
// auth-server refactor moves the bundle key around again, this test fails
// first.
func TestRedeemAndPersist_brokenEnvelopeShape_returnsEmptyBundle(t *testing.T) {
	// Buggy shape: success fields under `errors`, data is null.
	mock := newTokenServerMock(t, http.StatusOK, `{
		"api_id": "req-id-2",
		"data": null,
		"errors": {
			"plivo_auth_id":   "MA_FAKE_ID",
			"plivo_auth_token": "fake_token_for_testing",
			"aom_uuid":         "aom-uuid-7",
			"region":           "us-east-1"
		},
		"message": ""
	}`)
	client, profileName := setupBrowserLoginTestEnv(t, mock.srv.URL)

	err := redeemAndPersist(client, "s", "c", "v", profileName, "")
	if err == nil {
		t.Fatal("want error on broken envelope shape, got nil (would have silently saved a blank profile)")
	}
	if !strings.Contains(err.Error(), "empty bundle") {
		t.Errorf("error message = %q, want it to contain 'empty bundle'", err.Error())
	}

	// And critically: nothing was persisted. A regression here means we
	// silently wrote a profile with empty auth_id which would then fail
	// every subsequent command with a confusing "auth missing".
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if _, ok := cfg.Profiles[profileName]; ok {
		t.Error("profile saved despite empty bundle — should have aborted before config.Save")
	}
	if tok, _ := config.GetToken(profileName); tok != "" {
		t.Errorf("token in keychain = %q, want empty (nothing should have been stored)", tok)
	}
}

// TestRedeemAndPersist_4xxWithGlobalError surfaces the upstream
// `errors.global_error` message cleanly via the api.Client → clierr.Error
// path, instead of dropping it on the floor or printing a raw HTTP-400.
// This is the typical "state expired" / "code already redeemed" failure
// shape from the auth server's CLI-auth controller.
func TestRedeemAndPersist_4xxWithGlobalError(t *testing.T) {
	const upstreamMsg = "state not found or expired"
	mock := newTokenServerMock(t, http.StatusBadRequest, `{
		"api_id": "req-id-3",
		"data": null,
		"errors": {"global_error": "`+upstreamMsg+`"},
		"message": ""
	}`)
	client, profileName := setupBrowserLoginTestEnv(t, mock.srv.URL)

	err := redeemAndPersist(client, "s", "c", "v", profileName, "")
	if err == nil {
		t.Fatal("want error from auth-server 4xx, got nil")
	}

	// The clierr.Error should carry the upstream message verbatim — that's
	// the actionable bit the user sees ("state expired → retry login").
	if !strings.Contains(err.Error(), upstreamMsg) {
		t.Errorf("err = %q, want it to contain upstream message %q", err.Error(), upstreamMsg)
	}
	// And it should be the structured clierr.Error, not a plain Go error —
	// downstream renderers branch on Code / StatusCode.
	var ce *clierr.Error
	if !errorsAs(err, &ce) {
		t.Fatalf("err type = %T, want *clierr.Error", err)
	}
	if ce.StatusCode != http.StatusBadRequest {
		t.Errorf("clierr.StatusCode = %d, want 400", ce.StatusCode)
	}

	// 4xx → no persistence.
	cfg, _ := config.Load()
	if _, ok := cfg.Profiles[profileName]; ok {
		t.Error("profile saved despite auth-server 4xx")
	}
}

// errorsAs is a tiny shim to keep the test file's import surface small
// (errors.As would otherwise require importing "errors", which collides
// optically with internal/clierr.Error). Behaviour matches stdlib errors.As
// for the two-deep wrap chain we care about here.
func errorsAs(err error, target **clierr.Error) bool {
	for err != nil {
		if e, ok := err.(*clierr.Error); ok {
			*target = e
			return true
		}
		type wrapper interface{ Unwrap() error }
		w, ok := err.(wrapper)
		if !ok {
			return false
		}
		err = w.Unwrap()
	}
	return false
}
