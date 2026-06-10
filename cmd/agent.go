//go:build internal

// Gated behind the `internal` build tag: the Vibe AI agent surface depends on
// Plivo-internal services (Contacto gateway, aiassist, PHLO config, hodor) and
// is not part of the public v1. Build with `-tags internal` to include it.

package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/contacto"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/plivo/plivo-cli/internal/version"
	"github.com/spf13/cobra"
)

// PHLO config service routes (via regional Contacto auth-api gateway).
// `/phlo` and `/flow` are aliases server-side.
const (
	phloListPath     = "/v1/contacto-core/contacto-config/phlo"
	phloItemPath     = "/v1/contacto-core/contacto-config/phlo/" // append uuid
	vibeGeneratePath = "/v1/ai-assist/vibe-agent/generate"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage Plivo Vibe AI agents",
	Long: `plivo agent — manage AI agents (PHLO flows) from the command line.

Requires a Contacto session: run 'plivo contacto login' first.

These commands hit the same regional Contacto API gateway the web console uses,
so anything they do is visible immediately in the dev console UI.

The attach subcommand uses your Plivo classic credentials (PLIVO_AUTH_ID +
PLIVO_AUTH_TOKEN) against api.plivo.com to rewire an application's answer URL —
that's a public Plivo REST operation, not Contacto-side.`,
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List Vibe Agents on the active Contacto org",
	RunE:  runAgentList,
}

var agentGetCmd = &cobra.Command{
	Use:   "get <uuid>",
	Short: "Get a single Vibe Agent by uuid",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentGet,
}

var (
	agentCreatePrompt       string
	agentCreateName         string
	agentCreateType         string
	agentCreateFromTemplate string
	agentCreateContinue     string
	agentCreateSessionID    string
	agentCreateFrom         string
)

var agentCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a Vibe Agent — interactive on first call, resumable via -c on subsequent calls",
	Long: `Create a Vibe Agent (a PHLO flow) via the LLM-driven vibe pipeline.

Three modes:
  Fresh interactive:  plivo agent create --prompt "a joke telling bot"
  Headless continue:  plivo agent create -c "approve and build it"
  Fast template copy: plivo agent create --from-template <existing_uuid>

The interactive flow shows the LLM's plan, asks you to approve/change/discard,
and (on approve) builds + saves the flow as a draft.

The -c flow is one-turn-per-invocation: session state is persisted to
~/.plivo/vibe-session.json so an AI driver can script multi-turn agent
creation as a sequence of one-shot commands. Use 'plivo agent session show'
to inspect the current session and 'plivo agent session clear' to reset.`,
	RunE: runAgentCreate,
}

var agentSessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Inspect or clear the saved vibe-agent create session (~/.plivo/vibe-session.json)",
}

var agentSessionShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the current vibe-agent session (session_id, last turn, workflow size)",
	RunE:  runAgentSessionShow,
}

var agentSessionClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete the saved vibe-agent session file",
	RunE:  runAgentSessionClear,
}

var (
	agentUpdateFile string
	agentUpdateName string
)

var agentUpdateCmd = &cobra.Command{
	Use:   "update <uuid>",
	Short: "Update an existing Vibe Agent (--from <workflow.json> or --name <new-name>)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentUpdate,
}

var agentPublishCmd = &cobra.Command{
	Use:   "publish <uuid>",
	Short: "Promote a DRAFT agent to ACTIVE so the runner can serve it",
	Long: `plivo agent publish — flip DRAFT → ACTIVE.

The runner only serves agents in state=ACTIVE with enabled=true. Vibe
saves new agents as DRAFT. This command re-uploads the workflow with
publish fields overlaid (state=ACTIVE, enabled=true, changed=true,
group=public) — mirrors the console UI's publishFlow at
apps/web/src/v2/pages/flow/hooks/use-publish-flow.tsx:518-539.

After this, 'plivo agent run' will succeed against the agent.`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentPublish,
}

var agentDownloadOut string

var agentDeleteCmd = &cobra.Command{
	Use:   "delete <uuid>",
	Short: "Delete a Vibe Agent permanently (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentDelete,
}

var agentDownloadCmd = &cobra.Command{
	Use:   "download <uuid>",
	Short: "Download an agent's full config JSON (for inspection, diffing, version control)",
	Long: `plivo agent download — fetch an agent's complete flow config and write it
to a local JSON file. Same data the console UI's "Download JSON" button gives
you, in the canonical PHLO API shape (nodes, connections, phlo, global_meta).

Useful for: letting an AI agent verify its own flow configuration, checkpointing
before edits, diffing two versions, or feeding into 'plivo agent update --from
<file>'.

Defaults to writing <phlo_name>-config.json in the current directory. Use --out
to override.`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentDownload,
}

var (
	agentRunPhoneNumber string
	agentRunParams      []string
)

var agentRunCmd = &cobra.Command{
	Use:   "run <uuid>",
	Short: "HTTP-trigger an agent flow (use this for outbound agents whose Start node has triggers: [\"http\"])",
	Long: `plivo agent run — invoke an HTTP-triggered agent flow.

Outbound voice agents typically have a Start node with triggers: ["http"] —
they expect to be POSTed with a JSON body that fills the Start node's
payload_format (e.g. {"phone_number": "+91XXXXXXXXXX"}). The flow then
places its own outbound call via the initiate_call node and runs the
AI agent on connection.

This is the RIGHT invocation for agent flows. 'plivo call make' is for
direct outbound calls with a static answer_url — different model.

The agent must be PUBLISHED (state=ACTIVE, enabled=true). Drafts will
return 'No phlo found' from the runner.

Example:
  plivo agent run a566fd28-... --phone-number +919891859683
  plivo agent run <uuid> --param phone_number=+91X --param greeting="Hi"

Decide between this and 'plivo call make':
  agent run    — when Start.triggers contains "http" (most voice-AI agents)
  call make    — when you want to place a direct outbound call yourself
                 and supply a static answer_url (legacy / non-agent flows)`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentRun,
}

var (
	agentAttachApp    string
	agentAttachNumber string
	agentAttachRegion string
)

var agentAttachCmd = &cobra.Command{
	Use:   "attach <agent_uuid>",
	Short: "Attach an agent to a Plivo application (rewrites answer/message/hangup URLs)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentAttach,
}

func init() {
	agentCreateCmd.Flags().StringVar(&agentCreatePrompt, "prompt", "", "initial description (skips the interactive prompt)")
	agentCreateCmd.Flags().StringVar(&agentCreateName, "name", "", "agent name (override the LLM-suggested name)")
	agentCreateCmd.Flags().StringVar(&agentCreateType, "type", "outgoing_call", "phlo_type: outgoing_call | incoming_call | outgoing_sms | incoming_sms | outgoing_whatsapp | incoming_whatsapp")
	agentCreateCmd.Flags().StringVar(&agentCreateFromTemplate, "from-template", "", "copy an existing agent uuid as the starting point (skips the LLM and lands a savable draft in one shot)")
	agentCreateCmd.Flags().StringVarP(&agentCreateContinue, "continue", "c", "", "continue the saved vibe session with this follow-up message (one turn, non-interactive)")
	agentCreateCmd.Flags().StringVar(&agentCreateSessionID, "session-id", "", "explicit session uuid to resume (default: read from ~/.plivo/vibe-session.json). Requires --continue.")
	agentCreateCmd.Flags().StringVar(&agentCreateFrom, "from", "", "E.164 caller-id (e.g. +14695184352) to fill into any outbound initiate_call nodes the LLM emits — required if the agent does outbound calls")

	agentUpdateCmd.Flags().StringVar(&agentUpdateFile, "from", "", "path to workflow JSON file")
	agentUpdateCmd.Flags().StringVar(&agentUpdateName, "name", "", "rename the agent (sends just {\"name\":...} via PATCH)")

	agentAttachCmd.Flags().StringVar(&agentAttachApp, "app", "", "application id to wire (one of --app or --number required)")
	agentAttachCmd.Flags().StringVar(&agentAttachNumber, "number", "", "phone number to wire (resolves to its application)")
	agentAttachCmd.Flags().StringVar(&agentAttachRegion, "region", "us-east-1", "runner-service region for the answer URL")

	agentRunCmd.Flags().StringVar(&agentRunPhoneNumber, "phone-number", "", "shorthand for --param phone_number=<E.164> — the most common Start-node trigger param")
	agentRunCmd.Flags().StringArrayVar(&agentRunParams, "param", nil, "extra Start-node payload params as key=value (repeatable)")

	agentDownloadCmd.Flags().StringVar(&agentDownloadOut, "out", "", "output filename (default: <phlo_name>-config.json)")

	agentSessionCmd.AddCommand(agentSessionShowCmd, agentSessionClearCmd)
	agentCmd.AddCommand(agentListCmd, agentGetCmd, agentCreateCmd, agentUpdateCmd, agentAttachCmd, agentRunCmd, agentPublishCmd, agentDownloadCmd, agentDeleteCmd, agentSessionCmd)
	rootCmd.AddCommand(agentCmd)
}

