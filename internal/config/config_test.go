package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// withHomeDir redirects os.UserHomeDir() to a tmp dir so tests don't pollute
// the real ~/.plivo. Also clears HOME-related vars on the way out.
func withHomeDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp) // Windows
	return tmp
}

// clearEnv removes the cred env vars for the scope of a single test.
func clearEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PLIVO_AUTH_ID", "")
	t.Setenv("PLIVO_AUTH_TOKEN", "")
}

// TestMain swaps in go-keyring's in-memory mock so credential tests never touch
// (or prompt) the developer's real OS keychain.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

// ─── OS keychain token storage ───────────────────────────────────────────────

func TestKeychain_setGetDelete(t *testing.T) {
	if err := SetToken("kc-profile", "tok-secret"); err != nil {
		t.Fatalf("SetToken: %v", err)
	}
	got, err := GetToken("kc-profile")
	if err != nil || got != "tok-secret" {
		t.Fatalf("GetToken = %q, %v; want tok-secret", got, err)
	}
	if err := DeleteToken("kc-profile"); err != nil {
		t.Fatalf("DeleteToken: %v", err)
	}
	// A miss returns ("", nil), not an error.
	got, err = GetToken("kc-profile")
	if err != nil || got != "" {
		t.Fatalf("GetToken after delete = %q, %v; want \"\", nil", got, err)
	}
	// Deleting a missing entry is a no-op.
	if err := DeleteToken("never-existed"); err != nil {
		t.Errorf("DeleteToken(missing) = %v; want nil", err)
	}
}

// TestResolve_keychainBackedProfile verifies Resolve pulls the token from the
// keychain when config.toml has the auth_id but no auth_token.
func TestResolve_keychainBackedProfile(t *testing.T) {
	withHomeDir(t)
	clearEnv(t)
	_ = Save(&Config{Active: "kc", Profiles: map[string]Profile{"kc": {AuthID: "MAkc"}}})
	if err := SetToken("kc", "tok-from-keychain"); err != nil {
		t.Fatalf("SetToken: %v", err)
	}
	t.Cleanup(func() { _ = DeleteToken("kc") })

	prof, src, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if prof.AuthToken != "tok-from-keychain" || prof.AuthID != "MAkc" || src != "kc" {
		t.Errorf("Resolve = %+v src=%q; want token sourced from keychain", prof, src)
	}
}

// ─── Path / Load / Save ──────────────────────────────────────────────────────

func TestPath_endsAtPlivoConfigToml(t *testing.T) {
	tmp := withHomeDir(t)
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tmp, ".plivo", "config.toml")
	if p != want {
		t.Errorf("Path = %q, want %q", p, want)
	}
}

