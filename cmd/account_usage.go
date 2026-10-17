package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// maxUsageLimit is the most usage rows the Usage Summary API returns a page.
const maxUsageLimit = 1000

// Values the Usage Summary API takes; it answers any other with a 400.
var (
	usageGranularities = []string{"hour", "day", "month", "year"}
	usageProductValues = []string{"message", "voice", "zentrunk", "transcription", "other"}
)

var (
	usageSince, usageUntil, usageGranularity, usageDirection        string
	usageCountry, usageSubaccount, usageHangupCause, usageErrorCode string
	usageProducts                                                   []string
	usageLimit                                                      int
)

var accountUsageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Show usage and spend for a date window: by product, other charges, total",
	Long: `Show what the account used and spent over a date window, from Plivo's usage
summary: a row per period, product, type, country and direction, then other
charges (number rentals, lookups, fees, taxes), then the total spend. Answer
spend questions with this, never by adding up call or message records.

--since and --until are UTC dates, YYYY-MM-DD, sent as given. --until is
exclusive: the window ends as that day starts, so --until 2026-11-01 ends with
October 31. Without --since the window is the 7 days before --until; without
--until it runs through today. Plivo caps the window by --granularity (hour 7
days, day 92 days, month 24 months, year no cap) and answers a longer one with
a 400 naming the granularity to use.

--all reads every page of usage rows. The totals and other charges come with
the first page and cover the whole window. --limit 1000 needs the fewest
requests: Plivo allows 20 a minute.`,
	Example: `  plivo account usage --since 2026-10-01 --until 2026-11-01 --granularity month
  plivo account usage --since 2026-10-01 --product voice --direction outbound --all -o json`,
	Args: cobra.NoArgs,
	RunE: runAccountUsage,
}

func init() {
	f := accountUsageCmd.Flags()
	f.StringVar(&usageSince, "since", "", "first day of the window, YYYY-MM-DD (UTC)")
	f.StringVar(&usageSince, "from-date", "", "same as --since")
	f.StringVar(&usageUntil, "until", "", "day the window ends before, YYYY-MM-DD (UTC): exclusive, that day is left out")
	f.StringVar(&usageUntil, "to-date", "", "same as --until")
	f.StringVar(&usageGranularity, "granularity", "", "period of a row: "+oneOf(usageGranularities)+" (Plivo's default: day)")
	f.StringArrayVar(&usageProducts, "product", nil, "only this product: "+oneOf(usageProductValues)+" (repeatable)")
	f.StringVar(&usageDirection, "direction", "", oneOf(directionValues))
	f.StringVar(&usageCountry, "country", "", "two-letter country code, e.g. US")
	f.StringVar(&usageSubaccount, "subaccount", "", "only this subaccount (its auth ID)")
	f.StringVar(&usageHangupCause, "hangup-cause", "", "voice and SIP trunk rows with this hangup cause")
	f.StringVar(&usageErrorCode, "error-code", "", "message rows with this error code")
	f.IntVar(&usageLimit, "limit", 100, fmt.Sprintf("usage rows per page (1-%d)", maxUsageLimit))
	registerAllFlag(accountUsageCmd)
	accountCmd.AddCommand(accountUsageCmd)
}

func runAccountUsage(cmd *cobra.Command, args []string) error {
	q, err := usageQuery(cmd)
	if err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.UsageSummary
	if err := fetchUsage(client, q, &resp); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONList(os.Stdout, resp.Raw(), "usage")
	}
	if err := usageTable(&resp); err != nil {
		return err
	}
	if resp.Meta.NextPageToken != "" && !allFlag && !quietFlag {
		fmt.Fprintf(os.Stderr, "More usage rows than this page holds: add --all, or a larger --limit (up to %d). "+
			"total_spend already covers the whole window.\n", maxUsageLimit)
	}
	return nil
}

// countryCode is an ISO 3166-1 alpha-2 code, written as the API's docs do.
var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)

