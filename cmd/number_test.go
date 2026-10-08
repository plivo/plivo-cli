package cmd

import (
	"strings"
	"testing"
)

const zeroUUID = "00000000-0000-0000-0000-000000000000"

// Resellers hold one compliance application per end customer, so renting an
// Indian number for one of them means naming that customer's application.
func TestNumbersBuy_complianceApplicationID(t *testing.T) {
	t.Run("sends the id, trimmed", func(t *testing.T) {
		setFakeCreds(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		err, _, _ := execCmd(t, "numbers", "buy", "+14155551234", "--compliance-application-id", " "+zeroUUID+" ", "--yes")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p := post(reqs(), "/PhoneNumber/")
		if p == nil || p.body["compliance_application_id"] != zeroUUID {
			t.Fatalf("compliance_application_id not sent: %v", p)
		}
	})

	t.Run("omits it when unset", func(t *testing.T) {
		setFakeCreds(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		if err, _, _ := execCmd(t, "numbers", "buy", "+14155551234", "--yes"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p := post(reqs(), "/PhoneNumber/")
		if p == nil {
			t.Fatal("no rent request sent")
		}
		if _, ok := p.body["compliance_application_id"]; ok {
			t.Errorf("sent compliance_application_id nobody asked for: %v", p.body)
		}
	})

	t.Run("dry-run previews it", func(t *testing.T) {
		setFakeCreds(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		err, _, stderr := execCmd(t, "numbers", "buy", "+14155551234", "--compliance-application-id", zeroUUID, "--dry-run")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stderr, `"compliance_application_id": "`+zeroUUID+`"`) {
			t.Errorf("preview does not show the field, stderr:\n%s", stderr)
		}
		if len(reqs()) != 0 {
			t.Errorf("dry-run sent %d requests", len(reqs()))
		}
	})
}
