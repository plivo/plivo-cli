package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/cliupgrade"
	"github.com/plivo/plivo-cli/internal/version"
)

// These tests stand up an httptest server pretending to be the CLI
// gateway (/v1/cli/api/*) and exercise the real api.Client end-to-end.
// They lock the request/response contract:
//
//   - Every CLI request lands at /v1/cli/api/*
//   - AccountURL composes to /v1/cli/api/v1/Account/<id>/...
//   - LookupURL composes to /v1/cli/api/v1/Lookup/Number/...
//   - Every request carries X-Plivo-CLI-Version
//   - X-Plivo-CLI-Command is set when api.CLICommand is set
//   - HTTP Basic auth uses auth_id:auth_token
//   - HTTP 426 maps to clierr.CodeCLITooOld
//   - X-Plivo-CLI-Upgrade-Required response header signals the nudge

type capturedRequest struct {
	method        string
	path          string
	authorization string
	cliVersion    string
	cliCommand    string
	cliOS         string
	cliArch       string
	cliEmail      string
	cliAuthID     string
	cliRegion     string
	cliAomUUID    string
	userAgent     string
	body          []byte
}

func newFakeServer(t *testing.T, status int, respBody string, respHeaders map[string]string) (*httptest.Server, *capturedRequest) {
	captured := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.authorization = r.Header.Get("Authorization")
		captured.cliVersion = r.Header.Get("X-Plivo-CLI-Version")
		captured.cliCommand = r.Header.Get("X-Plivo-CLI-Command")
		captured.cliOS = r.Header.Get("X-Plivo-CLI-OS")
		captured.cliArch = r.Header.Get("X-Plivo-CLI-Arch")
		captured.cliEmail = r.Header.Get("X-Plivo-CLI-Email")
		captured.cliAuthID = r.Header.Get("X-Plivo-CLI-Auth-ID")
		captured.cliRegion = r.Header.Get("X-Plivo-CLI-Region")
		captured.cliAomUUID = r.Header.Get("X-Plivo-CLI-AOM-UUID")
		captured.userAgent = r.Header.Get("User-Agent")
		b, _ := io.ReadAll(r.Body)
		captured.body = b
		for k, v := range respHeaders {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

// newTestClient wires a Client to the fake server with the same
// /v1/cli/api prefix the real DefaultBaseURL carries. That way AccountURL
// composition matches production shape exactly.
func newTestClient(srvURL string) *Client {
	c := New("MAXYZ123", "secret-tok", 5*time.Second)
	c.BaseURL = srvURL + "/v1/cli/api"
	return c
}

// TestE2E_AccountURLRouting — a typical voice-list call lands at
// /v1/cli/api/v1/Account/<id>/Call/ on the fake server.
func TestE2E_AccountURLRouting(t *testing.T) {
	srv, captured := newFakeServer(t, 200, `{"api_id":"x","objects":[]}`, nil)
	c := newTestClient(srv.URL)
	url := c.AccountURL("Call")

	if !strings.HasSuffix(url, "/v1/cli/api/v1/Account/MAXYZ123/Call/") {
		t.Errorf("AccountURL composes wrong: %q", url)
	}
	if _, err := c.Do("GET", url, nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if captured.path != "/v1/cli/api/v1/Account/MAXYZ123/Call/" {
		t.Errorf("path mismatch: %q", captured.path)
	}
	if captured.method != "GET" {
		t.Errorf("method mismatch: %q", captured.method)
	}
}

// TestE2E_LookupURLRouting — a lookup call lands at
// /v1/cli/api/v1/Lookup/Number/<n>.
func TestE2E_LookupURLRouting(t *testing.T) {
	srv, captured := newFakeServer(t, 200, `{"phone_number":"+15551234"}`, nil)
	c := newTestClient(srv.URL)
	url := c.LookupURL("+15551234")

	want := srv.URL + "/v1/cli/api/v1/Lookup/Number/+15551234"
	if url != want {
		t.Errorf("LookupURL composes wrong: %q, want %q", url, want)
	}
	if _, err := c.Do("GET", url, nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if captured.path != "/v1/cli/api/v1/Lookup/Number/+15551234" {
		t.Errorf("path mismatch: %q", captured.path)
	}
}

// TestE2E_CLIVersionHeader — every request sends X-Plivo-CLI-Version with
// the binary's compiled version string.
func TestE2E_CLIVersionHeader(t *testing.T) {
	srv, captured := newFakeServer(t, 200, `{}`, nil)
	c := newTestClient(srv.URL)
	if _, err := c.Do("GET", c.AccountURL("Number"), nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if captured.cliVersion == "" {
		t.Error("X-Plivo-CLI-Version header was empty")
	}
	if captured.cliVersion != version.Value {
		t.Errorf("X-Plivo-CLI-Version = %q, want %q", captured.cliVersion, version.Value)
	}
}

// TestE2E_CLICommandHeader — when api.CLICommand is set, that value lands
// on the X-Plivo-CLI-Command request header.
func TestE2E_CLICommandHeader(t *testing.T) {
	srv, captured := newFakeServer(t, 200, `{}`, nil)
	c := newTestClient(srv.URL)

	old := CLICommand
	t.Cleanup(func() { CLICommand = old })
	CLICommand = "voice.calls.list"

	if _, err := c.Do("GET", c.AccountURL("Call"), nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if captured.cliCommand != "voice.calls.list" {
		t.Errorf("X-Plivo-CLI-Command = %q, want voice.calls.list", captured.cliCommand)
	}
}

// TestE2E_CLICommandHeaderAbsentWhenUnset — when api.CLICommand is empty,
// the header is NOT injected.
func TestE2E_CLICommandHeaderAbsentWhenUnset(t *testing.T) {
	srv, captured := newFakeServer(t, 200, `{}`, nil)
	c := newTestClient(srv.URL)

	old := CLICommand
	t.Cleanup(func() { CLICommand = old })
	CLICommand = ""

	if _, err := c.Do("GET", c.AccountURL("Call"), nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if captured.cliCommand != "" {
		t.Errorf("X-Plivo-CLI-Command should be empty, got %q", captured.cliCommand)
	}
}

// TestE2E_BasicAuthHeader — Plivo Basic auth (auth_id:auth_token) flows
// through to the server untouched.
func TestE2E_BasicAuthHeader(t *testing.T) {
	srv, captured := newFakeServer(t, 200, `{}`, nil)
	c := newTestClient(srv.URL)

	if _, err := c.Do("GET", c.AccountURL("Number"), nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !strings.HasPrefix(captured.authorization, "Basic ") {
		t.Errorf("Authorization not Basic: %q", captured.authorization)
	}
}

// TestE2E_426MapsToCLITooOld — the server's version-gate block
// (HTTP 426) maps to a clierr.Error with CodeCLITooOld and exit code 6.
func TestE2E_426MapsToCLITooOld(t *testing.T) {
	body := `{"error":"CLI version too old","code":"CLI_TOO_OLD","current_version":"0.0.5","min_version":"0.2.0","upgrade_command":"plivo upgrade"}`
	srv, _ := newFakeServer(t, 426, body, nil)
	c := newTestClient(srv.URL)

	apiErr, err := c.Do("GET", c.AccountURL("Number"), nil, nil, nil)
	if err != nil {
		t.Fatalf("transport err on 426: %v", err)
	}
	if apiErr == nil {
		t.Fatal("expected APIError for 426, got nil")
	}
	if apiErr.Code != "CLI_TOO_OLD" {
		t.Errorf("Code = %q, want CLI_TOO_OLD", apiErr.Code)
	}
	if !strings.Contains(apiErr.Hint, "plivo upgrade") {
		t.Errorf("hint should mention upgrade command, got %q", apiErr.Hint)
	}
	if !strings.Contains(apiErr.Hint, "0.2.0") {
		t.Errorf("hint should mention min version, got %q", apiErr.Hint)
	}
	if got := apiErr.ExitCode(); got != 6 {
		t.Errorf("ExitCode = %d, want 6", got)
	}
}

// TestE2E_UpgradeWarnHeaderFlipsNudge — when the server returns a
// 200 with X-Plivo-CLI-Upgrade-Required: true, the in-process nudge
// flag flips so rootCmd.Execute() can render the stderr line.
func TestE2E_UpgradeWarnHeaderFlipsNudge(t *testing.T) {
	cliupgrade.Reset()
	t.Cleanup(cliupgrade.Reset)

	srv, _ := newFakeServer(t, 200, `{}`, map[string]string{
		"X-Plivo-CLI-Upgrade-Required": "true",
		"X-Plivo-CLI-Min-Version":      "0.3.0",
	})
	c := newTestClient(srv.URL)
	if _, err := c.Do("GET", c.AccountURL("Number"), nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	pending, minVer := cliupgrade.Pending()
	if !pending {
		t.Error("expected nudge flag to flip on warn header; it didn't")
	}
	if minVer != "0.3.0" {
		t.Errorf("min version captured = %q, want 0.3.0", minVer)
	}
}

// TestE2E_NoUpgradeWarnWhenHeaderAbsent — clean 200 response leaves the
// nudge flag alone.
func TestE2E_NoUpgradeWarnWhenHeaderAbsent(t *testing.T) {
	cliupgrade.Reset()
	t.Cleanup(cliupgrade.Reset)

	srv, _ := newFakeServer(t, 200, `{}`, nil)
	c := newTestClient(srv.URL)
	if _, err := c.Do("GET", c.AccountURL("Number"), nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	pending, _ := cliupgrade.Pending()
	if pending {
		t.Error("nudge should NOT flip when warn header absent")
	}
}

// TestE2E_POSTBodyPassesThrough — POST with JSON body lands at the server
// with body intact (multipart-free path).
func TestE2E_POSTBodyPassesThrough(t *testing.T) {
	srv, captured := newFakeServer(t, 201, `{}`, nil)
	c := newTestClient(srv.URL)
	body := map[string]string{"src": "+15551111", "dst": "+15552222", "text": "hi"}

	if _, err := c.Do("POST", c.AccountURL("Message"), body, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(captured.body, &got); err != nil {
		t.Fatalf("captured body not valid JSON: %v (raw: %s)", err, string(captured.body))
	}
	if got["text"] != "hi" {
		t.Errorf("body lost: %v", got)
	}
}

// TestE2E_UserAgentPreserved — the User-Agent stays Plivo-CLI/<version>.
func TestE2E_UserAgentPreserved(t *testing.T) {
	srv, captured := newFakeServer(t, 200, `{}`, nil)
	c := newTestClient(srv.URL)

	if _, err := c.Do("GET", c.AccountURL("Number"), nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !strings.HasPrefix(captured.userAgent, "Plivo-CLI/") {
		t.Errorf("User-Agent = %q, want Plivo-CLI/ prefix", captured.userAgent)
	}
}

// TestE2E_ClientMetaHeaders — every request carries X-Plivo-CLI-OS and
// X-Plivo-CLI-Arch so the server has the user's actual platform (not
// whatever the analytics pipeline auto-detects from the request origin).
// Values mirror runtime.GOOS/GOARCH of the binary.
func TestE2E_ClientMetaHeaders(t *testing.T) {
	srv, captured := newFakeServer(t, 200, `{}`, nil)
	c := newTestClient(srv.URL)

	if _, err := c.Do("GET", c.AccountURL("Number"), nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	// Don't lock to a specific GOOS — test runs on Linux CI + dev Macs.
	// Lock the contract: non-empty + lowercase + in the known set.
	knownOS := map[string]bool{"darwin": true, "linux": true, "windows": true, "freebsd": true}
	if !knownOS[captured.cliOS] {
		t.Errorf("X-Plivo-CLI-OS = %q, want one of %v", captured.cliOS, knownOS)
	}
	knownArch := map[string]bool{"amd64": true, "arm64": true, "386": true, "arm": true}
	if !knownArch[captured.cliArch] {
		t.Errorf("X-Plivo-CLI-Arch = %q, want one of %v", captured.cliArch, knownArch)
	}
}

// TestE2E_EmailHeader_setWhenProfileHasEmail — X-Plivo-CLI-Email rides on
// the request whenever Client.Email is non-empty. Used as the per-user
// attribution tag in analytics (auth_id is org-level, multiple humans
// share it).
func TestE2E_EmailHeader_setWhenProfileHasEmail(t *testing.T) {
	srv, captured := newFakeServer(t, 200, `{}`, nil)
	c := newTestClient(srv.URL)
	c.Email = "user@example.com"

	if _, err := c.Do("GET", c.AccountURL("Number"), nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if captured.cliEmail != "user@example.com" {
		t.Errorf("X-Plivo-CLI-Email = %q, want user@example.com", captured.cliEmail)
	}
}

// Older profiles created before the Email field existed (and the manual
// flow when user skips the email prompt) leave Client.Email empty — header
// must NOT be sent. Absent-header means "unknown user"; sending an empty
// header would shadow that.
func TestE2E_EmailHeader_omittedWhenProfileHasNoEmail(t *testing.T) {
	srv, captured := newFakeServer(t, 200, `{}`, nil)
	c := newTestClient(srv.URL) // c.Email defaults to ""

	if _, err := c.Do("GET", c.AccountURL("Number"), nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if captured.cliEmail != "" {
		t.Errorf("X-Plivo-CLI-Email = %q, want empty (no email on profile)", captured.cliEmail)
	}
}

// TestE2E_IdentityHeaders — every authed request ships X-Plivo-CLI-Auth-ID
// + X-Plivo-CLI-Region + X-Plivo-CLI-AOM-UUID when those fields are
// populated on the Client. Lets unauthenticated routes (feedback) tag
// their PostHog events with the same identity dimensions the chokepoint
// captures from its auth-middleware context.
func TestE2E_IdentityHeaders(t *testing.T) {
	srv, captured := newFakeServer(t, 200, `{}`, nil)
	c := newTestClient(srv.URL)
	c.Region = "us-east-1"
	c.AomUUID = "aom-fixture-uuid"

	if _, err := c.Do("GET", c.AccountURL("Number"), nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if captured.cliAuthID == "" {
		t.Error("X-Plivo-CLI-Auth-ID should be set when Client.AuthID populated")
	}
	if captured.cliRegion != "us-east-1" {
		t.Errorf("X-Plivo-CLI-Region = %q, want us-east-1", captured.cliRegion)
	}
	if captured.cliAomUUID != "aom-fixture-uuid" {
		t.Errorf("X-Plivo-CLI-AOM-UUID = %q, want aom-fixture-uuid", captured.cliAomUUID)
	}
}

// Identity headers stay absent when the fields are empty — same "no
// silent shadowing" rule as the email header.
func TestE2E_IdentityHeaders_absentWhenEmpty(t *testing.T) {
	srv, captured := newFakeServer(t, 200, `{}`, nil)
	c := newTestClient(srv.URL)
	c.AuthID = "" // override the test default
	c.Region = ""
	c.AomUUID = ""

	if _, err := c.Do("GET", c.BaseURL+"/anywhere", nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if captured.cliAuthID != "" || captured.cliRegion != "" || captured.cliAomUUID != "" {
		t.Errorf("identity headers should be absent when Client fields empty; got auth=%q region=%q aom=%q",
			captured.cliAuthID, captured.cliRegion, captured.cliAomUUID)
	}
}
