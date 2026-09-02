package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var subaccountCmd = &cobra.Command{
	Use:     "subaccounts",
	Aliases: []string{"sub", "subaccount"},
	Short:   "Manage subaccounts under the master account",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var (
	subListLimit  int
	subListOffset int
)

var subListCmd = &cobra.Command{
	Use:   "list",
	Short: "List subaccounts",
	RunE:  runSubList,
}

var subGetCmd = &cobra.Command{
	Use:   "get <subauth_id>",
	Short: "Get a subaccount by auth_id",
	Args:  cobra.ExactArgs(1),
	RunE:  runSubGet,
}

var (
	subCreateName    string
	subCreateEnabled bool
)

var subCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a subaccount",
	RunE:  runSubCreate,
}

var (
	subUpdateName    string
	subUpdateEnabled string // tri-state via string so we can distinguish "not set" from "false"
)

var subUpdateCmd = &cobra.Command{
	Use:   "update <subauth_id>",
	Short: "Update a subaccount",
	Args:  cobra.ExactArgs(1),
	RunE:  runSubUpdate,
}

var subDeleteCmd = &cobra.Command{
	Use:   "delete <subauth_id>",
	Short: "Delete a subaccount (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSubDelete,
}

func init() {
	subListCmd.Flags().IntVar(&subListLimit, "limit", 20, "results per page")
	subListCmd.Flags().IntVar(&subListOffset, "offset", 0, "pagination offset")

	subCreateCmd.Flags().StringVar(&subCreateName, "name", "", "subaccount name (required)")
	_ = subCreateCmd.MarkFlagRequired("name")
	subCreateCmd.Flags().BoolVar(&subCreateEnabled, "enabled", true, "enable on creation")

	subUpdateCmd.Flags().StringVar(&subUpdateName, "name", "", "new name")
	subUpdateCmd.Flags().StringVar(&subUpdateEnabled, "enabled", "", "true|false")

	subaccountCmd.AddCommand(subListCmd, subGetCmd, subCreateCmd, subUpdateCmd, subDeleteCmd)
	accountCmd.AddCommand(subaccountCmd)
}

func runSubList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(subListLimit))
	q.Set("offset", strconv.Itoa(subListOffset))
	var resp api.SubaccountList
	apiErr, err := client.Do("GET", client.AccountURL("Subaccount"), nil, q, &resp)
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
		return output.JSONRaw(os.Stdout, resp.Raw())
	}
	rows := [][]string{{"AUTH_ID", "NAME", "ENABLED", "CREATED"}}
	for _, s := range resp.Objects {
		rows = append(rows, []string{s.AuthID, s.Name, fmt.Sprintf("%v", s.Enabled), s.CreatedOn})
	}
	return output.Table(os.Stdout, rows)
}

func runSubGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var s api.Subaccount
	apiErr, err := client.Do("GET", client.AccountURL("Subaccount", id), nil, nil, &s)
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
		return output.JSONRaw(os.Stdout, s.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"auth_id", s.AuthID},
		{"name", s.Name},
		{"enabled", fmt.Sprintf("%v", s.Enabled)},
		{"created_on", s.CreatedOn},
		{"modified_on", s.ModifiedOn},
	})
}

func runSubCreate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"name":    subCreateName,
		"enabled": subCreateEnabled,
	}
	var resp struct {
		api.RawBody
		APIID     string `json:"api_id"`
		AuthID    string `json:"auth_id"`
		AuthToken string `json:"auth_token"`
		Message   string `json:"message"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("Subaccount"), body, nil, &resp)
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
		return output.JSONRaw(os.Stdout, resp.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"auth_id", resp.AuthID},
		{"auth_token", resp.AuthToken},
		{"message", resp.Message},
	})
}

func runSubUpdate(cmd *cobra.Command, args []string) error {
	id := args[0]
	body := map[string]any{}
	if subUpdateName != "" {
		body["name"] = subUpdateName
	}
	switch subUpdateEnabled {
	case "true":
		body["enabled"] = true
	case "false":
		body["enabled"] = false
	case "":
	default:
		return clierr.BadFlag("enabled", "must be 'true' or 'false'")
	}
	if len(body) == 0 {
		return clierr.BadInput("at least one of --name or --enabled required")
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("Subaccount", id), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Updated subaccount %s: %s\n", id, resp.Message)
	return nil
}

func runSubDelete(cmd *cobra.Command, args []string) error {
	id := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("delete subaccount " + id)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("Subaccount", id), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Deleted subaccount %s\n", id)
	return nil
}
