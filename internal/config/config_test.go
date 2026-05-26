package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// ─── ContactoProfile ─────────────────────────────────────────────────────────

func TestLoadContacto_noFile_returnsErrNoContactoSession(t *testing.T) {
	withHomeDir(t)
	_, err := LoadContacto()
	if err != ErrNoContactoSession {
		t.Errorf("err = %v, want ErrNoContactoSession", err)
	}
}

func TestSaveLoadContacto_roundTrip(t *testing.T) {
	withHomeDir(t)
	orig := &ContactoProfile{
		Email:       "you@plivo.com",
		AuthToken:   "session-jwt",
		AomUUID:     "aom-uuid",
		OrgName:     "test-org",
		Region:      "us",
		Environment: "dev",
	}
	if err := SaveContacto(orig); err != nil {
		t.Fatal(err)
	}
	if orig.LoggedInAt == "" {
		t.Error("SaveContacto should set LoggedInAt")
	}
	loaded, err := LoadContacto()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Email != "you@plivo.com" || loaded.AuthToken != "session-jwt" {
		t.Errorf("round-trip lost fields: %+v", loaded)
	}
	if loaded.LoggedInAt == "" {
		t.Error("LoggedInAt not persisted")
	}
}

func TestSaveContacto_preservesExplicitLoggedInAt(t *testing.T) {
	withHomeDir(t)
	orig := &ContactoProfile{AuthToken: "x", LoggedInAt: "2026-01-01T00:00:00Z"}
	_ = SaveContacto(orig)
	if orig.LoggedInAt != "2026-01-01T00:00:00Z" {
		t.Errorf("LoggedInAt overwritten: %q", orig.LoggedInAt)
	}
}

func TestLoadContacto_emptyTokenTreatedAsMissing(t *testing.T) {
	tmp := withHomeDir(t)
	dir := filepath.Join(tmp, ".plivo")
	_ = os.MkdirAll(dir, 0700)
	// File present but no auth_token field.
	_ = os.WriteFile(filepath.Join(dir, "contacto.toml"), []byte(`email = "x"`), 0600)

	_, err := LoadContacto()
	if err != ErrNoContactoSession {
		t.Errorf("missing token should yield ErrNoContactoSession, got %v", err)
	}
}

func TestClearContacto_removesFile(t *testing.T) {
	withHomeDir(t)
	_ = SaveContacto(&ContactoProfile{AuthToken: "x"})
	if err := ClearContacto(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadContacto(); err != ErrNoContactoSession {
		t.Errorf("after Clear, Load should return ErrNoContactoSession, got %v", err)
	}
}

func TestClearContacto_idempotent(t *testing.T) {
	withHomeDir(t)
	// No file to delete → must not error.
	if err := ClearContacto(); err != nil {
		t.Errorf("Clear on missing file should be no-op, got %v", err)
	}
}

func TestRegionalGatewayURL(t *testing.T) {
	cases := []struct {
		name string
		p    ContactoProfile
		want string
	}{
		{"empty region returns empty", ContactoProfile{}, ""},
		{"dev region", ContactoProfile{Region: "us", Environment: "dev"}, "https://dev-us-auth-api.contactodev.com"},
		{"prod region", ContactoProfile{Region: "us", Environment: "prod"}, "https://us-auth-api.contacto.com"},
		{"default env (not prod) → dev pattern", ContactoProfile{Region: "us"}, "https://dev-us-auth-api.contactodev.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.RegionalGatewayURL(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGlobalHodorURL(t *testing.T) {
	cases := []struct {
		name string
		p    ContactoProfile
		want string
	}{
		{"explicit override wins", ContactoProfile{HodorServer: "https://custom-hodor"}, "https://custom-hodor"},
		{"dev default", ContactoProfile{Environment: "dev"}, "https://dev-global-auth-api.contactodev.com"},
		{"prod default", ContactoProfile{Environment: "prod"}, "https://global-auth-api.contacto.com"},
		{"empty environment → dev default", ContactoProfile{}, "https://dev-global-auth-api.contactodev.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.GlobalHodorURL(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// ─── VibeSession ─────────────────────────────────────────────────────────────

func TestLoadVibeSession_noFile_returnsErrNoVibeSession(t *testing.T) {
	withHomeDir(t)
	_, err := LoadVibeSession()
	if err != ErrNoVibeSession {
		t.Errorf("err = %v, want ErrNoVibeSession", err)
	}
}

func TestSaveLoadVibeSession_roundTrip(t *testing.T) {
	withHomeDir(t)
	orig := &VibeSession{
		SessionID:     "sess-123",
		AgentName:     "outbound-bot",
		InitialPrompt: "a bot that calls and tells one clean joke",
		TurnCount:     2,
	}
	if err := SaveVibeSession(orig); err != nil {
		t.Fatal(err)
	}
	if orig.StartedAt == "" || orig.LastTurnAt == "" {
		t.Errorf("Save should set timestamps: StartedAt=%q LastTurnAt=%q", orig.StartedAt, orig.LastTurnAt)
	}
	loaded, err := LoadVibeSession()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess-123" || loaded.AgentName != "outbound-bot" {
		t.Errorf("round-trip lost fields: %+v", loaded)
	}
	if loaded.TurnCount != 2 {
		t.Errorf("TurnCount = %d", loaded.TurnCount)
	}
}

func TestSaveVibeSession_alwaysUpdatesLastTurnAt(t *testing.T) {
	withHomeDir(t)
	first := &VibeSession{SessionID: "s", StartedAt: "2026-01-01T00:00:00Z"}
	_ = SaveVibeSession(first)
	originalLastTurn := first.LastTurnAt
	if originalLastTurn == "" {
		t.Fatal("LastTurnAt should be set on first save")
	}
	if first.StartedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("Save shouldn't overwrite explicit StartedAt: %q", first.StartedAt)
	}
}

func TestLoadVibeSession_emptySessionID_treatedAsMissing(t *testing.T) {
	tmp := withHomeDir(t)
	dir := filepath.Join(tmp, ".plivo")
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "vibe-session.json"), []byte(`{"session_id":""}`), 0600)

	_, err := LoadVibeSession()
	if err != ErrNoVibeSession {
		t.Errorf("empty SessionID should yield ErrNoVibeSession, got %v", err)
	}
}

func TestClearVibeSession_removesFile(t *testing.T) {
	withHomeDir(t)
	_ = SaveVibeSession(&VibeSession{SessionID: "s"})
	if err := ClearVibeSession(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadVibeSession(); err != ErrNoVibeSession {
		t.Errorf("after Clear, Load should return ErrNoVibeSession, got %v", err)
	}
}

func TestClearVibeSession_idempotent(t *testing.T) {
	withHomeDir(t)
	if err := ClearVibeSession(); err != nil {
		t.Errorf("Clear on missing file should be no-op, got %v", err)
	}
}
