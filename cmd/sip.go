package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// SIP Trunking lives under the `Zentrunk` API path for historical reasons. The
// product name is SIP Trunking, so nothing user-facing here says otherwise.

// maxSIPCallLimit is the API's ceiling. Enforced locally so an over-large
// --limit fails immediately instead of costing a round-trip to learn the same.
const maxSIPCallLimit = 20

// sipHangupCodesDocsURL is the Zentrunk hangup-code reference.
const sipHangupCodesDocsURL = "https://www.plivo.com/docs/sip-trunking/troubleshooting/zentrunk-hangup-codes"

var sipCmd = &cobra.Command{
	Use:     "sip",
	Aliases: []string{"sip-trunking"},
	Short:   "SIP Trunking — trunks, CDRs, access control",
	Long: `Inspect SIP Trunking traffic and configuration.

For SIP endpoints (registered devices/usernames) see ` + "`plivo voice endpoints`" + ` —
that is a different product.`,
	Args: cobra.NoArgs,
	RunE: groupRunE,
}

var sipCallsCmd = &cobra.Command{
	Use:     "calls",
	Aliases: []string{"call", "cdrs"},
	Short:   "Inspect SIP Trunking calls (CDRs)",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var sipTrunksCmd = &cobra.Command{
	Use:     "trunks",
	Aliases: []string{"trunk"},
	Short:   "Inbound and outbound trunks",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var sipACLCmd = &cobra.Command{
	Use:     "ip-acl",
	Aliases: []string{"acl", "ipacl"},
	Short:   "IP access control lists",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var (
	sipCallsLimit      int
	sipCallsOffset     int
	sipCallsFrom       string
	sipCallsTo         string
	sipCallsDirection  string
	sipCallsSince      string
	sipCallsUntil      string
	sipCallsCauseCode  int
	sipCallsSource     string
	sipCallsSTIR       string
	sipTrunksLimit     int
	sipTrunksOffset    int
	sipTrunksDirection string
	sipACLLimit        int
	sipACLOffset       int
)

var sipCallsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List SIP Trunking calls",
	Example: `  plivo sip calls list --limit 20
  plivo sip calls list --direction outbound --since 2026-09-01
  plivo sip calls list --hangup-source carrier -o json`,
	RunE: runSIPCallsList,
}

var sipCallsDiagnoseCmd = &cobra.Command{
	Use:     "diagnose <call_uuid>",
	Aliases: []string{"diag"},
	Short:   "Diagnose what happened on a SIP Trunking call (AI-powered)",
	Long: `Diagnose a SIP Trunking call by UUID. Asks Plivo's AI assistant to pull the
SIP ladder and trunk configuration and explain what happened.

Only SIP Trunking calls. For a Voice call use ` + "`plivo voice calls diagnose`" + `:
the two read different stores, so neither can answer for the other.`,
	Example: `  plivo sip calls diagnose 8f3c1a2e-0d44-4a6f-9c31-2b7e5a90d1f7`,
	Args:    cobra.ExactArgs(1),
	RunE:    runSIPCallsDiagnose,
}

var sipCallsGetCmd = &cobra.Command{
	Use:     "get <call_uuid>",
	Short:   "Get one SIP Trunking call by UUID",
	Example: `  plivo sip calls get 8f3c1a2e-0d44-4a6f-9c31-2b7e5a90d1f7`,
	Args:    cobra.ExactArgs(1),
	RunE:    runSIPCallsGet,
}

var sipTrunksListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List trunks",
	Example: `  plivo sip trunks list --direction outbound`,
	RunE:    runSIPTrunksList,
}

var sipTrunksGetCmd = &cobra.Command{
	Use:   "get <trunk_id>",
	Short: "Get one trunk by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runSIPTrunksGet,
}

var sipACLListCmd = &cobra.Command{
	Use:   "list",
	Short: "List IP access control lists",
	RunE:  runSIPACLList,
}

var sipACLGetCmd = &cobra.Command{
	Use:   "get <ipacl_uuid>",
	Short: "Get one IP access control list by UUID",
	Args:  cobra.ExactArgs(1),
	RunE:  runSIPACLGet,
}

func init() {
	f := sipCallsListCmd.Flags()
	f.IntVar(&sipCallsLimit, "limit", maxSIPCallLimit, "rows to return (1-20)")
	f.IntVar(&sipCallsOffset, "offset", 0, "rows to skip")
	f.StringVar(&sipCallsFrom, "from-number", "", "filter by caller ID")
	f.StringVar(&sipCallsTo, "to-number", "", "filter by destination")
	f.StringVar(&sipCallsDirection, "direction", "", "inbound|outbound")
	f.StringVar(&sipCallsSince, "since", "", "calls ending at or after this UTC time (YYYY-MM-DD[ HH:MM[:SS]])")
	f.StringVar(&sipCallsUntil, "until", "", "calls ending at or before this UTC time (YYYY-MM-DD[ HH:MM[:SS]])")
	f.IntVar(&sipCallsCauseCode, "hangup-cause-code", 0, "filter by numeric hangup cause code")
	f.StringVar(&sipCallsSource, "hangup-source", "", "who ended the call: customer|carrier|zentrunk")
	f.StringVar(&sipCallsSTIR, "stir-verification", "", `"Verified"|"Not Verified"|"Not Applicable"`)

	tf := sipTrunksListCmd.Flags()
	tf.IntVar(&sipTrunksLimit, "limit", 20, "rows to return")
	tf.IntVar(&sipTrunksOffset, "offset", 0, "rows to skip")
	tf.StringVar(&sipTrunksDirection, "direction", "", "inbound|outbound")

	af := sipACLListCmd.Flags()
	af.IntVar(&sipACLLimit, "limit", 20, "rows to return")
	af.IntVar(&sipACLOffset, "offset", 0, "rows to skip")

	sipCallsCmd.AddCommand(sipCallsListCmd, sipCallsGetCmd, sipCallsDiagnoseCmd)
	sipTrunksCmd.AddCommand(sipTrunksListCmd, sipTrunksGetCmd)
	sipACLCmd.AddCommand(sipACLListCmd, sipACLGetCmd)
	sipCmd.AddCommand(sipCallsCmd, sipTrunksCmd, sipACLCmd)
	rootCmd.AddCommand(sipCmd)
}

// sipTimeLayouts are what --since/--until accept. The API itself only takes
// "YYYY-MM-DD HH:MM[:SS]", so a bare date is widened here rather than sent
// through to fail upstream.
var sipTimeLayouts = []string{"2006-01-02 15:04:05", "2006-01-02 15:04", time.RFC3339}

const sipAPITimeLayout = "2006-01-02 15:04:05"

// normalizeSIPTime widens a bare date to a full timestamp. endOfDay picks the
// last second of that day so --until includes the day the user named.
func normalizeSIPTime(v string, endOfDay bool) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		if endOfDay {
			t = t.Add(24*time.Hour - time.Second)
		}
		return t.Format(sipAPITimeLayout), nil
	}
	for _, layout := range sipTimeLayouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC().Format(sipAPITimeLayout), nil
		}
	}
	return "", clierr.BadInput(fmt.Sprintf(
		"could not read %q as a time — use YYYY-MM-DD, YYYY-MM-DD HH:MM, or YYYY-MM-DD HH:MM:SS (UTC)", v))
}

