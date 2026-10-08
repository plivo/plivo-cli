package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var numberCmd = &cobra.Command{
	Use:     "numbers",
	Aliases: []string{"number"},
	Short:   "Manage account phone numbers",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
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
	numberUpdateTrunkID    string
	numberUpdateAlias      string
	numberUpdateSubaccount string
	numberUpdateForce      bool
)

var numberUpdateCmd = &cobra.Command{
	Use:   "update <number>",
	Short: "Update settings on a rented number",
	Long: `Update settings on a rented number.

Routing an India (+91) number with --app-id or --trunk-id first reads its
compliance application. The update is refused when that application is not
accepted or cannot be read, and goes ahead with a warning when none is
attached. --force skips only this check; Plivo still enforces KYC.`,
	Args: cobra.ExactArgs(1),
	RunE: runNumberUpdate,
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

var (
	numberBuyAppID        string
	numberBuyComplianceID string
)

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
	numberUpdateCmd.Flags().StringVar(&numberUpdateTrunkID, "trunk-id", "", "route the number to an inbound SIP trunk")
	numberUpdateCmd.Flags().StringVar(&numberUpdateAlias, "alias", "", "set alias")
	numberUpdateCmd.Flags().StringVar(&numberUpdateSubaccount, "subaccount", "", "move under subaccount")
	numberUpdateCmd.Flags().BoolVar(&numberUpdateForce, "force", false, "skip the India compliance check on --app-id/--trunk-id (Plivo still enforces KYC)")

	numberSearchCmd.Flags().StringVar(&numberSearchCountry, "country", "", "ISO country code, e.g. US (required)")
	_ = numberSearchCmd.MarkFlagRequired("country")
	numberSearchCmd.Flags().StringVar(&numberSearchType, "type", "", "local|tollfree|mobile|fixed")
	numberSearchCmd.Flags().StringVar(&numberSearchPattern, "pattern", "", "digit pattern")
	numberSearchCmd.Flags().StringVar(&numberSearchRegion, "region", "", "region filter")
	numberSearchCmd.Flags().IntVar(&numberSearchLimit, "limit", 20, "results per page")
	numberSearchCmd.Flags().IntVar(&numberSearchOffset, "offset", 0, "pagination offset")

	numberBuyCmd.Flags().StringVar(&numberBuyAppID, "app-id", "", "auto-attach to this application after purchase")
	numberBuyCmd.Flags().StringVar(&numberBuyComplianceID, "compliance-application-id", "", "accepted compliance application to attach; if unset, Plivo picks your most recent applicable one")
	registerExplainFlag(numberBuyCmd)
	registerExplainFlag(numberReleaseCmd)

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
	if numberUpdateAppID != "" && numberUpdateTrunkID != "" {
		return clierr.BadInput("--app-id and --trunk-id both set the same field; pass one")
	}
	body := map[string]any{}
	if numberUpdateAppID != "" {
		body["app_id"] = numberUpdateAppID
	}
	if numberUpdateTrunkID != "" {
		// The API takes a trunk in app_id. --trunk-id exists so nobody has to
		// know that a trunk goes in a flag named after applications.
		if err := requireInboundTrunk(client, numberUpdateTrunkID); err != nil {
			return err
		}
		body["app_id"] = numberUpdateTrunkID
	}
	if numberUpdateAlias != "" {
		body["alias"] = numberUpdateAlias
	}
	if numberUpdateSubaccount != "" {
		body["subaccount"] = numberUpdateSubaccount
	}
	if len(body) == 0 {
		return clierr.BadInput("pass at least one of --app-id, --trunk-id, --alias, --subaccount")
	}
	if _, routing := body["app_id"]; routing && !numberUpdateForce {
		warning, err := checkIndiaCompliance(client, number)
		if err != nil {
			return err
		}
		if warning != "" {
			fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
		}
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
	if id := strings.TrimSpace(numberBuyComplianceID); id != "" {
		body["compliance_application_id"] = id
	}
	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will POST %s (rent number %s)\n", client.AccountURL("PhoneNumber", number), number)
	}
	// POST /Account/{auth_id}/PhoneNumber/{number}/
	var resp struct {
		api.RawBody
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
		return output.JSONRaw(os.Stdout, resp.Raw())
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

// requireInboundTrunk refuses an outbound trunk. Numbers receive calls, and an
// outbound trunk has no origination URI to send them to, so the attach would
// succeed and the number would simply stop answering.
func requireInboundTrunk(client *api.Client, trunkID string) error {
	var t api.SIPTrunk
	var apiErr *api.APIError
	// Must read even under --dry-run, or the preview shows a POST the real run
	// refuses. A GET is not a write.
	if err := readThrough(client, func() error {
		var e error
		apiErr, e = client.Do("GET", client.AccountURL("Zentrunk", "Trunk", trunkID), nil, nil, &t)
		return e
	}); err != nil {
		return nil // transport trouble: let the API have the final say
	}
	if apiErr != nil {
		if apiErr.StatusCode == http.StatusNotFound {
			return &clierr.Error{
				Code:       clierr.CodeResourceNotFound,
				Message:    fmt.Sprintf("trunk %s not found on this account", trunkID),
				Hint:       "`plivo sip trunks list` shows the trunks you have.",
				StatusCode: http.StatusNotFound,
			}
		}
		return apiErr
	}
	if t = unwrapSIPTrunk(t); t.TrunkDirection == dirOutbound {
		return clierr.BadInput(fmt.Sprintf(
			"trunk %s is outbound; a number can only be routed to an inbound trunk", trunkID))
	}
	return nil
}

// indiaKYCDocsURL is Plivo's India KYC guide; the API's own compliance errors
// link to it.
const indiaKYCDocsURL = "https://www.plivo.com/docs/numbers/rent-india-numbers"

// checkIndiaCompliance is the India KYC rule for routing a rented number to an
// application or a trunk. Commands that route a number call it rather than
// restating the rule.
//
// Numbers outside +91 pass without a request. For an India number it reads the
// number and, when one is attached, its compliance application:
//
//   - accepted: no warning, no error
//   - none attached: a warning for the caller to print; routing goes ahead
//   - any other status, or any read failing: a refusal whose hint lists the
//     KYC steps
//
// Both reads go through readThrough, so --dry-run runs the same check.
func checkIndiaCompliance(client *api.Client, number string) (warning string, err error) {
	// E.164 digits; no other country code starts with 91.
	if !strings.HasPrefix(strings.TrimPrefix(strings.TrimSpace(number), "+"), "91") {
		return "", nil
	}
	var n struct {
		// Not in the API docs, but on every number record: null when no
		// application is attached. Absent is not the same as null.
		ComplianceID json.RawMessage `json:"compliance_application_id"`
	}
	if e := getForCheck(client, client.AccountURL("Number", number), &n); e != nil {
		e.Message = "could not read the number to check its compliance application: " + e.Message
		return "", kycRefusal(e, number, "")
	}
	var id string
	if json.Unmarshal(n.ComplianceID, &id) != nil {
		return "", kycRefusal(&clierr.Error{Code: clierr.CodeValidation,
			Message: "the number's record has no readable compliance_application_id"}, number, "")
	}
	if id == "" {
		return fmt.Sprintf("%s has no compliance application attached; Plivo needs an accepted one before an India number can place calls. "+
			"`plivo numbers compliance list --country IN --status accepted` lists yours, and "+
			"`plivo numbers compliance link --link %s=<compliance_id>` attaches one.", number, number), nil
	}
	var app struct {
		Status string `json:"status"`
		// The documented response nests the application under "compliance".
		Compliance struct {
			Status string `json:"status"`
		} `json:"compliance"`
	}
	if e := getForCheck(client, client.AccountURL("PhoneNumber", "Compliance", id), &app); e != nil {
		e.Message = fmt.Sprintf("could not read its compliance application %s: %s", id, e.Message)
		return "", kycRefusal(e, number, id)
	}
	status := app.Compliance.Status
	if status == "" {
		status = app.Status
	}
	switch {
	case status == "":
		return "", kycRefusal(&clierr.Error{Code: clierr.CodeValidation,
			Message: fmt.Sprintf("its compliance application %s came back without a status", id)}, number, id)
	case !strings.EqualFold(status, "accepted"):
		return "", kycRefusal(&clierr.Error{Code: clierr.CodeValidation,
			Message: fmt.Sprintf("its compliance application %s is %q, not \"accepted\"", id, status)}, number, id)
	}
	return "", nil
}

// getForCheck reads endpoint into out, even under --dry-run, and returns a
// failed read of either kind (transport or API) as one error.
func getForCheck(client *api.Client, endpoint string, out any) *clierr.Error {
	var apiErr *api.APIError
	if err := readThrough(client, func() error {
		var e error
		apiErr, e = client.Do("GET", endpoint, nil, nil, out)
		return e
	}); err != nil {
		return clierr.Wrap(err)
	}
	return apiErr
}

// kycRefusal turns e into the refusal of a routing change: the reason in the
// message, and the India KYC steps, as CLI commands, in the hint.
func kycRefusal(e *clierr.Error, number, appID string) *clierr.Error {
	e.Message = fmt.Sprintf("refusing to route %s: %s", number, e.Message)
	see := "`plivo numbers compliance list --country IN` lists your applications"
	if appID != "" {
		see = fmt.Sprintf("`plivo numbers compliance get %s` shows its status and any rejection reason", appID)
	}
	e.Hint = fmt.Sprintf("India numbers need an accepted compliance application (KYC): 1) %s; "+
		"2) `plivo numbers compliance create --data @app.json --file documents[0].file=@doc.pdf` submits one "+
		"(`update <compliance_id>` with the same flags resubmits a rejected one); "+
		"3) `plivo numbers compliance link --link %s=<compliance_id>` attaches an accepted one. "+
		"--force skips this check; Plivo still enforces KYC.", see, number)
	e.DocsURL = indiaKYCDocsURL
	return e
}
