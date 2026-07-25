package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// agentCmd manages AI agent flows (voice/chat/message agents built from a
// node graph) — the public Agents API, same shape everywhere: paths are
// /v1/Account/{auth_id}/Agent/... via client.AccountURL. Subaccount
// credentials are not supported for this API and get a 403.
var agentCmd = &cobra.Command{
	Use:     "agents",
	Aliases: []string{"agent"},
	Short:   "Manage AI agent flows (voice/chat/message agents)",
	Long: `Manage AI agent flows: node-graph-based voice/chat/message agents.

An agent is a graph of typed nodes (see 'agents nodes list' for the
catalogue) wired together with connections. Build or edit the graph with
--file, a JSON document shaped like:

  {"name": "...", "description": "...", "nodes": [...], "connections": [...]}

Every node must be referenced by at least one connection or the API
rejects the request with 422, naming the orphaned node id(s). Connections
reference "<node_id>.<handle>" (e.g. "start-1.message") — a bare node id
with no handle is a 400.

Note: subaccount credentials are not supported for this API (403) — use
master account credentials.`,
}

var (
	agentCreateName        string
	agentCreateDescription string
	agentCreateFile        string
)

var agentCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new agent flow",
	Long: `Create a new agent flow.

--name is required unless --file supplies a top-level "name". Flags always
win over the file's fields when both are set. The server may store a
different name than requested if it collides with an existing one (it
silently appends " 1", " 2", etc.) — the stored name is always shown.`,
	Example: `  plivo agents create --name "Order Status Agent"
  plivo agents create --name "Order Status Agent" --file flow.json
  plivo agents create --file flow.json   # flow.json supplies "name" itself`,
	RunE: runAgentCreate,
}

var (
	agentListLimit  int
	agentListOffset int
	agentListName   string
	agentListState  string
)

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List agent flows",
	RunE:  runAgentList,
}

var agentGetCmd = &cobra.Command{
	Use:   "get <agent_id>",
	Short: "Get an agent flow's full definition (fields, nodes, connections)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentGet,
}

var (
	agentUpdateName        string
	agentUpdateDescription string
	agentUpdateFile        string
)

var agentUpdateCmd = &cobra.Command{
	Use:   "update <agent_id>",
	Short: "Rename an agent flow, or replace its graph",
	Long: `Update an agent flow.

The API accepts either a pure rename (--name alone) or a full graph
replace (--file, or --description together with a graph) — mixing
--description with --name but no graph is rejected by the server with a
clear 400. Pass --file for any change beyond a plain rename.`,
	Example: `  plivo agents update abc123 --name "New Name"
  plivo agents update abc123 --file flow.json`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentUpdate,
}

var agentDeleteCmd = &cobra.Command{
	Use:   "delete <agent_id>",
	Short: "Delete an agent flow (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentDelete,
}

func init() {
	agentCreateCmd.Flags().StringVar(&agentCreateName, "name", "", "agent name (required unless --file supplies one)")
	agentCreateCmd.Flags().StringVar(&agentCreateDescription, "description", "", "agent description")
	agentCreateCmd.Flags().StringVar(&agentCreateFile, "file", "", "JSON file with the flow graph ({name, description, nodes, connections}); flags win over its fields")

	agentListCmd.Flags().IntVar(&agentListLimit, "limit", 20, "results per page (max 20)")
	agentListCmd.Flags().IntVar(&agentListOffset, "offset", 0, "pagination offset")
	agentListCmd.Flags().StringVar(&agentListName, "name", "", "filter by name (substring match)")
	agentListCmd.Flags().StringVar(&agentListState, "state", "", "filter by state (e.g. DRAFT, ACTIVE)")

	agentUpdateCmd.Flags().StringVar(&agentUpdateName, "name", "", "rename the agent")
	agentUpdateCmd.Flags().StringVar(&agentUpdateDescription, "description", "", "new description (must be paired with --file — see above)")
	agentUpdateCmd.Flags().StringVar(&agentUpdateFile, "file", "", "JSON file with the full flow graph ({name, description, nodes, connections}); flags win over its fields")

	agentCmd.AddCommand(agentCreateCmd, agentListCmd, agentGetCmd, agentUpdateCmd, agentDeleteCmd)
	rootCmd.AddCommand(agentCmd)
}

// agentFlowFile is the optional --file payload for agents create/update: a
// flow graph (nodes + connections), optionally with a name/description.
// Flags always win over the file's name/description when both are set.
type agentFlowFile struct {
	Name        string                     `json:"name,omitempty"`
	Description string                     `json:"description,omitempty"`
	Nodes       []api.AgentGraphNode       `json:"nodes,omitempty"`
	Connections []api.AgentGraphConnection `json:"connections,omitempty"`
}

