package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/plivo/plivo-cli/internal/version"
)

// EndpointEnvVar is the env var the CLI consults to find a custom
// collector URL. Empty / unset → Submit() falls back to the default
// hodor /v1/accounts/cli/feedback route. Lets self-hosters point at a
// private collector without code changes.
const EndpointEnvVar = "PLIVO_FEEDBACK_ENDPOINT"

// TelemetryOptOutEnvVar lets users disable feedback submission entirely
// (Submit becomes a silent no-op). Narrower than PLIVO_CLI_TELEMETRY
// (internal/config), which only gates identity headers and leaves
// submission itself alone.
const TelemetryOptOutEnvVar = "PLIVO_FEEDBACK_TELEMETRY"

// MachineIDEnvVar overrides the per-machine UUID. Only used by tests
// to make assertions deterministic.
const MachineIDEnvVar = "PLIVO_FEEDBACK_MACHINE_ID"

// Trigger is the high-level reason this event was generated. Distinct
// values mean we can answer "rating distribution by trigger" — sentiment
// after a milestone is rarely the same shape as sentiment after a
// command failure.
type Trigger string

const (
	TriggerExplicit           Trigger = "explicit_command"
	TriggerDailyPrompt        Trigger = "daily_prompt" // post-success once-per-PromptInterval auto-ask
	TriggerFirstImpression    Trigger = "first_impression"
	TriggerAnniversary7d      Trigger = "anniversary_7d"
	TriggerAnniversary30d     Trigger = "anniversary_30d"
	TriggerAnniversary90d     Trigger = "anniversary_90d"
	TriggerAnniversary365d    Trigger = "anniversary_365d"
	TriggerVersionUpgrade     Trigger = "version_upgrade"
	TriggerMilestone50Cmds    Trigger = "milestone_50_commands"
	TriggerMilestoneFirstCall Trigger = "milestone_first_call"
	TriggerMilestoneFirstAsk  Trigger = "milestone_first_ask"
)

// Context captures the CLI-side state at the moment of submission. Only
// dimension-safe fields here; no flag values, no arg values, no free
// text other than the user's own comment.
type Context struct {
	CommandPath   string   `json:"command_path,omitempty"`    // dotted path; empty for the explicit cmd
	LastOutcome   string   `json:"last_outcome,omitempty"`    // success | api_error | client_error | cancelled | timeout
	Last3Commands []string `json:"last_3_commands,omitempty"` // paths only, most-recent-first
	CLIVersion    string   `json:"cli_version"`
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	GoVersion     string   `json:"go_version"`
	InstallMethod string   `json:"install_method,omitempty"` // install.sh | install.ps1 | manual | brew | unknown
	IsCI          bool     `json:"is_ci"`
	IsTTY         bool     `json:"is_tty"`
}

// Event is the JSON shape we ship to the collector. The raw auth_id (when
// the user is logged in) travels as the X-Plivo-CLI-Auth-ID header — not
// inside the body — so the server can use it as the PostHog distinct_id
// directly, matching the cli.request scheme so Persons stitch correctly.
type Event struct {
	Event         string    `json:"event"` // always "cli.feedback.submitted"
	Timestamp     time.Time `json:"timestamp"`
	SessionID     string    `json:"session_id"`
	AnonMachineID string    `json:"anon_machine_id"`

	Rating         int    `json:"rating,omitempty"`         // 1-5, or 0 if comment-only
	Comment        string `json:"comment,omitempty"`        // post-sanitisation
	CommentLength  int    `json:"comment_length,omitempty"` // pre-truncation
	RedactionCount int    `json:"redaction_count"`          // how many PII patterns fired

	Trigger Trigger `json:"trigger"`
	Context Context `json:"context"`
}

