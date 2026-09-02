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
// /v1/Account/{auth_id}/AgentFlow/... via client.AccountURL. Subaccount
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
	Args: cobra.NoArgs,
	RunE: groupRunE,
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

// The three lifecycle verbs. The API exposes Publish/Pause/Resume and the whole
// DRAFT -> ACTIVE workflow pivots on Publish, so without these `agents create`
// left you stuck in draft and forced a drop to raw curl -- the exact thing this
// CLI exists to replace.
//
// One shared runner: the three differ only in a path segment and their wording,
// so three near-identical copies would be pure duplication.
var (
	agentPublishCmd = agentLifecycleCmd(
		"publish", "Publish",
		"Publish an agent flow, moving it from DRAFT to ACTIVE")
	agentPauseCmd = agentLifecycleCmd(
		"pause", "Pause",
		"Pause an ACTIVE agent flow so it stops handling traffic")
	agentResumeCmd = agentLifecycleCmd(
		"resume", "Resume",
		"Resume a PAUSED agent flow, returning it to ACTIVE")
)

func agentLifecycleCmd(verb, segment, short string) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " <agent_id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentLifecycle(verb, segment, args[0])
		},
	}
}

func runAgentLifecycle(verb, segment, agentID string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	url := client.AccountURL("AgentFlow", agentID, segment)
	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will POST %s\n", url)
	}
	// Deliberately no body: the action is fixed by the route, and the server
	// accepts these routes without one.
	var resp api.AgentActionResponse
	apiErr, err := client.Do("POST", url, nil, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	msg := resp.Message
	if msg == "" {
		msg = verb + "d"
	}
	fmt.Fprintf(os.Stderr, "Agent %s: %s\n", agentID, msg)
	return nil
}

