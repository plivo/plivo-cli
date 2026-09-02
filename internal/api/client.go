// Package api provides the Plivo REST API client and response types.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/cliupgrade"
	"github.com/plivo/plivo-cli/internal/version"
)

// CLICommand is process-global; cmd/root.go sets it at command-start so
// every outbound HTTP request carries the X-Plivo-CLI-Command header.
// Used to attribute traffic per cobra command for usage analytics.
var CLICommand string

// addCLIHeaders injects the version + command + client-meta headers on
// every request. Called once per request from Do / DoMultipart / StreamSSE.
// OS/arch are forwarded so the server has the user's actual platform
// (not whatever the analytics pipeline auto-detects). Email (when present
// on the profile) is forwarded so per-user attribution works inside an
// org — auth_id alone is org-level.
//
// Email/Auth-ID/Region/AOM-UUID are gated on TelemetryEnabled; Version/OS/
// Arch/Command always go out (the server needs Version for the upgrade nudge).
func (c *Client) addCLIHeaders(req *http.Request) {
	req.Header.Set(headerCLIVersion, version.Value)
	req.Header.Set(headerCLIOS, runtime.GOOS)
	req.Header.Set(headerCLIArch, runtime.GOARCH)
	if CLICommand != "" {
		req.Header.Set(headerCLICommand, CLICommand)
	}
	if c == nil || !c.TelemetryEnabled {
		return
	}
	if c.Email != "" {
		req.Header.Set(headerCLIEmail, c.Email)
	}
	if c.AuthID != "" {
		req.Header.Set(headerCLIAuthID, c.AuthID)
	}
	if c.Region != "" {
		req.Header.Set(headerCLIRegion, c.Region)
	}
	if c.AomUUID != "" {
		req.Header.Set(headerCLIAomUUID, c.AomUUID)
	}
}

// checkUpgradeWarn inspects response headers for the server-driven
// upgrade nudge. The print itself fires after rootCmd.Execute() returns.
func checkUpgradeWarn(resp *http.Response) {
	if resp == nil {
		return
	}
	if resp.Header.Get(headerUpgradeRequired) == "true" {
		cliupgrade.SignalUpgradeRequired(resp.Header.Get(headerMinVersion))
	}
}

// URL conventions:
//
//   - api.plivo.com / lookup.plivo.com — Plivo's customer REST APIs.
//   - The Plivo AI-assistant endpoint backing `plivo ask` / `plivo support`
//     lives at a separate base URL; resolved at runtime via env / config
//     (see applyBuddyURL in cmd/buddy.go), with a built-in default below.
const (
	// DefaultBaseURL is the gateway for CLI REST traffic.
	DefaultBaseURL = "https://hodor.plivo.com/v1/cli/api"
	// DefaultBuddyBase is the prod endpoint serving /v1/aiassist/buddy-ext
	// (Plivo's customer AI assistant). Plivo Basic auth.
	DefaultBuddyBase = "https://hodor.plivo.com"

	// Headers sent on every CLI request. Auth-ID + Region + AOM-UUID are
	// redundant for the authenticated chokepoint (which derives auth_id
	// from Basic auth) but are how the public feedback route gets the
	// same identity for unified PostHog Person stitching.
	headerCLIVersion = "X-Plivo-CLI-Version"
	headerCLICommand = "X-Plivo-CLI-Command"
	headerCLIOS      = "X-Plivo-CLI-OS"
	headerCLIArch    = "X-Plivo-CLI-Arch"
	headerCLIEmail   = "X-Plivo-CLI-Email"
	headerCLIAuthID  = "X-Plivo-CLI-Auth-ID"
	headerCLIRegion  = "X-Plivo-CLI-Region"
	headerCLIAomUUID = "X-Plivo-CLI-AOM-UUID"
	// Headers the server may return — version gate signals.
	headerUpgradeRequired = "X-Plivo-CLI-Upgrade-Required"
	headerMinVersion      = "X-Plivo-CLI-Min-Version"
)

