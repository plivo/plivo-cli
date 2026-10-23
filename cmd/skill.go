package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	audiostreamingskill "github.com/plivo/plivo-cli/audio-streaming-skill"
	cliskill "github.com/plivo/plivo-cli/cli-skill"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	siptrunkingskill "github.com/plivo/plivo-cli/sip-trunking-skill"
	voicexmlskill "github.com/plivo/plivo-cli/voice-xml-skill"
	"github.com/spf13/cobra"
)

// bundledSkill is one skill embedded in the binary. dirName is the directory it
// installs into under the agent skills root, and doubles as the selector a user
// types (`plivo skill install cli`).
type bundledSkill struct {
	selector string // what the user types
	dirName  string // ~/.claude/skills/<dirName>/SKILL.md
	content  string
	summary  string
	replaces []string // skill directories from earlier releases that this skill supersedes
}

// bundledSkills is ordered; the FIRST entry is the default when no selector is
// given, which keeps bare `plivo skill install` behaving as it always has.
var bundledSkills = []bundledSkill{
	{
		selector: "cli",
		dirName:  "plivo-cli",
		content:  cliskill.SkillMD,
		summary:  "the CLI reference — use `plivo` instead of raw curl",
	},
	{selector: "audio-streaming", dirName: "plivo-audio-streaming", content: audiostreamingskill.SkillMD, summary: "a voice bot on real calls with <Stream>, from setup to go-live", replaces: []string{"plivo-first-agent"}},
	{selector: "sip-trunking", dirName: "plivo-sip-trunking", content: siptrunkingskill.SkillMD, summary: "connect an AI voice platform over SIP trunking"},
	{selector: "voice-xml", dirName: "plivo-voice-xml", content: voicexmlskill.SkillMD, summary: "write and fix Plivo Voice XML"},
}

// The CX agents skill (agents-skill/) stays in the repo, keeping its own
// embed file and tests, but nothing imports it here. So it reaches neither
// bundledSkills, ValidArgs and the help text, nor the binary itself, while
// the feature isn't live. See TestSkillInstall_cxAgentsSkillIsNotOffered.

// lookupSkill resolves a user-typed selector. An empty selector means the
// default (first) skill.
func lookupSkill(selector string) (bundledSkill, error) {
	if selector == "" {
		return bundledSkills[0], nil
	}
	for _, s := range bundledSkills {
		if s.selector == selector {
			return s, nil
		}
	}
	names := make([]string, 0, len(bundledSkills)+1)
	for _, s := range bundledSkills {
		names = append(names, s.selector)
	}
	names = append(names, "all")
	return bundledSkill{}, clierr.Wrap(fmt.Errorf(
		"unknown skill %q; available: %s", selector, strings.Join(names, ", ")))
}

// skill install flags.
var (
	skillDir     string
	skillPrint   bool
	skillProject bool
	skillForce   bool
	skillAgent   string
)

// agentSkillsDirs maps each --agent value to the user skills directory that
// coding agent reads, under the home directory.
var agentSkillsDirs = map[string]string{
	"claude": filepath.Join(".claude", "skills"),
	"codex":  filepath.Join(".agents", "skills"),
}

// skillFileName is the on-disk name of the skill file inside the target dir.
const skillFileName = "SKILL.md"

// skillCmd hosts the skill subcommands (currently just `install`).
var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage the plivo-cli agent skill",
	Args:  cobra.NoArgs,
	RunE:  groupRunE,
}

