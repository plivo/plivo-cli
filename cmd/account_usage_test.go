package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
)

// Each flag is its own API field, sent as given (the country upper-cased);
// --product repeats.
func TestAccountUsage_flagsReachTheQuery(t *testing.T) {
	setFakeCreds(t)
	urls, _ := sipServer(t, http.StatusOK, `{"meta":{},"usage":[]}`)

	err, _, _ := execCmd(t, "account", "usage", "--from-date", "2026-10-01", "--to-date", "2026-11-01",
		"--granularity", "month", "--product", "voice", "--product", "message, other", "--direction", "outbound",
		"--country", "us", "--subaccount", "SA1", "--hangup-cause", "4000", "--error-code", "200", "--limit", "1000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u, _ := url.Parse(urls()[0])
	want := url.Values{"from_date": {"2026-10-01"}, "to_date": {"2026-11-01"}, "granularity": {"month"},
		"product": {"voice", "message,other"}, "direction": {"outbound"}, "country": {"US"}, "subaccount_auth": {"SA1"},
		"hangup_cause": {"4000"}, "error_code": {"200"}, "page_size": {"1000"}}
	if got := u.Query(); !reflect.DeepEqual(got, want) || !strings.HasSuffix(u.Path, "/UsageSummary/") {
		t.Fatalf("GET %s?%v\nwant query %v", u.Path, got, want)
	}
}

// usagePages serves a summary paged by token: page n holds one usage row and,
// before page last, next_page_token "p<n+1>"; page 1 also the totals and other
// charges. fail, when set, may answer request n (1-based) with a status
// instead. Returns the page_token each request sent.
func usagePages(t *testing.T, last int, fail func(n int) int) func() []string {
	t.Helper()
	var mu sync.Mutex
	var tokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("page_token")
		mu.Lock()
		tokens = append(tokens, token)
		n := len(tokens)
		mu.Unlock()
		if fail != nil && fail(n) != 0 {
			w.WriteHeader(fail(n))
			return
		}
		page, _ := strconv.Atoi(strings.TrimPrefix(token, "p"))
		page = max(page, 1)
		meta, more := map[string]any{}, ""
		if page == 1 {
			meta["total_spend"], more = 3.5, `,"other_charges":[{"description":"Number Charges"}]`
		}
		if page < last {
			meta["next_page_token"] = fmt.Sprintf("p%d", page+1)
		}
		m, _ := json.Marshal(meta)
		_, _ = fmt.Fprintf(w, `{"meta":%s,"usage":[{"product":"row-%d"}]%s}`, m, page, more)
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), tokens...)
	}
}

