package cmd

import (
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

func init() {
	f := callerIDListCmd.Flags()
	f.StringVar(&callerIDListAlias, "alias", "", "only the caller ID with this alias")
	f.StringVar(&callerIDListCountry, "country", "", "only this country, a two-letter code such as US")
	f.StringVar(&callerIDListSub, "subaccount", "", "only this subaccount's caller IDs (its auth ID)")
	registerListFlags(callerIDListCmd, &callerIDListLimit, &callerIDListOffset)

	callerIDCmd.AddCommand(callerIDListCmd, callerIDGetCmd)
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
