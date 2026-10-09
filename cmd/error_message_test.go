package cmd

import (
	"errors"
	"net/http"
	"testing"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// SIP Trunking's capital-E error body must reach the user as its readable
// text, the same in both output modes (the renderers print one Message), with
// the parsed body kept for -o json as context.upstream {status, body}.
func TestSIPTrunksGet_capitalErrorBodyIsReadable(t *testing.T) {
	for _, format := range []string{"json", "table"} {
		t.Run(format, func(t *testing.T) {
			setFakeCreds(t)
			sipServer(t, http.StatusNotFound,
				`{"Error":"Trunk details for 00000000000000000 not found","api_id":"00000000-0000-0000-0000-000000000000"}`)

			err, _, _ := execCmd(t, "sip", "trunks", "get", "00000000000000000", "-o", format)
			var ce *clierr.Error
			if !errors.As(err, &ce) {
				t.Fatalf("err = %v (%T), want *clierr.Error", err, err)
			}
			if want := "Trunk details for 00000000000000000 not found"; ce.Message != want {
				t.Errorf("Message = %q, want %q", ce.Message, want)
			}
			if ce.Code != clierr.CodeResourceNotFound {
				t.Errorf("Code = %s, want %s", ce.Code, clierr.CodeResourceNotFound)
			}
			upstream, _ := ce.Context["upstream"].(map[string]any)
			body, _ := upstream["body"].(map[string]any)
			if upstream["status"] != http.StatusNotFound || body["api_id"] != "00000000-0000-0000-0000-000000000000" {
				t.Errorf("context.upstream = %v, want {status: 404, body: <parsed body>}", ce.Context["upstream"])
			}
		})
	}
}
