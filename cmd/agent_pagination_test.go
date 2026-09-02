package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
)

// --all's whole history is claiming to auto-paginate and not doing it (see
// registerAllFlag in root.go). These tests prove the opposite for the two
// agents list commands: WITHOUT --all only the first page is fetched, and
// WITH --all every page is fetched and merged — in both the typed rows
// (table mode) and the raw JSON envelope (-o json, via accumulateRawObjects).

// agentsPageServer replies with byOffset[offset] (falling back to
// byOffset["0"] for a request with no offset param) and wires clientForTest
// so the command layer reaches it — the same hook every other command's
// httptest-backed test uses (see pointCommandsAtTestServer in
// number_json_test.go). Returns a getter for every "offset" value seen, in
// request order, so callers can assert exactly how many requests fired.
func agentsPageServer(t *testing.T, byOffset map[string]string) func() []string {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		mu.Lock()
		seen = append(seen, offset)
		mu.Unlock()
		body, ok := byOffset[offset]
		if !ok {
			body = byOffset["0"]
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{
		BaseURL:   srv.URL,
		AuthID:    "MAFAKEFORTEST",
		AuthToken: "fake-token",
		HTTP:      &http.Client{},
	}
	t.Cleanup(func() { clientForTest = nil })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(seen))
		copy(out, seen)
		return out
	}
}

