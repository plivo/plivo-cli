package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/config"
	"golang.org/x/term"
)

// ctrlD, as the answer, closes stdin under the question.
const ctrlD = "\x04"

// offerEnv runs the test in the first coding agent's shell (CLAUDECODE=1, the
// other agents' variables cleared) with HOME on a temp dir, on a terminal when
// tty.
func offerEnv(t *testing.T, tty bool) (home string) {
	t.Helper()
	home = setEmptyHome(t)
	for _, a := range codingAgents {
		name, _, _ := strings.Cut(a.env, "=")
		t.Setenv(name, "")
	}
	t.Setenv("CLAUDECODE", "1")
	prevAskable, prevLines := askable, stdinLines
	askable = func() bool { return tty }
	t.Cleanup(func() { askable, stdinLines = prevAskable, prevLines })
	return home
}

// offer runs offerSkills with stray lines typed before the question and answer
// typed once the question is on stderr, and returns what it printed.
func offer(t *testing.T, answer string, stray ...string) (stdout, stderr string) {
	t.Helper()
	lines := make(chan string, len(stray))
	for _, s := range stray {
		lines <- s
	}
	stdinLines = func() <-chan string { return lines }
	origOut, origErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()
	var printed []byte
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf, asked := make([]byte, 4096), false
		for {
			n, err := rErr.Read(buf)
			printed = append(printed, buf[:n]...)
			if !asked && strings.Contains(string(printed), "(Y/n) ") {
				asked = true
				if answer == ctrlD {
					close(lines)
				} else {
					lines <- answer
				}
			}
			if err != nil {
				return
			}
		}
	}()
	offerSkills()
	_ = wOut.Close()
	_ = wErr.Close()
	<-done
	out, _ := io.ReadAll(rOut)
	return string(out), string(printed)
}

func offeredTo(t *testing.T) []string {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Skills.Offered
}

// A row's env is NAME=value, or NAME set to anything; the first match wins.
func TestDetectAgent_envForms(t *testing.T) {
	prev := codingAgents
	t.Cleanup(func() { codingAgents = prev })
	codingAgents = []codingAgent{{name: "exact", env: "PLIVO_TEST_AGENT_A=1"}, {name: "any", env: "PLIVO_TEST_AGENT_B"}}
	for _, c := range []struct{ a, b, want string }{{"1", "x", "exact"}, {"2", "", ""}, {"2", "x", "any"}, {"", "", ""}} {
		t.Setenv("PLIVO_TEST_AGENT_A", c.a)
		t.Setenv("PLIVO_TEST_AGENT_B", c.b)
		if a, _ := detectAgent(); a.name != c.want {
			t.Errorf("A=%q B=%q: detected %q, want %q", c.a, c.b, a.name, c.want)
		}
	}
}

// The question goes to stderr, so it is asked only when stderr is a terminal.
func TestAskable_needsStderrOnATerminal(t *testing.T) {
	t.Cleanup(func() { isTerminal = term.IsTerminal })
	isTerminal = func(fd int) bool { return fd != int(os.Stderr.Fd()) }
	if askable() {
		t.Error("askable with stderr redirected")
	}
}

func TestOfferSkills(t *testing.T) {
	const ask = "Install the Plivo skills for Claude Code? (Y/n) "
	const hint = "Install the Plivo skills for Claude Code with: plivo skill install all\n"
	for _, c := range []struct {
		name, answer string
		tty          bool
		stray        []string
		stderr       string // all of stderr, or how it starts once skills are installed
		installed    bool
	}{
		{name: "no", tty: true, answer: "n", stderr: ask},
		{name: "Enter is yes", tty: true, answer: "", stderr: ask + "Installed skill: ", installed: true},
		{name: "lines typed before the question", tty: true, stray: []string{"", "y"}, answer: "n", stderr: ask},
		{name: "stdin closes under the question", tty: true, answer: ctrlD, stderr: ask + "\n" + hint},
		{name: "no terminal", answer: "y", stderr: hint},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := offerEnv(t, c.tty)
			stdout, stderr := offer(t, c.answer, c.stray...)
			if stdout != "" || !strings.HasPrefix(stderr, c.stderr) || (!c.installed && stderr != c.stderr) {
				t.Errorf("stdout %q, stderr %q; want stderr %q", stdout, stderr, c.stderr)
			}
			_, err := os.Stat(filepath.Join(home, ".claude", "skills", bundledSkills[0].dirName, skillFileName))
			if (err == nil) != c.installed || len(offeredTo(t)) != 1 {
				t.Errorf("installed: %v, want %v; offered to %v", err == nil, c.installed, offeredTo(t))
			}
		})
	}
}

func TestOfferSkills_asksOnce(t *testing.T) {
	offerEnv(t, true)
	offer(t, "n")
	if _, stderr := offer(t, "y"); stderr != "" {
		t.Errorf("asked again: %q", stderr)
	}
}

func TestOfferSkills_yesInstallsEverySkillAndAFailureOnlyWarns(t *testing.T) {
	home := offerEnv(t, true)
	edited := writeSkillFile(t, home, bundledSkills[0].dirName, "my own notes\n")
	stdout, stderr := offer(t, "y")
	for _, s := range bundledSkills[1:] {
		if !fileIs(t, filepath.Join(home, ".claude", "skills", s.dirName, skillFileName), s.content) {
			t.Errorf("%s not installed", s.selector)
		}
	}
	if !fileIs(t, edited, "my own notes\n") || !strings.Contains(stderr, "Warning: not every skill was installed") || stdout != "" {
		t.Errorf("want the edited copy kept, with a warning on stderr only; stdout %q, stderr %q", stdout, stderr)
	}
}

func TestOfferSkills_skipsOutsideAnAgentOrWithCurrentCopies(t *testing.T) {
	home := offerEnv(t, true)
	t.Setenv("CLAUDECODE", "")
	if _, stderr := offer(t, "y"); stderr != "" {
		t.Errorf("offered outside an agent: %q", stderr)
	}
	t.Setenv("CLAUDECODE", "1")
	for _, s := range bundledSkills {
		writeSkillFile(t, home, s.dirName, s.content)
	}
	if _, stderr := offer(t, "y"); stderr != "" || len(offeredTo(t)) != 0 {
		t.Errorf("offered with current copies: %q, recorded %v", stderr, offeredTo(t))
	}
}
