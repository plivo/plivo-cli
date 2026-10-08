package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/docs"
	"github.com/spf13/cobra"
)

func TestValidateEnum(t *testing.T) {
	t.Run("unset passes", func(t *testing.T) {
		for _, in := range []string{"", "   "} {
			v := in
			if err := validateEnum("direction", &v, directionValues...); err != nil || v != "" {
				t.Fatalf("validateEnum(%q) = %v, value %q; want nil, \"\"", in, err, v)
			}
		}
	})

	t.Run("documented value passes, trimmed", func(t *testing.T) {
		v := "  inbound "
		if err := validateEnum("direction", &v, directionValues...); err != nil || v != "inbound" {
			t.Fatalf("got %v, value %q; want nil, \"inbound\"", err, v)
		}
	})

	t.Run("unknown value is a BAD_FLAG naming the allowed set", func(t *testing.T) {
		v := "sideways"
		var ce *clierr.Error
		if !errors.As(validateEnum("direction", &v, directionValues...), &ce) || ce.Code != clierr.CodeBadFlag {
			t.Fatalf("want BAD_FLAG, got %#v", ce)
		}
		if ce.Context["flag"] != "direction" || ce.Context["value"] != "sideways" {
			t.Errorf("context = %v", ce.Context)
		}
		if !strings.Contains(ce.Hint, `"inbound", "outbound"`) {
			t.Errorf("hint does not list the allowed values: %q", ce.Hint)
		}
	})

	t.Run("wrong case names the right spelling", func(t *testing.T) {
		v := "UPDATE_required"
		var ce *clierr.Error
		if !errors.As(validateEnum("status", &v, tollfreeStatuses...), &ce) {
			t.Fatal("want an error")
		}
		if !strings.Contains(ce.Hint, `case-sensitive: use "UPDATE_REQUIRED"`) {
			t.Errorf("hint = %q", ce.Hint)
		}
	})
}

func TestValidateEnumList(t *testing.T) {
	v := " voice , sms"
	if err := validateEnumList("services", &v, numberServices...); err != nil || v != "voice,sms" {
		t.Fatalf("got %v, value %q; want nil, \"voice,sms\"", err, v)
	}
	for _, bad := range []string{"voice,fax", "voice,,sms", "sms,", ","} {
		v := bad
		var ce *clierr.Error
		if !errors.As(validateEnumList("services", &v, numberServices...), &ce) || ce.Code != clierr.CodeBadFlag {
			t.Errorf("%q: want BAD_FLAG, got %#v", bad, ce)
		}
	}
}

// The whole point is to fail before the API is asked: an unknown value sent
// through comes back as an empty list with exit 0.
func TestListFilters_unknownValueFailsBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		flag string
		args []string
	}{
		{"direction", []string{"sip", "calls", "list", "--direction", "sideways"}},
		{"hangup-source", []string{"sip", "calls", "list", "--hangup-source", "plivo"}},
		{"stir-verification", []string{"sip", "calls", "list", "--stir-verification", "verified"}},
		{"direction", []string{"sip", "trunks", "list", "--direction", "both"}},
		{"direction", []string{"voice", "calls", "list", "--direction", "Inbound"}},
		{"state", []string{"messaging", "sms", "list", "--state", "read"}},
		{"direction", []string{"messaging", "whatsapp", "list", "--direction", "out"}},
		{"state", []string{"messaging", "mms", "list", "--state", "bogus"}},
		{"type", []string{"numbers", "list", "--type", "bogus"}},
		{"services", []string{"numbers", "list", "--services", "voice,fax"}},
		{"type", []string{"numbers", "search", "--country", "US", "--type", "toll-free"}},
		{"status", []string{"voice", "multiparty", "list", "--status", "live"}},
		{"status", []string{"messaging", "sms", "tollfree", "list", "--status", "IN_REVIEW"}},
		{"status", []string{"verify", "sessions", "list", "--status", "pending"}},
		{"state", []string{"agents", "list", "--state", "draft"}},
		{"status", []string{"numbers", "compliance", "list", "--status", "approved"}},
		{"number-type", []string{"numbers", "compliance", "list", "--number-type", "national"}},
		{"user-type", []string{"numbers", "compliance", "list", "--user-type", "company"}},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			setFakeCreds(t)
			_, count := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

			err, _, _ := execCmd(t, tc.args...)
			var ce *clierr.Error
			if !errors.As(err, &ce) || ce.Code != clierr.CodeBadFlag {
				t.Fatalf("want BAD_FLAG, got %v", err)
			}
			if ce.Context["flag"] != tc.flag {
				t.Errorf("flag in context = %v, want %s", ce.Context["flag"], tc.flag)
			}
			if n := count(); n != 0 {
				t.Fatalf("%d request(s) went out before validation", n)
			}
		})
	}
}

