// Package redact removes credentials from request bodies before they are
// shown to a human or written to a log.
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
	"regexp"
	"strings"
)

// Placeholder replaces a redacted value.
const Placeholder = "[REDACTED]"

// sensitiveKey reports whether a JSON/form field name names a credential.
// Substring matching on a lowercased key, so password / Password /
// sip_password / new_password are all covered without enumerating them.
func sensitiveKey(k string) bool {
	k = strings.ToLower(k)
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
var formValue = regexp.MustCompile(`([A-Za-z0-9_\-.]+)=([^&\s]*)`)

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
		out, err := json.Marshal(walk(parsed))
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
