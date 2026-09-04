package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cliskill "github.com/plivo/plivo-cli/cli-skill"
)

func TestResolveSkillDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows

	t.Run("default lands under ~/.claude/skills/plivo-cli", func(t *testing.T) {
		got, err := resolveSkillDir("", "plivo-cli")
		if err != nil {
			t.Fatalf("resolveSkillDir(\"\"): %v", err)
		}
		want := filepath.Join(home, ".claude", "skills", "plivo-cli")
		if got != want {
			t.Errorf("default dir = %q, want %q", got, want)
		}
	})

	t.Run("absolute override is returned as-is", func(t *testing.T) {
		override := filepath.Join(t.TempDir(), "agent", "plivo-cli")
		got, err := resolveSkillDir(override, "plivo-cli")
		if err != nil {
			t.Fatalf("resolveSkillDir(%q): %v", override, err)
		}
		if got != override {
			t.Errorf("override dir = %q, want %q", got, override)
		}
	})

	t.Run("tilde override expands to home", func(t *testing.T) {
		got, err := resolveSkillDir("~/agent/plivo-cli", "plivo-cli")
		if err != nil {
			t.Fatalf("resolveSkillDir(tilde): %v", err)
		}
		want := filepath.Join(home, "agent", "plivo-cli")
		if got != want {
			t.Errorf("tilde dir = %q, want %q", got, want)
		}
	})
}

// TestSkillInstall_writesFile drives runSkillInstall against a temp --dir and
// asserts the embedded skill lands at <dir>/SKILL.md byte-for-byte. No network.
func TestSkillInstall_writesFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "plivo-cli")

	skillDir = dir
	skillPrint = false
	dryRunFlag = false
	t.Cleanup(func() { skillDir = ""; skillPrint = false; dryRunFlag = false })

	if err := runSkillInstall(skillInstallCmd, nil); err != nil {
		t.Fatalf("runSkillInstall: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, skillFileName))
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if string(got) != cliskill.SkillMD {
		t.Errorf("installed skill differs from embedded SkillMD (%d vs %d bytes)", len(got), len(cliskill.SkillMD))
	}
}

// TestSkillInstall_dryRunWritesNothing ensures --dry-run reports but never
// touches disk.
func TestSkillInstall_dryRunWritesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plivo-cli")

	skillDir = dir
	skillPrint = false
	dryRunFlag = true
	t.Cleanup(func() { skillDir = ""; skillPrint = false; dryRunFlag = false })

	if err := runSkillInstall(skillInstallCmd, nil); err != nil {
		t.Fatalf("runSkillInstall (dry-run): %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, skillFileName)); !os.IsNotExist(err) {
		t.Errorf("dry-run wrote a file (stat err = %v), want it absent", err)
	}
}

// TestSkillInstall_printToStdout verifies --print emits the embedded skill and
// installs nothing.
func TestSkillInstall_printToStdout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plivo-cli")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w

	skillDir = dir
	skillPrint = true
	dryRunFlag = false
	t.Cleanup(func() { skillDir = ""; skillPrint = false; dryRunFlag = false })

	runErr := runSkillInstall(skillInstallCmd, nil)
	_ = w.Close()
	os.Stdout = orig
	if runErr != nil {
		t.Fatalf("runSkillInstall (--print): %v", runErr)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != cliskill.SkillMD {
		t.Errorf("--print output differs from embedded SkillMD (%d vs %d bytes)", len(out), len(cliskill.SkillMD))
	}
	// --print must not install.
	if _, err := os.Stat(filepath.Join(dir, skillFileName)); !os.IsNotExist(err) {
		t.Errorf("--print wrote a file (stat err = %v), want it absent", err)
	}
}

// The CX agents skill is embedded but deliberately NOT listed: the code stays
// in the repo while the feature is not live. This guards that decision, so
// re-listing it has to be an intentional edit that updates this test too,
// rather than a silent reappearance in a user-facing list.
func TestSkillInstall_cxAgentsSkillIsNotOffered(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Cleanup(func() { skillDir = ""; skillPrint = false; dryRunFlag = false })

	for _, s := range bundledSkills {
		if s.selector == "agents" || s.dirName == "plivo-cx-agents" {
			t.Fatalf("the CX agents skill is listed again (%q -> %q)", s.selector, s.dirName)
		}
	}

	if err := runSkillInstall(nil, []string{"agents"}); err == nil {
		t.Error("`skill install agents` succeeded; it must not install while unlisted")
	}

	// `all` fans out over bundledSkills, so it must not reach the CX skill either.
	if err := runSkillInstall(nil, []string{"all"}); err != nil {
		t.Fatalf("install all: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "plivo-cx-agents", skillFileName)); err == nil {
		t.Error("`skill install all` wrote the CX agents skill despite it being unlisted")
	}

	// Match the selector and install dir, not the bare word "agents" — that
	// appears legitimately in "LLM coding agents".
	for _, txt := range []string{skillInstallCmd.Use, skillInstallCmd.Long, skillInstallCmd.Example} {
		if strings.Contains(txt, "install agents") || strings.Contains(txt, "plivo-cx-agents") {
			t.Errorf("help text still advertises the CX agents skill: %q", txt)
		}
	}
}

// --dir names a single destination, so "all" would write both skills over each
// other. That must be an error, not a silent overwrite.
func TestSkillInstall_allRejectsDirAndPrint(t *testing.T) {
	t.Cleanup(func() { skillDir = ""; skillPrint = false; dryRunFlag = false })

	skillDir = t.TempDir()
	if err := runSkillInstall(nil, []string{"all"}); err == nil {
		t.Error("all + --dir must error rather than overwrite one skill with the other")
	}
	skillDir = ""

	skillPrint = true
	if err := runSkillInstall(nil, []string{"all"}); err == nil {
		t.Error("all + --print must error rather than concatenate two skills")
	}
}

func TestSkillInstall_unknownSelectorIsRejected(t *testing.T) {
	t.Cleanup(func() { skillDir = ""; skillPrint = false; dryRunFlag = false })
	err := runSkillInstall(nil, []string{"nope"})
	if err == nil {
		t.Fatal("unknown selector must error")
	}
	if !strings.Contains(err.Error(), "cli") {
		t.Errorf("error should list the available skills, got: %v", err)
	}
}
