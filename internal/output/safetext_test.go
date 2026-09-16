package output

import (
	"bytes"
	"strings"
	"testing"
)

// TestSafeTextNeutralisesControlSequences is SA-07. API-provided escape
// sequences reached human tables, key-value output and errors, so a backend
// storing hostile text (an agent name, an alias, a caller ID) could repaint
// the terminal or hide output.
func TestSafeTextNeutralisesControlSequences(t *testing.T) {
	cases := []struct{ name, in string }{
		{"ANSI colour", "\x1b[31mDANGER\x1b[0m"},
		{"clear screen", "\x1b[2J"},
		{"cursor move", "\x1b[10;10H"},
		{"carriage return overwrite", "real value\rFAKE VALUE"},
		{"bell", "ding\x07"},
		{"backspace", "abc\b\b\bxyz"},
		{"DEL", "abc\x7f"},
		{"OSC clipboard", "\x1b]52;c;aGk=\x07"},
		{"null byte", "a\x00b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SafeText(c.in)
			for _, r := range got {
				if (r < 0x20 && r != '\t' && r != '\n') || r == 0x7f {
					t.Fatalf("control character %q survived: %q", r, got)
				}
			}
		})
	}
}

// Must not mangle legitimate content: this is about control characters, not
// about restricting what text may contain.
func TestSafeTextLeavesPrintableAlone(t *testing.T) {
	keep := []string{
		"ordinary text",
		"+14155551234",
		"agent-name_v2 (production)",
		"日本語のテキスト",
		"emoji 🎧 and accents éàü",
		"tabs\tand\nnewlines are layout",
		"",
	}
	for _, s := range keep {
		if got := SafeText(s); got != s {
			t.Errorf("altered printable text: %q -> %q", s, got)
		}
	}
}

// The renderers are the actual exposure, so assert through them.
func TestTableAndKVEscapeRemoteText(t *testing.T) {
	hostile := "name\x1b[2J\x1b[Hwiped"

	var tb bytes.Buffer
	if err := Table(&tb, [][]string{{"NAME"}, {hostile}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tb.String(), "\x1b") {
		t.Error("Table passed a live escape sequence through")
	}

	var kv bytes.Buffer
	if err := KV(&kv, [][2]string{{"name", hostile}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(kv.String(), "\x1b") {
		t.Error("KV passed a live escape sequence through")
	}

	var er bytes.Buffer
	PlainError(&er, "CODE", hostile, hostile, "req-1", "https://x", false, 400)
	if strings.Contains(er.String(), "\x1b") {
		t.Error("PlainError passed a live escape sequence through")
	}
}
