package cmd

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/feedback"
	"github.com/spf13/cobra"
)

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
