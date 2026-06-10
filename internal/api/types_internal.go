//go:build internal

package api

// Agent mirrors the subset of the workflow-config service's model that the
// CLI surfaces. The full workflow lives in nested fields not exposed here.
type Agent struct {
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

// AgentListResponse is the shape returned by the workflow-config service's
// list endpoint. Different routes return slightly different envelopes; we
// try common keys.
type AgentListResponse struct {
	Objects []Agent `json:"objects,omitempty"`
}
