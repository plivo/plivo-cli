//go:build internal

package api

// PhloAgent mirrors the subset of the (older, PHLO-based) workflow-config
// service's model that a never-shipped internal build once surfaced. The
// full workflow lives in nested fields not exposed here.
//
// Unrelated to the public, node-graph-based api.Agent (types_agents.go) —
// renamed from the original "Agent" to free up that name once the public
// Agents API shipped; this type has no callers in this build.
type PhloAgent struct {
	UUID         string `json:"uuid"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	State        string `json:"state,omitempty"`
	PhloType     string `json:"phlo_type,omitempty"`
	IsDefault    bool   `json:"is_default,omitempty"`
	OutboundPhlo bool   `json:"outbound_phlo,omitempty"`
	Enabled      bool   `json:"enabled,omitempty"`
	Version      int    `json:"version,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
}

// PhloAgentListResponse is the shape returned by the workflow-config
// service's list endpoint. Different routes return slightly different
// envelopes; we try common keys.
type PhloAgentListResponse struct {
	Objects []PhloAgent `json:"objects,omitempty"`
}