// writeJSONStdout pretty-prints any JSON byte slice to stdout. Agent endpoints
// return minified JSON by default; without re-indenting, `plivo agent list | head -n 5`
// is useless because the entire payload is a single line.
func writeJSONStdout(body []byte) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		_, _ = os.Stdout.Write(body) // fallback if not JSON
		fmt.Fprintln(os.Stdout)
		return
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		_, _ = os.Stdout.Write(body)
		fmt.Fprintln(os.Stdout)
		return
	}
	_, _ = os.Stdout.Write(pretty)
	fmt.Fprintln(os.Stdout)
}

// getContactoClient loads the session and returns a Contacto HTTP client.
func getContactoClient() (*contacto.Client, error) {
	prof, err := config.LoadContacto()
	if err != nil {
		return nil, err
	}
	return contacto.New(prof), nil
}

func runAgentDelete(cmd *cobra.Command, args []string) error {
	uuidArg := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("delete agent " + uuidArg)
	}
	c, err := getContactoClient()
	if err != nil {
		return err
	}
	resp, err := c.Do(cmd.Context(), "DELETE", phloItemPath+uuidArg, nil)
	if err != nil {
		return err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.Status, strings.TrimSpace(string(resp.Body)))
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Deleted agent %s\n", uuidArg)
	return nil
}

func runAgentList(cmd *cobra.Command, args []string) error {
	c, err := getContactoClient()
	if err != nil {
		return err
	}
	resp, err := c.Do(cmd.Context(), "GET", phloListPath, nil)
	if err != nil {
		return err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.Status, strings.TrimSpace(string(resp.Body)))
	}
	if effectiveFormat() == output.FormatJSON {
		writeJSONStdout(resp.Body)
		return nil
	}
	return renderAgentList(resp.Body)
}

// renderAgentList tries to surface the most useful columns from PHLO config
// service's grouped-flow response. The response shape varies (some endpoints
// return {objects:[]}, others return a flow-group object with nested lists);
// we fall back to dumping JSON when we can't recognise it.
func renderAgentList(body []byte) error {
	var generic any
	if err := json.Unmarshal(body, &generic); err != nil {
		fmt.Fprintln(os.Stdout, string(body))
		return nil
	}

	flows := extractFlows(generic)
	if len(flows) == 0 {
		fmt.Fprintln(os.Stderr, "(no agents)")
		fmt.Fprintln(os.Stdout, string(body))
		return nil
	}
	rows := [][]string{{"UUID", "NAME", "STATE", "TYPE", "UPDATED"}}
	for _, f := range flows {
		uuid := str(f["uuid"])
		if uuid == "" {
			uuid = str(f["id"]) // PHLO config service returns `id` not `uuid`
		}
		rows = append(rows, []string{
			uuid,
			str(f["name"]),
			str(f["state"]),
			str(f["phlo_type"]),
			formatTimestamp(f["updated_at"]),
		})
	}
	return output.Table(os.Stdout, rows)
}

// formatTimestamp turns a possible unix timestamp (number or string) into a
// short YYYY-MM-DD; strings that aren't unix seconds pass through unchanged.
func formatTimestamp(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case float64:
		if t > 0 {
			return time.Unix(int64(t), 0).UTC().Format("2006-01-02")
		}
	case int:
		if t > 0 {
			return time.Unix(int64(t), 0).UTC().Format("2006-01-02")
		}
	case int64:
		if t > 0 {
			return time.Unix(t, 0).UTC().Format("2006-01-02")
		}
	case string:
		return t
	}
	return fmt.Sprintf("%v", v)
}

// extractFlows walks a flexible response shape to find a list of flow objects.
// PHLO config service may return: an array, {objects:[]}, {flows:[]}, or
// {phlos:[...]}, or a flow-group keyed by group name.
func extractFlows(v any) []map[string]any {
	switch t := v.(type) {
	case []any:
		return mapList(t)
	case map[string]any:
		for _, key := range []string{"objects", "flows", "phlos", "data", "results"} {
			if list, ok := t[key].([]any); ok {
				return mapList(list)
			}
		}
		// Grouped: values may be arrays of flows
		var out []map[string]any
		for _, val := range t {
			if list, ok := val.([]any); ok {
				out = append(out, mapList(list)...)
			}
		}
		return out
	}
	return nil
}

func mapList(items []any) []map[string]any {
	var out []map[string]any
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func runAgentGet(cmd *cobra.Command, args []string) error {
	uuidArg := args[0]
	c, err := getContactoClient()
	if err != nil {
		return err
	}
	resp, err := c.Do(cmd.Context(), "GET", phloItemPath+uuidArg, nil)
	if err != nil {
		return err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.Status, strings.TrimSpace(string(resp.Body)))
	}
	writeJSONStdout(resp.Body)
	return nil
}

func runAgentUpdate(cmd *cobra.Command, args []string) error {
	uuidArg := args[0]
	c, err := getContactoClient()
	if err != nil {
		return err
	}

	var body map[string]any
	if agentUpdateFile != "" {
		raw, err := os.ReadFile(agentUpdateFile)
		if err != nil {
			return fmt.Errorf("read %s: %w", agentUpdateFile, err)
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return fmt.Errorf("parse %s as JSON: %w", agentUpdateFile, err)
		}
	} else if agentUpdateName != "" {
		body = map[string]any{"name": agentUpdateName}
	} else {
		return fmt.Errorf("specify --from <file> or --name <new-name>")
	}

	// PATCH if only renaming, POST (full replace) if --from was used.
	method := "POST"
	if agentUpdateFile == "" && agentUpdateName != "" {
		method = "PATCH"
	}

	resp, err := c.Do(cmd.Context(), method, phloItemPath+uuidArg, body)
	if err != nil {
		return err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.Status, strings.TrimSpace(string(resp.Body)))
	}
	fmt.Fprintf(os.Stderr, "✓ Updated agent %s\n", uuidArg)
	if effectiveFormat() == output.FormatJSON {
		writeJSONStdout(resp.Body)
	}
	return nil
}

