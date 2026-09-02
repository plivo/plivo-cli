package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
)

// A real /Number/ row carries 32 fields; api.Number tags 16 of them. Decoding
// into the struct and re-marshalling therefore dropped half of every record,
// which is what made `-o json` unusable for scripts and agents.
const numberListBody = `{"api_id": "00000000-0000-0000-0000-000000000000", "meta": {"limit": 1, "next": null, "offset": 0, "previous": null, "total_count": 1}, "objects": [{"active": true, "added_on": "2021-11-03", "alias": "test-alias", "application": null, "carrier": "Plivo", "city": "Tazewell", "cnam": null, "cnam_lookup": "disabled", "cnam_registration_status": null, "compliance_application_id": null, "compliance_status": null, "country": "United States", "mms_enabled": true, "mms_rate": "0.01800", "monthly_rental_rate": "0.50000", "number": "14155550001", "number_type": "local", "region": "Virginia, UNITED STATES", "renewal_date": "2026-08-28", "resource_uri": "/v1/Account/MAFAKEFORTEST/Number/14155550001/", "sms_enabled": true, "sms_rate": "0.00770", "sub_account": null, "tendlc_campaign_id": null, "tendlc_registration_status": "COMPLETED", "toll_free_sms_verification": null, "toll_free_sms_verification_id": null, "toll_free_sms_verification_order_status": null, "type": "local", "verification_info": {}, "voice_enabled": true, "voice_rate": "0.00880"}]}`

func pointCommandsAtTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{
		BaseURL:   srv.URL,
		AuthID:    "MAFAKEFORTEST",
		AuthToken: "fake-token",
		HTTP:      &http.Client{},
	}
	t.Cleanup(func() { clientForTest = nil })
	return srv
}

// upstreamKeys returns every field name the server actually sent.
func upstreamKeys(t *testing.T, body string) []string {
	t.Helper()
	var env struct {
		Objects []map[string]json.RawMessage `json:"objects"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	if len(env.Objects) == 0 {
		t.Fatal("fixture has no objects")
	}
	keys := make([]string, 0, len(env.Objects[0]))
	for k := range env.Objects[0] {
		keys = append(keys, k)
	}
	return keys
}

func TestNumbersList_JSONIsLossless(t *testing.T) {
	setFakeCreds(t)
	pointCommandsAtTestServer(t, numberListBody)

	err, stdout, _ := execCmd(t, "numbers", "list", "-o", "json", "--limit", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v (stdout: %s)", err, stdout)
	}

	var got struct {
		Data struct {
			Objects []map[string]json.RawMessage `json:"objects"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(stdout), &got); e != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", e, stdout)
	}
	if len(got.Data.Objects) != 1 {
		t.Fatalf("expected 1 object, got %d (stdout: %s)", len(got.Data.Objects), stdout)
	}

	want := upstreamKeys(t, numberListBody)
	if len(got.Data.Objects[0]) != len(want) {
		t.Errorf("field count = %d, want %d", len(got.Data.Objects[0]), len(want))
	}
	for _, k := range want {
		if _, ok := got.Data.Objects[0][k]; !ok {
			t.Errorf("field %q was dropped", k)
		}
	}
}

// The envelope must still carry meta, and api_id must survive too — both were
// lost or flattened by the old typed path.
func TestNumbersList_JSONKeepsEnvelopeFields(t *testing.T) {
	setFakeCreds(t)
	pointCommandsAtTestServer(t, numberListBody)

	err, stdout, _ := execCmd(t, "numbers", "list", "-o", "json", "--limit", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]any
	if e := json.Unmarshal([]byte(stdout), &got); e != nil {
		t.Fatalf("not valid JSON: %v", e)
	}
	data, ok := got["data"].(map[string]any)
	if !ok {
		t.Fatalf("data is not an object: %T", got["data"])
	}
	for _, k := range []string{"api_id", "meta", "objects"} {
		if _, ok := data[k]; !ok {
			t.Errorf("envelope lost %q", k)
		}
	}
}

// Table mode still renders from the typed struct, so it must be unaffected.
func TestNumbersList_TableStillRenders(t *testing.T) {
	setFakeCreds(t)
	pointCommandsAtTestServer(t, numberListBody)

	err, stdout, _ := execCmd(t, "numbers", "list", "-o", "table", "--limit", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"NUMBER", "14155550001"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table output missing %q, got: %s", want, stdout)
		}
	}
}
