package cmd

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var callerIDCmd = &cobra.Command{
	Use:     "caller-ids",
	Aliases: []string{"caller-id"},
	Short:   "Verified caller IDs: numbers you may show as the caller of an outbound call",
	Long: `A verified caller ID is a number you do not rent from Plivo, proved yours with
a one-time passcode, that you may then show as the caller of an outbound call.
A number rented from Plivo needs no verification.`,
	Args: cobra.NoArgs,
	RunE: groupRunE,
}

var (
	callerIDListLimit, callerIDListOffset                   int
	callerIDListAlias, callerIDListCountry, callerIDListSub string
)

var callerIDListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List the account's verified caller IDs",
	Example: "  plivo voice caller-ids list\n  plivo voice caller-ids list --country IN --alias \"front desk\"",
	Args:    cobra.NoArgs,
	RunE:    runCallerIDList,
}

var callerIDGetCmd = &cobra.Command{
	Use:   "get <number>",
	Short: "Get a verified caller ID by its number",
	Long: `Get one verified caller ID. A number that is not one answers "not found": it
is not verified yet, or it belongs to another account.`,
	Example: "  plivo voice caller-ids get +14155550100",
	Args:    cobra.ExactArgs(1),
	RunE:    runCallerIDGet,
}

var callerIDChannels = []string{"sms", "call"}

var (
	callerIDCreateAlias, callerIDCreateChannel, callerIDCreateSub string
	callerIDVerifyOTP                                             string
)

var callerIDCreateCmd = &cobra.Command{
	Use:     "create <number>",
	Aliases: []string{"add"},
	Short:   "Start verifying a caller ID: sends a one-time passcode (requires --yes)",
	Long: `Start verifying a number as a caller ID. Plivo sends a one-time passcode to the
number, by SMS or, with --channel call, by a voice call, and answers with a
verification_uuid. Finish with plivo voice caller-ids verify <verification_uuid>
--otp <code>. The passcode is valid for 15 minutes.

The number is in E.164; the + is added if you leave it off. A number rented from
Plivo needs no verification. Plivo does not verify Indian (+91) numbers: it
refuses before sending anything.`,
	Example: `  plivo voice caller-ids create +14155550100 --alias "front desk" --yes
  plivo voice caller-ids create +14155550100 --channel call --yes`,
	Args: cobra.ExactArgs(1),
	RunE: runCallerIDCreate,
}

var callerIDVerifyCmd = &cobra.Command{
	Use:   "verify <verification_uuid>",
	Short: "Finish verifying a caller ID with the passcode it was sent",
	Long: `Submit the passcode that plivo voice caller-ids create sent. <verification_uuid>
is the one create printed. A wrong or expired passcode is Plivo's error, passed
on as it came; run create again for a new one.`,
	Example: "  plivo voice caller-ids verify 11111111-2222-3333-4444-555555555555 --otp 123456",
	Args:    cobra.ExactArgs(1),
	RunE:    runCallerIDVerify,
}

func init() {
	f := callerIDListCmd.Flags()
	f.StringVar(&callerIDListAlias, "alias", "", "only the caller ID with this alias")
	f.StringVar(&callerIDListCountry, "country", "", "only this country, a two-letter code such as US")
	f.StringVar(&callerIDListSub, "subaccount", "", "only this subaccount's caller IDs (its auth ID)")
	registerListFlags(callerIDListCmd, &callerIDListLimit, &callerIDListOffset)

	g := callerIDCreateCmd.Flags()
	g.StringVar(&callerIDCreateAlias, "alias", "", "a name for the caller ID")
	g.StringVar(&callerIDCreateChannel, "channel", "", "how the passcode is sent: "+oneOf(callerIDChannels)+" (Plivo's default: sms)")
	g.StringVar(&callerIDCreateSub, "subaccount", "", "the subaccount that gets the caller ID (its auth ID)")
	callerIDVerifyCmd.Flags().StringVar(&callerIDVerifyOTP, "otp", "", "the passcode Plivo sent (required)")
	_ = callerIDVerifyCmd.MarkFlagRequired("otp")

	callerIDCmd.AddCommand(callerIDListCmd, callerIDGetCmd, callerIDCreateCmd, callerIDVerifyCmd)
	voiceCmd.AddCommand(callerIDCmd)
}