func runAgentCreate(cmd *cobra.Command, args []string) error {
	c, err := getContactoClient()
	if err != nil {
		return err
	}

	// Fast path: --from-template skips the LLM entirely and copies an existing
	// agent as a draft. Reliable, single round-trip, lands in the dev UI
	// immediately. Use this when the vibe-create flow is flaky or for demos.
	if agentCreateFromTemplate != "" {
		return runAgentCreateFromTemplate(cmd, c)
	}

	// Flag-shape validation: --session-id only makes sense paired with -c, and
	// --prompt + -c are mutually exclusive (one starts, the other resumes).
	if agentCreateSessionID != "" && agentCreateContinue == "" {
		return fmt.Errorf("--session-id requires --continue / -c")
	}
	if agentCreateContinue != "" && agentCreatePrompt != "" {
		return fmt.Errorf("--prompt starts a new session, --continue / -c resumes one — pick one")
	}

	reader := bufio.NewReader(os.Stdin)

	// Resolve the session: either resume (`-c`) or start fresh.
	var (
		sessionID       string
		currentWorkflow string
		initialName     string
		prompt          string
		headless        bool // -c mode: one turn, no interactive prompts
	)

	if agentCreateContinue != "" {
		// Resume path: pull session from disk (or use the explicit --session-id).
		headless = true
		prompt = agentCreateContinue
		sess, err := config.LoadVibeSession()
		if err != nil {
			if errors.Is(err, config.ErrNoVibeSession) && agentCreateSessionID == "" {
				return fmt.Errorf("no vibe session to continue. Start one with: plivo agent create --prompt \"...\"")
			}
			if !errors.Is(err, config.ErrNoVibeSession) {
				return err
			}
			// Explicit --session-id, no on-disk session — build a minimal stub.
			sess = &config.VibeSession{SessionID: agentCreateSessionID}
		}
		if agentCreateSessionID != "" {
			sess.SessionID = agentCreateSessionID
		}
		sessionID = sess.SessionID
		currentWorkflow = sess.CurrentWorkflow
		initialName = sess.AgentName
		if initialName == "" {
			initialName = "cli-vibe-" + time.Now().UTC().Format("20060102-150405")
		}
		fmt.Fprintf(os.Stderr, "[resuming session %s — turn #%d]\n", sessionID, sess.TurnCount+1)
	} else {
		// Fresh path: read --prompt or prompt the user.
		prompt = agentCreatePrompt
		if prompt == "" {
			fmt.Fprint(os.Stderr, "Describe your agent: ")
			line, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			prompt = strings.TrimSpace(line)
		}
		if prompt == "" {
			return fmt.Errorf("a description is required")
		}
		sessionID = uuid.NewString()
		initialName = agentCreateName
		if initialName == "" {
			initialName = "cli-vibe-" + time.Now().UTC().Format("20060102-150405")
		}
		// Persist an initial session row so an immediate -c works even if the
		// first turn never reaches a terminal event.
		_ = config.SaveVibeSession(&config.VibeSession{
			SessionID:     sessionID,
			AgentName:     initialName,
			InitialPrompt: prompt,
			TurnCount:     0,
		})
	}
	_ = initialName // used at save time

	// The vibe-agent flow has two stages: PLAN (LLM describes, asks "approve?")
	// then BUILD (LLM emits node_added/edge_added). Each round we send the
	// user's message, stream until we hit waiting_for_approval / completed /
	// save_draft_requested, then decide what to do based on which terminal
	// signal arrived.
	turnsInLoop := 0
	persistSession := func(workflow string) {
		turnsInLoop++
		_ = config.SaveVibeSession(&config.VibeSession{
			SessionID:       sessionID,
			CurrentWorkflow: workflow,
			AgentName:       initialName,
			InitialPrompt:   agentCreatePrompt,
			TurnCount:       turnsInLoop,
		})
	}
	for {
		// Keep the body minimal — yesterday's bg run with just user_message +
		// session_id (no phlo_uuid, no agent_settings, no flow_name) actually
		// emitted node_added events before timing out. Adding the Console-style
		// extras seemed to regress LLM behaviour back to plan-only mode.
		body := map[string]any{
			"user_message": prompt,
			"session_id":   sessionID,
		}
		if currentWorkflow != "" {
			body["current_workflow"] = currentWorkflow
		}

		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "[vibe agent ◷ generating...]")

		finalWorkflow := ""
		terminalEvent := ""
		nodeCount := 0
		aiBuffer := ""
		flushAI := func() {
			if aiBuffer != "" {
				fmt.Fprintln(os.Stderr)
				aiBuffer = ""
			}
		}
		streamErr := c.SSE(cmd.Context(), vibeGeneratePath, body, func(ev contacto.SSEEvent) bool {
			updateType, payload, workflow := parseVibeEvent(ev.Data)
			if workflow != "" {
				finalWorkflow = workflow
			}
			switch updateType {
			case "ai_message_chunk":
				if m, ok := payload["message"].(string); ok && m != "" {
					if aiBuffer == "" {
						fmt.Fprint(os.Stderr, "  💬 ")
					}
					fmt.Fprint(os.Stderr, m)
					aiBuffer += m
				}
			case "ai_message":
				flushAI()
				if m, ok := payload["message"].(string); ok && m != "" {
					fmt.Fprintf(os.Stderr, "  💬 %s\n", m)
				}
			case "reasoning_started":
				flushAI()
				fmt.Fprintln(os.Stderr, "  🧠 thinking...")
			case "node_added":
				flushAI()
				nodeCount++
				// The vibe-agent emits node_added in React-Flow shape: either
				// {node: {id, type, data: {meta, config}}} or the node fields
				// at the payload root. Walk both layouts.
				name, nodeType := extractNodeNameAndType(payload)
				fmt.Fprintf(os.Stderr, "  ➕ node: %s (%s)\n", name, nodeType)
			case "edge_added":
				flushAI()
				src, dst := extractEdgeSourceTarget(payload)
				fmt.Fprintf(os.Stderr, "  → %s → %s\n", src, dst)
			case "tool_call_started":
				flushAI()
				name, _ := payload["tool_name"].(string)
				desc, _ := payload["client_description"].(string)
				if desc == "" {
					desc = name
				}
				fmt.Fprintf(os.Stderr, "  🛠  %s\n", desc)
			case "waiting_for_approval":
				flushAI()
				terminalEvent = "waiting_for_approval"
				return false
			case "save_draft_requested", "completed":
				flushAI()
				terminalEvent = updateType
				return false
			case "error":
				flushAI()
				fmt.Fprintf(os.Stderr, "  ❌ %v\n", payload)
				terminalEvent = "error"
				return false
			case "heartbeat":
				// ignore
			}
			return true
		})
		flushAI()
		if streamErr != nil {
			return streamErr
		}

		// Persist what we have so far before deciding what to do — so a
		// follow-up `plivo agent create -c "..."` can pick up from here even
		// if the user Ctrl+Cs out of the interactive prompt below.
		persistSession(finalWorkflow)

		// Empty-completion guard: the LLM sometimes ends with `completed` /
		// `save_draft_requested` after only planning, never emitting node_added.
		// Treat that as an implicit waiting_for_approval so we either prompt the
		// user (interactive) or print a clear next-step hint (headless).
		if (terminalEvent == "completed" || terminalEvent == "save_draft_requested") && nodeCount == 0 {
			terminalEvent = "waiting_for_approval"
		}

		// Stream ended — decide based on terminal signal.
		switch terminalEvent {
		case "completed", "save_draft_requested":
			// Build phase complete. Save the workflow as a draft via POST /phlo.
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "═══════════════════════════════════════════════════════")
			renderWorkflowSummary(os.Stderr, finalWorkflow)
			fmt.Fprintln(os.Stderr, "═══════════════════════════════════════════════════════")
			if finalWorkflow == "" {
				return fmt.Errorf("vibe agent finished but workflow is empty — nothing to save")
			}
			// Fill in any required-but-empty fields the LLM left blank
			// (e.g. initiate_call.from). Interactive: prompt. Headless: error.
			if err := promptForMissingFieldsIfNeeded(finalWorkflow, reader, headless); err != nil {
				return err
			}
			saveName := agentCreateName
			if saveName == "" {
				saveName = initialName
			}
			if err := saveContactoWorkflow(cmd, c, finalWorkflow, saveName, ""); err != nil {
				return err
			}
			// Session is done — clear it so the next `create` is fresh.
			_ = config.ClearVibeSession()
			return nil
		case "waiting_for_approval":
			// Plan phase — LLM described, asking before building.
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "═══════════════════════════════════════════════════════")
			if nodeCount > 0 {
				renderWorkflowSummary(os.Stderr, finalWorkflow)
			} else {
				fmt.Fprintln(os.Stderr, "Plan above. LLM is asking before it builds the actual flow nodes.")
			}
			fmt.Fprintln(os.Stderr, "═══════════════════════════════════════════════════════")

			if headless {
				// In -c mode we don't prompt — print the next step and exit cleanly.
				fmt.Fprintln(os.Stderr, "Session saved. Continue with one of:")
				fmt.Fprintln(os.Stderr, "  plivo agent create -c \"approve and build it\"")
				fmt.Fprintln(os.Stderr, "  plivo agent create -c \"change X to Y\"")
				fmt.Fprintln(os.Stderr, "  plivo agent session clear      # abandon this session")
				return nil
			}

			fmt.Fprint(os.Stderr, "[a] approve — go ahead and build  [c] ask for changes  [d] discard\n> ")
			choice, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			switch strings.ToLower(strings.TrimSpace(choice)) {
			case "a", "approve":
				// Console UI sends this exact string on Approve & Continue
				// (copilot-approval-buttons.tsx:35). The LLM is trained to
				// recognise the phrase as the build trigger.
				prompt = "Looks good to me! Let's build it!"
				currentWorkflow = finalWorkflow
				continue
			case "c", "change":
				fmt.Fprint(os.Stderr, "What to change? > ")
				line, _ := reader.ReadString('\n')
				prompt = strings.TrimSpace(line)
				currentWorkflow = finalWorkflow
				continue
			case "d", "discard", "":
				fmt.Fprintln(os.Stderr, "Discarded. (Session kept — run 'plivo agent session clear' to wipe.)")
				return nil
			default:
				fmt.Fprintln(os.Stderr, "Unknown choice; discarding.")
				return nil
			}
		case "error":
			return fmt.Errorf("vibe agent emitted error event — see output above")
		default:
			return fmt.Errorf("stream ended without a terminal event (got %q)", terminalEvent)
		}
	}
}

