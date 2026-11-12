package cmd

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	statsDefaultDays     = 7
	statsDefaultMaxCalls = 10000
	statsUnknown         = "(unknown)"
)

var (
	statsRange                         listTimeRange
	statsBy, statsNumber               string
	statsFrom, statsTo, statsDirection string
	statsIncludeLegs                   bool
	statsMaxCalls                      int
	statsByValues                      = []string{"number", "direction", "none"}
	statsTimeLayouts                   = []string{"2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05", time.RFC3339}
)

var callStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show calls and connect rate per number over a window",
	Long: `Count the calls on each of your numbers over a window, and how many connected,
worked out here from the call list. Connected means the call was answered
(it has an answer_time), whatever its hangup cause; on an inbound call that
means Plivo answered it. A number is the one on your side: from_number on an
outbound call, to_number on an inbound one. Rows are a number and a direction;
--by direction groups by direction alone, --by none gives the totals only.

The window is the calls that ended in it, set as in ` + "`plivo voice calls list`" + `: UTC,
at most 31 days, within the last 90. With neither --since nor --until it is
the last 7 days; --until needs --since. Calls that are a child leg of another
(they have a parent_call_uuid) are left out and counted as legs_excluded;
--include-legs counts them.

--number reads one number's outbound calls and its inbound calls, two reads of
the list, and can't be combined with --from, --to or --direction.

The list is read 20 calls a request, one request at a time, and stops after
--max-calls (0: no cap), saying so and exiting 0 with truncated set. Narrow the
window or add --number to read less. Spend is not worked out here: see
` + "`plivo account usage`" + `.

-o json has window, filters, totals and groups (window.covered_since is the
earliest end_time actually read); -o jsonl and -o csv write one record per group.`,
	Example: `  plivo voice calls stats --since 2026-11-01
  plivo voice calls stats --number +14155550100 --since 2026-11-01 -o json --query data.totals
  plivo voice calls stats --by direction`,
	Args: cobra.NoArgs,
	RunE: runCallStats,
}

func init() {
	f := callStatsCmd.Flags()
	registerTimeRangeFlags(callStatsCmd, &statsRange, "calls ending", rangeLimits{retainDays: listRetainDays, spanDays: listSpanDays})
	f.StringVar(&statsBy, "by", "number", "group rows by "+oneOf(statsByValues))
	f.StringVar(&statsNumber, "number", "", "one of your numbers (E.164): its outbound and inbound calls (not with --from, --to or --direction)")
	f.StringVar(&statsFrom, "from", "", "filter by from_number")
	f.StringVar(&statsTo, "to", "", "filter by to_number")
	f.StringVar(&statsDirection, "direction", "", oneOf(directionValues))
	f.BoolVar(&statsIncludeLegs, "include-legs", false, "count child legs too (calls with a parent_call_uuid)")
	f.IntVar(&statsMaxCalls, "max-calls", statsDefaultMaxCalls, "stop after reading this many calls (0: no cap)")
	callCmd.AddCommand(callStatsCmd)
}

// statsRow is the part of a call record stats reads. answer_time stays raw so
// a null can be told from a missing key, and the numbers that some records
// send as strings are FlexStrings.
type statsRow struct {
	CallUUID   string          `json:"call_uuid"`
	Parent     *string         `json:"parent_call_uuid"`
	Direction  string          `json:"call_direction"`
	From       string          `json:"from_number"`
	To         string          `json:"to_number"`
	AnswerTime json.RawMessage `json:"answer_time"`
	EndTime    string          `json:"end_time"`
	Bill       api.FlexString  `json:"bill_duration"`
	HangupName string          `json:"hangup_cause_name"`
	HangupCode api.FlexString  `json:"hangup_cause_code"`
}

// connected: answered, whatever came after. Only a record with no answer_time
// key at all falls back to a billed duration.
func (r statsRow) connected() bool {
	if r.AnswerTime == nil {
		return r.talk() > 0
	}
	var at string
	_ = json.Unmarshal(r.AnswerTime, &at)
	return strings.TrimSpace(at) != ""
}

