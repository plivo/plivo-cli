//go:build internal

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// These cover ContactoProfile + VibeSession, which are gated behind the
// `internal` build tag (they back the internal-only agent surface). The
// withHomeDir helper lives in config_test.go, compiled in both build modes.

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
		{"dev region", ContactoProfile{Region: "us-east-1", Environment: "dev"}, "https://dev-us-east-1-auth-api.contactodev.com"},
		{"prod us-east-1", ContactoProfile{Region: "us-east-1", Environment: "prod"}, "https://hodor-use1.plivo.com"},
		{"prod ap-south-1", ContactoProfile{Region: "ap-south-1", Environment: "prod"}, "https://hodor-aps1.plivo.com"},
		{"prod unknown region returns empty", ContactoProfile{Region: "eu-west-1", Environment: "prod"}, ""},
		{"empty env defaults to prod (us-east-1)", ContactoProfile{Region: "us-east-1"}, "https://hodor-use1.plivo.com"},
		{"empty env defaults to prod (ap-south-1)", ContactoProfile{Region: "ap-south-1"}, "https://hodor-aps1.plivo.com"},
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
		{"prod default", ContactoProfile{Environment: "prod"}, "https://hodor.plivo.com"},
		{"empty environment defaults to prod", ContactoProfile{}, "https://hodor.plivo.com"},
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
	if first.LastTurnAt == "" {
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