// Every documented value must still reach the API, exactly as spelled there;
// --dry-run must not skip the check.
func TestListFilters_documentedValuesReachTheQuery(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"sip", "calls", "list", "--stir-verification", "Not Verified", "--hangup-source", "zentrunk"},
			[]string{"stir_verification=Not+Verified", "hangup_source=zentrunk"}},
		{[]string{"sip", "trunks", "list", "--direction", "inbound"}, []string{"trunk_direction=inbound"}},
		{[]string{"voice", "calls", "list", "--direction", " outbound "}, []string{"call_direction=outbound"}},
		{[]string{"messaging", "whatsapp", "list", "--state", "received", "--direction", "inbound"},
			[]string{"message_state=received", "message_direction=inbound"}},
		{[]string{"numbers", "list", "--type", "national", "--services", "voice, sms"},
			[]string{"type=national", "services=voice%2Csms"}},
		{[]string{"numbers", "search", "--country", "US", "--type", "national"}, []string{"type=national"}},
		{[]string{"voice", "multiparty", "list", "--status", "initialized"}, []string{"status=initialized"}},
		{[]string{"messaging", "sms", "tollfree", "list", "--status", "UPDATE_REQUIRED"}, []string{"status=UPDATE_REQUIRED"}},
		{[]string{"verify", "sessions", "list", "--status", "in-progress"}, []string{"status=in-progress"}},
		{[]string{"agents", "list", "--state", "PAUSED"}, []string{"state=PAUSED"}},
		{[]string{"numbers", "compliance", "list", "--status", "accepted", "--number-type", "mobile", "--user-type", "individual"},
			[]string{"status=accepted", "number_type=mobile", "user_type=individual"}},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			setFakeCreds(t)
			urls, _ := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

			if err, _, _ := execCmd(t, tc.args...); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := urls()
			if len(got) != 1 {
				t.Fatalf("expected 1 request, got %v", got)
			}
			for _, w := range tc.want {
				if !strings.Contains(got[0], w) {
					t.Errorf("query missing %s\n  got: %s", w, got[0])
				}
			}
		})
	}

	t.Run("dry-run still validates", func(t *testing.T) {
		setFakeCreds(t)
		err, _, _ := execCmd(t, "numbers", "list", "--type", "bogus", "--dry-run")
		var ce *clierr.Error
		if !errors.As(err, &ce) || ce.Code != clierr.CodeBadFlag {
			t.Fatalf("want BAD_FLAG under --dry-run, got %v", err)
		}
	})
}

// The compliance list sends its rows under "compliances", not "objects"; read
// from the wrong key, the table is empty however many applications exist.
func TestComplianceList_tableReadsTheCompliancesKey(t *testing.T) {
	setFakeCreds(t)
	sipServer(t, http.StatusOK, `{"api_id":"x","meta":{"limit":20,"offset":0,"total_count":1},
		"compliances":[{"compliance_id":"00000000-0000-0000-0000-000000000000","alias":"acme-in",
		"status":"accepted","country_iso":"IN","number_type":"local","user_type":"business"}]}`)

	err, stdout, _ := execCmd(t, "numbers", "compliance", "list", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"00000000-0000-0000-0000-000000000000", "acme-in", "accepted"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table missing %q:\n%s", want, stdout)
		}
	}
}

