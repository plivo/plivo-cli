package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

// maxListLimit is the most rows a list endpoint returns in one page.
const maxListLimit = 20

// maxListPages stops --all after 100 pages: 2,000 rows at --limit 20.
const maxListPages = 100

// A page that answers 429 is retried up to pageRetries times, waiting what its
// Retry-After asks, never longer than maxRetryWait or --timeout. Without one the
// waits double from 1s: 61s in all, past the 60s window the Message API counts
// its 20 requests in (it sends no Retry-After).
const (
	pageRetries  = 6
	maxRetryWait = 30 * time.Second
)

// waitBeforeRetry is time.Sleep; tests swap it to keep the clock out of them.
var waitBeforeRetry = time.Sleep

// partialErr is the error that stopped an --all walk after it had read rows.
// fetchList keeps those rows for the command to print rather than returning
// it, and execute returns it once the command is done, so the exit code still
// says the list is incomplete.
var partialErr error

// registerListFlags adds --limit, --offset and --all to a list backed by a
// paged API, and checks them before the command runs. Past 20 rows a page the
// endpoints disagree (some clamp silently, some answer 400, a few return
// more), so the CLI holds every one of them to 1-20.
func registerListFlags(cmd *cobra.Command, limit, offset *int) {
	registerPageFlags(cmd, limit, offset)
	registerAllFlag(cmd)
}

// registerPageFlags is registerListFlags without --all, for a paged search
// where reading every page is not a sensible request.
func registerPageFlags(cmd *cobra.Command, limit, offset *int) {
	cmd.Flags().IntVar(limit, "limit", maxListLimit, fmt.Sprintf("results per page (1-%d)", maxListLimit))
	cmd.Flags().IntVar(offset, "offset", 0, "pagination offset")
	// Ahead of any hook the command already has, never instead of it. cobra
	// skips PreRun once PreRunE is set, so a PreRun is chained too.
	prevE, prev := cmd.PreRunE, cmd.PreRun
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		if err := validatePage(*limit, *offset, c.Flags().Lookup("all") != nil); err != nil {
			return err
		}
		if allFlag && c.Flags().Changed("offset") {
			return clierr.BadFlag("all", "cannot be combined with --offset: --all reads every page from the first")
		}
		if prevE != nil {
			return prevE(c, args)
		}
		if prev != nil {
			prev(c, args)
		}
		return nil
	}
}

func validatePage(limit, offset int, hasAll bool) error {
	if limit < 1 || limit > maxListLimit {
		e := clierr.BadFlag("limit", fmt.Sprintf("must be between 1 and %d, got %d", maxListLimit, limit))
		e.Hint = fmt.Sprintf("A page holds at most %d rows; page through with --offset.", maxListLimit)
		if hasAll {
			e.Hint = fmt.Sprintf("A page holds at most %d rows; --all reads every page.", maxListLimit)
		}
		return e
	}
	if offset < 0 {
		return clierr.BadFlag("offset", fmt.Sprintf("must be 0 or more, got %d", offset))
	}
	return nil
}

// listTimeRange is a list's --since/--until. --from-date and --to-date set the
// same two fields: they are the names people also reach for.
type listTimeRange struct {
	since, until string
	names        [2][]string // every flag for each end, for the both-given check
	rangeLimits
}

// rangeLimits are the limits a list's API puts on a range, checked before any
// request; 0 leaves that one to the API.
type rangeLimits struct {
	retainDays int // how far back the API reads
	spanDays   int // the most it searches per request; a range with no end runs to now
}

// listRetainDays is how far back the Call and Message APIs read, and
// listSpanDays the most the Call and Recording APIs search per request.
const (
	listRetainDays = 90
	listSpanDays   = 31
)

// listTimeNow is the clock the range checks read, so tests can move it.
var listTimeNow = time.Now

// registerTimeRangeFlags adds --since/--until and --from-date/--to-date to a
// list. what names the rows and the time they are filtered on; limits are the
// list's API's, named in the --since help.
func registerTimeRangeFlags(cmd *cobra.Command, r *listTimeRange, what string, limits rangeLimits) {
	r.rangeLimits = limits
	r.names = [2][]string{{"since", "from-date"}, {"until", "to-date"}}
	sinceUsage := what + " at or after this UTC time (YYYY-MM-DD[ HH:MM[:SS]])"
	if limits.retainDays > 0 {
		sinceUsage += fmt.Sprintf(", within the last %d days", limits.retainDays)
	}
	if limits.spanDays > 0 {
		sinceUsage += fmt.Sprintf(", at most %d days per request", limits.spanDays)
	}
	f := cmd.Flags()
	f.StringVar(&r.since, "since", "", sinceUsage)
	f.StringVar(&r.until, "until", "", what+" at or before this UTC time (YYYY-MM-DD[ HH:MM[:SS]])")
	f.StringVar(&r.since, "from-date", "", "same as --since")
	f.StringVar(&r.until, "to-date", "", "same as --until")
}