// decodeDataObjects reads the {"data": {"objects": [...]}} envelope every
// agents list command now renders in JSON mode (output.JSONRaw off the
// upstream bytes).
func decodeDataObjects(t *testing.T, stdout string) []map[string]any {
	t.Helper()
	var out struct {
		Data struct {
			Objects []map[string]any `json:"objects"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	return out.Data.Objects
}

const agentListPage1 = `{"api_id":"envelope-a","meta":{"limit":1,"offset":0,"total_count":2,"next":"/v1/Account/MAFAKEFORTEST/AgentFlow/?limit=1&offset=1","previous":null},"objects":[{"agent_uuid":"agent-1","name":"Agent One","state":"DRAFT","flow_type":"incoming_sms","version":1,"created_at":"2026-01-01 00:00:00.000000+00:00","updated_at":"2026-01-01 00:00:00.000000+00:00","resource_uri":"/v1/Account/MAFAKEFORTEST/AgentFlow/agent-1/"}]}`

const agentListPage2 = `{"api_id":"envelope-a","meta":{"limit":1,"offset":1,"total_count":2,"next":null,"previous":"/v1/Account/MAFAKEFORTEST/AgentFlow/?limit=1&offset=0"},"objects":[{"agent_uuid":"agent-2","name":"Agent Two","state":"ACTIVE","flow_type":"incoming_sms","version":1,"created_at":"2026-01-02 00:00:00.000000+00:00","updated_at":"2026-01-02 00:00:00.000000+00:00","resource_uri":"/v1/Account/MAFAKEFORTEST/AgentFlow/agent-2/"}]}`

func TestAgentsList_withoutAll_fetchesOnlyFirstPage(t *testing.T) {
	setFakeCreds(t)
	hits := agentsPageServer(t, map[string]string{"0": agentListPage1, "1": agentListPage2})

	err, stdout, _ := execCmd(t, "agents", "list", "--limit", "1", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v (stdout: %s)", err, stdout)
	}
	if got := hits(); len(got) != 1 {
		t.Fatalf("expected exactly 1 request without --all, got %d: %v", len(got), got)
	}
	if objs := decodeDataObjects(t, stdout); len(objs) != 1 {
		t.Fatalf("expected 1 object without --all, got %d (stdout: %s)", len(objs), stdout)
	}
}

func TestAgentsList_withAll_walksEveryPage(t *testing.T) {
	setFakeCreds(t)
	hits := agentsPageServer(t, map[string]string{"0": agentListPage1, "1": agentListPage2})

	err, stdout, _ := execCmd(t, "agents", "list", "--limit", "1", "--all", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v (stdout: %s)", err, stdout)
	}
	if got := hits(); len(got) != 2 {
		t.Fatalf("expected 2 requests with --all (page 1 + page 2), got %d: %v", len(got), got)
	}

	objs := decodeDataObjects(t, stdout)
	if len(objs) != 2 {
		t.Fatalf("expected 2 objects with --all (both pages merged), got %d (stdout: %s)", len(objs), stdout)
	}
	ids := map[string]bool{}
	for _, o := range objs {
		if v, ok := o["agent_uuid"].(string); ok {
			ids[v] = true
		}
	}
	if !ids["agent-1"] || !ids["agent-2"] {
		t.Errorf("expected both agent-1 and agent-2 in the merged -o json output, got %v", objs)
	}
}

// Table mode uses the typed, accumulated resp.Objects rather than the raw
// bytes — confirm --all's page walk feeds that path too, not just JSON.
func TestAgentsList_withAll_tableModeShowsEveryRow(t *testing.T) {
	setFakeCreds(t)
	agentsPageServer(t, map[string]string{"0": agentListPage1, "1": agentListPage2})

	err, stdout, _ := execCmd(t, "agents", "list", "--limit", "1", "--all", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v (stdout: %s)", err, stdout)
	}
	for _, want := range []string{"agent-1", "agent-2"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table output missing %q, got: %s", want, stdout)
		}
	}
}

const agentRunListPage1 = `{"api_id":"envelope-b","meta":{"limit":1,"offset":0,"total_count":2,"next":"/v1/Account/MAFAKEFORTEST/AgentFlow/agent-x/Run/?limit=1&offset=1","previous":null},"objects":[{"run_uuid":"run-1","agent_uuid":"agent-x","status":"completed","started_at":"2026-01-01 00:00:00.000000+00:00","ended_at":"2026-01-01 00:01:00.000000+00:00","is_playground":false,"resource_uri":"/v1/Account/MAFAKEFORTEST/AgentFlow/agent-x/Run/run-1/"}]}`

const agentRunListPage2 = `{"api_id":"envelope-b","meta":{"limit":1,"offset":1,"total_count":2,"next":null,"previous":"/v1/Account/MAFAKEFORTEST/AgentFlow/agent-x/Run/?limit=1&offset=0"},"objects":[{"run_uuid":"run-2","agent_uuid":"agent-x","status":"failed","started_at":"2026-01-02 00:00:00.000000+00:00","ended_at":"2026-01-02 00:01:00.000000+00:00","is_playground":true,"resource_uri":"/v1/Account/MAFAKEFORTEST/AgentFlow/agent-x/Run/run-2/"}]}`

func TestAgentsRunsList_withoutAll_fetchesOnlyFirstPage(t *testing.T) {
	setFakeCreds(t)
	hits := agentsPageServer(t, map[string]string{"0": agentRunListPage1, "1": agentRunListPage2})

	err, stdout, _ := execCmd(t, "agents", "runs", "list", "agent-x", "--limit", "1", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v (stdout: %s)", err, stdout)
	}
	if got := hits(); len(got) != 1 {
		t.Fatalf("expected exactly 1 request without --all, got %d: %v", len(got), got)
	}
	if objs := decodeDataObjects(t, stdout); len(objs) != 1 {
		t.Fatalf("expected 1 object without --all, got %d (stdout: %s)", len(objs), stdout)
	}
}

func TestAgentsRunsList_withAll_walksEveryPage(t *testing.T) {
	setFakeCreds(t)
	hits := agentsPageServer(t, map[string]string{"0": agentRunListPage1, "1": agentRunListPage2})

	err, stdout, _ := execCmd(t, "agents", "runs", "list", "agent-x", "--limit", "1", "--all", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v (stdout: %s)", err, stdout)
	}
	if got := hits(); len(got) != 2 {
		t.Fatalf("expected 2 requests with --all (page 1 + page 2), got %d: %v", len(got), got)
	}

	objs := decodeDataObjects(t, stdout)
	if len(objs) != 2 {
		t.Fatalf("expected 2 objects with --all (both pages merged), got %d (stdout: %s)", len(objs), stdout)
	}
	ids := map[string]bool{}
	for _, o := range objs {
		if v, ok := o["run_uuid"].(string); ok {
			ids[v] = true
		}
	}
	if !ids["run-1"] || !ids["run-2"] {
		t.Errorf("expected both run-1 and run-2 in the merged -o json output, got %v", objs)
	}
}
