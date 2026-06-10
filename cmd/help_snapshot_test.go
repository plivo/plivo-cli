//go:build !internal

// Help snapshots validate the public command tree.

package cmd

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// updateGoldens controls whether help-snapshot tests rewrite their golden
// files instead of comparing. Invoke with: `go test ./cmd/ -update`.
var updateGoldens = flag.Bool("update", false, "regenerate help-snapshot golden files")

// snapshotDir is the testdata path where golden files live.
const snapshotDir = "testdata/help"

// collectCmdPaths walks rootCmd and returns the full path (in command-name
// segments) for every user-facing command. Skips cobra's auto-added
// `help` and `completion` commands.
func collectCmdPaths(root *cobra.Command) [][]string {
	var out [][]string
	out = append(out, []string{root.Name()}) // plivo itself

	var visit func(c *cobra.Command, path []string)
	visit = func(c *cobra.Command, path []string) {
		if c.Name() == "help" || c.Name() == "completion" {
			return
		}
		full := append(append([]string(nil), path...), c.Name())
		out = append(out, full)
		for _, child := range c.Commands() {
			visit(child, full)
		}
	}
	for _, child := range root.Commands() {
		visit(child, []string{root.Name()})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i], "_") < strings.Join(out[j], "_")
	})
	return out
}

// captureHelp returns the rendered --help text for the command at the given
// path. Uses cobra.Command.Help() rather than rootCmd.Execute([..., "--help"])
// so cobra's internal flag/args state doesn't get polluted between tests.
//
// Calls InitDefaultHelpFlag() on every command in the tree first so the
// `-h, --help` line is present in the output deterministically — without this,
// help rendering differs depending on whether any prior test went through
// rootCmd.Execute() (which adds --help lazily).
func captureHelp(t *testing.T, path []string) string {
	t.Helper()
	ensureHelpFlagOnTree(rootCmd)

	var cmd *cobra.Command
	if len(path) <= 1 {
		// path == ["plivo"] — render the root help.
		cmd = rootCmd
	} else {
		found, _, err := rootCmd.Find(path[1:])
		if err != nil || found == nil {
			t.Fatalf("can't find command for path %v: %v", path, err)
		}
		cmd = found
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	defer func() {
		cmd.SetOut(nil)
		cmd.SetErr(nil)
	}()

	if err := cmd.Help(); err != nil {
		t.Fatalf("Help() failed for %v: %v", path, err)
	}
	return buf.String()
}

// ensureHelpFlagOnTree walks every command and forces the default -h/--help
// flag into existence. Idempotent.
func ensureHelpFlagOnTree(c *cobra.Command) {
	c.InitDefaultHelpFlag()
	for _, child := range c.Commands() {
		ensureHelpFlagOnTree(child)
	}
}

// TestMain warms up cobra's lazily-added commands (the `help` subcommand and
// the `completion` subcommand) so they appear consistently in every snapshot
// regardless of test ordering. Without this, the snapshot for `plivo --help`
// includes/excludes these commands depending on whether some earlier test in
// the same package went through rootCmd.Execute().
func TestMain(m *testing.M) {
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	os.Exit(m.Run())
}

// goldenPath returns the testdata file path for a given command path slice.
func goldenPath(path []string) string {
	return filepath.Join(snapshotDir, strings.Join(path, "_")+".txt")
}

func TestHelpSnapshots(t *testing.T) {
	paths := collectCmdPaths(rootCmd)
	if len(paths) < 50 {
		t.Fatalf("only %d commands discovered — expected 100+; did the command tree shrink?", len(paths))
	}

	for _, p := range paths {
		p := p
		t.Run(strings.Join(p, "_"), func(t *testing.T) {
			got := captureHelp(t, p)
			file := goldenPath(p)

			if *updateGoldens {
				_ = os.MkdirAll(filepath.Dir(file), 0755)
				if err := os.WriteFile(file, []byte(got), 0644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}

			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("golden missing for %v: %s\nRun: go test ./cmd/ -update", p, file)
			}
			if string(want) != got {
				t.Errorf("--help drift for plivo %s\n  golden: %s\n  run `go test ./cmd/ -update` to refresh\n\n--- expected ---\n%s\n+++ got +++\n%s",
					strings.Join(p[1:], " "), file, string(want), got)
			}
		})
	}
}

// TestHelpSnapshots_everyCommandHasUsageLine is a structural check separate
// from the snapshot diff — it catches commands whose --help comes back empty
// (which would happen if SetOut isn't wired or the command is misregistered).
func TestHelpSnapshots_everyCommandHasUsageLine(t *testing.T) {
	for _, p := range collectCmdPaths(rootCmd) {
		t.Run(strings.Join(p, "_"), func(t *testing.T) {
			got := captureHelp(t, p)
			if !strings.Contains(got, "Usage:") {
				t.Errorf("plivo %s --help missing 'Usage:' line.\nFull output:\n%s", strings.Join(p[1:], " "), got)
			}
		})
	}
}
