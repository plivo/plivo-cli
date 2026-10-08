package cmd

import (
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/spf13/cobra"
)

// Every group, root included, refuses a word that is none of its
// subcommands with BAD_INPUT, with or without --help or -h. With them it
// used to print the group's help and exit 0.
func TestUnknownSubcommand_badInputWithOrWithoutHelp(t *testing.T) {
	setFakeCreds(t)
	groups := [][]string{nil}
	walkOwnCommands(func(c *cobra.Command, path []string) {
		if c.HasSubCommands() {
			groups = append(groups, path)
		}
	})
	const word = "this-subcommand-does-not-exist"
	for _, group := range groups {
		want := `unknown command "` + word + `" for "` + strings.TrimSpace("plivo "+strings.Join(group, " ")) + `"`
		for _, flag := range []string{"", "--help", "-h"} {
			args := append(append([]string(nil), group...), word)
			if flag != "" {
				args = append(args, flag)
			}
			t.Run(strings.Join(args, "_"), func(t *testing.T) {
				err, stdout, _ := execCmd(t, args...)
				e, ok := err.(*clierr.Error)
				if !ok || e.Code != clierr.CodeBadInput || e.Message != want || !strings.Contains(e.Hint, "--help` to see its commands") {
					t.Fatalf("want BAD_INPUT %q with a hint, got %#v", want, err)
				}
				if e.ExitCode() != ExitUserError {
					t.Errorf("exit code %d, want %d", e.ExitCode(), ExitUserError)
				}
				if stdout != "" {
					t.Errorf("printed help anyway: %q", stdout)
				}
			})
		}
	}
}

// cobra's suggestions become the hint, on both paths.
func TestUnknownSubcommand_hintSuggestsTheCommand(t *testing.T) {
	setFakeCreds(t)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"sip", "trunks", "lst", "--help"}, "`plivo sip trunks list`"},
		{[]string{"sip", "trnks", "list"}, "Did you mean `plivo sip trunks`?"},
		{[]string{"nubmers", "-h"}, "Did you mean `plivo numbers`?"},
	}
	for _, tc := range cases {
		err, _, _ := execCmd(t, tc.args...)
		if e, ok := err.(*clierr.Error); !ok || !strings.Contains(e.Hint, tc.want) {
			t.Errorf("%v: want a hint with %q, got %#v", tc.args, tc.want, err)
		}
	}
}

// Only an unknown word changes: real commands with --help, cobra's help and
// completion commands, and shell completion requests answer as before.
func TestUnknownSubcommand_helpAndCompletionUnchanged(t *testing.T) {
	setFakeCreds(t)
	// Running __complete leaves cobra's hidden __complete command on root,
	// where later tree walks (the help snapshots) would find it.
	t.Cleanup(func() {
		for _, c := range rootCmd.Commands() {
			if strings.HasPrefix(c.Name(), "__complete") {
				rootCmd.RemoveCommand(c)
			}
		}
	})
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"sip", "trunks", "-h"}, "plivo sip trunks [command]"},
		{[]string{"sip", "trunks", "--help"}, "plivo sip trunks [command]"},
		{[]string{"sip", "trunks", "list", "--help"}, "plivo sip trunks list [flags]"},
		{[]string{"__complete", "sip", "tr"}, "trunks"},
		{[]string{"help", "sip", "trunks"}, "plivo sip trunks [command]"},
		// cobra binds the script's writer when it adds the command, before
		// this test captures stdout; smoke.sh checks the script itself.
		{[]string{"completion", "zsh"}, ""},
		{[]string{"completion", "--help"}, "plivo completion [command]"},
		{[]string{"help", "--help"}, "plivo help [command]"},
		{[]string{"help", "sip", "-h"}, "plivo help [command]"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			err, stdout, _ := execCmd(t, tc.args...)
			if err != nil {
				t.Fatalf("want success, got %v", err)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Errorf("stdout lacks %q:\n%s", tc.want, stdout)
			}
		})
	}
}

// --schema of a mistyped path is refused, not answered with the parent's.
func TestUnknownSubcommand_schemaOfAMistypedPath(t *testing.T) {
	setFakeCreds(t)
	err, stdout, _ := execCmd(t, "sip", "trnks", "list", "--schema")
	if e, ok := err.(*clierr.Error); !ok || e.Code != clierr.CodeBadInput || stdout != "" {
		t.Errorf("want BAD_INPUT and no schema, got %#v / %q", err, stdout)
	}
}
