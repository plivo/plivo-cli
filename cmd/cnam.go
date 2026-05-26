package cmd

import (
	"os"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var cnamCmd = &cobra.Command{
	Use:   "cnam <number>",
	Short: "Caller-ID Name (CNAM) lookup for a US/CA number (spends money — requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runCnam,
}

func init() {
	rootCmd.AddCommand(cnamCmd)
}

func runCnam(cmd *cobra.Command, args []string) error {
	number := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}

	effectiveDryRun := dryRunFlag || !yesFlag
	if effectiveDryRun {
		client.DryRun = true
		if !dryRunFlag {
			_, _ = os.Stderr.WriteString("[dry-run] cnam lookup defaults to dry-run; pass --yes to actually charge\n")
		}
	}

	var c api.CnamLookup
	apiErr, err := client.Do("GET", client.AccountURL("CnamLookup", number), nil, nil, &c)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if effectiveDryRun {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, c, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"number", c.Number},
		{"caller_name", c.CallerName},
		{"caller_type", c.CallerType},
		{"charge", c.Charge},
	})
}
