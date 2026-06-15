package cmd

import (
	"fmt"
	"os"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "Inspect and update the active Plivo account",
}

var accountGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get account details (name, billing mode, credits, address)",
	RunE:  runAccountGet,
}

var (
	acctUpdateName     string
	acctUpdateAddress  string
	acctUpdateCity     string
	acctUpdateTimezone string
)

var accountUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update account profile fields",
	RunE:  runAccountUpdate,
}

func init() {
	accountUpdateCmd.Flags().StringVar(&acctUpdateName, "name", "", "account name")
	accountUpdateCmd.Flags().StringVar(&acctUpdateAddress, "address", "", "billing address")
	accountUpdateCmd.Flags().StringVar(&acctUpdateCity, "city", "", "city")
	accountUpdateCmd.Flags().StringVar(&acctUpdateTimezone, "timezone", "", "IANA timezone (e.g. Asia/Kolkata)")

	accountCmd.AddCommand(accountGetCmd, accountUpdateCmd)
	rootCmd.AddCommand(accountCmd)
}

func runAccountGet(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var a api.Account
	apiErr, err := client.Do("GET", client.AccountURL(), nil, nil, &a)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, a, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"auth_id", a.AuthID},
		{"name", a.Name},
		{"account_type", a.AccountType},
		{"credits", a.CashCredits},
		{"address", a.Address},
		{"city", a.City},
		{"state", a.State},
		{"timezone", a.Timezone},
		{"auto_recharge", fmt.Sprintf("%v", a.AutoRecharge)},
	})
}

func runAccountUpdate(cmd *cobra.Command, args []string) error {
	body := map[string]any{}
	if acctUpdateName != "" {
		body["name"] = acctUpdateName
	}
	if acctUpdateAddress != "" {
		body["address"] = acctUpdateAddress
	}
	if acctUpdateCity != "" {
		body["city"] = acctUpdateCity
	}
	if acctUpdateTimezone != "" {
		body["timezone"] = acctUpdateTimezone
	}
	if len(body) == 0 {
		return clierr.BadInput("at least one of --name, --address, --city, --timezone required")
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL(), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Updated account: %s\n", resp.Message)
	return nil
}
