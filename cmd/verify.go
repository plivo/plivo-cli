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

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Plivo Verify — OTP / phone-number verification sessions",
}

var verifySessionCmd = &cobra.Command{
	Use:     "sessions",
	Aliases: []string{"session"},
	Short:   "Manage Verify sessions",
}

var (
	vsCreateRecipient   string
	vsCreateAppUUID     string
	vsCreateChannel     string
	vsCreateAlphaSender string
	vsCreateLocale      string
	vsCreateMethod      string
	vsCreateURL         string
)

var verifySessionCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a Verify session (spends money — requires --yes)",
	RunE:  runVerifySessionCreate,
}

var verifySessionGetCmd = &cobra.Command{
	Use:   "get <session_uuid>",
	Short: "Get a Verify session by uuid",
	Args:  cobra.ExactArgs(1),
	RunE:  runVerifySessionGet,
}

var (
	vsListLimit  int
	vsListOffset int
	vsListStatus string
)

var verifySessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List Verify sessions",
	RunE:  runVerifySessionList,
}

var vsValidateOTP string

var verifySessionValidateCmd = &cobra.Command{
	Use:   "validate <session_uuid>",
	Short: "Submit the OTP to validate a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runVerifySessionValidate,
}

func init() {
	verifySessionCreateCmd.Flags().StringVar(&vsCreateRecipient, "recipient", "", "E.164 destination number (required)")
	_ = verifySessionCreateCmd.MarkFlagRequired("recipient")
	verifySessionCreateCmd.Flags().StringVar(&vsCreateAppUUID, "app-uuid", "", "Verify application uuid (required)")
	_ = verifySessionCreateCmd.MarkFlagRequired("app-uuid")
	verifySessionCreateCmd.Flags().StringVar(&vsCreateChannel, "channel", "sms", "delivery channel: sms|voice|whatsapp")
	verifySessionCreateCmd.Flags().StringVar(&vsCreateAlphaSender, "alpha-sender", "", "alphanumeric sender id")
	verifySessionCreateCmd.Flags().StringVar(&vsCreateLocale, "locale", "", "BCP-47 locale, e.g. en-US")
	verifySessionCreateCmd.Flags().StringVar(&vsCreateMethod, "method", "", "HTTP method for callback URL")
	verifySessionCreateCmd.Flags().StringVar(&vsCreateURL, "url", "", "callback URL for session status events")

	verifySessionListCmd.Flags().IntVar(&vsListLimit, "limit", 20, "results per page")
	verifySessionListCmd.Flags().IntVar(&vsListOffset, "offset", 0, "pagination offset")
	verifySessionListCmd.Flags().StringVar(&vsListStatus, "status", "", "filter by status: pending|verified|expired")

	verifySessionValidateCmd.Flags().StringVar(&vsValidateOTP, "otp", "", "OTP code received by the recipient (required)")
	_ = verifySessionValidateCmd.MarkFlagRequired("otp")

	verifySessionCmd.AddCommand(verifySessionCreateCmd, verifySessionGetCmd, verifySessionListCmd, verifySessionValidateCmd)
	verifyCmd.AddCommand(verifySessionCmd)
	rootCmd.AddCommand(verifyCmd)
}

func runVerifySessionCreate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"recipient": vsCreateRecipient,
		"app_uuid":  vsCreateAppUUID,
		"channel":   vsCreateChannel,
	}
	if vsCreateAlphaSender != "" {
		body["alpha_sender"] = vsCreateAlphaSender
	}
	if vsCreateLocale != "" {
		body["locale"] = vsCreateLocale
	}
	if vsCreateMethod != "" {
		body["method"] = vsCreateMethod
	}
	if vsCreateURL != "" {
		body["url"] = vsCreateURL
	}

	proceed, dryRun, gerr := guardSpend("create verify session for " + vsCreateRecipient)
	if !proceed {
		return gerr
	}
	applyDryRun(client, dryRun)

	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will POST %s (recipient=%s channel=%s)\n", client.AccountURL("Verify", "Session"), vsCreateRecipient, vsCreateChannel)
	}

	var resp struct {
		APIID       string `json:"api_id"`
		SessionUUID string `json:"session_uuid"`
		Message     string `json:"message"`
		APIError    string `json:"api_error,omitempty"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("Verify", "Session"), body, nil, &resp)
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
		{"session_uuid", resp.SessionUUID},
		{"message", resp.Message},
	})
}

func runVerifySessionGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var s api.VerifySession
	apiErr, err := client.Do("GET", client.AccountURL("Verify", "Session", id), nil, nil, &s)
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
		return output.JSONSuccess(os.Stdout, s, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"session_uuid", s.SessionUUID},
		{"app_uuid", s.AppUUID},
		{"recipient", s.Recipient},
		{"channel", s.Channel},
		{"status", s.Status},
		{"attempts", strconv.Itoa(s.CountOfAttempts)},
		{"locale", s.LocaleUsed},
		{"country", s.DestinationCountryISO2},
		{"charge", s.ChargeAmount + " " + s.ChargeAmountCurrency},
		{"created", s.CreationTime},
	})
}

func runVerifySessionList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(vsListLimit))
	q.Set("offset", strconv.Itoa(vsListOffset))
	if vsListStatus != "" {
		q.Set("status", vsListStatus)
	}
	var resp api.VerifySessionList
	apiErr, err := client.Do("GET", client.AccountURL("Verify", "Session"), nil, q, &resp)
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
		return output.JSONSuccess(os.Stdout, resp.Objects, resp.Meta)
	}
	rows := [][]string{{"SESSION_UUID", "RECIPIENT", "CHANNEL", "STATUS", "ATTEMPTS", "CREATED"}}
	for _, s := range resp.Objects {
		rows = append(rows, []string{s.SessionUUID, s.Recipient, s.Channel, s.Status, strconv.Itoa(s.CountOfAttempts), s.CreationTime})
	}
	return output.Table(os.Stdout, rows)
}

func runVerifySessionValidate(cmd *cobra.Command, args []string) error {
	id := args[0]
	if vsValidateOTP == "" {
		return clierr.BadInput("--otp is required")
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{"otp": vsValidateOTP}
	var resp struct {
		APIID    string `json:"api_id"`
		Message  string `json:"message"`
		Verified bool   `json:"verified,omitempty"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("Verify", "Session", id, "Validation"), body, nil, &resp)
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
		return output.JSONSuccess(os.Stdout, resp, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"message", resp.Message},
		{"verified", fmt.Sprintf("%v", resp.Verified)},
	})
}
