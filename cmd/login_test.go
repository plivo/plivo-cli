package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/config"
)

// stdinTokenFn pipes the given string into os.Stdin for the duration of fn,
// so the `--auth-token-stdin` code path has something to read.
func stdinTokenFn(t *testing.T, in string, fn func()) {
	t.Helper()
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = origStdin
	}()
	go func() {
		_, _ = io.WriteString(w, in)
		_ = w.Close()
	}()
	fn()
}

// loadConfigForTest re-reads the config.toml that the command just wrote so
// we can assert on it without depending on the runLogin closure's state.
func loadConfigForTest(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func TestLogin_authIDStdinToken_noVerify_savesProfile(t *testing.T) {
	setFakeCreds(t)
	stdinTokenFn(t, "my-secret-token\n", func() {
		err, _, errOut := execCmd(t,
			"login",
			"--auth-id", "MAFROMTEST",
			"--auth-token-stdin",
			"--no-verify",
			"--name", "fromtest",
		)
		if err != nil {
			t.Fatalf("login: %v\nstderr=%s", err, errOut)
		}
		if !strings.Contains(errOut, "Saved profile") || !strings.Contains(errOut, "MAFROMTEST") {
			t.Errorf("stderr missing 'Saved profile' / auth_id; got: %s", errOut)
		}
	})

	cfg := loadConfigForTest(t)
	prof, ok := cfg.Profiles["fromtest"]
	if !ok {
		t.Fatal("profile 'fromtest' not saved")
	}
	if prof.AuthID != "MAFROMTEST" {
		t.Errorf("AuthID = %q, want MAFROMTEST", prof.AuthID)
	}
	if cfg.Active != "fromtest" {
		t.Errorf("Active = %q, want fromtest (first-profile-wins)", cfg.Active)
	}
	// Token is in the keychain (or inline as fallback). Confirm we can read it back.
	tok, err := config.GetToken("fromtest")
	if err == nil && tok == "my-secret-token" {
		// happy path: keychain stored it
		return
	}
	// Fallback: inline in config.toml.
	if prof.AuthToken != "my-secret-token" {
		t.Errorf("token not retrievable: keychain err=%v, inline=%q", err, prof.AuthToken)
	}
}

func TestLogin_emptyStdinToken_errors(t *testing.T) {
	setFakeCreds(t)
	stdinTokenFn(t, "\n", func() {
		err, _, _ := execCmd(t,
			"login",
			"--auth-id", "MAEMPTY",
			"--auth-token-stdin",
			"--no-verify",
			"--name", "shouldnotexist",
		)
		if err == nil || !strings.Contains(err.Error(), "empty") {
			t.Errorf("expected empty-token error, got: %v", err)
		}
	})
	// No profile should have been saved.
	cfg := loadConfigForTest(t)
	if _, ok := cfg.Profiles["shouldnotexist"]; ok {
		t.Error("profile saved despite empty token — should have been rejected")
	}
}

func TestLogin_emptyAuthID_errors(t *testing.T) {
	setFakeCreds(t)
	// No --auth-id flag, prompt-reads from stdin → blank line is empty.
	stdinTokenFn(t, "\n", func() {
		err, _, _ := execCmd(t,
			"login",
			"--auth-token-stdin",
			"--no-verify",
		)
		if err == nil || !strings.Contains(err.Error(), "auth_id") {
			t.Errorf("expected auth_id required error, got: %v", err)
		}
	})
}

func TestLogout_namedProfile_removesItAndKeychainEntry(t *testing.T) {
	setFakeCreds(t)
	// Seed a profile we can log out.
	stdinTokenFn(t, "x\n", func() {
		err, _, _ := execCmd(t,
			"login", "--auth-id", "MAGOODBYE", "--auth-token-stdin", "--no-verify", "--name", "todelete",
		)
		if err != nil {
			t.Fatalf("seed login: %v", err)
		}
	})

	err, _, errOut := execCmd(t, "logout", "todelete")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	if !strings.Contains(errOut, "Logged out") || !strings.Contains(errOut, "todelete") {
		t.Errorf("stderr missing confirmation; got: %s", errOut)
	}
	cfg := loadConfigForTest(t)
	if _, ok := cfg.Profiles["todelete"]; ok {
		t.Error("profile still present after logout")
	}
	// Keychain entry should be gone (or never have been there); GetToken returns ("", nil) on miss.
	if tok, err := config.GetToken("todelete"); err == nil && tok != "" {
		t.Errorf("token still in keychain after logout: %q", tok)
	}
}

func TestLogout_noArg_noActive_errors(t *testing.T) {
	setFakeCreds(t)
	// Brand-new HOME → no profiles → no active.
	err, _, _ := execCmd(t, "logout")
	if err == nil || !strings.Contains(err.Error(), "no active profile") {
		t.Errorf("expected 'no active profile' error, got: %v", err)
	}
}

func TestLogout_nonExistentProfile_errors(t *testing.T) {
	setFakeCreds(t)
	err, _, _ := execCmd(t, "logout", "ghost")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestLogin_namedProfileSecondary_preservesActive(t *testing.T) {
	setFakeCreds(t)
	// First login → becomes active.
	stdinTokenFn(t, "tok1\n", func() {
		_, _, _ = execCmd(t, "login", "--auth-id", "MAFIRST", "--auth-token-stdin", "--no-verify", "--name", "first")
	})
	// Second login under a different name → does NOT change Active.
	stdinTokenFn(t, "tok2\n", func() {
		_, _, _ = execCmd(t, "login", "--auth-id", "MASECOND", "--auth-token-stdin", "--no-verify", "--name", "second")
	})
	cfg := loadConfigForTest(t)
	if cfg.Active != "first" {
		t.Errorf("Active = %q after second login, want 'first' (logging in to a new profile shouldn't steal active)", cfg.Active)
	}
	if _, ok := cfg.Profiles["second"]; !ok {
		t.Error("second profile not saved")
	}
}

func TestLogin_configFilePermissions(t *testing.T) {
	setFakeCreds(t)
	stdinTokenFn(t, "tok\n", func() {
		_, _, _ = execCmd(t, "login", "--auth-id", "MAPERMS", "--auth-token-stdin", "--no-verify", "--name", "perms")
	})
	cfgPath, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Clean(cfgPath))
	if err != nil {
		t.Fatalf("stat config.toml: %v", err)
	}
	mode := info.Mode().Perm()
	// Must be readable only by owner — token may be stored inline as fallback.
	if mode&0o077 != 0 {
		t.Errorf("config.toml mode = %#o, want owner-only (no group/world bits)", mode)
	}
}
