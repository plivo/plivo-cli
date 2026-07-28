package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agentsskill "github.com/plivo/plivo-cli/agents-skill"
	cliskill "github.com/plivo/plivo-cli/cli-skill"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/spf13/cobra"
)

// bundledSkill is one skill embedded in the binary. dirName is the directory it
// installs into under the agent skills root, and doubles as the selector a user
// types (`plivo skill install agents`).
type bundledSkill struct {
	selector string // what the user types
	dirName  string // ~/.claude/skills/<dirName>/SKILL.md
	content  string
	summary  string
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
	{
		selector: "agents",
		dirName:  "plivo-cx-agents",
		content:  agentsskill.SkillMD,
		summary:  "build CX agent flows through the public Agents API",
	},
}

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
	skillDir   string
	skillPrint bool
)

// skillFileName is the on-disk name of the skill file inside the target dir.
const skillFileName = "SKILL.md"

// skillCmd hosts the skill subcommands (currently just `install`).
var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage the plivo-cli agent skill",
}

// skillInstallCmd writes the embedded SKILL.md into the agent skills directory
// (default ~/.claude/skills/plivo-cli; override with --dir, or --print to stdout).
var skillInstallCmd = &cobra.Command{
	Use:   "install [cli|agents|all]",
	Short: "Install an agent skill so coding agents auto-load the reference",
	Long: `Install a Plivo agent skill.

A skill is a single-file reference (SKILL.md) written for LLM coding agents.
Both are bundled in the binary, so this writes them out without a network call.

  cli      the CLI reference — use ` + "`plivo`" + ` instead of raw curl
  agents   build CX agent flows through the public Agents API
  all      both of the above

With no argument, installs the CLI skill (unchanged from previous releases).
Each skill lands at ~/.claude/skills/<skill>/SKILL.md by default. Use --dir to
target another agent's skills directory, or --print to write the content to
stdout so any other tool can capture it; both act on a single skill.`,
	Example: `  plivo skill install                    # CLI skill -> ~/.claude/skills/plivo-cli/
  plivo skill install agents             # Agents skill -> ~/.claude/skills/plivo-cx-agents/
  plivo skill install all                # both
  plivo skill install agents --print > plivo-agents.md   # capture for any agent
  plivo skill install agents --dir ~/.config/agent/skills/plivo-cx-agents
  plivo skill install all --dry-run      # show destinations, write nothing`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"cli", "agents", "all"},
	RunE:      runSkillInstall,
}

func init() {
	skillInstallCmd.Flags().StringVar(&skillDir, "dir", "", "destination directory (default: ~/.claude/skills/<skill>)")
	skillInstallCmd.Flags().BoolVar(&skillPrint, "print", false, "write the skill content to stdout instead of installing")
	skillCmd.AddCommand(skillInstallCmd)
	rootCmd.AddCommand(skillCmd)
}

func runSkillInstall(cmd *cobra.Command, args []string) error {
	selector := ""
	if len(args) == 1 {
		selector = args[0]
	}

	// "all" fans out; --dir and --print each name a single destination, so they
	// are incompatible with it.
	if selector == "all" {
		if skillDir != "" || skillPrint {
			return clierr.Wrap(fmt.Errorf(
				"--dir and --print act on one skill; name it instead of \"all\""))
		}
		for _, s := range bundledSkills {
			if err := installSkill(s); err != nil {
				return err
			}
		}
		return nil
	}

	s, err := lookupSkill(selector)
	if err != nil {
		return err
	}

	// --print emits the skill to stdout; ignores --dir / --dry-run.
	if skillPrint {
		_, err := fmt.Fprint(os.Stdout, s.content)
		return err
	}

	if err := installSkill(s); err != nil {
		return err
	}

	// Only nudge when the user took the default, so an explicit choice stays quiet.
	if selector == "" && !dryRunFlag {
		fmt.Fprintf(os.Stderr, "Also available: plivo skill install agents  (%s)\n",
			bundledSkills[1].summary)
	}
	return nil
}

// installSkill writes one skill to its resolved directory, honouring --dir and
// --dry-run.
func installSkill(s bundledSkill) error {
	dir, err := resolveSkillDir(skillDir, s.dirName)
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, skillFileName)

	// --dry-run: report the destination without touching disk.
	if dryRunFlag {
		fmt.Fprintf(os.Stderr, "Would write skill to %s\n", dest)
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return clierr.Wrap(fmt.Errorf("create skill directory %s: %w", dir, err))
	}
	if err := os.WriteFile(dest, []byte(s.content), 0o644); err != nil {
		return clierr.Wrap(fmt.Errorf("write skill to %s: %w", dest, err))
	}

	fmt.Fprintf(os.Stderr, "Installed skill: %s\n", dest)
	return nil
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
