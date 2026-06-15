package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	cliskill "github.com/plivo/plivo-cli/cli-skill"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/spf13/cobra"
)

// skill install flags.
var (
	skillDir   string
	skillPrint bool
)

// skillFileName is the on-disk name of the skill file inside the target dir.
const skillFileName = "SKILL.md"

// skillCmd hosts the skill-management subcommands. Today that's just
// `install`, which drops the agent skill (bundled in the binary) where a
// coding agent will auto-load it.
var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage the plivo-cli agent skill",
}

// skillInstallCmd writes the embedded SKILL.md into the agent skills
// directory so future agent sessions auto-load the CLI reference. Default
// target follows the Claude Code layout (~/.claude/skills/plivo-cli); override
// with --dir for other agents, or --print to capture the content directly.
var skillInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the agent skill so coding agents auto-load the CLI reference",
	Long: `Install the plivo-cli agent skill.

The skill is a single-file reference (SKILL.md) written for LLM coding agents.
It is bundled in the binary, so this writes it out without a network call.

By default it lands at ~/.claude/skills/plivo-cli/SKILL.md (Claude Code).
Use --dir to target another agent's skills directory, or --print to write the
content to stdout so any other tool can capture it.`,
	Example: `  plivo skill install                    # install to ~/.claude/skills/plivo-cli/SKILL.md
  plivo skill install --dir ~/.config/agent/skills/plivo-cli
  plivo skill install --print > plivo-cli.md   # capture for any agent
  plivo skill install --dry-run          # show the destination, write nothing`,
	Args: cobra.NoArgs,
	RunE: runSkillInstall,
}

func init() {
	skillInstallCmd.Flags().StringVar(&skillDir, "dir", "", "destination directory (default: ~/.claude/skills/plivo-cli)")
	skillInstallCmd.Flags().BoolVar(&skillPrint, "print", false, "write the skill content to stdout instead of installing")
	skillCmd.AddCommand(skillInstallCmd)
	rootCmd.AddCommand(skillCmd)
}

func runSkillInstall(cmd *cobra.Command, args []string) error {
	// --print short-circuits: emit the skill to stdout so any agent/tool can
	// capture it. Ignores --dir / --dry-run by design.
	if skillPrint {
		_, err := fmt.Fprint(os.Stdout, cliskill.SkillMD)
		return err
	}

	dir, err := resolveSkillDir(skillDir)
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
	if err := os.WriteFile(dest, []byte(cliskill.SkillMD), 0o644); err != nil {
		return clierr.Wrap(fmt.Errorf("write skill to %s: %w", dest, err))
	}

	fmt.Fprintf(os.Stderr, "Installed skill: %s\n", dest)
	return nil
}

// resolveSkillDir resolves the destination directory for the skill. A non-empty
// override is expanded (a leading ~ becomes the home dir) and returned as-is;
// otherwise the default ~/.claude/skills/plivo-cli is used.
func resolveSkillDir(override string) (string, error) {
	if override != "" {
		return expandHome(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", clierr.Wrap(fmt.Errorf("resolve home directory: %w", err))
	}
	return filepath.Join(home, ".claude", "skills", "plivo-cli"), nil
}

// expandHome rewrites a leading "~" (bare or "~/…") to the user's home
// directory. Other paths are returned unchanged. The shell normally does this,
// but a quoted or programmatic --dir value reaches us with the tilde intact.
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

// startsWithTildeSlash reports whether path begins with "~/" (or "~\" on
// Windows), i.e. a home-relative path the shell would normally expand.
func startsWithTildeSlash(path string) bool {
	return len(path) >= 2 && path[0] == '~' && (path[1] == '/' || path[1] == os.PathSeparator)
}
