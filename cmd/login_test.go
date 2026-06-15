package cmd

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/config"
)

// loadConfigForTest re-reads the config.toml that the command just wrote so
// we can assert on it without depending on the runLogin closure's state.
// Browser-flow tests use this helper too — see login_browser_test.go.
func loadConfigForTest(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

// stdinTokenFn pipes a string into os.Stdin for the duration of fn — used
// by api_test (POST body from `--body @-`) and any future test that
// exercises a stdin-reading flag.
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

// ─── logout coverage ────────────────────────────────────────────────────────
// The login flow itself is covered exhaustively by login_browser_test.go —
// browser PKCE is the only `plivo login` path on main. The logout tests
// below stay because they're cheap, fast, and independently useful.

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