func TestLoad_noFile_returnsEmptyConfig(t *testing.T) {
	withHomeDir(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Active != "" {
		t.Errorf("Active = %q, want empty", cfg.Active)
	}
	if cfg.Profiles == nil {
		t.Error("Profiles map should be initialised, not nil")
	}
	if len(cfg.Profiles) != 0 {
		t.Errorf("Profiles len = %d, want 0", len(cfg.Profiles))
	}
}

func TestSaveLoad_roundTrip(t *testing.T) {
	withHomeDir(t)
	orig := &Config{
		Active: "work",
		Profiles: map[string]Profile{
			"work":     {AuthID: "MAxxx", AuthToken: "tokx"},
			"personal": {AuthID: "MAyyy", AuthToken: "toky", Region: "us"},
		},
	}
	if err := Save(orig); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Active != "work" {
		t.Errorf("Active = %q", loaded.Active)
	}
	if loaded.Profiles["work"].AuthID != "MAxxx" {
		t.Errorf("work.AuthID = %q", loaded.Profiles["work"].AuthID)
	}
	if loaded.Profiles["personal"].Region != "us" {
		t.Errorf("personal.Region = %q", loaded.Profiles["personal"].Region)
	}
}

func TestSave_createsParentDirWith0700(t *testing.T) {
	tmp := withHomeDir(t)
	if err := Save(&Config{Profiles: map[string]Profile{}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(tmp, ".plivo"))
	if err != nil {
		t.Fatalf(".plivo dir not created: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0700 {
		// Some systems may apply umask differently; just warn if not 0700.
		t.Logf("note: .plivo perms = %o (expected 0700; may be umasked)", mode)
	}
}

func TestSave_writeMode0600(t *testing.T) {
	tmp := withHomeDir(t)
	_ = Save(&Config{Active: "x", Profiles: map[string]Profile{"x": {AuthID: "MA", AuthToken: "t"}}})
	info, err := os.Stat(filepath.Join(tmp, ".plivo", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Logf("note: config.toml perms = %o (expected 0600; may be umasked)", mode)
	}
}

func TestLoad_corruptedToml_returnsError(t *testing.T) {
	tmp := withHomeDir(t)
	dir := filepath.Join(tmp, ".plivo")
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "config.toml"), []byte("this is not [valid"), 0600)

	_, err := Load()
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parsing") {
		t.Errorf("error should mention parsing: %v", err)
	}
}

// ─── Resolve — priority order ────────────────────────────────────────────────

func TestResolve_explicitProfileWins(t *testing.T) {
	withHomeDir(t)
	clearEnv(t)
	_ = Save(&Config{
		Active: "work",
		Profiles: map[string]Profile{
			"work":     {AuthID: "MAwork", AuthToken: "tokwork"},
			"personal": {AuthID: "MAperson", AuthToken: "tokperson"},
		},
	})
	prof, src, err := Resolve("personal")
	if err != nil {
		t.Fatal(err)
	}
	if prof.AuthID != "MAperson" {
		t.Errorf("AuthID = %q, want MAperson", prof.AuthID)
	}
	if src != "personal" {
		t.Errorf("src = %q, want personal", src)
	}
}

func TestResolve_activeProfileWhenNoFlag(t *testing.T) {
	withHomeDir(t)
	clearEnv(t)
	_ = Save(&Config{
		Active: "work",
		Profiles: map[string]Profile{
			"work": {AuthID: "MAwork", AuthToken: "tokwork"},
		},
	})
	prof, src, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if prof.AuthID != "MAwork" {
		t.Errorf("AuthID = %q", prof.AuthID)
	}
	if src != "work" {
		t.Errorf("src = %q", src)
	}
}

func TestResolve_envVarsWhenNoProfile(t *testing.T) {
	withHomeDir(t)
	t.Setenv("PLIVO_AUTH_ID", "MAfromenv")
	t.Setenv("PLIVO_AUTH_TOKEN", "tokfromenv")
	prof, src, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if prof.AuthID != "MAfromenv" {
		t.Errorf("AuthID = %q", prof.AuthID)
	}
	if src != "env" {
		t.Errorf("src = %q", src)
	}
}

func TestResolve_profileBeatsEnvVars(t *testing.T) {
	withHomeDir(t)
	t.Setenv("PLIVO_AUTH_ID", "MAenv")
	t.Setenv("PLIVO_AUTH_TOKEN", "tokenv")
	_ = Save(&Config{
		Active: "work",
		Profiles: map[string]Profile{
			"work": {AuthID: "MAprofile", AuthToken: "tokprofile"},
		},
	})
	prof, src, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if prof.AuthID != "MAprofile" {
		t.Errorf("profile should win over env: AuthID = %q", prof.AuthID)
	}
	if src != "work" {
		t.Errorf("src = %q", src)
	}
}

func TestResolve_namedProfileNotFound_returnsError(t *testing.T) {
	withHomeDir(t)
	clearEnv(t)
	_ = Save(&Config{Profiles: map[string]Profile{"work": {AuthID: "MA", AuthToken: "t"}}})

	_, _, err := Resolve("does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should name the missing profile: %v", err)
	}
}

func TestResolve_emptyProfileFallsThroughToEnv(t *testing.T) {
	// Profile entry exists but auth_id is blank → falls through (not picked).
	withHomeDir(t)
	t.Setenv("PLIVO_AUTH_ID", "MAenv")
	t.Setenv("PLIVO_AUTH_TOKEN", "tokenv")
	_ = Save(&Config{
		Active: "work",
		Profiles: map[string]Profile{
			"work": {AuthID: "", AuthToken: ""},
		},
	})
	prof, src, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if src != "env" || prof.AuthID != "MAenv" {
		t.Errorf("should fall through to env: src=%q AuthID=%q", src, prof.AuthID)
	}
}

