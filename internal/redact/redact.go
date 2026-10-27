// Package redact removes credentials from request and response bodies before
// they are shown to a human or written to a log.
//
// SA-05: --log-level debug and --dry-run printed the request body verbatim,
// so `plivo voice endpoints create --password ...` put the SIP password into
// the terminal, and from there into terminal recordings, CI logs, support
// attachments and agent transcripts. A single shared redactor is used by every
// path that prints a body, so a new printer cannot reintroduce the leak by
// forgetting to redact.
package redact

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/plivo/plivo-cli/internal/output"
)

// Placeholder replaces a redacted value.
const Placeholder = "[REDACTED]"

// sensitiveKey reports whether a JSON/form field name names a credential.
// Substring matching on a lowercased key, so password / Password /
// sip_password / new_password are all covered without enumerating them.
func sensitiveKey(k string) bool {
	// Decode first: a urlencoded body can spell the key as pass%77ord, which
	// would otherwise slip past a literal match. Undecodable keys fall back to
	// the raw text rather than being skipped.
	if dec, err := url.QueryUnescape(k); err == nil {
		k = dec
	}
	k = strings.ToLower(strings.TrimSpace(k))
	for _, needle := range []string{
		"password", "passwd", "pwd",
		"auth_token", "authtoken",
		"secret", "api_key", "apikey",
		"credential", "private_key", "privatekey",
		"access_token", "accesstoken", "refresh_token",
	} {
		if strings.Contains(k, needle) {
			return true
		}
	}
	return false
}

// walk redacts sensitive values in place, at any depth.
func walk(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if sensitiveKey(k) {
				t[k] = Placeholder
				continue
			}
			t[k] = walk(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = walk(val)
		}
		return t
	default:
		return v
	}
}

// formValue matches key=value pairs in a urlencoded body.
var formValue = regexp.MustCompile(`([A-Za-z0-9_%\-.]+)=([^&\s]*)`)

// Body returns body with credential values replaced.
//
// JSON is redacted structurally at any nesting depth. Anything else is treated
// as a urlencoded form. A body that parses as neither is returned unchanged
// rather than guessed at, since mangling it would make debugging worse without
// making it safer.
func Body(body []byte) []byte {
	if len(bytes.TrimSpace(body)) == 0 {
		return body
	}
	var parsed any
	if err := json.Unmarshal(body, &parsed); err == nil {
		out, err := output.Marshal(walk(parsed))
		if err == nil {
			return out
		}
		return body
	}
	if !bytes.Contains(body, []byte("=")) {
		return body
	}
	return formValue.ReplaceAllFunc(body, func(m []byte) []byte {
		i := bytes.IndexByte(m, '=')
		if i < 0 || !sensitiveKey(string(m[:i])) {
			return m
		}
		return append(m[:i+1:i+1], []byte(Placeholder)...)
	})
}

// String is Body for callers holding a string.
func String(s string) string { return string(Body([]byte(s))) }

// Secrets returns a JSON response body with the value of every field named
// *password or *auth_token replaced by Placeholder, and the rest, key order
// included, byte for byte as it was. Subaccount reads carry auth_token and
// new_auth_token, and endpoint reads a password hash.
//
// Narrower than Body on purpose: responses name IDs after credentials
// (credential_uuid). A null or empty value hides nothing and is left alone;
// any other, an object or a number too, is replaced whole. A body that is not
// JSON comes back unchanged.
func Secrets(body []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(body))
	var out []byte
	done := 0 // body[:done] is in out already
	// One frame per open object or array; keyNext while an object waits for a key.
	type frame struct{ object, keyNext bool }
	var stack []frame
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return body
		}
		top := len(stack) - 1
		switch t := tok.(type) {
		case json.Delim:
			if t == '{' || t == '[' {
				stack = append(stack, frame{object: t == '{', keyNext: t == '{'})
				continue
			}
			stack, top = stack[:top], top-1
		case string:
			if top >= 0 && stack[top].keyNext {
				stack[top].keyNext = false
				if !secretField(t) {
					continue // the next token is this key's value
				}
				var v json.RawMessage
				if dec.Decode(&v) != nil {
					return body
				}
				if s := string(v); s != "null" && s != `""` && s != `"`+Placeholder+`"` {
					end := int(dec.InputOffset())
					out = append(append(out, body[done:end-len(v)]...), `"`+Placeholder+`"`...)
					done = end
				}
			}
		}
		// A value ended, so an object waits for its next key.
		if top >= 0 && stack[top].object {
			stack[top].keyNext = true
		}
	}
	if out == nil {
		return body
	}
	return append(out, body[done:]...)
}

// secretField reports whether a response field holds a credential.
func secretField(k string) bool {
	k = strings.ToLower(k)
	return strings.HasSuffix(k, "password") || strings.HasSuffix(k, "auth_token")
}
