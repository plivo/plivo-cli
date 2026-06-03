package clierr

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// ─── Error.Error() ───────────────────────────────────────────────────────────

func TestError_String_withStatusCode(t *testing.T) {
	e := &Error{Code: CodeAuthInvalid, Message: "bad creds", StatusCode: 401}
	got := e.Error()
	if !strings.Contains(got, "AUTH_INVALID") || !strings.Contains(got, "401") || !strings.Contains(got, "bad creds") {
		t.Errorf("Error() string missing parts: %q", got)
	}
}

func TestError_String_withoutStatusCode(t *testing.T) {
	e := &Error{Code: CodeUserError, Message: "bad flag"}
	got := e.Error()
	if !strings.Contains(got, "USER_ERROR") || !strings.Contains(got, "bad flag") {
		t.Errorf("Error() string missing parts: %q", got)
	}
	if strings.Contains(got, "HTTP") {
		t.Errorf("Error() shouldn't say HTTP when StatusCode is 0: %q", got)
	}
}

func TestError_String_nilReceiver(t *testing.T) {
	var e *Error
	if got := e.Error(); got != "" {
		t.Errorf("nil receiver Error() = %q, want empty", got)
	}
}

// ─── ExitCode mapping ────────────────────────────────────────────────────────

func TestExitCode_byCategory(t *testing.T) {
	cases := []struct {
		code Code
		want int
	}{
		// Auth → 2
		{CodeAuthMissing, 2},
		{CodeAuthInvalid, 2},
		{CodeAuthForbidden, 2},
		{CodeAuthExpired, 2},
		{Code2FARequired, 2},
		{CodeRecaptchaRequired, 2},

		// Rate-limited → 4
		{CodeRateLimited, 4},

		// Destructive refused → 5
		{CodeDestructiveRefused, 5},

		// Network/upstream → 3
		{CodeNetworkError, 3},
		{CodeUpstreamTimeout, 3},
		{CodeUpstreamUnavailable, 3},
		{CodeUpstreamError, 3},
		{CodeInternalError, 3},

		// Validation / user-error / policy gates → 1
		{CodeValidation, 1},
		{CodeResourceNotFound, 1},
		{CodeResourceConflict, 1},
		{CodeBadFlag, 1},
		{CodeBadInput, 1},
		{CodeUserError, 1},
		{CodeGeoPermissionDenied, 1},
		{CodeOutboundDisabled, 1},
		{CodeInsufficientFunds, 1},
	}
	for _, tc := range cases {
		t.Run(string(tc.code), func(t *testing.T) {
			e := &Error{Code: tc.code}
			if got := e.ExitCode(); got != tc.want {
				t.Errorf("ExitCode for %s = %d, want %d", tc.code, got, tc.want)
			}
		})
	}
}

func TestExitCode_nilReceiver(t *testing.T) {
	var e *Error
	if got := e.ExitCode(); got != 0 {
		t.Errorf("nil ExitCode() = %d, want 0", got)
	}
}

func TestExitCode_unknownCode_defaults_to_1(t *testing.T) {
	e := &Error{Code: Code("NEVER_REGISTERED")}
	if got := e.ExitCode(); got != 1 {
		t.Errorf("unknown code ExitCode = %d, want 1", got)
	}
}

// ─── Constructors ────────────────────────────────────────────────────────────

func TestAuthMissing(t *testing.T) {
	e := AuthMissing()
	if e.Code != CodeAuthMissing {
		t.Errorf("Code = %s", e.Code)
	}
	if !strings.Contains(e.Hint, "plivo auth login") || !strings.Contains(e.Hint, "PLIVO_AUTH_ID") {
		t.Errorf("hint should suggest login + env vars: %q", e.Hint)
	}
}

func TestDestructiveRefused(t *testing.T) {
	e := DestructiveRefused("delete subaccount SAxxx")
	if e.Code != CodeDestructiveRefused {
		t.Errorf("Code = %s", e.Code)
	}
	if !strings.Contains(e.Message, "delete subaccount SAxxx") {
		t.Errorf("Message should include op: %q", e.Message)
	}
	if !strings.Contains(e.Hint, "--yes") {
		t.Errorf("hint should mention --yes: %q", e.Hint)
	}
}

func TestBadFlag(t *testing.T) {
	e := BadFlag("limit", "must be positive")
	if e.Code != CodeBadFlag {
		t.Errorf("Code = %s", e.Code)
	}
	if !strings.Contains(e.Message, "--limit") || !strings.Contains(e.Message, "must be positive") {
		t.Errorf("Message wrong: %q", e.Message)
	}
	if e.Context["flag"] != "limit" {
		t.Errorf("Context.flag = %v", e.Context["flag"])
	}
}

func TestBadInput(t *testing.T) {
	e := BadInput("must provide at least one of --name or --enabled")
	if e.Code != CodeBadInput {
		t.Errorf("Code = %s", e.Code)
	}
	if !strings.Contains(e.Message, "at least one of") {
		t.Errorf("Message wrong: %q", e.Message)
	}
}

