package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/feedback"
	"github.com/spf13/cobra"
)

const zeroRequestID = "00000000-0000-0000-0000-000000000000"

// testRequestID mixes letters and digits like a real request id, which the
// comment scrubber would redact as a token.
const testRequestID = "00000000-0000-0000-0000-00000000abcd"

// leafCmd returns a real command from the tree, for the recorder to name.
func leafCmd(t *testing.T, path ...string) *cobra.Command {
	t.Helper()
	c, _, err := rootCmd.Find(path)
	if err != nil || c == rootCmd {
		t.Fatalf("no command at %v: %v", path, err)
	}
	return c
}

func TestRecordLastError_keepsTheCategoryNotTheInput(t *testing.T) {
	home := setEmptyHome(t)
	recordLastError(leafCmd(t, "voice", "calls", "get"), &clierr.Error{
		Code:      clierr.CodeResourceNotFound,
		Message:   "Call to +14155551234 not found",
		Hint:      "List available resources",
		RequestID: testRequestID,
	}, 1)

	raw, err := os.ReadFile(filepath.Join(home, ".plivo", "last-error.json"))
	if err != nil {
		t.Fatalf("nothing recorded: %v", err)
	}
	for _, leak := range []string{"+14155551234", "not found", "List available"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("last-error.json carries %q, which came from the message or hint:\n%s", leak, raw)
		}
	}
	got, err := feedback.LoadLastError()
	if err != nil || got == nil {
		t.Fatalf("LoadLastError = %v, %v", got, err)
	}
	want := feedback.LastError{Command: "plivo voice calls get", ExitCode: 1, ErrorCode: "RESOURCE_NOT_FOUND",
		RequestID: testRequestID, CLIVersion: versionValue(), OS: runtime.GOOS, Arch: runtime.GOARCH, Timestamp: got.Timestamp}
	if *got != want {
		t.Errorf("recorded %+v, want %+v", *got, want)
	}
	if time.Since(got.Timestamp) > time.Minute || got.Timestamp.Location() != time.UTC {
		t.Errorf("timestamp %v is not a current UTC time", got.Timestamp)
	}
}

// A `feedback --bug` that fails to send must not replace the failure it was
// trying to report.
func TestRecordLastError_skipsFeedbackItself(t *testing.T) {
	setEmptyHome(t)
	recordLastError(feedbackCmd, &clierr.Error{Code: clierr.CodeNetworkError}, 3)
	if got, _ := feedback.LoadLastError(); got != nil {
		t.Errorf("a failed feedback run was recorded: %+v", got)
	}
}