func runAgentSessionShow(cmd *cobra.Command, args []string) error {
	sess, err := config.LoadVibeSession()
	if err != nil {
		if errors.Is(err, config.ErrNoVibeSession) {
			fmt.Fprintln(os.Stderr, "No vibe session. Start one with: plivo agent create --prompt \"...\"")
			return nil
		}
		return err
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, sess, nil)
	}
	wfBytes := len(sess.CurrentWorkflow)
	return output.KV(os.Stdout, [][2]string{
		{"session_id", sess.SessionID},
		{"agent_name", sess.AgentName},
		{"initial_prompt", sess.InitialPrompt},
		{"turn_count", fmt.Sprintf("%d", sess.TurnCount)},
		{"started_at", sess.StartedAt},
		{"last_turn_at", sess.LastTurnAt},
		{"current_workflow", fmt.Sprintf("%d bytes", wfBytes)},
	})
}

func runAgentSessionClear(cmd *cobra.Command, args []string) error {
	if err := config.ClearVibeSession(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Vibe session cleared.")
	return nil
}

// receiveToSendShape mutates wf in place to convert PHLO's GET response into
// the shape POST expects. The only delta in practice is config.model: GET
// returns it as a list of typed-field entries [{data: {k: v}, type, inputType,
// label, validation, ...}, ...]; POST expects a flat {k: v} dict.
//
// Field entries with inputType="settings" are themselves a list of sub-fields
// nested under .data ([{data: {k: v}, ...}, ...]) — we flatten those too.
//
// Anything else (output_states, node_vars, etc.) goes through untouched.
func receiveToSendShape(wf map[string]any) {
	nodes, _ := wf["nodes"].([]any)
	for _, n := range nodes {
		node, ok := n.(map[string]any)
		if !ok {
			continue
		}
		cfg, ok := node["config"].(map[string]any)
		if !ok {
			continue
		}
		modelList, isList := cfg["model"].([]any)
		if !isList {
			continue // already in send-shape (dict) — no-op
		}
		flat := map[string]any{}
		collect := func(data map[string]any) {
			for k, v := range data {
				if v != nil {
					flat[k] = v
				}
			}
		}
		for _, entry := range modelList {
			e, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			switch d := e["data"].(type) {
			case map[string]any:
				collect(d)
			case []any: // settings — nested array of sub-fields
				for _, sub := range d {
					if subEntry, ok := sub.(map[string]any); ok {
						if subData, ok := subEntry["data"].(map[string]any); ok {
							collect(subData)
						}
					}
				}
			}
		}
		cfg["model"] = flat
	}
}

// runAgentPublish promotes a DRAFT agent to ACTIVE by re-uploading the workflow
// with publish fields overlaid. Mirrors contacto-console's publishFlow logic
// (use-publish-flow.tsx:518-539).
func runAgentPublish(cmd *cobra.Command, args []string) error {
	agentUUID := args[0]
	c, err := getContactoClient()
	if err != nil {
		return err
	}

	// 1) GET current workflow
	getResp, err := c.Do(cmd.Context(), "GET", phloItemPath+agentUUID, nil)
	if err != nil {
		return err
	}
	if getResp.Status < 200 || getResp.Status >= 300 {
		return fmt.Errorf("fetch agent failed (HTTP %d): %s", getResp.Status, strings.TrimSpace(string(getResp.Body)))
	}
	var envelope map[string]any
	if err := json.Unmarshal(getResp.Body, &envelope); err != nil {
		return fmt.Errorf("parse agent: %w", err)
	}
	wf, _ := envelope["data"].(map[string]any)
	if wf == nil {
		wf = envelope
	}
	phlo, _ := wf["phlo"].(map[string]any)
	if phlo == nil {
		return fmt.Errorf("response missing phlo field — server returned an unexpected shape")
	}

	// 2) Convert PHLO receive-shape → send-shape. GET returns config.model as
	//    a list of {data, type, inputType, label, validation} field entries;
	//    POST expects config.model as a flat {key: value} dict (the same shape
	//    translateVibeWorkflow emits). Without this, the server fails with
	//    "'list' object has no attribute 'get'" on POST.
	receiveToSendShape(wf)

	// 3) Overlay publish fields (per use-publish-flow.tsx:522-539)
	phlo["state"] = "ACTIVE"
	phlo["enabled"] = true
	phlo["changed"] = true
	phlo["group"] = "public"
	phlo["redact"] = false
	if d, _ := phlo["description"].(string); d == "" {
		phlo["description"] = "Flow description"
	}
	// Server-only fields that fail validation on re-POST.
	delete(wf, "validation_errors")

	if logLevel == "debug" {
		if rendered, err := json.MarshalIndent(wf, "", "  "); err == nil {
			fmt.Fprintf(os.Stderr, "[debug] publish POST body:\n%s\n", string(rendered))
		}
	}

	// 3) POST back
	postResp, err := c.Do(cmd.Context(), "POST", phloItemPath+agentUUID, wf)
	if err != nil {
		return err
	}
	if postResp.Status < 200 || postResp.Status >= 300 {
		nodes, _ := wf["nodes"].([]any)
		if pretty, ok := formatPHLOFieldErrors(postResp.Body, nodes); ok {
			return fmt.Errorf("publish failed (HTTP %d) — PHLO rejected these fields:\n%s", postResp.Status, pretty)
		}
		return fmt.Errorf("publish failed (HTTP %d): %s", postResp.Status, strings.TrimSpace(string(postResp.Body)))
	}

	// 4) Confirm
	if effectiveFormat() == output.FormatJSON {
		writeJSONStdout(postResp.Body)
		return nil
	}
	var newEnv map[string]any
	_ = json.Unmarshal(postResp.Body, &newEnv)
	newWf, _ := newEnv["data"].(map[string]any)
	if newWf == nil {
		newWf = newEnv
	}
	newPhlo, _ := newWf["phlo"].(map[string]any)
	state, _ := newPhlo["state"].(string)
	enabled, _ := newPhlo["enabled"].(bool)
	version := "?"
	if v, ok := newPhlo["active_version"].(float64); ok {
		version = fmt.Sprintf("%d", int(v))
	}
	fmt.Fprintf(os.Stderr, "✓ Published %s\n", agentUUID)
	fmt.Fprintf(os.Stderr, "  state:   %s\n", state)
	fmt.Fprintf(os.Stderr, "  enabled: %v\n", enabled)
	fmt.Fprintf(os.Stderr, "  version: %s\n", version)
	return nil
}

// runAgentDownload writes the agent's full config JSON to a local file.
// Mirrors what contacto-console's "Download JSON" button gives you, in the
// canonical PHLO API receive-shape. Filename defaults to <phlo_name>-config.json
// (slugified) to match the console convention; --out overrides.
func runAgentDownload(cmd *cobra.Command, args []string) error {
	agentUUID := args[0]
	c, err := getContactoClient()
	if err != nil {
		return err
	}
	resp, err := c.Do(cmd.Context(), "GET", phloItemPath+agentUUID, nil)
	if err != nil {
		return err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return fmt.Errorf("fetch agent failed (HTTP %d): %s", resp.Status, strings.TrimSpace(string(resp.Body)))
	}

	// Pretty-print so the file is diff-able and human-readable.
	var v any
	if err := json.Unmarshal(resp.Body, &v); err != nil {
		return fmt.Errorf("parse agent JSON: %w", err)
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	out := agentDownloadOut
	if out == "" {
		// Extract phlo.name to mimic console's "<name>-config.json".
		if env, ok := v.(map[string]any); ok {
			if data, ok := env["data"].(map[string]any); ok {
				if phlo, ok := data["phlo"].(map[string]any); ok {
					if name, _ := phlo["name"].(string); name != "" {
						out = slugify(name) + "-config.json"
					}
				}
			}
		}
		if out == "" {
			out = agentUUID + "-config.json"
		}
	}

	if err := os.WriteFile(out, pretty, 0644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	fmt.Fprintf(os.Stderr, "✓ Saved %d bytes to %s\n", len(pretty), out)
	return nil
}

// slugify makes a file-system-safe name from a phlo name.
func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '_', r == '-', r == '.':
			b.WriteRune('-')
		}
	}
	out := b.String()
	if out == "" {
		return "flow"
	}
	return out
}