// Every list backed by a paged API holds --limit to 1-20 and --offset to 0 or
// more before any request. Walks the tree, so a list that registers its own
// --limit instead of the shared one fails here.
func TestListFlags_everyPagedListChecksThePageFirst(t *testing.T) {
	var lists []*cobra.Command
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		// docs search is local: its --limit caps search hits, not an API page.
		if c.Flags().Lookup("limit") != nil && c.CommandPath() != "plivo docs search" {
			lists = append(lists, c)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
	if len(lists) < 25 {
		t.Fatalf("found only %d lists with --limit; did the walk break?", len(lists))
	}
	for _, c := range lists {
		path := strings.Fields(c.CommandPath())[1:]
		for range strings.Count(c.Use, "<") {
			path = append(path, "placeholder")
		}
		for _, bad := range [][]string{{"--limit", "21"}, {"--limit", "0"}, {"--offset", "-1"}} {
			args := append(append([]string{}, path...), bad...)
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				setFakeCreds(t)
				_, count := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

				err, _, _ := execCmd(t, args...)
				var ce *clierr.Error
				if !errors.As(err, &ce) || ce.Code != clierr.CodeBadFlag {
					t.Fatalf("want BAD_FLAG, got %v", err)
				}
				if n := count(); n != 0 {
					t.Fatalf("%d request(s) went out before the page check", n)
				}
			})
		}
	}
}

func TestListJSON(t *testing.T) {
	cases := []struct{ name, body, key, want string }{
		{"null rows become []", `{"meta":{},"objects":null}`, "objects", `[]`},
		{"missing rows become []", `{"meta":{}}`, "objects", `[]`},
		{"rows under another key", `{"compliances":null}`, "compliances", `[]`},
		{"rows kept as sent", `{"objects":[{"id":"a"}]}`, "objects", `[{"id":"a"}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := listJSON(&buf, json.RawMessage(tc.body), tc.key); err != nil {
				t.Fatal(err)
			}
			var env struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
				t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
			}
			var got bytes.Buffer
			_ = json.Compact(&got, env.Data[tc.key])
			if got.String() != tc.want {
				t.Fatalf("%s = %s, want %s", tc.key, got.String(), tc.want)
			}
		})
	}

	t.Run("a body that is not an object passes through", func(t *testing.T) {
		var buf bytes.Buffer
		if err := listJSON(&buf, json.RawMessage(`[1,2]`), "objects"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "1,") {
			t.Fatalf("body changed: %s", buf.String())
		}
	})
}

// The commands the empty-list rule was written for: the SIP Trunking ACL list
// sends "objects": null, and the CLI-built lists left their slices nil.
func TestEmptyLists_renderAnEmptyArray(t *testing.T) {
	cases := []struct {
		args []string
		body string
		want string
	}{
		{[]string{"sip", "ip-acl", "list"}, `{"api_id":"x","meta":{"total_count":0},"objects":null}`, `"objects": []`},
		{[]string{"numbers", "compliance", "list"}, `{"api_id":"x","meta":{"total_count":0},"compliances":null}`, `"compliances": []`},
		{[]string{"voice", "conferences", "list"}, `{"api_id":"x"}`, `"conferences": []`},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			setFakeCreds(t)
			sipServer(t, http.StatusOK, tc.body)

			err, stdout, _ := execCmd(t, append(tc.args, "-o", "json")...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Fatalf("want %s in:\n%s", tc.want, stdout)
			}
			if tc.want != `"objects": []` && strings.Contains(stdout, `"objects"`) {
				t.Fatalf("added an objects key to a list that has none:\n%s", stdout)
			}
		})
	}

	t.Run("support with no escalations", func(t *testing.T) {
		setFakeCreds(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"api_id":"x","status":"ok","data":{}}`))
		}))
		t.Cleanup(srv.Close)
		supportClientForTest = &api.Client{BaseURL: srv.URL, BuddyBaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001",
			AuthToken: "tok", AomUUID: "aom-1", HTTP: &http.Client{}}
		t.Cleanup(func() { supportClientForTest = nil })

		err, stdout, _ := execCmd(t, "support", "-o", "json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stdout, `"data": []`) {
			t.Fatalf("want an empty array:\n%s", stdout)
		}
	})

	t.Run("docs list with an empty index", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		t.Cleanup(srv.Close)
		docsFetcherForTest = &docs.Fetcher{HTTP: srv.Client(), CacheDir: t.TempDir(),
			BaseIndex: srv.URL + "/llms.txt", BaseFull: srv.URL + "/llms-full.txt"}
		t.Cleanup(func() { docsFetcherForTest = nil })

		err, stdout, _ := execCmd(t, "docs", "list", "-o", "json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stdout, `"data": []`) {
			t.Fatalf("want an empty array:\n%s", stdout)
		}
	})
}

