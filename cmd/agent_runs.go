package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// agentRunsCmd is CRUD-read on an agent's execution history, peer of
// 'agents nodes' under 'agents'. Naming mirrors 'voice calls streams':
// a resource-scoped sub-collection nested under its parent.
var agentRunsCmd = &cobra.Command{
	Use:   "runs",
	Short: "Inspect runs (executions) of an agent flow",
	Args:  cobra.NoArgs,
	RunE:  groupRunE,
}

var (
	agentRunsListLimit  int
	agentRunsListOffset int
)

var agentRunsListCmd = &cobra.Command{
	Use:   "list <agent_id>",
	Short: "List runs of an agent flow",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentRunsList,
}

var agentRunsGetCmd = &cobra.Command{
	Use:   "get <agent_id> <run_id>",
	Short: "Get one run of an agent flow (status, logs, goal metrics)",
	Args:  cobra.ExactArgs(2),
	RunE:  runAgentRunsGet,
}

func init() {
	registerListFlags(agentRunsListCmd, &agentRunsListLimit, &agentRunsListOffset)

	agentRunsCmd.AddCommand(agentRunsListCmd, agentRunsGetCmd)
	agentCmd.AddCommand(agentRunsCmd)
}

func runAgentRunsList(cmd *cobra.Command, args []string) error {
	agentID := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(agentRunsListLimit))
	q.Set("offset", strconv.Itoa(agentRunsListOffset))

	var resp api.AgentRunList
	if err := fetchList(client, client.AccountURL("AgentFlow", agentID, "Run"), q, "objects", &resp); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return listJSON(os.Stdout, resp.Raw(), "objects")
	}
	rows := [][]string{{"RUN_ID", "STATUS", "STARTED_AT", "ENDED_AT", "PLAYGROUND"}}
	for _, r := range resp.Objects {
		rows = append(rows, []string{r.RunID, r.Status, r.StartedAt, r.EndedAt, fmt.Sprintf("%v", r.IsPlayground)})
	}
	return output.Table(os.Stdout, rows)
}

func runAgentRunsGet(cmd *cobra.Command, args []string) error {
	agentID, runID := args[0], args[1]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var r api.AgentRun
	apiErr, err := client.Do("GET", client.AccountURL("AgentFlow", agentID, "Run", runID), nil, nil, &r)
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
		return output.JSONRaw(os.Stdout, r.Raw())
	}
	// logs/goal_metrics are per-event-type shaped (varies with what the node
	// emitted) — table view shows a count; use -o json for full detail.
	return output.KV(os.Stdout, [][2]string{
		{"run_id", r.RunID},
		{"agent_id", r.AgentID},
		{"status", r.Status},
		{"started_at", r.StartedAt},
		{"ended_at", r.EndedAt},
		{"duration_s", fmt.Sprintf("%v", r.Duration)},
		{"is_playground", fmt.Sprintf("%v", r.IsPlayground)},
		{"logs", strconv.Itoa(len(r.Logs))},
	})
}
