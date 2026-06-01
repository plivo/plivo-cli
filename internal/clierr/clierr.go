// Package clierr defines the unified CLI error envelope.
//
// Both AI consumers (parsing JSON on stderr) and humans (reading terminal
// output) get the same fields: a stable Code (category), a human-readable
// Message, an actionable Hint, and metadata (retryable, docs URL, upstream
// status). The renderers in internal/output decide presentation per channel.
//
// Helpers in this package categorise raw upstream errors into the taxonomy
// below, so commands don't need to know about HTTP status codes — they just
// surface clierr.FromHTTP(...) or one of the named constructors.
package clierr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Code is a machine-readable error category. Stable string so AI clients can
// switch on it without parsing free-text messages.
type Code string

const (
	// Auth — credential / session problems.
	CodeAuthMissing         Code = "AUTH_MISSING"
	CodeAuthInvalid         Code = "AUTH_INVALID"
	CodeAuthForbidden       Code = "AUTH_FORBIDDEN"
	CodeAuthExpired         Code = "AUTH_EXPIRED"
	Code2FARequired         Code = "AUTH_2FA_REQUIRED"
	CodeRecaptchaRequired   Code = "AUTH_RECAPTCHA_REQUIRED"
	CodeContactoNotLoggedIn Code = "CONTACTO_NOT_LOGGED_IN"

	// Input / state problems.
	CodeValidation         Code = "VALIDATION_ERROR"
	CodeResourceNotFound   Code = "RESOURCE_NOT_FOUND"
	CodeResourceConflict   Code = "RESOURCE_CONFLICT"
	CodeDestructiveRefused Code = "DESTRUCTIVE_REFUSED"
	CodeBadFlag            Code = "BAD_FLAG"
	CodeBadInput           Code = "BAD_INPUT"

	// Capability / policy gates from upstream Plivo.
	CodeGeoPermissionDenied Code = "GEO_PERMISSION_DENIED"
	CodeOutboundDisabled    Code = "OUTBOUND_DISABLED"
	CodeInsufficientFunds   Code = "INSUFFICIENT_FUNDS"

	// Transport / upstream errors.
	CodeRateLimited         Code = "RATE_LIMITED"
	CodeUpstreamTimeout     Code = "UPSTREAM_TIMEOUT"
	CodeUpstreamUnavailable Code = "UPSTREAM_UNAVAILABLE"
	CodeUpstreamError       Code = "UPSTREAM_ERROR"
	CodeNetworkError        Code = "NETWORK_ERROR"

	// Catch-alls.
	CodeInternalError Code = "INTERNAL_ERROR"
	CodeUserError     Code = "USER_ERROR"
)

