package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
)

// statsServer serves the call list from rows, keyed by the call_direction
// filter ("" for none), 20 a page by offset, as the Call API does: no
// total_count, and meta.next null on the last page. It returns the queries it
// was sent; status may answer request n instead.
func statsServer(t *testing.T, rows map[string][]string, status func(n int) int) func() []url.Values {
	t.Helper()
	var mu sync.Mutex
	var seen []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mu.Lock()
		seen = append(seen, q)
		n := len(seen)
		mu.Unlock()
		if status != nil && status(n) != 0 {
			w.WriteHeader(status(n))
			return
		}
		all := rows[q.Get("call_direction")]
		offset, _ := strconv.Atoi(q.Get("offset"))
		page, next := []string{}, "null"
		if offset < len(all) {
			page = all[offset:min(offset+20, len(all))]
		}
		if offset+20 < len(all) {
			next = `"/next"`
		}
		_, _ = fmt.Fprintf(w, `{"meta":{"next":%s},"objects":[%s]}`, next, strings.Join(page, ","))
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })
	return func() []url.Values {
		mu.Lock()
		defer mu.Unlock()
		return append([]url.Values(nil), seen...)
	}
}

func statsCall(uuid, dir, from, to, extra string) string {
	return fmt.Sprintf(`{"call_uuid":%q,"call_direction":%q,"from_number":%q,"to_number":%q%s}`, uuid, dir, from, to, extra)
}

// statsManyCalls is n distinct outbound calls from one number.
func statsManyCalls(n int) []string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = statsCall(fmt.Sprintf("u%d", i), "outbound", "+14155550100", "14155550123", `,"answer_time":null`)
	}
	return rows
}

func statsAt(t *testing.T) {
	t.Helper()
	listTimeNow = func() time.Time { return time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { listTimeNow = time.Now })
}

func statsOut(t *testing.T, stdout string) statsResult {
	t.Helper()
	var out struct{ Data statsResult }
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	return out.Data
}

// One outbound number, in both of its spellings, with an unanswered call, one
// answered with an error cause, and records with the answer_time key missing,
// plus an inbound call, a B-leg and a repeated call_uuid.
var statsFixture = map[string][]string{"": {
	statsCall("c1", "outbound", "+14155550100", "14155550123", `,"answer_time":"2026-11-10 10:00:05+00:00","bill_duration":30,"end_time":"2026-11-10 03:00:00+05:30"`),
	statsCall("c2", "outbound", "14155550100", "14155550123", `,"answer_time":null,"bill_duration":0,"hangup_cause_name":"No Answer","end_time":"2026-11-09 23:59:59"`),
	statsCall("c3", "outbound", "+14155550100", "14155550123", `,"answer_time":"2026-11-10 10:00:05","bill_duration":0,"hangup_cause_code":8011,"end_time":"2026-11-10 15:30:00.250+05:30"`),
	statsCall("c4", "outbound", "+14155550100", "14155550123", `,"bill_duration":12`),
	statsCall("c5", "outbound", "+14155550100", "14155550123", `,"hangup_cause_code":4000`),
	statsCall("c6", "inbound", "+14155550123", "14155550111", `,"answer_time":"2026-11-10 10:00:05","bill_duration":10`),
	statsCall("c7", "outbound", "+14155550100", "14155550123", `,"parent_call_uuid":"c1","answer_time":"2026-11-10 10:00:05","bill_duration":9`),
	statsCall("c1", "outbound", "+14155550100", "14155550123", `,"answer_time":null`),
}}