type Client struct {
	BaseURL      string
	AdminBaseURL string // optional admin-override base URL
	BuddyBaseURL string // endpoint for /v1/aiassist/buddy-ext (Plivo Basic auth) — see applyBuddyURL
	AuthID       string
	AuthToken    string
	Email        string // optional human email from the profile; sent as X-Plivo-CLI-Email for per-user analytics
	Region       string // optional resolved region from the profile; sent as X-Plivo-CLI-Region so unauthenticated analytics routes (feedback) can tag events
	AomUUID      string // optional per-user identity row UUID from the profile; sent as X-Plivo-CLI-AOM-UUID for per-human analytics

	// TelemetryEnabled gates the Email/AuthID/Region/AomUUID headers.
	// Defaults true via New(); cmd/root.go overrides it from the user's
	// config/env telemetry setting.
	TelemetryEnabled bool

	HTTP       *http.Client
	DryRun     bool
	LogRequest func(method, url string, body []byte)
}

// IsScopedToken reports whether AuthToken is a scoped token (stk_ prefix).
// Scoped tokens are sent as Bearer; regular tokens use HTTP Basic.
func (c *Client) IsScopedToken() bool {
	return strings.HasPrefix(c.AuthToken, "stk_")
}

func New(authID, authToken string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		BaseURL:          DefaultBaseURL,
		BuddyBaseURL:     DefaultBuddyBase,
		AuthID:           authID,
		AuthToken:        authToken,
		TelemetryEnabled: true,
		HTTP:             &http.Client{Timeout: timeout},
	}
}

// AccountURL returns BaseURL + "/v1/Account/{auth_id}/" + joined parts + "/".
func (c *Client) AccountURL(parts ...string) string {
	u := c.BaseURL + "/v1/Account/" + c.AuthID + "/"
	if len(parts) == 0 {
		return u
	}
	return u + strings.Join(parts, "/") + "/"
}

// LookupURL returns the Lookup API URL for a number.
// No trailing slash; the caller appends ?type=carrier.
func (c *Client) LookupURL(number string) string {
	return c.BaseURL + "/v1/Lookup/Number/" + number
}

// BuddyURL joins BuddyBaseURL with the given absolute path. AI surfaces
// (/v1/aiassist/...) route under /v1/cli/cx so they share the same auth +
// version-gate + analytics middleware as /v1/cli/api. Login surfaces
// (/v1/accounts/cli/...) stay at the root — those routes are intentionally
// unauthenticated (PKCE bootstrap).
func (c *Client) BuddyURL(path string) string {
	base := strings.TrimRight(c.BuddyBaseURL, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.HasPrefix(path, "/v1/aiassist/") {
		return base + "/v1/cli/cx" + path
	}
	return base + path
}

// Do executes an HTTP request with basic auth and JSON encoding.
//
// If body is non-nil it is marshalled to JSON.
// queryParams (if non-nil and non-empty) are appended to the URL.
// On success (2xx), response is unmarshalled into out (if non-nil).
// On API error (>=400), returns an *APIError with no Go error.
// On transport errors, returns a Go error with nil *APIError.
func (c *Client) Do(method, fullURL string, body any, queryParams url.Values, out any) (*APIError, error) {
	if len(queryParams) > 0 {
		sep := "?"
		if strings.Contains(fullURL, "?") {
			sep = "&"
		}
		fullURL = fullURL + sep + queryParams.Encode()
	}

	var bodyReader io.Reader
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyBytes = b
		bodyReader = bytes.NewReader(b)
	}

	if c.DryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] %s %s\n", method, fullURL)
		if len(bodyBytes) > 0 {
			var pretty bytes.Buffer
			if json.Indent(&pretty, bodyBytes, "  ", "  ") == nil {
				fmt.Fprintf(os.Stderr, "  body:\n  %s\n", pretty.String())
			} else {
				fmt.Fprintf(os.Stderr, "  body: %s\n", string(bodyBytes))
			}
		}
		return nil, nil
	}
	if c.LogRequest != nil {
		c.LogRequest(method, fullURL, bodyBytes)
	}

	req, err := http.NewRequest(method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	// Skip auth-header injection when both creds are empty: lets callers
	// hit unauthenticated endpoints (e.g. `plivo login --browser`'s
	// /v1/accounts/cli/token, which uses PKCE state+code+verifier as
	// the credential triple) without sending a stray `Authorization:
	// Basic Og==`.
	switch {
	case c.IsScopedToken():
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	case c.AuthID == "" && c.AuthToken == "":
		// no-op — unauthenticated request
	default:
		req.SetBasicAuth(c.AuthID, c.AuthToken)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())
	c.addCLIHeaders(req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	checkUpgradeWarn(resp)

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return parseError(resp.StatusCode, resp.Header.Get("X-Request-ID"), respBytes), nil
	}

	if resp.StatusCode == 204 || len(respBytes) == 0 {
		return nil, nil
	}

	if out != nil {
		if err := json.Unmarshal(respBytes, out); err != nil {
			return nil, fmt.Errorf("decode response: %w (body: %s)", err, string(respBytes))
		}
		// Keep the bytes so -o json can echo them losslessly.
		if rc, ok := out.(RawCapturer); ok {
			rc.SetRaw(respBytes)
		}
	}
	return nil, nil
}

