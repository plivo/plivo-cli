package cmd

import (
	"os"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// agentNodesCmd browses the AgentFlowNode catalogue: the node types available
// to build an agent flow's graph, and the JSON Schema each one validates
// its `config` against. Read-only — the catalogue itself isn't editable via
// the CLI, only referenced when building a --file for 'agents create/update'.
var agentNodesCmd = &cobra.Command{
	Use:   "nodes",
	Short: "Browse the AI-agent node catalogue (types available for flow graphs)",
	Args:  cobra.NoArgs,
	RunE:  groupRunE,
}

var agentNodesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available agent node types",
	RunE:  runAgentFlowNodesList,
}

var agentNodesGetCmd = &cobra.Command{
	Use:   "get <node_type>",
	Short: "Get the JSON Schema for one agent node type",
	Long: `Get the JSON Schema for one agent node type.

Use this to see what a node's "config" object must contain before wiring
it into a flow graph with 'agents create --file' / 'agents update --file'.
The response also reports x-plivo-coverage: server-side rules (conditional
requirements, secret references) that aren't expressible in static JSON
Schema, so the schema alone may under-describe validation.`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentFlowNodesGet,
}

func init() {
	agentNodesCmd.AddCommand(agentNodesListCmd, agentNodesGetCmd)
	agentCmd.AddCommand(agentNodesCmd)
}

func runAgentFlowNodesList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.AgentFlowNodeList
	apiErr, err := client.Do("GET", client.AccountURL("AgentFlowNode"), nil, nil, &resp)
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
		return output.JSONRaw(os.Stdout, resp.Raw())
	}
	rows := [][]string{{"NODE_TYPE", "TITLE", "CATEGORY"}}
	for _, n := range resp.Objects {
		rows = append(rows, []string{n.NodeType, n.Title, n.Category})
	}
	return output.Table(os.Stdout, rows)
}

func runAgentFlowNodesGet(cmd *cobra.Command, args []string) error {
	nodeType := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var n api.AgentFlowNode
	apiErr, err := client.Do("GET", client.AccountURL("AgentFlowNode", nodeType), nil, nil, &n)
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
		return output.JSONRaw(os.Stdout, n.Raw())
	}
	// json_schema is often large — table view summarises; use -o json for
	// the full schema, examples, and coverage report.
	ids := make([]string, len(n.OutputStates))
	for i, s := range n.OutputStates {
		ids[i] = s.ID
	}
	return output.KV(os.Stdout, [][2]string{
		{"node_type", n.NodeType},
		{"title", n.Title},
		{"description", n.Description},
		{"usecase", n.Usecase},
		{"output_states", strings.Join(ids, ", ")},
	})
}