// skillInstallCmd writes the embedded SKILL.md into the agent skills directory
// (default ~/.claude/skills/plivo-cli; override with --dir, or --print to stdout).
var skillInstallCmd = &cobra.Command{
	Use:   "install [cli|audio-streaming|sip-trunking|voice-xml|all]",
	Short: "Install an agent skill so coding agents auto-load the reference",
	Long: `Install a Plivo agent skill.

A skill is a single-file reference (SKILL.md) written for LLM coding agents.
They are bundled in the binary, so this writes them out without a network call.

  cli              the CLI reference — use ` + "`plivo`" + ` instead of raw curl
  audio-streaming  a voice bot on real calls with <Stream>, from setup to go-live
  sip-trunking     connect an AI voice platform over SIP trunking
  voice-xml        write and fix Plivo Voice XML
  all              every listed skill

With no argument, installs the CLI skill (unchanged from previous releases).
Each skill lands at ~/.claude/skills/<skill>/SKILL.md by default, where Claude
Code and Cursor read skills; --agent codex installs into ~/.agents/skills, where
Codex reads them. Use --dir to target another agent's skills directory, or
--print to write the content to stdout so any other tool can capture it; both
act on a single skill.

--project installs into the git repository you are in instead, at
<repo>/.claude/skills/<skill>/SKILL.md, so everyone who clones it gets the
skill.

An installed copy that matches no released version holds someone's edits, so
it is kept unless you pass --force, as ` + "`plivo skill update`" + ` does. A retired
skill that an install replaces keeps its file, renamed SKILL.md.retired.`,
	Example: `  plivo skill install                    # CLI skill -> ~/.claude/skills/plivo-cli/
  plivo skill install audio-streaming    # -> ~/.claude/skills/plivo-audio-streaming/
  plivo skill install voice-xml          # -> ~/.claude/skills/plivo-voice-xml/
  plivo skill install all                # every listed skill
  plivo skill install all --project      # -> <repo>/.claude/skills/, for the whole team
  plivo skill install all --agent codex  # -> ~/.agents/skills/, where Codex reads them
  plivo skill install all --dry-run      # show destinations, write nothing`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"cli", "audio-streaming", "sip-trunking", "voice-xml", "all"},
	RunE:      runSkillInstall,
}

// skillListCmd shows every bundled skill and whether it's installed. Needs
// no network and no credentials — everything it reports comes from the
// embedded content and the local filesystem.
var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show bundled skills and whether each is installed",
	Example: `  plivo skill list                # in ~/.claude/skills
  plivo skill list --agent codex  # in ~/.agents/skills
  plivo skill list --project      # in <repo>/.claude/skills`,
	Args: cobra.NoArgs,
	RunE: runSkillList,
}

// skillUpdateCmd brings installed skills up to the bundled version without
// overwriting a copy someone edited. Like list, it needs no network and no
// credentials.
var skillUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update installed skills to the versions bundled in this binary",
	Long: `Update every installed Plivo skill to the version bundled in this binary.

It looks in each agent's skills folder, ~/.claude/skills (Claude Code and
Cursor) and ~/.agents/skills (Codex), and inside a git repository in the same
two folders under <repo>. A copy is replaced only when it matches a version a
release shipped; anything else is treated as your own edit and kept, and
--force replaces it too. Skills that are not installed stay that way: add them
with ` + "`plivo skill install`" + `. A retired skill still in a .claude/skills folder
is reported with the command that retires it; update itself deletes nothing.`,
	Example: `  plivo skill update              # update what is installed
  plivo skill update --dry-run    # show what would change, write nothing
  plivo skill update --force      # also replace copies you edited`,
	Args: cobra.NoArgs,
	RunE: runSkillUpdate,
}

func init() {
	skillUpdateCmd.Flags().BoolVar(&skillForce, "force", false, "also replace copies that match no released version (their edits are lost)")
	skillInstallCmd.Flags().StringVar(&skillDir, "dir", "", "destination directory (default: ~/.claude/skills/<skill>)")
	skillInstallCmd.Flags().BoolVar(&skillPrint, "print", false, "write the skill content to stdout instead of installing")
	skillInstallCmd.Flags().BoolVar(&skillProject, "project", false, "install into the current git repository's .claude/skills")
	skillInstallCmd.Flags().BoolVar(&skillForce, "force", false, "also overwrite a copy that matches no released version (its edits are lost)")
	skillInstallCmd.Flags().StringVar(&skillAgent, "agent", "", "coding agent to install for: claude (~/.claude/skills) or codex (~/.agents/skills) (default: claude)")
	skillListCmd.Flags().BoolVar(&skillProject, "project", false, "show the skills installed in the current git repository's .claude/skills")
	skillListCmd.Flags().StringVar(&skillAgent, "agent", "", "coding agent whose skills to show: claude (~/.claude/skills) or codex (~/.agents/skills) (default: claude)")
	skillCmd.AddCommand(skillInstallCmd, skillListCmd, skillUpdateCmd)
	rootCmd.AddCommand(skillCmd)
}