// trimPlus drops a leading "+" from a number filter. The CDR carries the number
// in +E.164, but the filter only matches it without the "+", so pasting a number
// straight out of `sip calls list` would silently return nothing.
func trimPlus(number string) string {
	return strings.TrimPrefix(strings.TrimSpace(number), "+")
}

func runSIPCallsList(cmd *cobra.Command, args []string) error {
	if sipCallsLimit < 1 || sipCallsLimit > maxSIPCallLimit {
		return clierr.BadInput(fmt.Sprintf("--limit must be between 1 and %d", maxSIPCallLimit))
	}
	// The API refuses an upper time bound without a lower one. Say so here
	// rather than let it come back as a 400 naming raw filter names.
	if sipCallsUntil != "" && sipCallsSince == "" {
		return clierr.BadInput("--until needs --since: the API rejects an end-time upper bound without a lower one")
	}
	since, err := normalizeSIPTime(sipCallsSince, false)
	if err != nil {
		return err
	}
	until, err := normalizeSIPTime(sipCallsUntil, true)
	if err != nil {
		return err
	}

	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(sipCallsLimit))
	q.Set("offset", strconv.Itoa(sipCallsOffset))
	for key, val := range map[string]string{
		"from_number":       trimPlus(sipCallsFrom),
		"to_number":         trimPlus(sipCallsTo),
		"call_direction":    sipCallsDirection,
		"end_time__gte":     since,
		"end_time__lte":     until,
		"hangup_source":     sipCallsSource,
		"stir_verification": sipCallsSTIR,
	} {
		if val != "" {
			q.Set(key, val)
		}
	}
	if sipCallsCauseCode != 0 {
		q.Set("hangup_cause_code", strconv.Itoa(sipCallsCauseCode))
	}

	var resp api.SIPTrunkCallList
	apiErr, err := client.Do("GET", client.AccountURL("Zentrunk", "Call"), nil, q, &resp)
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
	rows := [][]string{{"CALL_UUID", "FROM", "TO", "DIR", "DUR", "CAUSE", "HUNG_UP_BY", "END_TIME"}}
	for _, c := range resp.Objects {
		rows = append(rows, []string{
			c.CallUUID, c.FromNumber, c.ToNumber, c.CallDirection,
			strconv.Itoa(c.BillDuration), c.HangupCauseName, c.HangupSource, c.EndTime,
		})
	}
	return output.Table(os.Stdout, rows)
}

