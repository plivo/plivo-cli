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

var powerpackCmd = &cobra.Command{
	Use:     "powerpacks",
	Aliases: []string{"pp", "powerpack"},
	Short:   "Powerpacks — number pools for high-volume SMS",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var (
	ppListLimit  int
	ppListOffset int
)

var ppListCmd = &cobra.Command{
	Use:   "list",
	Short: "List powerpacks",
	RunE:  runPowerpackList,
}

var ppGetCmd = &cobra.Command{
	Use:   "get <uuid>",
	Short: "Get a powerpack by uuid",
	Args:  cobra.ExactArgs(1),
	RunE:  runPowerpackGet,
}

var (
	ppCreateName            string
	ppCreateStickySender    bool
	ppCreateLocalConnect    bool
	ppCreateApplicationType string
	ppCreateApplicationID   string
	ppCreateNumberPriority  string
)

var ppCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a powerpack",
	RunE:  runPowerpackCreate,
}

var (
	ppUpdateName         string
	ppUpdateStickySender string
	ppUpdateLocalConnect string
)

var ppUpdateCmd = &cobra.Command{
	Use:   "update <uuid>",
	Short: "Update a powerpack",
	Args:  cobra.ExactArgs(1),
	RunE:  runPowerpackUpdate,
}

var ppDeleteCmd = &cobra.Command{
	Use:   "delete <uuid>",
	Short: "Delete a powerpack (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runPowerpackDelete,
}

// number sub-group
var ppNumberCmd = &cobra.Command{
	Use:     "numbers",
	Aliases: []string{"number"},
	Short:   "Manage numbers inside a powerpack",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var (
	ppNumListLimit  int
	ppNumListOffset int
)

var ppNumListCmd = &cobra.Command{
	Use:   "list <uuid>",
	Short: "List numbers attached to a powerpack",
	Args:  cobra.ExactArgs(1),
	RunE:  runPowerpackNumberList,
}

var ppNumAddCmd = &cobra.Command{
	Use:   "add <uuid> <number>",
	Short: "Add a number to a powerpack",
	Args:  cobra.ExactArgs(2),
	RunE:  runPowerpackNumberAdd,
}

var ppNumRemoveCmd = &cobra.Command{
	Use:   "remove <uuid> <number>",
	Short: "Remove a number from a powerpack (requires --yes)",
	Args:  cobra.ExactArgs(2),
	RunE:  runPowerpackNumberRemove,
}

func init() {
	ppListCmd.Flags().IntVar(&ppListLimit, "limit", 20, "results per page")
	ppListCmd.Flags().IntVar(&ppListOffset, "offset", 0, "pagination offset")

	ppCreateCmd.Flags().StringVar(&ppCreateName, "name", "", "powerpack name (required)")
	_ = ppCreateCmd.MarkFlagRequired("name")
	ppCreateCmd.Flags().BoolVar(&ppCreateStickySender, "sticky-sender", false, "send from same number to same recipient")
	ppCreateCmd.Flags().BoolVar(&ppCreateLocalConnect, "local-connect", false, "use a local-prefix-matching number")
	ppCreateCmd.Flags().StringVar(&ppCreateApplicationType, "application-type", "", "default_message|xml_application")
	ppCreateCmd.Flags().StringVar(&ppCreateApplicationID, "application-id", "", "application uuid (when application-type=xml_application)")
	ppCreateCmd.Flags().StringVar(&ppCreateNumberPriority, "number-priority", "", "ordered priority list (e.g. \"local,tollfree\")")

	ppUpdateCmd.Flags().StringVar(&ppUpdateName, "name", "", "new name")
	ppUpdateCmd.Flags().StringVar(&ppUpdateStickySender, "sticky-sender", "", "true|false")
	ppUpdateCmd.Flags().StringVar(&ppUpdateLocalConnect, "local-connect", "", "true|false")

	ppNumListCmd.Flags().IntVar(&ppNumListLimit, "limit", 20, "results per page")
	ppNumListCmd.Flags().IntVar(&ppNumListOffset, "offset", 0, "pagination offset")

	ppNumberCmd.AddCommand(ppNumListCmd, ppNumAddCmd, ppNumRemoveCmd)
	powerpackCmd.AddCommand(ppListCmd, ppGetCmd, ppCreateCmd, ppUpdateCmd, ppDeleteCmd, ppNumberCmd)
	messagingSmsCmd.AddCommand(powerpackCmd)
}

func runPowerpackList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(ppListLimit))
	q.Set("offset", strconv.Itoa(ppListOffset))
	var resp api.PowerpackList
	apiErr, err := client.Do("GET", client.AccountURL("Powerpack"), nil, q, &resp)
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
	rows := [][]string{{"UUID", "NAME", "STICKY", "LOCAL", "APP_TYPE", "CREATED"}}
	for _, p := range resp.Objects {
		rows = append(rows, []string{p.UUID, p.Name, fmt.Sprintf("%v", p.StickySender), fmt.Sprintf("%v", p.LocalConnect), p.ApplicationType, p.CreatedOn})
	}
	return output.Table(os.Stdout, rows)
}

func runPowerpackGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var p api.Powerpack
	apiErr, err := client.Do("GET", client.AccountURL("Powerpack", id), nil, nil, &p)
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
		return output.JSONRaw(os.Stdout, p.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"uuid", p.UUID},
		{"name", p.Name},
		{"sticky_sender", fmt.Sprintf("%v", p.StickySender)},
		{"local_connect", fmt.Sprintf("%v", p.LocalConnect)},
		{"application_type", p.ApplicationType},
		{"application_id", p.ApplicationID},
		{"number_priority", p.NumberPriority},
		{"number_pool", p.NumberPoolUUID},
		{"created_on", p.CreatedOn},
	})
}

func runPowerpackCreate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"name":          ppCreateName,
		"sticky_sender": ppCreateStickySender,
		"local_connect": ppCreateLocalConnect,
	}
	if ppCreateApplicationType != "" {
		body["application_type"] = ppCreateApplicationType
	}
	if ppCreateApplicationID != "" {
		body["application_id"] = ppCreateApplicationID
	}
	if ppCreateNumberPriority != "" {
		body["number_priority"] = ppCreateNumberPriority
	}
	var resp struct {
		api.RawBody
		APIID   string `json:"api_id"`
		UUID    string `json:"uuid"`
		Message string `json:"message"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("Powerpack"), body, nil, &resp)
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
		{"uuid", resp.UUID},
		{"message", resp.Message},
	})
}

func runPowerpackUpdate(cmd *cobra.Command, args []string) error {
	id := args[0]
	body := map[string]any{}
	if ppUpdateName != "" {
		body["name"] = ppUpdateName
	}
	switch ppUpdateStickySender {
	case "true":
		body["sticky_sender"] = true
	case "false":
		body["sticky_sender"] = false
	case "":
	default:
		return clierr.BadFlag("sticky-sender", "must be 'true' or 'false'")
	}
	switch ppUpdateLocalConnect {
	case "true":
		body["local_connect"] = true
	case "false":
		body["local_connect"] = false
	case "":
	default:
		return clierr.BadFlag("local-connect", "must be 'true' or 'false'")
	}
	if len(body) == 0 {
		return clierr.BadInput("at least one of --name, --sticky-sender, --local-connect required")
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("Powerpack", id), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Updated powerpack %s: %s\n", id, resp.Message)
	return nil
}

func runPowerpackDelete(cmd *cobra.Command, args []string) error {
	id := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("delete powerpack " + id)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("Powerpack", id), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Deleted powerpack %s\n", id)
	return nil
}

func runPowerpackNumberList(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(ppNumListLimit))
	q.Set("offset", strconv.Itoa(ppNumListOffset))
	var resp api.PowerpackNumberList
	apiErr, err := client.Do("GET", client.AccountURL("Powerpack", id, "Number"), nil, q, &resp)
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
	rows := [][]string{{"NUMBER", "COUNTRY", "TYPE", "ADDED"}}
	for _, n := range resp.Objects {
		rows = append(rows, []string{n.Number, n.Country, n.Type, n.AddedOn})
	}
	return output.Table(os.Stdout, rows)
}

func runPowerpackNumberAdd(cmd *cobra.Command, args []string) error {
	id, number := args[0], args[1]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("Powerpack", id, "Number", number), nil, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Added %s to powerpack %s: %s\n", number, id, resp.Message)
	return nil
}

func runPowerpackNumberRemove(cmd *cobra.Command, args []string) error {
	id, number := args[0], args[1]
	if !yesFlag {
		return clierr.DestructiveRefused(fmt.Sprintf("remove %s from powerpack %s", number, id))
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("Powerpack", id, "Number", number), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Removed %s from powerpack %s\n", number, id)
	return nil
}