// --all follows next_page_token, retrying a rate-limited page, and keeps the
// first page's totals and other charges, which later pages do not send.
func TestAccountUsage_allFollowsTheTokenAndKeepsPageOne(t *testing.T) {
	setFakeCreds(t)
	waits := recordWaits(t)
	tokens := usagePages(t, 2, func(n int) int {
		if n == 2 {
			return http.StatusTooManyRequests
		}
		return 0
	})

	err, stdout, _ := execCmd(t, "account", "usage", "--all", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Join(tokens(), ","); got != ",p2,p2" || len(*waits) != 1 {
		t.Fatalf("page tokens sent = %q after %d waits, want \",p2,p2\" after 1", got, len(*waits))
	}
	rows, meta := dataRows(t, stdout, "usage")
	if len(rows) != 2 || meta["total_spend"] != 3.5 || meta["next_page_token"] != nil || meta["truncated"] != nil {
		t.Fatalf("rows=%v meta=%v, want both rows, page 1's total_spend and a null token", rows, meta)
	}
	if !strings.Contains(stdout, `"Number Charges"`) {
		t.Errorf("page 1's other_charges are gone:\n%s", stdout)
	}
}

// A page that fails after the first ends the walk: the rows read print, with
// truncated and the token of the page that failed, and the run fails with it.
func TestAccountUsage_allPrintsTheRowsReadBeforeAPageFails(t *testing.T) {
	setFakeCreds(t)
	usagePages(t, 3, func(n int) int {
		if n == 2 {
			return http.StatusInternalServerError
		}
		return 0
	})

	err, stdout, stderr := execCmd(t, "account", "usage", "--all", "-o", "json")
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.StatusCode != http.StatusInternalServerError {
		t.Fatalf("want page 2's 500 after the rows, got %v", err)
	}
	rows, meta := dataRows(t, stdout, "usage")
	if len(rows) != 1 || meta["total_spend"] != 3.5 || meta["truncated"] != true || meta["next_page_token"] != "p2" {
		t.Fatalf("rows=%v meta=%v, want page 1's row and totals, truncated, next_page_token p2", rows, meta)
	}
	if !strings.Contains(stderr, "stopped after 1 usage rows") {
		t.Errorf("no warning on stderr: %q", stderr)
	}
}

// --all stops after 100 pages and says where, in truncated and the token of
// the first page left unread.
func TestAccountUsage_allStopsAfter100Pages(t *testing.T) {
	setFakeCreds(t)
	tokens := usagePages(t, 105, nil)

	err, stdout, stderr := execCmd(t, "account", "usage", "--all", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rows, meta := dataRows(t, stdout, "usage")
	if n := len(tokens()); n != 100 || len(rows) != 100 || meta["truncated"] != true || meta["next_page_token"] != "p101" {
		t.Fatalf("%d requests, %d rows, meta=%v; want 100, 100, truncated, next_page_token p101", n, len(rows), meta)
	}
	if !strings.Contains(stderr, "stopped after 100 pages") {
		t.Errorf("no warning on stderr: %q", stderr)
	}
}

// The table names each period to its granularity, rounds float noise, and
// adds the subaccount and cause columns only because a row has them.
func TestAccountUsage_table(t *testing.T) {
	setFakeCreds(t)
	sipServer(t, http.StatusOK, `{"meta":{"granularity":"month","currency":"USD","total_spend":472.37760000000003,
		"next_page_token":"p2"},"usage":[{"from_date":"2026-05-01T00:00:00Z","product":"message","type":"sms",
		"country":"PR","direction":"outbound","total_units":5,"total_amount":0.17145,"subaccount_auth":"SA1",
		"error_code":"200","error_reason":"Opt-out block"},{"from_date":"2026-05-01T00:00:00Z","product":"voice",
		"type":"pstn","total_units":3,"duration_seconds":61,"total_amount":0}],"other_charges":[{"from_date":
		"2026-06-01T00:00:00Z","description":"Number Lookup","total_units":1,"total_amount":0.37760000000000005}]}`)

	err, stdout, stderr := execCmd(t, "account", "usage", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(stdout, "\n") {
		lines = append(lines, strings.Join(strings.Fields(l), " "))
	}
	for _, want := range []string{
		"PERIOD PRODUCT TYPE COUNTRY DIRECTION UNITS DURATION AMOUNT SUBACCOUNT CAUSE",
		"2026-05 message sms PR outbound 5 0.17145 SA1 200 Opt-out block", "2026-05 voice pstn 3 61 0",
		"2026-06 Number Lookup 1 0.3776", "total_spend: 472.3776", "currency: USD",
	} {
		if !slices.Contains(lines, want) {
			t.Errorf("table lacks the line %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "add --all") {
		t.Errorf("no note that more rows are left: %q", stderr)
	}
}

// Dates go to the API as given, so one it cannot read is refused first, and
// so is a window with no day in it: --until is exclusive, and left out it is
// today in UTC (here the 17th, while UTC+5:30 has the 18th).
func TestAccountUsage_badDatesRefusedBeforeAnyRequest(t *testing.T) {
	listTimeNow = func() time.Time { return time.Date(2026, 10, 18, 1, 0, 0, 0, time.FixedZone("UTC+5:30", 19800)) }
	t.Cleanup(func() { listTimeNow = time.Now })
	for want, flags := range map[string][]string{
		`could not read "2026/10/01"`:                             {"--since", "2026/10/01"},
		`could not read "2026-10-17 00:00"`:                       {"--to-date", "2026-10-17 00:00"},
		"same filter":                                             {"--until", "2026-10-17", "--to-date", "2026-10-17"},
		"--until (2026-10-17) must be after --since (2026-10-17)": {"--since", "2026-10-17", "--until", "2026-10-17"},
		"--until (2026-10-01) must be after --since (2026-10-17)": {"--from-date", "2026-10-17", "--to-date", "2026-10-01"},
		"must be before today (2026-10-17, UTC)":                  {"--since", "2026-10-17"},
	} {
		t.Run(want, func(t *testing.T) {
			requireRefusedBeforeAnyRequest(t, append([]string{"account", "usage"}, flags...), want)
		})
	}
}

// A window too long for its granularity is Plivo's to refuse, as it says.
func TestAccountUsage_windowLimitShownAsSent(t *testing.T) {
	setFakeCreds(t)
	const msg = "granularity=hour supports a max window of 7 days (requested 16 days); use granularity=day for larger windows"
	sipServer(t, http.StatusBadRequest, `{"api_id":"x","error":"`+msg+`"}`)

	err, _, _ := execCmd(t, "account", "usage", "--granularity", "hour", "--since", "2026-10-01", "--until", "2026-10-17")
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Message != msg || ce.StatusCode != http.StatusBadRequest {
		t.Fatalf("want the API's 400 as sent, got %v", err)
	}
}
