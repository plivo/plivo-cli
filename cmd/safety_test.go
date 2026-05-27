package cmd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// resetAllFlags walks the command tree and resets every flag to its DefValue
// + clears the Changed bit. Required because cmd globals (yesFlag, dryRunFlag,
// msgSendSrc, …) persist across rootCmd.Execute() calls and pollute later
// tests — including the help snapshots, which would otherwise drift on the
// second run under `go test -count=N`.
func resetAllFlags(c *cobra.Command) {
	visit := func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	}
	c.Flags().VisitAll(visit)
	c.PersistentFlags().VisitAll(visit)
	for _, child := range c.Commands() {
		resetAllFlags(child)
	}
}

// execCmd resets global flag state, sets argv, and invokes rootCmd.Execute.
// Returns the error from cobra (NOT the wrapped CLI error envelope) plus the
// captured stdout / stderr buffers.
//
// NOTE: rootCmd is a package-level global, so tests must be careful about
// state pollution between runs. We reset every persistent flag explicitly
// before each invocation.
func execCmd(t *testing.T, args ...string) (err error, stdout, stderr string) {
	t.Helper()
	// Reset global flag state — they're package-level booleans that persist
	// across rootCmd.Execute calls.
	yesFlag = false
	dryRunFlag = false
	explainFlag = false
	quietFlag = false
	noColorFlag = false
	allFlag = false
	profileFlag = ""
	outputFormat = ""
	logLevel = "warn"
	timeoutSec = 30
	hodorServer = ""

	// Capture stdout via os.Stdout redirection (cobra writes to os.Stderr/Stdout
	// directly in several places, so we need real OS-level redirection rather
	// than just cmd.SetOut/SetErr).
	origStdout, origStderr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	// Cobra also routes its own writes via SetOut/SetErr — point those at the
	// same pipes so help/error output is captured too.
	var outBuf, errBuf bytes.Buffer
	rootCmd.SetOut(wOut)
	rootCmd.SetErr(wErr)
	rootCmd.SetArgs(args)

	// Capture pipe data in background.
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&outBuf, rOut)
		_, _ = io.Copy(&errBuf, rErr)
		close(done)
	}()

	err = rootCmd.Execute()

	_ = wOut.Close()
	_ = wErr.Close()
	<-done
	os.Stdout = origStdout
	os.Stderr = origStderr
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	rootCmd.SetArgs(nil)

	// Reset every flag on every command to its default — without this, a
	// later test (especially help_snapshot_test) sees stale flag values and
	// fails under `-count >= 2`.
	resetAllFlags(rootCmd)

	return err, outBuf.String(), errBuf.String()
}

// setFakeCreds populates env vars + redirects HOME so the dev's
// ~/.plivo/config.toml doesn't override the test env.
func setFakeCreds(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	// Non-MA-prefixed so the gitleaks rule doesn't flag it.
	t.Setenv("PLIVO_AUTH_ID", "CIFAKEPLACEHOLDER001")
	t.Setenv("PLIVO_AUTH_TOKEN", "ci-only-not-a-real-token")
}

