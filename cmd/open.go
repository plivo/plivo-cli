package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// `plivo open` takes you from a CLI result to the same thing in the console.
// Every console path is one Plivo's docs link to or the console routes;
// a page with no such evidence (one number) is left out rather than guessed.

const (
	consoleBaseURL = "https://cx.plivo.com"
	docsBaseURL    = "https://www.plivo.com/docs/"
)

// browserOpener is openBrowser, swapped out in tests.
var browserOpener = openBrowser

// openTargets is every page `plivo open` knows, in the order help lists them.
var openTargets = []struct {
	name, arg, about string
	url              func(arg string) string
}{
	{"console", "", "the console home (the default)", func(string) string { return consoleBaseURL + "/home" }},
	{"calls", "", "voice call logs", func(string) string { return consoleBaseURL + "/logs/voice" }},
	{"call", "<call_uuid>", "one voice call, with its Call Insights", func(id string) string {
		return consoleBaseURL + "/logs/voice/" + url.PathEscape(id)
	}},
	{"sip-call", "<call_uuid>", "one SIP Trunking call", func(id string) string {
		return consoleBaseURL + "/logs/sip-trunking/" + url.PathEscape(id)
	}},
	{"docs", "[path]", "the docs home, or one page, such as voice/api/calls", docsPageURL},
}

var openCmd = &cobra.Command{
	Use:   "open [target] [id-or-path]",
	Short: "Open the console or docs page for a call, the call logs, or the docs",
	Long: `Open the matching Plivo console or docs page in your browser.

Targets:
` + openTargetList() + `
A docs path is the part after /docs/ in a page's ` + "`plivo docs list`" + ` URL.
The URL is printed first, so it can be copied if no browser opens. With
--dry-run it is printed and nothing is opened; -o json returns {url, opened}.
No login needed: the browser's own console session applies.`,
	Example: `  plivo open
  plivo open call 00000000-0000-0000-0000-000000000000
  plivo open sip-call 00000000-0000-0000-0000-000000000000 --dry-run
  plivo open docs voice/api/calls`,
	Args: cobra.MaximumNArgs(2),
	RunE: runOpen,
}

func init() {
	rootCmd.AddCommand(openCmd)
}

func openTargetList() string {
	var b strings.Builder
	for _, t := range openTargets {
		fmt.Fprintf(&b, "  %-24s %s\n", strings.TrimSpace(t.name+" "+t.arg), t.about)
	}
	return b.String()
}

type openResult struct {
	URL    string `json:"url"`
	Opened bool   `json:"opened"`
}

func runOpen(_ *cobra.Command, args []string) error {
	link, err := openTargetURL(args)
	if err != nil {
		return err
	}
	jsonOut := effectiveFormat() == output.FormatJSON
	if !jsonOut {
		fmt.Fprintln(os.Stdout, link)
	}
	opened := false
	if !dryRunFlag {
		if err := browserOpener(link); err != nil {
			fmt.Fprintf(os.Stderr, "Could not open a browser (%v); copy the URL instead.\n", err)
		} else {
			opened = true
		}
	}
	if jsonOut {
		return output.JSONSuccess(os.Stdout, openResult{URL: link, Opened: opened}, nil)
	}
	return nil
}

// openTargetURL checks the target and its argument and returns the page.
func openTargetURL(args []string) (string, error) {
	name, arg := "console", ""
	if len(args) > 0 {
		name = args[0]
	}
	if len(args) > 1 {
		arg = args[1]
	}
	for _, t := range openTargets {
		if t.name != name {
			continue
		}
		usage := "Usage: " + strings.TrimSpace("plivo open "+t.name+" "+t.arg)
		switch {
		case t.arg == "" && arg != "":
			return "", openInputError(fmt.Sprintf("plivo open %s takes no argument, got %q", name, arg), usage)
		case t.arg == "<call_uuid>" && !looksLikeUUID(arg):
			return "", openInputError(fmt.Sprintf("plivo open %s needs a call UUID, got %q", name, arg), usage)
		case t.arg == "[path]" && strings.Contains(arg, "://"):
			return "", openInputError("plivo open docs takes a docs path, not a URL", usage)
		}
		link := t.url(arg)
		if err := checkPlivoURL(link); err != nil {
			return "", err
		}
		return link, nil
	}
	names := make([]string, len(openTargets))
	for i, t := range openTargets {
		names[i] = t.name
	}
	return "", openInputError(fmt.Sprintf("unknown target %q for plivo open", name),
		"Targets: "+strings.Join(names, ", ")+". Run `plivo open --help` for what each opens.")
}

func openInputError(msg, hint string) *clierr.Error {
	err := clierr.BadInput(msg)
	err.Hint = hint
	return err
}

// docsPageURL builds a docs page URL from its path, with or without the
// leading docs/ and slashes.
func docsPageURL(path string) string {
	path = strings.Trim(strings.TrimPrefix(strings.Trim(path, "/"), "docs/"), "/")
	if path == "" {
		return docsBaseURL
	}
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return docsBaseURL + strings.Join(parts, "/")
}

// checkPlivoURL is the last check before a URL reaches the browser: https,
// and the console or docs host. The table only builds such URLs, so a
// failure here is a bug, never the user's input.
func checkPlivoURL(link string) error {
	u, err := url.Parse(link)
	if err == nil && u.Scheme == "https" && (u.Host == "cx.plivo.com" || u.Host == "www.plivo.com") {
		return nil
	}
	return &clierr.Error{Code: clierr.CodeInternalError, Message: "refusing to open " + link + ": not a Plivo console or docs URL"}
}