// checkSkillAgent refuses an --agent the CLI does not know, and one given
// with --dir or --project, which names the folder itself.
func checkSkillAgent() error {
	if skillAgent == "" {
		return nil
	}
	if _, ok := agentSkillsDirs[skillAgent]; !ok {
		return clierr.BadFlag("agent", fmt.Sprintf("unknown agent %q; use claude or codex", skillAgent))
	}
	if skillDir != "" || skillProject {
		flag := "--project"
		if skillDir != "" {
			flag = "--dir"
		}
		return clierr.BadFlag("agent", "can't be combined with "+flag+", which names the folder itself")
	}
	return nil
}

func runSkillInstall(cmd *cobra.Command, args []string) error {
	selector := ""
	if len(args) == 1 {
		selector = args[0]
	}
	if skillProject && skillDir != "" {
		return clierr.BadFlag("project", "can't be combined with --dir; --project installs into the repository's .claude/skills")
	}
	if err := checkSkillAgent(); err != nil {
		return err
	}

	// "all" fans out; --dir and --print each name a single destination, so they
	// are incompatible with it.
	if selector == "all" {
		if skillDir != "" || skillPrint {
			return clierr.Wrap(fmt.Errorf(
				"--dir and --print act on one skill; name it instead of \"all\""))
		}
		return installSkills(bundledSkills)
	}

	s, err := lookupSkill(selector)
	if err != nil {
		return err
	}

	// --print emits the skill to stdout; ignores --dir / --project / --dry-run.
	if skillPrint {
		_, err := fmt.Fprint(os.Stdout, s.content)
		return err
	}

	return installSkills([]bundledSkill{s})
}

// installSkills installs each skill, into the repository under --project and
// into the agent's skills directory under --agent. A copy that was kept fails
// the command once the rest are installed.
func installSkills(skills []bundledSkill) error {
	var root string
	var err error
	switch {
	case skillProject:
		root, err = projectSkillsRoot()
	case skillAgent != "":
		root, err = agentSkillsRoot(skillAgent)
	}
	if err != nil {
		return err
	}
	return installSkillsIn(skills, root)
}

// agentSkillsRoot returns the user skills directory the named agent reads.
func agentSkillsRoot(agent string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", clierr.Wrap(fmt.Errorf("resolve home directory: %w", err))
	}
	return filepath.Join(home, agentSkillsDirs[agent]), nil
}

// installSkillsIn installs each skill into root, or where --dir or the default
// (~/.claude/skills) puts it when root is "". A copy that was kept fails the
// call once the rest are installed.
func installSkillsIn(skills []bundledSkill, root string) error {
	var kept []string
	for _, s := range skills {
		k, err := installSkill(s, root)
		if err != nil {
			return err
		}
		if k != "" {
			kept = append(kept, k)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	what := kept[0] + ", which matches no released version"
	if len(kept) > 1 {
		what = strings.Join(kept, ", ") + ", which match no released version"
	}
	e := clierr.DestructiveRefused("overwrite " + what)
	e.Hint = "A copy that matches no released version holds someone's edits, so it was kept. " +
		"--force overwrites it anyway; the edits are lost."
	e.Context = map[string]any{"kept": kept}
	return e
}

// projectSkillsRoot returns <repo>/.claude/skills for the git repository the
// working directory is in.
func projectSkillsRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", clierr.Wrap(fmt.Errorf("resolve working directory: %w", err))
	}
	root, ok := findGitRoot(wd)
	if !ok {
		e := clierr.BadInput(wd + " is not inside a git repository, so --project has no repository to use")
		e.Hint = "Run it from inside the project, or drop --project for your user's skills (~/.claude/skills)."
		return "", e
	}
	return filepath.Join(root, ".claude", "skills"), nil
}

