package feedback

import (
	"os"
	"testing"
	"time"
)

// withTempHome isolates the state file under a tmp dir so tests don't
// touch the real ~/.plivo/feedback-state.json.
func withTempHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return tmp
}

// Round-trip: SaveState → LoadState recovers the same values.
func TestLoadSaveState_roundTrip(t *testing.T) {
	withTempHome(t)
	want := &State{
		LastPromptedAt:  time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
		LastSubmittedAt: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		SnoozeCount:     3,
		PromptCount:     7,
	}
	if err := SaveState(want); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !got.LastPromptedAt.Equal(want.LastPromptedAt) {
		t.Errorf("LastPromptedAt: got %v, want %v", got.LastPromptedAt, want.LastPromptedAt)
	}
	if got.SnoozeCount != want.SnoozeCount {
		t.Errorf("SnoozeCount: got %d, want %d", got.SnoozeCount, want.SnoozeCount)
	}
	if got.PromptCount != want.PromptCount {
		t.Errorf("PromptCount: got %d, want %d", got.PromptCount, want.PromptCount)
	}
}

// Missing state file → empty State, not an error. Lets first-time users
// run the CLI without a pre-existing file.
func TestLoadState_missingFileIsEmpty(t *testing.T) {
	withTempHome(t)
	got, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !got.LastPromptedAt.IsZero() {
		t.Errorf("expected zero LastPromptedAt, got %v", got.LastPromptedAt)
	}
}

// Corrupt JSON → empty state, no error. Defensive: a broken local file
// shouldn't crash the CLI on every run.
func TestLoadState_corruptFileIsEmpty(t *testing.T) {
	tmp := withTempHome(t)
	_ = os.MkdirAll(tmp+"/.plivo", 0700)
	_ = os.WriteFile(tmp+"/.plivo/feedback-state.json", []byte("not json"), 0600)
	got, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got.PromptCount != 0 {
		t.Errorf("corrupt file should give empty State, got %+v", got)
	}
}

func TestShouldPrompt(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	// "Activity-met" is the convenience baseline for cases that aren't
	// exercising the first-prompt floor: SuccessCount past the minimum
	// means the floor is satisfied, so the other gates dominate.
	activityMet := MinSuccessfulBeforeFirstPrompt
	cases := []struct {
		name string
		s    State
		env  string // value for PLIVO_FEEDBACK_PROMPT
		want bool
	}{
		{
			name: "fresh state, no activity → no (first-prompt floor blocks)",
			s:    State{},
			want: false,
		},
		{
			name: "fresh state, activity met → yes",
			s:    State{SuccessCount: activityMet},
			want: true,
		},
		{
			name: "fresh state, low activity but installed >24h ago → yes",
			s:    State{FirstRunAt: now.Add(-25 * time.Hour), SuccessCount: 1},
			want: true,
		},
		{
			name: "fresh state, low activity and installed <24h ago → no",
			s:    State{FirstRunAt: now.Add(-1 * time.Hour), SuccessCount: 1},
			want: false,
		},
		{
			name: "prompted 1 hour ago → no",
			s:    State{LastPromptedAt: now.Add(-1 * time.Hour), SuccessCount: activityMet},
			want: false,
		},
		{
			name: "prompted 25 hours ago → yes (past PromptInterval)",
			s:    State{LastPromptedAt: now.Add(-25 * time.Hour), SuccessCount: activityMet},
			want: true,
		},
		{
			name: "submitted 12 hours ago → no (don't re-ask same day)",
			s:    State{LastSubmittedAt: now.Add(-12 * time.Hour), SuccessCount: activityMet},
			want: false,
		},
		{
			name: "submitted 30 hours ago → yes",
			s:    State{LastSubmittedAt: now.Add(-30 * time.Hour), SuccessCount: activityMet},
			want: true,
		},
		{
			name: "opted out → never, regardless of times",
			s:    State{SuccessCount: activityMet},
			env:  "0",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(PromptOptOutEnvVar, tc.env)
			if got := ShouldPrompt(&tc.s, now); got != tc.want {
				t.Errorf("ShouldPrompt = %v, want %v", got, tc.want)
			}
		})
	}
}
