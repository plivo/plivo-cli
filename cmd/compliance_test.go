package cmd

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// The documented get response nests the application under "compliance".
const complianceGetBody = `{"api_id":"x","compliance":{"compliance_id":"00000000-0000-0000-0000-000000000000","alias":"test-alias","status":"rejected","country_iso":"IN","number_type":"local","user_type":"business","rejection_reason":"document unreadable","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"}}`

// The table read only top-level fields, so the documented response printed
// every value blank. A flat body must keep working too.
func TestComplianceGet_tableReadsTheApplication(t *testing.T) {
	flat := `{"api_id":"x","compliance_id":"00000000-0000-0000-0000-000000000000","alias":"test-alias","status":"rejected","country_iso":"IN","number_type":"local","user_type":"business","rejection_reason":"document unreadable","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"}`
	want := map[string]string{
		"compliance_id":    "00000000-0000-0000-0000-000000000000",
		"alias":            "test-alias",
		"status":           "rejected",
		"country_iso":      "IN",
		"number_type":      "local",
		"user_type":        "business",
		"rejection_reason": "document unreadable",
		"created_at":       "2026-01-01T00:00:00Z",
		"updated_at":       "2026-01-02T00:00:00Z",
	}
	for name, body := range map[string]string{"documented": complianceGetBody, "flat": flat} {
		t.Run(name, func(t *testing.T) {
			setFakeCreds(t)
			pointCommandsAtTestServer(t, body)
			err, stdout, _ := execCmd(t, "numbers", "compliance", "get", "00000000-0000-0000-0000-000000000000", "-o", "table")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := map[string]string{}
			for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
				k, v, _ := strings.Cut(line, ":")
				got[k] = strings.TrimSpace(v)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("table = %v\nwant    %v", got, want)
			}
		})
	}
}

// -o json is the server's body as sent, nesting included.
func TestComplianceGet_jsonStaysRaw(t *testing.T) {
	setFakeCreds(t)
	pointCommandsAtTestServer(t, complianceGetBody)
	err, stdout, _ := execCmd(t, "numbers", "compliance", "get", "00000000-0000-0000-0000-000000000000", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got struct {
		Data any `json:"data"`
	}
	var want any
	if e := json.Unmarshal([]byte(stdout), &got); e != nil {
		t.Fatalf("output is not JSON: %v\n%s", e, stdout)
	}
	_ = json.Unmarshal([]byte(complianceGetBody), &want)
	if !reflect.DeepEqual(got.Data, want) {
		t.Errorf("data = %v\nwant   %v", got.Data, want)
	}
}
