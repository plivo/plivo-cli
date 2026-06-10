package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/feedback"
)

// resetFeedbackFlags zeros out the package-level feedback flags between
// tests so one test's --rating doesn't bleed into another's defaults.
func resetFeedbackFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		feedbackRating = 0
		feedbackMessage = ""
		feedbackNoContext = false
		feedbackYes = false
	})
	feedbackRating = 0
	feedbackMessage = ""
	feedbackNoContext = false
	feedbackYes = false
}

// runWithFakeStdio invokes the feedback command with controlled stdin
// + captured stdout/stderr. Returns stderr (where the command writes
// its prompts + summary) and any error from RunE.
func runWithFakeStdio(t *testing.T, stdin string) (stderr string, err error) {
	t.Helper()
	in := strings.NewReader(stdin)
	var outBuf, errBuf bytes.Buffer
	feedbackCmd.SetIn(in)
	feedbackCmd.SetOut(&errBuf) // command writes prompts to OutOrStderr
	feedbackCmd.SetErr(&errBuf)
	t.Cleanup(func() {
		feedbackCmd.SetIn(nil)
		feedbackCmd.SetOut(nil)
		feedbackCmd.SetErr(nil)
	})
	err = feedbackCmd.RunE(feedbackCmd, []string{})
	_ = outBuf
	return errBuf.String(), err
}

func TestFeedback_oneShot_bothFlags_skipsPrompts(t *testing.T) {
	resetFeedbackFlags(t)
	t.Setenv(feedback.MachineIDEnvVar, "test-machine")

	var got feedback.Event
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(feedback.EndpointEnvVar, srv.URL)

	feedbackRating = 4
	feedbackMessage = "the upgrade flow was great"
	feedbackYes = true

	out, err := runWithFakeStdio(t, "")
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if got.Rating != 4 {
		t.Errorf("collector got rating %d, want 4", got.Rating)
	}
	if got.Comment != "the upgrade flow was great" {
		t.Errorf("collector got comment %q", got.Comment)
	}
	if !strings.Contains(out, "Submitted") {
		t.Errorf("expected 'Submitted' in output, got: %s", out)
	}
}

func TestFeedback_oneShot_ratingOnly(t *testing.T) {
	resetFeedbackFlags(t)
	t.Setenv(feedback.MachineIDEnvVar, "test-machine")

	var got feedback.Event
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(feedback.EndpointEnvVar, srv.URL)

	feedbackRating = 5
	feedbackYes = true
	// stdin not a TTY → no comment prompt; rating from flag, comment empty.
	_, err := runWithFakeStdio(t, "")
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if got.Rating != 5 {
		t.Errorf("rating = %d, want 5", got.Rating)
	}
	if got.Comment != "" {
		t.Errorf("comment = %q, want empty", got.Comment)
	}
}

func TestFeedback_oneShot_messageOnly(t *testing.T) {
	resetFeedbackFlags(t)
	t.Setenv(feedback.MachineIDEnvVar, "test-machine")

	var got feedback.Event
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(feedback.EndpointEnvVar, srv.URL)

	feedbackMessage = "the docs for compliance create are confusing"
	feedbackYes = true

	_, err := runWithFakeStdio(t, "")
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if got.Rating != 0 {
		t.Errorf("rating = %d, want 0", got.Rating)
	}
	if !strings.Contains(got.Comment, "compliance create") {
		t.Errorf("comment = %q", got.Comment)
	}
}

func TestFeedback_noTTYAndNoFlags_errors(t *testing.T) {
	resetFeedbackFlags(t)
	t.Setenv(feedback.EndpointEnvVar, "http://unused")
	t.Setenv(feedback.MachineIDEnvVar, "test-machine")

	_, err := runWithFakeStdio(t, "")
	if err == nil {
		t.Fatal("expected error when no TTY + no flags")
	}
	if !strings.Contains(err.Error(), "needs a terminal") {
		t.Errorf("err = %v, want 'needs a terminal'", err)
	}
}

func TestFeedback_emptyRatingAndComment_doesNotSubmit(t *testing.T) {
	resetFeedbackFlags(t)
	t.Setenv(feedback.MachineIDEnvVar, "test-machine")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(feedback.EndpointEnvVar, srv.URL)

	// One-shot --message but message is whitespace-only → treated as empty.
	feedbackMessage = "   "
	feedbackYes = true

	out, err := runWithFakeStdio(t, "")
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if calls != 0 {
		t.Errorf("collector got %d calls, want 0", calls)
	}
	if !strings.Contains(out, "Nothing to submit") {
		t.Errorf("expected 'Nothing to submit' in output, got: %s", out)
	}
}

func TestFeedback_endpointUnset_surfacesFriendlyError(t *testing.T) {
	resetFeedbackFlags(t)
	t.Setenv(feedback.EndpointEnvVar, "")
	t.Setenv(feedback.MachineIDEnvVar, "test-machine")

	feedbackRating = 3
	feedbackYes = true

	out, err := runWithFakeStdio(t, "")
	if err != nil {
		t.Fatalf("RunE returned err: %v", err)
	}
	if !strings.Contains(out, "PLIVO_FEEDBACK_ENDPOINT") {
		t.Errorf("expected mention of env var in error, got: %s", out)
	}
	if !strings.Contains(out, "issue") {
		t.Errorf("expected GitHub issues fallback, got: %s", out)
	}
}

func TestFeedback_badRatingFlag_errors(t *testing.T) {
	resetFeedbackFlags(t)
	t.Setenv(feedback.MachineIDEnvVar, "test-machine")

	feedbackRating = 9 // out of 1-5

	_, err := runWithFakeStdio(t, "")
	if err == nil {
		t.Fatal("expected error for rating 9")
	}
	if !strings.Contains(err.Error(), "1-5") {
		t.Errorf("err should mention 1-5 range, got: %v", err)
	}
}

func TestFeedback_messageTooLong_errors(t *testing.T) {
	resetFeedbackFlags(t)
	t.Setenv(feedback.MachineIDEnvVar, "test-machine")

	feedbackMessage = strings.Repeat("a", feedback.MaxCommentChars+1)

	_, err := runWithFakeStdio(t, "")
	if err == nil {
		t.Fatal("expected error for over-length message")
	}
	if !strings.Contains(err.Error(), "≤") {
		t.Errorf("err should mention max length, got: %v", err)
	}
}

func TestFeedback_redactsPIIBeforeSubmit(t *testing.T) {
	resetFeedbackFlags(t)
	t.Setenv(feedback.MachineIDEnvVar, "test-machine")

	var got feedback.Event
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(feedback.EndpointEnvVar, srv.URL)

	feedbackRating = 2
	feedbackMessage = "tried with MAABCDEFGHIJKLMNOPQR and got an error"
	feedbackYes = true

	if _, err := runWithFakeStdio(t, ""); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if strings.Contains(got.Comment, "MAABCDEFGHIJKLMNOPQR") {
		t.Errorf("auth_id leaked through to collector: %q", got.Comment)
	}
	if got.RedactionCount != 1 {
		t.Errorf("RedactionCount = %d, want 1", got.RedactionCount)
	}
}