// findGitRoot walks up from dir to the nearest folder holding .git: a
// directory in a clone, a file in a linked worktree or submodule.
func findGitRoot(dir string) (string, bool) {
	dir = filepath.Clean(dir)
	for {
		if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil && (fi.IsDir() || fi.Mode().IsRegular()) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// crlfToLF reads CRLF line endings as LF. Git for Windows checks text out with
// CRLF by default, and a copy that differs only in that is the same skill.
func crlfToLF(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// skillState describes whether a bundled skill is on disk and current.
// "differs from bundled" is the useful one: it catches a skill written by an
// older binary, which is the drift embedding the content creates.
const (
	skillStateAbsent  = "not installed"
	skillStateCurrent = "installed"
	skillStateStale   = "installed (differs from bundled)"
)

// skillListEntry is one row of `plivo skill list`, and the JSON shape.
type skillListEntry struct {
	Selector   string `json:"selector"`
	InstallsTo string `json:"installs_to"`
	State      string `json:"state"`
	Summary    string `json:"summary"`
	Path       string `json:"path,omitempty"`
}

func runSkillList(cmd *cobra.Command, _ []string) error {
	if err := checkSkillAgent(); err != nil {
		return err
	}
	root, rootErr := resolveSkillDir("", "") // ~/.claude/skills
	switch {
	case skillProject:
		if root, rootErr = projectSkillsRoot(); rootErr != nil {
			return rootErr
		}
	case skillAgent != "":
		root, rootErr = agentSkillsRoot(skillAgent)
	}
	rows := make([]skillListEntry, 0, len(bundledSkills))
	for _, sk := range bundledSkills {
		row := skillListEntry{
			Selector:   sk.selector,
			InstallsTo: sk.dirName,
			State:      skillStateAbsent,
			Summary:    sk.summary,
		}
		// A home directory we cannot resolve is reported as absent rather
		// than failing the whole listing.
		if rootErr == nil {
			row.Path = filepath.Join(root, sk.dirName, skillFileName)
			row.State = skillStateAt(sk, row.Path)
		}
		rows = append(rows, row)
	}

	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, rows, nil)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "SELECTOR\tINSTALLS TO\tSTATE\tWHAT IT IS")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Selector, r.InstallsTo, r.State, r.Summary)
	}
	if err := w.Flush(); err != nil {
		return clierr.Wrap(err)
	}
	install := "plivo skill install <selector>"
	if skillProject {
		install += " --project"
	}
	if skillAgent != "" {
		install += " --agent " + skillAgent
	}
	fmt.Fprintf(os.Stderr, "\nInstall one with: %s  (or \"all\")\n", install)
	return nil
}

// skillStateAt reports the state of the copy of s at path. CRLF line endings
// read as LF, as they do for `skill update`: a Windows checkout of a committed
// skill is the same version.
func skillStateAt(s bundledSkill, path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return skillStateAbsent
	}
	if skillDigest(b) != skillDigest([]byte(s.content)) {
		return skillStateStale
	}
	return skillStateCurrent
}

// installSkill writes one skill into root, or to its resolved directory when
// root is "", honouring --dir and --dry-run; under --project, root is the
// repository's skills root. A copy that holds edits is left alone unless
// --force, and its path returned: home and --dir copies used to be
// overwritten without a look.
func installSkill(s bundledSkill, root string) (kept string, err error) {
	dir := filepath.Join(root, s.dirName)
	if root == "" {
		if dir, err = resolveSkillDir(skillDir, s.dirName); err != nil {
			return "", err
		}
	}
	dest := filepath.Join(dir, skillFileName)

	if !skillForce {
		if b, err := os.ReadFile(dest); err == nil && skillEdited(s, b) {
			return dest, nil
		}
	}

	// --dry-run: report the destination without touching disk.
	if dryRunFlag {
		fmt.Fprintf(os.Stderr, "Would write skill to %s\n", dest)
	} else {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", clierr.Wrap(fmt.Errorf("create skill directory %s: %w", dir, err))
		}
		if err := os.WriteFile(dest, []byte(s.content), 0o644); err != nil {
			return "", clierr.Wrap(fmt.Errorf("write skill to %s: %w", dest, err))
		}
		fmt.Fprintf(os.Stderr, "Installed skill: %s\n", dest)
	}

	// --dir names this skill's own folder, so there is no telling where an
	// older copy of a replaced skill would be. No release wrote a retired
	// skill into a project, so a project has none of the CLI's to retire.
	if skillDir != "" || skillProject {
		return "", nil
	}
	retireReplacedSkills(s, filepath.Dir(dir))
	return "", nil
}

