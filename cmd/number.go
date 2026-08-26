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

var numberCmd = &cobra.Command{
	Use:     "numbers",
	Aliases: []string{"number"},
	Short:   "Manage account phone numbers",
}

var (
	numberListType       string
	numberListStartswith string
	numberListSubaccount string
	numberListAlias      string
	numberListServices   string
	numberListLimit      int
	numberListOffset     int
)

var numberListCmd = &cobra.Command{
	Use:   "list",
	Short: "List numbers rented to your account",
	RunE:  runNumberList,
}

var numberGetCmd = &cobra.Command{
	Use:   "get <number>",
	Short: "Get details of a rented number",
	Args:  cobra.ExactArgs(1),
	RunE:  runNumberGet,
}

var (
	numberUpdateAppID      string
	numberUpdateAlias      string
	numberUpdateSubaccount string
)

var numberUpdateCmd = &cobra.Command{
	Use:   "update <number>",
	Short: "Update settings on a rented number",
	Args:  cobra.ExactArgs(1),
	RunE:  runNumberUpdate,
}

var (
	numberSearchCountry string
	numberSearchType    string
	numberSearchPattern string
	numberSearchRegion  string
	numberSearchLimit   int
	numberSearchOffset  int
)

var numberSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search available numbers to rent",
	RunE:  runNumberSearch,
}

var numberBuyAppID string

var numberBuyCmd = &cobra.Command{
	Use:   "buy <number>",
	Short: "Rent a phone number (requires --yes; spends money)",
	Args:  cobra.ExactArgs(1),
	RunE:  runNumberBuy,
}

var numberReleaseCmd = &cobra.Command{
	Use:   "release <number>",
	Short: "Release a rented number (requires --yes; stops monthly billing)",
	Args:  cobra.ExactArgs(1),
	RunE:  runNumberRelease,
}

func init() {
	numberListCmd.Flags().StringVar(&numberListType, "type", "", "filter by type: local|tollfree|mobile|fixed")
	numberListCmd.Flags().StringVar(&numberListStartswith, "starts-with", "", "prefix filter on E.164")
	numberListCmd.Flags().StringVar(&numberListSubaccount, "subaccount", "", "filter by subaccount auth_id")
	numberListCmd.Flags().StringVar(&numberListAlias, "alias", "", "filter by alias")
	numberListCmd.Flags().StringVar(&numberListServices, "services", "", "filter by services: voice|sms|mms|voice,sms ...")
	numberListCmd.Flags().IntVar(&numberListLimit, "limit", 20, "results per page (max 20)")
	numberListCmd.Flags().IntVar(&numberListOffset, "offset", 0, "pagination offset")

	numberUpdateCmd.Flags().StringVar(&numberUpdateAppID, "app-id", "", "associate an application")
	numberUpdateCmd.Flags().StringVar(&numberUpdateAlias, "alias", "", "set alias")
	numberUpdateCmd.Flags().StringVar(&numberUpdateSubaccount, "subaccount", "", "move under subaccount")

	numberSearchCmd.Flags().StringVar(&numberSearchCountry, "country", "", "ISO country code, e.g. US (required)")
	_ = numberSearchCmd.MarkFlagRequired("country")
	numberSearchCmd.Flags().StringVar(&numberSearchType, "type", "", "local|tollfree|mobile|fixed")
	numberSearchCmd.Flags().StringVar(&numberSearchPattern, "pattern", "", "digit pattern")
	numberSearchCmd.Flags().StringVar(&numberSearchRegion, "region", "", "region filter")
	numberSearchCmd.Flags().IntVar(&numberSearchLimit, "limit", 20, "results per page")
	numberSearchCmd.Flags().IntVar(&numberSearchOffset, "offset", 0, "pagination offset")

	numberBuyCmd.Flags().StringVar(&numberBuyAppID, "app-id", "", "auto-attach to this application after purchase")

	numberCmd.AddCommand(numberListCmd, numberGetCmd, numberUpdateCmd, numberSearchCmd, numberBuyCmd, numberReleaseCmd)
	rootCmd.AddCommand(numberCmd)
}

