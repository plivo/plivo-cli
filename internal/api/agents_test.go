package api

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The agents commands cannot be redirected at runtime — the CLI always targets
// the production gateway — so HTTP-shape coverage lives here, where a Client
// can be pointed at a test server directly.

func agentsClient(t *testing.T, h http.HandlerFunc) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(h)
	c := New("MAAUTHID000000000000", "token", 5*time.Second)
	c.BaseURL = srv.URL
	return c, srv.Close
}

// A create whose requested name collides is stored under a DIFFERENT name --
// the backend appends " 1"/" 2". The response carries the STORED name, and
// callers must surface that rather than echoing what they asked for.
func TestAgentCreate_responseCarriesStoredName(t *testing.T) {
	c, done := agentsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"api_id":"a","message":"agent created","agent_uuid":"id-1","name":"Support 1"}`))
	})
	defer done()

	var out AgentCreateResponse
	apiErr, err := c.Do(http.MethodPost, c.AccountURL("AgentFlow"), map[string]any{"name": "Support"}, nil, &out)
	if err != nil || apiErr != nil {
		t.Fatalf("create: err=%v apiErr=%v", err, apiErr)
	}
	if out.Name != "Support 1" {
		t.Errorf("stored name must be surfaced verbatim, got %q want %q", out.Name, "Support 1")
	}
	if out.AgentID != "id-1" {
		t.Errorf("agent_uuid = %q, want id-1", out.AgentID)
	}
}

func TestAgentURLs_matchThePublicContract(t *testing.T) {
	c := New("MAAUTHID000000000000", "token", time.Second)
	c.BaseURL = "https://example.test"
	cases := map[string]string{
		c.AccountURL("AgentFlow"):                     "https://example.test/v1/Account/MAAUTHID000000000000/AgentFlow/",
		c.AccountURL("AgentFlow", "id-1"):             "https://example.test/v1/Account/MAAUTHID000000000000/AgentFlow/id-1/",
		c.AccountURL("AgentFlow", "id-1", "Run"):      "https://example.test/v1/Account/MAAUTHID000000000000/AgentFlow/id-1/Run/",
		c.AccountURL("AgentFlowNode"):                 "https://example.test/v1/Account/MAAUTHID000000000000/AgentFlowNode/",
		c.AccountURL("AgentFlowNode", "http_request"): "https://example.test/v1/Account/MAAUTHID000000000000/AgentFlowNode/http_request/",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("AccountURL = %q, want %q", got, want)
		}
	}
}

// A flow whose nodes are not referenced by any connection is rejected 422 with
// the offending ids named. That message must reach the user intact, not be
// flattened into a generic failure.
func TestAgentCreate_surfacesValidationMessageVerbatim(t *testing.T) {
	const msg = "These nodes are not referenced by any connection and would not be saved: s1, r1."
	c, done := agentsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"api_id": "a", "error": msg})
	})
	defer done()

	apiErr, err := c.Do(http.MethodPost, c.AccountURL("AgentFlow"), map[string]any{"name": "x"}, nil, nil)
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	if apiErr == nil {
		t.Fatal("expected an APIError for a 422")
	}
	if apiErr.Message != msg {
		t.Errorf("validation message must survive intact:\n got %q\nwant %q", apiErr.Message, msg)
	}
}

func TestAgentDelete_acceptsEmpty204(t *testing.T) {
	c, done := agentsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	defer done()

	apiErr, err := c.Do(http.MethodDelete, c.AccountURL("AgentFlow", "id-1"), nil, nil, nil)
	if err != nil || apiErr != nil {
		t.Fatalf("204 must not be treated as an error: err=%v apiErr=%v", err, apiErr)
	}
}

// The resource key follows Plivo's rule: the suffix tracks the VALUE's shape, so
// uuid-valued ids get `_uuid` (message_uuid, profile_uuid) and numeric ones get
// `_id` (app_id, endpoint_id). Ours are uuids.
//
// This has now broken twice. First the struct said `id` while the API said
// `agent_id`; then the API moved to `agent_uuid` while the struct still said
// `agent_id`. Both times `plivo agents list` printed an empty ID column against a
// perfectly correct server, and both times the mocked tests passed because they
// had been updated to assert whatever the struct happened to say.
//
// So this fixture is a REAL captured response from the live API, not a
// hand-written literal. If the wire contract moves again, re-capture it and the
// mismatch shows up here instead of in a user's terminal.
//
//go:embed testdata/agent_list_response.json
var realAgentListResponse []byte

func TestAgentList_decodesARealCapturedResponse(t *testing.T) {
	c, done := agentsClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(realAgentListResponse)
	})
	defer done()

	var out AgentList
	if apiErr, err := c.Do(http.MethodGet, c.AccountURL("AgentFlow"), nil, nil, &out); err != nil || apiErr != nil {
		t.Fatalf("list: err=%v apiErr=%v", err, apiErr)
	}
	if len(out.Objects) != 1 {
		t.Fatalf("objects = %d, want 1", len(out.Objects))
	}
	a := out.Objects[0]
	if a.ID == "" {
		t.Error("ID is empty -- Agent.ID does not map to the wire's id key")
	}
	if a.Name == "" {
		t.Error("Name is empty")
	}
	if a.FlowType == "" {
		t.Error("FlowType is empty -- it decides which console route can open the agent")
	}
	// Assert the SHAPE, not just presence. A non-empty check is what let the
	// Agent -> AgentFlow rename drift past this fixture unnoticed: any string
	// decodes into ResourceURI, so "not empty" proves nothing about the contract.
	if !strings.Contains(a.ResourceURI, "/AgentFlow/") {
		t.Errorf("ResourceURI = %q, want it to contain %q -- the public resource segment moved, "+
			"so re-capture this fixture", a.ResourceURI, "/AgentFlow/")
	}
	if !strings.HasSuffix(a.ResourceURI, "/") {
		t.Errorf("ResourceURI = %q, want a trailing slash (Plivo publishes them that way)", a.ResourceURI)
	}
	if !strings.Contains(out.Meta.Next, "/AgentFlow/") {
		t.Errorf("meta.next = %q, want it to contain %q", out.Meta.Next, "/AgentFlow/")
	}
	if out.Meta.TotalCount == 0 {
		t.Error("meta.total_count did not decode")
	}
}

// Neither a bare `id` nor the old `agent_id` may populate Agent.ID: if either
// did, a server-side regression could hide behind a CLI that still worked.
func TestAgent_onlyAgentUUIDPopulatesID(t *testing.T) {
	for _, body := range []string{
		`{"id":"legacy","name":"x"}`,
		`{"agent_id":"older","name":"x"}`,
	} {
		var a Agent
		if err := json.Unmarshal([]byte(body), &a); err != nil {
			t.Fatalf("unmarshal %s: %v", body, err)
		}
		if a.ID != "" {
			t.Errorf("ID = %q from %s; the contract is agent_uuid only", a.ID, body)
		}
	}
}

// An explicit left/top of 0 must reach the wire. The server checks `is None`
// specifically so a deliberate 0 is honoured, and the fields are *float64 for
// exactly that reason -- a plain float64 with omitempty cannot tell "user wrote 0"
// from "unset" and silently drops it. Nothing else guards this: the captured
// fixture covers agent_uuid/resource_uri drift, not marshalling, so a future
// "simplify these back to float64" would reintroduce the bug invisibly.
func TestAgentGraphNode_explicitZeroPositionSurvivesMarshalling(t *testing.T) {
	zero := 0.0
	body, err := json.Marshal(AgentGraphNode{Type: "send_message", Left: &zero, Top: &zero})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(body)
	for _, want := range []string{`"left":0`, `"top":0`} {
		if !strings.Contains(got, want) {
			t.Errorf("marshalled node = %s, want it to contain %s", got, want)
		}
	}
}

func TestAgentGraphNode_absentPositionIsOmitted(t *testing.T) {
	body, err := json.Marshal(AgentGraphNode{Type: "send_message"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(body)
	for _, unwanted := range []string{"left", "top"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("marshalled node = %s, must omit %q so the server assigns a lane", got, unwanted)
		}
	}
}