func runCallerIDList(cmd *cobra.Command, args []string) error {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(callerIDListLimit))
	q.Set("offset", strconv.Itoa(callerIDListOffset))
	if v := strings.TrimSpace(callerIDListAlias); v != "" {
		q.Set("alias", v)
	}
	if cmd.Flags().Changed("country") {
		c := strings.ToUpper(strings.TrimSpace(callerIDListCountry))
		if !countryCode.MatchString(c) {
			e := clierr.BadFlag("country", fmt.Sprintf("must be a two-letter country code, got %q", callerIDListCountry))
			e.Hint = "Pass an ISO 3166-1 alpha-2 country code, such as US, GB or IN."
			return e
		}
		q.Set("country", c)
	}
	// Left out, an empty --subaccount would list the whole account; sent,
	// Plivo refuses it.
	if cmd.Flags().Changed("subaccount") {
		sub := strings.TrimSpace(callerIDListSub)
		if sub == "" {
			e := clierr.BadFlag("subaccount", "is empty")
			e.Hint = "Pass a subaccount's auth ID (`plivo account subaccounts list` shows them), or leave --subaccount out for the whole account."
			return e
		}
		q.Set("subaccount", sub)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.VerifiedCallerIDList
	if err := fetchList(client, client.AccountURL("VerifiedCallerId"), q, "objects", &resp); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return listJSON(os.Stdout, resp.Raw(), "objects")
	}
	var bySub bool
	for _, c := range resp.Objects {
		bySub = bySub || c.Subaccount != ""
	}
	rows := [][]string{{"NUMBER", "ALIAS", "COUNTRY", "CREATED", "VERIFICATION_UUID"}}
	if bySub {
		rows[0] = append(rows[0], "SUBACCOUNT")
	}
	for _, c := range resp.Objects {
		row := []string{c.PhoneNumber, dash(c.Alias), c.Country, c.CreatedAt, c.VerificationUUID}
		if bySub {
			row = append(row, dash(c.Subaccount))
		}
		rows = append(rows, row)
	}
	return output.Table(os.Stdout, rows)
}

func runCallerIDGet(cmd *cobra.Command, args []string) error {
	number := strings.TrimSpace(args[0])
	if !isE164Digits(strings.TrimPrefix(number, "+")) {
		e := clierr.BadInput(fmt.Sprintf("%q is not a phone number", args[0]))
		e.Hint = "Pass the number in E.164, such as +14155550100."
		return e
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var c api.VerifiedCallerID
	apiErr, err := client.Do("GET", client.AccountURL("VerifiedCallerId", number), nil, nil, &c)
	if err != nil {
		return err
	}
	if apiErr != nil {
		// Plivo answers a number that is not verified with HTTP 400 and
		// "Phone Number not found", not a 404.
		if apiErr.StatusCode == http.StatusNotFound ||
			apiErr.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(apiErr.Message), "phone number not found") {
			apiErr.Code = clierr.CodeResourceNotFound
			apiErr.Message = number + " is not a verified caller ID on this account"
			apiErr.Hint = "`plivo voice caller-ids list` shows the verified ones. A number rented from Plivo needs no verification: `plivo numbers list` shows those."
		}
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, c.Raw())
	}
	kv := [][2]string{
		{"phone_number", c.PhoneNumber},
		{"alias", c.Alias},
		{"country", c.Country},
		{"verification_uuid", c.VerificationUUID},
		{"created_at", c.CreatedAt},
		{"modified_at", c.ModifiedAt},
	}
	if c.Subaccount != "" {
		kv = append(kv, [2]string{"subaccount", c.Subaccount})
	}
	return output.KV(os.Stdout, kv)
}