func TestNetworkError(t *testing.T) {
	e := NetworkError("api.plivo.com", fmt.Errorf("connection refused"))
	if e.Code != CodeNetworkError {
		t.Errorf("Code = %s", e.Code)
	}
	if !e.Retryable {
		t.Error("network error should be retryable")
	}
	if !strings.Contains(e.Message, "api.plivo.com") {
		t.Errorf("Message should include target: %q", e.Message)
	}
	if e.Context["target"] != "api.plivo.com" {
		t.Errorf("Context.target = %v", e.Context["target"])
	}
}

// ─── FromHTTP — status-only classification ───────────────────────────────────

func TestFromHTTP_byStatusCode(t *testing.T) {
	cases := []struct {
		status        int
		wantCode      Code
		wantRetryable bool
	}{
		{401, CodeAuthInvalid, false},
		{403, CodeAuthForbidden, false},
		{404, CodeResourceNotFound, false},
		{409, CodeResourceConflict, false},
		{422, CodeValidation, false},
		{429, CodeRateLimited, true},
		{400, CodeValidation, false},
		{408, CodeUpstreamTimeout, true},
		{502, CodeUpstreamUnavailable, true},
		{503, CodeUpstreamUnavailable, true},
		{504, CodeUpstreamTimeout, true},
		{500, CodeUpstreamError, true},
		{501, CodeUpstreamError, true},
		{599, CodeUpstreamError, true},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("HTTP_%d", tc.status), func(t *testing.T) {
			e := FromHTTP(tc.status, "rid-x", []byte(`{"error":"generic"}`))
			if e.Code != tc.wantCode {
				t.Errorf("status %d: Code = %s, want %s", tc.status, e.Code, tc.wantCode)
			}
			if e.Retryable != tc.wantRetryable {
				t.Errorf("status %d: Retryable = %v, want %v", tc.status, e.Retryable, tc.wantRetryable)
			}
			if e.StatusCode != tc.status {
				t.Errorf("StatusCode = %d, want %d", e.StatusCode, tc.status)
			}
			if e.RequestID != "rid-x" {
				t.Errorf("RequestID = %q", e.RequestID)
			}
		})
	}
}

// ─── FromHTTP — body-text fingerprinting (these win over status-only) ────────

func TestFromHTTP_geoPermission(t *testing.T) {
	e := FromHTTP(403, "", []byte(`{"error":"Geo-Permission Denied: destination country not enabled"}`))
	if e.Code != CodeGeoPermissionDenied {
		t.Errorf("Code = %s, want GEO_PERMISSION_DENIED", e.Code)
	}
	if !strings.Contains(e.Hint, "Geo Permissions") {
		t.Errorf("hint should point at console: %q", e.Hint)
	}
}

func TestFromHTTP_outboundDisabled(t *testing.T) {
	e := FromHTTP(403, "", []byte(`{"error":"outbound calling is disabled for this account"}`))
	if e.Code != CodeOutboundDisabled {
		t.Errorf("Code = %s, want OUTBOUND_DISABLED", e.Code)
	}
}

func TestFromHTTP_recaptcha(t *testing.T) {
	e := FromHTTP(400, "", []byte(`{"error":"reCAPTCHA verification failed"}`))
	if e.Code != CodeRecaptchaRequired {
		t.Errorf("Code = %s, want AUTH_RECAPTCHA_REQUIRED", e.Code)
	}
}

func TestFromHTTP_2fa(t *testing.T) {
	cases := []string{
		`{"error":"2FA required"}`,
		`{"error":"two_fa challenge required"}`,
		`{"error":"two-factor authentication required"}`,
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			e := FromHTTP(401, "", []byte(body))
			if e.Code != Code2FARequired {
				t.Errorf("body %q: Code = %s, want AUTH_2FA_REQUIRED", body, e.Code)
			}
		})
	}
}

func TestFromHTTP_insufficientFunds(t *testing.T) {
	e := FromHTTP(400, "", []byte(`{"error":"Insufficient funds to send this message"}`))
	if e.Code != CodeInsufficientFunds {
		t.Errorf("Code = %s, want INSUFFICIENT_FUNDS", e.Code)
	}
}

func TestFromHTTP_identicalSrcDst(t *testing.T) {
	e := FromHTTP(422, "", []byte(`{"error":"src is identical to src in dst"}`))
	if e.Code != CodeValidation {
		t.Errorf("Code = %s", e.Code)
	}
	if !strings.Contains(e.Hint, "src and dst must differ") {
		t.Errorf("hint should explain: %q", e.Hint)
	}
}

func TestFromHTTP_invalidAuthToken(t *testing.T) {
	e := FromHTTP(401, "", []byte(`{"error":"Invalid auth token"}`))
	if e.Code != CodeAuthInvalid {
		t.Errorf("Code = %s, want AUTH_INVALID", e.Code)
	}
}