// skillUpdateRow is one installed copy `skill update` looked at, and the JSON
// shape. Result is updated, would_update (--dry-run), up_to_date, kept or
// retired; a retired row's Hint is the command that removes it.
type skillUpdateRow struct {
	Selector string `json:"selector"`
	Path     string `json:"path"`
	Result   string `json:"result"`
	OldLines int    `json:"old_lines"`
	NewLines int    `json:"new_lines"`
	Hint     string `json:"hint,omitempty"`
}

func runSkillUpdate(cmd *cobra.Command, _ []string) error {
	// Each agent's skills folder, in the home directory and in the git
	// repository the working directory is in.
	var bases, roots []string
	home, _ := os.UserHomeDir()
	if home != "" {
		bases = append(bases, home)
	}
	if wd, err := os.Getwd(); err == nil {
		if repo, ok := findGitRoot(wd); ok && !slices.Contains(bases, repo) {
			bases = append(bases, repo)
		}
	}
	for _, base := range bases {
		for _, agent := range slices.Sorted(maps.Keys(agentSkillsDirs)) {
			roots = append(roots, filepath.Join(base, agentSkillsDirs[agent]))
		}
	}

	rows := []skillUpdateRow{}
	seen := map[string]bool{} // a copy reached twice, through a link npx skills made, is one row
	for _, root := range roots {
		for _, s := range bundledSkills {
			path := filepath.Join(root, s.dirName, skillFileName)
			b, err := os.ReadFile(path)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return clierr.Wrap(fmt.Errorf("read %s: %w", path, err))
			}
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				if seen[resolved] {
					continue
				}
				seen[resolved] = true
			}
			row := skillUpdateRow{Selector: s.selector, Path: path, OldLines: lineCount(b), NewLines: lineCount([]byte(s.content))}
			have := skillDigest(b)
			switch {
			case have == skillDigest([]byte(s.content)):
				row.Result = "up_to_date"
			case !skillForce && skillEdited(s, b):
				row.Result = "kept"
			case dryRunFlag:
				row.Result = "would_update"
			default:
				if err := os.WriteFile(path, []byte(s.content), 0o644); err != nil {
					return clierr.Wrap(fmt.Errorf("write skill to %s: %w", path, err))
				}
				row.Result = "updated"
			}
			rows = append(rows, row)
		}
	}
	// The retired skill predates --agent, so only a .claude/skills can hold it.
	for _, base := range bases {
		for _, s := range bundledSkills {
			rows = append(rows, retiredSkillRows(s, filepath.Join(base, agentSkillsDirs["claude"]), base == home)...)
		}
	}

	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, rows, nil)
	}
	if len(rows) == 0 {
		fmt.Fprintf(os.Stderr, "No Plivo skills installed in %s. Install one with: plivo skill install\n", strings.Join(roots, ", "))
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "SKILL\tRESULT\tPATH")
	kept := false
	for _, r := range rows {
		result := "up to date"
		switch r.Result {
		case "updated":
			result = fmt.Sprintf("updated, %d -> %d lines", r.OldLines, r.NewLines)
		case "would_update":
			result = fmt.Sprintf("would update, %d -> %d lines", r.OldLines, r.NewLines)
		case "kept":
			result, kept = "kept: local edits", true
		case "retired":
			result = "retired: run " + r.Hint
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", r.Selector, result, r.Path)
	}
	if err := w.Flush(); err != nil {
		return clierr.Wrap(err)
	}
	if kept {
		fmt.Fprintln(os.Stderr, "\nA kept copy matches no released version, so it looks edited. Pass --force to replace it; the edits are lost.")
	}
	return nil
}