func runSIPCallsGet(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var c api.SIPTrunkCall
	apiErr, err := client.Do("GET", client.AccountURL("Zentrunk", "Call", args[0]), nil, nil, &c)
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
		return output.JSONRaw(os.Stdout, c.Raw())
	}
	if err := output.KV(os.Stdout, sipCallKV(c)); err != nil {
		return err
	}
	// Point at the hangup-code reference rather than at `voice calls diagnose`:
	// a trunk CDR is not a Voice CDR and that command cannot read it.
	if !quietFlag && c.HangupCauseName != "" {
		fmt.Fprintf(os.Stdout, "\nWhat %s means: %s\n", c.HangupCauseName, sipHangupCodesDocsURL)
	}
	return nil
}

func sipCallKV(c api.SIPTrunkCall) [][2]string {
	return [][2]string{
		{"call_uuid", c.CallUUID},
		{"call_id", c.CallID},
		{"from_number", c.FromNumber},
		{"to_number", c.ToNumber},
		{"call_direction", c.CallDirection},
		{"initiation_time", c.InitiationTime},
		{"answer_time", c.AnswerTime},
		{"end_time", c.EndTime},
		{"call_duration", strconv.Itoa(c.CallDuration)},
		{"bill_duration", strconv.Itoa(c.BillDuration)},
		{"hangup_cause_name", c.HangupCauseName},
		{"hangup_cause_code", strconv.Itoa(c.HangupCauseCode)},
		{"hangup_source", c.HangupSource},
		{"trunk_domain", c.TrunkDomain},
		{"from_country", c.FromCountry},
		{"to_country", c.ToCountry},
		{"transport_protocol", c.TransportProtocol},
		{"srtp", strconv.FormatBool(c.SRTP)},
		{"secure_trunking", strconv.FormatBool(c.SecureTrunking)},
		{"stir_verification", c.STIRVerification},
		{"attestation_indicator", c.AttestationIndicator},
		{"total_rate", c.TotalRate},
		{"total_amount", c.TotalAmount},
		{"cnam_lookup", strconv.FormatBool(c.CnamLookup)},
		{"cnam_lookup_rate", c.CnamLookupRate},
	}
}

func runSIPTrunksList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(sipTrunksLimit))
	q.Set("offset", strconv.Itoa(sipTrunksOffset))
	if sipTrunksDirection != "" {
		q.Set("trunk_direction", sipTrunksDirection)
	}
	var resp api.SIPTrunkList
	apiErr, err := client.Do("GET", client.AccountURL("Zentrunk", "Trunk"), nil, q, &resp)
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
	rows := [][]string{{"TRUNK_ID", "NAME", "TRUNK_DIRECTION", "TRUNK_STATUS", "TRUNK_DOMAIN", "PRIMARY_URI_UUID"}}
	for _, t := range resp.Objects {
		rows = append(rows, []string{
			t.TrunkID, t.Name, t.TrunkDirection, t.TrunkStatus,
			t.TrunkDomain, t.PrimaryURIUUID,
		})
	}
	return output.Table(os.Stdout, rows)
}

