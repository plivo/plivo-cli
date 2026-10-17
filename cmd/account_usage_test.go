package cmd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
)

// Each flag is its own API field, sent as given; --product repeats.
func TestAccountUsage_flagsReachTheQuery(t *testing.T) {
	setFakeCreds(t)
	urls, _ := sipServer(t, http.StatusOK, `{"meta":{},"usage":[]}`)

	err, _, _ := execCmd(t, "account", "usage", "--from-date", "2026-10-01", "--to-date", "2026-11-01",
		"--granularity", "month", "--product", "voice", "--product", "message, other", "--direction", "outbound",
		"--country", "US", "--subaccount", "SA1", "--hangup-cause", "4000", "--error-code", "200", "--limit", "1000")
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

// --all follows next_page_token, retrying a rate-limited page, and keeps the
// first page's totals and other charges, which later pages do not send.
func TestAccountUsage_allFollowsTheTokenAndKeepsPageOne(t *testing.T) {
	setFakeCreds(t)
	waits := recordWaits(t)
	var mu sync.Mutex
	var tokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		tokens = append(tokens, r.URL.Query().Get("page_token"))
		n := len(tokens)
		mu.Unlock()
		switch n {
		case 1:
			_, _ = w.Write([]byte(`{"meta":{"total_spend":3.5,"next_page_token":"p2"},"usage":[{"product":"voice"}],` +
				`"other_charges":[{"description":"Number Charges"}]}`))
		case 2:
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			_, _ = w.Write([]byte(`{"meta":{},"usage":[{"product":"message"}]}`))
		}
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })

	err, stdout, _ := execCmd(t, "account", "usage", "--all", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mu.Lock()
	got := strings.Join(tokens, ",")
	mu.Unlock()
	if got != ",p2,p2" || len(*waits) != 1 {
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
		if !strings.Contains(strings.Join(lines, "\n"), want) {
			t.Errorf("table lacks %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "add --all") {
		t.Errorf("no note that more rows are left: %q", stderr)
	}
}

// Dates go to the API as given, so one it cannot read is refused first.
func TestAccountUsage_badDatesRefusedBeforeAnyRequest(t *testing.T) {
	for want, flags := range map[string][]string{
		`could not read "2026/10/01"`:       {"--since", "2026/10/01"},
		`could not read "2026-10-17 00:00"`: {"--to-date", "2026-10-17 00:00"},
		"same filter":                       {"--until", "2026-10-17", "--to-date", "2026-10-17"},
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
