package plivosig

import "testing"

// plivoParams is the parameter set from Plivo's own SDK test vectors.
func plivoParams() map[string]string {
	return map[string]string{
		"Direction":       "outbound",
		"From":            "19792014278",
		"ALegUUID":        "3e82ae9d-2c78-4d85-b1a4-6eae7dbafb36",
		"CallStatus":      "in-progress",
		"BillRate":        "0.002",
		"ParentAuthID":    "MANWVLYTK4ZWU1YTY4QA",
		"To":              "sip:PlivoSignature382029104058171078704104@phone-qa.voice.plivodev.com",
		"ALegRequestUUID": "3e82ae9d-2c78-4d85-b1a4-6eae7dbafb36",
		"CallUUID":        "3e82ae9d-2c78-4d85-b1a4-6eae7dbafb36",
		"RequestUUID":     "3e82ae9d-2c78-4d85-b1a4-6eae7dbafb36",
		"SIP-H-To":        "<sip:PlivoSignature382029104058171078704104@52.9.11.55;transport=udp>;tag=1",
		"SessionStart":    "2020-04-08 11:34:33.238707",
		"Event":           "StartApp",
	}
}

const (
	vecURL   = "https://plivobin.non-prod.plivops.com/api/v1/validate_signature03.xml/?a=b&c=d"
	vecNonce = "31627761595286130198"
	vecToken = "Y2Q2ZDgxZmY5YWRiOTI5YmQ1Njg0MTAxZWIyOTc4"
)

// TestAgainstPlivoSDKVectors is the load-bearing test. These signatures come
// from Plivo's own plivo-go test suite. If this implementation diverges from
// the reference, real Plivo traffic would be rejected and `voice streams
// forward` would break for everyone, so this must be exact rather than
// plausible.
func TestAgainstPlivoSDKVectors(t *testing.T) {
	cases := []struct{ name, method, sig string }{
		{"POST", "POST", "k7Pusd4OxCIjR5IfA9iedDNu/h/gbdYqdzG/MiYtd1c="},
		{"GET", "GET", "UBq8jAtd32wR8EK9VgxbBn4n5rpI/l1H9iN4WfSEHFQ="},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Compute(vecToken, vecURL, c.method, vecNonce, plivoParams())
			if err != nil {
				t.Fatal(err)
			}
			if got != c.sig {
				t.Fatalf("signature mismatch, this implementation diverges from plivo-go\n got  %s\n want %s", got, c.sig)
			}
			if !Validate(vecToken, vecURL, c.method, vecNonce, c.sig, plivoParams()) {
				t.Error("Validate rejected a signature Compute produces")
			}
		})
	}
}

// Rejections that matter: each of these would be a hole if it passed.
func TestValidateRejects(t *testing.T) {
	p := plivoParams()
	cases := []struct {
		name                           string
		token, uri, method, nonce, sig string
	}{
		{"wrong token", "not-the-token", vecURL, "POST", vecNonce, "k7Pusd4OxCIjR5IfA9iedDNu/h/gbdYqdzG/MiYtd1c="},
		{"wrong nonce", vecToken, vecURL, "POST", "0000000000", "k7Pusd4OxCIjR5IfA9iedDNu/h/gbdYqdzG/MiYtd1c="},
		{"wrong url", vecToken, "https://evil.example/x", "POST", vecNonce, "k7Pusd4OxCIjR5IfA9iedDNu/h/gbdYqdzG/MiYtd1c="},
		{"GET sig on POST", vecToken, vecURL, "POST", vecNonce, "UBq8jAtd32wR8EK9VgxbBn4n5rpI/l1H9iN4WfSEHFQ="},
		{"empty signature", vecToken, vecURL, "POST", vecNonce, ""},
		{"whitespace signature", vecToken, vecURL, "POST", vecNonce, "   "},
		{"empty nonce", vecToken, vecURL, "POST", "", "k7Pusd4OxCIjR5IfA9iedDNu/h/gbdYqdzG/MiYtd1c="},
		{"empty token", "", vecURL, "POST", vecNonce, "k7Pusd4OxCIjR5IfA9iedDNu/h/gbdYqdzG/MiYtd1c="},
		{"only commas", vecToken, vecURL, "POST", vecNonce, ",,,"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if Validate(c.token, c.uri, c.method, c.nonce, c.sig, p) {
				t.Error("accepted a request it must reject")
			}
		})
	}
}

// Plivo signs with every active auth token and sends a comma-separated list,
// so a valid signature in any position must pass.
func TestValidateAcceptsAnyOfMultipleSignatures(t *testing.T) {
	good := "k7Pusd4OxCIjR5IfA9iedDNu/h/gbdYqdzG/MiYtd1c="
	for _, sig := range []string{
		good + ",AAAAfake=",
		"AAAAfake=," + good,
		"AAAAfake=," + good + ",BBBBfake=",
		" " + good + " , AAAAfake= ",
	} {
		if !Validate(vecToken, vecURL, "POST", vecNonce, sig, plivoParams()) {
			t.Errorf("rejected a list containing a valid signature: %q", sig)
		}
	}
}
