package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
)

// sipServer records full request URLs (query string included, unlike the
// shared helper) and replies with body.
func sipServer(t *testing.T, status int, body string) (func() []string, func() int) {
	t.Helper()
	var mu sync.Mutex
	var urls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		urls = append(urls, r.URL.String())
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{
		BaseURL: srv.URL, BuddyBaseURL: srv.URL,
		AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{},
	}
	t.Cleanup(func() { clientForTest = nil })
	get := func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(urls))
		copy(out, urls)
		return out
	}
	return get, func() int { return len(get()) }
}

// Flags are package-level and cobra leaves them at their last parsed value, so
// a filter set by one test would leak into the next.
func resetSIPFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		sipCallsLimit, sipCallsOffset = maxSIPCallLimit, 0
		sipCallsFrom, sipCallsTo, sipCallsDirection = "", "", ""
		sipCallsSince, sipCallsUntil = "", ""
		sipCallsCauseCode, sipCallsSource, sipCallsSTIR = 0, "", ""
		sipTrunksLimit, sipTrunksOffset, sipTrunksDirection = 20, 0, ""
		sipACLLimit, sipACLOffset = 20, 0
	})
}

func TestNormalizeSIPTime(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		endOfDay bool
		want     string
		wantErr  bool
	}{
		{"empty stays empty", "", false, "", false},
		{"bare date opens the day", "2026-09-01", false, "2026-09-01 00:00:00", false},
		{"bare date closes the day", "2026-09-01", true, "2026-09-01 23:59:59", false},
		{"minute precision", "2026-09-01 14:30", false, "2026-09-01 14:30:00", false},
		{"second precision", "2026-09-01 14:30:09", false, "2026-09-01 14:30:09", false},
		{"rfc3339 converts to utc", "2026-09-01T10:30:00+05:30", false, "2026-09-01 05:00:00", false},
		{"surrounding space tolerated", "  2026-09-01  ", false, "2026-09-01 00:00:00", false},
		{"prose rejected", "yesterday", false, "", true},
		{"slashes rejected", "01/09/2026", false, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeSIPTime(tc.in, tc.endOfDay)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q, got %q", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeSIPTime(%q, %v) = %q, want %q", tc.in, tc.endOfDay, got, tc.want)
			}
		})
	}
}

// --until must include the day the user named. A naive widening to 00:00:00
// silently drops that whole day's calls.
func TestSIPCallsList_untilIncludesTheNamedDay(t *testing.T) {
	setFakeCreds(t)
	resetSIPFlags(t)
	urls, _ := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

	if err, _, _ := execCmd(t, "sip", "calls", "list", "--until", "2026-09-15"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := urls()
	if len(got) != 1 {
		t.Fatalf("expected 1 request, got %v", got)
	}
	if !strings.Contains(got[0], "end_time__lte=2026-09-15+23%3A59%3A59") {
		t.Fatalf("--until did not close the day: %s", got[0])
	}
}

func TestSIPCallsList_mapsEveryFilterToItsQueryParam(t *testing.T) {
	setFakeCreds(t)
	resetSIPFlags(t)
	urls, _ := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

	err, _, _ := execCmd(t, "sip", "calls", "list",
		"--limit", "5", "--offset", "10",
		"--from-number", "+14155551234", "--to-number", "+13125551234",
		"--direction", "outbound", "--since", "2026-09-01",
		"--hangup-cause-code", "3000", "--hangup-source", "carrier",
		"--stir-verification", "Verified")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := urls()[0]
	for _, want := range []string{
		"limit=5", "offset=10",
		"from_number=%2B14155551234", "to_number=%2B13125551234",
		"call_direction=outbound", "end_time__gte=2026-09-01+00%3A00%3A00",
		"hangup_cause_code=3000", "hangup_source=carrier",
		"stir_verification=Verified",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("query missing %s\n  got: %s", want, got)
		}
	}
}

