package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	cliskill "github.com/plivo/plivo-cli/cli-skill"
	"github.com/plivo/plivo-cli/internal/clierr"
	voicexmlskill "github.com/plivo/plivo-cli/voice-xml-skill"
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

// Every bundled skill must install byte-identically to what is embedded. A
// broken //go:embed yields an empty string rather than a build failure, so
// this is the check that a skill actually shipped.
func TestSkillInstall_allBundledSkillsInstallByteIdentical(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Cleanup(func() { skillDir = ""; skillPrint = false; dryRunFlag = false })

	if err := runSkillInstall(nil, []string{"all"}); err != nil {
		t.Fatalf("install all: %v", err)
	}
	for _, sk := range bundledSkills {
		got, err := os.ReadFile(filepath.Join(home, ".claude", "skills", sk.dirName, skillFileName))
		if err != nil {
			t.Errorf("%s: not installed by `all`: %v", sk.selector, err)
			continue
		}
		if string(got) != sk.content {
			t.Errorf("%s: installed %d bytes, embedded %d — content differs", sk.selector, len(got), len(sk.content))
		}
		if !strings.HasPrefix(string(got), "---") {
			t.Errorf("%s: installed file lost its YAML frontmatter; agents parse that to discover the skill", sk.selector)
		}
	}
}

// v1.1.3 installed plivo-first-agent, whose flow now lives in
// plivo-audio-streaming. Two skills claiming the same request make an agent
// pick between them, so installing the replacement removes the old copy from
// the default skills root, and from nowhere else.
func TestSkillInstall_removesRetiredFirstAgentSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Cleanup(func() { skillDir = ""; skillPrint = false; dryRunFlag = false })

	old := writeRetiredSkill(t, filepath.Join(home, ".claude", "skills"))

	dryRunFlag = true
	if err := runSkillInstall(nil, []string{"audio-streaming"}); err != nil {
		t.Fatalf("dry-run install: %v", err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("--dry-run removed the retired skill: %v", err)
	}
	dryRunFlag = false

	// --dir names the skill's own folder: a sibling plivo-first-agent there, or
	// the copy in the default root, is not the install's to remove.
	other := t.TempDir()
	sibling := writeRetiredSkill(t, other)
	skillDir = filepath.Join(other, "plivo-audio-streaming")
	if err := runSkillInstall(nil, []string{"audio-streaming"}); err != nil {
		t.Fatalf("--dir install: %v", err)
	}
	for _, p := range []string{sibling, old} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("--dir install removed %s: %v", p, err)
		}
	}
	skillDir = ""

	if err := runSkillInstall(nil, []string{"audio-streaming"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("plivo-first-agent still installed after installing its replacement (stat err: %v)", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "plivo-audio-streaming", skillFileName)); err != nil {
		t.Errorf("replacement not installed: %v", err)
	}
}