// deprecatedNames adds older names for the two ends, kept so scripts that
// pass them still work. cobra hides them from help and says what to use.
func (r *listTimeRange) deprecatedNames(cmd *cobra.Command, since, until string) {
	f := cmd.Flags()
	f.StringVar(&r.since, since, "", "same as --since")
	f.StringVar(&r.until, until, "", "same as --until")
	_ = f.MarkDeprecated(since, "use --since")
	_ = f.MarkDeprecated(until, "use --until")
	r.names[0] = append(r.names[0], since)
	r.names[1] = append(r.names[1], until)
}

// bounds returns the range in UTC, a zero time for an end that is not set.
// Refused before any request: two names for one end (only the last would
// count); an end without a start (the call and recording APIs answer it with
// a 400, and the Message API searches only recent days, so an earlier end
// matches nothing); and a range past the list's API limits.
func (r *listTimeRange) bounds(cmd *cobra.Command) (since, until time.Time, err error) {
	for _, names := range r.names {
		var given []string
		for _, n := range names {
			if cmd.Flags().Changed(n) {
				given = append(given, "--"+n)
			}
		}
		if len(given) > 1 {
			return time.Time{}, time.Time{}, clierr.BadInput(strings.Join(given, " and ") + " are the same filter; pass one of them")
		}
	}
	if since, err = parseListTime(r.since, false); err != nil {
		return time.Time{}, time.Time{}, err
	}
	if until, err = parseListTime(r.until, true); err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !until.IsZero() && since.IsZero() {
		e := clierr.BadInput("--until needs --since (--to-date needs --from-date)")
		e.Hint = "Add --since (or --from-date) for the start of the range."
		return time.Time{}, time.Time{}, e
	}
	now := listTimeNow()
	if r.retainDays > 0 {
		if err = checkRetention(since, now, r.retainDays); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if r.spanDays > 0 {
		if err = checkSpan(since, until, now, r.spanDays); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	return since, until, nil
}

// checkRetention refuses a start more than days back.
func checkRetention(since, now time.Time, days int) error {
	earliest := now.UTC().AddDate(0, 0, -days)
	if since.IsZero() || !since.Before(earliest) {
		return nil
	}
	day := earliest.Truncate(24 * time.Hour)
	if day.Before(earliest) {
		day = day.AddDate(0, 0, 1)
	}
	e := clierr.BadInput(fmt.Sprintf("the range starts %s, more than %d days ago: Plivo searches only the last %d days",
		since.Format(time.DateOnly), days, days))
	e.Hint = fmt.Sprintf("Start it on or after %s.", day.Format(time.DateOnly))
	return e
}

// checkSpan refuses a range longer than days, the most the list's API
// searches per request, with the ranges to run instead.
func checkSpan(since, until, now time.Time, days int) error {
	if since.IsZero() {
		return nil
	}
	now = now.UTC()
	to, why := until, "this range is longer"
	if to.IsZero() {
		to, why = now, "a range with no end runs to now"
	}
	if to.Sub(since) <= time.Duration(days)*24*time.Hour {
		return nil
	}
	e := clierr.BadInput(fmt.Sprintf("Plivo searches at most %d days per request, and %s", days, why))
	// Nothing has happened after today, so the ranges stop there: an --until
	// far ahead would otherwise list one for every month up to it.
	if endOfToday := now.Truncate(24 * time.Hour).Add(24*time.Hour - time.Second); until.IsZero() || to.After(endOfToday) {
		to = endOfToday
	}
	var ranges []string
	for _, r := range spanRanges(since, to, days) {
		ranges = append(ranges, fmt.Sprintf("--since %s --until %s", rangeFlag(r[0], false), rangeFlag(r[1], true)))
	}
	if len(ranges) > 0 {
		e.Hint = "Run one range at a time: " + strings.Join(ranges, "; ") + "."
		e.Context = map[string]any{"ranges": ranges}
	}
	return e
}

// spanRanges splits from..to into ranges of at most days calendar days: the
// first starts at from and the last ends at to.
func spanRanges(from, to time.Time, days int) [][2]time.Time {
	var out [][2]time.Time
	for start := from; !start.After(to); {
		end := start.Truncate(24*time.Hour).AddDate(0, 0, days).Add(-time.Second)
		if end.After(to) {
			end = to
		}
		out = append(out, [2]time.Time{start, end})
		start = end.Add(time.Second)
	}
	return out
}

// rangeFlag writes one end of a hinted range: a bare date when it falls on the
// day's edge (the start of the day, or with end its last second), otherwise
// the full UTC time, quoted for the shell.
func rangeFlag(t time.Time, end bool) string {
	edge := t.Truncate(24 * time.Hour)
	if end {
		edge = edge.Add(24*time.Hour - time.Second)
	}
	if t.Equal(edge) {
		return t.Format(time.DateOnly)
	}
	return strconv.Quote(t.Format(time.DateTime))
}

// setTimeRange puts a range on a list query as field__gte and field__lte,
// leaving out an end that is not set.
func setTimeRange(q url.Values, field string, since, until time.Time) {
	if !since.IsZero() {
		q.Set(field+"__gte", since.Format(time.DateTime))
	}
	if !until.IsZero() {
		q.Set(field+"__lte", until.Format(time.DateTime))
	}
}

// listTimeLayouts are what --since/--until accept besides a bare date. The
// APIs take only "YYYY-MM-DD HH:MM[:SS]", so a bare date is widened here
// rather than sent through to fail upstream.
var listTimeLayouts = []string{time.DateTime, "2006-01-02 15:04", time.RFC3339}

// parseListTime reads a --since/--until value as UTC, a zero time when it is
// empty. A bare date is the start of that day or, with endOfDay, its last
// second, so --until includes the day the user named.
func parseListTime(v string, endOfDay bool) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.DateOnly, v); err == nil {
		if endOfDay {
			t = t.Add(24*time.Hour - time.Second)
		}
		return t, nil
	}
	for _, layout := range listTimeLayouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, clierr.BadInput(fmt.Sprintf(
		"could not read %q as a time — use YYYY-MM-DD, YYYY-MM-DD HH:MM, or YYYY-MM-DD HH:MM:SS (UTC)", v))
}