func (r statsRow) talk() int {
	f, _ := strconv.ParseFloat(string(r.Bill), 64)
	return int(f)
}

func (r statsRow) cause() string {
	return cmp.Or(strings.TrimSpace(r.HangupName), strings.TrimSpace(string(r.HangupCode)), "Unknown")
}

// statsNumberKey names a number: its digits with a leading +, or what it is
// when not all digits (a sip: URI, a username).
func statsNumberKey(s string) string {
	d := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(s), " ", ""), "+")
	switch {
	case d == "":
		return statsUnknown
	case strings.Trim(d, "0123456789") == "":
		return "+" + d
	}
	return s
}

func parseStatsTime(s string) (time.Time, bool) {
	for _, layout := range statsTimeLayouts {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

type statsTally struct {
	Calls        int            `json:"calls"`
	Connected    int            `json:"connected"`
	Rate         *float64       `json:"connect_rate_pct"`
	AvgTalk      *float64       `json:"avg_talk_seconds"`
	Talk         int            `json:"talk_seconds"`
	NotConnected map[string]int `json:"not_connected"`
}

func (t *statsTally) count(r statsRow) {
	t.Calls++
	if !r.connected() {
		t.NotConnected[r.cause()]++
		return
	}
	t.Connected++
	t.Talk += r.talk()
}

func (t *statsTally) finish() {
	round := func(f float64) *float64 { f = math.Round(f*10) / 10; return &f }
	if t.Calls > 0 {
		t.Rate = round(100 * float64(t.Connected) / float64(t.Calls))
	}
	if t.Connected > 0 {
		t.AvgTalk = round(float64(t.Talk) / float64(t.Connected))
	}
}

// cells are the table columns after a row's key.
func (t statsTally) cells() []string {
	rate, avg, top, n := "-", "-", "-", 0
	if t.Rate != nil {
		rate = fmt.Sprintf("%.1f%%", *t.Rate)
	}
	if t.AvgTalk != nil {
		avg = fmt.Sprintf("%.1fs", *t.AvgTalk)
	}
	for cause, c := range t.NotConnected {
		if c > n || (c == n && cause < top) {
			top, n = cause, c
		}
	}
	if n > 0 {
		top = fmt.Sprintf("%s (%d)", top, n)
	}
	return []string{strconv.Itoa(t.Calls), strconv.Itoa(t.Connected), rate, avg, fmt.Sprintf("%.1f", float64(t.Talk)/60), top}
}

type statsKey struct {
	Number    string `json:"number,omitempty"`
	Direction string `json:"direction,omitempty"`
}

type statsGroup struct {
	statsKey
	statsTally
}

type statsWindow struct {
	Since        string  `json:"since"`
	Until        string  `json:"until"`
	CoveredSince *string `json:"covered_since"`
	Timezone     string  `json:"timezone"`
}

type statsFilters struct {
	Number      *string `json:"number"`
	From        *string `json:"from"`
	To          *string `json:"to"`
	Direction   *string `json:"direction"`
	IncludeLegs bool    `json:"include_legs"`
}

// statsResult is what -o json puts under data.
type statsResult struct {
	Window            statsWindow  `json:"window"`
	Filters           statsFilters `json:"filters"`
	GroupBy           string       `json:"group_by"`
	CallsRead         int          `json:"calls_read"`
	CallsCounted      int          `json:"calls_counted"`
	LegsExcluded      int          `json:"legs_excluded"`
	DuplicatesDropped int          `json:"duplicates_dropped"`
	Truncated         bool         `json:"truncated"`
	Totals            statsTally   `json:"totals"`
	Groups            []statsGroup `json:"groups"`
}

// statsRun is one walk of the list, tallied as it is read.
type statsRun struct {
	seen      map[string]bool
	groups    map[statsKey]*statsGroup
	res       statsResult
	pages     int
	earliest  time.Time
	truncated bool
}

func newTally() statsTally { return statsTally{NotConnected: map[string]int{}} }

func (s *statsRun) add(raw json.RawMessage) {
	s.res.CallsRead++
	var r statsRow
	if json.Unmarshal(raw, &r) != nil {
		return
	}
	if end, ok := parseStatsTime(r.EndTime); ok && (s.earliest.IsZero() || end.Before(s.earliest)) {
		s.earliest = end
	}
	if r.CallUUID != "" {
		if s.seen[r.CallUUID] {
			s.res.DuplicatesDropped++
			return
		}
		s.seen[r.CallUUID] = true
	}
	if r.Parent != nil && *r.Parent != "" && !statsIncludeLegs {
		s.res.LegsExcluded++
		return
	}
	dir, num := cmp.Or(r.Direction, statsUnknown), r.From
	if r.Direction == dirInbound {
		num = r.To
	}
	key := statsKey{Direction: dir}
	if statsBy == "number" {
		key.Number = statsNumberKey(num)
	}
	if statsBy != "none" {
		g := s.groups[key]
		if g == nil {
			g = &statsGroup{key, newTally()}
			s.groups[key] = g
		}
		g.count(r)
	}
	s.res.Totals.count(r)
}

// walk reads one filtered list into s, up to the calls --max-calls leaves.
// Rows left unread (more in the page, or a next page) mark the run truncated.
func (s *statsRun) walk(client *api.Client, q url.Values) error {
	if statsMaxCalls > 0 && s.res.CallsRead >= statsMaxCalls {
		s.truncated = true
		return nil
	}
	capped := false
	_, next, _, err := walkPages(client, client.AccountURL("Call"), q, "objects", 0, func(page []json.RawMessage) bool {
		if s.pages++; !quietFlag && s.pages%25 == 0 {
			fmt.Fprintf(os.Stderr, "Read %d calls (%d pages)...\n", s.res.CallsRead, s.pages)
		}
		if left := statsMaxCalls - s.res.CallsRead; statsMaxCalls > 0 && len(page) > left {
			page, s.truncated = page[:left], true
		}
		for _, row := range page {
			s.add(row)
		}
		capped = statsMaxCalls > 0 && s.res.CallsRead >= statsMaxCalls
		return !capped
	})
	if n := strings.TrimSpace(string(next)); capped && n != "" && n != "null" {
		s.truncated = true
	}
	return err
}

// statsWalks are the filtered reads a run makes. --number is its outbound
// calls from it and its inbound calls to it, with the direction sent.
func statsWalks() []url.Values {
	walks := []url.Values{{}}
	if statsNumber != "" {
		n := "+" + trimPlus(statsNumber)
		walks = []url.Values{{"call_direction": {dirOutbound}, "from_number": {n}}, {"call_direction": {dirInbound}, "to_number": {n}}}
	}
	for _, kv := range [][2]string{{"from_number", statsFrom}, {"to_number", statsTo}, {"call_direction", statsDirection}} {
		if kv[1] != "" {
			walks[0].Set(kv[0], kv[1])
		}
	}
	return walks
}

func runCallStats(cmd *cobra.Command, _ []string) error {
	if err := validateEnum("by", &statsBy, statsByValues...); err != nil {
		return err
	}
	if err := validateEnum("direction", &statsDirection, directionValues...); err != nil {
		return err
	}
	switch {
	case statsMaxCalls < 0:
		return clierr.BadFlag("max-calls", fmt.Sprintf("must be 0 or more, got %d", statsMaxCalls))
	case statsNumber != "" && !isE164Digits(trimPlus(statsNumber)):
		return clierr.BadFlag("number", fmt.Sprintf("%q is not an E.164 number such as +14155551234", statsNumber))
	case statsNumber != "" && (statsFrom != "" || statsTo != "" || statsDirection != ""):
		return clierr.BadFlag("number", "can't be combined with --from, --to or --direction")
	}
	since, until, err := statsRange.bounds(cmd)
	if err != nil {
		return err
	}
	runStart := listTimeNow().UTC().Truncate(time.Second)
	if since.IsZero() {
		since = runStart.AddDate(0, 0, -statsDefaultDays)
	}
	if until.IsZero() || until.After(runStart) {
		until = runStart
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	s := &statsRun{seen: map[string]bool{}, groups: map[statsKey]*statsGroup{}}
	s.res.Totals = newTally()
	var werr error
	for _, q := range statsWalks() {
		setTimeRange(q, "end_time", since, until)
		q.Set("limit", strconv.Itoa(maxListLimit))
		q.Set("offset", "0")
		if client.DryRun {
			if _, err := client.Do("GET", client.AccountURL("Call"), nil, q, nil); err != nil {
				return err
			}
			continue
		}
		if werr = s.walk(client, q); werr != nil {
			break
		}
	}
	if client.DryRun {
		fmt.Fprintln(os.Stderr, "[dry-run] stats would read the pages after this one too.")
		return nil
	}
	if werr != nil {
		if s.pages == 0 {
			return werr
		}
		partialErr, s.truncated = werr, true
		fmt.Fprintf(os.Stderr, "Warning: stopped after %d calls on the error below. The stats cover only those.\n", s.res.CallsRead)
	} else if s.truncated {
		fmt.Fprintf(os.Stderr, "Warning: stopped at --max-calls (%d calls read) and the window has more. "+
			"The stats cover only those; narrow the window or add --number.\n", s.res.CallsRead)
	}
	if !quietFlag {
		fmt.Fprintf(os.Stderr, "Window %s to %s UTC, %d calls read, %d legs excluded\n",
			since.Format(time.DateTime), until.Format(time.DateTime), s.res.CallsRead, s.res.LegsExcluded)
		if s.res.Totals.Calls == 0 {
			fmt.Fprintln(os.Stderr, "No calls counted in this window.")
		}
	}
	return renderStats(s.finish(since, until))
}

// finish puts the tallies into the result, groups busiest first.
func (s *statsRun) finish(since, until time.Time) statsResult {
	r := s.res
	nz := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	r.Window = statsWindow{Since: since.Format(time.RFC3339), Until: until.Format(time.RFC3339), Timezone: "UTC"}
	if !s.earliest.IsZero() {
		r.Window.CoveredSince = nz(s.earliest.Format(time.RFC3339))
	}
	r.Filters = statsFilters{nz(statsNumber), nz(statsFrom), nz(statsTo), nz(statsDirection), statsIncludeLegs}
	r.GroupBy, r.Truncated, r.CallsCounted = statsBy, s.truncated, r.Totals.Calls
	r.Totals.finish()
	r.Groups = []statsGroup{}
	for _, g := range s.groups {
		g.finish()
		r.Groups = append(r.Groups, *g)
	}
	slices.SortFunc(r.Groups, func(a, b statsGroup) int {
		return cmp.Or(cmp.Compare(b.Calls, a.Calls), cmp.Compare(a.Number, b.Number), cmp.Compare(a.Direction, b.Direction))
	})
	return r
}

func renderStats(r statsResult) error {
	if effectiveFormat() == output.FormatJSON {
		raw, err := output.Marshal(r)
		if err != nil {
			return err
		}
		return output.JSONList(os.Stdout, raw, "groups")
	}
	rows := [][]string{{"NUMBER", "DIR", "CALLS", "CONNECTED", "RATE", "AVG_TALK", "TALK_MIN", "TOP_FAILURE"}}
	for _, g := range r.Groups {
		rows = append(rows, append([]string{cmp.Or(g.Number, "-"), g.Direction}, g.cells()...))
	}
	return output.Table(os.Stdout, append(rows, append([]string{"TOTAL", "-"}, r.Totals.cells()...)))
}
