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
	"github.com/zalando/go-keyring"
)

// resetAllFlags walks the command tree and resets every flag to its DefValue
// + clears the Changed bit. Required because cmd globals (yesFlag, dryRunFlag,
// msgSendSrc, …) persist across rootCmd.Execute() calls and pollute later
// tests — including the help snapshots, which would otherwise drift on the
// second run under `go test -count=N`.
func resetAllFlags(c *cobra.Command) {
	visit := func(f *pflag.Flag) {
		// Slice flags (StringArray etc.) must be cleared via Replace: calling
		// Set(DefValue) on them APPENDS the literal default ("[]") rather than
		// resetting, which pollutes later runs (and snapshots under -count=2).
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
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
	adminServer = ""
	apiURLFlag = ""

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
//
// Also swaps the OS keychain for an in-memory mock — the underlying
// go-keyring library talks to the system keychain regardless of HOME,
// so tests that exercise SetToken/DeleteToken (e.g. login/logout) would
// otherwise pop a real "Keychain Not Found" dialog on macOS. MockInit
// is a no-op when not running under tests.
func setFakeCreds(t *testing.T) {
	t.Helper()
	keyring.MockInit()
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
		{"numbers release", []string{"numbers", "release", "+14155551234"}},
		{"voice calls hangup", []string{"voice", "calls", "hangup", "CALL-UUID"}},
		{"voice recordings delete", []string{"voice", "recordings", "delete", "REC-UUID"}},
		{"account subaccounts delete", []string{"account", "subaccounts", "delete", "SAxxx"}},
		{"voice endpoints delete", []string{"voice", "endpoints", "delete", "EP-ID"}},
		{"account applications delete", []string{"account", "applications", "delete", "APP-ID"}},
		{"numbers compliance delete", []string{"numbers", "compliance", "delete", "DOC-ID"}},
		{"numbers masking sessions delete", []string{"numbers", "masking", "sessions", "delete", "SESS-UUID"}},
		{"voice conferences hangup", []string{"voice", "conferences", "hangup", "room-1"}},
		{"voice conferences member kick", []string{"voice", "conferences", "member", "kick", "room-1", "member-id"}},
		{"voice multiparty end", []string{"voice", "multiparty", "end", "MPC-UUID"}},
		{"voice multiparty participant kick", []string{"voice", "multiparty", "participant", "kick", "MPC-UUID", "PART-ID"}},
		{"voice calls streams stop", []string{"voice", "calls", "streams", "stop", "CALL-UUID"}},
		{"messaging sms powerpacks delete", []string{"messaging", "sms", "powerpacks", "delete", "PP-UUID"}},
		{"messaging sms powerpacks numbers remove", []string{"messaging", "sms", "powerpacks", "numbers", "remove", "PP-UUID", "+14155551234"}},
		{"messaging sms 10dlc links delete", []string{"messaging", "sms", "10dlc", "links", "delete", "LINK-ID"}},
		{"agents delete", []string{"agents", "delete", "AGENT-ID"}},
		// `plivo api` escape hatch: mutating methods share the spend-verb gate
		// so an agent can't accidentally POST/PUT/PATCH/DELETE without --yes.
		{"api POST", []string{"api", "POST", "/Message/", "--body", `{"src":"+1","dst":"+1","text":"hi"}`}},
		{"api PUT", []string{"api", "PUT", "/Application/APP-1/"}},
		{"api PATCH", []string{"api", "PATCH", "/Application/APP-1/"}},
		{"api DELETE", []string{"api", "DELETE", "/Number/+14155551234/"}},
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

// TestDestructiveVerb_refusesWithoutYes_viaCanonicalPath proves the
// destructive-refusal contract still triggers when reached via the
// canonical command path. (The pre-grammar argv-rewrite shim that used to
// also exercise this via `call hangup` is gone — that pathway no longer
// exists, but the underlying refusal logic is what we actually want to
// pin down.)
func TestDestructiveVerb_refusesWithoutYes_viaCanonicalPath(t *testing.T) {
	setFakeCreds(t)
	err, _, _ := execCmd(t, "voice", "calls", "hangup", "CALL-UUID")
	if err == nil || !strings.Contains(err.Error(), "DESTRUCTIVE_REFUSED") {
		t.Errorf("`voice calls hangup` should refuse without --yes, got: %v", err)
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
		{"numbers release --yes --dry-run", []string{"numbers", "release", "+14155551234", "--yes", "--dry-run"}},
		{"voice calls hangup --yes --dry-run", []string{"voice", "calls", "hangup", "CALL-UUID", "--yes", "--dry-run"}},
		{"voice recordings delete --yes --dry-run", []string{"voice", "recordings", "delete", "REC-UUID", "--yes", "--dry-run"}},
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

// ─── Spend verbs refuse without --yes (unified contract) ────────────────────

// spendVerbCases enumerates every server-state-mutating command that should
// honour the unified spend-verb contract:
//
//	--yes              → proceed; HTTP call goes through
//	--dry-run          → proceed; client.DryRun=true, no HTTP
//	neither            → DESTRUCTIVE_REFUSED (exit 5)
//
// Same list drives two sibling tests (refusal + dry-run preview), so adding
// a new spend verb only requires editing this table once.
func spendVerbCases() []struct {
	name string
	args []string
} {
	return []struct {
		name string
		args []string
	}{
		{"messaging sms send", []string{"messaging", "sms", "send", "--src", "+1", "--dst", "+1", "--text", "hi"}},
		{"messaging mms send", []string{"messaging", "mms", "send", "--src", "+1", "--dst", "+1", "--text", "hi"}},
		{"messaging whatsapp send", []string{"messaging", "whatsapp", "send", "--src", "+1", "--dst", "+1", "--text", "hi"}},
		{"voice calls make", []string{"voice", "calls", "make", "--from", "+1", "--to", "+1"}},
		{"verify sessions create", []string{"verify", "sessions", "create", "--recipient", "+1", "--app-uuid", "abc"}},
		{"numbers cnam", []string{"numbers", "cnam", "+14155551234"}},
		{"numbers buy", []string{"numbers", "buy", "+14155551234"}},
		{"numbers masking sessions create", []string{"numbers", "masking", "sessions", "create", "--first-party", "+1", "--second-party", "+2"}},
		{"voice multiparty create", []string{"voice", "multiparty", "create", "--name", "ci-test"}},
		{"messaging sms 10dlc brands create", []string{"messaging", "sms", "10dlc", "brands", "create", "--alias", "ci", "--legal-name", "ACME Inc"}},
		{"messaging sms 10dlc campaigns create", []string{
			"messaging", "sms", "10dlc", "campaigns", "create",
			"--alias", "ci",
			"--brand-id", "b1",
			"--usecase", "MARKETING",
			"--description", "x",
			"--message-flow", "x",
			"--sample-message-1", "x",
		}},
	}
}

// TestSpendVerbs_refuseWithoutYes pins the post-unification contract:
// every spend verb invoked without --yes must return DESTRUCTIVE_REFUSED
// (exit 5) and must NOT touch the network. The previous behaviour
// (silently downgrading to dry-run with a stderr banner) was misleading
// for agents reading exit 0 from an un-confirmed `messaging sms send`.
func TestSpendVerbs_refuseWithoutYes(t *testing.T) {
	setFakeCreds(t)

	srv, hits := startCapturingHTTPServer(t, 200, `{}`)
	_ = srv
	origTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = origTransport }()
	http.DefaultTransport = &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return url.Parse(srv.URL) },
	}

	for _, tc := range spendVerbCases() {
		t.Run(tc.name, func(t *testing.T) {
			before := len(hits())
			err, _, _ := execCmd(t, tc.args...)
			after := len(hits())
			if err == nil {
				t.Fatalf("plivo %s — expected DESTRUCTIVE_REFUSED, got nil err", strings.Join(tc.args, " "))
			}
			if !strings.Contains(err.Error(), "DESTRUCTIVE_REFUSED") {
				t.Errorf("plivo %s — expected DESTRUCTIVE_REFUSED, got: %v", strings.Join(tc.args, " "), err)
			}
			if after != before {
				t.Errorf("plivo %s — refused command still hit the network (hits delta = %d)", strings.Join(tc.args, " "), after-before)
			}
		})
	}
}

// TestSpendVerbs_dryRunAlonePreviews confirms --dry-run alone (no --yes) is
// the documented preview path: the command proceeds without error and
// without hitting the network.
func TestSpendVerbs_dryRunAlonePreviews(t *testing.T) {
	setFakeCreds(t)

	srv, hits := startCapturingHTTPServer(t, 200, `{}`)
	_ = srv
	origTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = origTransport }()
	http.DefaultTransport = &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return url.Parse(srv.URL) },
	}

	for _, tc := range spendVerbCases() {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string(nil), tc.args...)
			args = append(args, "--dry-run")
			before := len(hits())
			err, _, stderr := execCmd(t, args...)
			after := len(hits())
			if err != nil {
				t.Errorf("plivo %s --dry-run — expected nil err, got: %v", strings.Join(tc.args, " "), err)
			}
			if after != before {
				t.Errorf("plivo %s --dry-run — dry-run still hit the network (hits delta = %d)", strings.Join(tc.args, " "), after-before)
			}
			if !strings.Contains(stderr, "[dry-run]") && !strings.Contains(stderr, "dry-run") {
				t.Errorf("plivo %s --dry-run — stderr should mention dry-run, got: %q", strings.Join(tc.args, " "), stderr)
			}
		})
	}
}

