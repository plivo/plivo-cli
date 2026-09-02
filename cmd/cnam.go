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
	numberCmd.AddCommand(cnamCmd)
}

// runCnam is gated by the unified spend-verb contract — CNAM lookups
// are billed per request, so the command refuses without --yes
// (DESTRUCTIVE_REFUSED, exit 5) and accepts --dry-run for preview.
func runCnam(cmd *cobra.Command, args []string) error {
	number := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}

	proceed, dryRun, gerr := guardSpend("cnam lookup for " + number)
	if !proceed {
		return gerr
	}
	applyDryRun(client, dryRun)

	var c api.CnamLookup
	apiErr, err := client.Do("GET", client.AccountURL("CnamLookup", number), nil, nil, &c)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRun {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, c.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"number", c.Number},
		{"caller_name", c.CallerName},
		{"caller_type", c.CallerType},
		{"charge", c.Charge},
	})
}
