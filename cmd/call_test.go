package cmd

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// daysAgo is the UTC date n days back, so a voice range stays inside the Call
// API's 90 days whenever the test runs.
func daysAgo(n int) string {
	return time.Now().UTC().AddDate(0, 0, -n).Format("2006-01-02")
}

func TestCheckVoiceCallWindow(t *testing.T) {
	now := time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)
	cases := []struct {
		name, since, until string
		wantErr            string // "" when the range is allowed
		wantHint           string
	}{
		{"no range", "", "", "", ""},
		{"31 days to the minute", "2026-09-01 00:00:00", "2026-10-02 00:00:00", "", ""},
		{"31 days and a minute", "2026-09-01 00:00:00", "2026-10-02 00:01:00", "at most 31 days",
			"Run one range at a time: --since 2026-09-01 --until 2026-10-01; --since 2026-10-02 --until 2026-10-02."},
		{"bare dates 31 days apart, both counted", "2026-09-01 00:00:00", "2026-10-01 23:59:59", "", ""},
		{"no end, inside 31 days", "2026-09-20 00:00:00", "", "", ""},
		{"no end runs to now", "2026-08-01 00:00:00", "", "a range with no end runs to now",
			"Run one range at a time: --since 2026-08-01 --until 2026-08-31; --since 2026-09-01 --until 2026-10-01; --since 2026-10-02 --until 2026-10-12."},
		{"89 days", "2026-07-15 00:00:00", "2026-10-12 23:59:59", "this range is longer",
			"Run one range at a time: --since 2026-07-15 --until 2026-08-14; --since 2026-08-15 --until 2026-09-14; --since 2026-09-15 --until 2026-10-12."},
		{"exactly 90 days back", "2026-07-14 15:00:00", "2026-07-20 23:59:59", "", ""},
		{"more than 90 days back", "2026-07-14 00:00:00", "2026-07-20 23:59:59", "more than 90 days ago",
			"Start it on or after 2026-07-15."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkVoiceCallWindow(tc.since, tc.until, now)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want the range allowed, got %v", err)
				}
				return
			}
			var ce *clierr.Error
			if !errors.As(err, &ce) || ce.Code != clierr.CodeBadInput || !strings.Contains(ce.Message, tc.wantErr) {
				t.Fatalf("want BAD_INPUT with %q, got %v", tc.wantErr, err)
			}
			if ce.Hint != tc.wantHint {
				t.Errorf("hint = %q\n want %q", ce.Hint, tc.wantHint)
			}
		})
	}
}

// Each range the hint offers must itself be one the check allows.
func TestVoiceCallRanges_eachRangeIsAllowed(t *testing.T) {
	now := time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)
	from := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	ranges := voiceCallRanges(from, now)
	if len(ranges) != 3 {
		t.Fatalf("ranges = %v, want 3", ranges)
	}
	for _, r := range ranges {
		f := strings.Fields(r) // --since D --until D
		since, _ := normalizeListTime(f[1], false)
		until, _ := normalizeListTime(f[3], true)
		if err := checkVoiceCallWindow(since, until, now); err != nil {
			t.Errorf("%s: %v", r, err)
		}
	}
}

func TestVoiceCallsList_rangeReachesTheQuery(t *testing.T) {
	for _, names := range [][2]string{{"--since", "--until"}, {"--from-date", "--to-date"}} {
		t.Run(names[0], func(t *testing.T) {
			setFakeCreds(t)
			urls, _ := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

			err, _, _ := execCmd(t, "voice", "calls", "list", names[0], daysAgo(10), names[1], daysAgo(3))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := urls()
			if len(got) != 1 {
				t.Fatalf("expected 1 request, got %v", got)
			}
			for _, want := range []string{
				"end_time__gte=" + daysAgo(10) + "+00%3A00%3A00",
				"end_time__lte=" + daysAgo(3) + "+23%3A59%3A59",
			} {
				if !strings.Contains(got[0], want) {
					t.Errorf("query missing %s\n  got: %s", want, got[0])
				}
			}
		})
	}
}

// Each of these is a 400 from the Call API; the CLI refuses it first.
func TestVoiceCallsList_refusesARangeTheAPIWouldBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"until without since", []string{"--until", daysAgo(3)}, "--until needs --since"},
		{"to-date without from-date", []string{"--to-date", daysAgo(3)}, "--to-date needs --from-date"},
		{"longer than 31 days", []string{"--since", daysAgo(40), "--until", daysAgo(3)}, "at most 31 days"},
		{"no end, over 31 days back", []string{"--from-date", daysAgo(40)}, "runs to now"},
		{"older than 90 days", []string{"--since", daysAgo(95), "--until", daysAgo(91)}, "more than 90 days ago"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			_, count := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

			err, _, _ := execCmd(t, append([]string{"voice", "calls", "list"}, tc.args...)...)
			var ce *clierr.Error
			if !errors.As(err, &ce) || ce.Code != clierr.CodeBadInput || !strings.Contains(ce.Message, tc.want) {
				t.Fatalf("want BAD_INPUT with %q, got %v", tc.want, err)
			}
			if n := count(); n != 0 {
				t.Fatalf("%d request(s) went out", n)
			}
		})
	}
}

// A refused range carries the ranges to run in the error's context, so an
// agent reading -o json need not parse the hint.
func TestVoiceCallsList_refusedRangeListsTheRangesInContext(t *testing.T) {
	setFakeCreds(t)
	sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

	err, _, _ := execCmd(t, "voice", "calls", "list", "--since", daysAgo(40), "--until", daysAgo(3))
	var ce *clierr.Error
	if !errors.As(err, &ce) {
		t.Fatalf("want a CLI error, got %v", err)
	}
	ranges, _ := ce.Context["ranges"].([]string)
	if len(ranges) != 2 || !strings.HasPrefix(ranges[0], "--since "+daysAgo(40)+" --until ") {
		t.Fatalf("ranges = %v, want two starting at %s", ce.Context["ranges"], daysAgo(40))
	}
}
