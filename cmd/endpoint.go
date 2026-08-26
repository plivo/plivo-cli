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

var endpointCmd = &cobra.Command{
	Use:     "endpoints",
	Aliases: []string{"ep", "endpoint"},
	Short:   "Manage SIP endpoints (registered SIP devices/usernames)",
}

var (
	epListLimit  int
	epListOffset int
)

var epListCmd = &cobra.Command{
	Use:   "list",
	Short: "List SIP endpoints",
	RunE:  runEndpointList,
}

var epGetCmd = &cobra.Command{
	Use:   "get <endpoint_id>",
	Short: "Get an endpoint by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runEndpointGet,
}

var (
	epCreateUsername string
	epCreatePassword string
	epCreateAlias    string
	epCreateAppID    string
)

var epCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a SIP endpoint",
	RunE:  runEndpointCreate,
}

var (
	epUpdatePassword string
	epUpdateAlias    string
	epUpdateAppID    string
)

var epUpdateCmd = &cobra.Command{
	Use:   "update <endpoint_id>",
	Short: "Update a SIP endpoint",
	Args:  cobra.ExactArgs(1),
	RunE:  runEndpointUpdate,
}

var epDeleteCmd = &cobra.Command{
	Use:   "delete <endpoint_id>",
	Short: "Delete a SIP endpoint (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runEndpointDelete,
}

func init() {
	epListCmd.Flags().IntVar(&epListLimit, "limit", 20, "results per page")
	epListCmd.Flags().IntVar(&epListOffset, "offset", 0, "pagination offset")

	epCreateCmd.Flags().StringVar(&epCreateUsername, "username", "", "SIP username (required)")
	_ = epCreateCmd.MarkFlagRequired("username")
	epCreateCmd.Flags().StringVar(&epCreatePassword, "password", "", "SIP password (required)")
	_ = epCreateCmd.MarkFlagRequired("password")
	epCreateCmd.Flags().StringVar(&epCreateAlias, "alias", "", "human-friendly label")
	epCreateCmd.Flags().StringVar(&epCreateAppID, "app-id", "", "application to attach")

	epUpdateCmd.Flags().StringVar(&epUpdatePassword, "password", "", "new password")
	epUpdateCmd.Flags().StringVar(&epUpdateAlias, "alias", "", "new alias")
	epUpdateCmd.Flags().StringVar(&epUpdateAppID, "app-id", "", "new application id")

	endpointCmd.AddCommand(epListCmd, epGetCmd, epCreateCmd, epUpdateCmd, epDeleteCmd)
	voiceCmd.AddCommand(endpointCmd)
}

func runEndpointList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(epListLimit))
	q.Set("offset", strconv.Itoa(epListOffset))
	var resp api.EndpointList
	apiErr, err := client.Do("GET", client.AccountURL("Endpoint"), nil, q, &resp)
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
	rows := [][]string{{"ENDPOINT_ID", "USERNAME", "ALIAS", "SIP_URI", "APP_ID"}}
	for _, e := range resp.Objects {
		rows = append(rows, []string{e.EndpointID, e.Username, e.Alias, e.SipURI, e.AppID})
	}
	return output.Table(os.Stdout, rows)
}

func runEndpointGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var e api.Endpoint
	apiErr, err := client.Do("GET", client.AccountURL("Endpoint", id), nil, nil, &e)
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
		return output.JSONRaw(os.Stdout, e.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"endpoint_id", e.EndpointID},
		{"username", e.Username},
		{"alias", e.Alias},
		{"sip_uri", e.SipURI},
		{"app_id", e.AppID},
		{"sub_account", e.Subaccount},
	})
}

func runEndpointCreate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"username": epCreateUsername,
		"password": epCreatePassword,
	}
	if epCreateAlias != "" {
		body["alias"] = epCreateAlias
	}
	if epCreateAppID != "" {
		body["app_id"] = epCreateAppID
	}
	var resp struct {
		api.RawBody
		APIID      string `json:"api_id"`
		EndpointID string `json:"endpoint_id"`
		Username   string `json:"username"`
		Alias      string `json:"alias"`
		Message    string `json:"message"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("Endpoint"), body, nil, &resp)
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
		{"endpoint_id", resp.EndpointID},
		{"username", resp.Username},
		{"message", resp.Message},
	})
}

func runEndpointUpdate(cmd *cobra.Command, args []string) error {
	id := args[0]
	body := map[string]any{}
	if epUpdatePassword != "" {
		body["password"] = epUpdatePassword
	}
	if epUpdateAlias != "" {
		body["alias"] = epUpdateAlias
	}
	if epUpdateAppID != "" {
		body["app_id"] = epUpdateAppID
	}
	if len(body) == 0 {
		return clierr.BadInput("at least one of --password, --alias, --app-id required")
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("Endpoint", id), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Updated endpoint %s: %s\n", id, resp.Message)
	return nil
}

func runEndpointDelete(cmd *cobra.Command, args []string) error {
	id := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("delete endpoint " + id)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("Endpoint", id), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Deleted endpoint %s\n", id)
	return nil
}