// listJSON writes a list response for -o json with its rows array never null.
// A few endpoints send null, or nothing, for an empty list where the rest send
// [], and a script reading the rows should not need a case for that. key names
// the array: "objects" on most lists.
func listJSON(w io.Writer, raw json.RawMessage, key string) error {
	var env map[string]json.RawMessage
	if json.Unmarshal(raw, &env) == nil && env != nil {
		if rows, ok := env[key]; !ok || bytes.Equal(bytes.TrimSpace(rows), []byte("null")) {
			env[key] = json.RawMessage("[]")
			if b, err := output.Marshal(env); err == nil {
				raw = b
			}
		}
	}
	return output.JSONRaw(w, raw)
}

// fetchList reads one page of a list into out or, with --all, every page: their
// rows merged under key into the first page's envelope, which keeps the
// server's meta.total_count, with meta.next null or, when the walk stopped
// early, where the rows it left unread start. out then renders exactly as one
// page would. A walk that fails after its first page still renders the rows it
// read, and leaves the error in partialErr.
func fetchList(client *api.Client, endpoint string, q url.Values, key string, out api.RawCapturer) error {
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
	var rows []json.RawMessage
	env, next, capped, werr := walkPages(client, endpoint, q, key, maxListPages, func(page []json.RawMessage) bool {
		rows = append(rows, page...)
		return true
	})
	if env == nil {
		return werr // the first page failed: nothing was read
	}
	if rows == nil {
		rows = []json.RawMessage{}
	}
	merged, err := output.Marshal(rows)
	if err != nil {
		return err
	}
	env[key] = merged
	env["meta"] = walkMeta(env["meta"], next, capped || werr != nil)
	switch {
	case werr != nil:
		partialErr = werr
		fmt.Fprintf(os.Stderr, "Warning: --all stopped after %d rows on the error below. "+
			"Those rows are printed; meta.next is where the rest start.\n", len(rows))
	case capped:
		fmt.Fprintf(os.Stderr, "Warning: --all stopped after %d pages (%d rows) and the list has more. "+
			"Narrow the filters to read the rest.\n", maxListPages, len(rows))
	}
	body, err := output.Marshal(env)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return clierr.Upstream("the merged pages did not decode: " + err.Error())
	}
	out.SetRaw(body)
	return nil
}

