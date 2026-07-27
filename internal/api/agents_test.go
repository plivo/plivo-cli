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