// usageQuery checks the flags before any request and maps each to its API
// field, sent as given. What the API checks itself, such as the window
// against --granularity, is left to its 400.
func usageQuery(cmd *cobra.Command) (url.Values, error) {
	if usageLimit < 1 || usageLimit > maxUsageLimit {
		e := clierr.BadFlag("limit", fmt.Sprintf("must be between 1 and %d, got %d", maxUsageLimit, usageLimit))
		e.Hint = fmt.Sprintf("A page holds at most %d usage rows; --all reads every page.", maxUsageLimit)
		return nil, e
	}
	if err := oneFlagPerEnd(cmd, [2][]string{{"since", "from-date"}, {"until", "to-date"}}); err != nil {
		return nil, err
	}
	if err := validateEnum("granularity", &usageGranularity, usageGranularities...); err != nil {
		return nil, err
	}
	if err := validateEnum("direction", &usageDirection, directionValues...); err != nil {
		return nil, err
	}
	q := url.Values{"page_size": {strconv.Itoa(usageLimit)}}
	for _, p := range usageProducts {
		if err := validateEnumList("product", &p, usageProductValues...); err != nil {
			return nil, err
		}
		if p != "" {
			q.Add("product", p)
		}
	}
	for field, v := range map[string]string{
		"from_date": usageSince, "to_date": usageUntil, "granularity": usageGranularity,
		"direction": usageDirection, "country": usageCountry, "subaccount_auth": usageSubaccount,
		"hangup_cause": usageHangupCause, "error_code": usageErrorCode,
	} {
		if v = strings.TrimSpace(v); v != "" {
			q.Set(field, v)
		}
	}
	for _, field := range []string{"from_date", "to_date"} {
		if d := q.Get(field); d != "" {
			if _, err := time.Parse(time.DateOnly, d); err != nil {
				return nil, clierr.BadInput(fmt.Sprintf("could not read %q as a date: use YYYY-MM-DD (UTC)", d))
			}
		}
	}
	if c := q.Get("country"); c != "" && !countryCode.MatchString(c) {
		e := clierr.BadFlag("country", fmt.Sprintf("must be two capital letters, got %q", c))
		e.Hint = "Pass an ISO 3166-1 alpha-2 country code, such as US, GB or IN."
		e.Context["value"] = c
		return nil, e
	}
	return q, nil
}

// fetchUsage reads one page of the summary into out or, with --all, every
// page. Only the first carries the totals and other_charges, so the later
// pages add just their usage rows to it. meta.next_page_token is then null,
// or where the walk stopped early, with truncated: true; a page that fails
// after the first leaves the rows read printed and its error in partialErr.
func fetchUsage(client *api.Client, q url.Values, out *api.UsageSummary) error {
	endpoint := client.AccountURL("UsageSummary")
	if !allFlag || client.DryRun {
		apiErr, err := client.Do("GET", endpoint, nil, q, out)
		if err != nil {
			return err
		}
		if apiErr != nil {
			return apiErr
		}
		if allFlag {
			fmt.Fprintln(os.Stderr, "[dry-run] --all would read the pages after this one too.")
		}
		return nil
	}
	var first map[string]json.RawMessage
	var rows []json.RawMessage
	var token string
	for page := 1; ; page++ {
		env, got, _, err := getPage(client, endpoint, q, "usage")
		if err != nil {
			if first == nil {
				return err
			}
			partialErr = err
			fmt.Fprintf(os.Stderr, "Warning: --all stopped after %d usage rows on the error below. "+
				"Those rows are printed; meta.next_page_token is where the rest start.\n", len(rows))
			break
		}
		if first == nil {
			first = env
		}
		rows = append(rows, got...)
		var meta struct {
			NextPageToken string `json:"next_page_token"`
		}
		_ = json.Unmarshal(env["meta"], &meta)
		if token = meta.NextPageToken; token == "" {
			break
		}
		if page == maxListPages {
			fmt.Fprintf(os.Stderr, "Warning: --all stopped after %d pages (%d usage rows) and there are more. "+
				"Narrow the window or the filters to read the rest.\n", maxListPages, len(rows))
			break
		}
		q.Set("page_token", token)
	}
	var next json.RawMessage
	if token != "" {
		next, _ = output.Marshal(token)
	}
	return mergePages(first, "usage", rows, "next_page_token", next, token != "", out)
}

