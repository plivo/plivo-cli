package redact

import (
	"strings"
	"testing"
)

// TestBodyRedactsCredentials is SA-05: --log-level debug and --dry-run printed
// the request body verbatim, so a SIP endpoint password went to the terminal
// and from there into recordings, CI logs and support attachments.
func TestBodyRedactsCredentials(t *testing.T) {
	cases := []struct{ name, in string }{
		{"sip endpoint create", `{"username":"bob","password":"SuperSecret123","alias":"t"}`},
		{"capitalised key", `{"Password":"SuperSecret123"}`},
		{"prefixed key", `{"sip_password":"SuperSecret123"}`},
		{"auth token", `{"auth_token":"SuperSecret123"}`},
		{"api key", `{"api_key":"SuperSecret123"}`},
		{"nested object", `{"outer":{"inner":{"password":"SuperSecret123"}}}`},
		{"inside an array", `{"items":[{"password":"SuperSecret123"}]}`},
		{"urlencoded form", `username=bob&password=SuperSecret123&alias=t`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := String(c.in)
			if strings.Contains(got, "SuperSecret123") {
				t.Errorf("secret survived: %s", got)
			}
			if !strings.Contains(got, Placeholder) {
				t.Errorf("no placeholder written: %s", got)
			}
		})
	}
}

// Redaction must not destroy the debugging value of the output, or people
// will stop using --dry-run to check what a command will send.
func TestBodyKeepsNonSecrets(t *testing.T) {
	got := String(`{"username":"bob","password":"s3cret","alias":"desk-phone","to":"+14155551234"}`)
	for _, keep := range []string{"bob", "desk-phone", "+14155551234"} {
		if !strings.Contains(got, keep) {
			t.Errorf("redacted a non-secret %q: %s", keep, got)
		}
	}
}

// A body that is neither JSON nor a form must pass through untouched rather
// than be mangled into something misleading.
func TestBodyLeavesUnknownShapesAlone(t *testing.T) {
	for _, in := range []string{"", "   ", "plain text with no structure", "<xml>hi</xml>"} {
		if got := String(in); got != in {
			t.Errorf("altered an unparseable body: %q -> %q", in, got)
		}
	}
}