// The retired folder may hold the user's own files. Only the SKILL.md that
// v1.1.3 wrote goes, and that is enough to stop agents loading the old skill.
func TestSkillInstall_keepsUserFilesInRetiredSkillFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Cleanup(func() { skillDir = ""; skillPrint = false; dryRunFlag = false })

	old := writeRetiredSkill(t, filepath.Join(home, ".claude", "skills"))
	notes := filepath.Join(old, "notes.md")
	if err := os.WriteFile(notes, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runSkillInstall(nil, []string{"audio-streaming"}); err != nil {
		t.Fatalf("install failed because the retired folder held another file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(old, skillFileName)); !os.IsNotExist(err) {
		t.Errorf("retired SKILL.md still there, so agents keep loading the old skill (stat err: %v)", err)
	}
	if _, err := os.Stat(notes); err != nil {
		t.Errorf("the user's own file was deleted: %v", err)
	}
}

func writeRetiredSkill(t *testing.T, root string) string {
	t.Helper()
	old := filepath.Join(root, "plivo-first-agent")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, skillFileName), []byte("---\nname: plivo-first-agent\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return old
}

func TestSkillList_reportsEverySkillAndItsState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Cleanup(func() { skillDir = ""; skillPrint = false; dryRunFlag = false; outputFormat = "" })

	outputFormat = "json"
	var buf bytes.Buffer

	// Nothing installed yet.
	rows := skillListRows(t)
	if len(rows) != len(bundledSkills) {
		t.Fatalf("listed %d skills, want %d", len(rows), len(bundledSkills))
	}
	for _, r := range rows {
		if r.State != skillStateAbsent {
			t.Errorf("%s: state %q before any install, want %q", r.Selector, r.State, skillStateAbsent)
		}
	}

	// Install one, then it reports installed.
	if err := runSkillInstall(nil, nil); err != nil {
		t.Fatalf("default install: %v", err)
	}
	for _, r := range skillListRows(t) {
		if r.Selector == bundledSkills[0].selector && r.State != skillStateCurrent {
			t.Errorf("after installing %s, state = %q, want %q", r.Selector, r.State, skillStateCurrent)
		}
	}

	// Tamper with it: that is the drift embedding creates, and the state that
	// tells a user to re-run install.
	p := filepath.Join(home, ".claude", "skills", bundledSkills[0].dirName, skillFileName)
	if err := os.WriteFile(p, []byte("--- \nstale content from an older binary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range skillListRows(t) {
		if r.Selector == bundledSkills[0].selector {
			found = true
			if r.State != skillStateStale {
				t.Errorf("a modified skill reports %q, want %q", r.State, skillStateStale)
			}
		}
	}
	if !found {
		t.Error("the tampered skill vanished from the listing")
	}
	_ = buf
}

// The CX agents skill must not appear in the listing either.
func TestSkillList_omitsTheUnlistedCXAgentsSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Cleanup(func() { outputFormat = "" })
	outputFormat = "json"

	for _, r := range skillListRows(t) {
		if r.Selector == "agents" || r.InstallsTo == "plivo-cx-agents" {
			t.Errorf("the CX agents skill appeared in `skill list`: %+v", r)
		}
	}
}

