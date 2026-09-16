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

// sortedParams renders params either as a query string (k=v&k=v) or as the
// bare concatenation (kv kv) the POST form of the signature uses.
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

func queryToMap(q url.Values) map[string]string {
	m := make(map[string]string, len(q))
	for k, v := range q {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}

// generateURL rebuilds the exact string Plivo signed.
func generateURL(uri string, params map[string]string, method string) (string, error) {
	p, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	out := p.Scheme + "://" + p.Host + p.Path
	if len(params) > 0 || len(p.RawQuery) > 0 {
		out += "?"
	}
	if len(p.RawQuery) > 0 {
		if method == "GET" {
			merged := queryToMap(p.Query())
			for k, v := range params {
				merged[k] = v
			}
			out += sortedParams(merged, true)
		} else {
			out += sortedParams(queryToMap(p.Query()), true) + "." + sortedParams(params, false)
			out = strings.TrimRight(out, ".")
		}
	} else {
		out += sortedParams(params, method == "GET")
	}
	return out, nil
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