// pagedServer serves rows rows under key, honouring limit and offset like the
// real endpoints, with meta.total_count set to total (left out when total < 0).
// fail, when set, may answer request n (1-based) instead: status and
// Retry-After.
func pagedServer(t *testing.T, key string, rows, total int, fail func(n int) (int, string)) func() []string {
	t.Helper()
	var mu sync.Mutex
	var offsets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mu.Lock()
		offsets = append(offsets, q.Get("offset"))
		n := len(offsets)
		mu.Unlock()
		if fail != nil {
			if status, after := fail(n); status != 0 {
				w.Header().Set("Retry-After", after)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"too many requests"}`))
				return
			}
		}
		limit, _ := strconv.Atoi(q.Get("limit"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		page := []string{}
		for i := offset; i < offset+limit && i < rows; i++ {
			page = append(page, fmt.Sprintf(`{"id":"row-%d","name":"<%d> & co"}`, i, i))
		}
		meta := fmt.Sprintf(`{"limit":%d,"offset":%d,"total_count":%d}`, limit, offset, total)
		if total < 0 {
			meta = fmt.Sprintf(`{"limit":%d,"offset":%d}`, limit, offset)
		}
		_, _ = fmt.Fprintf(w, `{"api_id":"x","meta":%s,%q:[%s]}`, meta, key, strings.Join(page, ","))
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), offsets...)
	}
}

// recordWaits swaps the retry sleep for a recorder, so no test waits.
func recordWaits(t *testing.T) *[]time.Duration {
	t.Helper()
	var waits []time.Duration
	waitBeforeRetry = func(d time.Duration) { waits = append(waits, d) }
	t.Cleanup(func() { waitBeforeRetry = time.Sleep })
	return &waits
}

// dataRows decodes the rows and meta out of -o json's {"data": {...}}.
func dataRows(t *testing.T, stdout, key string) ([]map[string]any, map[string]any) {
	t.Helper()
	var out struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	var rows []map[string]any
	var meta map[string]any
	_ = json.Unmarshal(out.Data[key], &rows)
	_ = json.Unmarshal(out.Data["meta"], &meta)
	return rows, meta
}

func TestListAll_walksEveryPageIntoOneEnvelope(t *testing.T) {
	setFakeCreds(t)
	hits := pagedServer(t, "objects", 45, 45, nil)

	err, stdout, _ := execCmd(t, "sip", "calls", "list", "--all", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Join(hits(), ","); got != "0,20,40" {
		t.Fatalf("offsets requested = %s, want 0,20,40", got)
	}
	rows, meta := dataRows(t, stdout, "objects")
	if len(rows) != 45 || rows[0]["id"] != "row-0" || rows[44]["id"] != "row-44" {
		t.Fatalf("merged %d rows, want row-0..row-44 in order", len(rows))
	}
	if meta["total_count"] != float64(45) || meta["truncated"] != nil {
		t.Errorf("meta = %v, want the server's total_count and no truncated", meta)
	}
	if !strings.Contains(stdout, `"<44> & co"`) {
		t.Errorf("rows were re-escaped on the way through:\n%s", stdout)
	}
}

func TestListAll_stopConditions(t *testing.T) {
	cases := []struct {
		name        string
		rows, total int
		limit       string
		want        string
	}{
		{"short page ends it without a total", 30, -1, "20", "0,20"},
		{"total_count ends it on a full page", 40, 40, "20", "0,20"},
		{"empty first page", 0, 0, "20", "0"},
		{"advances by rows returned, not by --limit", 7, 7, "5", "0,5"},
		{"an empty page ends it whatever total_count says", 15, 40, "20", "0,15"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			hits := pagedServer(t, "objects", tc.rows, tc.total, nil)

			err, stdout, _ := execCmd(t, "voice", "calls", "list", "--all", "--limit", tc.limit, "-o", "json")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.Join(hits(), ","); got != tc.want {
				t.Fatalf("offsets requested = %s, want %s", got, tc.want)
			}
			if rows, _ := dataRows(t, stdout, "objects"); len(rows) != tc.rows {
				t.Fatalf("got %d rows, want %d", len(rows), tc.rows)
			}
		})
	}
}

// Some endpoints return fewer rows than asked for. When meta.total_count is
// sent, only reaching it ends the walk, so a short page is not taken for the
// last one.
func TestListAll_aShortPageDoesNotEndItBeforeTheTotal(t *testing.T) {
	setFakeCreds(t)
	var mu sync.Mutex
	var offsets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		mu.Lock()
		offsets = append(offsets, r.URL.Query().Get("offset"))
		mu.Unlock()
		page := []string{}
		for i := offset; i < offset+10 && i < 25; i++ { // this server's page is 10 rows
			page = append(page, fmt.Sprintf(`{"id":"row-%d"}`, i))
		}
		_, _ = fmt.Fprintf(w, `{"meta":{"total_count":25},"objects":[%s]}`, strings.Join(page, ","))
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })

	err, stdout, _ := execCmd(t, "voice", "calls", "list", "--all", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mu.Lock()
	got := strings.Join(offsets, ",")
	mu.Unlock()
	if got != "0,10,20" {
		t.Fatalf("offsets requested = %s, want 0,10,20", got)
	}
	if rows, _ := dataRows(t, stdout, "objects"); len(rows) != 25 {
		t.Fatalf("got %d rows, want all 25", len(rows))
	}
}

// A list larger than the safety stop gets 100 pages, a warning, and
// meta.truncated so a script can tell it did not get everything.
func TestListAll_stopsAfter100Pages(t *testing.T) {
	setFakeCreds(t)
	hits := pagedServer(t, "objects", 5000, 5000, nil)

	err, stdout, stderr := execCmd(t, "messaging", "sms", "list", "--all", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := len(hits()); n != 100 {
		t.Fatalf("made %d requests, want 100", n)
	}
	rows, meta := dataRows(t, stdout, "objects")
	if len(rows) != 2000 || meta["truncated"] != true || meta["total_count"] != float64(5000) {
		t.Fatalf("rows=%d meta=%v, want 2000 rows, truncated, total_count 5000", len(rows), meta)
	}
	if !strings.Contains(stderr, "stopped after 100 pages") {
		t.Errorf("no warning on stderr: %q", stderr)
	}
}

func TestListAll_retriesARateLimitedPage(t *testing.T) {
	setFakeCreds(t)
	waits := recordWaits(t)
	// Requests 2 and 3 (the second page and its first retry) are refused.
	hits := pagedServer(t, "objects", 25, 25, func(n int) (int, string) {
		if n == 2 || n == 3 {
			return http.StatusTooManyRequests, "2"
		}
		return 0, ""
	})

	err, stdout, _ := execCmd(t, "voice", "calls", "list", "--all", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Join(hits(), ","); got != "0,20,20,20" {
		t.Fatalf("offsets requested = %s, want 0,20,20,20", got)
	}
	if fmt.Sprint(*waits) != "[2s 2s]" {
		t.Errorf("waits = %v, want the server's 2s twice", *waits)
	}
	if rows, _ := dataRows(t, stdout, "objects"); len(rows) != 25 {
		t.Fatalf("got %d rows, want 25", len(rows))
	}
}

func TestListAll_givesUpAfterThreeRetries(t *testing.T) {
	setFakeCreds(t)
	waits := recordWaits(t)
	hits := pagedServer(t, "objects", 25, 25, func(int) (int, string) { return http.StatusTooManyRequests, "" })

	err, _, _ := execCmd(t, "voice", "calls", "list", "--all")
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Code != clierr.CodeRateLimited {
		t.Fatalf("want RATE_LIMITED, got %v", err)
	}
	if n := len(hits()); n != 4 {
		t.Fatalf("made %d requests, want 1 + 3 retries", n)
	}
	if fmt.Sprint(*waits) != "[1s 2s 4s]" {
		t.Errorf("waits = %v, want 1s 2s 4s without a Retry-After", *waits)
	}
}

// Without --all a 429 is returned as is: the retry lives in the walk only.
func TestListAll_noRetryWithoutAll(t *testing.T) {
	setFakeCreds(t)
	recordWaits(t)
	hits := pagedServer(t, "objects", 25, 25, func(int) (int, string) { return http.StatusTooManyRequests, "1" })

	err, _, _ := execCmd(t, "voice", "calls", "list")
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Code != clierr.CodeRateLimited || len(hits()) != 1 {
		t.Fatalf("want one request and RATE_LIMITED, got %v after %d", err, len(hits()))
	}
}

func TestRetryWait(t *testing.T) {
	cases := []struct {
		attempt             int
		retryAfter, timeout time.Duration
		want                time.Duration
	}{
		{0, 0, 0, time.Second},
		{2, 0, 0, 4 * time.Second},
		{0, 5 * time.Second, 0, 5 * time.Second},
		{0, time.Hour, 0, 30 * time.Second},
		{0, 20 * time.Second, 10 * time.Second, 10 * time.Second},
	}
	for _, tc := range cases {
		if got := retryWait(tc.attempt, tc.retryAfter, tc.timeout); got != tc.want {
			t.Errorf("retryWait(%d, %v, %v) = %v, want %v", tc.attempt, tc.retryAfter, tc.timeout, got, tc.want)
		}
	}
}

func TestListAll_refusesOffset(t *testing.T) {
	setFakeCreds(t)
	hits := pagedServer(t, "objects", 5, 5, nil)

	err, _, _ := execCmd(t, "sip", "trunks", "list", "--all", "--offset", "20")
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Code != clierr.CodeBadFlag {
		t.Fatalf("want BAD_FLAG, got %v", err)
	}
	if len(hits()) != 0 {
		t.Fatal("a request went out")
	}
}

// The real client (not a test one) so --dry-run reaches it: anything actually
// sent would go to the live API with fake credentials and fail.
func TestListAll_dryRunSendsNothing(t *testing.T) {
	setFakeCreds(t)

	err, _, stderr := execCmd(t, "verify", "sessions", "list", "--all", "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Count(stderr, "[dry-run] GET") != 1 || !strings.Contains(stderr, "offset=0") {
		t.Errorf("want exactly the first page previewed: %q", stderr)
	}
	if !strings.Contains(stderr, "--all would read the pages after this one") {
		t.Errorf("no note on what --all would do: %q", stderr)
	}
}

// Lists whose rows are not under "objects" must merge their own key, or --all
// would walk every page and show none of it.
func TestListAll_mergesUnderEachListsKey(t *testing.T) {
	cases := []struct {
		key  string
		args []string
	}{
		{"brands", []string{"messaging", "sms", "10dlc", "brands", "list"}},
		{"campaigns", []string{"messaging", "sms", "10dlc", "campaigns", "list"}},
		{"compliances", []string{"numbers", "compliance", "list"}},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			setFakeCreds(t)
			pagedServer(t, tc.key, 25, 25, nil)

			err, stdout, _ := execCmd(t, append(tc.args, "--all", "-o", "json")...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rows, _ := dataRows(t, stdout, tc.key); len(rows) != 25 {
				t.Fatalf("got %d rows under %s, want 25", len(rows), tc.key)
			}
			if strings.Contains(stdout, `"objects"`) {
				t.Errorf("an objects key appeared:\n%s", stdout)
			}
		})
	}
}

// Every paged list takes --all except numbers search: walking the whole
// inventory of numbers for sale is not a sensible request.
func TestListAll_onEveryPagedList(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Flags().Lookup("limit") != nil && c.CommandPath() != "plivo docs search" {
			hasAll := c.Flags().Lookup("all") != nil
			if want := c.CommandPath() != "plivo numbers search"; hasAll != want {
				t.Errorf("%s: --all registered = %v, want %v", c.CommandPath(), hasAll, want)
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}

// The page check goes in front of a pre-run hook the command already has; it
// must not replace it. cobra skips PreRun once PreRunE is set, so a PreRun is
// carried into the chain too.
func TestRegisterListFlags_keepsAnExistingPreRunHook(t *testing.T) {
	var limit, offset int
	var ran []string
	withE := &cobra.Command{Use: "a", PreRunE: func(*cobra.Command, []string) error {
		ran = append(ran, "PreRunE")
		return nil
	}}
	plain := &cobra.Command{Use: "b", PreRun: func(*cobra.Command, []string) { ran = append(ran, "PreRun") }}
	for _, c := range []*cobra.Command{withE, plain} {
		registerListFlags(c, &limit, &offset)
		if err := c.PreRunE(c, nil); err != nil {
			t.Fatalf("%s: unexpected error: %v", c.Use, err)
		}
	}
	if strings.Join(ran, ",") != "PreRunE,PreRun" {
		t.Fatalf("hooks run = %v, want both", ran)
	}

	ran, limit = nil, 0
	var ce *clierr.Error
	if !errors.As(withE.PreRunE(withE, nil), &ce) || ce.Code != clierr.CodeBadFlag || len(ran) != 0 {
		t.Fatalf("a bad page must stop before the hook: err %v, ran %v", ce, ran)
	}
}