// skillListRows runs `skill list` in JSON mode and decodes the envelope.
func skillListRows(t *testing.T) []skillListEntry {
	t.Helper()
	prev := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := runSkillList(nil, nil)
	w.Close()
	os.Stdout = prev
	if runErr != nil {
		t.Fatalf("skill list: %v", runErr)
	}
	var env struct {
		Data []skillListEntry `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&env); err != nil {
		t.Fatalf("decode skill list envelope: %v", err)
	}
	return env.Data
}

// fakeRepo makes a temp git repository (a .git directory, or the .git file a
// linked worktree has) and runs the test from a folder nested inside it, with
// HOME on a separate temp dir. --project tests use it so an install can reach
// neither the real home nor this repository.
func fakeRepo(t *testing.T, gitFile bool) (root, home string) {
	t.Helper()
	root = t.TempDir()
	git := filepath.Join(root, ".git")
	var err error
	if gitFile {
		err = os.WriteFile(git, []byte("gitdir: /elsewhere/.git/worktrees/x\n"), 0o644)
	} else {
		err = os.Mkdir(git, 0o755)
	}
	if err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "src", "app")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return root, home
}

func projectSkillPath(root, dirName string) string {
	return filepath.Join(root, ".claude", "skills", dirName, skillFileName)
}

func TestFindGitRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	// A linked worktree or submodule has a .git FILE, and is the nearer root.
	inner := filepath.Join(root, "a")
	if err := os.WriteFile(filepath.Join(inner, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ start, want string }{
		{root, root},
		{deep, inner},
		{inner, inner},
	} {
		if got, ok := findGitRoot(tc.start); !ok || got != tc.want {
			t.Errorf("findGitRoot(%s) = %q, %v; want %q", tc.start, got, ok, tc.want)
		}
	}

	outside := t.TempDir()
	if got, ok := findGitRoot(outside); ok {
		t.Skipf("the temp dir is inside a git repository (%s); can't test the outside case here", got)
	}
}

func TestSkillInstall_projectInstallsAtTheRepoRoot(t *testing.T) {
	for _, gitFile := range []bool{false, true} {
		root, home := fakeRepo(t, gitFile)

		if err, _, stderr := execCmd(t, "skill", "install", "all", "--project"); err != nil {
			t.Fatalf("install all --project (git file: %v): %v\n%s", gitFile, err, stderr)
		}
		for _, sk := range bundledSkills {
			got, err := os.ReadFile(projectSkillPath(root, sk.dirName))
			if err != nil {
				t.Errorf("%s not installed in the project: %v", sk.selector, err)
				continue
			}
			if string(got) != sk.content {
				t.Errorf("%s: project copy differs from the bundled skill", sk.selector)
			}
		}
		if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
			t.Errorf("--project also wrote under HOME (stat err: %v)", err)
		}
	}
}

func TestSkillInstall_projectRefusesOutsideARepo(t *testing.T) {
	dir := t.TempDir()
	if got, ok := findGitRoot(dir); ok {
		t.Skipf("the temp dir is inside a git repository (%s)", got)
	}
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir())

	err, _, _ := execCmd(t, "skill", "install", "--project")
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Code != clierr.CodeBadInput {
		t.Fatalf("err = %v, want BAD_INPUT", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
		t.Errorf("wrote into a folder that is not a repository (stat err: %v)", err)
	}
}

func TestSkillInstall_projectAndDirConflict(t *testing.T) {
	root, _ := fakeRepo(t, false)
	other := filepath.Join(t.TempDir(), "plivo-cli")

	err, _, _ := execCmd(t, "skill", "install", "--project", "--dir", other)
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Code != clierr.CodeBadFlag {
		t.Fatalf("err = %v, want BAD_FLAG", err)
	}
	for _, p := range []string{other, filepath.Join(root, ".claude")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("a rejected command wrote %s (stat err: %v)", p, err)
		}
	}
}

// A project copy is committed and shared, so it may hold the team's edits: one
// that differs is kept unless --force, while the rest of `all` still installs.
func TestSkillInstall_projectKeepsAFileThatDiffers(t *testing.T) {
	root, _ := fakeRepo(t, false)
	edited := projectSkillPath(root, "plivo-cli")
	if err := os.MkdirAll(filepath.Dir(edited), 0o755); err != nil {
		t.Fatal(err)
	}
	const mine = "---\nname: plivo-cli\n---\nour team's notes\n"
	if err := os.WriteFile(edited, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"skill", "install", "--project"},
		{"skill", "install", "--project", "--dry-run"},
		{"skill", "install", "all", "--project"},
	} {
		err, _, _ := execCmd(t, args...)
		var ce *clierr.Error
		if !errors.As(err, &ce) || ce.Code != clierr.CodeDestructiveRefused {
			t.Fatalf("%v: err = %v, want DESTRUCTIVE_REFUSED", args, err)
		}
		if !strings.Contains(ce.Message, edited) || !strings.Contains(ce.Hint, "--force") {
			t.Errorf("%v: want the kept file named and --force in the hint: %+v", args, ce)
		}
		if got, _ := os.ReadFile(edited); string(got) != mine {
			t.Fatalf("%v overwrote the project's edited skill", args)
		}
	}
	// `all` went on to install the skills that were not there yet.
	if _, err := os.Stat(projectSkillPath(root, "plivo-voice-xml")); err != nil {
		t.Errorf("`all` stopped at the kept file: %v", err)
	}

	if err, _, stderr := execCmd(t, "skill", "install", "--project", "--force"); err != nil {
		t.Fatalf("--force: %v\n%s", err, stderr)
	}
	if got, _ := os.ReadFile(edited); string(got) != cliskill.SkillMD {
		t.Error("--force did not overwrite the project copy")
	}
	// Same bytes again: nothing to refuse.
	if err, _, _ := execCmd(t, "skill", "install", "--project"); err != nil {
		t.Errorf("reinstalling an identical copy: %v", err)
	}
}

func TestSkillInstall_projectDryRunWritesNothing(t *testing.T) {
	root, _ := fakeRepo(t, false)
	if err, _, _ := execCmd(t, "skill", "install", "all", "--project", "--dry-run"); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Errorf("--dry-run wrote into the project (stat err: %v)", err)
	}
}

// Git for Windows checks a committed skill out with CRLF line endings by
// default. That copy is the same skill, not an edit to keep.
func TestSkillInstall_projectReadsCRLFAsLF(t *testing.T) {
	root, _ := fakeRepo(t, false)
	p := projectSkillPath(root, "plivo-cli")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	crlf := func(s string) []byte { return []byte(strings.ReplaceAll(s, "\n", "\r\n")) }

	if err := os.WriteFile(p, crlf(cliskill.SkillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if err, _, stderr := execCmd(t, "skill", "install", "--project"); err != nil {
		t.Fatalf("a CRLF checkout of the bundled skill was kept as an edit: %v\n%s", err, stderr)
	}

	edited := crlf(cliskill.SkillMD + "our team's notes\n")
	if err := os.WriteFile(p, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	err, _, _ := execCmd(t, "skill", "install", "--project")
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Code != clierr.CodeDestructiveRefused {
		t.Fatalf("a CRLF copy with an edit: err = %v, want DESTRUCTIVE_REFUSED", err)
	}
	if got, _ := os.ReadFile(p); string(got) != string(edited) {
		t.Error("the edited CRLF copy was overwritten")
	}
}

// --force only means something for a project copy; a home or --dir install
// always overwrites, so the flag there is a mistake to report, not ignore.
func TestSkillInstall_forceNeedsProject(t *testing.T) {
	_, home := fakeRepo(t, false)
	err, _, _ := execCmd(t, "skill", "install", "--force")
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Code != clierr.CodeBadFlag {
		t.Fatalf("err = %v, want BAD_FLAG", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Errorf("a rejected command wrote under HOME (stat err: %v)", err)
	}
}

// Without --project nothing changes: the home copy is the CLI's own and is
// overwritten, as every earlier release did.
func TestSkillInstall_homeInstallStillOverwrites(t *testing.T) {
	_, home := fakeRepo(t, false)
	p := filepath.Join(home, ".claude", "skills", "plivo-cli", skillFileName)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("older\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err, _, _ := execCmd(t, "skill", "install"); err != nil {
		t.Fatalf("home install: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != cliskill.SkillMD {
		t.Error("a home install no longer overwrites")
	}
}

// The hash list is what tells a released copy from a user's edit, so the
// version this binary bundles must be on it: otherwise `skill update` would
// call its own output an edit.
func TestSkillHashes_listEveryBundledSkill(t *testing.T) {
	for _, s := range bundledSkills {
		if !slices.Contains(shippedSkillHashes[s.dirName], skillDigest([]byte(s.content))) {
			t.Errorf("%s: the bundled SKILL.md is not in cmd/skill_hashes.go; run `make skill-hashes`", s.selector)
		}
	}
	for dir := range shippedSkillHashes {
		if !slices.ContainsFunc(bundledSkills, func(s bundledSkill) bool { return s.dirName == dir }) {
			t.Errorf("cmd/skill_hashes.go lists %q, which is not a bundled skill", dir)
		}
	}
}

// skillUpdateHome points HOME at a temp dir and runs the test from a temp
// folder outside any repository, so `skill update` sees only that home.
func skillUpdateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	wd := t.TempDir()
	if got, ok := findGitRoot(wd); ok {
		t.Skipf("the temp dir is inside a git repository (%s)", got)
	}
	t.Chdir(wd)
	return home
}

// shipVersion makes content count as a released version of the skill that
// installs into dir, for the length of the test.
func shipVersion(t *testing.T, dir, content string) {
	t.Helper()
	prev := shippedSkillHashes[dir]
	shippedSkillHashes[dir] = append(slices.Clone(prev), skillDigest([]byte(content)))
	t.Cleanup(func() { shippedSkillHashes[dir] = prev })
}

func writeSkillFile(t *testing.T, root, dir, content string) string {
	t.Helper()
	p := filepath.Join(root, ".claude", "skills", dir, skillFileName)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func skillUpdateRows(t *testing.T, args ...string) []skillUpdateRow {
	t.Helper()
	err, stdout, stderr := execCmd(t, append([]string{"skill", "update", "-o", "json"}, args...)...)
	if err != nil {
		t.Fatalf("skill update %v: %v\n%s", args, err, stderr)
	}
	var env struct {
		Data []skillUpdateRow `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil || env.Data == nil {
		t.Fatalf("decode %q: %v (data must be a list, even an empty one)", stdout, err)
	}
	return env.Data
}

func fileIs(t *testing.T, path, want string) bool {
	t.Helper()
	got, err := os.ReadFile(path)
	return err == nil && string(got) == want
}

const olderCLISkill = "---\nname: plivo-cli\n---\nan older release\n"

func TestSkillUpdate_replacesAReleasedVersion(t *testing.T) {
	home := skillUpdateHome(t)
	shipVersion(t, "plivo-cli", olderCLISkill)
	p := writeSkillFile(t, home, "plivo-cli", olderCLISkill)
	newLines := strings.Count(cliskill.SkillMD, "\n")

	err, stdout, _ := execCmd(t, "skill", "update", "-o", "table")
	if err != nil {
		t.Fatalf("skill update: %v", err)
	}
	if want := fmt.Sprintf("4 -> %d lines", newLines); !strings.Contains(stdout, want) || !strings.Contains(stdout, p) {
		t.Errorf("want %q and the path in the table, got:\n%s", want, stdout)
	}
	if !fileIs(t, p, cliskill.SkillMD) {
		t.Fatal("the released copy was not replaced")
	}

	rows := skillUpdateRows(t)
	if len(rows) != 1 || rows[0].Result != "up_to_date" || rows[0].OldLines != newLines {
		t.Errorf("second run: %+v, want one up_to_date row", rows)
	}
}

func TestSkillUpdate_keepsAnEditedCopyUnlessForced(t *testing.T) {
	home := skillUpdateHome(t)
	const mine = "---\nname: plivo-cli\n---\nmy own notes\n"
	p := writeSkillFile(t, home, "plivo-cli", mine)

	err, _, stderr := execCmd(t, "skill", "update", "-o", "table")
	if err != nil {
		t.Fatalf("skill update: %v", err)
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("want --force named on stderr, got:\n%s", stderr)
	}
	if rows := skillUpdateRows(t); len(rows) != 1 || rows[0].Result != "kept" {
		t.Errorf("rows = %+v, want one kept row", rows)
	}
	if !fileIs(t, p, mine) {
		t.Fatal("an edited copy was overwritten without --force")
	}

	if rows := skillUpdateRows(t, "--force"); len(rows) != 1 || rows[0].Result != "updated" {
		t.Errorf("--force rows = %+v", rows)
	}
	if !fileIs(t, p, cliskill.SkillMD) {
		t.Error("--force did not replace the edited copy")
	}
}

func TestSkillUpdate_dryRunWritesNothing(t *testing.T) {
	home := skillUpdateHome(t)
	shipVersion(t, "plivo-cli", olderCLISkill)
	p := writeSkillFile(t, home, "plivo-cli", olderCLISkill)

	rows := skillUpdateRows(t, "--dry-run")
	if len(rows) != 1 || rows[0].Result != "would_update" || rows[0].OldLines != 4 {
		t.Errorf("rows = %+v, want one would_update row", rows)
	}
	if !fileIs(t, p, olderCLISkill) {
		t.Error("--dry-run wrote the file")
	}
}

func TestSkillUpdate_coversTheProjectInsideARepo(t *testing.T) {
	root, home := fakeRepo(t, false)
	const older = "---\nname: plivo-voice-xml\n---\nolder\n"
	shipVersion(t, "plivo-voice-xml", older)
	paths := []string{
		writeSkillFile(t, home, "plivo-voice-xml", older),
		writeSkillFile(t, root, "plivo-voice-xml", older),
	}

	rows := skillUpdateRows(t)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want the home and the project copy", rows)
	}
	for _, p := range paths {
		if !fileIs(t, p, voicexmlskill.SkillMD) {
			t.Errorf("%s was not updated", p)
		}
	}
}

