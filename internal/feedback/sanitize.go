// Package feedback handles `plivo feedback` — capturing user-typed
// ratings + comments and shipping them to a collector. Phase 1 only
// covers the explicit user-invoked command; the contextual auto-prompt
// surface (anniversaries, version-upgrade, milestones) comes later.
//
// The package draws a clear boundary on what leaves the user's machine:
//
//   - Rating (1-5)                                       always OK
//   - Comment text                                       OK after sanitisation
//   - CLI metadata (version, os, arch, command path)     always OK
//   - Anonymous machine id + hashed auth_id              always OK
//   - Phone and card numbers, IPs, SIP URIs              NEVER, regex-stripped
//   - Auth tokens, raw auth_ids, emails                  NEVER, regex-stripped
//
// Sanitisation runs client-side, before anything is sent. It errs toward
// over-matching because the cost of a PII leak via feedback (which is
// user-typed free text) is higher than the cost of stripping a benign string.
// It strips the shapes in redactionPatterns, not every way to write a number.
package feedback

import (
	"regexp"
	"strings"
)

// MaxCommentChars is the hard limit on comment length we accept. Past
// this the prompt steers users to GitHub Issues — keeps the in-CLI
// channel for one-liner sentiment + the long-form for actual bug
// reports.
const MaxCommentChars = 500

// redactionPatterns are applied in order to every user-typed comment
// before submission. Each match is replaced with a category-specific
// placeholder so we can tell from aggregate stats which leak categories
// are most common (turns redactions themselves into a signal).
//
// Patterns deliberately err on the side of over-matching — a benign
// string that happens to look like a phone number is fine to redact;
// a real phone number that slips through to analytics is a compliance
// incident.
var redactionPatterns = []struct {
	name        string
	re          *regexp.Regexp
	replacement string
}{
	// SIP URIs, first: they hold users, passwords, numbers and hosts.
	{
		name:        "sip-uri",
		re:          regexp.MustCompile(`(?i)\bsips?:[^\s"'<>]+`),
		replacement: "[REDACTED-SIP-URI]",
	},
	// Plivo auth_ids: MA (account) or SA (subaccount) + 18 uppercase
	// alphanumerics.
	{
		name:        "auth-id",
		re:          regexp.MustCompile(`\b[MS]A[A-Z0-9]{18}\b`),
		replacement: "[REDACTED-AUTH-ID]",
	},
	// Plivo scoped tokens: stk_ + alphanumerics.
	{
		name:        "scoped-token",
		re:          regexp.MustCompile(`\bstk_[A-Za-z0-9]+\b`),
		replacement: "[REDACTED-TOKEN]",
	},
	// Email addresses.
	{
		name:        "email",
		re:          regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),
		replacement: "[REDACTED-EMAIL]",
	},
	// IPv4, then IPv6 in full or with "::" (so a time like 10:58:00 stays).
	{
		name:        "ipv4",
		re:          regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`),
		replacement: "[REDACTED-IP]",
	},
	{
		name:        "ipv6",
		re:          regexp.MustCompile(`\b(?:[0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}\b|\b(?:[0-9A-Fa-f]{1,4}:)+:(?:[0-9A-Fa-f]{1,4}\b(?::[0-9A-Fa-f]{1,4}\b)*)?`),
		replacement: "[REDACTED-IP]",
	},
	// UUIDs whole, before phones and cards, which would cut out an all-digit
	// group and leave the rest. Unanchored, so call_<uuid> goes too.
	{
		name:        "uuid",
		re:          regexp.MustCompile(`[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}`),
		replacement: "[REDACTED-TOKEN]",
	},
	// Phones with a + country code: 8-15 digits, spaces, dots, dashes or
	// brackets between them (+1 415 555 1212, +1 (415) 555-1212).
	{
		name:        "phone-international",
		re:          regexp.MustCompile(`\+\d(?:[\s.()-]{0,2}\d){7,14}\b`),
		replacement: "[REDACTED-PHONE]",
	},
	// Card numbers: 13-19 digits, maybe in space or dash groups.
	{
		name:        "card",
		re:          regexp.MustCompile(`\b\d(?:[ -]?\d){12,18}\b`),
		replacement: "[REDACTED-CARD]",
	},
	// National phones: 415-555-1212, (415) 555-1212, 98765 43210, and a
	// bare run of 10 to 12 digits (13 or more are cards, above).
	{
		name:        "phone-national",
		re:          regexp.MustCompile(`(?:\(\d{3}\)\s?|\b\d{3}[-.\s])\d{3}[-.\s]\d{4}\b|\b\d{5}[-.\s]\d{5}\b|\b\d{10,12}\b`),
		replacement: "[REDACTED-PHONE]",
	},
}

// Sanitize runs the user-typed comment through the redaction pipeline +
// length cap. Returns the cleaned comment and a count of how many
// substitutions fired (useful for telemetry — "how often do users try
// to type PII into feedback?").
//
// The cleaned comment is what gets shipped. The count is metadata.
// tokenCandidate matches anything with a token's SHAPE. Whether it really is
// one is decided separately by hasLetterAndDigit.
//
// SA-06: this used to be a single regex that required 30-80 characters AFTER a
// prefix proving both character classes were present. That conflates length
// with arrangement, so a 40-character token whose only digit sat near the end
// needed 60+ characters to match and survived redaction. Go's RE2 has no
// lookahead, so the two conditions cannot be expressed in one pattern; they are
// now checked independently, which is what the shape actually requires.
var tokenCandidate = regexp.MustCompile(`\b[A-Za-z0-9_\-]{32,80}\b`)

// hasLetterAndDigit keeps prose and long URL slugs out of the redactor: a
// credential mixes both classes, an English word does not.
func hasLetterAndDigit(s string) bool {
	var letter, digit bool
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			letter = true
		}
		if letter && digit {
			return true
		}
	}
	return false
}

// redactLongTokens replaces every token-shaped run that carries both a letter
// and a digit, regardless of where in the string they fall.
func redactLongTokens(in string) (string, int) {
	count := 0
	out := tokenCandidate.ReplaceAllStringFunc(in, func(m string) string {
		if !hasLetterAndDigit(m) {
			return m
		}
		count++
		return "[REDACTED-TOKEN]"
	})
	return out, count
}

func Sanitize(comment string) (cleaned string, redactionCount int) {
	cleaned = strings.TrimSpace(comment)
	if cleaned == "" {
		return "", 0
	}
	for _, p := range redactionPatterns {
		matches := p.re.FindAllStringIndex(cleaned, -1)
		redactionCount += len(matches)
		cleaned = p.re.ReplaceAllString(cleaned, p.replacement)
	}
	// Long opaque tokens: shape by regex, classes in code. See SA-06 above.
	{
		cleanedTokens, n := redactLongTokens(cleaned)
		cleaned = cleanedTokens
		redactionCount += n
	}

	// Length cap goes last so we don't truncate halfway through a
	// redaction placeholder, leaving "[REDACTED-A" in the wild.
	if len(cleaned) > MaxCommentChars {
		cleaned = cleaned[:MaxCommentChars]
	}
	return cleaned, redactionCount
}