// An unset filter must not be sent: the endpoint 400s on an empty value for
// several of these.
func TestSIPCallsList_omitsUnsetFilters(t *testing.T) {
	setFakeCreds(t)
	resetSIPFlags(t)
	urls, _ := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

	if err, _, _ := execCmd(t, "sip", "calls", "list"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := urls()[0]
	for _, absent := range []string{
		"from_number", "to_number", "call_direction",
		"end_time__gte", "end_time__lte", "hangup_source",
		"stir_verification", "hangup_cause_code",
	} {
		if strings.Contains(got, absent) {
			t.Errorf("unset filter %s was sent: %s", absent, got)
		}
	}
}

// Both local validations must fail before any request leaves — the point is to
// save the round-trip, not just to report the same thing the API would.
func TestSIPCallsList_localValidationMakesNoRequest(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"limit above the API ceiling", []string{"sip", "calls", "list", "--limit", "21"}},
		{"limit below one", []string{"sip", "calls", "list", "--limit", "0"}},
		{"unparseable since", []string{"sip", "calls", "list", "--since", "yesterday"}},
		{"unparseable until", []string{"sip", "calls", "list", "--until", "01/09/2026"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			resetSIPFlags(t)
			_, count := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

			err, _, _ := execCmd(t, tc.args...)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if n := count(); n != 0 {
				t.Fatalf("validation should short-circuit, but %d request(s) were made", n)
			}
		})
	}
}

// --limit 20 is the documented ceiling and must remain reachable; an
// off-by-one in the bound would silently cap everyone at 19.
func TestSIPCallsList_limitAtCeilingIsAccepted(t *testing.T) {
	setFakeCreds(t)
	resetSIPFlags(t)
	_, count := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

	if err, _, _ := execCmd(t, "sip", "calls", "list", "--limit", "20"); err != nil {
		t.Fatalf("--limit 20 rejected: %v", err)
	}
	if count() != 1 {
		t.Fatal("expected the request to go out")
	}
}

func TestSIPCallsList_tableRendersRows(t *testing.T) {
	setFakeCreds(t)
	resetSIPFlags(t)
	sipServer(t, http.StatusOK, `{"meta":{},"objects":[{
		"call_uuid":"8f3c1a2e","from_number":"+14155551234","to_number":"+13125551234",
		"call_direction":"outbound","bill_duration":42,
		"hangup_cause_name":"normal_hangup","hangup_source":"carrier",
		"end_time":"2026-09-15 14:32:00"}]}`)

	err, stdout, _ := execCmd(t, "sip", "calls", "list", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"CALL_UUID", "8f3c1a2e", "+14155551234", "normal_hangup", "carrier", "42"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table missing %q\n%s", want, stdout)
		}
	}
}

// -o json echoes the upstream body, so a field the struct does not model must
// still reach the caller.
func TestSIPCallsGet_jsonPassesThroughUnmodelledFields(t *testing.T) {
	setFakeCreds(t)
	resetSIPFlags(t)
	sipServer(t, http.StatusOK, `{"call_uuid":"8f3c1a2e","a_field_we_do_not_model":"keep me"}`)

	err, stdout, _ := execCmd(t, "sip", "calls", "get", "8f3c1a2e", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "a_field_we_do_not_model") || !strings.Contains(stdout, "keep me") {
		t.Fatalf("unmodelled field was dropped:\n%s", stdout)
	}
}

func TestSIPACLList_joinsAddressesIntoOneCell(t *testing.T) {
	setFakeCreds(t)
	resetSIPFlags(t)
	sipServer(t, http.StatusOK, `{"meta":{},"objects":[{
		"ipacl_uuid":"f19c4773","name":"production-servers",
		"ip_addresses":["192.168.1.1","192.168.1.2"]}]}`)

	err, stdout, _ := execCmd(t, "sip", "acl", "list", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "192.168.1.1, 192.168.1.2") {
		t.Fatalf("addresses not joined:\n%s", stdout)
	}
}

