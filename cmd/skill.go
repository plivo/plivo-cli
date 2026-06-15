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

// skillCmd hosts the skill subcommands (currently just `install`).
var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage the plivo-cli agent skill",
}

// skillInstallCmd writes the embedded SKILL.md into the agent skills directory
// (default ~/.claude/skills/plivo-cli; override with --dir, or --print to stdout).
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
	// --print emits the skill to stdout; ignores --dir / --dry-run.
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

// resolveSkillDir returns the override (with ~ expanded) or the default
// ~/.claude/skills/plivo-cli.
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
