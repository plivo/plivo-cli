package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var mpcCmd = &cobra.Command{
	Use:   "mpc",
	Short: "Multi-Party Calls (MPC) — group voice rooms with dynamic participants",
}

var (
	mpcListLimit  int
	mpcListOffset int
	mpcListStatus string
)

var mpcListCmd = &cobra.Command{
	Use:   "list",
	Short: "List multi-party calls",
	RunE:  runMPCList,
}

var mpcGetCmd = &cobra.Command{
	Use:   "get <mpc_uuid_or_name>",
	Short: "Get an MPC by uuid or friendly_name",
	Args:  cobra.ExactArgs(1),
	RunE:  runMPCGet,
}

var (
	mpcCreateName     string
	mpcCreateMaxParts int
	mpcCreateRecord   bool
)

var mpcCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a multi-party call (spends money — requires --yes)",
	RunE:  runMPCCreate,
}

var mpcEndCmd = &cobra.Command{
	Use:   "end <mpc_uuid_or_name>",
	Short: "End the MPC and hang up all participants (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runMPCEnd,
}

// participant sub-group
var mpcParticipantCmd = &cobra.Command{
	Use:     "participant",
	Aliases: []string{"part"},
	Short:   "Per-participant actions inside an MPC",
}

var (
	mpcPartListLimit  int
	mpcPartListOffset int
)

var mpcPartListCmd = &cobra.Command{
	Use:   "list <mpc_uuid_or_name>",
	Short: "List participants in an MPC",
	Args:  cobra.ExactArgs(1),
	RunE:  runMPCPartList,
}

var (
	mpcPartAddFrom string
	mpcPartAddTo   string
	mpcPartAddRole string
)

var mpcPartAddCmd = &cobra.Command{
	Use:   "add <mpc_uuid_or_name>",
	Short: "Add a participant by dialing out (spends money — requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runMPCPartAdd,
}

var mpcPartKickCmd = &cobra.Command{
	Use:   "kick <mpc_uuid_or_name> <participant_id>",
	Short: "Remove a participant (requires --yes)",
	Args:  cobra.ExactArgs(2),
	RunE:  runMPCPartKick,
}

var mpcPartMuteCmd = &cobra.Command{
	Use:   "mute <mpc_uuid_or_name> <participant_id>",
	Short: "Mute a participant",
	Args:  cobra.ExactArgs(2),
	RunE:  runMPCPartMute,
}

var mpcPartUnmuteCmd = &cobra.Command{
	Use:   "unmute <mpc_uuid_or_name> <participant_id>",
	Short: "Unmute a participant",
	Args:  cobra.ExactArgs(2),
	RunE:  runMPCPartUnmute,
}

var mpcPartHoldCmd = &cobra.Command{
	Use:   "hold <mpc_uuid_or_name> <participant_id>",
	Short: "Put a participant on hold",
	Args:  cobra.ExactArgs(2),
	RunE:  runMPCPartHold,
}

var mpcPartUnholdCmd = &cobra.Command{
	Use:   "unhold <mpc_uuid_or_name> <participant_id>",
	Short: "Take a participant off hold",
	Args:  cobra.ExactArgs(2),
	RunE:  runMPCPartUnhold,
}

func init() {
	mpcListCmd.Flags().IntVar(&mpcListLimit, "limit", 20, "results per page")
	mpcListCmd.Flags().IntVar(&mpcListOffset, "offset", 0, "pagination offset")
	mpcListCmd.Flags().StringVar(&mpcListStatus, "status", "", "filter by status: active|initialized|ended")

	mpcCreateCmd.Flags().StringVar(&mpcCreateName, "name", "", "friendly_name for the MPC (required)")
	_ = mpcCreateCmd.MarkFlagRequired("name")
	mpcCreateCmd.Flags().IntVar(&mpcCreateMaxParts, "max-participants", 0, "cap on simultaneous participants")
	mpcCreateCmd.Flags().BoolVar(&mpcCreateRecord, "record", false, "auto-record the MPC")

	mpcPartListCmd.Flags().IntVar(&mpcPartListLimit, "limit", 20, "results per page")
	mpcPartListCmd.Flags().IntVar(&mpcPartListOffset, "offset", 0, "pagination offset")

	mpcPartAddCmd.Flags().StringVar(&mpcPartAddFrom, "from", "", "source number for the dial-out (required)")
	_ = mpcPartAddCmd.MarkFlagRequired("from")
	mpcPartAddCmd.Flags().StringVar(&mpcPartAddTo, "to", "", "destination number to add (required)")
	_ = mpcPartAddCmd.MarkFlagRequired("to")
	mpcPartAddCmd.Flags().StringVar(&mpcPartAddRole, "role", "agent", "participant role: agent|supervisor|customer")

	mpcParticipantCmd.AddCommand(mpcPartListCmd, mpcPartAddCmd, mpcPartKickCmd,
		mpcPartMuteCmd, mpcPartUnmuteCmd, mpcPartHoldCmd, mpcPartUnholdCmd)
	mpcCmd.AddCommand(mpcListCmd, mpcGetCmd, mpcCreateCmd, mpcEndCmd, mpcParticipantCmd)
	rootCmd.AddCommand(mpcCmd)
}

