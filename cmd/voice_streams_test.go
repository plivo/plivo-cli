package cmd

import (
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// TestVoiceStreams_unknownSubcommandErrors pins the reported bug:
// `plivo voice streams bogustypo` used to print help and exit 0 — cobra's
// default silently accepts an unrecognized subcommand on any non-root
// parent command (only the true root gets that check for free). It must
// now return an error that maps to a non-zero exit.
func TestVoiceStreams_unknownSubcommandErrors(t *testing.T) {
	setFakeCreds(t)
	err, _, _ := execCmd(t, "voice", "streams", "bogustypo")
	if err == nil {
		t.Fatal("plivo voice streams bogustypo — expected an error, got nil (would exit 0)")
	}
	if !strings.Contains(err.Error(), "bogustypo") {
		t.Errorf("expected error to name the bad subcommand, got: %v", err)
	}
	if code := clierr.Wrap(err).ExitCode(); code != ExitUserError {
		t.Errorf("exit code = %d, want %d (ExitUserError)", code, ExitUserError)
	}
}

// TestVoiceStreams_bareInvocationStillShowsHelp guards the non-regression:
// `plivo voice streams` with no subcommand is a legitimate invocation and
// must keep printing help and exiting 0, not start erroring too.
func TestVoiceStreams_bareInvocationStillShowsHelp(t *testing.T) {
	setFakeCreds(t)
	err, stdout, _ := execCmd(t, "voice", "streams")
	if err != nil {
		t.Fatalf("plivo voice streams (bare) — expected nil error, got: %v", err)
	}
	if !strings.Contains(stdout, "Available Commands") {
		t.Errorf("expected help text listing subcommands, got: %q", stdout)
	}
}