// runAgentRun POSTs the Start-node payload to the runner-service URL, which
// kicks off a phlo run. Right invocation for HTTP-triggered outbound agents.
func runAgentRun(cmd *cobra.Command, args []string) error {
	agentUUID := args[0]

	// We need the plivo auth_id to build the runner URL path. Coming from the
	// active REST profile (set by `plivo contacto login` since the single-login
	// merge, or `plivo auth login`).
	plivoProf, _, err := config.Resolve(profileFlag)
	if err != nil {
		return fmt.Errorf("Plivo REST creds required to build the runner URL — run `plivo contacto login` (which sets both REST + Contacto in one shot) or `plivo auth login`: %w", err)
	}

	// Runner URL is environment-dependent. Default to dev; switch to prod
	// if the Contacto profile indicates so.
	runnerBase := "https://dev-runner-service.contactodev.com"
	if contProf, err := config.LoadContacto(); err == nil && contProf.Environment == "prod" {
		runnerBase = "https://us-east-1-runner-service.contacto.com"
	}
	runnerURL := fmt.Sprintf("%s/v1/account/%s/phlo/%s", runnerBase, plivoProf.AuthID, agentUUID)

	// Collect params from --phone-number + repeatable --param key=value.
	params := map[string]any{}
	if agentRunPhoneNumber != "" {
		params["phone_number"] = agentRunPhoneNumber
	}
	for _, kv := range agentRunParams {
		eq := strings.IndexByte(kv, '=')
		if eq < 1 {
			return fmt.Errorf("invalid --param %q, expected key=value", kv)
		}
		params[kv[:eq]] = kv[eq+1:]
	}
	if len(params) == 0 {
		return fmt.Errorf("at least one of --phone-number or --param key=value is required (the Start node's payload_format needs values)")
	}

	body, _ := json.Marshal(params)

	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will POST %s\n  body: %s\n", runnerURL, string(body))
	}
	if dryRunFlag {
		fmt.Fprintf(os.Stderr, "[dry-run] POST %s\n  body: %s\n", runnerURL, string(body))
		return nil
	}

	req, err := http.NewRequestWithContext(cmd.Context(), "POST", runnerURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("runner request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		hint := ""
		if resp.StatusCode == 404 {
			hint = "  hint: phlo not found on the runner. Did you publish the agent? state=ACTIVE + enabled=true required.\n"
		}
		return fmt.Errorf("run failed (HTTP %d):\n%s%s", resp.StatusCode, hint, strings.TrimSpace(string(respBody)))
	}

	if effectiveFormat() == output.FormatJSON {
		writeJSONStdout(respBody)
		return nil
	}
	var parsed map[string]any
	_ = json.Unmarshal(respBody, &parsed)
	msg, _ := parsed["message"].(string)
	if msg == "" {
		msg = "queued"
	}
	runUUID, _ := parsed["phlo_run_uuid"].(string)
	if runUUID == "" {
		runUUID, _ = parsed["request_uuid"].(string)
	}
	fmt.Fprintf(os.Stderr, "✓ %s\n", msg)
	if runUUID != "" {
		fmt.Fprintf(os.Stderr, "  phlo_run_uuid: %s\n", runUUID)
	}
	return nil
}

func parseVibeEvent(data string) (updateType string, payload map[string]any, workflow string) {
	if data == "" {
		return "", nil, ""
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return "", nil, ""
	}
	updateType, _ = raw["update_type"].(string)
	if p, ok := raw["payload"].(map[string]any); ok {
		payload = p
	}
	if w, ok := raw["workflow"].(string); ok {
		workflow = w
	}
	return
}

func renderWorkflowSummary(w io.Writer, workflowJSON string) {
	var wf map[string]any
	if err := json.Unmarshal([]byte(workflowJSON), &wf); err != nil {
		fmt.Fprintln(w, "(workflow JSON unreadable — saving raw)")
		return
	}
	if name, ok := wf["name"].(string); ok && name != "" {
		fmt.Fprintf(w, "Name: %s\n", name)
	}
	if desc, ok := wf["description"].(string); ok && desc != "" {
		fmt.Fprintf(w, "Description: %s\n", desc)
	}
	if nodes, ok := wf["nodes"].([]any); ok {
		fmt.Fprintf(w, "Nodes (%d):\n", len(nodes))
		for _, n := range nodes {
			node, ok := n.(map[string]any)
			if !ok {
				continue
			}
			name, nodeType := extractNodeNameAndType(node)
			fmt.Fprintf(w, "  • %s (%s)\n", name, nodeType)
		}
	}
	edgeCount := 0
	if edges, ok := wf["connections"].([]any); ok {
		edgeCount = len(edges)
	} else if edges, ok := wf["edges"].([]any); ok {
		edgeCount = len(edges)
	}
	fmt.Fprintf(w, "Connections: %d\n", edgeCount)
}