// startCapturingHTTPServer returns an httptest server that records URLs hit
// and replies with the given JSON. Use for spend-verb tests where we want to
// confirm that --yes actually causes an HTTP call.
//
// Access to the hits slice is mutex-guarded so the test can safely race against
// the HTTP handler (the standard http.Server runs handlers on a worker pool).
func startCapturingHTTPServer(t *testing.T, respStatus int, respBody string) (srv *httptest.Server, get func() []string) {
	t.Helper()
	var mu sync.Mutex
	urls := make([]string, 0, 4)
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		urls = append(urls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.WriteHeader(respStatus)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	get = func() []string {
		mu.Lock()
		defer mu.Unlock()
		// Return a copy so callers can't see racing appends.
		out := make([]string, len(urls))
		copy(out, urls)
		return out
	}
	return srv, get
}

// ─── Destructive verbs refuse without --yes ──────────────────────────────────

// Every command listed here must return a *clierr.Error with code
// DESTRUCTIVE_REFUSED when invoked without --yes. This is the safety
// contract the CLI exposes to humans + AI agents alike.
func TestDestructiveVerbs_refuseWithoutYes(t *testing.T) {
	setFakeCreds(t)

	cases := []struct {
		name string
		args []string
	}{
		{"number release", []string{"number", "release", "+14155551234"}},
		{"call hangup", []string{"call", "hangup", "CALL-UUID"}},
		{"recording delete", []string{"recording", "delete", "REC-UUID"}},
		// `agent delete` is internal-only; verified in internal_registration_test.go.
		{"subaccount delete", []string{"subaccount", "delete", "SAxxx"}},
		{"endpoint delete", []string{"endpoint", "delete", "EP-ID"}},
		{"application delete", []string{"application", "delete", "APP-ID"}},
		{"compliance delete", []string{"compliance", "delete", "DOC-ID"}},
		{"masking session delete", []string{"masking", "session", "delete", "SESS-UUID"}},
		{"conference hangup", []string{"conference", "hangup", "room-1"}},
		{"conference member kick", []string{"conference", "member", "kick", "room-1", "member-id"}},
		{"mpc end", []string{"mpc", "end", "MPC-UUID"}},
		{"mpc participant kick", []string{"mpc", "participant", "kick", "MPC-UUID", "PART-ID"}},
		{"stream stop", []string{"stream", "stop", "CALL-UUID"}},
		{"powerpack delete", []string{"powerpack", "delete", "PP-UUID"}},
		{"powerpack number remove", []string{"powerpack", "number", "remove", "PP-UUID", "+14155551234"}},
		{"link delete", []string{"link", "delete", "LINK-ID"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err, _, _ := execCmd(t, tc.args...)
			if err == nil {
				t.Fatalf("plivo %s — expected error, got nil", strings.Join(tc.args, " "))
			}
			// Either *clierr.Error with DESTRUCTIVE_REFUSED, or string contains it.
			if !strings.Contains(err.Error(), "DESTRUCTIVE_REFUSED") {
				t.Errorf("plivo %s — expected DESTRUCTIVE_REFUSED in error, got: %v", strings.Join(tc.args, " "), err)
			}
		})
	}
}

func TestDestructiveVerbs_acceptWithYes(t *testing.T) {
	// With --yes set, the --yes gate is satisfied. The command then proceeds
	// to getClient → HTTP call. We don't need the HTTP to succeed; we just
	// need to confirm the early DESTRUCTIVE_REFUSED branch was skipped.
	//
	// Strategy: invoke with --yes + --dry-run. Dry-run skips HTTP entirely,
	// so the command should return nil error (because the --yes path took
	// the dry-run skip), no DESTRUCTIVE_REFUSED.
	setFakeCreds(t)

	cases := []struct {
		name string
		args []string
	}{
		{"number release --yes --dry-run", []string{"number", "release", "+14155551234", "--yes", "--dry-run"}},
		{"call hangup --yes --dry-run", []string{"call", "hangup", "CALL-UUID", "--yes", "--dry-run"}},
		{"recording delete --yes --dry-run", []string{"recording", "delete", "REC-UUID", "--yes", "--dry-run"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err, _, _ := execCmd(t, tc.args...)
			if err != nil {
				t.Errorf("plivo %s — expected nil err with --yes --dry-run, got: %v", strings.Join(tc.args, " "), err)
			}
		})
	}
}

// ─── Spend verbs default to dry-run when --yes is absent ────────────────────

func TestSpendVerbs_defaultToDryRun(t *testing.T) {
	// Spend verbs (message send, call make, verify session create, cnam,
	// masking session create, mpc create, mpc participant add, brand create,
	// campaign create) should NEVER hit the network when --yes is missing.
	//
	// We point the api client at an httptest server (via custom transport)
	// and confirm the server never gets a request — proving the dry-run
	// kicked in before any HTTP call.
	setFakeCreds(t)

	srv, hits := startCapturingHTTPServer(t, 200, `{}`)
	// (t.Cleanup is already wired inside startCapturingHTTPServer)
	_ = srv

	// Override api.Client.HTTP for every new client created during this test
	// by intercepting api.New via a small hack: we patch the http.DefaultTransport
	// indirectly by replacing the api client's BaseURL with our test server
	// and providing a custom transport. Simpler: patch the http.DefaultTransport.
	origTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = origTransport }()
	http.DefaultTransport = &http.Transport{
		// Forward everything to our test server.
		Proxy: func(*http.Request) (*url.URL, error) {
			return url.Parse(srv.URL)
		},
	}

	cases := []struct {
		name string
		args []string
	}{
		{"message send", []string{"message", "send", "--src", "+1", "--dst", "+1", "--text", "hi"}},
		{"call make", []string{"call", "make", "--from", "+1", "--to", "+1"}},
		{"verify session create", []string{"verify", "session", "create", "--recipient", "+1", "--app-uuid", "abc"}},
		{"cnam", []string{"cnam", "+14155551234"}},
		{"masking session create", []string{"masking", "session", "create", "--first-party", "+1", "--second-party", "+2"}},
		{"mpc create", []string{"mpc", "create", "--name", "ci-test"}},
		{"brand create", []string{"brand", "create", "--alias", "ci", "--legal-name", "ACME Inc"}},
		{"campaign create", []string{
			"campaign", "create",
			"--alias", "ci",
			"--brand-id", "b1",
			"--usecase", "MARKETING",
			"--description", "x",
			"--message-flow", "x",
			"--sample-message-1", "x",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := len(hits())
			err, _, stderr := execCmd(t, tc.args...)
			after := len(hits())
			if err != nil {
				t.Errorf("plivo %s — expected nil err on default dry-run path, got: %v", strings.Join(tc.args, " "), err)
			}
			// Must NOT have called the server (dry-run skips HTTP).
			if after != before {
				t.Errorf("plivo %s — spend verb hit the network without --yes (hits delta = %d)", strings.Join(tc.args, " "), after-before)
			}
			// Should have printed the dry-run banner to stderr.
			if !strings.Contains(stderr, "[dry-run]") && !strings.Contains(stderr, "dry-run") {
				t.Errorf("plivo %s — stderr should mention dry-run, got: %q", strings.Join(tc.args, " "), stderr)
			}
		})
	}
}

// ─── Sanity: api.Client.IsScopedToken consistency under destructive flow ────

func TestNew_clientPicksAuthMode(t *testing.T) {
	// Sanity check that the helper api.New respects the token shape so the
	// authToken commands ultimately send the right auth header.
	c := api.New("MA", "stk_xyz", time.Second)
	if !c.IsScopedToken() {
		t.Error("stk_ token should be detected as scoped")
	}
	c = api.New("MA", "regular", time.Second)
	if c.IsScopedToken() {
		t.Error("regular token should NOT be detected as scoped")
	}
	// Cross-check the clierr surface tests still see the same code constant.
	if clierr.CodeDestructiveRefused == "" {
		t.Error("DESTRUCTIVE_REFUSED code constant unexpectedly empty")
	}
}