func TestCallStats_countsWhatConnected(t *testing.T) {
	setFakeCreds(t)
	statsServer(t, statsFixture, nil)

	err, stdout, _ := execCmd(t, "voice", "calls", "stats", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	d := statsOut(t, stdout)
	if d.CallsRead != 8 || d.CallsCounted != 6 || d.LegsExcluded != 1 || d.DuplicatesDropped != 1 || len(d.Groups) != 2 {
		t.Fatalf("read %d counted %d legs %d dups %d, %d groups; want 8 6 1 1, 2", d.CallsRead, d.CallsCounted, d.LegsExcluded, d.DuplicatesDropped, len(d.Groups))
	}
	// Answered counts whatever followed (c3), a missing key falls back to the
	// billed time (c4, c5), and both spellings of the number are one row.
	g, in := d.Groups[0], d.Groups[1]
	if g.Number != "+14155550100" || g.Direction != "outbound" || g.Calls != 5 || g.Connected != 3 || *g.Rate != 60 || *g.AvgTalk != 14 ||
		!reflect.DeepEqual(g.NotConnected, map[string]int{"4000": 1, "No Answer": 1}) {
		t.Errorf("outbound row = %+v", g)
	}
	if in.Number != "+14155550111" || in.Direction != "inbound" || in.Calls != 1 || in.Connected != 1 || d.Totals.Talk != 52 || *d.Totals.Rate != 66.7 {
		t.Errorf("inbound row = %+v, totals %+v", in, d.Totals)
	}
	// c1's end_time is 21:30 UTC the day before, the earliest once offsets apply.
	if got := d.Window.CoveredSince; got == nil || *got != "2026-11-09T21:30:00Z" {
		t.Errorf("covered_since = %v", got)
	}
	if _, stdout, _ := execCmd(t, "voice", "calls", "stats", "--include-legs", "-o", "json"); statsOut(t, stdout).CallsCounted != 7 {
		t.Errorf("--include-legs did not count the B-leg:\n%s", stdout)
	}
	if _, stdout, _ := execCmd(t, "voice", "calls", "stats", "--query", "data.totals.calls"); strings.TrimSpace(stdout) != "6" {
		t.Errorf("--query data.totals.calls = %q", stdout)
	}
}

// jsonl and csv write a row per group, from data.groups; an empty window is a
// result with no groups, not an error.
func TestCallStats_rowFormatsAndEmptyWindow(t *testing.T) {
	setFakeCreds(t)
	statsServer(t, statsFixture, nil)

	_, jsonl, _ := execCmd(t, "voice", "calls", "stats", "-o", "jsonl")
	_, csv, _ := execCmd(t, "voice", "calls", "stats", "-o", "csv")
	if lines := strings.Split(strings.TrimSpace(jsonl), "\n"); len(lines) != 2 || !strings.HasPrefix(lines[0], `{"number":"+14155550100"`) {
		t.Errorf("jsonl is not a line per group:\n%s", jsonl)
	}
	if lines := strings.Split(strings.TrimSpace(csv), "\n"); len(lines) != 3 || !strings.HasPrefix(lines[0], "number,direction,calls,connected,connect_rate_pct") {
		t.Errorf("csv is not a header and a row per group:\n%s", csv)
	}

	statsServer(t, nil, nil)
	err, stdout, stderr := execCmd(t, "voice", "calls", "stats", "-o", "json")
	d := statsOut(t, stdout)
	if err != nil || !strings.Contains(stdout, `"groups": []`) || d.Totals.Rate != nil || d.Totals.AvgTalk != nil || !strings.Contains(stderr, "No calls counted") {
		t.Errorf("empty window: err %v, stderr %q\n%s", err, stderr, stdout)
	}
}

// The window is frozen at the start of the run and always sent explicitly; a
// short page ends each walk (the list sends no total_count); --number reads its
// outbound calls from it and its inbound calls to it, with the direction sent.
func TestCallStats_numberWalksBothDirectionsInAFrozenWindow(t *testing.T) {
	setFakeCreds(t)
	statsAt(t)
	queries := statsServer(t, map[string][]string{
		"outbound": statsManyCalls(25),
		"inbound":  {statsCall("i1", "inbound", "+14155550123", "14155550100", `,"answer_time":"2026-11-10 10:00:05"`)},
	}, nil)

	err, stdout, _ := execCmd(t, "voice", "calls", "stats", "--number", "+14155550100", "-o", "json")
	q := queries()
	if err != nil || len(q) != 3 || statsOut(t, stdout).CallsCounted != 26 {
		t.Fatalf("err %v, %d requests, want 3 for 26 calls:\n%s", err, len(q), stdout)
	}
	out := q[0].Get("call_direction") + q[0].Get("from_number") + q[0].Get("end_time__gte") + q[0].Get("end_time__lte")
	in := q[2].Get("call_direction") + q[2].Get("to_number") + q[2].Get("from_number") + q[2].Get("end_time__lte")
	if out != "outbound+141555501002026-11-05 12:00:002026-11-12 12:00:00" || in != "inbound+141555501002026-11-12 12:00:00" {
		t.Errorf("outbound query %q, inbound query %q", out, in)
	}

	// An --until past the start of the run is held to it.
	_, _, _ = execCmd(t, "voice", "calls", "stats", "--since", "2026-11-10", "--until", "2026-11-30", "--number", "+14155550100")
	if q := queries()[3]; q.Get("end_time__gte") != "2026-11-10 00:00:00" || q.Get("end_time__lte") != "2026-11-12 12:00:00" {
		t.Errorf("window %q to %q", q.Get("end_time__gte"), q.Get("end_time__lte"))
	}

	// --max-calls is global: the outbound read reaching it skips the inbound one.
	sent := len(queries())
	_, stdout, _ = execCmd(t, "voice", "calls", "stats", "--number", "+14155550100", "--max-calls", "25", "-o", "json")
	if d := statsOut(t, stdout); len(queries())-sent != 2 || d.CallsRead != 25 || !d.Truncated {
		t.Errorf("--max-calls 25: %d requests, read %d, truncated %v; want 2, 25, true", len(queries())-sent, d.CallsRead, d.Truncated)
	}

	// --dry-run sends nothing and shows the first request of each read.
	clientForTest.DryRun = true
	before := len(queries())
	_, _, stderr := execCmd(t, "voice", "calls", "stats", "--number", "+14155550100", "--dry-run")
	if len(queries()) != before || strings.Count(stderr, "[dry-run] GET") != 2 {
		t.Errorf("--dry-run: %d requests, stderr %q", len(queries())-before, stderr)
	}
}

// --max-calls stops the walk at the calls it allows. A run that ends on the
// list's last page is not truncated; one that leaves rows behind is, which the
// walk's meta.next says.
func TestCallStats_maxCallsTruncates(t *testing.T) {
	setFakeCreds(t)
	for _, tc := range []struct {
		rows, max, requests int
		truncated           bool
	}{{40, 40, 2, false}, {41, 40, 2, true}, {45, 30, 2, true}} {
		queries := statsServer(t, map[string][]string{"": statsManyCalls(tc.rows)}, nil)
		err, stdout, stderr := execCmd(t, "voice", "calls", "stats", "--max-calls", strconv.Itoa(tc.max), "-o", "json")
		d := statsOut(t, stdout)
		if err != nil || d.Truncated != tc.truncated || d.CallsRead != min(tc.rows, tc.max) || len(queries()) != tc.requests ||
			strings.Contains(stderr, "--max-calls") != tc.truncated {
			t.Errorf("%d rows, cap %d: err %v, truncated %v, read %d, %d requests, stderr %q", tc.rows, tc.max, err, d.Truncated, d.CallsRead, len(queries()), stderr)
		}
	}
}

// A page that fails after the first leaves what was read printed, truncated,
// with the error's exit code (4 for a rate limit); a first page that fails
// prints nothing.
func TestCallStats_failureMidWalk(t *testing.T) {
	setFakeCreds(t)
	recordWaits(t)
	statsServer(t, map[string][]string{"": statsManyCalls(45)}, func(n int) int {
		if n >= 2 {
			return http.StatusTooManyRequests
		}
		return 0
	})
	err, stdout, stderr := execCmd(t, "voice", "calls", "stats", "-o", "json")
	var ce *clierr.Error
	if d := statsOut(t, stdout); !errors.As(err, &ce) || ce.ExitCode() != 4 || !d.Truncated || d.CallsRead != 20 || !strings.Contains(stderr, "stopped after 20 calls") {
		t.Errorf("err %v, stderr %q, result %+v", err, stderr, d)
	}

	statsServer(t, map[string][]string{"": statsManyCalls(5)}, func(int) int { return http.StatusInternalServerError })
	if err, stdout, _ := execCmd(t, "voice", "calls", "stats", "-o", "json"); err == nil || stdout != "" {
		t.Errorf("a failed first page: err %v, stdout %q", err, stdout)
	}
}

func TestCallStats_refusesBadFlagsBeforeAnyRequest(t *testing.T) {
	setFakeCreds(t)
	queries := statsServer(t, nil, nil)
	for args, want := range map[string]string{
		"--number +14155550100 --from +14155550123": "can't be combined",
		"--number +14155550100 --direction inbound": "can't be combined",
		"--number 415-555":                          "not an E.164 number",
		"--by week":                                 "unknown value",
		"--max-calls -1":                            "must be 0 or more",
	} {
		err, _, _ := execCmd(t, append([]string{"voice", "calls", "stats"}, strings.Fields(args)...)...)
		var ce *clierr.Error
		if !errors.As(err, &ce) || !strings.Contains(ce.Message, want) || len(queries()) != 0 {
			t.Errorf("%s: %v with %d requests, want %q", args, err, len(queries()), want)
		}
	}
}
