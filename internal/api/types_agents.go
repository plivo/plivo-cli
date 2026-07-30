package api

import "encoding/json"

// Agent — /Account/{id}/AgentFlow/ (AI agent flow definitions: voice/chat/message
// agents built from a node graph). The same struct backs both the list
// envelope's Objects (id, name, description, state, version, created_at,
// updated_at only) and the single-get response (which additionally carries
// APIID, Nodes, and Connections) — mirroring how Application is reused
// across ApplicationList and the single-get response.
type Agent struct {
	APIID       string                 `json:"api_id,omitempty"`
	ID          string                 `json:"agent_uuid"`
	ResourceURI string                 `json:"resource_uri,omitempty"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	State       string                 `json:"state,omitempty"`
	FlowType    string                 `json:"flow_type,omitempty"`
	Version     int                    `json:"version,omitempty"`
	CreatedAt   string                 `json:"created_at,omitempty"`
	UpdatedAt   string                 `json:"updated_at,omitempty"`
	Nodes       []AgentGraphNode       `json:"nodes,omitempty"`
	Connections []AgentGraphConnection `json:"connections,omitempty"`
}

type AgentList struct {
	APIID   string   `json:"api_id"`
	Meta    ListMeta `json:"meta"`
	Objects []Agent  `json:"objects"`
}

// AgentGraphNode is one node in an agent's flow graph (the `nodes` array on
// Agent). Config is intentionally raw JSON rather than a typed struct: its
// shape is entirely node-type dependent (see AgentFlowNode.JSONSchema for the
// per-type schema) and re-serialising it through a fixed Go struct would
// risk dropping or reordering fields the backend expects verbatim on
// update. Every node must be referenced by at least one AgentGraphConnection
// or the API rejects the whole create/update with 422, naming the orphaned
// node ids.
type AgentGraphNode struct {
	ID          string          `json:"id,omitempty"`
	Name        string          `json:"name,omitempty"`
	Type        string          `json:"type"`
	Description string          `json:"description,omitempty"`
	Left        float64         `json:"left,omitempty"`
	Top         float64         `json:"top,omitempty"`
	Config      json.RawMessage `json:"config,omitempty"`
}

// AgentGraphConnection is one edge in an agent's flow graph. Source and
// Target must be "<node_id>.<handle>" (e.g. "abc123.success") — a bare node
// id with no handle is rejected by the API with 400.
type AgentGraphConnection struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// AgentCreateResponse is the response from POST Agent/. Name is the STORED
// name, which may differ from what was requested: the backend silently
// appends " 1", " 2", etc. on a name collision. Always show this back to
// the user rather than echoing what they typed.
type AgentCreateResponse struct {
	APIID       string `json:"api_id"`
	Message     string `json:"message,omitempty"`
	AgentID     string `json:"agent_uuid"`
	Name        string `json:"name"`
	ResourceURI string `json:"resource_uri,omitempty"`
}

// AgentRun — GET Agent/{id}/Run/ (list) and GET Agent/{id}/Run/{run_id}/
// (detail). The same struct backs both; APIID/Logs/GoalMetrics are populated
// on the detail response only. Logs and GoalMetrics stay raw JSON: log
// entries vary in shape by event type (a NODE_EXECUTED entry carries
// current_node/node_config; a message entry carries sender/text/metadata
// instead), so typing them as a fixed struct would silently drop fields
// instead of just not-labeling them.
type AgentRun struct {
	APIID        string            `json:"api_id,omitempty"`
	RunID        string            `json:"run_uuid"`
	AgentID      string            `json:"agent_uuid,omitempty"`
	ResourceURI  string            `json:"resource_uri,omitempty"`
	Status       string            `json:"status,omitempty"`
	StartedAt    string            `json:"started_at,omitempty"`
	EndedAt      string            `json:"ended_at,omitempty"`
	Duration     float64           `json:"duration,omitempty"`
	IsPlayground bool              `json:"is_playground,omitempty"`
	Logs         []json.RawMessage `json:"logs,omitempty"`
	GoalMetrics  json.RawMessage   `json:"goal_metrics,omitempty"`
}

type AgentRunList struct {
	APIID   string     `json:"api_id"`
	Meta    ListMeta   `json:"meta"`
	Objects []AgentRun `json:"objects"`
}

// AgentFlowNode — GET AgentFlowNode/ (catalogue list) and GET AgentFlowNode/{node_type}/
// (single node's JSON Schema). The same struct backs both; APIID,
// SchemaVersion, JSONSchema, Examples, and Coverage are populated on the
// detail response only (the catalogue list omits them for size).
type AgentFlowNode struct {
	APIID         string             `json:"api_id,omitempty"`
	SchemaVersion string             `json:"schema_version,omitempty"`
	NodeType      string             `json:"node_type"`
	Title         string             `json:"title,omitempty"`
	Category      string             `json:"category,omitempty"`
	Description   string             `json:"description,omitempty"`
	Usecase       string             `json:"usecase,omitempty"`
	OutputStates  []AgentOutputState `json:"output_states,omitempty"`
	// Detail-only fields.
	JSONSchema json.RawMessage        `json:"json_schema,omitempty"`
	Examples   []json.RawMessage      `json:"examples,omitempty"`
	Coverage   *AgentFlowNodeCoverage `json:"x-plivo-coverage,omitempty"`
}

// AgentOutputState is one possible exit branch of a node (e.g. "success" /
// "failed" / "timeout") — the set of valid handles a connection's Source can
// reference for that node.
type AgentOutputState struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Selected bool   `json:"selected,omitempty"`
}

// AgentFlowNodeCoverage reports how faithfully JSONSchema captures the node's
// real server-side validation (some rules — conditional-required fields,
// secret references — aren't expressible in static JSON Schema and are
// listed here instead of silently missing).
type AgentFlowNodeCoverage struct {
	DroppedCount  int      `json:"dropped_count,omitempty"`
	DegradedCount int      `json:"degraded_count,omitempty"`
	DroppedRules  []string `json:"dropped_rules,omitempty"`
	DegradedRules []string `json:"degraded_rules,omitempty"`
}

// AgentFlowNodeList is the GET AgentFlowNode/ catalogue envelope. Unlike the other
// list resources in this package it carries no ListMeta — the catalogue is
// small and unpaginated.
type AgentFlowNodeList struct {
	APIID         string          `json:"api_id"`
	SchemaVersion string          `json:"schema_version,omitempty"`
	Objects       []AgentFlowNode `json:"objects"`
}
