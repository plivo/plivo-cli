package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var endpointCmd = &cobra.Command{
	Use:     "endpoints",
	Aliases: []string{"ep", "endpoint"},
	Short:   "Manage SIP endpoints (registered SIP devices/usernames)",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var (
	epListLimit  int
	epListOffset int
)

var epListCmd = &cobra.Command{
	Use:   "list",
	Short: "List SIP endpoints",
	Args:  cobra.NoArgs,
	RunE:  runEndpointList,
}

var epGetCmd = &cobra.Command{
	Use:   "get <endpoint_id>",
	Short: "Get an endpoint by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runEndpointGet,
}

var (
	epCreateUsername string
	epCreatePassword string
	epCreateAlias    string
	epCreateAppID    string
	epCreateAgent    string
)

var epCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a SIP endpoint",
	Long: `Create a SIP endpoint.

With --agent, a Plivo CX agent answers the calls the endpoint makes: the
endpoint is created, then linked to the agent. If the link fails, the endpoint
is kept, and the error gives the command that links it.`,
	Args: cobra.NoArgs,
	RunE: runEndpointCreate,
}

var (
	epUpdatePassword string
	epUpdateAlias    string
	epUpdateAppID    string
	epUpdateAgent    string
)

var epUpdateCmd = &cobra.Command{
	Use:   "update <endpoint_id>",
	Short: "Update a SIP endpoint",
	Long: `Update a SIP endpoint.

With --agent, a Plivo CX agent answers the calls the endpoint makes, in place
of its application. Any other change is made first. To route the endpoint to
an application again, pass --app-id.`,
	Args: cobra.ExactArgs(1),
	RunE: runEndpointUpdate,
}

var epDeleteCmd = &cobra.Command{
	Use:   "delete <endpoint_id>",
	Short: "Delete a SIP endpoint (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runEndpointDelete,
}

func init() {
	registerListFlags(epListCmd, &epListLimit, &epListOffset)

	epCreateCmd.Flags().StringVar(&epCreateUsername, "username", "", "SIP username (required)")
	_ = epCreateCmd.MarkFlagRequired("username")
	epCreateCmd.Flags().StringVar(&epCreatePassword, "password", "", "SIP password (required)")
	_ = epCreateCmd.MarkFlagRequired("password")
	epCreateCmd.Flags().StringVar(&epCreateAlias, "alias", "", "human-friendly label (required)")
	_ = epCreateCmd.MarkFlagRequired("alias")
	epCreateCmd.Flags().StringVar(&epCreateAppID, "app-id", "", "application to attach")
	epCreateCmd.Flags().StringVar(&epCreateAgent, "agent", "", agentFlagUsage)

	epUpdateCmd.Flags().StringVar(&epUpdatePassword, "password", "", "new password")
	epUpdateCmd.Flags().StringVar(&epUpdateAlias, "alias", "", "new alias")
	epUpdateCmd.Flags().StringVar(&epUpdateAppID, "app-id", "", "new application id")
	epUpdateCmd.Flags().StringVar(&epUpdateAgent, "agent", "", agentFlagUsage)

	endpointCmd.AddCommand(epListCmd, epGetCmd, epCreateCmd, epUpdateCmd, epDeleteCmd)
	voiceCmd.AddCommand(endpointCmd)
}

func runEndpointList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(epListLimit))
	q.Set("offset", strconv.Itoa(epListOffset))
	var resp api.EndpointList
	if err := fetchList(client, client.AccountURL("Endpoint"), q, "objects", &resp); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return listJSON(os.Stdout, resp.Raw(), "objects")
	}
	rows := [][]string{{"ENDPOINT_ID", "USERNAME", "ALIAS", "SIP_URI", "APP_ID"}}
	for _, e := range resp.Objects {
		rows = append(rows, []string{e.EndpointID, e.Username, e.Alias, e.SipURI, e.ResolvedAppID()})
	}
	return output.Table(os.Stdout, rows)
}

func runEndpointGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var e api.Endpoint
	apiErr, err := client.Do("GET", client.AccountURL("Endpoint", id), nil, nil, &e)
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
		return output.JSONRaw(os.Stdout, e.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"endpoint_id", e.EndpointID},
		{"username", e.Username},
		{"alias", e.Alias},
		{"sip_uri", e.SipURI},
		{"app_id", e.ResolvedAppID()},
		{"sub_account", e.Subaccount},
	})
}

const agentFlagUsage = "CX agent (agent_uuid) to answer the endpoint's calls; not with --app-id"