// retiredSkillRows reports the skills s replaced that are still installed in
// root, so an agent there may load both. Only `skill install` removes them,
// and only from the home root, which is the one place a release wrote them.
func retiredSkillRows(s bundledSkill, root string, isHome bool) []skillUpdateRow {
	var rows []skillUpdateRow
	for _, name := range s.replaces {
		dir := filepath.Join(root, name)
		if _, err := os.Stat(filepath.Join(dir, skillFileName)); err != nil {
			continue
		}
		fix := "plivo skill install " + s.selector
		if !isHome {
			fix += " --project, then delete " + dir
		}
		rows = append(rows, skillUpdateRow{Selector: s.selector, Path: filepath.Join(dir, skillFileName), Result: "retired", Hint: fix})
	}
	return rows
}

// skillEdited reports whether b, an installed copy of s, matches neither the
// bundled version nor one a release shipped, and so holds someone's edits.
func skillEdited(s bundledSkill, b []byte) bool {
	have := skillDigest(b)
	return have != skillDigest([]byte(s.content)) && !slices.Contains(shippedSkillHashes[s.dirName], have)
}

// skillDigest is the sha256 of a SKILL.md with CRLF read as LF, so a copy a
// Windows checkout converted still matches the version it came from.
// tools/skillhashes computes the same digest for cmd/skill_hashes.go.
func skillDigest(b []byte) string {
	sum := sha256.Sum256([]byte(crlfToLF(string(b))))
	return hex.EncodeToString(sum[:])
}

// lineCount counts lines as an editor shows them.
func lineCount(b []byte) int {
	n := bytes.Count(b, []byte("\n"))
	if len(b) > 0 && b[len(b)-1] != '\n' {
		n++
	}
	return n
}

// retireReplacedSkills renames, in the skills root s was installed into, the
// SKILL.md of each skill that s supersedes to SKILL.md.retired, so an agent
// never loads both. It is renamed, not deleted, since the copy may hold edits,
// and for the same reason a copy retired before is kept: os.Rename would
// replace it, so this one takes SKILL.md.retired.2 or the next free name. The
// folder stays, as an agent finds a skill by its SKILL.md. A failure is only a
// warning, because the new skill is already installed.
func retireReplacedSkills(s bundledSkill, root string) {
	for _, name := range s.replaces {
		old := filepath.Join(root, name)
		path := filepath.Join(old, skillFileName)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if dryRunFlag {
			fmt.Fprintf(os.Stderr, "Would retire skill: %s\n", old)
			continue
		}
		dst := path + ".retired"
		for i := 2; ; i++ {
			if _, err := os.Lstat(dst); err != nil {
				break
			}
			dst = fmt.Sprintf("%s.retired.%d", path, i)
		}
		if err := os.Rename(path, dst); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not retire skill %s: %v\n", old, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "Retired skill %s (now part of %s): renamed its SKILL.md to %s\n", old, s.dirName, filepath.Base(dst))
	}
}

// resolveSkillDir returns the override (with ~ expanded) or the default
// ~/.claude/skills/<name>.
func resolveSkillDir(override, name string) (string, error) {
	if override != "" {
		return expandHome(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", clierr.Wrap(fmt.Errorf("resolve home directory: %w", err))
	}
	return filepath.Join(home, ".claude", "skills", name), nil
}

// expandHome rewrites a leading "~" or "~/…" to the home dir; other paths unchanged.
func expandHome(path string) (string, error) {
	if path != "~" && !startsWithTildeSlash(path) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", clierr.Wrap(fmt.Errorf("resolve home directory: %w", err))
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

// startsWithTildeSlash reports whether path begins with "~/" (or "~\" on Windows).
func startsWithTildeSlash(path string) bool {
	return len(path) >= 2 && path[0] == '~' && (path[1] == '/' || path[1] == os.PathSeparator)
}
