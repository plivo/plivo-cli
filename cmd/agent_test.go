package cmd

import (
	"strings"
	"testing"
)

// TestAgentsBaseURLOverride_flagChangesRequestHost proves --api-url actually
// redirects traffic — the core contract of the override — by running a real
// command against a local httptest server instead of prod, and asserts the
// stderr notice appears while stdout stays clean.
func TestAgentsBaseURLOverride_flagChangesRequestHost(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startCapturingHTTPServer(t, 200, `{"api_id":"x","meta":{"limit":20,"offset":0,"total_count":0},"objects":[]}`)

	err, stdout, stderr := execCmd(t, "--api-url", srv.URL, "agents", "list", "-o", "json")
	if err != nil {
		t.Fatalf("agents list --api-url %s: unexpected err: %v (stderr=%s)", srv.URL, err, stderr)
	}
	got := hits()
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 request to the overridden host, got %d: %v", len(got), got)
	}
	if !strings.HasPrefix(got[0], "GET /v1/Account/") || !strings.HasSuffix(got[0], "/Agent/") {
		t.Errorf("request = %q, want a GET to .../Agent/", got[0])
	}
	if !strings.Contains(stderr, "non-default API base URL") {
		t.Errorf("stderr should note the non-default base URL, got: %q", stderr)
	}
	if strings.Contains(stdout, "non-default") {
		t.Errorf("the override notice must never leak onto stdout (must stay pipe-clean for jq): %q", stdout)
	}
	if !strings.Contains(stdout, `"data"`) {
		t.Errorf("stdout should still carry the normal JSON envelope: %q", stdout)
	}
}

// TestAgentsBaseURLOverride_viaEnv proves PLIVO_API_URL alone (no flag) also
// redirects traffic, covering the env branch of the precedence chain
// end-to-end (root_test.go covers the pure resolution logic in isolation).
func TestAgentsBaseURLOverride_viaEnv(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startCapturingHTTPServer(t, 200, `{"api_id":"x","schema_version":"1.0.0","objects":[]}`)
	t.Setenv("PLIVO_API_URL", srv.URL)

	err, _, stderr := execCmd(t, "agents", "nodes", "list")
	if err != nil {
		t.Fatalf("agents nodes list via PLIVO_API_URL: unexpected err: %v (stderr=%s)", err, stderr)
	}
	got := hits()
	if len(got) != 1 || !strings.HasSuffix(got[0], "/AgentNode/") {
		t.Errorf("expected 1 GET to .../AgentNode/ via PLIVO_API_URL, got %v", got)
	}
	if !strings.Contains(stderr, "PLIVO_API_URL") {
		t.Errorf("stderr notice should name PLIVO_API_URL as the source, got: %q", stderr)
	}
}

// TestAPIURLOverride_rejectsMalformedValue_endToEnd exercises the same
// rejection as root_test.go's unit test, but through a full command
// invocation, proving a bad --api-url surfaces as BAD_INPUT before any
// network call — not a silent fallback to production.
func TestAPIURLOverride_rejectsMalformedValue_endToEnd(t *testing.T) {
	setFakeCreds(t)
	err, _, _ := execCmd(t, "--api-url", "not-a-url", "agents", "list")
	if err == nil {
		t.Fatal("expected an error for a malformed --api-url, got nil")
	}
	if !strings.Contains(err.Error(), "BAD_INPUT") {
		t.Errorf("expected BAD_INPUT, got: %v", err)
	}
}

// TestAgentsCreate_showsStoredNameOnCollisionRename pins the collision-rename
// UX: the backend may silently store a different name than requested (it
// appends " 1", " 2", ... on a collision), and the CLI must call that out
// rather than only echoing back what the user typed.
func TestAgentsCreate_showsStoredNameOnCollisionRename(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startCapturingHTTPServer(t, 201, `{"api_id":"x","message":"created","agent_id":"AGENT1","name":"My Agent 1"}`)

	err, stdout, stderr := execCmd(t, "--api-url", srv.URL, "agents", "create", "--name", "My Agent")
	if err != nil {
		t.Fatalf("agents create: unexpected err: %v (stderr=%s)", err, stderr)
	}
	got := hits()
	if len(got) != 1 || !strings.HasSuffix(got[0], "/Agent/") || !strings.HasPrefix(got[0], "POST") {
		t.Errorf("expected 1 POST to .../Agent/, got %v", got)
	}
	if !strings.Contains(stderr, `"My Agent"`) || !strings.Contains(stderr, `"My Agent 1"`) {
		t.Errorf("stderr should call out requested-vs-stored name on a rename, got: %q", stderr)
	}
	if !strings.Contains(stdout, "My Agent 1") {
		t.Errorf("stdout should show the stored name, got: %q", stdout)
	}
}

// TestAgentsCreate_requiresNameFromFlagOrFile confirms the conditional
// requirement (name is not a plain cobra-required flag, since --file can
// supply it instead) is still enforced before any HTTP call.
func TestAgentsCreate_requiresNameFromFlagOrFile(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startCapturingHTTPServer(t, 201, `{}`)

	err, _, _ := execCmd(t, "--api-url", srv.URL, "agents", "create")
	if err == nil {
		t.Fatal("agents create with neither --name nor --file: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "BAD_INPUT") {
		t.Errorf("expected BAD_INPUT, got: %v", err)
	}
	if len(hits()) != 0 {
		t.Errorf("should not have made an HTTP call, got %d hits", len(hits()))
	}
}

// TestAgentsGet_rendersFullDetail exercises the get-one path end-to-end,
// including the nested nodes/connections arrays.
func TestAgentsGet_rendersFullDetail(t *testing.T) {
	setFakeCreds(t)
	body := `{"api_id":"x","id":"AGENT1","name":"n","description":"d","state":"DRAFT","version":1,
	 "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z",
	 "nodes":[{"id":"n1","type":"start","config":{}}],
	 "connections":[{"source":"n1.http","target":"n1.Input"}]}`
	srv, hits := startCapturingHTTPServer(t, 200, body)

	err, stdout, _ := execCmd(t, "--api-url", srv.URL, "agents", "get", "AGENT1")
	if err != nil {
		t.Fatalf("agents get: unexpected err: %v", err)
	}
	got := hits()
	if len(got) != 1 || !strings.HasSuffix(got[0], "/Agent/AGENT1/") {
		t.Errorf("expected a GET to .../Agent/AGENT1/, got %v", got)
	}
	if !strings.Contains(stdout, "AGENT1") || !strings.Contains(stdout, "DRAFT") {
		t.Errorf("table output should include id and state, got: %q", stdout)
	}
}

// TestAgentsDelete_deletesAgainstOverriddenHost proves the destructive verb
// also honours the override (it's gated by --yes separately in safety_test.go).
func TestAgentsDelete_deletesAgainstOverriddenHost(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startCapturingHTTPServer(t, 204, "")

	err, _, stderr := execCmd(t, "--api-url", srv.URL, "agents", "delete", "AGENT1", "--yes")
	if err != nil {
		t.Fatalf("agents delete --yes: unexpected err: %v", err)
	}
	got := hits()
	if len(got) != 1 || !strings.HasPrefix(got[0], "DELETE") || !strings.HasSuffix(got[0], "/Agent/AGENT1/") {
		t.Errorf("expected 1 DELETE to .../Agent/AGENT1/, got %v", got)
	}
	if !strings.Contains(stderr, "AGENT1") {
		t.Errorf("stderr should confirm the deleted id, got: %q", stderr)
	}
}
