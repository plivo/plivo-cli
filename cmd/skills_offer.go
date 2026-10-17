package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
)

// codingAgent is an AI coding agent the CLI can run inside, and where that
// agent loads skills from. Adding a row is all it takes to offer the skills
// to another agent after login.
type codingAgent struct {
	name    string // shown in the offer, and recorded once it is made
	env     string // NAME=value, or a NAME set to anything, in the agent's shells
	dir     string // its user skills directory, under the home directory
	install string // the command that installs every skill into dir
}

// codingAgents is checked in order; the first one detected is offered.
var codingAgents = []codingAgent{
	{name: "Claude Code", env: "CLAUDECODE=1", dir: filepath.Join(".claude", "skills"), install: "plivo skill install all"},
}

// detectAgent returns the coding agent this process runs inside, if any.
func detectAgent() (codingAgent, bool) {
	for _, a := range codingAgents {
		name, value, exact := strings.Cut(a.env, "=")
		if v := os.Getenv(name); v != "" && (!exact || v == value) {
			return a, true
		}
	}
	return codingAgent{}, false
}

// askable reports whether a question can be put to the user: stdin, stdout and
// stderr, where the question goes, are a terminal, and reading it won't stop
// this process (`plivo login &`). Tests stand in a terminal.
var askable = func() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		if !isTerminal(int(f.Fd())) {
			return false
		}
	}
	return inForeground(os.Stdin)
}

// offerSkills runs after a successful login, and nowhere else. Inside a coding
// agent whose skills folder lacks current copies of the Plivo skills, it asks
// on a terminal whether to install them, or without one prints the command
// that does; --yes installs them without asking, and --quiet skips the offer.
// Each agent is offered once per machine whatever the answer, and nothing here
// can fail the login: stderr only, and a failed install warns.
func offerSkills() {
	a, ok := detectAgent()
	home, err := os.UserHomeDir()
	if !ok || err != nil || dryRunFlag || quietFlag {
		return
	}
	root := filepath.Join(home, a.dir)
	cfg, err := config.Load()
	if err != nil || slices.Contains(cfg.Skills.Offered, a.name) || skillsCurrentIn(root) {
		return
	}
	// Recorded before asking, so a question left unanswered isn't asked again.
	cfg.Skills.Offered = append(cfg.Skills.Offered, a.name)
	if config.Save(cfg) != nil || (!yesFlag && !askToInstall(a)) {
		return
	}
	if err := installSkillsIn(bundledSkills, root); err != nil {
		e := clierr.Wrap(err)
		retry := a.install
		if e.Code == clierr.CodeDestructiveRefused {
			retry += " --force" // a copy holding someone's edits was kept
		}
		fmt.Fprintf(os.Stderr, "Warning: not every skill was installed: %s\n", e.Message)
		if e.Hint != "" {
			fmt.Fprintln(os.Stderr, e.Hint)
		}
		fmt.Fprintf(os.Stderr, "Retry with: %s\n", retry)
	}
}

// askToInstall puts the offer to the user and reports a yes. Without a
// terminal to ask on, or once stdin has closed, it prints the install command
// instead.
func askToInstall(a codingAgent) bool {
	if askable() {
		lines := stdinLines()
		// A line typed before the question is not an answer to it.
		if drainLines(lines) {
			fmt.Fprintf(os.Stderr, "Install the Plivo skills for %s? (Y/n) ", a.name)
			if answer, ok := <-lines; ok {
				answer = strings.ToLower(strings.TrimSpace(answer))
				return answer == "" || answer == "y" || answer == "yes"
			}
			fmt.Fprintln(os.Stderr) // stdin closed under the question
		}
	}
	fmt.Fprintf(os.Stderr, "Install the Plivo skills for %s with: %s\n", a.name, a.install)
	return false
}

// drainLines drops the lines already waiting in lines, without blocking, and
// reports whether lines is still open.
func drainLines(lines <-chan string) bool {
	for {
		select {
		case _, ok := <-lines:
			if !ok {
				return false
			}
		default:
			return true
		}
	}
}

// skillsCurrentIn reports whether every bundled skill is installed in root at
// the version this binary bundles.
func skillsCurrentIn(root string) bool {
	for _, s := range bundledSkills {
		if skillStateAt(s, filepath.Join(root, s.dirName, skillFileName)) != skillStateCurrent {
			return false
		}
	}
	return true
}