func runMPCList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(mpcListLimit))
	q.Set("offset", strconv.Itoa(mpcListOffset))
	if mpcListStatus != "" {
		q.Set("status", mpcListStatus)
	}
	var resp api.MPCList
	apiErr, err := client.Do("GET", client.AccountURL("MultiPartyCall"), nil, q, &resp)
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
	rows := [][]string{{"MPC_UUID", "NAME", "STATUS", "BILLING", "CREATED"}}
	for _, m := range resp.Objects {
		rows = append(rows, []string{m.MPCUUID, m.FriendlyName, m.Status, m.BillingType, m.CreatedAt})
	}
	return output.Table(os.Stdout, rows)
}

func runMPCGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var m api.MPC
	apiErr, err := client.Do("GET", mpcResourceURL(client, id), nil, nil, &m)
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
		return output.JSONSuccess(os.Stdout, m, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"mpc_uuid", m.MPCUUID},
		{"friendly_name", m.FriendlyName},
		{"status", m.Status},
		{"billing_type", m.BillingType},
		{"created_at", m.CreatedAt},
		{"end_at", m.EndAt},
	})
}

func runMPCCreate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{"friendly_name": mpcCreateName}
	if mpcCreateMaxParts > 0 {
		body["max_participants"] = mpcCreateMaxParts
	}
	if mpcCreateRecord {
		body["record"] = true
	}

	effectiveDryRun := dryRunFlag || !yesFlag
	if effectiveDryRun {
		client.DryRun = true
		if !dryRunFlag {
			fmt.Fprintln(os.Stderr, "[dry-run] mpc create defaults to dry-run; pass --yes to actually create")
		}
	}

	var resp struct {
		APIID   string `json:"api_id"`
		MPCUUID string `json:"mpc_uuid"`
		Message string `json:"message"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("MultiPartyCall"), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if effectiveDryRun {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, resp, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"mpc_uuid", resp.MPCUUID},
		{"message", resp.Message},
	})
}

func runMPCEnd(cmd *cobra.Command, args []string) error {
	id := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("end MPC " + id)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", mpcResourceURL(client, id), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Ended MPC %s\n", id)
	return nil
}

func runMPCPartList(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(mpcPartListLimit))
	q.Set("offset", strconv.Itoa(mpcPartListOffset))
	var resp api.MPCParticipantList
	apiErr, err := client.Do("GET", mpcResourceURL(client, id)+"Participant/", nil, q, &resp)
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
	rows := [][]string{{"PARTICIPANT_ID", "FROM", "TO", "CALL_UUID", "MUTED", "HOLD", "ROLE"}}
	for _, p := range resp.Objects {
		rows = append(rows, []string{p.ParticipantID, p.From, p.To, p.CallUUID, fmt.Sprintf("%v", p.Muted), fmt.Sprintf("%v", p.OnHold), p.Role})
	}
	return output.Table(os.Stdout, rows)
}

func runMPCPartAdd(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"from": mpcPartAddFrom,
		"to":   mpcPartAddTo,
		"role": mpcPartAddRole,
	}

	effectiveDryRun := dryRunFlag || !yesFlag
	if effectiveDryRun {
		client.DryRun = true
		if !dryRunFlag {
			fmt.Fprintln(os.Stderr, "[dry-run] mpc participant add defaults to dry-run; pass --yes to actually dial")
		}
	}

	var resp api.GenericResponse
	apiErr, err := client.Do("POST", mpcResourceURL(client, id)+"Participant/", body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if effectiveDryRun {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Added participant to %s: %s\n", id, resp.Message)
	return nil
}

func runMPCPartKick(cmd *cobra.Command, args []string) error {
	id, part := args[0], args[1]
	if !yesFlag {
		return clierr.DestructiveRefused("kick participant " + part + " from MPC " + id)
	}
	return mpcParticipantDelete(id, part, "")
}

func runMPCPartMute(cmd *cobra.Command, args []string) error {
	return mpcParticipantPost(args[0], args[1], "Mute", "muted")
}

func runMPCPartUnmute(cmd *cobra.Command, args []string) error {
	return mpcParticipantDelete(args[0], args[1], "Mute")
}

func runMPCPartHold(cmd *cobra.Command, args []string) error {
	return mpcParticipantPost(args[0], args[1], "Hold", "on hold")
}

func runMPCPartUnhold(cmd *cobra.Command, args []string) error {
	return mpcParticipantDelete(args[0], args[1], "Hold")
}

// mpcResourceURL returns the full URL for an MPC, choosing the path segment
// (`/MultiPartyCall/uuid_<uuid>/` vs `/MultiPartyCall/name_<n>/`) based on
// whether `id` looks like a UUID or a friendly name.
func mpcResourceURL(c *api.Client, id string) string {
	// Plivo accepts both. We use the `uuid_<id>/` form when the id has dashes
	// (UUID-shaped), otherwise treat it as a friendly_name.
	if looksLikeUUID(id) {
		return c.AccountURL("MultiPartyCall", "uuid_"+id)
	}
	return c.AccountURL("MultiPartyCall", "name_"+id)
}

func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func mpcParticipantPost(id, part, sub, label string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", mpcResourceURL(client, id)+"Participant/"+part+"/"+sub+"/", nil, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "%s: %s/%s — %s\n", label, id, part, resp.Message)
	return nil
}

func mpcParticipantDelete(id, part, sub string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	urlPath := mpcResourceURL(client, id) + "Participant/" + part + "/"
	if sub != "" {
		urlPath += sub + "/"
	}
	apiErr, err := client.Do("DELETE", urlPath, nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if sub == "" {
		fmt.Fprintf(os.Stderr, "Kicked participant %s from MPC %s\n", part, id)
	} else {
		fmt.Fprintf(os.Stderr, "Cleared %s on %s/%s\n", sub, id, part)
	}
	return nil
}