func TestFromHTTP_invalidCredentials(t *testing.T) {
	e := FromHTTP(401, "", []byte(`{"error":"Invalid credentials"}`))
	if e.Code != CodeAuthInvalid {
		t.Errorf("Code = %s", e.Code)
	}
}

func TestFromHTTP_bodyOverridesStatus(t *testing.T) {
	// 403 alone → AUTH_FORBIDDEN. With geo-permission body, becomes GEO_PERMISSION_DENIED.
	e := FromHTTP(403, "", []byte(`{"error":"Geo Permission Denied"}`))
	if e.Code != CodeGeoPermissionDenied {
		t.Errorf("body fingerprint should win over status: got %s", e.Code)
	}
}

// ─── extractMessage — various Plivo response shapes ─────────────────────────

func TestFromHTTP_extractsErrorKey(t *testing.T) {
	e := FromHTTP(400, "", []byte(`{"error":"specific reason"}`))
	if e.Message != "specific reason" {
		t.Errorf("Message = %q", e.Message)
	}
}

func TestFromHTTP_extractsMessageKey(t *testing.T) {
	e := FromHTTP(400, "", []byte(`{"message":"specific reason"}`))
	if e.Message != "specific reason" {
		t.Errorf("Message = %q", e.Message)
	}
}

func TestFromHTTP_extractsGlobalErrorKey(t *testing.T) {
	e := FromHTTP(400, "", []byte(`{"global_error":"global reason"}`))
	if e.Message != "global reason" {
		t.Errorf("Message = %q", e.Message)
	}
}

func TestFromHTTP_extractsNestedDataError(t *testing.T) {
	// PHLO config service shape: {"data": {"error": "..."}}
	e := FromHTTP(400, "", []byte(`{"data":{"error":"nested reason"}}`))
	if e.Message != "nested reason" {
		t.Errorf("Message = %q", e.Message)
	}
}

func TestFromHTTP_extractsNestedDataMessage(t *testing.T) {
	e := FromHTTP(400, "", []byte(`{"data":{"message":"nested message"}}`))
	if e.Message != "nested message" {
		t.Errorf("Message = %q", e.Message)
	}
}

func TestFromHTTP_extractsErrorsGlobalError(t *testing.T) {
	// Contacto auth gateway shape: {"errors": {"global_error": "..."}}
	e := FromHTTP(400, "", []byte(`{"errors":{"global_error":"gateway reason"}}`))
	if e.Message != "gateway reason" {
		t.Errorf("Message = %q", e.Message)
	}
}

func TestFromHTTP_nonJSONBodyFallback(t *testing.T) {
	e := FromHTTP(500, "", []byte("Internal Server Error\nplain text"))
	if !strings.Contains(e.Message, "Internal Server Error") {
		t.Errorf("Message should include raw body: %q", e.Message)
	}
}

func TestFromHTTP_longBodyTruncated(t *testing.T) {
	long := strings.Repeat("x", 1000)
	e := FromHTTP(500, "", []byte(long))
	if len(e.Message) > 410 {
		t.Errorf("Long non-JSON body should be truncated to ~400+ellipsis, got %d chars", len(e.Message))
	}
	if !strings.HasSuffix(e.Message, "…") {
		t.Errorf("truncated message should end with ellipsis: %q", e.Message[len(e.Message)-10:])
	}
}

func TestFromHTTP_emptyBody(t *testing.T) {
	e := FromHTTP(500, "", []byte(""))
	// No body → empty message but still classified by status.
	if e.Code != CodeUpstreamError {
		t.Errorf("Code = %s", e.Code)
	}
}

func TestFromHTTP_unknownJSONStructure_serializes(t *testing.T) {
	// Unknown shape — fallback path serializes the JSON.
	e := FromHTTP(400, "", []byte(`{"unexpected":"shape","x":1}`))
	if !strings.Contains(e.Message, "unexpected") {
		t.Errorf("Message should serialize unknown shape: %q", e.Message)
	}
}

// ─── Wrap ────────────────────────────────────────────────────────────────────

func TestWrap_nil(t *testing.T) {
	if Wrap(nil) != nil {
		t.Error("Wrap(nil) should return nil")
	}
}

func TestWrap_existingError(t *testing.T) {
	existing := &Error{Code: CodeAuthInvalid, Message: "preserved"}
	got := Wrap(existing)
	if got != existing {
		t.Error("Wrap should return same *Error pointer when input is already *Error")
	}
}

func TestWrap_genericError(t *testing.T) {
	got := Wrap(errors.New("some random error"))
	if got.Code != CodeUserError {
		t.Errorf("Code = %s, want USER_ERROR", got.Code)
	}
	if got.Message != "some random error" {
		t.Errorf("Message = %q", got.Message)
	}
}