// walkPages reads a list page by page from the first row, handing each page's
// rows to visit. When the server sends meta.total_count, reaching it ends the
// walk; only when it does not is a short page taken for the last one. An empty
// page always ends it, as does visit returning false or maxPages pages (0: no
// limit); capped reports that last case, when rows were left unread. Returns
// the first page's envelope, and next: the last page's meta.next, where the
// rows left unread start, or nil when the walk reached the end.
func walkPages(client *api.Client, endpoint string, q url.Values, key string, maxPages int,
	visit func(rows []json.RawMessage) bool,
) (first map[string]json.RawMessage, next json.RawMessage, capped bool, err error) {
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > maxListLimit {
		limit = maxListLimit
	}
	offset := 0
	for page := 0; ; page++ {
		if maxPages > 0 && page == maxPages {
			return first, next, true, nil
		}
		pq := url.Values{}
		for k, v := range q {
			pq[k] = v
		}
		pq.Set("limit", strconv.Itoa(limit))
		pq.Set("offset", strconv.Itoa(offset))
		env, rows, meta, perr := getPage(client, endpoint, pq, key)
		if perr != nil {
			return first, next, false, perr
		}
		if first == nil {
			first = env
		}
		offset += len(rows)
		// The total, when sent, says where the list ends; otherwise a short page
		// does. An empty page ends it either way.
		last := len(rows) < limit
		if meta.TotalCount != nil {
			last = len(rows) == 0 || offset >= *meta.TotalCount
		}
		if next = meta.Next; last {
			next = nil
		}
		if !visit(rows) || last {
			return first, next, false, nil
		}
	}
}

// pageMeta is what a walk reads from a page's meta. TotalCount is nil when the
// page did not send one.
type pageMeta struct {
	TotalCount *int            `json:"total_count"`
	Next       json.RawMessage `json:"next"`
}

// getPage reads one page of a walk, retrying a 429 up to pageRetries times.
func getPage(client *api.Client, endpoint string, q url.Values, key string,
) (env map[string]json.RawMessage, rows []json.RawMessage, meta pageMeta, err error) {
	var timeout time.Duration
	if client.HTTP != nil {
		timeout = client.HTTP.Timeout
	}
	var raw json.RawMessage
	for attempt := 0; ; attempt++ {
		apiErr, derr := client.Do("GET", endpoint, nil, q, &raw)
		if derr != nil {
			return nil, nil, pageMeta{}, derr
		}
		if apiErr == nil {
			break
		}
		if apiErr.Code != clierr.CodeRateLimited || attempt == pageRetries {
			return nil, nil, pageMeta{}, apiErr
		}
		wait := retryWait(attempt, apiErr.RetryAfter, timeout)
		if !quietFlag {
			fmt.Fprintf(os.Stderr, "Rate limited; retrying in %s (%d of %d)\n", wait, attempt+1, pageRetries)
		}
		waitBeforeRetry(wait)
	}
	if json.Unmarshal(raw, &env) != nil || env == nil {
		return nil, nil, pageMeta{}, clierr.Upstream("a page of the list was not a JSON object")
	}
	if v := env[key]; len(v) > 0 {
		if json.Unmarshal(v, &rows) != nil {
			return nil, nil, pageMeta{}, clierr.Upstream(fmt.Sprintf("a page of the list had no %q array", key))
		}
	}
	_ = json.Unmarshal(env["meta"], &meta) // absent or odd: the short page ends the walk
	return env, rows, meta, nil
}

// retryWait is the wait before retry attempt+1 of a page: the server's
// Retry-After when it sent one, else 1s doubling each time; capped at
// maxRetryWait and at the request timeout.
func retryWait(attempt int, retryAfter, timeout time.Duration) time.Duration {
	wait := retryAfter
	if wait <= 0 {
		wait = time.Second << attempt
	}
	wait = min(wait, maxRetryWait)
	if timeout > 0 {
		wait = min(wait, timeout)
	}
	return wait
}

