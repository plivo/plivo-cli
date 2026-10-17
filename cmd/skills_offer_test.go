package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/config"
)

// offerEnv puts the test inside Claude Code with HOME on a temp dir, on a
// terminal when tty, where answer is typed.
func offerEnv(t *testing.T, tty bool, answer string) (home string) {
	t.Helper()
	home = setEmptyHome(t)
	t.Setenv("CLAUDECODE", "1")
	prevAskable, prevLines := askable, stdinLines
	askable = func() bool { return tty }
	stdinLines = func() <-chan string { return lineFeed(strings.NewReader(answer)) }
	t.Cleanup(func() { askable, stdinLines = prevAskable, prevLines })
	return home
}

func offer(t *testing.T) (stdout, stderr string) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	offerSkills()
	os.Stdout, os.Stderr = origOut, origErr
	_ = wOut.Close()
	_ = wErr.Close()
	out, _ := io.ReadAll(rOut)
	errOut, _ := io.ReadAll(rErr)
	return string(out), string(errOut)
}

func offeredTo(t *testing.T) []string {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Skills.Offered
}

func TestOfferSkills_asksOnceOnATerminal(t *testing.T) {
	home := offerEnv(t, true, "n\n")
	if stdout, stderr := offer(t); stdout != "" || stderr != "Install the Plivo skills for Claude Code? (Y/n) " {
		t.Errorf("stdout %q, stderr %q; want only the question, on stderr", stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) || len(offeredTo(t)) != 1 {
		t.Errorf("after no: installed (stat err %v), or not recorded: %v", err, offeredTo(t))
	}
	if _, stderr := offer(t); stderr != "" {
		t.Errorf("asked again: %q", stderr)
	}
}

func TestOfferSkills_yesInstallsEverySkillAndAFailureOnlyWarns(t *testing.T) {
	home := offerEnv(t, true, "\n")
	edited := writeSkillFile(t, home, bundledSkills[0].dirName, "my own notes\n")
	stdout, stderr := offer(t)
	for _, s := range bundledSkills[1:] {
		if !fileIs(t, filepath.Join(home, ".claude", "skills", s.dirName, skillFileName), s.content) {
			t.Errorf("%s not installed", s.selector)
		}
	}
	if !fileIs(t, edited, "my own notes\n") || !strings.Contains(stderr, "Warning: not every skill was installed") || stdout != "" {
		t.Errorf("want the edited copy kept, with a warning on stderr only; stdout %q, stderr %q", stdout, stderr)
	}
}

func TestOfferSkills_withoutATerminalPrintsTheCommand(t *testing.T) {
	home := offerEnv(t, false, "y\n")
	if stdout, stderr := offer(t); stdout != "" || stderr != "Install the Plivo skills for Claude Code with: plivo skill install all\n" {
		t.Errorf("stdout %q, stderr %q", stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) || len(offeredTo(t)) != 1 {
		t.Errorf("installed without asking (stat err %v), or not recorded: %v", err, offeredTo(t))
	}
}

func TestOfferSkills_skipsOutsideAnAgentOrWithCurrentCopies(t *testing.T) {
	home := offerEnv(t, true, "y\n")
	t.Setenv("CLAUDECODE", "")
	if _, stderr := offer(t); stderr != "" {
		t.Errorf("offered outside an agent: %q", stderr)
	}
	t.Setenv("CLAUDECODE", "1")
	for _, s := range bundledSkills {
		writeSkillFile(t, home, s.dirName, s.content)
	}
	if _, stderr := offer(t); stderr != "" || len(offeredTo(t)) != 0 {
		t.Errorf("offered with current copies: %q, recorded %v", stderr, offeredTo(t))
	}
}
