package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/plivo/plivo-cli/internal/version"
	"github.com/spf13/cobra"
)

// `plivo api` is the generic REST escape hatch — the `gh api`-shaped fallback
// for anything not yet wrapped in a typed subcommand. Agents that hit a missing
// surface can still call api.plivo.com without dropping back to raw curl (and
// without learning Plivo's auth_id:auth_token Basic-auth dance).
//
// Path expansion: callers either pass an absolute REST path (`/v1/Account/...`)
// or a relative one that starts at the account scope (`/Message/`,
// `/Application/`). Relative paths expand to `/v1/Account/<active auth_id>/...`
// — auth_id taken from the resolved profile so the same command works across
// profiles without rewriting the path.
//
// Mutating methods (POST/PUT/PATCH/DELETE) flow through the same spend-verb
// gate as every other mutating command — `--yes` is required, `--dry-run`
// previews. Idempotent reads (GET/HEAD) pass through without confirmation.
//
// We re-use api.Client for the transport so basic auth + scoped-token routing
// + the configured BaseURL + dry-run + LogRequest behaviour are all identical
// to typed commands. Custom --header values are layered on after Do's defaults
// (Accept/User-Agent), so the caller can override them for debugging.

var (
	apiMethodFlag  string
	apiBodyFlag    string
	apiQueryFlags  []string
	apiHeaderFlags []string

	// apiClientForTest is a package-level test hook. When non-nil, runAPI
	// uses this client instead of going through getClient — lets the e2e
	// tests inject an api.Client pointed at an httptest server without
	// patching http.DefaultTransport (which doesn't bridge HTTP/HTTPS).
	apiClientForTest *api.Client
)

var apiCmd = &cobra.Command{
	Use:   "api <method> <path>",
	Short: "Generic REST escape hatch — hit any Plivo API path that isn't yet wrapped",
	Long: `Make an authenticated request to api.plivo.com without learning auth_id:auth_token.

This is the fallback for endpoints the CLI doesn't yet have a typed command
for. Anything you'd otherwise reach with curl + Basic auth — this command does,
plus profile resolution, dry-run, structured error envelopes, and the same exit
codes as the rest of the CLI.

Paths come in two flavours:

  /v1/Account/MA…/Message/        absolute — used as-is
  /Message/                       account-scoped — expanded to
                                  /v1/Account/<active auth_id>/Message/

Mutating verbs (POST, PUT, PATCH, DELETE) require --yes (matches the rest of
the CLI). Use --dry-run to preview without sending. GET and HEAD pass through.

Examples:

  plivo api GET /Account/
  plivo api GET /Message/ --query "limit=10"
  plivo api POST /Message/ --body @msg.json --yes
  cat msg.json | plivo api --method POST /Message/ --body @- --yes
  plivo api GET /Application/ --header "X-Debug: 1"
`,
	Args: cobra.MaximumNArgs(2),
	RunE: runAPI,
}

func init() {
	apiCmd.Flags().StringVar(&apiMethodFlag, "method", "", "HTTP method (alternative to the positional arg; useful when piping)")
	apiCmd.Flags().StringVar(&apiBodyFlag, "body", "", "request body: literal JSON, @path/to/file, or @- for stdin")
	apiCmd.Flags().StringArrayVar(&apiQueryFlags, "query", nil, "query param as key=value (repeatable)")
	apiCmd.Flags().StringArrayVar(&apiHeaderFlags, "header", nil, "extra header as 'Key: Value' (repeatable; overrides defaults)")
	registerExplainFlag(apiCmd)
	rootCmd.AddCommand(apiCmd)
}

