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

// offerEnv runs the test in a CLAUDECODE=1 shell and no other agent's, with
// HOME on a temp dir, on a terminal when tty.
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

// offer runs offerSkills, typing stray before the question and answer after it.
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

// offeredTo lists the agents asked or given the command.
func offeredTo(t *testing.T) []string {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return append(cfg.Skills.Offered, cfg.Skills.Hinted...)
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
		name, answer, claudecode string // claudecode: CLAUDECODE, when not 1
		tty                      bool
		flag                     *bool
		stray                    []string
		stderr                   string // all of stderr, or how it starts once skills are installed
		installed, skipped       bool   // skipped: not offered, so nothing recorded either
	}{
		{name: "no", tty: true, answer: "n", stderr: ask},
		{name: "Enter is yes", tty: true, answer: "", stderr: ask + "Installed skill: ", installed: true},
		{name: "lines typed before the question", tty: true, stray: []string{"", "y"}, answer: "n", stderr: ask},
		{name: "stdin closes under the question", tty: true, answer: ctrlD, stderr: ask + "\n" + hint},
		{name: "no terminal", answer: "y", stderr: hint},
		{name: "--yes installs without asking", flag: &yesFlag, stderr: "Installed skill: ", installed: true},
		{name: "--quiet", tty: true, flag: &quietFlag, answer: "y", skipped: true},
		{name: "--dry-run", tty: true, flag: &dryRunFlag, answer: "y", skipped: true},
		{name: "CLAUDECODE=0 is no agent", tty: true, claudecode: "0", answer: "y", skipped: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := offerEnv(t, c.tty)
			if c.claudecode != "" {
				t.Setenv("CLAUDECODE", c.claudecode)
			}
			if c.flag != nil {
				*c.flag = true
				t.Cleanup(func() { *c.flag = false })
			}
			stdout, stderr := offer(t, c.answer, c.stray...)
			if stdout != "" || !strings.HasPrefix(stderr, c.stderr) || (!c.installed && stderr != c.stderr) {
				t.Errorf("stdout %q, stderr %q; want stderr %q", stdout, stderr, c.stderr)
			}
			_, err := os.Stat(filepath.Join(home, ".claude", "skills", bundledSkills[0].dirName, skillFileName))
			if (err == nil) != c.installed || (len(offeredTo(t)) == 1) == c.skipped {
				t.Errorf("installed: %v, want %v; offered to %v", err == nil, c.installed, offeredTo(t))
			}
		})
	}
}

func TestOfferSkills_aKeptCopyWarnsWithForce(t *testing.T) {
	home := offerEnv(t, true)
	edited := writeSkillFile(t, home, bundledSkills[0].dirName, "my own notes\n")
	stdout, stderr := offer(t, "y")
	if !fileIs(t, edited, "my own notes\n") || stdout != "" || !strings.Contains(stderr, "holds someone's edits") ||
		!strings.HasSuffix(stderr, "Retry with: plivo skill install all --force\n") {
		t.Errorf("want the edited copy kept, and a warning naming --force on stderr only; stdout %q, stderr %q", stdout, stderr)
	}
}

// A printed command is not the machine's one question: an agent with no
// terminal is given the command, once, and a later one on a terminal is still
// asked. Then each other agent is given its own command, once, which installs
// where it reads skills; current copies there skip it. A record v1.3.0 wrote,
// for a question or a command, counts as asked.
func TestOfferSkills_oncePerMachine(t *testing.T) {
	home := offerEnv(t, false)
	as := func(env string) { // the login runs in this agent alone
		for _, a := range codingAgents {
			name, _, _ := strings.Cut(a.env, "=")
			t.Setenv(name, "")
		}
		t.Setenv(env, "1")
	}
	for _, want := range []string{"Install the Plivo skills for Claude Code with: plivo skill install all\n", ""} {
		if _, stderr := offer(t, "y"); stderr != want {
			t.Fatalf("no terminal: %q, want %q", stderr, want)
		}
	}
	askable = func() bool { return true }
	as("CURSOR_AGENT")
	if _, stderr := offer(t, "n"); stderr != "Install the Plivo skills for Cursor? (Y/n) " {
		t.Fatalf("a later agent on a terminal: %q, want the question", stderr)
	}
	as("CODEX_THREAD_ID")
	_, stderr := offer(t, "y")
	args, ok := strings.CutPrefix(stderr, "Install the Plivo skills for Codex with: plivo ")
	if !ok {
		t.Fatalf("Codex was asked, or not given the command: %q", stderr)
	}
	if _, again := offer(t, "y"); again != "" {
		t.Errorf("Codex given the command again: %q", again)
	}
	if err, _, _ := execCmd(t, strings.Fields(args)...); err != nil || !skillsCurrentIn(filepath.Join(home, ".agents", "skills")) {
		t.Errorf("plivo %s did not install into ~/.agents/skills: %v", strings.TrimSpace(args), err)
	}
	writeProfile(t, home, "[skills]\noffered = [\"Cursor\"]\n")
	if _, stderr := offer(t, ""); stderr != "" {
		t.Errorf("Codex offered with current copies in ~/.agents/skills: %q", stderr)
	}
	as("CLAUDECODE")
	if _, stderr := offer(t, "y"); !strings.HasPrefix(stderr, "Install the Plivo skills for Claude Code with: ") {
		t.Errorf("under a v1.3.0 record: %q, want the command, not the question", stderr)
	}
}
