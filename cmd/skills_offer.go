package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

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

// askable reports whether a question can be put to the user: stdin and stdout
// are a terminal, and reading it won't stop this process (`plivo login &`).
// Tests stand in a terminal.
var askable = func() bool {
	return isTerminal(int(os.Stdin.Fd())) && isTerminal(int(os.Stdout.Fd())) && inForeground(os.Stdin)
}

// offerSkills runs after a successful login, and nowhere else. Inside a coding
// agent whose skills folder lacks current copies of the Plivo skills, it asks
// on a terminal whether to install them, or without one prints the command
// that does. Each agent is offered once per machine whatever the answer, and
// nothing here can fail the login: stderr only, and a failed install warns.
func offerSkills() {
	a, ok := detectAgent()
	home, err := os.UserHomeDir()
	if !ok || err != nil || dryRunFlag {
		return
	}
	root := filepath.Join(home, a.dir)
	cfg, err := config.Load()
	if err != nil || slices.Contains(cfg.Skills.Offered, a.name) || skillsCurrentIn(root) {
		return
	}
	// Recorded before asking, so a question left unanswered isn't asked again.
	cfg.Skills.Offered = append(cfg.Skills.Offered, a.name)
	if config.Save(cfg) != nil {
		return
	}
	if !askable() {
		fmt.Fprintf(os.Stderr, "Install the Plivo skills for %s with: %s\n", a.name, a.install)
		return
	}
	fmt.Fprintf(os.Stderr, "Install the Plivo skills for %s? (Y/n) ", a.name)
	answer, ok := <-stdinLines()
	if answer = strings.ToLower(strings.TrimSpace(answer)); !ok || (answer != "" && answer != "y" && answer != "yes") {
		return
	}
	if err := installSkillsIn(bundledSkills, root); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: not every skill was installed: %v\nRetry with: %s\n", err, a.install)
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
