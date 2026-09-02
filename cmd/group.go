package cmd

import "github.com/spf13/cobra"

// groupRunE is the RunE for parent commands that only host subcommands
// (no direct action of their own). Cobra's own default — no Args, no
// RunE — treats such a command as "not runnable", which makes it print
// help and exit 0 for an unrecognized subcommand instead of erroring.
// Pair with Args: cobra.NoArgs on the same command: together they make a
// bare invocation behave exactly as before (help, exit 0) while a bogus
// subcommand now returns cobra's own "unknown command" error.
func groupRunE(cmd *cobra.Command, _ []string) error {
	return cmd.Help()
}