// walkMeta is a merged list's meta: the first page's, with next where a script
// reads on from (null once every row is read) and, when the walk stopped
// early, truncated: true so a script can tell.
func walkMeta(meta, next json.RawMessage, truncated bool) json.RawMessage {
	m := map[string]json.RawMessage{}
	if json.Unmarshal(meta, &m) != nil || m == nil {
		m = map[string]json.RawMessage{}
	}
	m["next"] = next // a nil next encodes as null
	if truncated {
		m["truncated"] = json.RawMessage("true")
	}
	b, err := output.Marshal(m)
	if err != nil {
		return meta
	}
	return b
}

// Values the list filters accept, as the API reference documents them (the
// agents states come from the agents skill). Matching is case-sensitive: the
// docs give each value in one spelling only, and the SIP Trunking call list
// answers a wrong one with "(case sensitive)".
var (
	directionValues     = []string{dirInbound, dirOutbound}
	sipHangupSources    = []string{"customer", "carrier", "zentrunk"}
	stirValues          = []string{"Verified", "Not Verified", "Not Applicable"}
	messageStateValues  = []string{"queued", "sent", "delivered", "undelivered", "failed", "received"}
	numberTypeValues    = []string{"local", "mobile", "fixed", "national", "tollfree"}
	numberServices      = []string{"voice", "sms", "mms"}
	mpcStatusValues     = []string{"active", "initialized", "ended"}
	tollfreeStatuses    = []string{"SUBMITTED", "PROCESSING", "APPROVED", "REJECTED", "UPDATE_REQUIRED"}
	verifyStatusValues  = []string{"in-progress", "verified", "expired"}
	agentStateValues    = []string{"DRAFT", "ACTIVE", "PAUSED"}
	complianceStatuses  = []string{"draft", "submitted", "accepted", "rejected", "suspended", "expired"}
	complianceNumTypes  = []string{"local", "mobile", "tollfree"}
	complianceUserTypes = []string{"individual", "business"}
)

// oneOf renders an allowed set for a flag's usage line, so --help lists exactly
// what validateEnum accepts. When any value has a space, every value is quoted.
func oneOf(allowed []string) string {
	if !strings.Contains(strings.Join(allowed, ""), " ") {
		return strings.Join(allowed, "|")
	}
	out := make([]string, len(allowed))
	for i, v := range allowed {
		out[i] = strconv.Quote(v)
	}
	return strings.Join(out, "|")
}

// validateEnum refuses a filter value the API does not document, before any
// request. Sent as-is, an unknown value mostly comes back as an empty list with
// exit 0, which reads as "nothing matched" when the filter itself was wrong.
//
// Surrounding whitespace is trimmed in place; an empty value means the filter
// is unset and passes.
func validateEnum(flag string, value *string, allowed ...string) error {
	*value = strings.TrimSpace(*value)
	if *value == "" {
		return nil
	}
	for _, a := range allowed {
		if *value == a {
			return nil
		}
	}
	return badEnumValue(flag, *value, fmt.Sprintf("unknown value %q", *value), allowed)
}

// validateEnumList is validateEnum for a comma-separated filter such as
// --services voice,sms. Each entry must be allowed; the value is rewritten
// without the spaces around entries.
func validateEnumList(flag string, value *string, allowed ...string) error {
	if strings.TrimSpace(*value) == "" {
		*value = ""
		return nil
	}
	entries := strings.Split(*value, ",")
	for i := range entries {
		entries[i] = strings.TrimSpace(entries[i])
		if entries[i] == "" {
			return badEnumValue(flag, *value, fmt.Sprintf("empty entry in %q", *value), allowed)
		}
		if err := validateEnum(flag, &entries[i], allowed...); err != nil {
			return err
		}
	}
	*value = strings.Join(entries, ",")
	return nil
}

// badEnumValue builds the BAD_FLAG for a value outside the allowed set. A value
// that differs only in case names the spelling to use.
func badEnumValue(flag, value, reason string, allowed []string) error {
	quoted := make([]string, len(allowed))
	for i, a := range allowed {
		quoted[i] = strconv.Quote(a)
	}
	e := clierr.BadFlag(flag, reason)
	e.Hint = "Allowed values: " + strings.Join(quoted, ", ") + "."
	for _, a := range allowed {
		if strings.EqualFold(value, a) {
			e.Hint = fmt.Sprintf("Values are case-sensitive: use %q. %s", a, e.Hint)
			break
		}
	}
	e.Context["value"] = value
	e.Context["allowed"] = allowed
	return e
}
