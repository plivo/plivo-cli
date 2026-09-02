package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file covers argument handling that runs BEFORE any request — no
// server involved. HTTP-level behaviour (request shape) is covered in
// internal/api, where Client.BaseURL is settable directly; end-to-end
// behaviour against a fake server (e.g. --all's page walk, in
// agent_pagination_test.go) goes through the cmd-layer clientForTest hook
// in root.go, same as every other command's httptest-backed test.

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

// readAgentFlowFile's two failure branches. Only the missing-file one was covered;
// invalid JSON is the likelier mistake in practice (a hand-edited flow file), and it
// must surface as BAD_INPUT naming the path, not as an opaque API-shaped error.
func TestReadAgentFlowFile_invalidJSONIsBadInputNamingThePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.json")
	if err := os.WriteFile(path, []byte(`{"name": "broken",`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := readAgentFlowFile(path)
	if err == nil {
		t.Fatalf("expected an error for truncated JSON, got %+v", f)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error must name the offending file, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error must say the JSON is the problem, got %q", err.Error())
	}
}

func TestReadAgentFlowFile_validFileParses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.json")
	body := `{"name":"Support","nodes":[{"type":"send_message"}],"connections":[]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := readAgentFlowFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Name != "Support" || len(f.Nodes) != 1 {
		t.Errorf("parsed = %+v, want name Support with 1 node", f)
	}
}
