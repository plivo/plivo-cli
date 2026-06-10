package feedback

import (
	"strings"
	"testing"
)

func TestSanitize_redactsPlivoAuthID(t *testing.T) {
	in := "I tried with MAABCDEFGHIJKLMNOPQR but got an error"
	got, count := Sanitize(in)
	if strings.Contains(got, "MAABCDEFGHIJKLMNOPQR") {
		t.Errorf("raw auth_id leaked through: %q", got)
	}
	if !strings.Contains(got, "[REDACTED-AUTH-ID]") {
		t.Errorf("missing redaction placeholder: %q", got)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestSanitize_redactsScopedToken(t *testing.T) {
	in := "scoped token stk_abc123def456 keeps expiring"
	got, count := Sanitize(in)
	if strings.Contains(got, "stk_abc123def456") {
		t.Errorf("scoped token leaked: %q", got)
	}
	if !strings.Contains(got, "[REDACTED-TOKEN]") {
		t.Errorf("missing redaction: %q", got)
	}
	if count == 0 {
		t.Errorf("count should be ≥1, got %d", count)
	}
}

func TestSanitize_redactsLongTokens(t *testing.T) {
	// A 40-char alphanumeric string — looks like an opaque token.
	in := "the auth_token AbCdEf1234567890AbCdEf1234567890zzzzzzzz is rejected"
	got, count := Sanitize(in)
	if strings.Contains(got, "AbCdEf1234567890AbCdEf1234567890zzzzzzzz") {
		t.Errorf("long token leaked: %q", got)
	}
	if count == 0 {
		t.Errorf("count should be ≥1")
	}
}

func TestSanitize_redactsE164Phones(t *testing.T) {
	in := "calling +14155551212 from +918012345678 both fail"
	got, count := Sanitize(in)
	if strings.Contains(got, "+14155551212") || strings.Contains(got, "+918012345678") {
		t.Errorf("phone leaked: %q", got)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestSanitize_redactsDashedPhones(t *testing.T) {
	cases := []string{
		"call 415-555-1212 please",
		"my number is 415.555.1212",
		"reach me at 415 555 1212 anytime",
	}
	for _, in := range cases {
		got, count := Sanitize(in)
		if !strings.Contains(got, "[REDACTED-PHONE]") {
			t.Errorf("input %q didn't redact: got %q", in, got)
		}
		if count != 1 {
			t.Errorf("input %q: count = %d, want 1", in, count)
		}
	}
}

func TestSanitize_redactsEmails(t *testing.T) {
	in := "ping support@plivo.com or alice+test@example.co.uk"
	got, count := Sanitize(in)
	if strings.Contains(got, "support@plivo.com") || strings.Contains(got, "alice+test@example.co.uk") {
		t.Errorf("email leaked: %q", got)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestSanitize_preservesBenignText(t *testing.T) {
	in := "the help text could be clearer on which fields are required"
	got, count := Sanitize(in)
	if got != in {
		t.Errorf("benign text was modified: %q → %q", in, got)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestSanitize_emptyAndWhitespace(t *testing.T) {
	cases := []string{"", "   ", "\t\n", "  \n  \t"}
	for _, in := range cases {
		got, count := Sanitize(in)
		if got != "" {
			t.Errorf("input %q should produce empty, got %q", in, got)
		}
		if count != 0 {
			t.Errorf("count for %q = %d, want 0", in, count)
		}
	}
}

func TestSanitize_lengthCap(t *testing.T) {
	long := strings.Repeat("a", MaxCommentChars+200)
	got, _ := Sanitize(long)
	if len(got) != MaxCommentChars {
		t.Errorf("len = %d, want %d", len(got), MaxCommentChars)
	}
}

func TestSanitize_redactionsBeforeLengthCap(t *testing.T) {
	// Build a string where the auth_id sits past the MaxCommentChars
	// boundary but the redaction should still happen before truncation.
	prefix := strings.Repeat("x", MaxCommentChars-10)
	in := prefix + " MAABCDEFGHIJKLMNOPQR tail"
	got, count := Sanitize(in)
	// Either the auth_id got redacted (count ≥1) and the placeholder
	// might be truncated — that's acceptable. What's NOT acceptable
	// is the raw auth_id surviving truncation.
	if strings.Contains(got, "MAABCDEFGHIJKLMNOPQR") {
		t.Errorf("raw auth_id leaked past length cap: %q", got)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestSanitize_multiplePIIInOneComment(t *testing.T) {
	in := "called +14155551212 with MAABCDEFGHIJKLMNOPQR; emailed bob@x.com about it"
	got, count := Sanitize(in)
	// All three should be redacted.
	if strings.Contains(got, "+14155551212") {
		t.Error("phone leaked")
	}
	if strings.Contains(got, "MAABCDEFGHIJKLMNOPQR") {
		t.Error("auth_id leaked")
	}
	if strings.Contains(got, "bob@x.com") {
		t.Error("email leaked")
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}
