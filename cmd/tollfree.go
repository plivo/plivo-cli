package cmd

import (
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var tollfreeCmd = &cobra.Command{
	Use:     "tollfree",
	Aliases: []string{"tfv"},
	Short:   "Toll-free verification (US TFN messaging compliance)",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var (
	tfvListLimit  int
	tfvListOffset int
	tfvListStatus string
)

var tfvListCmd = &cobra.Command{
	Use:   "list",
	Short: "List toll-free verification profiles",
	RunE:  runTfvList,
}

var tfvGetCmd = &cobra.Command{
	Use:   "get <profile_uuid>",
	Short: "Get a toll-free verification profile by uuid",
	Args:  cobra.ExactArgs(1),
	RunE:  runTfvGet,
}

var (
	tfvSubmitBizName        string
	tfvSubmitBizWebsite     string
	tfvSubmitUseCase        string
	tfvSubmitUseCaseSummary string
	tfvSubmitMessageVolume  string
	tfvSubmitMessageContent string
	tfvSubmitOptIn          string
	tfvSubmitNumbers        string
)

var tfvSubmitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submit a new toll-free verification profile",
	RunE:  runTfvSubmit,
}

func init() {
	tfvListCmd.Flags().IntVar(&tfvListLimit, "limit", 20, "results per page")
	tfvListCmd.Flags().IntVar(&tfvListOffset, "offset", 0, "pagination offset")
	tfvListCmd.Flags().StringVar(&tfvListStatus, "status", "", "filter by status: SUBMITTED|IN_REVIEW|APPROVED|REJECTED")

	tfvSubmitCmd.Flags().StringVar(&tfvSubmitBizName, "business-name", "", "business name (required)")
	_ = tfvSubmitCmd.MarkFlagRequired("business-name")
	tfvSubmitCmd.Flags().StringVar(&tfvSubmitBizWebsite, "business-website", "", "business website URL")
	tfvSubmitCmd.Flags().StringVar(&tfvSubmitUseCase, "use-case", "", "use case category (required)")
	_ = tfvSubmitCmd.MarkFlagRequired("use-case")
	tfvSubmitCmd.Flags().StringVar(&tfvSubmitUseCaseSummary, "use-case-summary", "", "free-text use case summary")
	tfvSubmitCmd.Flags().StringVar(&tfvSubmitMessageVolume, "message-volume", "", "expected volume: LOW|MEDIUM|HIGH")
	tfvSubmitCmd.Flags().StringVar(&tfvSubmitMessageContent, "production-message-content", "", "sample message content")
	tfvSubmitCmd.Flags().StringVar(&tfvSubmitOptIn, "opt-in-workflow", "", "describe how recipients opt in")
	tfvSubmitCmd.Flags().StringVar(&tfvSubmitNumbers, "numbers", "", "comma-separated toll-free numbers to verify")

	tollfreeCmd.AddCommand(tfvListCmd, tfvGetCmd, tfvSubmitCmd)
	messagingSmsCmd.AddCommand(tollfreeCmd)
}

func runTfvList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(tfvListLimit))
	q.Set("offset", strconv.Itoa(tfvListOffset))
	if tfvListStatus != "" {
		q.Set("status", tfvListStatus)
	}
	var resp api.TollFreeVerificationList
	apiErr, err := client.Do("GET", client.AccountURL("TollfreeVerification"), nil, q, &resp)
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
	rows := [][]string{{"PROFILE_UUID", "BUSINESS", "USE_CASE", "VOLUME", "STATUS", "CREATED"}}
	for _, t := range resp.Objects {
		rows = append(rows, []string{t.ProfileUUID, t.BusinessName, t.UseCase, t.MessageVolume, t.Status, t.CreatedAt})
	}
	return output.Table(os.Stdout, rows)
}

func runTfvGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var t api.TollFreeVerification
	apiErr, err := client.Do("GET", client.AccountURL("TollfreeVerification", id), nil, nil, &t)
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
		return output.JSONRaw(os.Stdout, t.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"profile_uuid", t.ProfileUUID},
		{"business_name", t.BusinessName},
		{"business_website", t.BusinessWebsite},
		{"use_case", t.UseCase},
		{"use_case_summary", t.UseCaseSummary},
		{"message_volume", t.MessageVolume},
		{"message_content", t.ProductionMessageContent},
		{"opt_in_workflow", t.OptInWorkflow},
		{"numbers", strings.Join(t.NumbersList, ", ")},
		{"status", t.Status},
		{"created_at", t.CreatedAt},
	})
}

func runTfvSubmit(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"business_name": tfvSubmitBizName,
		"use_case":      tfvSubmitUseCase,
	}
	addIfSet := func(k, v string) {
		if v != "" {
			body[k] = v
		}
	}
	addIfSet("business_website", tfvSubmitBizWebsite)
	addIfSet("use_case_summary", tfvSubmitUseCaseSummary)
	addIfSet("message_volume", tfvSubmitMessageVolume)
	addIfSet("production_message_content", tfvSubmitMessageContent)
	addIfSet("opt_in_workflow", tfvSubmitOptIn)
	if tfvSubmitNumbers != "" {
		body["numbers_list"] = strings.Split(tfvSubmitNumbers, ",")
	}

	var resp struct {
		api.RawBody
		APIID       string `json:"api_id"`
		ProfileUUID string `json:"profile_uuid"`
		Message     string `json:"message"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("TollfreeVerification"), body, nil, &resp)
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
	return output.KV(os.Stdout, [][2]string{
		{"profile_uuid", resp.ProfileUUID},
		{"message", resp.Message},
	})
}
