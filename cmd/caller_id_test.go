package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
)

// A trimmed answer of GET /VerifiedCallerId/: the shape is the API's (checked
// live), the numbers and ids made up.
const callerIDList = `{"api_id":"a","meta":{"limit":20,"offset":0,"total_count":2,"next":null,"previous":null},
"objects":[
 {"phone_number":"+14155550100","alias":"Front desk","country":"US","verification_uuid":"11111111-2222-3333-4444-555555555555",
  "created_at":"2026-11-01T10:00:00Z","modified_at":"2026-11-02T10:00:00Z","resource_uri":"/v1/Account/CIFAKEPLACEHOLDER001/VerifiedCallerId/+14155550100/"},
 {"phone_number":"+919876543210","alias":"","country":"IN","verification_uuid":"66666666-7777-8888-9999-000000000000",
  "created_at":"2026-11-03T10:00:00Z","modified_at":"2026-11-03T10:00:00Z","subaccount":"SAQASUBACCOUNT1"}]}`

func TestCallerIDsList_flagsReachTheQuery(t *testing.T) {
	setFakeCreds(t)
	urls, _ := sipServer(t, http.StatusOK, callerIDList)

	err, _, _ := execCmd(t, "voice", "caller-ids", "list", "--alias", " front desk ", "--country", "in",
		"--subaccount", "SAQASUBACCOUNT1", "--limit", "5", "--offset", "10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u, _ := url.Parse(urls()[0])
	q := u.Query()
	if !strings.HasSuffix(u.Path, "/VerifiedCallerId/") || q.Get("alias") != "front desk" || q.Get("country") != "IN" ||
		q.Get("subaccount") != "SAQASUBACCOUNT1" || q.Get("limit") != "5" || q.Get("offset") != "10" {
		t.Errorf("want GET .../VerifiedCallerId/ with alias, country=IN, subaccount, limit=5, offset=10, got %s", u)
	}
}

func TestCallerIDsList_leavesOutAFilterItWasNotGiven(t *testing.T) {
	setFakeCreds(t)
	urls, _ := sipServer(t, http.StatusOK, callerIDList)

	if err, _, _ := execCmd(t, "voice", "caller-ids", "list"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, _ := url.Parse(urls()[0])
	for _, k := range []string{"alias", "country", "subaccount"} {
		if q.Query().Has(k) {
			t.Errorf("%s was sent without its flag: %s", k, q)
		}
	}
}

func TestCallerIDsList_badFilterIsRefusedBeforeAnyRequest(t *testing.T) {
	for _, args := range [][]string{
		{"--country", "USA"},
		{"--country", "1"},
		{"--subaccount", " "},
		{"--limit", "0"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			setFakeCreds(t)
			urls, _ := sipServer(t, http.StatusOK, callerIDList)
			if err, _, _ := execCmd(t, append([]string{"voice", "caller-ids", "list"}, args...)...); err == nil {
				t.Fatal("want an error")
			}
			if n := len(urls()); n != 0 {
				t.Errorf("a bad filter reached the network: %d requests", n)
			}
		})
	}
}

func TestCallerIDsList_tableAndJSON(t *testing.T) {
	setFakeCreds(t)
	sipServer(t, http.StatusOK, callerIDList)

	err, stdout, _ := execCmd(t, "voice", "caller-ids", "list", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"NUMBER ALIAS COUNTRY", "+14155550100 Front desk US", "+919876543210", "IN", "11111111-2222-3333-4444-555555555555"} {
		if !strings.Contains(squeeze(stdout), want) {
			t.Errorf("table lacks %q:\n%s", want, stdout)
		}
	}

	err, stdout, _ = execCmd(t, "voice", "caller-ids", "list", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{`"objects"`, `"total_count"`, `"modified_at"`, `"resource_uri"`, `"subaccount"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("-o json lacks %s:\n%s", want, stdout)
		}
	}
}

func TestCallerIDsGet_sendsTheNumberAsGiven(t *testing.T) {
	for in, want := range map[string]string{
		"+14155550100":   "/VerifiedCallerId/+14155550100/",
		" +14155550100 ": "/VerifiedCallerId/+14155550100/",
		"14155550100":    "/VerifiedCallerId/14155550100/",
	} {
		t.Run(in, func(t *testing.T) {
			setFakeCreds(t)
			urls, _ := sipServer(t, http.StatusOK, `{"api_id":"a","phone_number":"+14155550100","alias":"x","country":"US","verification_uuid":"u"}`)
			if err, _, _ := execCmd(t, "voice", "caller-ids", "get", in); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			u, _ := url.Parse(urls()[0])
			if !strings.HasSuffix(u.Path, want) {
				t.Errorf("want a GET ending %s, got %s", want, u.Path)
			}
		})
	}
}

func TestCallerIDsGet_badNumberIsRefusedBeforeAnyRequest(t *testing.T) {
	for _, n := range []string{"abc", "+", "", "+1415 555 0100", "+1415-555-0100", "1234567890123456", "../Application", "+1415555010?x=1"} {
		t.Run(n, func(t *testing.T) {
			setFakeCreds(t)
			urls, _ := sipServer(t, http.StatusOK, `{}`)
			err, _, _ := execCmd(t, "voice", "caller-ids", "get", n)
			var ce *clierr.Error
			if !errors.As(err, &ce) || ce.Code != clierr.CodeBadInput || !strings.Contains(ce.Hint, "+14155550100") {
				t.Errorf("want BAD_INPUT with an example number in the hint, got %v", err)
			}
			if len(urls()) != 0 {
				t.Errorf("a bad number reached the network: %v", urls())
			}
		})
	}
}

// Plivo answers an unverified number with HTTP 400 and "Phone Number not
// found" (seen live), not 404.
func TestCallerIDsGet_anUnverifiedNumberIsNotFound(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   clierr.Code
	}{
		{"400 not found", http.StatusBadRequest, `{"api_id":"a","error":"Phone Number not found"}`, clierr.CodeResourceNotFound},
		{"404", http.StatusNotFound, `{"api_id":"a","error":"not found"}`, clierr.CodeResourceNotFound},
		{"another 400 stays a validation error", http.StatusBadRequest, `{"api_id":"a","error":"Invalid phone number"}`, clierr.CodeValidation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			sipServer(t, tc.status, tc.body)
			err, _, _ := execCmd(t, "voice", "caller-ids", "get", "+14155550100")
			var ce *clierr.Error
			if !errors.As(err, &ce) || ce.Code != tc.want {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
			if tc.want == clierr.CodeResourceNotFound &&
				(!strings.Contains(ce.Message, "+14155550100") || !strings.Contains(ce.Hint, "plivo voice caller-ids list")) {
				t.Errorf("want the number in the message and the list command in the hint, got %q / %q", ce.Message, ce.Hint)
			}
		})
	}
}

func TestCallerIDsGet_tableAndJSON(t *testing.T) {
	setFakeCreds(t)
	sipServer(t, http.StatusOK, `{"api_id":"a","phone_number":"+14155550100","alias":"Front desk","country":"US",
"verification_uuid":"11111111-2222-3333-4444-555555555555","created_at":"2026-11-01T10:00:00Z","modified_at":"2026-11-02T10:00:00Z"}`)

	err, stdout, _ := execCmd(t, "voice", "caller-ids", "get", "+14155550100", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"phone_number: +14155550100", "alias: Front desk", "country: US", "verification_uuid: 11111111", "created_at: 2026-11-01", "modified_at: 2026-11-02"} {
		if !strings.Contains(squeeze(stdout), want) {
			t.Errorf("table lacks %q:\n%s", want, stdout)
		}
	}
	err, stdout, _ = execCmd(t, "voice", "caller-ids", "get", "+14155550100", "-o", "json")
	if err != nil || !strings.Contains(stdout, `"verification_uuid"`) {
		t.Errorf("-o json should pass the record through, got %v:\n%s", err, stdout)
	}
}

// sentRequest is what a write sent, as captureServer saw it.
type sentRequest struct {
	method, path string
	body         map[string]any
}

// captureServer answers every request with status and body and records the
// method, path and JSON body of each.
func captureServer(t *testing.T, status int, body string) func() []sentRequest {
	t.Helper()
	var mu sync.Mutex
	var got []sentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b map[string]any
		_ = json.Unmarshal(raw, &b)
		mu.Lock()
		got = append(got, sentRequest{r.Method, r.URL.Path, b})
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })
	return func() []sentRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]sentRequest(nil), got...)
	}
}

const (
	callerIDInitiated = `{"api_id":"a","message":"Verification code is sent to number +14155550100 which is valid for 15 minutes.","verification_uuid":"11111111-2222-3333-4444-555555555555"}`
	callerIDUUID      = "11111111-2222-3333-4444-555555555555"
)

func TestCallerIDsCreate_needsYes(t *testing.T) {
	setFakeCreds(t)
	sent := captureServer(t, http.StatusOK, callerIDInitiated)

	err, _, _ := execCmd(t, "voice", "caller-ids", "create", "+14155550100")
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Code != clierr.CodeDestructiveRefused {
		t.Fatalf("want DESTRUCTIVE_REFUSED without --yes, got %v", err)
	}
	if len(sent()) != 0 {
		t.Errorf("a passcode was requested without --yes: %v", sent())
	}
}

func TestCallerIDsCreate_dryRunSendsNothing(t *testing.T) {
	setFakeCreds(t)
	sent := captureServer(t, http.StatusOK, callerIDInitiated)

	err, _, stderr := execCmd(t, "voice", "caller-ids", "create", "+14155550100", "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sent()) != 0 || !strings.Contains(stderr, "[dry-run]") || !strings.Contains(stderr, "POST") {
		t.Errorf("want a preview and no request, got %d requests, stderr %q", len(sent()), stderr)
	}
}

func TestCallerIDsCreate_sendsEachFlagUnderItsAPIName(t *testing.T) {
	setFakeCreds(t)
	sent := captureServer(t, http.StatusOK, callerIDInitiated)

	err, _, _ := execCmd(t, "voice", "caller-ids", "add", "14155550100", "--alias", " Front desk ", "--channel", "call",
		"--subaccount", "SAQASUBACCOUNT1", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := sent()
	if len(got) != 1 || got[0].method != "POST" || !strings.HasSuffix(got[0].path, "/VerifiedCallerId/") {
		t.Fatalf("want one POST to .../VerifiedCallerId/, got %v", got)
	}
	// E.164 starts with +, so a number typed without it gets one.
	want := map[string]any{"phone_number": "+14155550100", "alias": "Front desk", "channel": "call", "subaccount": "SAQASUBACCOUNT1"}
	for k, v := range want {
		if got[0].body[k] != v {
			t.Errorf("body[%s] = %v, want %v (body %v)", k, got[0].body[k], v, got[0].body)
		}
	}
}

func TestCallerIDsCreate_leavesOutWhatWasNotGiven(t *testing.T) {
	setFakeCreds(t)
	sent := captureServer(t, http.StatusOK, callerIDInitiated)

	if err, _, _ := execCmd(t, "voice", "caller-ids", "create", "+14155550100", "--yes"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b := sent()[0].body; len(b) != 1 || b["phone_number"] != "+14155550100" {
		t.Errorf("only phone_number should be sent, got %v", b)
	}
}

func TestCallerIDsCreate_badInputIsRefusedBeforeAnyRequest(t *testing.T) {
	for _, args := range [][]string{
		{"abc"},
		{"+"},
		{"+1415"},
		{"+1415 555 0100"},
		{"1234567890123456"},
		{"+14155550100", "--channel", "whatsapp"},
		{"+14155550100", "--channel", "SMS "},
		{"+14155550100", "--subaccount", " "},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			setFakeCreds(t)
			sent := captureServer(t, http.StatusOK, callerIDInitiated)
			if err, _, _ := execCmd(t, append(append([]string{"voice", "caller-ids", "create"}, args...), "--yes")...); err == nil {
				t.Fatal("want an error")
			}
			if len(sent()) != 0 {
				t.Errorf("bad input reached the network: %v", sent())
			}
		})
	}
}

func TestCallerIDsCreate_tellsWhatToDoNext(t *testing.T) {
	setFakeCreds(t)
	captureServer(t, http.StatusOK, callerIDInitiated)

	err, stdout, stderr := execCmd(t, "voice", "caller-ids", "create", "+14155550100", "--yes", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"verification_uuid: " + callerIDUUID, "valid for 15 minutes"} {
		if !strings.Contains(squeeze(stdout), want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if want := "plivo voice caller-ids verify " + callerIDUUID + " --otp"; !strings.Contains(stderr, want) {
		t.Errorf("stderr should name the next command %q, got %q", want, stderr)
	}

	_, _, quiet := execCmd(t, "voice", "caller-ids", "create", "+14155550100", "--yes", "-o", "table", "-q")
	if quiet != "" {
		t.Errorf("-q should drop the hint, got %q", quiet)
	}
	err, stdout, _ = execCmd(t, "voice", "caller-ids", "create", "+14155550100", "--yes", "-o", "json")
	if err != nil || !strings.Contains(stdout, `"verification_uuid"`) {
		t.Errorf("-o json should pass the answer through, got %v:\n%s", err, stdout)
	}
}

func TestCallerIDsVerify_postsTheOTPToTheVerification(t *testing.T) {
	setFakeCreds(t)
	sent := captureServer(t, http.StatusOK, `{"api_id":"a","message":"Number verified successfully."}`)

	err, stdout, _ := execCmd(t, "voice", "caller-ids", "verify", " "+strings.ToUpper(callerIDUUID)+" ", "--otp", " 123456 ", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := sent()
	if len(got) != 1 || got[0].method != "POST" || !strings.HasSuffix(got[0].path, "/VerifiedCallerId/Verification/"+strings.ToUpper(callerIDUUID)+"/") ||
		got[0].body["otp"] != "123456" || len(got[0].body) != 1 {
		t.Errorf("want POST .../VerifiedCallerId/Verification/<uuid>/ with only otp=123456, got %v", got)
	}
	if !strings.Contains(stdout, "Number verified successfully.") {
		t.Errorf("stdout should show the API's message, got %q", stdout)
	}
}

func TestCallerIDsVerify_badInputIsRefusedBeforeAnyRequest(t *testing.T) {
	for _, args := range [][]string{
		{"abc", "--otp", "123456"},
		{"../Application", "--otp", "123456"},
		{callerIDUUID, "--otp", ""},
		{callerIDUUID, "--otp", "123 456"},
		{callerIDUUID, "--otp", "12345678901234567"},
		{callerIDUUID},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			setFakeCreds(t)
			sent := captureServer(t, http.StatusOK, `{}`)
			if err, _, _ := execCmd(t, append([]string{"voice", "caller-ids", "verify"}, args...)...); err == nil {
				t.Fatal("want an error")
			}
			if len(sent()) != 0 {
				t.Errorf("bad input reached the network: %v", sent())
			}
		})
	}
}

// A wrong code is Plivo's to judge: the command passes its error on and does
// not print a success.
func TestCallerIDsVerify_aWrongCodeIsAnError(t *testing.T) {
	setFakeCreds(t)
	captureServer(t, http.StatusBadRequest, `{"api_id":"a","error":"Invalid OTP"}`)

	err, stdout, _ := execCmd(t, "voice", "caller-ids", "verify", callerIDUUID, "--otp", "000000", "-o", "table")
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.StatusCode != http.StatusBadRequest {
		t.Fatalf("want the API's 400, got %v", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("a failed verification printed %q", stdout)
	}
}

// Seen live: a verification that never started, expired or is done answers
// HTTP 400 "session_uuid ... is not an active session".
func TestCallerIDsVerify_anInactiveVerificationSaysWhatToDo(t *testing.T) {
	setFakeCreds(t)
	captureServer(t, http.StatusBadRequest, `{"api_id":"a","error":"session_uuid `+callerIDUUID+` is not an active session"}`)

	err, _, _ := execCmd(t, "voice", "caller-ids", "verify", callerIDUUID, "--otp", "123456")
	var ce *clierr.Error
	if !errors.As(err, &ce) || !strings.Contains(ce.Hint, "caller-ids create") || !strings.Contains(ce.Hint, "15 minutes") {
		t.Errorf("want a hint that names create and the 15 minutes, got %v", err)
	}
}

// Seen live: Plivo refuses an Indian number before it sends anything.
func TestCallerIDsCreate_anIndianNumberSaysWhatToUseInstead(t *testing.T) {
	setFakeCreds(t)
	captureServer(t, http.StatusBadRequest, `{"api_id":"a","error":"verification of Indian number is not allowed, use Plivo's India numbers for traffic in India."}`)

	err, _, _ := execCmd(t, "voice", "caller-ids", "create", "+919876543210", "--yes")
	var ce *clierr.Error
	if !errors.As(err, &ce) || !strings.Contains(ce.Hint, "Plivo India number") || !strings.Contains(ce.Hint, "numbers search --country IN") {
		t.Errorf("want a hint that points at a Plivo India number, got %v", err)
	}
}