// usageTable prints the usage rows, the other charges, then the totals. The
// API splits rows by subaccount and by error code or hangup cause, so a
// column for each appears once any row has one: without it those rows would
// read as duplicates.
func usageTable(s *api.UsageSummary) error {
	var bySub, byCause bool
	for _, u := range s.Usage {
		bySub = bySub || u.SubaccountAuth != ""
		byCause = byCause || usageCause(u) != ""
	}
	g := s.Meta.Granularity
	rows := [][]string{{"PERIOD", "PRODUCT", "TYPE", "COUNTRY", "DIRECTION", "UNITS", "DURATION", "AMOUNT"}}
	if bySub {
		rows[0] = append(rows[0], "SUBACCOUNT")
	}
	if byCause {
		rows[0] = append(rows[0], "CAUSE")
	}
	for _, u := range s.Usage {
		row := []string{usagePeriod(u.FromDate, g), u.Product, u.Type, u.Country, u.Direction,
			usageNumber(u.TotalUnits), usageNumber(u.DurationSeconds), usageNumber(u.TotalAmount)}
		if bySub {
			row = append(row, u.SubaccountAuth)
		}
		if byCause {
			row = append(row, usageCause(u))
		}
		rows = append(rows, row)
	}
	if err := output.Table(os.Stdout, rows); err != nil {
		return err
	}
	if len(s.OtherCharges) > 0 {
		rows = [][]string{{"PERIOD", "OTHER CHARGE", "UNITS", "AMOUNT"}}
		for _, c := range s.OtherCharges {
			rows = append(rows, []string{usagePeriod(c.FromDate, g), c.Description, usageNumber(c.TotalUnits), usageNumber(c.TotalAmount)})
		}
		fmt.Fprintln(os.Stdout)
		if err := output.Table(os.Stdout, rows); err != nil {
			return err
		}
	}
	var totals [][2]string
	for _, kv := range [][2]string{
		{"total_spend", usageNumber(s.Meta.TotalSpend)},
		{"total_subaccount_spend", usageNumber(s.Meta.TotalSubaccountSpend)},
		{"currency", s.Meta.Currency},
	} {
		if kv[1] != "" {
			totals = append(totals, kv)
		}
	}
	fmt.Fprintln(os.Stdout)
	return output.KV(os.Stdout, totals)
}

// usageCause is a row's error code or hangup cause, then its label.
func usageCause(u api.UsageRow) string {
	code := string(u.ErrorCode)
	if code == "" {
		code = string(u.HangupCause)
	}
	return strings.TrimSpace(code + " " + u.ErrorReason)
}

// usagePeriodLayouts write a period's start to its granularity: 2026-05 for a
// month. Any other granularity is a day.
var usagePeriodLayouts = map[string]string{"hour": "2006-01-02 15:04", "month": "2006-01", "year": "2006"}

// usagePeriod names a period by its start in UTC, or as sent if that does not
// parse.
func usagePeriod(start, granularity string) string {
	t, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return start
	}
	layout, ok := usagePeriodLayouts[granularity]
	if !ok {
		layout = time.DateOnly
	}
	return t.UTC().Format(layout)
}

// usageNumber writes a count, duration or amount for the table with at most
// six decimals, which drops the float noise the API can send
// (0.37760000000000005 for 0.3776); -o json has it as sent. Empty when the API
// left it out.
func usageNumber(v *float64) string {
	if v == nil {
		return ""
	}
	return strings.TrimSuffix(strings.TrimRight(strconv.FormatFloat(*v, 'f', 6, 64), "0"), ".")
}
