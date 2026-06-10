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
//   - Phone numbers, auth tokens, raw auth_ids, emails   NEVER, regex-stripped
//
// Sanitisation runs client-side as defence in depth; the collector
// re-runs the same pipeline server-side. Belt + suspenders because the
// cost of a PII leak via feedback (which is user-typed free text) is
// higher than the cost of double-stripping a benign string.
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
	// Plivo auth_ids: MA + 18 uppercase alphanumerics.
	{
		name:        "auth-id",
		re:          regexp.MustCompile(`\bMA[A-Z0-9]{18}\b`),
		replacement: "[REDACTED-AUTH-ID]",
	},
	// Plivo scoped tokens: stk_ + alphanumerics.
	{
		name:        "scoped-token",
		re:          regexp.MustCompile(`\bstk_[A-Za-z0-9]+\b`),
		replacement: "[REDACTED-TOKEN]",
	},
	// Plivo auth tokens: ~40-char opaque base64url-ish strings. We
	// scope this narrowly — require at least one digit AND at least
	// one letter AND total length 32-80 chars — so prose words and
	// long URLs don't trigger false-positives. The server-side
	// redaction pipeline catches anything we miss (defence in depth).
	{
		name:        "long-token-digit-letter-mix",
		re:          regexp.MustCompile(`\b(?:[A-Za-z][A-Za-z0-9_\-]*[0-9]|[0-9][A-Za-z0-9_\-]*[A-Za-z])[A-Za-z0-9_\-]{30,80}\b`),
		replacement: "[REDACTED-TOKEN]",
	},
	// E.164 phone numbers and common dialled formats.
	{
		name:        "phone-e164",
		re:          regexp.MustCompile(`\+\d{10,15}\b`),
		replacement: "[REDACTED-PHONE]",
	},
	{
		name:        "phone-dashed",
		re:          regexp.MustCompile(`\b\d{3}[-.\s]\d{3}[-.\s]\d{4}\b`),
		replacement: "[REDACTED-PHONE]",
	},
	// Email addresses.
	{
		name:        "email",
		re:          regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),
		replacement: "[REDACTED-EMAIL]",
	},
}

// Sanitize runs the user-typed comment through the redaction pipeline +
// length cap. Returns the cleaned comment and a count of how many
// substitutions fired (useful for telemetry — "how often do users try
// to type PII into feedback?").
//
// The cleaned comment is what gets shipped. The count is metadata.
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
	// Length cap goes last so we don't truncate halfway through a
	// redaction placeholder, leaving "[REDACTED-A" in the wild.
	if len(cleaned) > MaxCommentChars {
		cleaned = cleaned[:MaxCommentChars]
	}
	return cleaned, redactionCount
}