// The trunk detail endpoint may nest the record under `object`. We cannot tell
// which shape we will get, so both must render.
func TestUnwrapSIPTrunk(t *testing.T) {
	load := func(body string) api.SIPTrunk {
		var tr api.SIPTrunk
		if err := json.Unmarshal([]byte(body), &tr); err != nil {
			t.Fatalf("fixture does not parse: %v", err)
		}
		tr.SetRaw([]byte(body))
		return tr
	}

	t.Run("flat body is returned untouched", func(t *testing.T) {
		got := unwrapSIPTrunk(load(`{"trunk_id":"flat-id","name":"direct"}`))
		if got.TrunkID != "flat-id" || got.Name != "direct" {
			t.Fatalf("flat body mangled: %+v", got)
		}
	})

	t.Run("nested body is unwrapped", func(t *testing.T) {
		got := unwrapSIPTrunk(load(`{"api_id":"x","object":{"trunk_id":"nested-id","name":"wrapped"}}`))
		if got.TrunkID != "nested-id" || got.Name != "wrapped" {
			t.Fatalf("nested body not unwrapped: %+v", got)
		}
	})

	t.Run("neither shape leaves the input alone", func(t *testing.T) {
		got := unwrapSIPTrunk(load(`{"something":"else"}`))
		if got.TrunkID != "" {
			t.Fatalf("invented a trunk id: %+v", got)
		}
	})
}

// A verbatim prod response. Types here are guesswork-prone and a single wrong
// one fails the WHOLE command with "decode response", not just that field —
// cnam_lookup is a bool, which an earlier version of this modelled as a string.
const realSIPTrunkCDR = `{"call_uuid": "f3f74402-59af-40b3-909c-80c50a4b063f",
"call_id": "459580122_124515270@206.146.101.14", "from_number": "+919902443540",
"to_number": "+17322179088", "call_direction": "inbound", "call_duration": 32,
"bill_duration": 60, "end_time": "2026-09-18 08:52:26",
"hangup_cause_name": "normal_hangup", "hangup_source": "carrier",
"total_rate": "0.00280", "total_amount": "0.00280",
"initiation_time": "2026-09-18 08:51:53", "answer_time": "2026-09-18 08:51:54",
"trunk_domain": "42741122632602585.zt.plivo.com", "from_country": "IN",
"to_country": "US", "transport_protocol": "tcp", "srtp": false,
"hangup_cause_code": 3000, "secure_trunking": false,
"secure_trunking_rate": "0.00000", "cnam_lookup": false,
"cnam_lookup_rate": "0.00000", "stir_verification": "Not Verified",
"attestation_indicator": "C", "billed_duration": 60}`

func TestSIPCallsGet_decodesARealResponse(t *testing.T) {
	setFakeCreds(t)
	resetSIPFlags(t)
	sipServer(t, http.StatusOK, realSIPTrunkCDR)

	err, stdout, _ := execCmd(t, "sip", "calls", "get", "f3f74402", "-o", "table")
	if err != nil {
		t.Fatalf("real response failed to decode: %v", err)
	}
	for _, want := range []string{
		"459580122_124515270@206.146.101.14", // call_id, the SIP-level identifier
		"normal_hangup", "carrier", "3000",
		"Not Verified", "tcp", "0.00280",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in rendered detail:\n%s", want, stdout)
		}
	}
}

// The trunk detail endpoint really does nest the record under `object` (the CDR
// endpoint does not), so the unwrap is load-bearing: without it the table is
// blank. This body is a verbatim prod response.
func TestSIPTrunksGet_decodesTheRealNestedResponse(t *testing.T) {
	setFakeCreds(t)
	resetSIPFlags(t)
	sipServer(t, http.StatusOK, `{"api_id":"db10e2de-f016-43de-a4ed-2eb8abc78beb","object":{
		"trunk_id":"42741122632602585","name":"Trunk-e9dda2","trunk_status":"enabled",
		"secure":false,"trunk_domain":"42741122632602585.zt.plivo.com",
		"trunk_direction":"inbound","ipacl_uuid":null,"credential_uuid":null,
		"primary_uri_uuid":"8fde2db4-e7e0-43ce-a6ea-5690fe402e38","fallback_uri_uuid":null,
		"created_at":"2026-08-23T19:57:52Z","updated_at":"2026-08-23T19:57:52Z"}}`)

	err, stdout, _ := execCmd(t, "sip", "trunks", "get", "42741122632602585", "-o", "table")
	if err != nil {
		t.Fatalf("real nested response failed: %v", err)
	}
	for _, want := range []string{"42741122632602585", "Trunk-e9dda2", "inbound", "enabled"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q — the nested record did not render:\n%s", want, stdout)
		}
	}
}
