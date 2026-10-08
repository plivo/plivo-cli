package cmd

import (
	"fmt"
	"os"
	"path/filepath"
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
)

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
Each skill lands at ~/.claude/skills/<skill>/SKILL.md by default. Use --dir to
target another agent's skills directory, or --print to write the content to
stdout so any other tool can capture it; both act on a single skill.

--project installs into the git repository you are in instead, at
<repo>/.claude/skills/<skill>/SKILL.md, so everyone who clones it gets the
skill. A project copy that differs from the bundled one may hold your team's
edits, so it is kept unless you pass --force.`,
	Example: `  plivo skill install                    # CLI skill -> ~/.claude/skills/plivo-cli/
  plivo skill install audio-streaming    # -> ~/.claude/skills/plivo-audio-streaming/
  plivo skill install voice-xml          # -> ~/.claude/skills/plivo-voice-xml/
  plivo skill install all                # every listed skill
  plivo skill install all --project      # -> <repo>/.claude/skills/, for the whole team
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
	Args:  cobra.NoArgs,
	RunE:  runSkillList,
}

func init() {
	skillInstallCmd.Flags().StringVar(&skillDir, "dir", "", "destination directory (default: ~/.claude/skills/<skill>)")
	skillInstallCmd.Flags().BoolVar(&skillPrint, "print", false, "write the skill content to stdout instead of installing")
	skillInstallCmd.Flags().BoolVar(&skillProject, "project", false, "install into the current git repository's .claude/skills")
	skillInstallCmd.Flags().BoolVar(&skillForce, "force", false, "with --project, overwrite a skill file that differs from the bundled one")
	skillCmd.AddCommand(skillInstallCmd, skillListCmd)
	rootCmd.AddCommand(skillCmd)
}

func runSkillInstall(cmd *cobra.Command, args []string) error {
	selector := ""
	if len(args) == 1 {
		selector = args[0]
	}
	if skillProject && skillDir != "" {
		return clierr.BadFlag("project", "can't be combined with --dir; --project installs into the repository's .claude/skills")
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

// installSkills installs each skill, into the repository under --project. A
// project copy that was kept fails the command once the rest are installed.
func installSkills(skills []bundledSkill) error {
	projectRoot := ""
	if skillProject {
		root, err := projectSkillsRoot()
		if err != nil {
			return err
		}
		projectRoot = root
	}
	var kept []string
	for _, s := range skills {
		k, err := installSkill(s, projectRoot)
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
	what := kept[0] + ", which differs from the bundled skill"
	if len(kept) > 1 {
		what = strings.Join(kept, ", ") + ", which differ from the bundled skills"
	}
	e := clierr.DestructiveRefused("overwrite " + what)
	e.Hint = "A project copy may hold your team's edits. Pass --force to overwrite it, " +
		"or compare first: plivo skill install <skill> --print | diff <file> -"
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
		e := clierr.BadInput(wd + " is not inside a git repository, so --project has nowhere to install")
		e.Hint = "Run it from inside the project, or drop --project to install for your user (~/.claude/skills)."
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
	rows := make([]skillListEntry, 0, len(bundledSkills))
	for _, sk := range bundledSkills {
		row := skillListEntry{
			Selector:   sk.selector,
			InstallsTo: sk.dirName,
			State:      skillStateAbsent,
			Summary:    sk.summary,
		}
		// A directory we cannot resolve is reported as absent rather than
		// failing the whole listing.
		if dir, err := resolveSkillDir("", sk.dirName); err == nil {
			path := filepath.Join(dir, skillFileName)
			row.Path = path
			if b, rerr := os.ReadFile(path); rerr == nil {
				row.State = skillStateCurrent
				if string(b) != sk.content {
					row.State = skillStateStale
				}
			}
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
	fmt.Fprintf(os.Stderr, "\nInstall one with: plivo skill install <selector>  (or \"all\")\n")
	return nil
}

// installSkill writes one skill to its resolved directory, honouring --dir and
// --dry-run; under --project, projectRoot is the repository's skills root. A
// project copy that differs from the bundled one is left alone unless --force,
// and its path returned.
func installSkill(s bundledSkill, projectRoot string) (kept string, err error) {
	dir := filepath.Join(projectRoot, s.dirName)
	if projectRoot == "" {
		if dir, err = resolveSkillDir(skillDir, s.dirName); err != nil {
			return "", err
		}
	}
	dest := filepath.Join(dir, skillFileName)

	if projectRoot != "" && !skillForce {
		if b, err := os.ReadFile(dest); err == nil && string(b) != s.content {
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
	// skill into a project, so a project has none of the CLI's to remove.
	if skillDir != "" || projectRoot != "" {
		return "", nil
	}
	removeReplacedSkills(s)
	return "", nil
}

// removeReplacedSkills removes, from the default skills root, the skills that s
// supersedes, so an agent never sees both. Earlier releases wrote only
// SKILL.md: a folder that holds other files keeps them. A failure is only a
// warning, because the new skill is already installed.
func removeReplacedSkills(s bundledSkill) {
	for _, name := range s.replaces {
		old, err := resolveSkillDir("", name)
		if err != nil {
			continue
		}
		info, err := os.Lstat(old)
		if err != nil {
			continue
		}
		if dryRunFlag {
			fmt.Fprintf(os.Stderr, "Would remove retired skill: %s\n", old)
			continue
		}
		if info.IsDir() {
			if err := os.Remove(filepath.Join(old, skillFileName)); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "Warning: could not remove retired skill %s: %v\n", old, err)
				continue
			}
		}
		if err := os.Remove(old); err != nil {
			if info.IsDir() {
				fmt.Fprintf(os.Stderr, "Removed the retired skill file from %s; kept the folder, which holds other files\n", old)
			} else {
				fmt.Fprintf(os.Stderr, "Warning: could not remove retired skill %s: %v\n", old, err)
			}
			continue
		}
		fmt.Fprintf(os.Stderr, "Removed retired skill: %s (now part of %s)\n", old, s.dirName)
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