func TestResolve_noProfileNoEnv_returnsAuthMissing(t *testing.T) {
	withHomeDir(t)
	clearEnv(t)
	_, _, err := Resolve("")
	if err == nil {
		t.Fatal("expected AuthMissing")
	}
	if !strings.Contains(err.Error(), "AUTH_MISSING") {
		t.Errorf("error should be AUTH_MISSING: %v", err)
	}
}

func TestResolve_partialEnvVars_doesNotMatch(t *testing.T) {
	// Only AUTH_ID set, no token → falls through to AuthMissing.
	withHomeDir(t)
	t.Setenv("PLIVO_AUTH_ID", "MAonly")
	t.Setenv("PLIVO_AUTH_TOKEN", "")
	_, _, err := Resolve("")
	if err == nil {
		t.Fatal("expected AuthMissing when only AUTH_ID is set")
	}
}

// ─── TelemetryEnabled ─────────────────────────────────────────────────────

func TestTelemetryEnabled_defaultOn_noConfigFile(t *testing.T) {
	withHomeDir(t)
	t.Setenv(TelemetryEnvVar, "")
	if !TelemetryEnabled() {
		t.Error("TelemetryEnabled should default true with no config.toml")
	}
}

func TestTelemetryEnabled_defaultOn_configFileWithoutTelemetryTable(t *testing.T) {
	withHomeDir(t)
	t.Setenv(TelemetryEnvVar, "")
	// A config.toml saved before this field existed — Telemetry.Enabled
	// round-trips as nil, which must still mean "on".
	_ = Save(&Config{Active: "work", Profiles: map[string]Profile{"work": {AuthID: "MA"}}})
	if !TelemetryEnabled() {
		t.Error("TelemetryEnabled should stay true when [telemetry] table is absent")
	}
}

func TestTelemetryEnabled_offPersistsAndRoundTrips(t *testing.T) {
	withHomeDir(t)
	t.Setenv(TelemetryEnvVar, "")
	off := false
	_ = Save(&Config{Profiles: map[string]Profile{}, Telemetry: TelemetryConfig{Enabled: &off}})

	if TelemetryEnabled() {
		t.Error("TelemetryEnabled should be false after saving Enabled=false")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telemetry.Enabled == nil || *cfg.Telemetry.Enabled != false {
		t.Errorf("Telemetry.Enabled round-trip = %v, want pointer to false", cfg.Telemetry.Enabled)
	}
}

func TestTelemetryEnabled_onPersistsAndRoundTrips(t *testing.T) {
	withHomeDir(t)
	t.Setenv(TelemetryEnvVar, "")
	on := true
	_ = Save(&Config{Profiles: map[string]Profile{}, Telemetry: TelemetryConfig{Enabled: &on}})

	if !TelemetryEnabled() {
		t.Error("TelemetryEnabled should be true after saving Enabled=true")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telemetry.Enabled == nil || *cfg.Telemetry.Enabled != true {
		t.Errorf("Telemetry.Enabled round-trip = %v, want pointer to true", cfg.Telemetry.Enabled)
	}
}

// TestTelemetryEnabled_envVarWinsOverConfig — PLIVO_CLI_TELEMETRY=0 is the
// umbrella off-switch: it disables telemetry even when config.toml says on.
func TestTelemetryEnabled_envVarWinsOverConfig(t *testing.T) {
	withHomeDir(t)
	on := true
	_ = Save(&Config{Profiles: map[string]Profile{}, Telemetry: TelemetryConfig{Enabled: &on}})
	t.Setenv(TelemetryEnvVar, "0")

	if TelemetryEnabled() {
		t.Error("PLIVO_CLI_TELEMETRY=0 should win over config.toml's telemetry=on")
	}
}

// TestTelemetryEnabled_envVarOnlyDisablesOnExactZero matches this repo's
// existing convention (internal/feedback: `== "0"`) — any other value,
// including "false", is not the off signal and leaves the config in charge.
func TestTelemetryEnabled_envVarOnlyDisablesOnExactZero(t *testing.T) {
	withHomeDir(t)
	t.Setenv(TelemetryEnvVar, "false")
	if !TelemetryEnabled() {
		t.Error(`PLIVO_CLI_TELEMETRY="false" should NOT disable telemetry — only exact "0" does`)
	}
}
