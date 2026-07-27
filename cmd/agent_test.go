package cmd

import (
	"strings"
	"testing"
)

// The agents commands talk to the production gateway baked into
// api.DefaultBaseURL — there is deliberately no way to redirect them at
// runtime, so HTTP-level behaviour is covered in internal/api (where BaseURL
// is settable directly) rather than through the command layer. What is
// asserted here is the argument handling that runs BEFORE any request.

func TestAgentsCreate_requiresNameFromFlagOrFile(t *testing.T) {
	setFakeCreds(t)

	err, _, _ := execCmd(t, "agents", "create")
	if err == nil {
		t.Fatal("agents create with neither --name nor --file: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "BAD_INPUT") {
		t.Errorf("expected BAD_INPUT, got: %v", err)
	}
}

func TestAgentsCreate_rejectsUnreadableFile(t *testing.T) {
	setFakeCreds(t)

	err, _, _ := execCmd(t, "agents", "create", "--name", "x", "--file", "/nonexistent/flow.json")
	if err == nil {
		t.Fatal("agents create --file with a missing path: expected an error, got nil")
	}
}