// readAgentFlowFile reads and parses a --file argument. Errors are BAD_INPUT
// (the file path or its JSON is the problem, not the API).
func readAgentFlowFile(path string) (*agentFlowFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, clierr.BadInput(fmt.Sprintf("reading --file %s: %v", path, err))
	}
	var f agentFlowFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, clierr.BadInput(fmt.Sprintf("--file %s is not valid JSON: %v", path, err))
	}
	return &f, nil
}

func runAgentCreate(cmd *cobra.Command, args []string) error {
	body := map[string]any{}
	requestedName := ""

	if agentCreateFile != "" {
		f, err := readAgentFlowFile(agentCreateFile)
		if err != nil {
			return err
		}
		if f.Name != "" {
			body["name"] = f.Name
			requestedName = f.Name
		}
		if f.Description != "" {
			body["description"] = f.Description
		}
		if f.Nodes != nil {
			body["nodes"] = f.Nodes
		}
		if f.Connections != nil {
			body["connections"] = f.Connections
		}
	}
	// Flags win over the file.
	if agentCreateName != "" {
		body["name"] = agentCreateName
		requestedName = agentCreateName
	}
	if agentCreateDescription != "" {
		body["description"] = agentCreateDescription
	}
	if _, ok := body["name"]; !ok {
		return clierr.BadInput(`agents create needs --name, or a --file whose JSON has a top-level "name"`)
	}

	client, _, err := getClient()
	if err != nil {
		return err
	}
	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will POST %s\n", client.AccountURL("Agent"))
	}

	var resp api.AgentCreateResponse
	apiErr, err := client.Do("POST", client.AccountURL("Agent"), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	// The backend silently renames on a collision (appends " 1", " 2", ...) —
	// make sure that's impossible to miss.
	if requestedName != "" && resp.Name != "" && resp.Name != requestedName {
		fmt.Fprintf(os.Stderr, "note: name %q was already taken; stored as %q\n", requestedName, resp.Name)
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, resp, nil)
	}
	if resp.Message != "" {
		fmt.Fprintf(os.Stderr, "%s\n", resp.Message)
	}
	return output.KV(os.Stdout, [][2]string{
		{"agent_id", resp.AgentID},
		{"name", resp.Name},
		{"api_id", resp.APIID},
	})
}

func runAgentList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(agentListLimit))
	q.Set("offset", strconv.Itoa(agentListOffset))
	if agentListName != "" {
		q.Set("name", agentListName)
	}
	if agentListState != "" {
		q.Set("state", agentListState)
	}

	var resp api.AgentList
	apiErr, err := client.Do("GET", client.AccountURL("Agent"), nil, q, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, resp.Objects, resp.Meta)
	}
	rows := [][]string{{"ID", "NAME", "STATE", "VERSION", "UPDATED_AT"}}
	for _, a := range resp.Objects {
		rows = append(rows, []string{a.ID, a.Name, a.State, strconv.Itoa(a.Version), a.UpdatedAt})
	}
	return output.Table(os.Stdout, rows)
}

func runAgentGet(cmd *cobra.Command, args []string) error {
	agentID := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var a api.Agent
	apiErr, err := client.Do("GET", client.AccountURL("Agent", agentID), nil, nil, &a)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, a, nil)
	}
	// The full graph can be huge (deeply nested per-node config) — the table
	// view shows counts; use -o json for the full nodes/connections detail.
	return output.KV(os.Stdout, [][2]string{
		{"id", a.ID},
		{"name", a.Name},
		{"description", a.Description},
		{"state", a.State},
		{"version", strconv.Itoa(a.Version)},
		{"nodes", strconv.Itoa(len(a.Nodes))},
		{"connections", strconv.Itoa(len(a.Connections))},
		{"created_at", a.CreatedAt},
		{"updated_at", a.UpdatedAt},
	})
}

func runAgentUpdate(cmd *cobra.Command, args []string) error {
	agentID := args[0]
	body := map[string]any{}

	if agentUpdateFile != "" {
		f, err := readAgentFlowFile(agentUpdateFile)
		if err != nil {
			return err
		}
		if f.Name != "" {
			body["name"] = f.Name
		}
		if f.Description != "" {
			body["description"] = f.Description
		}
		if f.Nodes != nil {
			body["nodes"] = f.Nodes
		}
		if f.Connections != nil {
			body["connections"] = f.Connections
		}
	}
	if agentUpdateName != "" {
		body["name"] = agentUpdateName
	}
	if agentUpdateDescription != "" {
		body["description"] = agentUpdateDescription
	}
	if len(body) == 0 {
		return clierr.BadInput("at least one of --name, --description, --file required")
	}

	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("Agent", agentID), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Updated agent %s: %s\n", agentID, resp.Message)
	return nil
}

func runAgentDelete(cmd *cobra.Command, args []string) error {
	agentID := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("delete agent " + agentID)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("Agent", agentID), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Deleted agent %s\n", agentID)
	return nil
}
