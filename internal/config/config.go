// Package config manages CLI credential profiles in ~/.plivo/config.toml,
// with fallback to PLIVO_AUTH_ID / PLIVO_AUTH_TOKEN env vars.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/plivo/plivo-cli/internal/clierr"
)

type Profile struct {
	AuthID            string `toml:"auth_id"`
	AuthToken         string `toml:"auth_token,omitempty"`
	DefaultSubaccount string `toml:"default_subaccount,omitempty"`
	Region            string `toml:"region,omitempty"`
	// Email is the human's email captured at login time. Sent as
	// X-Plivo-CLI-Email on every request so server-side analytics can
	// attribute action-per-user within an org (auth_id is org-level —
	// multiple humans share it). Empty is fine; some flows (older builds,
	// manual auth_id+token entry without prompt) won't have it.
	Email string `toml:"email,omitempty"`
	// Name is the human's display name from the login response. Surfaced
	// in the "Logged in as …" line and stashed here so subsequent commands
	// can greet the user without an extra API round-trip.
	Name string `toml:"name,omitempty"`
	// AomUUID is the per-user identity row that ties this human to this
	// org. Needed for any subsequent server call scoped to "who" rather
	// than just "which org". Browser + email/password flows both surface
	// it; manual auth_id+token flow does not (no way to resolve which
	// human shares the org-level creds).
	AomUUID string `toml:"aom_uuid,omitempty"`
	// Env, if set, names a non-prod environment for this profile (e.g.
	// "dev"). Resolved against cmd.loginEnvURLs at runtime. Empty (the
	// common case) means "use the default prod environment" — we never
	// write "prod" since that's the implicit default.
	Env string `toml:"env,omitempty"`
	// OrgName is the organization's display name, captured at login. Used
	// to auto-name profiles per org and to label them in `plivo auth
	// list` / the post-login confirmation line. AuthID (not OrgName) is
	// what actually decides "same org" for conflict detection — this
	// field is display-only. Empty on older configs/servers.
	OrgName string `toml:"org_name,omitempty"`
	// OrgUUID is the organization's stable identifier, when the auth
	// server sends one. Not consulted by any logic yet (AuthID already
	// identifies "which org" — see OrgName above); stored alongside it in
	// case a future feature needs the org's own id rather than the
	// credential's.
	OrgUUID string `toml:"org_uuid,omitempty"`
}

type Config struct {
	Active    string             `toml:"active"`
	Profiles  map[string]Profile `toml:"profiles"`
	Buddy     BuddyConfig        `toml:"buddy,omitempty"`
	Telemetry TelemetryConfig    `toml:"telemetry,omitempty"`
}

// BuddyConfig is the optional [buddy] table in ~/.plivo/config.toml. Tunes
// the `plivo ask` / `plivo support` commands. Only URL is consulted today;
// the other knobs reserve schema space for v1.1 (per-frame read timeout,
// render toggles).
type BuddyConfig struct {
	URL             string `toml:"url,omitempty"`
	ReadTimeoutSecs int    `toml:"read_timeout_secs,omitempty"`
	ShowNarration   *bool  `toml:"show_narration,omitempty"`
	ShowToolCalls   *bool  `toml:"show_tool_calls,omitempty"`
}

// EffectiveURL returns the buddy URL configured in ~/.plivo/config.toml,
// or "" if unset (caller falls through to env / default).
func (b BuddyConfig) EffectiveURL() string { return b.URL }

// TelemetryConfig is the optional [telemetry] table in ~/.plivo/config.toml.
// Controls whether identity fields (email, auth ID, region, AOM UUID) ride
// on outbound requests. Nil Enabled means "on" — an existing config.toml
// without this table behaves exactly as before.
type TelemetryConfig struct {
	Enabled *bool `toml:"enabled,omitempty"`
}

// TelemetryEnvVar is the umbrella opt-out for identity telemetry, checked
// by TelemetryEnabled. See also feedback.TelemetryOptOutEnvVar, a narrower
// switch that only silences `plivo feedback` submission.
const TelemetryEnvVar = "PLIVO_CLI_TELEMETRY"

// TelemetryEnabled reports whether identity headers should go out.
// PLIVO_CLI_TELEMETRY=0 always wins; otherwise it's the config.toml
// [telemetry] setting, default on.
func TelemetryEnabled() bool {
	if os.Getenv(TelemetryEnvVar) == "0" {
		return false
	}
	cfg, err := Load()
	if err != nil {
		return true
	}
	return cfg.Telemetry.Enabled == nil || *cfg.Telemetry.Enabled
}

var ErrNoCredentials = errors.New("no credentials: set PLIVO_AUTH_ID/PLIVO_AUTH_TOKEN or run `plivo login`")

// Path returns the config file path: ~/.plivo/config.toml.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".plivo", "config.toml"), nil
}

func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Profiles: map[string]Profile{}}, nil
		}
		return nil, err
	}
	var c Config
	if _, err := toml.Decode(string(data), &c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p, err)
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	return &c, nil
}

func Save(c *Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

// Resolve returns the credentials to use.
// Order: explicit --profile → PLIVO_AUTH_ID/TOKEN env vars → active profile.
// The second return value is the source label ("profile-name" or "env").
//
// Env vars beat the *active* profile so that exporting credentials works even
// when a profile is already stored — matching the aws, stripe and twilio CLIs.
// An explicit --profile still beats env: naming a profile is explicit intent.
func Resolve(profileName string) (Profile, string, error) {
	cfg, err := Load()
	if err != nil {
		return Profile{}, "", err
	}

	if profileName != "" {
		p, ok, err := profileWithToken(cfg, profileName)
		if err != nil {
			return Profile{}, "", err
		}
		if ok {
			return p, profileName, nil
		}
		return Profile{}, "", fmt.Errorf("profile %q not found or has no stored token in %s", profileName, mustPath())
	}

	if authID, authToken := os.Getenv("PLIVO_AUTH_ID"), os.Getenv("PLIVO_AUTH_TOKEN"); authID != "" && authToken != "" {
		return Profile{AuthID: authID, AuthToken: authToken}, "env", nil
	}

	if cfg.Active != "" {
		p, ok, err := profileWithToken(cfg, cfg.Active)
		if err != nil {
			return Profile{}, "", err
		}
		if ok {
			return p, cfg.Active, nil
		}
	}
	return Profile{}, "", clierr.AuthMissing()
}

// profileWithToken loads a named profile and fills in its token. Reports
// ok=false when the profile is absent or has no usable token.
func profileWithToken(cfg *Config, name string) (Profile, bool, error) {
	p, exists := cfg.Profiles[name]
	if !exists || p.AuthID == "" {
		return Profile{}, false, nil
	}
	// A token in config.toml (legacy, or the fallback when no OS keychain is
	// available) wins; otherwise pull it from the keychain where login stores it.
	if p.AuthToken == "" {
		tok, err := GetToken(name)
		if err != nil {
			// A real keychain failure (locked / access denied) — not a plain
			// miss, which GetToken maps to ("", nil). Surface it instead of
			// falling through to a confusing AUTH_MISSING.
			return Profile{}, false, fmt.Errorf("reading auth token for profile %q from the OS keychain: %w", name, err)
		}
		p.AuthToken = tok
	}
	if p.AuthToken == "" {
		return Profile{}, false, nil
	}
	return p, true, nil
}

func mustPath() string {
	p, err := Path()
	if err != nil {
		return "~/.plivo/config.toml"
	}
	return p
}