// DoMultipart sends a multipart/form-data request: a "data" form field set to
// dataJSON, plus one file part per files entry (form field name -> file path,
// e.g. "documents[0].file" -> "/path/passport.pdf"). Auth, headers, dry-run,
// and error handling mirror Do; used by the compliance create/update verbs.
func (c *Client) DoMultipart(method, fullURL string, dataJSON []byte, files map[string]string, out any) (*APIError, error) {
	if c.DryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] %s %s (multipart/form-data)\n", method, fullURL)
		if len(dataJSON) > 0 {
			var pretty bytes.Buffer
			if json.Indent(&pretty, dataJSON, "  ", "  ") == nil {
				fmt.Fprintf(os.Stderr, "  data:\n  %s\n", pretty.String())
			} else {
				fmt.Fprintf(os.Stderr, "  data: %s\n", string(dataJSON))
			}
		}
		for field, path := range files {
			fmt.Fprintf(os.Stderr, "  file: %s=%s\n", field, path)
		}
		return nil, nil
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if len(dataJSON) > 0 {
		if err := w.WriteField("data", string(dataJSON)); err != nil {
			return nil, fmt.Errorf("write data field: %w", err)
		}
	}
	for field, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		part, err := w.CreateFormFile(field, filepath.Base(path))
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("create form file %s: %w", field, err)
		}
		if _, err := io.Copy(part, f); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("copy %s: %w", path, err)
		}
		_ = f.Close()
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	if c.LogRequest != nil {
		c.LogRequest(method, fullURL, nil)
	}

	req, err := http.NewRequest(method, fullURL, &buf)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if c.IsScopedToken() {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	} else {
		req.SetBasicAuth(c.AuthID, c.AuthToken)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())
	c.addCLIHeaders(req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	checkUpgradeWarn(resp)

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return parseError(resp.StatusCode, resp.Header.Get("X-Request-ID"), respBytes), nil
	}
	if resp.StatusCode == 204 || len(respBytes) == 0 {
		return nil, nil
	}
	if out != nil {
		if err := json.Unmarshal(respBytes, out); err != nil {
			return nil, fmt.Errorf("decode response: %w (body: %s)", err, string(respBytes))
		}
		// Keep the bytes so -o json can echo them losslessly.
		if rc, ok := out.(RawCapturer); ok {
			rc.SetRaw(respBytes)
		}
	}
	return nil, nil
}