// extractNodeNameAndType reads a name+type pair from either a node_added event
// payload or a node entry in the cumulative workflow. The vibe-agent emits
// React-Flow nodes shaped as either {id, type, data: {meta:{name}, config:{name}}}
// at the payload root, or wrapped under a "node" key. Be liberal in what we
// accept.
func extractNodeNameAndType(v map[string]any) (name, nodeType string) {
	if v == nil {
		return "?", "?"
	}
	if inner, ok := v["node"].(map[string]any); ok {
		return extractNodeNameAndType(inner)
	}
	nodeType, _ = v["type"].(string)
	if data, ok := v["data"].(map[string]any); ok {
		if cfg, ok := data["config"].(map[string]any); ok {
			if n, ok := cfg["name"].(string); ok && n != "" {
				name = n
			}
		}
		if name == "" {
			if meta, ok := data["meta"].(map[string]any); ok {
				if n, ok := meta["name"].(string); ok && n != "" {
					name = n
				}
			}
		}
	}
	if name == "" {
		if n, ok := v["name"].(string); ok && n != "" {
			name = n
		}
	}
	if name == "" {
		name = "(unnamed)"
	}
	if nodeType == "" {
		nodeType = "?"
	}
	return name, nodeType
}

// extractEdgeSourceTarget pulls source / target node ids from either a raw
// edge_added payload or an edges[] entry, supporting both React-Flow's
// {source, target} and PHLO's "<src>.<handle>" / "<tgt>.Input" string form.
func extractEdgeSourceTarget(v map[string]any) (src, dst string) {
	if v == nil {
		return "?", "?"
	}
	if inner, ok := v["edge"].(map[string]any); ok {
		return extractEdgeSourceTarget(inner)
	}
	src, _ = v["source"].(string)
	dst, _ = v["target"].(string)
	// Strip handles for display: "abc.call" → "abc".
	if i := strings.IndexByte(src, '.'); i > 0 {
		src = src[:i]
	}
	if i := strings.IndexByte(dst, '.'); i > 0 {
		dst = dst[:i]
	}
	// Display the short uuid prefix to keep the line readable.
	short := func(s string) string {
		if len(s) > 8 {
			return s[:8]
		}
		return s
	}
	return short(src), short(dst)
}

// translateVibeWorkflow converts the workflow JSON emitted by aiassist's
// vibe-agent stream into the shape PHLO config service's POST /flow endpoint
// expects.
//
// The vibe-agent stream emits a React-Flow shape:
//
//	{
//	  nodes: [{ id, type, data: { meta: {name}, config: {name, ...flat key/values} } }],
//	  edges: [{ id, source, sourceHandle, target }]
//	}
//
// PHLO config service expects a flat canonical shape (mirrors contacto-console's
// `transformFlowData` at apps/web/src/v2/pages/flow/utils/data-transformer.ts:359):
//
//	{
//	  phlo: { name, phlo_type, ... },
//	  nodes: [{ id, name, type, component, top, left, changed: true,
//	            config: { model: <flat config dict> } }],
//	  connections: [{ source: "<src_uuid>.<sourceHandle>",
//	                  target: "<tgt_uuid>.Input",
//	                  data: { id, type: "output" } }]
//	}
//
// Without this translation PHLO rejects the body with "config required field",
// "Node names are repeated" (because every empty name collides), and
// "unknown field: id/sourceHandle" on connections.
func translateVibeWorkflow(rawJSON, overrideName string) (map[string]any, error) {
	var src map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &src); err != nil {
		return nil, fmt.Errorf("parse workflow JSON: %w (raw: %s)", err, rawJSON)
	}

	// --- nodes -----------------------------------------------------------
	rawNodes, _ := src["nodes"].([]any)
	nodes := make([]any, 0, len(rawNodes))
	usedNames := map[string]int{}
	for _, n := range rawNodes {
		node, ok := n.(map[string]any)
		if !ok {
			continue
		}
		data, _ := node["data"].(map[string]any)
		meta, _ := data["meta"].(map[string]any)
		cfg, _ := data["config"].(map[string]any)
		if cfg == nil {
			cfg = map[string]any{}
		}
		nodeType, _ := node["type"].(string)
		// Name precedence: config.name → meta.name → type. PHLO rejects empty
		// or duplicate names, so we suffix collisions with -2, -3, ...
		name, _ := cfg["name"].(string)
		if name == "" {
			name, _ = meta["name"].(string)
		}
		if name == "" {
			name = nodeType
		}
		if usedNames[name] > 0 {
			name = fmt.Sprintf("%s-%d", name, usedNames[name]+1)
		}
		usedNames[name]++
		// Ensure config.name matches the deduped node name.
		cfg["name"] = name
		// Inject --from into any outbound-call nodes the LLM left blank.
		// PHLO rejects an empty config.from on initiate_call with
		// "Please enter a valid Caller ID". This is the most common content
		// hole in vibe-emitted flows for outbound-voice agents.
		if agentCreateFrom != "" && nodeType == "initiate_call" {
			if cur, _ := cfg["from"].(string); cur == "" {
				cfg["from"] = agentCreateFrom
			}
		}
		desc, _ := cfg["description"].(string)
		// React-Flow position → PHLO top/left. LLM doesn't always emit a
		// position; default to a small auto-stagger so the editor doesn't
		// pile every node at (0,0).
		var top, left float64
		if pos, ok := node["position"].(map[string]any); ok {
			if y, ok := pos["y"].(float64); ok {
				top = y
			}
			if x, ok := pos["x"].(float64); ok {
				left = x
			}
		}
		if top == 0 && left == 0 {
			idx := float64(len(nodes))
			top = 120 + idx*140
			left = 200
		}
		nodes = append(nodes, map[string]any{
			"id":          node["id"],
			"name":        name,
			"description": desc,
			"type":        nodeType,
			"component":   nodeType,
			"top":         top,
			"left":        left,
			"changed":     true,
			"config":      map[string]any{"model": cfg},
		})
	}

	// --- connections (edges → "<src>.<handle>" / "<tgt>.Input") ----------
	rawEdges, _ := src["edges"].([]any)
	if rawEdges == nil {
		rawEdges, _ = src["connections"].([]any)
	}
	conns := make([]any, 0, len(rawEdges))
	for _, e := range rawEdges {
		edge, ok := e.(map[string]any)
		if !ok {
			continue
		}
		edgeID, _ := edge["id"].(string)
		srcID, _ := edge["source"].(string)
		sourceHandle, _ := edge["sourceHandle"].(string)
		if sourceHandle == "" {
			sourceHandle = "call"
		}
		tgtID, _ := edge["target"].(string)
		// Skip malformed edges where the LLM didn't bind both endpoints.
		if srcID == "" || tgtID == "" {
			continue
		}
		conns = append(conns, map[string]any{
			"data":   map[string]any{"id": edgeID, "type": "output"},
			"source": srcID + "." + sourceHandle,
			"target": tgtID + ".Input",
		})
	}

	// --- phlo container ---------------------------------------------------
	phlo := map[string]any{}
	if v, ok := src["phlo"].(map[string]any); ok {
		for k, val := range v {
			phlo[k] = val
		}
	}
	if overrideName != "" {
		phlo["name"] = overrideName
	} else if name, ok := src["name"].(string); ok && name != "" {
		phlo["name"] = name
	}
	if name, _ := phlo["name"].(string); name == "" {
		phlo["name"] = "cli-vibe-" + time.Now().UTC().Format("20060102-150405")
	}
	if _, ok := phlo["phlo_type"]; !ok {
		if pt, ok := src["phlo_type"].(string); ok && pt != "" {
			phlo["phlo_type"] = pt
		} else {
			phlo["phlo_type"] = "private"
		}
	}
	for _, k := range []string{"description", "enabled", "voice_ai_config", "event_callbacks", "schedule", "knowledge_base_ids"} {
		if _, already := phlo[k]; already {
			continue
		}
		if v, ok := src[k]; ok && v != nil {
			phlo[k] = v
		}
	}

	out := map[string]any{
		"phlo":        phlo,
		"nodes":       nodes,
		"connections": conns,
	}
	for _, k := range []string{"notes", "is_playground", "global_meta", "editor_metadata"} {
		if v, ok := src[k]; ok && v != nil {
			out[k] = v
		}
	}

	return out, nil
}

