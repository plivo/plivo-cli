package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestCLITokenEnvelope_unmarshalsHodorResponse locks in the wire shape we
// expect from hodor's /v1/accounts/cli/token: the standard {api_id, data}
// envelope. If hodor's response wrapper drifts (data → result, or no
// envelope at all), this fails loudly before the silent empty-bundle path
// in runLoginBrowser surfaces it as 'token redemption returned an empty bundle'.
func TestCLITokenEnvelope_unmarshalsHodorResponse(t *testing.T) {
	// Same wire shape hodor emits today (utils.BuildResponse(metaData, response, "", nil, nil)).
	raw := `{
		"api_id": "abc-123",
		"data": {
			"plivo_auth_id":   "MAFROMHODOR",
			"plivo_auth_token": "tok-xyz",
			"aom_uuid":         "aom-uuid-1",
			"region":           "us-east-1"
		}
	}`

	var got cliTokenEnvelope
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if got.Data.PlivoAuthID != "MAFROMHODOR" {
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
// hodor's PKCE check on /v1/cli/token enforces — if this test fails,
// no CLI invocation can ever succeed.
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
		"https://global-auth-api.contacto.com/",
		"http://127.0.0.1:54321/",
		"my-state",
		"my-challenge",
	)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Host != "global-auth-api.contacto.com" {
		t.Errorf("host = %q", u.Host)
	}
	if u.Path != "/v1/accounts/cli/authorize" {
		t.Errorf("path = %q", u.Path)
	}
	q := u.Query()
	for _, k := range []string{"cb", "state", "code_challenge", "code_challenge_method"} {
		if q.Get(k) == "" {
			t.Errorf("missing query param: %s", k)
		}
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	// cb should be the exact loopback URL passed in.
	if q.Get("cb") != "http://127.0.0.1:54321/" {
		t.Errorf("cb = %q", q.Get("cb"))
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
