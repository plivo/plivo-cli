package cmd

import (
	"net/http"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/spf13/cobra"
)

// walkOwnCommands visits every command below root except cobra's own help
// and completion, with its path.
func walkOwnCommands(fn func(c *cobra.Command, path []string)) {
	var walk func(c *cobra.Command, path []string)
	walk = func(c *cobra.Command, path []string) {
		for _, child := range c.Commands() {
			if child.Name() == "help" || child.Name() == "completion" {
				continue
			}
			childPath := append(append([]string(nil), path...), child.Name())
			fn(child, childPath)
			walk(child, childPath)
		}
	}
	walk(rootCmd, nil)
}

// A command whose Use names no arguments refuses a stray one with BAD_INPUT
// saying so, before any request, instead of ignoring it.
func TestLeaves_rejectStrayArguments(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startCapturingHTTPServer(t, http.StatusOK, `{}`)
	clientForTest = &api.Client{BaseURL: srv.URL, BuddyBaseURL: srv.URL, AuthID: "MAFAKEFORTEST", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })

	n := 0
	walkOwnCommands(func(c *cobra.Command, path []string) {
		if c.HasSubCommands() || !c.Runnable() || len(strings.Fields(c.Use)) > 1 {
			return
		}
		n++
		t.Run(strings.Join(path, "_"), func(t *testing.T) {
			err, _, _ := execCmd(t, append(append([]string(nil), path...), "--dry-run", "stray-token")...)
			want := "`plivo " + strings.Join(path, " ") + "` takes no arguments; got \"stray-token\""
			e, ok := err.(*clierr.Error)
			if !ok || e.Code != clierr.CodeBadInput || e.Message != want || e.Hint == "" {
				t.Errorf("want BAD_INPUT %q with a hint, got %#v", want, err)
			}
		})
	})
	if n < 60 {
		t.Errorf("checked %d argument-less commands, expected 60 or more", n)
	}
	if got := hits(); len(got) != 0 {
		t.Errorf("stray arguments still sent requests: %v", got)
	}
}