// promptForMissingFieldsIfNeeded inspects the LLM-emitted workflow before the
// translator runs, and fills in required fields the LLM left blank. Today only
// covers `from` on initiate_call nodes (the most common content hole).
//
// Interactive mode: prompts the user on stderr for the value.
// Headless mode (-c): returns an error with a --from hint, since stdin is not
// the right place to prompt an automation script.
func promptForMissingFieldsIfNeeded(workflowJSON string, reader *bufio.Reader, headless bool) error {
	if agentCreateFrom != "" {
		return nil // already supplied via flag
	}
	var wf map[string]any
	if err := json.Unmarshal([]byte(workflowJSON), &wf); err != nil {
		return nil // let saveContactoWorkflow surface the parse error
	}
	nodes, _ := wf["nodes"].([]any)
	needsFrom := false
	var firstName string
	for _, n := range nodes {
		node, _ := n.(map[string]any)
		if t, _ := node["type"].(string); t != "initiate_call" {
			continue
		}
		data, _ := node["data"].(map[string]any)
		cfg, _ := data["config"].(map[string]any)
		from, _ := cfg["from"].(string)
		if from != "" {
			continue
		}
		needsFrom = true
		if firstName == "" {
			firstName, _ = cfg["name"].(string)
		}
		// keep scanning so we count all needers, but firstName is enough for the prompt
	}
	if !needsFrom {
		return nil
	}
	if headless {
		return fmt.Errorf("node %q (initiate_call) has no caller-id. Re-run with --from +14695184352 (or any E.164 number on your account)", firstName)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "  ⚠  Node %q makes an outbound call but no caller-id is set.\n", firstName)
	fmt.Fprint(os.Stderr, "  Enter caller-id (E.164, e.g. +14695184352): ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading caller-id: %w", err)
	}
	from := strings.TrimSpace(line)
	if from == "" {
		return fmt.Errorf("caller-id required — re-run with --from on retry")
	}
	if !strings.HasPrefix(from, "+") {
		return fmt.Errorf("caller-id %q must be E.164 (start with +), e.g. +14695184352", from)
	}
	agentCreateFrom = from
	return nil
}

// preflightSaveValidate walks the translated body and surfaces the most common
// content holes the LLM leaves behind, so we error before the server does and
// with a hint the user can act on. PHLO will still validate authoritatively;
// this just catches the friendly cases.
func preflightSaveValidate(body map[string]any) error {
	nodes, _ := body["nodes"].([]any)
	for _, n := range nodes {
		node, _ := n.(map[string]any)
		nodeType, _ := node["type"].(string)
		name, _ := node["name"].(string)
		cfgWrap, _ := node["config"].(map[string]any)
		cfg, _ := cfgWrap["model"].(map[string]any)
		if cfg == nil {
			continue
		}
		if nodeType == "initiate_call" {
			if from, _ := cfg["from"].(string); from == "" {
				return fmt.Errorf("node %q (initiate_call) has no caller-id. Re-run with --from +14695184352 (or any E.164 number on your account)", name)
			}
		}
	}
	return nil
}

// formatPHLOFieldErrors turns PHLO config-service's nested validation envelope
// into a human-readable string. Shape:
//
//	{ "data": { "nodes": { "<uuid>": [{ "config": { "from": ["err msg"] } }] } } }
//
// returns ("", false) when the body isn't a recognised PHLO error shape — caller
// should fall back to the raw body.
func formatPHLOFieldErrors(body []byte, nodes []any) (string, bool) {
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Data == nil {
		return "", false
	}
	nodeErrs, _ := env.Data["nodes"].(map[string]any)
	if len(nodeErrs) == 0 {
		return "", false
	}
	// uuid → display name lookup
	nameByID := map[string]string{}
	for _, n := range nodes {
		if nn, ok := n.(map[string]any); ok {
			id, _ := nn["id"].(string)
			nm, _ := nn["name"].(string)
			nameByID[id] = nm
		}
	}
	var b strings.Builder
	for uuid, raw := range nodeErrs {
		display := nameByID[uuid]
		if display == "" {
			display = uuid
		}
		fmt.Fprintf(&b, "  • node %q:\n", display)
		walk(&b, raw, "      ")
	}
	return b.String(), true
}

func walk(b *strings.Builder, v any, indent string) {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			walk(b, item, indent)
		}
	case map[string]any:
		for k, val := range x {
			fmt.Fprintf(b, "%s%s: ", indent, k)
			if arr, ok := val.([]any); ok {
				msgs := []string{}
				for _, m := range arr {
					if s, ok := m.(string); ok {
						msgs = append(msgs, s)
					}
				}
				if len(msgs) > 0 {
					fmt.Fprintln(b, strings.Join(msgs, "; "))
					continue
				}
			}
			fmt.Fprintln(b)
			walk(b, val, indent+"  ")
		}
	case string:
		fmt.Fprintln(b, indent, x)
	}
}

// runAgentCreateFromTemplate copies an existing agent into a new DRAFT with a
// fresh uuid + the user-supplied name. Uses PHLO config service's
// POST /phlo/copy/<uuid> endpoint with body {"phlo_name": "..."}.
func runAgentCreateFromTemplate(cmd *cobra.Command, c *contacto.Client) error {
	name := agentCreateName
	if name == "" {
		name = "plivo-cli-agent-" + time.Now().UTC().Format("20060102-150405")
	}
	if len(name) < 4 {
		return fmt.Errorf("agent name must be at least 4 characters")
	}

	body := map[string]any{"phlo_name": name}
	resp, err := c.Do(cmd.Context(), "POST", "/v1/contacto-core/contacto-config/phlo/copy/"+agentCreateFromTemplate, body)
	if err != nil {
		return err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return fmt.Errorf("copy failed (HTTP %d): %s", resp.Status, strings.TrimSpace(string(resp.Body)))
	}

	var created map[string]any
	_ = json.Unmarshal(resp.Body, &created)
	uuidStr := ""
	if d, ok := created["data"].(map[string]any); ok {
		if u, ok := d["phlo_id"].(string); ok {
			uuidStr = u
		}
	}
	if uuidStr != "" {
		fmt.Fprintf(os.Stderr, "✓ Created agent: %s\n", uuidStr)
		fmt.Fprintf(os.Stderr, "  name:  %s\n", name)
		fmt.Fprintf(os.Stderr, "  state: DRAFT (copied from %s)\n", agentCreateFromTemplate)
		fmt.Fprintf(os.Stderr, "  open:  https://dev-us-east-1-console.contactodev.com/v2/agent/builder/%s\n", uuidStr)
	}
	if effectiveFormat() == output.FormatJSON {
		writeJSONStdout(resp.Body)
	}
	return nil
}

