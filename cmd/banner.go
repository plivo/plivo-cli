package cmd

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"golang.org/x/term"
)

// The banner is the Plivo wordmark in blue with a darker shadow, and "CLI"
// under it. It marks a
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

// bannerCLI is "CLI" in the same pixels, as tall as the logo's lowercase
// letters and as heavy as its bars.
var bannerCLI = [...]string{
	".#######...####.......####",
	"#########..####.......####",
	"#########..####.......####",
	"#########..####.......####",
	"####..###..####.......####",
	"####.......####.......####",
	"####.......####.......####",
	"####..###..####.......####",
	"#########..#########..####",
	"#########..#########..####",
	"#########..#########..####",
	".#######...#########..####",
}

const (
	bannerGap     = 2  // pixel rows between the logo and "CLI"
	bannerCLIAt   = 2  // column "CLI" starts at: under the stem of the P
	bannerMinCols = 56 // narrower terminals get the welcome lines alone
)

// pixel is what one banner pixel shows: nothing, ink, or the shadow behind it.
type pixel uint8

const (
	pxNone pixel = iota
	pxInk
	pxShadow
)

// bannerPalette gives each pixel's colour: 24-bit where the terminal does it,
// else the nearest colour of the 256 palette (c256). Ink is #4d6aff, the shadow
// a darker blue. Nothing is drawn behind the letters.
var bannerPalette = map[pixel]struct{ r, g, b, c256 int }{
	pxInk:    {77, 106, 255, 63},
	pxShadow: {38, 52, 140, 60},
}

const bannerOff = "\x1b[0m"

// bannerEsc is the escape that sets the foreground, or with bg the background,
// to p's colour.
func bannerEsc(p pixel, bg bool) string {
	c, plane := bannerPalette[p], 38
	if bg {
		plane = 48
	}
	if v := os.Getenv("COLORTERM"); v == "truecolor" || v == "24bit" {
		return fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", plane, c.r, c.g, c.b)
	}
	return fmt.Sprintf("\x1b[%d;5;%dm", plane, c.c256)
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

// bannerCell is one terminal cell: two stacked pixels, drawn as a half block
// with the top pixel as its foreground and, when the bottom one differs, as
// its background.
type bannerCell struct {
	glyph  string
	fg, bg pixel
}

func newBannerCell(top, bot pixel) bannerCell {
	switch {
	case top == pxNone && bot == pxNone:
		return bannerCell{glyph: " "}
	case top == bot:
		return bannerCell{"█", top, pxNone}
	case top == pxNone:
		return bannerCell{"▄", bot, pxNone}
	case bot == pxNone:
		return bannerCell{"▀", top, pxNone}
	}
	return bannerCell{"▀", top, bot}
}

// renderBanner returns the logo with "CLI" under it, one string per terminal
// row. Painted, each letter also casts a shadow one pixel down and right.
func renderBanner(paint bool) []string {
	logoH, logoW := len(bannerLogo), len(bannerLogo[0])
	cliTop := logoH + bannerGap
	h, w := cliTop+len(bannerCLI), logoW
	if paint {
		h, w = h+1, w+1
	}
	px := make([][]pixel, h)
	for y := range px {
		px[y] = make([]pixel, w)
	}
	for x := 0; x < logoW; x++ {
		for y := 0; y < logoH; y++ {
			if bannerLogo[y][x] == '#' {
				px[y][x] = pxInk
			}
		}
		for y := range bannerCLI {
			if cx := x - bannerCLIAt; cx >= 0 && cx < len(bannerCLI[0]) && bannerCLI[y][cx] == '#' {
				px[cliTop+y][x] = pxInk
			}
		}
	}
	if paint {
		for y := 1; y < h; y++ {
			for x := 1; x < w; x++ {
				if px[y][x] == pxNone && px[y-1][x-1] == pxInk {
					px[y][x] = pxShadow
				}
			}
		}
	}

	rows := make([]string, 0, (h+1)/2)
	for y := 0; y < h; y += 2 {
		cells := make([]bannerCell, w)
		for x := range cells {
			bot := pxNone
			if y+1 < h {
				bot = px[y+1][x]
			}
			cells[x] = newBannerCell(px[y][x], bot)
		}
		for len(cells) > 0 && cells[len(cells)-1].glyph == " " {
			cells = cells[:len(cells)-1]
		}
		var sb strings.Builder
		fg, bg := pxNone, pxNone // the colours in effect
		for _, c := range cells {
			if paint {
				if c.glyph != " " && c.fg != fg {
					fg = c.fg
					sb.WriteString(bannerEsc(fg, false))
				}
				if c.bg != bg {
					bg = c.bg
					if bg == pxNone {
						sb.WriteString("\x1b[49m")
					} else {
						sb.WriteString(bannerEsc(bg, true))
					}
				}
			}
			sb.WriteString(c.glyph)
		}
		if paint && len(cells) > 0 {
			sb.WriteString(bannerOff)
		}
		rows = append(rows, sb.String())
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