// endpointAgent returns --agent, or "" when it is not given. It refuses one
// given with --app-id, or that is no UUID, before anything is sent: a create
// would otherwise leave an endpoint behind.
func endpointAgent(cmd *cobra.Command, agentID string) (string, error) {
	switch {
	case !cmd.Flags().Changed("agent"):
		return "", nil
	case cmd.Flags().Changed("app-id"):
		return "", clierr.BadFlag("agent", "cannot be combined with --app-id: the agent's application becomes the endpoint's app_id")
	case !looksLikeUUID(agentID):
		e := clierr.BadFlag("agent", fmt.Sprintf("expected an agent UUID, got %q", agentID))
		e.Hint = "`plivo agents list` shows your agents' IDs."
		return "", e
	}
	return agentID, nil
}

// linkEndpointAgent has the agent answer the endpoint's calls: Plivo sets the
// agent's application as the endpoint's app_id. done is what this run already
// did to the endpoint ("created", "updated", or ""); a failed link after it
// names the endpoint and the command that links it.
func linkEndpointAgent(client *api.Client, endpointID, agentID, done string) (api.GenericResponse, error) {
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("AgentFlow", agentID, "Endpoint"),
		map[string]any{"endpoint_id": endpointID}, nil, &resp)
	if err == nil && apiErr == nil {
		return resp, nil
	}
	if apiErr == nil {
		apiErr = clierr.Wrap(err)
	}
	if done != "" {
		apiErr.Message = fmt.Sprintf("Endpoint %s was %s but not linked to agent %s: %s", endpointID, done, agentID, apiErr.Message)
		apiErr.Hint = fmt.Sprintf("Link it with `plivo voice endpoints update %s --agent %s`.", endpointID, agentID)
	}
	return resp, apiErr
}

func runEndpointCreate(cmd *cobra.Command, args []string) error {
	agentID, err := endpointAgent(cmd, epCreateAgent)
	if err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"username": epCreateUsername,
		"password": epCreatePassword,
		"alias":    epCreateAlias,
	}
	if epCreateAppID != "" {
		body["app_id"] = epCreateAppID
	}
	var resp struct {
		api.RawBody
		APIID      string `json:"api_id"`
		EndpointID string `json:"endpoint_id"`
		Username   string `json:"username"`
		Alias      string `json:"alias"`
		Message    string `json:"message"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("Endpoint"), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	var link api.GenericResponse
	if agentID != "" {
		endpointID := resp.EndpointID
		if dryRunFlag {
			endpointID = "<endpoint_id>" // the create was not sent, so there is no ID yet
		}
		if link, err = linkEndpointAgent(client, endpointID, agentID, "created"); err != nil {
			return err
		}
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, resp.Raw())
	}
	if agentID != "" {
		fmt.Fprintf(os.Stderr, "Linked endpoint %s to agent %s: %s\n", resp.EndpointID, agentID, link.Message)
	}
	return output.KV(os.Stdout, [][2]string{
		{"endpoint_id", resp.EndpointID},
		{"username", resp.Username},
		{"message", resp.Message},
	})
}

func runEndpointUpdate(cmd *cobra.Command, args []string) error {
	id := args[0]
	agentID, err := endpointAgent(cmd, epUpdateAgent)
	if err != nil {
		return err
	}
	body := map[string]any{}
	if epUpdatePassword != "" {
		body["password"] = epUpdatePassword
	}
	if epUpdateAlias != "" {
		body["alias"] = epUpdateAlias
	}
	if epUpdateAppID != "" {
		body["app_id"] = epUpdateAppID
	}
	if len(body) == 0 && agentID == "" {
		return clierr.BadInput("at least one of --password, --alias, --app-id, --agent required")
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.GenericResponse
	var human []string
	done := ""
	if len(body) > 0 {
		apiErr, err := client.Do("POST", client.AccountURL("Endpoint", id), body, nil, &resp)
		if err != nil {
			return err
		}
		if apiErr != nil {
			return apiErr
		}
		human = append(human, fmt.Sprintf("Updated endpoint %s: %s", id, resp.Message))
		done = "updated"
	}
	if agentID != "" {
		if resp, err = linkEndpointAgent(client, id, agentID, done); err != nil {
			return err
		}
		human = append(human, fmt.Sprintf("Linked endpoint %s to agent %s: %s", id, agentID, resp.Message))
	}
	if dryRunFlag {
		return nil
	}
	return emitResponse(strings.Join(human, "\n"), resp.Raw())
}

func runEndpointDelete(cmd *cobra.Command, args []string) error {
	id := args[0]
	if err := confirmDestructive("delete endpoint " + id); err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("Endpoint", id), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	return emitWrite(fmt.Sprintf("Deleted endpoint %s", id), map[string]any{"endpoint_id": id, "deleted": true})
}