func runSIPTrunksGet(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var t api.SIPTrunk
	apiErr, err := client.Do("GET", client.AccountURL("Zentrunk", "Trunk", args[0]), nil, nil, &t)
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
		return output.JSONRaw(os.Stdout, t.Raw())
	}
	t = unwrapSIPTrunk(t)
	return output.KV(os.Stdout, [][2]string{
		{"trunk_id", t.TrunkID},
		{"name", t.Name},
		{"trunk_direction", t.TrunkDirection},
		{"trunk_status", t.TrunkStatus},
		{"trunk_domain", t.TrunkDomain},
		{"secure", strconv.FormatBool(t.Secure)},
		{"ipacl_uuid", t.IPACLUUID},
		{"credential_uuid", t.CredentialUUID},
		{"primary_uri_uuid", t.PrimaryURIUUID},
		{"fallback_uri_uuid", t.FallbackURIUUID},
	})
}

// unwrapSIPTrunk re-reads the body when the record arrives nested under
// `object` rather than flat. Returns the input untouched when it is flat.
func unwrapSIPTrunk(t api.SIPTrunk) api.SIPTrunk {
	if t.TrunkID != "" || len(t.Raw()) == 0 {
		return t
	}
	var wrapped struct {
		Object api.SIPTrunk `json:"object"`
	}
	if err := json.Unmarshal(t.Raw(), &wrapped); err != nil || wrapped.Object.TrunkID == "" {
		return t
	}
	return wrapped.Object
}

func runSIPACLList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(sipACLLimit))
	q.Set("offset", strconv.Itoa(sipACLOffset))
	var resp api.SIPTrunkACLList
	apiErr, err := client.Do("GET", client.AccountURL("Zentrunk", "IPAccessControlList"), nil, q, &resp)
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
	rows := [][]string{{"IPACL_UUID", "NAME", "IP_ADDRESSES"}}
	for _, a := range resp.Objects {
		rows = append(rows, []string{a.IPACLUUID, a.Name, strings.Join(a.IPAddresses, ", ")})
	}
	return output.Table(os.Stdout, rows)
}

func runSIPACLGet(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var a api.SIPTrunkACL
	apiErr, err := client.Do("GET", client.AccountURL("Zentrunk", "IPAccessControlList", args[0]), nil, nil, &a)
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
		return output.JSONRaw(os.Stdout, a.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"ipacl_uuid", a.IPACLUUID},
		{"name", a.Name},
		{"ip_addresses", strings.Join(a.IPAddresses, ", ")},
	})
}

// runSIPCallsDiagnose confirms the uuid really is a trunk call, then hands the
// turn to the assistant. A Voice uuid is refused by name rather than forwarded:
// the trunk debugger reads a different store and would answer about nothing.
func runSIPCallsDiagnose(cmd *cobra.Command, args []string) error {
	callUUID := args[0]
	if !dryRunFlag {
		client, _, err := getClient()
		if err != nil {
			return err
		}
		if !resourceExists(client, "Zentrunk", "Call", callUUID) {
			if resourceExists(client, "Call", callUUID) {
				return &clierr.Error{
					Code:       clierr.CodeBadInput,
					Message:    fmt.Sprintf("%s is a Voice call, not a SIP Trunking call", callUUID),
					Hint:       fmt.Sprintf("Run `plivo voice calls diagnose %s`.", callUUID),
					StatusCode: http.StatusNotFound,
				}
			}
			return &clierr.Error{
				Code:       clierr.CodeResourceNotFound,
				Message:    fmt.Sprintf("SIP Trunking call %s not found on this account", callUUID),
				Hint:       "Check the call id. `plivo sip calls list` lists recent ones.",
				StatusCode: http.StatusNotFound,
			}
		}
	}
	askCallUUID = callUUID
	prompt := "Help me debug this SIP Trunking call. Walk the SIP ladder and the trunk " +
		"configuration, and tell me what happened and whether anything is wrong."
	return runAsk(cmd, []string{prompt})
}
