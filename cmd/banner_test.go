package cmd

import (
	"bytes"
	"os"
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

// The installers draw the same logo before the CLI exists, so the rows they
// carry must be the ones in bannerLogo.
func TestInstallersCarryTheLogo(t *testing.T) {
	row := regexp.MustCompile(`^[\s'",]*([.#]{40,})[\s'",]*$`)
	want := strings.Join(bannerLogo[:], "\n")
	for _, script := range []string{"install.sh", "install.ps1"} {
		src, err := os.ReadFile("../" + script)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, line := range strings.Split(string(src), "\n") {
			if m := row.FindStringSubmatch(line); m != nil {
				got = append(got, m[1])
			}
		}
		if strings.Join(got, "\n") != want {
			t.Errorf("%s carries different logo rows than bannerLogo in cmd/banner.go:\n%s", script, strings.Join(got, "\n"))
		}
	}
}

func TestBannerGridIsRectangular(t *testing.T) {
	for i, row := range bannerLogo {
		if len(row) != len(bannerLogo[0]) || strings.Trim(row, "#.") != "" {
			t.Fatalf("logo row %d is %q: want %d chars of # and .", i, row, len(bannerLogo[0]))
		}
	}
}

func TestRenderBanner(t *testing.T) {
	bannerSession(t, true, 80)
	plain := renderBanner(false)
	if len(plain) != 9 {
		t.Fatalf("got %d rows, want 9", len(plain))
	}
	for i, row := range plain {
		if strings.Contains(row, "\x1b") {
			t.Errorf("row %d has an escape without colour: %q", i, row)
		}
		if n := utf8.RuneCountInString(row); n > 52 {
			t.Errorf("row %d is %d columns, want at most 52", i, n)
		}
	}
	if !strings.Contains(plain[0], "█") || !strings.Contains(plain[8], "█") {
		t.Errorf("the logo should reach the first and last rows: %q / %q", plain[0], plain[8])
	}

	// Painted, the shadow adds a pixel on the right and one below, so a row.
	painted := renderBanner(true)
	if len(painted) != 10 {
		t.Fatalf("painted: got %d rows, want 10", len(painted))
	}
	for i, row := range painted {
		if !strings.HasSuffix(row, bannerOff) {
			t.Errorf("painted row %d does not end in a reset: %q", i, row)
		}
		if n := utf8.RuneCountInString(ansiRe.ReplaceAllString(row, "")); n > 53 {
			t.Errorf("painted row %d is %d columns, want at most 53", i, n)
		}
	}
}

func TestBannerColours(t *testing.T) {
	bannerSession(t, true, 80)
	t.Setenv("COLORTERM", "truecolor")
	all := strings.Join(renderBanner(true), "\n")
	for _, want := range []string{"38;2;77;106;255m", "38;2;38;52;140m", "48;2;38;52;140m"} {
		if !strings.Contains(all, want) {
			t.Errorf("24-bit banner lacks %q", want)
		}
	}
	t.Setenv("COLORTERM", "")
	all = strings.Join(renderBanner(true), "\n")
	for _, want := range []string{"38;5;63m", "38;5;60m", "48;5;60m"} {
		if !strings.Contains(all, want) {
			t.Errorf("256-colour banner lacks %q", want)
		}
	}
	if strings.Contains(all, ";2;") {
		t.Error("256-colour banner has a 24-bit escape")
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
	if len(lines) != 11 || lines[0] != "" || lines[10] != "" {
		t.Errorf("want 9 rows between two blank lines, got %d lines", len(lines))
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
