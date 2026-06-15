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

var maskingCmd = &cobra.Command{
	Use:     "masking",
	Aliases: []string{"mask"},
	Short:   "Number-Masking sessions (privacy-preserving call/SMS bridge)",
}

var maskingSessionCmd = &cobra.Command{
	Use:     "sessions",
	Aliases: []string{"session"},
	Short:   "Manage number-masking sessions",
}

var (
	msCreateFirstParty    string
	msCreateSecondParty   string
	msCreateVirtualNumber string
	msCreateMode          string
	msCreateExpiry        int
	msCreateTimeLimit     int
	msCreateRecord        bool
)

var maskingSessionCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a masking session (spends money — requires --yes)",
	RunE:  runMaskingCreate,
}

var maskingSessionGetCmd = &cobra.Command{
	Use:   "get <session_uuid>",
	Short: "Get a masking session by uuid",
	Args:  cobra.ExactArgs(1),
	RunE:  runMaskingGet,
}

var (
	msListLimit  int
	msListOffset int
)

var maskingSessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List masking sessions",
	RunE:  runMaskingList,
}

var maskingSessionDeleteCmd = &cobra.Command{
	Use:   "delete <session_uuid>",
	Short: "End a masking session (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runMaskingDelete,
}

func init() {
	maskingSessionCreateCmd.Flags().StringVar(&msCreateFirstParty, "first-party", "", "first party E.164 (required)")
	_ = maskingSessionCreateCmd.MarkFlagRequired("first-party")
	maskingSessionCreateCmd.Flags().StringVar(&msCreateSecondParty, "second-party", "", "second party E.164 (required)")
	_ = maskingSessionCreateCmd.MarkFlagRequired("second-party")
	maskingSessionCreateCmd.Flags().StringVar(&msCreateVirtualNumber, "virtual-number", "", "virtual number to use (else allocates from pool)")
	maskingSessionCreateCmd.Flags().StringVar(&msCreateMode, "mode", "both", "voice|sms|both")
	maskingSessionCreateCmd.Flags().IntVar(&msCreateExpiry, "session-expiry", 0, "session lifetime in seconds")
	maskingSessionCreateCmd.Flags().IntVar(&msCreateTimeLimit, "call-time-limit", 0, "max per-call duration in seconds")
	maskingSessionCreateCmd.Flags().BoolVar(&msCreateRecord, "record", false, "record calls in this session")

	maskingSessionListCmd.Flags().IntVar(&msListLimit, "limit", 20, "results per page")
	maskingSessionListCmd.Flags().IntVar(&msListOffset, "offset", 0, "pagination offset")

	maskingSessionCmd.AddCommand(maskingSessionCreateCmd, maskingSessionGetCmd, maskingSessionListCmd, maskingSessionDeleteCmd)
	maskingCmd.AddCommand(maskingSessionCmd)
	numberCmd.AddCommand(maskingCmd)
}

func runMaskingCreate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"first_party":  msCreateFirstParty,
		"second_party": msCreateSecondParty,
		"mode":         msCreateMode,
		"record":       msCreateRecord,
	}
	if msCreateVirtualNumber != "" {
		body["virtual_number"] = msCreateVirtualNumber
	}
	if msCreateExpiry > 0 {
		body["session_expiry"] = msCreateExpiry
	}
	if msCreateTimeLimit > 0 {
		body["call_time_limit"] = msCreateTimeLimit
	}

	proceed, dryRun, gerr := guardSpend("create masking session")
	if !proceed {
		return gerr
	}
	applyDryRun(client, dryRun)

	var resp struct {
		APIID         string `json:"api_id"`
		SessionUUID   string `json:"session_uuid"`
		VirtualNumber string `json:"virtual_number"`
		Message       string `json:"message"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("Masking", "Session"), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRun {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, resp, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"session_uuid", resp.SessionUUID},
		{"virtual_number", resp.VirtualNumber},
		{"message", resp.Message},
	})
}

func runMaskingGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var s api.MaskingSession
	apiErr, err := client.Do("GET", client.AccountURL("Masking", "Session", id), nil, nil, &s)
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
		return output.JSONSuccess(os.Stdout, s, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"session_uuid", s.SessionUUID},
		{"first_party", s.FirstParty},
		{"second_party", s.SecondParty},
		{"virtual_number", s.VirtualNumber},
		{"mode", s.Mode},
		{"status", s.Status},
		{"created_on", s.CreatedOn},
		{"expires_on", s.ExpiresOn},
	})
}

func runMaskingList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(msListLimit))
	q.Set("offset", strconv.Itoa(msListOffset))
	var resp api.MaskingSessionList
	apiErr, err := client.Do("GET", client.AccountURL("Masking", "Session"), nil, q, &resp)
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
	rows := [][]string{{"SESSION_UUID", "FIRST", "SECOND", "VIRTUAL", "MODE", "STATUS"}}
	for _, s := range resp.Objects {
		rows = append(rows, []string{s.SessionUUID, s.FirstParty, s.SecondParty, s.VirtualNumber, s.Mode, s.Status})
	}
	return output.Table(os.Stdout, rows)
}

func runMaskingDelete(cmd *cobra.Command, args []string) error {
	id := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("end masking session " + id)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("Masking", "Session", id), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Ended masking session %s\n", id)
	return nil
}