// verificationUUID is the form of a verification_uuid.
var verificationUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func runCallerIDCreate(cmd *cobra.Command, args []string) error {
	// E.164 starts with +. At least 7 digits keeps a typo from sending a
	// passcode to a stranger.
	digits := strings.TrimPrefix(strings.TrimSpace(args[0]), "+")
	if !isE164Digits(digits) || len(digits) < 7 {
		e := clierr.BadInput(fmt.Sprintf("%q is not a phone number", args[0]))
		e.Hint = "Pass the number in E.164, such as +14155550100."
		return e
	}
	number := "+" + digits
	if err := validateEnum("channel", &callerIDCreateChannel, callerIDChannels...); err != nil {
		return err
	}
	body := map[string]any{"phone_number": number}
	if v := strings.TrimSpace(callerIDCreateAlias); v != "" {
		body["alias"] = v
	}
	if callerIDCreateChannel != "" {
		body["channel"] = callerIDCreateChannel
	}
	if cmd.Flags().Changed("subaccount") {
		sub := strings.TrimSpace(callerIDCreateSub)
		if sub == "" {
			e := clierr.BadFlag("subaccount", "is empty")
			e.Hint = "Pass a subaccount's auth ID (`plivo account subaccounts list` shows them), or leave --subaccount out."
			return e
		}
		body["subaccount"] = sub
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	proceed, dryRun, gerr := guardSpend("verify caller ID " + number + " (sends it a passcode)")
	if !proceed {
		return gerr
	}
	applyDryRun(client, dryRun)

	var resp struct {
		api.RawBody
		APIID            string `json:"api_id"`
		Message          string `json:"message"`
		VerificationUUID string `json:"verification_uuid"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("VerifiedCallerId"), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		// Seen live: HTTP 400 "verification of Indian number is not allowed,
		// use Plivo's India numbers for traffic in India." Nothing is sent.
		if apiErr.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(apiErr.Message), "indian number is not allowed") {
			apiErr.Hint = "Plivo does not verify Indian (+91) numbers as caller IDs. For traffic in India use a Plivo India number: " +
				"`plivo numbers search --country IN` finds one."
		}
		return apiErr
	}
	if dryRun {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, resp.Raw())
	}
	if err := output.KV(os.Stdout, [][2]string{
		{"verification_uuid", resp.VerificationUUID},
		{"message", resp.Message},
	}); err != nil {
		return err
	}
	if !quietFlag {
		fmt.Fprintf(os.Stderr, "Finish with: plivo voice caller-ids verify %s --otp <code>\n", resp.VerificationUUID)
	}
	return nil
}

func runCallerIDVerify(cmd *cobra.Command, args []string) error {
	id := strings.TrimSpace(args[0])
	if !verificationUUID.MatchString(id) {
		e := clierr.BadInput(fmt.Sprintf("%q is not a verification_uuid", args[0]))
		e.Hint = "Use the verification_uuid that `plivo voice caller-ids create` printed."
		return e
	}
	otp := strings.TrimSpace(callerIDVerifyOTP)
	if len(otp) > 16 || strings.ContainsAny(otp, " \t\r\n") {
		e := clierr.BadFlag("otp", "must be the passcode alone, without spaces")
		e.Hint = "It is the code Plivo sent to the number by SMS or voice call."
		return e
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("VerifiedCallerId", "Verification", id), map[string]any{"otp": otp}, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		// Seen live: HTTP 400 "session_uuid <uuid> is not an active session"
		// for a verification that never started, has expired or is done.
		if apiErr.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(apiErr.Message), "not an active session") {
			apiErr.Hint = "That verification is not active: it never started, it expired (a passcode lasts 15 minutes) or it is already done. " +
				"`plivo voice caller-ids create <number> --yes` sends a new passcode."
		}
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, resp.Raw())
	}
	return output.KV(os.Stdout, [][2]string{{"message", resp.Message}})
}
