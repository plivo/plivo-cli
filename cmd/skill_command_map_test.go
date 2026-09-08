package cmd

import (
	"regexp"
	"strings"
	"testing"

	cliskill "github.com/plivo/plivo-cli/cli-skill"
)

// mapBlock pulls the fenced block under "## Top-level command map".
var mapBlock = regexp.MustCompile("(?s)## Top-level command map\\s*\\n```[a-z]*\\n(.*?)\\n```")

// skillCommandMapEntries returns the command name at the head of each line of
// the skill's top-level command map.
func skillCommandMapEntries(md string) map[string]bool {
	m := mapBlock.FindStringSubmatch(md)
	if m == nil {
		return nil
	}
	out := map[string]bool{}
	for _, ln := range strings.Split(m[1], "\n") {
		f := strings.Fields(ln)
		if len(f) > 0 {
			out[f[0]] = true
		}
	}
	return out
}

// TestSkillCommandMap_matchesRealTree keeps the skill's command map honest.
//
// The map drifted badly before this existed: it listed `agent` as "coming
// soon — no subcommands yet" long after the group shipped nine working
// subcommands, and omitted `config`, `docs`, and `skill` entirely — including
// `plivo skill`, which is how the skill gets installed. A hand-maintained
// index of a generated tree only stays correct if something checks it.
//
// `completion` and `help` are Cobra built-ins and are deliberately absent.
func TestSkillCommandMap_matchesRealTree(t *testing.T) {
	builtin := map[string]bool{"completion": true, "help": true}

	entries := skillCommandMapEntries(cliskill.SkillMD)
	if len(entries) == 0 {
		t.Fatal("found no 'Top-level command map' block in the CLI skill; either " +
			"the heading moved or this test's regex is out of sync — check both " +
			"before assuming the map is fine")
	}

	var real []string
	for _, c := range rootCmd.Commands() {
		if c.Hidden || builtin[c.Name()] {
			continue
		}
		real = append(real, c.Name())
	}
	if len(real) < 10 {
		t.Fatalf("only found %d top-level commands; the tree lookup is wrong", len(real))
	}

	for _, name := range real {
		if !entries[name] {
			t.Errorf("command %q exists but is missing from the skill's command map", name)
		}
		delete(entries, name)
	}
	for name := range entries {
		if builtin[name] {
			continue
		}
		t.Errorf("skill's command map lists %q, which is not a top-level command", name)
	}
}
