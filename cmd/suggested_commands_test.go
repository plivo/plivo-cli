package cmd

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// helpTexts gathers every string a user can read out of the command tree.
func helpTexts(c *cobra.Command, out *[]string) {
	*out = append(*out, c.Short, c.Long, c.Example, c.Use)
	for _, sub := range c.Commands() {
		helpTexts(sub, out)
	}
}

// Every `plivo …` invocation quoted in help text must resolve to a real
// command. Three bugs this release were text telling users to run something
// that could not work — a doubled api path, a backwards precedence list, and
// an error pointing at `plivo contacto login`, which never existed in any
// build. This makes that class of bug fail CI instead of reaching a user.
func TestSuggestedCommands_allResolve(t *testing.T) {
	var texts []string
	helpTexts(rootCmd, &texts)

	// Both quoting styles are used in help text; a suggestion is just as
	// broken in single quotes as in backticks.
	quoted := regexp.MustCompile("[`']plivo ([a-z][a-z0-9 _-]*)[`']")
	seen := map[string]bool{}

	for _, txt := range texts {
		for _, m := range quoted.FindAllStringSubmatch(txt, -1) {
			path := strings.TrimSpace(m[1])
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true

			cur := rootCmd
			for _, w := range strings.Fields(path) {
				if strings.HasPrefix(w, "-") {
					break
				}
				found, _, err := cur.Find([]string{w})
				if err != nil || found == cur {
					t.Errorf("help text suggests `plivo %s` but %q is not a command", path, w)
					break
				}
				cur = found
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("no quoted `plivo …` suggestions found — extraction likely broke")
	}
	t.Logf("checked %d distinct suggested invocations", len(seen))
}
