package cmd

import (
	"regexp"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
)

// Every path in `plivo api --help` must actually resolve. One of them used to
// be `/Account/`, which expandAPIPath turns into
// /v1/Account/<auth_id>/Account/ — a doubled segment that 404s. The published
// documentation copied that example verbatim, so a wrong example here becomes
// a wrong example on the website.
func TestAPIHelpExamples_resolveWithoutDoubledAccount(t *testing.T) {
	c := api.New("MAEXAMPLEEXAMPLEEXAM", "tok", 0)

	// Pull the paths straight out of the help text, so adding a bad example
	// to the docs string fails this test rather than shipping.
	pathRE := regexp.MustCompile(`plivo api (?:--method )?[A-Z]+ (/[^\s]*)`)
	found := pathRE.FindAllStringSubmatch(apiCmd.Long, -1)
	if len(found) == 0 {
		t.Fatal("no example paths found in `plivo api` help — did the Long text change shape?")
	}

	for _, m := range found {
		path := m[1]
		// The doc text uses a typographic ellipsis as an auth_id placeholder.
		got := expandAPIPath(c, strings.ReplaceAll(path, "MA…", c.AuthID))

		if strings.Count(got, "/Account/") > 1 {
			t.Errorf("example %q expands to a doubled Account segment: %s", path, got)
		}
		if !strings.Contains(got, "/v1/") {
			t.Errorf("example %q expands without a /v1/ prefix: %s", path, got)
		}
	}
}

// The root help lists credential precedence; it must match config.Resolve.
// Env vars were made to beat a stored profile in v0.3.0 and this text kept
// claiming the opposite for three releases.
func TestRootHelp_credentialPrecedenceMatchesResolveOrder(t *testing.T) {
	long := rootCmd.Long
	envAt := strings.Index(long, "PLIVO_AUTH_ID")
	profAt := strings.Index(long, "active profile")
	if envAt < 0 || profAt < 0 {
		t.Fatal("root help no longer lists both env vars and the active profile")
	}
	if envAt > profAt {
		t.Error("root help lists the active profile above the env vars, but config.Resolve checks env FIRST")
	}
}