func runNumberRelease(cmd *cobra.Command, args []string) error {
	number := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("release number " + number)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will DELETE %s\n", client.AccountURL("Number", number))
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("Number", number), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Released %s\n", number)
	return nil
}

func runNumberList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	if numberListType != "" {
		q.Set("type", numberListType)
	}
	if numberListStartswith != "" {
		q.Set("number_startswith", numberListStartswith)
	}
	if numberListSubaccount != "" {
		q.Set("subaccount", numberListSubaccount)
	}
	if numberListAlias != "" {
		q.Set("alias", numberListAlias)
	}
	if numberListServices != "" {
		q.Set("services", numberListServices)
	}
	q.Set("limit", strconv.Itoa(numberListLimit))
	q.Set("offset", strconv.Itoa(numberListOffset))

	var resp api.NumberList
	apiErr, err := client.Do("GET", client.AccountURL("Number"), nil, q, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	return renderNumberList(resp)
}

func renderNumberList(resp api.NumberList) error {
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, resp.Raw())
	}
	rows := [][]string{{"NUMBER", "TYPE", "COUNTRY", "APP_ID", "ALIAS"}}
	for _, n := range resp.Objects {
		rows = append(rows, []string{n.Number, n.Type, n.Country, n.ResolvedAppID(), n.Alias})
	}
	return output.Table(os.Stdout, rows)
}

func runNumberGet(cmd *cobra.Command, args []string) error {
	number := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var n api.Number
	apiErr, err := client.Do("GET", client.AccountURL("Number", number), nil, nil, &n)
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
		return output.JSONRaw(os.Stdout, n.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"number", n.Number},
		{"type", n.Type},
		{"country", n.Country},
		{"region", n.Region},
		{"app_id", n.ResolvedAppID()},
		{"alias", n.Alias},
		{"voice_enabled", fmt.Sprintf("%v", n.VoiceEnabled)},
		{"sms_enabled", fmt.Sprintf("%v", n.SMSEnabled)},
		{"monthly_rental", n.MonthlyRental},
		{"renewal_date", n.RenewalDate},
	})
}

func runNumberUpdate(cmd *cobra.Command, args []string) error {
	number := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{}
	if numberUpdateAppID != "" {
		body["app_id"] = numberUpdateAppID
	}
	if numberUpdateAlias != "" {
		body["alias"] = numberUpdateAlias
	}
	if numberUpdateSubaccount != "" {
		body["subaccount"] = numberUpdateSubaccount
	}
	if len(body) == 0 {
		return fmt.Errorf("at least one of --app-id, --alias, --subaccount required")
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("Number", number), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Updated %s: %s\n", number, resp.Message)
	return nil
}

func runNumberBuy(cmd *cobra.Command, args []string) error {
	number := args[0]
	proceed, dryRun, gerr := guardSpend("buy number " + number)
	if !proceed {
		return gerr
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	applyDryRun(client, dryRun)
	body := map[string]any{}
	if numberBuyAppID != "" {
		body["app_id"] = numberBuyAppID
	}
	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will POST %s (rent number %s)\n", client.AccountURL("PhoneNumber", number), number)
	}
	// POST /Account/{auth_id}/PhoneNumber/{number}/
	var resp struct {
		APIID   string `json:"api_id"`
		Status  string `json:"status"`
		Message string `json:"message"`
		Numbers []struct {
			Number string `json:"number"`
			Status string `json:"status"`
		} `json:"numbers"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("PhoneNumber", number), body, nil, &resp)
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
		return output.JSONSuccess(os.Stdout, resp, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"number", number},
		{"status", resp.Status},
		{"message", resp.Message},
	})
}

func runNumberSearch(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("country_iso", numberSearchCountry)
	if numberSearchType != "" {
		q.Set("type", numberSearchType)
	}
	if numberSearchPattern != "" {
		q.Set("pattern", numberSearchPattern)
	}
	if numberSearchRegion != "" {
		q.Set("region", numberSearchRegion)
	}
	q.Set("limit", strconv.Itoa(numberSearchLimit))
	q.Set("offset", strconv.Itoa(numberSearchOffset))

	var resp api.NumberList
	apiErr, err := client.Do("GET", client.AccountURL("PhoneNumber"), nil, q, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	return renderNumberList(resp)
}
