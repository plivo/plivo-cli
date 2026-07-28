package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		_, _ = w.Write([]byte(`{"api_id":"a","message":"created","agent_id":"id-1","name":"Support 1"}`))
	})
	defer done()

	var out AgentCreateResponse
	apiErr, err := c.Do(http.MethodPost, c.AccountURL("Agent"), map[string]any{"name": "Support"}, nil, &out)
	if err != nil || apiErr != nil {
		t.Fatalf("create: err=%v apiErr=%v", err, apiErr)
	}
	if out.Name != "Support 1" {
		t.Errorf("stored name must be surfaced verbatim, got %q want %q", out.Name, "Support 1")
	}
	if out.AgentID != "id-1" {
		t.Errorf("agent_id = %q, want id-1", out.AgentID)
	}
}

func TestAgentURLs_matchThePublicContract(t *testing.T) {
	c := New("MAAUTHID000000000000", "token", time.Second)
	c.BaseURL = "https://example.test"
	cases := map[string]string{
		c.AccountURL("Agent"):                     "https://example.test/v1/Account/MAAUTHID000000000000/Agent/",
		c.AccountURL("Agent", "id-1"):             "https://example.test/v1/Account/MAAUTHID000000000000/Agent/id-1/",
		c.AccountURL("Agent", "id-1", "Run"):      "https://example.test/v1/Account/MAAUTHID000000000000/Agent/id-1/Run/",
		c.AccountURL("AgentNode"):                 "https://example.test/v1/Account/MAAUTHID000000000000/AgentNode/",
		c.AccountURL("AgentNode", "http_request"): "https://example.test/v1/Account/MAAUTHID000000000000/AgentNode/http_request/",
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

	apiErr, err := c.Do(http.MethodPost, c.AccountURL("Agent"), map[string]any{"name": "x"}, nil, nil)
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

	apiErr, err := c.Do(http.MethodDelete, c.AccountURL("Agent", "id-1"), nil, nil, nil)
	if err != nil || apiErr != nil {
		t.Fatalf("204 must not be treated as an error: err=%v apiErr=%v", err, apiErr)
	}
}

// The resource key is `agent_id` in every representation -- list rows, detail,
// and create -- matching Application's `app_id` and Endpoint's `endpoint_id`.
// It shipped as a bare `id` on list/detail while create already said
// `agent_id`, so `plivo agents list` printed an empty ID column against a
// correct server. Nothing caught it: no test decoded a real list payload.
func TestAgentList_decodesAgentIDFromTheWire(t *testing.T) {
	c, done := agentsClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"api_id":"a",
		  "meta":{"limit":20,"offset":0,"total_count":1},
		  "objects":[{"agent_id":"380848ff-c424-49be-9dc8-8e068dbf3ba4",
		              "name":"E2E real SMS run","state":"ACTIVE",
		              "flow_type":"api_request","version":1}]}`))
	})
	defer done()

	var out AgentList
	if apiErr, err := c.Do(http.MethodGet, c.AccountURL("Agent"), nil, nil, &out); err != nil || apiErr != nil {
		t.Fatalf("list: err=%v apiErr=%v", err, apiErr)
	}
	if len(out.Objects) != 1 {
		t.Fatalf("objects = %d, want 1", len(out.Objects))
	}
	if got := out.Objects[0].ID; got != "380848ff-c424-49be-9dc8-8e068dbf3ba4" {
		t.Errorf("ID = %q -- Agent.ID must map to the agent_id wire key", got)
	}
	if got := out.Objects[0].FlowType; got != "api_request" {
		t.Errorf("FlowType = %q, want api_request -- drives which console route can open the agent", got)
	}
}

// A bare `id` must NOT populate Agent.ID: if it did, this fix could regress
// server-side and the CLI would keep working, hiding the contract break.
func TestAgent_bareIDKeyIsNotAccepted(t *testing.T) {
	var a Agent
	if err := json.Unmarshal([]byte(`{"id":"legacy-key","name":"x"}`), &a); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if a.ID != "" {
		t.Errorf("ID = %q from a bare `id` key; the contract is agent_id only", a.ID)
	}
}
