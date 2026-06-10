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
}

type Config struct {
	Active   string             `toml:"active"`
	Profiles map[string]Profile `toml:"profiles"`
	Buddy    BuddyConfig        `toml:"buddy,omitempty"`
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
// Order: explicit profileName → active profile in config → PLIVO_AUTH_ID/TOKEN env vars.
// The second return value is the source label ("profile-name" or "env").
func Resolve(profileName string) (Profile, string, error) {
	cfg, err := Load()
	if err != nil {
		return Profile{}, "", err
	}
	name := profileName
	if name == "" {
		name = cfg.Active
	}
	if name != "" {
		if p, ok := cfg.Profiles[name]; ok && p.AuthID != "" {
			// Token precedence: a token in config.toml (legacy, or the
			// fallback used when no OS keychain is available) wins; otherwise
			// pull it from the OS keychain where `auth login` now stores it.
			if p.AuthToken == "" {
				tok, err := GetToken(name)
				if err != nil {
					// A real keychain failure (locked / access denied) — not a
					// plain miss, which GetToken maps to ("", nil). Surface it
					// instead of falling through to a confusing AUTH_MISSING.
					return Profile{}, "", fmt.Errorf("reading auth token for profile %q from the OS keychain: %w", name, err)
				}
				p.AuthToken = tok
			}
			if p.AuthToken != "" {
				return p, name, nil
			}
		}
		if profileName != "" {
			return Profile{}, "", fmt.Errorf("profile %q not found or has no stored token in %s", profileName, mustPath())
		}
	}
	authID := os.Getenv("PLIVO_AUTH_ID")
	authToken := os.Getenv("PLIVO_AUTH_TOKEN")
	if authID != "" && authToken != "" {
		return Profile{AuthID: authID, AuthToken: authToken}, "env", nil
	}
	return Profile{}, "", clierr.AuthMissing()
}

func mustPath() string {
	p, err := Path()
	if err != nil {
		return "~/.plivo/config.toml"
	}
	return p
}