// NewEvent builds a fresh event with all the machine-side fields
// populated. The caller fills in rating/comment/trigger/context-extras.
// authID may be empty for not-logged-in users. Note: we no longer carry
// any auth_id derivative in the body — identity goes via the
// X-Plivo-CLI-Auth-ID header. authID stays as a parameter so callers
// don't refactor; the value is unused here.
func NewEvent(authID string) *Event {
	_ = authID // retained for caller signature compat; identity travels via header now
	return &Event{
		Event:         "cli.feedback.submitted",
		Timestamp:     time.Now().UTC(),
		SessionID:     uuid.NewString(),
		AnonMachineID: machineID(),
		Context: Context{
			CLIVersion: version.Value,
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			GoVersion:  runtime.Version(),
		},
	}
}

// SetComment runs the user-typed string through Sanitize, captures the
// pre-truncation length, and records the redaction count.
func (e *Event) SetComment(raw string) {
	e.CommentLength = len(raw)
	cleaned, count := Sanitize(raw)
	e.Comment = cleaned
	e.RedactionCount = count
}

// Submit POSTs the event to the configured collector. Returns an error
// if the endpoint env var is unset (signals "backend not wired yet" to
// the caller, which can decide how loudly to surface that) or if the
// HTTP call fails.
//
// Times out at 5s — explicit-channel users actively typed this; we
// can afford a longer budget than the contextual channel's 1s
// fire-and-forget would allow.
func (e *Event) Submit(ctx context.Context, baseURL string, extraHeaders map[string]string) error {
	if os.Getenv(TelemetryOptOutEnvVar) == "0" {
		return ErrTelemetryDisabled
	}
	endpoint := os.Getenv(EndpointEnvVar)
	if endpoint == "" {
		// Default route: hodor's public /v1/accounts/cli/feedback endpoint.
		// baseURL is whatever the CLI resolves (env-aware via Profile.Env).
		// Empty baseURL means caller didn't resolve one → unsafe to guess.
		if baseURL == "" {
			return ErrEndpointNotConfigured
		}
		endpoint = strings.TrimRight(baseURL, "/") + "/v1/accounts/cli/feedback"
	}
	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Plivo-CLI/"+version.Value+" (feedback)")
	// Pass through X-Plivo-CLI-* (email, os, arch, version, …) so hodor's
	// handler can join feedback events to the per-user analytics
	// dashboards without the CLI re-deriving values.
	for k, v := range extraHeaders {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("post feedback: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("collector returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// ErrEndpointNotConfigured signals the caller didn't supply a baseURL
// AND no custom PLIVO_FEEDBACK_ENDPOINT is set. Should never happen in
// the wired CLI — getClient resolves a baseURL even for empty profiles.
var ErrEndpointNotConfigured = fmt.Errorf("no feedback endpoint resolved (set PLIVO_FEEDBACK_ENDPOINT to override)")

// ErrTelemetryDisabled signals PLIVO_FEEDBACK_TELEMETRY=0 — Submit is a
// silent no-op by user choice. Caller swallows or surfaces as it sees fit.
var ErrTelemetryDisabled = fmt.Errorf("feedback telemetry disabled via PLIVO_FEEDBACK_TELEMETRY=0")

// machineID returns a per-machine UUID, persisted in ~/.plivo/machine-id
// (so it survives reinstall in-place, gets regenerated on full wipe).
// Falls through to a process-lifetime UUID if the file isn't writable.
// PLIVO_FEEDBACK_MACHINE_ID overrides the lookup entirely (for tests).
func machineID() string {
	if forced := os.Getenv(MachineIDEnvVar); forced != "" {
		return forced
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return uuid.NewString()
	}
	path := home + "/.plivo/machine-id"
	if existing, err := os.ReadFile(path); err == nil {
		if id := string(bytes.TrimSpace(existing)); id != "" {
			return id
		}
	}
	// File missing or empty — mint a new one.
	id := uuid.NewString()
	_ = os.MkdirAll(home+"/.plivo", 0o700)
	_ = os.WriteFile(path, []byte(id), 0o600)
	return id
}