func runAPI(cmd *cobra.Command, args []string) error {
	method, path, err := resolveAPIMethodAndPath(args, apiMethodFlag)
	if err != nil {
		return err
	}

	if err := validateAPIPath(path); err != nil {
		return err
	}

	q, err := parseAPIQueryFlags(apiQueryFlags)
	if err != nil {
		return err
	}

	headers, err := parseAPIHeaderFlags(apiHeaderFlags)
	if err != nil {
		return err
	}

	bodyBytes, err := readAPIBody(apiBodyFlag, os.Stdin)
	if err != nil {
		return err
	}

	// Spend-verb gate. Mutating methods follow the same DESTRUCTIVE_REFUSED
	// contract as every typed mutating command: --yes required, --dry-run
	// previews. GET/HEAD/OPTIONS pass through — they don't mutate.
	//
	// Inlined (rather than going through a shared helper) so this command is
	// self-contained while the broader spend-verb refactor lands in parallel.
	dryRun := dryRunFlag
	if isMutatingMethod(method) {
		switch {
		case dryRunFlag:
			dryRun = true
		case yesFlag:
			dryRun = false
		default:
			return clierr.DestructiveRefused(fmt.Sprintf("%s %s", method, path))
		}
	}

	client := apiClientForTest
	if client == nil {
		c, _, err := getClient()
		if err != nil {
			return err
		}
		client = c
	}

	fullURL := expandAPIPath(client, path)
	if len(q) > 0 {
		sep := "?"
		if strings.Contains(fullURL, "?") {
			sep = "&"
		}
		fullURL = fullURL + sep + q.Encode()
	}

	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will %s %s\n", method, fullURL)
		if len(bodyBytes) > 0 {
			fmt.Fprintf(os.Stderr, "  body: %s\n", string(bodyBytes))
		}
	}

	if dryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] %s %s\n", method, fullURL)
		if len(bodyBytes) > 0 {
			var pretty bytes.Buffer
			if json.Indent(&pretty, bodyBytes, "  ", "  ") == nil {
				fmt.Fprintf(os.Stderr, "  body:\n  %s\n", pretty.String())
			} else {
				fmt.Fprintf(os.Stderr, "  body: %s\n", string(bodyBytes))
			}
		}
		return nil
	}

	if logLevel == "debug" {
		fmt.Fprintf(os.Stderr, "[%s] %s\n", method, fullURL)
		if len(bodyBytes) > 0 {
			fmt.Fprintf(os.Stderr, "  body: %s\n", string(bodyBytes))
		}
	}

	resp, respBytes, err := doAPIRequest(client, method, fullURL, bodyBytes, headers)
	if err != nil {
		// Transport-level failure — map to NETWORK_ERROR so the renderer +
		// exit code stay consistent with the rest of the CLI.
		return clierr.NetworkError(fullURL, err)
	}

	return renderAPIResponse(resp, respBytes)
}

// resolveAPIMethodAndPath picks the HTTP method + path from positional args
// and/or the --method flag. Supports three call shapes:
//
//	plivo api GET /path                  positional method
//	plivo api --method GET /path         flag method, positional path
//	plivo api --method GET --body @-     pipe form (path positional)
//
// Returns BAD_INPUT on a missing path or unknown method.
func resolveAPIMethodAndPath(args []string, methodFlag string) (string, string, error) {
	var method, path string
	switch len(args) {
	case 0:
		method = strings.ToUpper(strings.TrimSpace(methodFlag))
		if method == "" {
			return "", "", clierr.BadInput("missing method and path. Usage: plivo api <method> <path>")
		}
		return "", "", clierr.BadInput("missing path. Usage: plivo api <method> <path>")
	case 1:
		// Either `plivo api /path --method GET` or `plivo api GET` alone.
		// We treat a single arg as the path when --method is set; otherwise
		// require both.
		if methodFlag != "" {
			method = strings.ToUpper(strings.TrimSpace(methodFlag))
			path = args[0]
		} else {
			return "", "", clierr.BadInput("missing path. Usage: plivo api <method> <path>")
		}
	default:
		method = strings.ToUpper(strings.TrimSpace(args[0]))
		path = args[1]
		if methodFlag != "" && strings.ToUpper(strings.TrimSpace(methodFlag)) != method {
			return "", "", clierr.BadFlag("method", "conflicts with positional method "+method)
		}
	}

	if !isValidHTTPMethod(method) {
		return "", "", clierr.BadFlag("method", "unknown HTTP method "+method+" (allowed: GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS)")
	}
	if path == "" {
		return "", "", clierr.BadInput("path is required")
	}
	return method, path, nil
}