// update refreshes what is installed; it never installs.
func TestSkillUpdate_installsNothing(t *testing.T) {
	home := skillUpdateHome(t)
	if rows := skillUpdateRows(t); len(rows) != 0 {
		t.Errorf("rows = %+v, want none", rows)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Errorf("update created skills (stat err: %v)", err)
	}
}

// A Windows checkout with autocrlf turns a committed skill's LF into CRLF:
// that is the same version, not an edit.
func TestSkillUpdate_readsCRLFAsLF(t *testing.T) {
	home := skillUpdateHome(t)
	crlf := strings.ReplaceAll(voicexmlskill.SkillMD, "\n", "\r\n")
	p := writeSkillFile(t, home, "plivo-voice-xml", crlf)
	if rows := skillUpdateRows(t); len(rows) != 1 || rows[0].Result != "up_to_date" {
		t.Errorf("a CRLF copy of the bundled skill: %+v, want up_to_date", rows)
	}
	if !fileIs(t, p, crlf) {
		t.Error("an up-to-date copy was rewritten")
	}

	shipVersion(t, "plivo-cli", olderCLISkill)
	writeSkillFile(t, home, "plivo-cli", strings.ReplaceAll(olderCLISkill, "\n", "\r\n"))
	for _, r := range skillUpdateRows(t) {
		if r.Selector == "cli" && r.Result != "updated" {
			t.Errorf("a CRLF copy of a released version: %+v, want updated", r)
		}
	}
}

