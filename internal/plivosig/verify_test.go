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

// Cases where an earlier version of this port diverged from the reference
// implementation. Both produced a different signed string for a legitimate
// request, so a real callback was rejected. Expected values are taken from
// plivo-python's signature_v3.py run over the same inputs.
func TestGenerateURL_matchesReferenceOnQueryEdgeCases(t *testing.T) {
	cases := []struct {
		name, uri, method, want string
		params                  map[string]string
	}{
		{
			name:   "repeated query key keeps every value",
			uri:    "https://example.com/answer?a=1&a=2",
			method: "POST",
			params: map[string]string{"K": "v"},
			want:   "https://example.com/answer?a=1&a=2.Kv",
		},
		{
			name:   "repeated query key survives merging on GET",
			uri:    "https://example.com/answer?a=1&a=2",
			method: "GET",
			params: map[string]string{"b": "3"},
			want:   "https://example.com/answer?a=1&a=2&b=3",
		},
		{
			name:   "url query wins over a colliding param",
			uri:    "https://example.com/answer?a=1",
			method: "GET",
			params: map[string]string{"a": "2"},
			want:   "https://example.com/answer?a=1",
		},
		{
			name:   "post with params and no query gets ? but no dot",
			uri:    "https://example.com/answer",
			method: "POST",
			params: map[string]string{"CallUUID": "abc", "From": "+14155551234"},
			want:   "https://example.com/answer?CallUUIDabcFrom+14155551234",
		},
		{
			name:   "post with neither query nor params gets no ?",
			uri:    "https://example.com/answer",
			method: "POST",
			params: map[string]string{},
			want:   "https://example.com/answer",
		},
		{
			name:   "post with query but no params gets no dot",
			uri:    "https://example.com/answer?x=1",
			method: "POST",
			params: map[string]string{},
			want:   "https://example.com/answer?x=1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := generateURL(tc.uri, tc.params, tc.method)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("generateURL mismatch\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

// Stream signatures from Plivo's server signing code, which
// signs the <Stream> URL as written: scheme kept, decoded path, query decoded
// and sorted by key. A repeated query key is left out: the server signs only
// its first value, while this package keeps every value, as plivo-python
// does for HTTP callbacks (see the query edge cases above).
func TestStreamURLVectorsFromPlivoServer(t *testing.T) {
	const token, nonce = "test-auth-token", "12345678901234567890"
	cases := []struct{ uri, sig string }{
		{"wss://example.com/media/stream?tenant=42&env=prod", "Ni6eSQI9zg/LjUt95Nlt5nTYA7FpL6CQuY2F+Yy3jyc="},
		{"ws://example.com/media/stream?tenant=42&env=prod", "De3cjnXuPrS4pSTyrva6wFEYh9CpRgvlyaQ1gct9auo="},
		{"wss://example.com/media/stream", "paNuAIW+AAlyCT4lFH8ZrCvzj3gkNuvrX0r877mN3nM="},
		{"wss://example.com:8443/ws/?b=2&a=1", "d4mqJrd9gtwO+Dfc7IT4u144WmmoPrnCjYUTmM3HBSk="},
		{"wss://example.com/s?sig=abc%3D%3D&name=John+Doe", "WkbN9mLc0kqBHtOjlD4GiUIvDkZ+cSfqUsEKrF8ixmY="},
		{"wss://example.com/my%20stream/ws?id=1", "5hGH/IknEopkorKviPg1OTVLJvkZ/oqs3GBHF9Si4mw="},
		{"wss://example.com/s?name=Jos%C3%A9", "j0fffZVummAvqXtvGPkEVhnNDmDeFaTPXDP4CedUaow="},
		{"wss://example.com/s?flag&x=1", "EZ6eDWNK9Elkw49HS/1NHdZH/ItKIISoqPVbfhgM7tE="},
		{"wss://example.com/media/stream?", "paNuAIW+AAlyCT4lFH8ZrCvzj3gkNuvrX0r877mN3nM="},
		{"wss://example.com/media/stream#frag", "paNuAIW+AAlyCT4lFH8ZrCvzj3gkNuvrX0r877mN3nM="},
		{"wss://example.com", "mXPltZtATpEE0xZ2FUiuVHgIis/d14y8t/rtESnFGjU="},
		{"wss://example.com/a%2Fb/ws?q=1", "ZnGd8sbUoc4m+gsJK4smUKQF51bEy5S2dSEYnS/FZdc="},
	}
	for _, c := range cases {
		t.Run(c.uri, func(t *testing.T) {
			if !Validate(token, c.uri, "GET", nonce, c.sig, map[string]string{}) {
				t.Errorf("rejected Plivo's signature for %s", c.uri)
			}
		})
	}
}
