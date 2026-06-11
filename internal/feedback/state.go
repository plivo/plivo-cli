package feedback

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// PromptInterval is the minimum wall-clock gap between auto-prompts. A
// user who declines (or who just submitted) won't see another prompt
// until this window expires. Default 24h — picked so a daily-active
// user gets at most one ask per day, and a once-a-week user gets one
// per session.
const PromptInterval = 24 * time.Hour

// PromptOptOutEnvVar disables the auto-prompt entirely. The manual
// `plivo feedback` command keeps working regardless — opting out of the
// prompt is not the same as opting out of telemetry (that's
// TelemetryOptOutEnvVar). Two separate knobs because they answer
// different questions ("don't interrupt me" vs "don't ship anything").
const PromptOptOutEnvVar = "PLIVO_FEEDBACK_PROMPT"

// State persists the rolling timestamps the auto-prompt scheduler reads
// to decide whether to ask. Lives at ~/.plivo/feedback-state.json. JSON
// (not TOML) because nothing else cares about the format and the schema
// is internal.
type State struct {
	LastPromptedAt  time.Time `json:"last_prompted_at,omitempty"`  // bumped every time the user is asked, accept or decline
	LastSubmittedAt time.Time `json:"last_submitted_at,omitempty"` // bumped only when a feedback event ships successfully
	SnoozeCount     int       `json:"snooze_count,omitempty"`      // running count of "no" answers — for future "stop asking after N declines" logic
	PromptCount     int       `json:"prompt_count,omitempty"`      // total prompts shown (useful for funnels)
}

// StateFile returns ~/.plivo/feedback-state.json.
func StateFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".plivo", "feedback-state.json"), nil
}

// LoadState reads the state file. Missing file → empty State (caller
// treats it as "never prompted"). Corrupt file → empty State + nil err
// (don't refuse to run because of a malformed local file).
func LoadState() (*State, error) {
	p, err := StateFile()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return &State{}, nil
	}
	return &s, nil
}

// SaveState writes the state to disk. Best-effort: file write errors
// surface to the caller, but the caller typically swallows (a failed
// state save degrades to "ask again next time", not a hard error).
func SaveState(s *State) error {
	p, err := StateFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}

// ShouldPrompt decides whether to fire the auto-prompt right now. False
// when:
//   - user opted out via PLIVO_FEEDBACK_PROMPT=0
//   - last prompted within PromptInterval (don't badger)
//   - last submitted within PromptInterval (already heard from them)
//
// Pure function — env + state + clock in, bool out. Easy to test.
func ShouldPrompt(s *State, now time.Time) bool {
	if os.Getenv(PromptOptOutEnvVar) == "0" {
		return false
	}
	if !s.LastPromptedAt.IsZero() && now.Sub(s.LastPromptedAt) < PromptInterval {
		return false
	}
	if !s.LastSubmittedAt.IsZero() && now.Sub(s.LastSubmittedAt) < PromptInterval {
		return false
	}
	return true
}