// Best effort: a write that fails neither panics nor prints, so the error the
// user sees and the exit code stay exactly as they were.
func TestRecordLastError_aFailedWriteIsSilent(t *testing.T) {
	home := setEmptyHome(t)
	if err := os.WriteFile(filepath.Join(home, ".plivo"), []byte("a file where the directory goes"), 0o600); err != nil {
		t.Fatal(err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	r, w, _ := os.Pipe()
	os.Stdout, os.Stderr = w, w
	recordLastError(leafCmd(t, "voice", "calls", "get"), &clierr.Error{Code: clierr.CodeUpstreamError}, 3)
	os.Stdout, os.Stderr = origOut, origErr
	_ = w.Close()
	if out, _ := io.ReadAll(r); len(out) != 0 {
		t.Errorf("a failed write printed %q", out)
	}
}

// executeQuietly runs one invocation through execute(), the path Execute
// takes, with its output discarded, and returns the command it reports.
func executeQuietly(t *testing.T, args ...string) (*cobra.Command, error) {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = devnull, devnull
	t.Cleanup(func() {
		os.Stdout, os.Stderr = stdout, stderr
		_ = devnull.Close()
		rootCmd.SetArgs(nil)
		resetAllFlags(rootCmd)
	})
	rootCmd.SetArgs(args)
	return execute(args)
}

// The argv pre-scan answers some invocations before cobra runs. Its
// failures are recorded like any other, naming the command the arguments
// reached, with the exit code the user got.
func TestReportError_recordsPrescanFailures(t *testing.T) {
	for _, tc := range []struct {
		name, command, code string
		args                []string
	}{
		{"unknown subcommand with --help", "plivo sip trunks", "BAD_INPUT", []string{"sip", "trunks", "bogus", "--help"}},
		{"--schema with a bad format", "plivo numbers get", "BAD_INPUT", []string{"numbers", "get", "--schema", "-o", "bogus"}},
		{"--schema with a bad query", "plivo numbers get", "BAD_FLAG", []string{"numbers", "get", "--schema", "--query", "["}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEmptyHome(t)
			ran, err := executeQuietly(t, tc.args...)
			if err == nil {
				t.Fatal("want an error")
			}
			exit := reportError(ran, err)
			got, _ := feedback.LoadLastError()
			if got == nil || got.Command != tc.command || got.ErrorCode != tc.code || got.ExitCode != exit || exit != 1 {
				t.Errorf("recorded %+v (exit %d), want %s / %s / 1", got, exit, tc.command, tc.code)
			}
		})
	}
}

// Failures cobra itself raises, before any command runs, name the command too.
func TestReportError_recordsFlagErrors(t *testing.T) {
	setEmptyHome(t)
	ran, err := executeQuietly(t, "numbers", "list", "--limit", "abc")
	if err == nil {
		t.Fatal("want an error")
	}
	reportError(ran, err)
	if got, _ := feedback.LoadLastError(); got == nil || got.Command != "plivo numbers list" {
		t.Errorf("recorded %+v, want plivo numbers list", got)
	}
}

// bugCollector stands in for the feedback collector. The handler runs on the
// server's goroutine, so everything it records is behind mu.
type bugCollector struct {
	mu      sync.Mutex
	hits    int
	event   feedback.Event
	headers http.Header
	url     string
}

func newBugCollector(t *testing.T, status int) *bugCollector {
	t.Helper()
	c := &bugCollector{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.hits++
		c.headers = r.Header.Clone()
		_ = json.Unmarshal(body, &c.event)
		c.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	c.url = srv.URL
	t.Setenv(feedback.EndpointEnvVar, srv.URL)
	return c
}

func (c *bugCollector) snapshot() (int, feedback.Event, http.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.event, c.headers
}

// bugReportSetup gives the test a temp HOME holding a logged-in profile and a
// recorded failure, with telemetry on, and turns on --bug.
func bugReportSetup(t *testing.T) {
	t.Helper()
	resetFeedbackFlags(t)
	writeProfile(t, setEmptyHome(t), "active = \"test\"\n\n[profiles.test]\n"+
		"auth_id = \"CIFAKEPLACEHOLDER001\"\nauth_token = \"ci-only-not-a-real-token\"\n"+
		"email = \"dev@example.com\"\nregion = \"us\"\naom_uuid = \""+zeroRequestID+"\"\n")
	t.Setenv(config.TelemetryEnvVar, "")
	t.Setenv(feedback.MachineIDEnvVar, "test-machine")
	prevProfile, prevDryRun := profileFlag, dryRunFlag
	profileFlag = ""
	t.Cleanup(func() { profileFlag, dryRunFlag = prevProfile, prevDryRun })
	recordLastError(leafCmd(t, "voice", "calls", "get"), &clierr.Error{Code: clierr.CodeUpstreamError, RequestID: testRequestID}, 3)
	feedbackBug = true
}

func TestFeedbackBug_dryRunShowsTheExactRequest(t *testing.T) {
	bugReportSetup(t)
	c := newBugCollector(t, http.StatusNoContent)
	feedbackMessage = "trunk list hangs"
	dryRunFlag = true

	out, err := runWithFakeStdio(t, "")
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if hits, _, _ := c.snapshot(); hits != 0 {
		t.Fatalf("--dry-run sent %d request(s)", hits)
	}
	for _, want := range []string{
		"POST " + c.url,
		"X-Plivo-CLI-Auth-ID: CIFAKEPLACEHOLDER001",
		"X-Plivo-CLI-Email: dev@example.com",
		`"trigger": "bug_report"`,
		"trunk list hangs",
		"plivo voice calls get",
		"UPSTREAM_ERROR",
		testRequestID,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the preview is missing %q:\n%s", want, out)
		}
	}
}

func TestFeedbackBug_telemetryOffShowsNoIdentity(t *testing.T) {
	bugReportSetup(t)
	newBugCollector(t, http.StatusNoContent)
	t.Setenv(config.TelemetryEnvVar, "0")
	dryRunFlag = true

	out, err := runWithFakeStdio(t, "")
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	for _, h := range []string{"X-Plivo-CLI-Auth-ID", "X-Plivo-CLI-Email", "X-Plivo-CLI-Region", "X-Plivo-CLI-AOM-UUID"} {
		if strings.Contains(out, h) {
			t.Errorf("telemetry is off but the preview lists %s:\n%s", h, out)
		}
	}
}

func TestFeedbackBug_withoutATerminalNeedsYes(t *testing.T) {
	bugReportSetup(t)
	c := newBugCollector(t, http.StatusNoContent)
	feedbackMessage = "x"

	_, err := runWithFakeStdio(t, "")
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Code != clierr.CodeDestructiveRefused {
		t.Fatalf("err = %v, want DESTRUCTIVE_REFUSED", err)
	}
	if hits, _, _ := c.snapshot(); hits != 0 {
		t.Errorf("an unconfirmed report was sent")
	}
}

func TestFeedbackBug_sendsTheCommentAndTheFailure(t *testing.T) {
	bugReportSetup(t)
	c := newBugCollector(t, http.StatusNoContent)
	feedbackMessage = "calling +14155551234 fails"
	yesFlag = true

	if _, err := runWithFakeStdio(t, ""); err != nil {
		t.Fatalf("send: %v", err)
	}
	hits, ev, headers := c.snapshot()
	if hits != 1 || ev.Trigger != feedback.TriggerBugReport {
		t.Fatalf("hits = %d, trigger = %q", hits, ev.Trigger)
	}
	// The comment is scrubbed; the recorded failure, which the scrubber would
	// read as a token, is added after it.
	if strings.Contains(ev.Comment, "+14155551234") || !strings.Contains(ev.Comment, "[REDACTED-PHONE]") {
		t.Errorf("the comment was not scrubbed: %q", ev.Comment)
	}
	for _, want := range []string{"plivo voice calls get", "exit code: 3", "UPSTREAM_ERROR", testRequestID} {
		if !strings.Contains(ev.Comment, want) {
			t.Errorf("the sent comment lacks %q: %q", want, ev.Comment)
		}
	}
	if headers.Get("X-Plivo-CLI-Email") != "dev@example.com" {
		t.Errorf("the identity the preview shows was not sent: %v", headers)
	}
}

func TestFeedbackBug_aFailedSendPrintsAnIssueLink(t *testing.T) {
	bugReportSetup(t)
	newBugCollector(t, http.StatusInternalServerError)
	feedbackMessage = "trunk list hangs"
	yesFlag = true

	out, err := runWithFakeStdio(t, "")
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Code != clierr.CodeNetworkError {
		t.Fatalf("err = %v, want NETWORK_ERROR", err)
	}
	link := ""
	for _, ln := range strings.Split(out, "\n") {
		if l := strings.TrimSpace(ln); strings.HasPrefix(l, "https://github.com/plivo/plivo-cli/issues/new?") {
			link = l
		}
	}
	if link == "" {
		t.Fatalf("no issue link printed:\n%s", out)
	}
	if len(link) > maxIssueURLLen {
		t.Errorf("link is %d chars, over the %d cap", len(link), maxIssueURLLen)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("template") != "bug_report.md" || !strings.Contains(q.Get("body"), "trunk list hangs") || !strings.Contains(q.Get("body"), testRequestID) {
		t.Errorf("link does not prefill the report: %v", q)
	}
	for _, id := range []string{"dev@example.com", "CIFAKEPLACEHOLDER001", "test-machine"} {
		if strings.Contains(link, url.QueryEscape(id)) {
			t.Errorf("the public issue link carries %q", id)
		}
	}
}

func TestBugIssueURL_capsTheLength(t *testing.T) {
	last := &feedback.LastError{Command: "plivo voice calls get", ExitCode: 3, ErrorCode: "UPSTREAM_ERROR", RequestID: testRequestID}
	link := bugIssueURL(strings.Repeat("ü", 3000), last)
	if len(link) > maxIssueURLLen {
		t.Fatalf("link is %d chars, over the %d cap", len(link), maxIssueURLLen)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	body := u.Query().Get("body")
	if !strings.Contains(body, "ü…") || !strings.Contains(body, testRequestID) {
		t.Errorf("want the comment cut with … and the failure kept, got %q", body)
	}
}

func TestFeedbackBug_nothingToReport(t *testing.T) {
	resetFeedbackFlags(t)
	setEmptyHome(t)
	t.Setenv(feedback.MachineIDEnvVar, "test-machine")
	c := newBugCollector(t, http.StatusNoContent)
	feedbackBug, yesFlag = true, true

	out, err := runWithFakeStdio(t, "")
	if err != nil || !strings.Contains(out, "Nothing to report") {
		t.Errorf("err = %v, out:\n%s", err, out)
	}
	if hits, _, _ := c.snapshot(); hits != 0 {
		t.Errorf("an empty report was sent")
	}
}
