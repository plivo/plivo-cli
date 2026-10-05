// Package plivosig validates Plivo's V3 webhook signatures.
//
// Ported deliberately faithfully from Plivo's own Go SDK (plivo-go utils.go:
// ComputeSignatureV3 / GenerateUrl / GetSortedQueryParamString) rather than
// from the prose documentation, which does not capture the query-string and
// GET/POST handling. The SDK's published test vectors are exercised verbatim
// in verify_test.go, so a divergence from the reference shows up as a failure
// rather than as silently rejected traffic.
package plivosig

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/url"
	"sort"
	"strings"
)

// Headers Plivo sends on a signed request.
const (
	HeaderSignature   = "X-Plivo-Signature-V3"
	HeaderSignatureMA = "X-Plivo-Signature-Ma-V3"
	HeaderNonce       = "X-Plivo-Signature-V3-Nonce"
)

// sortedParams renders params as the bare concatenation (kv kv) the POST form
// of the signature uses. asQuery is kept for the GET-shaped rendering used by
// sortedQuery's single-valued callers.
func sortedParams(params map[string]string, asQuery bool) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		if asQuery {
			b.WriteString(k + "=" + params[k] + "&")
		} else {
			b.WriteString(k + params[k])
		}
	}
	out := b.String()
	if asQuery {
		out = strings.TrimRight(out, "&")
	}
	return out
}

// mergeForSignature combines the URL's own query with the supplied params.
// The query wins on a key collision, matching the reference implementation
// (which updates the params map FROM the query, not the other way round).
func mergeForSignature(q url.Values, params map[string]string) url.Values {
	merged := make(url.Values, len(q)+len(params))
	for k, v := range params {
		merged[k] = []string{v}
	}
	for k, vs := range q {
		merged[k] = append([]string(nil), vs...)
	}
	return merged
}

// sortedQuery renders k=v pairs sorted by key. A key carrying several values
// renders one pair per value, values sorted — dropping the repeats would change
// the signed string and reject a legitimate request.
func sortedQuery(v url.Values) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		vals := append([]string(nil), v[k]...)
		sort.Strings(vals)
		for _, val := range vals {
			parts = append(parts, k+"="+val)
		}
	}
	return strings.Join(parts, "&")
}

// generateURL rebuilds the exact string Plivo signed.
func generateURL(uri string, params map[string]string, method string) (string, error) {
	p, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	base := p.Scheme + "://" + p.Host + p.Path
	q := p.Query()

	if method == "GET" {
		if qs := sortedQuery(mergeForSignature(q, params)); qs != "" {
			return base + "?" + qs, nil
		}
		return base, nil
	}

	// POST signs the query as a query string and the body params as a bare
	// concatenation. The "?" appears when either side is non-empty; the "."
	// separator only when BOTH are.
	qs := sortedQuery(q)
	hasParams := len(params) > 0
	out := base
	if qs != "" || hasParams {
		out += "?" + qs
	}
	if qs != "" && hasParams {
		out += "."
	}
	return out + sortedParams(params, false), nil
}

// Compute returns the V3 signature for a request.
func Compute(authToken, uri, method, nonce string, params map[string]string) (string, error) {
	base, err := generateURL(uri, params, method)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(authToken))
	mac.Write([]byte(base + "." + nonce))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

// Validate reports whether signature matches.
//
// signature may be a comma-separated list: Plivo signs with every active auth
// token on the account, so any one matching is a pass. Compared in constant
// time, and an empty signature or nonce is always a failure rather than a
// vacuous match.
func Validate(authToken, uri, method, nonce, signature string, params map[string]string) bool {
	if authToken == "" || nonce == "" || strings.TrimSpace(signature) == "" {
		return false
	}
	want, err := Compute(authToken, uri, method, nonce, params)
	if err != nil {
		return false
	}
	for _, got := range strings.Split(signature, ",") {
		got = strings.TrimSpace(got)
		if got == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
			return true
		}
	}
	return false
}