func init() {
	agentCreateCmd.Flags().StringVar(&agentCreateName, "name", "", "agent name (required unless --file supplies one)")
	agentCreateCmd.Flags().StringVar(&agentCreateDescription, "description", "", "agent description")
	agentCreateCmd.Flags().StringVar(&agentCreateFile, "file", "", "JSON file with the flow graph ({name, description, nodes, connections}); flags win over its fields")

	agentListCmd.Flags().IntVar(&agentListLimit, "limit", 20, "results per page (max 20)")
	agentListCmd.Flags().IntVar(&agentListOffset, "offset", 0, "pagination offset")
	agentListCmd.Flags().StringVar(&agentListName, "name", "", "filter by name (substring match)")
	agentListCmd.Flags().StringVar(&agentListState, "state", "", "filter by state (e.g. DRAFT, ACTIVE)")
	registerAllFlag(agentListCmd)

	agentUpdateCmd.Flags().StringVar(&agentUpdateName, "name", "", "rename the agent")
	agentUpdateCmd.Flags().StringVar(&agentUpdateDescription, "description", "", "new description (must be paired with --file — see above)")
	agentUpdateCmd.Flags().StringVar(&agentUpdateFile, "file", "", "JSON file with the full flow graph ({name, description, nodes, connections}); flags win over its fields")

	agentCmd.AddCommand(agentCreateCmd, agentListCmd, agentGetCmd, agentUpdateCmd, agentDeleteCmd,
		agentPublishCmd, agentPauseCmd, agentResumeCmd)
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

// mergeAgentFlowFile copies the fields a flow file actually sets onto the request
// body, leaving absent fields alone so an update never blanks what it omits.
// Returns the name the file asked for ("" if it set none), which create tracks to
// warn when the server stores a numbered variant.
func mergeAgentFlowFile(body map[string]any, f *agentFlowFile) string {
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
	return f.Name
}

func runAgentCreate(cmd *cobra.Command, args []string) error {
	body := map[string]any{}
	requestedName := ""

	if agentCreateFile != "" {
		f, err := readAgentFlowFile(agentCreateFile)
		if err != nil {
			return err
		}
		requestedName = mergeAgentFlowFile(body, f)
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
		fmt.Fprintf(os.Stderr, "Will POST %s\n", client.AccountURL("AgentFlow"))
	}

	var resp api.AgentCreateResponse
	apiErr, err := client.Do("POST", client.AccountURL("AgentFlow"), body, nil, &resp)
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
		return output.JSONRaw(os.Stdout, resp.Raw())
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

// accumulateRawObjects folds page's raw "objects" array onto dst's raw
// envelope. dst.Objects (the typed slice) already has every row --all
// fetched, which is all table mode needs; but -o json now renders straight
// from the captured bytes (see RawBody/JSONRaw), so without this a page
// walk would leave -o json showing only the first page — the exact "claims
// to paginate, doesn't" defect that got the old --all removed, just moved
// to the JSON path instead of table.
//
// Best-effort: leaves dst's raw body untouched (still valid JSON, just
// first-page-only) if either side isn't the expected {"objects": [...]}
// shape.
func accumulateRawObjects(dst, page api.RawCapturer) {
	var env map[string]json.RawMessage
	if err := json.Unmarshal(dst.Raw(), &env); err != nil {
		return
	}
	var objects []json.RawMessage
	if err := json.Unmarshal(env["objects"], &objects); err != nil {
		return
	}
	var pageEnv map[string]json.RawMessage
	if err := json.Unmarshal(page.Raw(), &pageEnv); err != nil {
		return
	}
	var pageObjects []json.RawMessage
	if err := json.Unmarshal(pageEnv["objects"], &pageObjects); err != nil {
		return
	}
	merged, err := json.Marshal(append(objects, pageObjects...))
	if err != nil {
		return
	}
	env["objects"] = merged
	if out, err := json.Marshal(env); err == nil {
		dst.SetRaw(out)
	}
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

	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will GET %s?%s\n", client.AccountURL("AgentFlow"), q.Encode())
	}

	var resp api.AgentList
	apiErr, err := client.Do("GET", client.AccountURL("AgentFlow"), nil, q, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	// --all was a declared-but-unconsumed root flag, so it advertised
	// auto-pagination on every command and delivered it nowhere. The server caps
	// limit at 20 (clamping, not rejecting), so >20 agents is ordinary and a
	// silently truncated list is the worst outcome. Walk the pages here.
	if allFlag && !dryRunFlag {
		offset := agentListOffset + len(resp.Objects)
		for len(resp.Objects) < resp.Meta.TotalCount {
			pq := url.Values{}
			for k, v := range q {
				pq[k] = v
			}
			pq.Set("offset", strconv.Itoa(offset))
			var page api.AgentList
			apiErr, err = client.Do("GET", client.AccountURL("AgentFlow"), nil, pq, &page)
			if err != nil {
				return err
			}
			if apiErr != nil {
				return apiErr
			}
			// Defensive: without this a server that stops returning rows before
			// total_count is reached would spin forever.
			if len(page.Objects) == 0 {
				break
			}
			resp.Objects = append(resp.Objects, page.Objects...)
			accumulateRawObjects(&resp, &page)
			offset += len(page.Objects)
		}
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, resp.Raw())
	}
	rows := [][]string{{"AGENT_ID", "NAME", "STATE", "FLOW_TYPE", "VERSION", "UPDATED_AT"}}
	for _, a := range resp.Objects {
		rows = append(rows, []string{a.ID, a.Name, a.State, a.FlowType, strconv.Itoa(a.Version), a.UpdatedAt})
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
	apiErr, err := client.Do("GET", client.AccountURL("AgentFlow", agentID), nil, nil, &a)
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
		return output.JSONRaw(os.Stdout, a.Raw())
	}
	// The full graph can be huge (deeply nested per-node config) — the table
	// view shows counts; use -o json for the full nodes/connections detail.
	return output.KV(os.Stdout, [][2]string{
		{"agent_id", a.ID},
		{"name", a.Name},
		{"description", a.Description},
		{"state", a.State},
		{"flow_type", a.FlowType},
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
		mergeAgentFlowFile(body, f)
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
	apiErr, err := client.Do("POST", client.AccountURL("AgentFlow", agentID), body, nil, &resp)
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
	apiErr, err := client.Do("DELETE", client.AccountURL("AgentFlow", agentID), nil, nil, nil)
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