// validateAPIPath rejects any path that looks like a full URL. `plivo api`
// is intentionally limited to api.plivo.com (whatever the resolved BaseURL
// points at) — letting users pass arbitrary hosts here would be a footgun
// and a security boundary the rest of the CLI doesn't cross.
func validateAPIPath(path string) error {
	if strings.Contains(path, "://") {
		return clierr.BadInput("path must not include a scheme — plivo api only talks to the configured Plivo API base URL")
	}
	return nil
}

// expandAPIPath turns the user-supplied path into a full URL against the
// client's BaseURL. Absolute REST paths (`/v1/...`) pass through; relative
// paths (`/Message/`, `Application/`) get the account-scope prefix added.
func expandAPIPath(c *api.Client, path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// Anything starting with /v1/ is already an absolute REST path — leave it.
	if strings.HasPrefix(path, "/v1/") {
		return c.BaseURL + path
	}
	// Otherwise treat it as account-scoped: /Foo/ → /v1/Account/<auth_id>/Foo/
	rest := strings.TrimPrefix(path, "/")
	return c.BaseURL + "/v1/Account/" + c.AuthID + "/" + rest
}

// parseAPIQueryFlags turns repeated --query "key=value" flags into a url.Values.
// Splits on the first "=" so values may contain '='.
func parseAPIQueryFlags(flags []string) (url.Values, error) {
	q := url.Values{}
	for _, kv := range flags {
		eq := strings.Index(kv, "=")
		if eq <= 0 {
			return nil, clierr.BadFlag("query", "expected key=value, got "+kv)
		}
		q.Add(kv[:eq], kv[eq+1:])
	}
	return q, nil
}

// parseAPIHeaderFlags turns repeated --header "Key: Value" flags into an
// http.Header. Spaces around the colon and value are trimmed; multiple
// values per key are preserved.
func parseAPIHeaderFlags(flags []string) (http.Header, error) {
	h := http.Header{}
	for _, raw := range flags {
		colon := strings.Index(raw, ":")
		if colon <= 0 {
			return nil, clierr.BadFlag("header", "expected 'Key: Value', got "+raw)
		}
		k := strings.TrimSpace(raw[:colon])
		v := strings.TrimSpace(raw[colon+1:])
		if k == "" {
			return nil, clierr.BadFlag("header", "header name cannot be empty")
		}
		h.Add(k, v)
	}
	return h, nil
}

// readAPIBody resolves the --body flag value into raw bytes:
//
//	""                empty body
//	"@-"              stdin
//	"@path"           file contents at path
//	other             literal string
func readAPIBody(body string, stdin io.Reader) ([]byte, error) {
	if body == "" {
		return nil, nil
	}
	if body == "@-" {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return nil, clierr.BadInput("reading body from stdin: " + err.Error())
		}
		return b, nil
	}
	if strings.HasPrefix(body, "@") {
		path := body[1:]
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, clierr.BadInput("reading body file " + path + ": " + err.Error())
		}
		return b, nil
	}
	return []byte(body), nil
}

// isMutatingMethod returns true for verbs that change server state.
// Used to decide whether the spend-verb --yes gate applies.
func isMutatingMethod(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

func isValidHTTPMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return true
	default:
		return false
	}
}