// Error is the unified CLI error envelope.
type Error struct {
	Code       Code           `json:"code"`
	Message    string         `json:"message"`
	Hint       string         `json:"hint,omitempty"`
	Retryable  bool           `json:"retryable"`
	StatusCode int            `json:"status_code,omitempty"`
	RequestID  string         `json:"request_id,omitempty"`
	DocsURL    string         `json:"docs_url,omitempty"`
	Context    map[string]any `json:"context,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("[%s, HTTP %d] %s", e.Code, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// ExitCode maps an Error to a CLI process exit code. Stable per category so
// shell scripts and AI driver loops can branch on it.
func (e *Error) ExitCode() int {
	if e == nil {
		return 0
	}
	switch e.Code {
	case CodeAuthMissing, CodeAuthInvalid, CodeAuthForbidden, CodeAuthExpired,
		Code2FARequired, CodeRecaptchaRequired, CodeContactoNotLoggedIn:
		return 2
	case CodeRateLimited:
		return 4
	case CodeDestructiveRefused:
		return 5
	case CodeNetworkError, CodeUpstreamTimeout, CodeUpstreamUnavailable, CodeUpstreamError, CodeInternalError:
		return 3
	case CodeGeoPermissionDenied, CodeOutboundDisabled, CodeInsufficientFunds,
		CodeResourceNotFound, CodeResourceConflict, CodeValidation,
		CodeBadFlag, CodeBadInput, CodeUserError:
		return 1
	default:
		return 1
	}
}

// Helpers for the common cases. Each constructor sets a clear default hint so
// commands don't have to author the message themselves.

func AuthMissing() *Error {
	return &Error{
		Code:    CodeAuthMissing,
		Message: "No Plivo credentials configured",
		Hint:    "Run `plivo auth login` or set PLIVO_AUTH_ID and PLIVO_AUTH_TOKEN env vars.",
	}
}

func ContactoSessionMissing() *Error {
	return &Error{
		Code:    CodeContactoNotLoggedIn,
		Message: "No Contacto session active",
		Hint:    "Run `plivo contacto login --email <e> --password <p>` first.",
	}
}

func DestructiveRefused(operation string) *Error {
	return &Error{
		Code:    CodeDestructiveRefused,
		Message: fmt.Sprintf("Destructive operation refused: %s", operation),
		Hint:    "Pass --yes to confirm. To preview without sending, pass --dry-run.",
	}
}

func BadFlag(field, reason string) *Error {
	return &Error{
		Code:    CodeBadFlag,
		Message: fmt.Sprintf("Bad flag --%s: %s", field, reason),
		Hint:    "Run the command with --help to see valid flag values.",
		Context: map[string]any{"flag": field},
	}
}

func BadInput(reason string) *Error {
	return &Error{
		Code:    CodeBadInput,
		Message: reason,
		Hint:    "Run the command with --help for usage.",
	}
}

func NetworkError(target string, err error) *Error {
	return &Error{
		Code:      CodeNetworkError,
		Message:   fmt.Sprintf("Could not reach %s: %v", target, err),
		Hint:      "Check your internet connection. If on a VPN, confirm the host is reachable.",
		Retryable: true,
		Context:   map[string]any{"target": target},
	}
}

// FromHTTP classifies an upstream HTTP response into an Error. It looks at both
// the status code and the body text to pick a specific Code where possible
// (e.g. distinguishing GEO_PERMISSION_DENIED from a generic 403).
func FromHTTP(statusCode int, requestID string, body []byte) *Error {
	msg := extractMessage(body)
	e := &Error{
		StatusCode: statusCode,
		RequestID:  requestID,
		Message:    msg,
	}

	// Body-text fingerprinting first — these win over status-code defaults.
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "geo-permission") || strings.Contains(lower, "geo permission"):
		e.Code = CodeGeoPermissionDenied
		e.Hint = "Enable the destination country in Plivo Console → Messaging/Voice → Geo Permissions, then retry."
	case strings.Contains(lower, "outbound calling is disabled"):
		e.Code = CodeOutboundDisabled
		e.Hint = "Account-level outbound calling is off. Ask Plivo support to enable it, or use a different account."
	case strings.Contains(lower, "recaptcha"):
		e.Code = CodeRecaptchaRequired
		e.Hint = "Use a CLI-friendly login (`plivo contacto login`) — the /accounts/login endpoint requires browser reCAPTCHA."
	case strings.Contains(lower, "2fa") || strings.Contains(lower, "two_fa") || strings.Contains(lower, "two-factor"):
		e.Code = Code2FARequired
		e.Hint = "Account has 2FA enabled. Disable for the CLI account, or use a non-2FA path."
	case strings.Contains(lower, "insufficient") && strings.Contains(lower, "fund"):
		e.Code = CodeInsufficientFunds
		e.Hint = "Top up the account balance and retry."
	case strings.Contains(lower, "identical to src"):
		e.Code = CodeValidation
		e.Hint = "src and dst must differ. Use a destination phone number you can receive on."
	case strings.Contains(lower, "invalid auth token") || strings.Contains(lower, "invalid credentials"):
		e.Code = CodeAuthInvalid
		e.Hint = "Re-check PLIVO_AUTH_ID / PLIVO_AUTH_TOKEN, or run `plivo auth login`."
	}
	if e.Code != "" {
		// Status-specific retryability still applies.
		e.Retryable = isRetryable(statusCode)
		return e
	}

	// Fall back to status-code-only classification.
	switch {
	case statusCode == http.StatusUnauthorized:
		e.Code = CodeAuthInvalid
		e.Hint = "Credentials were rejected. Try `plivo auth whoami` to verify, or `plivo login` to re-enter."
	case statusCode == http.StatusForbidden:
		e.Code = CodeAuthForbidden
		e.Hint = "This account lacks permission for this resource. Check role/scope or contact support."
	case statusCode == http.StatusNotFound:
		e.Code = CodeResourceNotFound
		e.Hint = "List available resources with the matching `... list` command."
	case statusCode == http.StatusTooManyRequests:
		e.Code = CodeRateLimited
		e.Hint = "Plivo rate-limit is 300 req / 5 s. Back off and retry."
		e.Retryable = true
	case statusCode == http.StatusRequestTimeout, statusCode == http.StatusGatewayTimeout:
		e.Code = CodeUpstreamTimeout
		e.Hint = "Upstream took too long. Pass --timeout 60 for slow networks, then retry."
		e.Retryable = true
	case statusCode == http.StatusBadGateway, statusCode == http.StatusServiceUnavailable:
		e.Code = CodeUpstreamUnavailable
		e.Hint = "Plivo edge is briefly unreachable. Retry in a few seconds."
		e.Retryable = true
	case statusCode == http.StatusUnprocessableEntity:
		e.Code = CodeValidation
		e.Hint = "Server rejected the payload. Inspect the response body or re-run with `--log-level debug`."
	case statusCode == http.StatusConflict:
		e.Code = CodeResourceConflict
		e.Hint = "A record with this identifier already exists. Choose a unique name/id."
	case statusCode >= 400 && statusCode < 500:
		e.Code = CodeValidation
		e.Hint = "Re-check the command flags and re-run with --help."
	case statusCode >= 500:
		e.Code = CodeUpstreamError
		e.Hint = "Plivo upstream returned an error. Retry; if persistent, contact support with the request_id."
		e.Retryable = true
	default:
		e.Code = CodeUpstreamError
	}
	return e
}

// Wrap turns any plain error into a generic USER_ERROR-coded Error so the
// renderer can still output a structured envelope.
func Wrap(err error) *Error {
	if err == nil {
		return nil
	}
	if existing, ok := err.(*Error); ok {
		return existing
	}
	return &Error{
		Code:    CodeUserError,
		Message: err.Error(),
	}
}

// extractMessage pulls a human-readable error string from Plivo's various
// response shapes ({"error": "..."}, {"message": "..."}, {"data": {"error": ...}},
// nested validation maps, etc).
func extractMessage(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	var generic map[string]any
	if err := json.Unmarshal(body, &generic); err != nil {
		// Not JSON — return raw if short enough, else truncated.
		if len(trimmed) > 400 {
			return trimmed[:400] + "…"
		}
		return trimmed
	}

	// Order matters — most-specific first.
	for _, key := range []string{"error", "global_error", "message"} {
		if v, ok := generic[key].(string); ok && v != "" {
			return v
		}
	}
	// Nested data.error / data.message (PHLO config service shape).
	if data, ok := generic["data"].(map[string]any); ok {
		for _, key := range []string{"error", "message", "global_error"} {
			if v, ok := data[key].(string); ok && v != "" {
				return v
			}
		}
	}
	// errors.global_error (Contacto auth gateway shape).
	if errs, ok := generic["errors"].(map[string]any); ok {
		if v, ok := errs["global_error"].(string); ok && v != "" {
			return v
		}
	}
	// Last resort: serialise the whole thing.
	if b, err := json.Marshal(generic); err == nil {
		if len(b) > 400 {
			return string(b[:400]) + "…"
		}
		return string(b)
	}
	return trimmed
}

func isRetryable(statusCode int) bool {
	if statusCode == 429 {
		return true
	}
	if statusCode >= 500 {
		return true
	}
	return false
}
