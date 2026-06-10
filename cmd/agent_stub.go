//go:build !internal

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// agentCmd is a placeholder in the public build. Public build only ships
// the coming-soon stub; the full agent surface is gated behind a build
// tag for now.
//
// This stub keeps `plivo agent` discoverable and tells users it's coming,
// rather than the command simply not existing.
var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "AI voice agents (coming soon)",
	Long:  `AI voice agents — coming soon.`,
	// Accept any args so `plivo agent create`, `plivo agent run`, etc. all land
	// on the coming-soon notice instead of an "unknown command" error.
	Args: cobra.ArbitraryArgs,
	// Swallow unknown flags (e.g. `--prompt`, `--from`) so a user copy-pasting
	// a future agent command still gets the coming-soon notice rather than an
	// "unknown flag" error. `--help` stays functional (it's a known flag).
	FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(os.Stderr, "AI voice agents from the CLI — coming soon.")
		fmt.Fprintln(os.Stderr, "Track it in the roadmap:")
		fmt.Fprintln(os.Stderr, "  https://github.com/plivo/plivo-cli/blob/main/ROADMAP.md")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(agentCmd)
}
