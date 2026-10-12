package cmd

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// daysAgo is the UTC date n days back, so a range stays inside the APIs' 90
// days whenever the test runs.
func daysAgo(n int) string {
	return time.Now().UTC().AddDate(0, 0, -n).Format(time.DateOnly)
}

// at reads a test timestamp; "" is the zero time, an end that is not set.
func at(t *testing.T, s string) time.Time {
	t.Helper()
	if s == "" {
		return time.Time{}
	}
	v, err := time.Parse(time.DateTime, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCheckVoiceCallWindow(t *testing.T) {
	now := time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)
	cases := []struct {
		name, since, until string
		wantErr            string // "" when the range is allowed
		wantRanges         []string
	}{
		{"no range", "", "", "", nil},
		{"31 days to the second", "2026-09-01 00:00:00", "2026-10-02 00:00:00", "", nil},
		{"31 days and a second", "2026-09-01 00:00:00", "2026-10-02 00:00:01", "this range is longer",
			[]string{"--since 2026-09-01 --until 2026-10-01", `--since 2026-10-02 --until "2026-10-02 00:00:01"`}},
		{"bare dates 31 days apart, both counted", "2026-09-01 00:00:00", "2026-10-01 23:59:59", "", nil},
		{"no end, inside 31 days", "2026-09-20 00:00:00", "", "", nil},
		{"no end runs to now", "2026-08-01 00:00:00", "", "a range with no end runs to now",
			[]string{"--since 2026-08-01 --until 2026-08-31", "--since 2026-09-01 --until 2026-10-01", "--since 2026-10-02 --until 2026-10-12"}},
		{"89 days", "2026-07-15 00:00:00", "2026-10-12 23:59:59", "this range is longer",
			[]string{"--since 2026-07-15 --until 2026-08-14", "--since 2026-08-15 --until 2026-09-14", "--since 2026-09-15 --until 2026-10-12"}},
		{"a start with a time keeps it in the first range", "2026-07-14 16:00:00", "2026-10-12 23:59:59", "this range is longer",
			[]string{`--since "2026-07-14 16:00:00" --until 2026-08-13`, "--since 2026-08-14 --until 2026-09-13", "--since 2026-09-14 --until 2026-10-12"}},
		{"an end far ahead stops the ranges at today", "2026-10-01 00:00:00", "9999-12-31 23:59:59", "this range is longer",
			[]string{"--since 2026-10-01 --until 2026-10-12"}},
		{"a range wholly ahead has no ranges to offer", "2099-01-01 00:00:00", "2099-12-31 23:59:59", "this range is longer", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkVoiceCallWindow(at(t, tc.since), at(t, tc.until), now)
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
			got, _ := ce.Context["ranges"].([]string)
			if strings.Join(got, "; ") != strings.Join(tc.wantRanges, "; ") {
				t.Errorf("ranges = %v\n want %v", got, tc.wantRanges)
			}
			if len(tc.wantRanges) > 0 {
				if want := "Run one range at a time: " + strings.Join(tc.wantRanges, "; ") + "."; ce.Hint != want {
					t.Errorf("hint = %q\n want %q", ce.Hint, want)
				}
			}
		})
	}
}

// Each range the hint offers must itself be allowed, the first included when
// it starts within the hour after the 90-day edge.
func TestVoiceCallRanges_eachRangeIsAllowed(t *testing.T) {
	now := time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)
	ranges := voiceCallRanges(time.Date(2026, 7, 14, 16, 0, 0, 0, time.UTC), time.Date(2026, 10, 12, 23, 59, 59, 0, time.UTC))
	if len(ranges) != 3 {
		t.Fatalf("ranges = %v, want 3", ranges)
	}
	for _, r := range ranges {
		if err := checkRetention(r[0], now, listRetainDays); err != nil {
			t.Errorf("%v: %v", r, err)
		}
		if err := checkVoiceCallWindow(r[0], r[1], now); err != nil {
			t.Errorf("%v: %v", r, err)
		}
	}
}

func TestVoiceCallsList_checksTheSpanBeforeAnyRequest(t *testing.T) {
	requireRefusedBeforeAnyRequest(t,
		[]string{"voice", "calls", "list", "--since", daysAgo(40), "--until", daysAgo(3)}, "at most 31 days")
}
