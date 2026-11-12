package cmd

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// bannerSession stands in a terminal of the given width and clears every
// opt-out, so each case sets only the one it tests.
func bannerSession(t *testing.T, tty bool, cols int) {
	t.Helper()
	oldTTY, oldCols, oldQuiet := isTerminal, termCols, quietFlag
	isTerminal = func(int) bool { return tty }
	termCols = func(int) int { return cols }
	quietFlag = false
	t.Cleanup(func() { isTerminal, termCols, quietFlag = oldTTY, oldCols, oldQuiet })
	for _, k := range []string{"PLIVO_NO_BANNER", "CI", "NO_COLOR", "TERM"} {
		t.Setenv(k, "")
	}
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestBannerLogoIsRectangular(t *testing.T) {
	for i, row := range bannerLogo {
		if len(row) != len(bannerLogo[0]) || strings.Trim(row, "#.") != "" {
			t.Fatalf("logo row %d is %q: want %d chars of # and .", i, row, len(bannerLogo[0]))
		}
	}
}

func TestRenderBanner(t *testing.T) {
	plain := renderBanner(false)
	if len(plain) != 11 {
		t.Fatalf("got %d rows, want 11", len(plain))
	}
	for i, row := range plain {
		if strings.Contains(row, "\x1b") {
			t.Errorf("row %d has an escape without colour: %q", i, row)
		}
		if n := utf8.RuneCountInString(row); n > 52 {
			t.Errorf("row %d is %d columns, want at most 52", i, n)
		}
	}
	if !strings.Contains(plain[10], bannerLabel) {
		t.Errorf("label row %q does not hold %q", plain[10], bannerLabel)
	}

	for i, row := range renderBanner(true) {
		if !strings.HasPrefix(row, bannerOn()) || !strings.HasSuffix(row, bannerOff) {
			t.Errorf("row %d is not wrapped in the ink colour: %q", i, row)
		}
		if got := ansiRe.ReplaceAllString(row, ""); got != plain[i] {
			t.Errorf("row %d changes its letters when painted:\n got %q\nwant %q", i, got, plain[i])
		}
	}
}

func TestBannerInk(t *testing.T) {
	bannerSession(t, true, 80)
	t.Setenv("COLORTERM", "truecolor")
	if got := renderBanner(true)[0]; !strings.Contains(got, "38;2;77;106;255") {
		t.Errorf("24-bit terminal should get #4d6aff ink: %q", got)
	}
	t.Setenv("COLORTERM", "")
	got := renderBanner(true)[0]
	if !strings.Contains(got, "38;5;63") || strings.Contains(got, "38;2;") {
		t.Errorf("256-colour terminal should get colour 63: %q", got)
	}
}

func TestBannerWanted(t *testing.T) {
	cases := []struct {
		name  string
		tty   bool
		cols  int
		env   map[string]string
		quiet bool
		want  bool
	}{
		{"terminal", true, 80, nil, false, true},
		{"just wide enough", true, bannerMinCols, nil, false, true},
		{"too narrow", true, bannerMinCols - 1, nil, false, false},
		{"width unknown", true, 0, nil, false, false},
		{"not a terminal", false, 80, nil, false, false},
		{"ci", true, 80, map[string]string{"CI": "true"}, false, false},
		{"opted out", true, 80, map[string]string{"PLIVO_NO_BANNER": "1"}, false, false},
		{"quiet", true, 80, nil, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bannerSession(t, c.tty, c.cols)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			quietFlag = c.quiet
			if got := bannerWanted(); got != c.want {
				t.Errorf("bannerWanted() = %v, want %v", got, c.want)
			}
			var buf bytes.Buffer
			if did := printBanner(&buf); did != c.want || (buf.Len() > 0) != c.want {
				t.Errorf("printBanner() = %v, wrote %d bytes, want drawn=%v", did, buf.Len(), c.want)
			}
		})
	}
}

func TestBannerPaint(t *testing.T) {
	bannerSession(t, true, 80)
	t.Setenv("WT_SESSION", "x") // a Windows console is painted only under Windows Terminal
	if !bannerPaint() {
		t.Error("a plain terminal should be painted")
	}
	t.Setenv("NO_COLOR", "1")
	if bannerPaint() {
		t.Error("NO_COLOR should drop the colour")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if bannerPaint() {
		t.Error("a dumb terminal should not be painted")
	}
}

func TestPrintBannerLayout(t *testing.T) {
	bannerSession(t, true, 80)
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	if !printBanner(&buf) {
		t.Fatal("banner not drawn")
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 13 || lines[0] != "" || lines[12] != "" {
		t.Errorf("want 11 rows between two blank lines, got %d lines", len(lines))
	}
}

func TestPrintWelcome(t *testing.T) {
	cases := []struct{ name, email, org, want string }{
		{"Ada Lovelace", "ada@example.com", "Analytical", "Welcome Ada Lovelace <ada@example.com>\n  Org: Analytical\n\n"},
		{"Ada Lovelace", "", "", "Welcome Ada Lovelace\n\n"},
		{"", "ada@example.com", "Analytical", ""},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		printWelcome(&buf, c.name, c.email, c.org)
		if buf.String() != c.want {
			t.Errorf("printWelcome(%q, %q, %q) = %q, want %q", c.name, c.email, c.org, buf.String(), c.want)
		}
	}
}
