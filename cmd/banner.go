package cmd

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"golang.org/x/term"
)

// The banner is the Plivo wordmark in blue, with "CLI" under it. It marks a
// moment worth a pause (a login, an upgrade, a skill install) and nothing
// else: it goes to stderr, only on a terminal, and never in CI, so no script,
// pipe or coding agent ever reads it.

// bannerLogo is the wordmark as pixels, '#' lit: the logo scaled down to
// 52x18. A terminal cell holds two of these stacked (a half block), so a pixel
// is about square.
var bannerLogo = [...]string{
	"..##..........####...##.............................",
	"..####........####..####............................",
	"..######......####..####............................",
	"..#########...####...###............................",
	"...##########.####..................................",
	".....########.####..####..####....#####...######....",
	".##....######.####..####..####....####...#########..",
	"####....#####.####..####..#####...####..###########.",
	"..#...#######.####..####...####..#####.############.",
	"....#########.####..####...####..####..#####...#####",
	"..##########..####..####...##########..####.....####",
	"..########....####..####....########..#####.....####",
	"..######......####..####....########...####.....####",
	"..####........####..####.....######....#####...#####",
	"..####........####..####.....######....############.",
	"..####........####..####.....#####......###########.",
	"..####........####..####......####.......#########..",
	"...###.........###..####......###.........######....",
}

const (
	bannerGap     = 2 // pixel rows between the logo and the label
	bannerLabel   = "C L I"
	bannerMinCols = 56 // narrower terminals get the welcome lines alone
)

// The ink is #4d6aff where the terminal does 24-bit colour, else its nearest
// 256-colour match, 63. Nothing is drawn behind the letters.
const (
	bannerInkTrue = "\x1b[38;2;77;106;255m"
	bannerInk256  = "\x1b[38;5;63m"
	bannerOff     = "\x1b[0m"
)

// bannerOn is the escape that starts a painted row.
func bannerOn() string {
	if c := os.Getenv("COLORTERM"); c == "truecolor" || c == "24bit" {
		return bannerInkTrue
	}
	return bannerInk256
}

// termCols reports the terminal's width, or 0 when it can't tell. Tests stand
// in a width.
var termCols = func(fd int) int {
	w, _, err := term.GetSize(fd)
	if err != nil {
		return 0
	}
	return w
}

// bannerWanted reports whether the banner may print: stderr is a wide enough
// terminal, and nobody opted out. PLIVO_NO_BANNER and --quiet are the opt-outs;
// CI is never interactive.
func bannerWanted() bool {
	if os.Getenv("PLIVO_NO_BANNER") != "" || os.Getenv("CI") != "" || quietFlag {
		return false
	}
	fd := int(os.Stderr.Fd())
	return isTerminal(fd) && termCols(fd) >= bannerMinCols
}

// bannerPaint reports whether the letters may be coloured. Without colour
// (NO_COLOR, a dumb terminal, a Windows console that would print the escapes as
// text) they print in the terminal's own colour.
func bannerPaint() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return runtime.GOOS != "windows" || os.Getenv("WT_SESSION") != ""
}

// renderBanner returns the logo and its label, one string per terminal row.
func renderBanner(paint bool) []string {
	logoH, logoW := len(bannerLogo), len(bannerLogo[0])
	lit := func(py, x int) bool { return py < logoH && bannerLogo[py][x] == '#' }
	labelRow := (logoH + bannerGap) / 2
	labelAt := (logoW - len(bannerLabel)) / 2
	on := bannerOn()

	rows := make([]string, 0, labelRow+1)
	for tr := 0; tr <= labelRow; tr++ {
		var sb strings.Builder
		for x := 0; x < logoW; x++ {
			cell := " "
			switch top, bot := lit(2*tr, x), lit(2*tr+1, x); {
			case top && bot:
				cell = "█"
			case top:
				cell = "▀"
			case bot:
				cell = "▄"
			}
			if tr == labelRow && x >= labelAt && x < labelAt+len(bannerLabel) {
				cell = string(bannerLabel[x-labelAt])
			}
			sb.WriteString(cell)
		}
		row := strings.TrimRight(sb.String(), " ")
		if paint {
			if tr == labelRow {
				row = "\x1b[1m" + row
			}
			row = on + row + bannerOff
		}
		rows = append(rows, row)
	}
	return rows
}

// printBanner draws the logo on w, with a blank line either side, and reports
// whether it did. Callers pass os.Stderr.
func printBanner(w io.Writer) bool {
	if !bannerWanted() {
		return false
	}
	fmt.Fprintln(w)
	for _, row := range renderBanner(bannerPaint()) {
		fmt.Fprintln(w, row)
	}
	fmt.Fprintln(w)
	return true
}

// printWelcome greets the person a login just signed in, under the logo.
func printWelcome(w io.Writer, name, email, org string) {
	if name == "" {
		return
	}
	who := name
	if email != "" {
		who += " <" + email + ">"
	}
	fmt.Fprintf(w, "Welcome %s\n", who)
	if org != "" {
		fmt.Fprintf(w, "  Org: %s\n", org)
	}
	fmt.Fprintln(w)
}