// createStubPhlo mirrors contacto-console's getNewFlowData → flowService.upsertFlow.
// Currently unused — kept for the next iteration where we figure out the missing
// vibe-agent signal that puts the LLM in build mode.
//
//nolint:unused
func createStubPhlo(cmd *cobra.Command, c *contacto.Client, phloType, name string) (string, error) {
	triggerNodeID := uuid.NewString()
	body := map[string]any{
		"phlo": map[string]any{
			"name":        name,
			"group":       "public",
			"enabled":     false,
			"description": "Created via Plivo Shell CLI",
			"changed":     true,
			"redact":      false,
			"state":       "DRAFT",
			"phlo_type":   phloType,
		},
		"nodes": []map[string]any{
			{
				"id":        triggerNodeID,
				"type":      "select_trigger",
				"top":       50,
				"left":      550,
				"name":      "Select Trigger",
				"component": "select_trigger",
				"config": map[string]any{
					"model": []map[string]any{
						{"data": map[string]any{"name": "Select Trigger"}, "type": "string"},
					},
				},
				"changed": false,
			},
		},
		"connections": []any{},
		"notes":       map[string]any{"notes": []any{}},
		"global_meta": map[string]any{},
	}

	resp, err := c.Do(cmd.Context(), "POST", phloListPath, body)
	if err != nil {
		return "", err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return "", fmt.Errorf("stub PHLO create failed (HTTP %d): %s", resp.Status, strings.TrimSpace(string(resp.Body)))
	}
	var created map[string]any
	if err := json.Unmarshal(resp.Body, &created); err != nil {
		return "", fmt.Errorf("parse create response: %w", err)
	}
	inner := created
	if d, ok := created["data"].(map[string]any); ok {
		inner = d
	}
	if phlo, ok := inner["phlo"].(map[string]any); ok {
		for _, k := range []string{"uuid", "id"} {
			if u, ok := phlo[k].(string); ok && u != "" {
				return u, nil
			}
		}
	}
	for _, k := range []string{"uuid", "id"} {
		if u, ok := inner[k].(string); ok && u != "" {
			return u, nil
		}
	}
	return "", fmt.Errorf("stub PHLO created but uuid not in response: %s", string(resp.Body))
}

func saveContactoWorkflow(cmd *cobra.Command, c *contacto.Client, workflowJSON, overrideName, phloUUID string) error {
	body, err := translateVibeWorkflow(workflowJSON, overrideName)
	if err != nil {
		return err
	}

	// Pre-validate so we surface a clear error instead of PHLO's verbose 400.
	if nodes, _ := body["nodes"].([]any); len(nodes) == 0 {
		return fmt.Errorf("vibe agent produced no nodes — workflow is empty; LLM likely asked for approval but never built the actual flow. Try '[a] approve' to nudge it into the build stage")
	}
	if err := preflightSaveValidate(body); err != nil {
		return err
	}

	if logLevel == "debug" {
		if rendered, err := json.MarshalIndent(body, "", "  "); err == nil {
			fmt.Fprintf(os.Stderr, "[debug] POST body to PHLO config:\n%s\n", string(rendered))
		}
	}

	// Upsert into the pre-allocated phlo_uuid (matches contacto-console's
	// flowService.upsertFlow → POST /v1/contacto-core/contacto-config/phlo/<uuid>).
	savePath := phloListPath
	if phloUUID != "" {
		savePath = phloItemPath + phloUUID
	}
	resp, err := c.Do(cmd.Context(), "POST", savePath, body)
	if err != nil {
		return err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		// Try to surface PHLO's per-field validation errors readably; fall
		// back to the raw body when the shape is unfamiliar.
		nodes, _ := body["nodes"].([]any)
		if pretty, ok := formatPHLOFieldErrors(resp.Body, nodes); ok {
			return fmt.Errorf("save failed (HTTP %d) — PHLO rejected these fields:\n%s", resp.Status, pretty)
		}
		return fmt.Errorf("save failed (HTTP %d): %s", resp.Status, strings.TrimSpace(string(resp.Body)))
	}

	// PHLO save response shapes seen in dev:
	//   {data: {phlo: {id: <uuid>, ...}, nodes: [...]}, ...}    (the wrapping case)
	//   {phlo: {uuid|id: ...}, ...}                              (legacy)
	//   {uuid|id: ...}                                           (flat fallback)
	var created map[string]any
	_ = json.Unmarshal(resp.Body, &created)
	uuidStr := findPhloUUID(created)
	if uuidStr != "" {
		fmt.Fprintf(os.Stderr, "✓ Created agent: %s\n", uuidStr)
		// Also print the runner URL — handy for `plivo call make --answer-url`.
		if data, ok := created["data"].(map[string]any); ok {
			if phlo, ok := data["phlo"].(map[string]any); ok {
				if u, ok := phlo["url"].(string); ok && u != "" {
					fmt.Fprintf(os.Stderr, "  runner: %s\n", u)
				}
			}
		}
	} else {
		fmt.Fprintln(os.Stderr, "✓ Agent created (uuid not parsed from response)")
	}
	if effectiveFormat() == output.FormatJSON {
		writeJSONStdout(resp.Body)
	}
	return nil
}

// findPhloUUID walks the various save-response shapes and returns the agent
// UUID, or "" if no recognisable shape matched.
func findPhloUUID(v map[string]any) string {
	// data.phlo.{id|uuid}
	if data, ok := v["data"].(map[string]any); ok {
		if phlo, ok := data["phlo"].(map[string]any); ok {
			if u, _ := phlo["id"].(string); u != "" {
				return u
			}
			if u, _ := phlo["uuid"].(string); u != "" {
				return u
			}
		}
	}
	// phlo.{uuid|id}
	if phlo, ok := v["phlo"].(map[string]any); ok {
		if u, _ := phlo["uuid"].(string); u != "" {
			return u
		}
		if u, _ := phlo["id"].(string); u != "" {
			return u
		}
	}
	// flat {uuid|id}
	if u, _ := v["uuid"].(string); u != "" {
		return u
	}
	if u, _ := v["id"].(string); u != "" {
		return u
	}
	return ""
}

// runAgentAttach uses the Plivo classic auth (api.plivo.com) to update an
// Application's answer/message/hangup URLs to point at the agent's runner URL.
// This is the same mechanism the Contacto console uses; pure public Plivo REST.
func runAgentAttach(cmd *cobra.Command, args []string) error {
	agentUUID := args[0]
	if agentAttachApp == "" && agentAttachNumber == "" {
		return fmt.Errorf("specify either --app <id> or --number <e164>")
	}
	if agentAttachApp != "" && agentAttachNumber != "" {
		return fmt.Errorf("specify only one of --app or --number")
	}

	client, _, err := getClient()
	if err != nil {
		return err
	}

	appID := agentAttachApp
	if appID == "" {
		var n api.Number
		apiErr, err := client.Do("GET", client.AccountURL("Number", agentAttachNumber), nil, nil, &n)
		if err != nil {
			return err
		}
		if apiErr != nil {
			return apiErr
		}
		appID = n.ResolvedAppID()
		if appID == "" {
			return fmt.Errorf("number %s has no application attached; create one first", agentAttachNumber)
		}
	}

	runnerURL := fmt.Sprintf(
		"https://%s-runner-svc-pvt.contacto.com/v1/account/%s/phlo/%s#er=%s",
		agentAttachRegion, client.AuthID, agentUUID, regionShortHand(agentAttachRegion),
	)

	body := map[string]any{
		"answer_url":     runnerURL,
		"answer_method":  "POST",
		"hangup_url":     runnerURL,
		"hangup_method":  "POST",
		"message_url":    runnerURL,
		"message_method": "POST",
	}

	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will POST %s with answer/message/hangup URL = %s\n",
			client.AccountURL("Application", appID), runnerURL)
	}

	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("Application", appID), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "✓ Attached agent %s to application %s\n", agentUUID, appID)
	return nil
}

func regionShortHand(region string) string {
	switch strings.ToLower(region) {
	case "us-east-1":
		return "n_virginia"
	case "ap-south-1":
		return "mumbai"
	case "eu-west-1":
		return "ireland"
	default:
		return region
	}
}