// v1.1.x installed plivo-first-agent, which plivo-audio-streaming replaced.
// update reports a copy still there with the command that cleans it up, and
// deletes nothing itself.
func TestSkillUpdate_reportsARetiredSkill(t *testing.T) {
	home := skillUpdateHome(t)
	old := filepath.Join(writeRetiredSkill(t, filepath.Join(home, ".claude", "skills")), skillFileName)

	var retired *skillUpdateRow
	rows := skillUpdateRows(t)
	for i := range rows {
		if rows[i].Result == "retired" {
			retired = &rows[i]
		}
	}
	if retired == nil || retired.Path != old || retired.Selector != "audio-streaming" ||
		retired.Hint != "plivo skill install audio-streaming" {
		t.Fatalf("rows = %+v, want a retired row for %s naming `plivo skill install audio-streaming`", rows, old)
	}
	err, stdout, _ := execCmd(t, "skill", "update", "-o", "table")
	if err != nil || !strings.Contains(stdout, "retired: run plivo skill install audio-streaming") {
		t.Errorf("the table does not name the fix (err %v):\n%s", err, stdout)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("update deleted the retired copy: %v", err)
	}

	// The command it names does clean it up.
	if err, _, _ := execCmd(t, "skill", "install", "audio-streaming"); err != nil {
		t.Fatalf("install audio-streaming: %v", err)
	}
	for _, r := range skillUpdateRows(t) {
		if r.Result == "retired" {
			t.Errorf("still reported after the fix: %+v", r)
		}
	}
}

// A project copy is not the CLI's to remove (no release wrote one there), so
// the fix it names is the project install plus deleting the old folder.
func TestSkillUpdate_reportsARetiredSkillInTheProject(t *testing.T) {
	root, _ := fakeRepo(t, false)
	old := writeRetiredSkill(t, filepath.Join(root, ".claude", "skills"))

	rows := skillUpdateRows(t)
	if len(rows) != 1 || rows[0].Result != "retired" ||
		rows[0].Hint != "plivo skill install audio-streaming --project, then delete "+old {
		t.Fatalf("rows = %+v", rows)
	}
	if _, err := os.Stat(filepath.Join(old, skillFileName)); err != nil {
		t.Errorf("update deleted the retired project copy: %v", err)
	}
}
