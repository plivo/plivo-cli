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
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/version"
)

// URL convention (enforced by TestNoDirectCXServiceURLs):
//
//   - api.plivo.com / lookup.plivo.com → Plivo customer APIs, hit directly.
//   - *.contacto.com / *.contactodev.com / *.plivops.com hosts MUST be hodor
//     edges (global + regional). Every CX-internal service (aiassist,
//     contacto-core, dobby, pai-voice, …) is reached only via hodor — never
//     directly — so hodor can resolve region from creds, enforce the
//     internal-services IP allowlist, rate-limit per auth_id, and audit-log.
//     That's a security boundary.
//
// Adding a new hodor edge? Also add it to allowedCXHosts in
// url_boundary_test.go, or the build fails.
const (
	DefaultBaseURL    = "https://api.plivo.com/v1"
	DefaultLookupBase = "https://lookup.plivo.com/v1"
	// DefaultBuddyBase is the prod hodor edge that hosts /v1/aiassist/buddy-ext
	// (Plivo Buddy — customer-facing AI assistant). Plivo Basic auth.
	DefaultBuddyBase = "https://global-auth-api.contacto.com"
)

type Client struct {
	BaseURL      string
	HodorBaseURL string // optional; used for /v1/agent/ and /v1/auth/token/ routes
	BuddyBaseURL string // hodor edge for /v1/aiassist/buddy-ext (Plivo Basic auth)
	AuthID       string
	AuthToken    string
	HTTP         *http.Client
	DryRun       bool
	LogRequest   func(method, url string, body []byte)
}

// IsScopedToken reports whether AuthToken is a scoped token (stk_ prefix).
// Scoped tokens are sent as Bearer; regular tokens use HTTP Basic.
func (c *Client) IsScopedToken() bool {
	return strings.HasPrefix(c.AuthToken, "stk_")
}

// HodorURL joins HodorBaseURL with the given path. Returns an error if no
// hodor server is configured.
func (c *Client) HodorURL(path string) (string, error) {
	if c.HodorBaseURL == "" {
		return "", fmt.Errorf("hodor server not configured: set --hodor-server or PLIVO_HODOR_SERVER")
	}
	base := strings.TrimRight(c.HodorBaseURL, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path, nil
}

func New(authID, authToken string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		BaseURL:      DefaultBaseURL,
		BuddyBaseURL: DefaultBuddyBase,
		AuthID:       authID,
		AuthToken:    authToken,
		HTTP:         &http.Client{Timeout: timeout},
	}
}

// AccountURL returns BaseURL + "/Account/{auth_id}/" + joined parts + "/".
func (c *Client) AccountURL(parts ...string) string {
	u := c.BaseURL + "/Account/" + c.AuthID + "/"
	if len(parts) == 0 {
		return u
	}
	return u + strings.Join(parts, "/") + "/"
}

// LookupURL returns the Lookup API URL for a number — uses the separate
// lookup.plivo.com base host, not the standard api.plivo.com one. No trailing
// slash; the caller appends ?type=carrier (required by the API).
func (c *Client) LookupURL(number string) string {
	return DefaultLookupBase + "/Number/" + number
}

// BuddyURL joins BuddyBaseURL with the given absolute path (e.g.
// "/v1/aiassist/buddy-ext/chat"). Used by the `plivo buddy …` commands.
func (c *Client) BuddyURL(path string) string {
	base := strings.TrimRight(c.BuddyBaseURL, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
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
	if c.IsScopedToken() {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	} else {
		req.SetBasicAuth(c.AuthID, c.AuthToken)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "plivo-cli/"+version.Value)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

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
	req.Header.Set("User-Agent", "plivo-cli/"+version.Value)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

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
	}
	return nil, nil
}
