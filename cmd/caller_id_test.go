package cmd

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

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