// ─── -o yaml / -o tsv → BAD_INPUT (PersistentPreRunE rejection) ─────────────

// TestOutputFormat_rejectsUnsupportedValues confirms `-o yaml`, `-o tsv`, etc.
// are hard errors (BAD_INPUT, exit 2) instead of the previous silent fall-
// through to JSON rendering. Wired via root.go's PersistentPreRunE so every
// command — even read-only ones like `numbers list` — sees the rejection
// before its RunE fires.
func TestOutputFormat_rejectsUnsupportedValues(t *testing.T) {
	setFakeCreds(t)

	cases := []string{"yaml", "tsv", "csv", "xml", "garbage"}
	for _, bad := range cases {
		t.Run("o_"+bad, func(t *testing.T) {
			err, _, _ := execCmd(t, "-o", bad, "numbers", "list")
			if err == nil {
				t.Fatalf("-o %s — expected BAD_INPUT, got nil", bad)
			}
			if !strings.Contains(err.Error(), "BAD_INPUT") {
				t.Errorf("-o %s — expected BAD_INPUT, got: %v", bad, err)
			}
			if !strings.Contains(err.Error(), bad) {
				t.Errorf("-o %s — error should echo the bad value, got: %v", bad, err)
			}
		})
	}
}

func TestOutputFormat_acceptsSupportedValues(t *testing.T) {
	setFakeCreds(t)
	for _, ok := range []string{"json", "table", "JSON", "Table"} {
		t.Run("o_"+ok, func(t *testing.T) {
			// --dry-run keeps us off the network. The Validate step runs in
			// PersistentPreRunE before the spend-verb gate, so a failing -o
			// aborts before --dry-run does. We only need to confirm valid
			// formats DON'T abort with BAD_INPUT.
			err, _, _ := execCmd(t, "-o", ok, "--dry-run", "numbers", "list")
			if err != nil && strings.Contains(err.Error(), "BAD_INPUT") {
				t.Errorf("-o %s — should be accepted, got BAD_INPUT: %v", ok, err)
			}
		})
	}
}

// ─── Sanity: api.Client.IsScopedToken consistency under destructive flow ────

// TestCompliance_dryRunNoNetwork exercises the create (multipart) and link
// (JSON) command paths end-to-end in dry-run: no network, no file opened, and
// the compliance URL is printed.
func TestCompliance_dryRunNoNetwork(t *testing.T) {
	setFakeCreds(t)

	err, _, stderr := execCmd(t, "numbers", "compliance", "create",
		"--data", `{"country_iso":"US","number_type":"local"}`,
		"--file", "documents[0].file=@/no/such/file.pdf", "--dry-run")
	if err != nil {
		t.Errorf("compliance create --dry-run: unexpected err: %v", err)
	}
	if !strings.Contains(stderr, "PhoneNumber/Compliance/") {
		t.Errorf("create dry-run should print the compliance URL, got: %q", stderr)
	}

	err, _, _ = execCmd(t, "numbers", "compliance", "link", "--link", "+14155551234=CMP1", "--dry-run")
	if err != nil {
		t.Errorf("compliance link --dry-run: unexpected err: %v", err)
	}
}

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