// doAPIRequest builds + sends the HTTP request, returning the *http.Response
// (already-closed body) and the body bytes. Auth + default headers replicate
// (*api.Client).Do so the transport behaviour is identical; custom headers are
// layered on top so callers can override.
//
// We don't go through (*api.Client).Do because Do hard-codes Content-Type
// application/json + Accept JSON + unmarshals the response — none of which fit
// the escape-hatch use case where the upstream may return text/xml or where
// the caller wants raw passthrough.
func doAPIRequest(c *api.Client, method, fullURL string, body []byte, extraHeaders http.Header) (*http.Response, []byte, error) {
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, fullURL, bodyReader)
	if err != nil {
		return nil, nil, err
	}

	// Auth — mirrors client.Do exactly (scoped token → Bearer, otherwise
	// HTTP Basic). Unauthenticated requests aren't supported here: a
	// missing profile would have failed in getClient.
	if c.IsScopedToken() {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	} else {
		req.SetBasicAuth(c.AuthID, c.AuthToken)
	}

	// Default headers — caller can override via --header.
	if len(body) > 0 && looksLikeJSON(body) {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())

	// Layer caller-supplied headers last so they win over the defaults.
	for k, vs := range extraHeaders {
		req.Header.Del(k)
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, err
	}
	return resp, respBytes, nil
}

// looksLikeJSON returns true if the body starts with `{` or `[` after
// whitespace. Used to decide whether to auto-set Content-Type: application/json.
func looksLikeJSON(b []byte) bool {
	for _, ch := range b {
		switch ch {
		case ' ', '\t', '\n', '\r':
			continue
		case '{', '[':
			return true
		default:
			return false
		}
	}
	return false
}

// renderAPIResponse converts the upstream response into our standard envelope
// shape — success path emits the upstream body verbatim (in a `data` wrapper
// for JSON mode), error path emits the unified error envelope with the
// upstream payload nested under `context.upstream` so agents can branch on it.
func renderAPIResponse(resp *http.Response, body []byte) error {
	contentType := resp.Header.Get("Content-Type")
	isJSON := strings.Contains(strings.ToLower(contentType), "application/json")
	format := effectiveFormat()

	if resp.StatusCode >= 400 {
		return apiErrorFromUpstream(resp.StatusCode, resp.Header.Get("X-Request-ID"), body, isJSON)
	}

	// Success — pretty-print JSON if upstream sent it; otherwise raw passthrough.
	if format == output.FormatJSON {
		if isJSON && len(body) > 0 {
			var raw json.RawMessage = body
			return output.JSONSuccess(os.Stdout, raw, nil)
		}
		// Non-JSON or empty body: still wrap so the envelope shape is stable.
		return output.JSONSuccess(os.Stdout, string(body), nil)
	}
	// Table mode for TTY: pretty-print JSON if possible, otherwise raw bytes.
	if isJSON && len(body) > 0 {
		var pretty bytes.Buffer
		if json.Indent(&pretty, body, "", "  ") == nil {
			_, _ = os.Stdout.Write(pretty.Bytes())
			if !bytes.HasSuffix(pretty.Bytes(), []byte("\n")) {
				_, _ = os.Stdout.Write([]byte("\n"))
			}
			return nil
		}
	}
	if len(body) > 0 {
		_, _ = os.Stdout.Write(body)
		if !bytes.HasSuffix(body, []byte("\n")) {
			_, _ = os.Stdout.Write([]byte("\n"))
		}
	}
	return nil
}

// apiErrorFromUpstream builds the unified UPSTREAM_ERROR envelope for a
// non-2xx response. The upstream payload is preserved verbatim under
// context.upstream so agents can branch on both the CLI code AND the raw
// Plivo error shape.
func apiErrorFromUpstream(status int, requestID string, body []byte, isJSON bool) error {
	upstream := map[string]any{"status": status}
	if isJSON && len(body) > 0 {
		var parsed any
		if err := json.Unmarshal(body, &parsed); err == nil {
			upstream["body"] = parsed
		} else {
			upstream["body"] = string(body)
		}
	} else if len(body) > 0 {
		upstream["body"] = string(body)
	}

	return &clierr.Error{
		Code:       clierr.CodeUpstreamError,
		Message:    fmt.Sprintf("Plivo API returned %d %s", status, http.StatusText(status)),
		Hint:       fmt.Sprintf("api.plivo.com returned %d. Inspect the upstream payload for details.", status),
		StatusCode: status,
		RequestID:  requestID,
		Retryable:  status == http.StatusTooManyRequests || status >= 500,
		Context:    map[string]any{"upstream": upstream},
	}
}
