package cmd

import (
	"net/url"
	"os"
	"strconv"

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
	Short: "List toll-free verification requests",
	Long: `List toll-free verification requests. Without a date filter the API returns
only those created in the last 7 days. For a wider window give both ends, at
most 2 years apart:

  plivo api GET /TollfreeVerification/ --query created__gte=2026-01-01 --query created__lte=2026-06-30

tollfree get <uuid> reads a request of any age.`,
	Args: cobra.NoArgs,
	RunE: runTfvList,
}

var tfvGetCmd = &cobra.Command{
	Use:   "get <uuid>",
	Short: "Get a toll-free verification request by its uuid",
	Args:  cobra.ExactArgs(1),
	RunE:  runTfvGet,
}

var (
	tfvSubmitProfile        string
	tfvSubmitNumber         string
	tfvSubmitUseCase        string
	tfvSubmitUseCaseSummary string
	tfvSubmitMessageContent string
	tfvSubmitOptInType      string
	tfvSubmitOptInImageURL  string
	tfvSubmitMessageVolume  string
	tfvSubmitTermsLink      string
	tfvSubmitPrivacyLink    string
)

var tfvSubmitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submit a toll-free number for verification",
	Long: `Submit one toll-free number for verification. The business's details come
from a profile, the kind 10DLC brands use: list yours with
plivo api GET /Profile/ (fields: plivo docs show messaging/api/10dlc/profile).`,
	Args: cobra.NoArgs,
	RunE: runTfvSubmit,
}

func init() {
	registerListFlags(tfvListCmd, &tfvListLimit, &tfvListOffset)
	tfvListCmd.Flags().StringVar(&tfvListStatus, "status", "", "filter by status: "+oneOf(tollfreeStatuses))

	for _, f := range []struct {
		value       *string
		name, usage string
	}{
		{&tfvSubmitProfile, "profile-uuid", "the profile holding the business's details"},
		{&tfvSubmitNumber, "number", "the toll-free number to verify, E.164"},
		{&tfvSubmitUseCase, "use-case", "use case, or several comma-separated, e.g. 2FA,CUSTOMER_CARE"},
		{&tfvSubmitUseCaseSummary, "use-case-summary", "how the number's messages are used"},
		{&tfvSubmitMessageContent, "production-message-content", "sample messages you will send"},
		{&tfvSubmitOptInType, "opt-in-type", "how recipients opt in: VERBAL|WEB_FORM|PAPER_FORM|VIA_TEXT|MOBILE_QR_CODE"},
		{&tfvSubmitOptInImageURL, "opt-in-image-url", "URL of an image or document that shows the opt-in"},
		{&tfvSubmitMessageVolume, "message-volume", "messages a month: 10, 100, 1000, 10000, 100000, 250000, 500000, " +
			"750000, 1000000, 5000000 or 10,000,000+"},
		{&tfvSubmitTermsLink, "terms-and-conditions-link", "public URL of your terms and conditions"},
		{&tfvSubmitPrivacyLink, "privacy-policy-link", "public URL of your privacy policy"},
	} {
		tfvSubmitCmd.Flags().StringVar(f.value, f.name, "", f.usage+" (required)")
		_ = tfvSubmitCmd.MarkFlagRequired(f.name)
	}

	tollfreeCmd.AddCommand(tfvListCmd, tfvGetCmd, tfvSubmitCmd)
	messagingSmsCmd.AddCommand(tollfreeCmd)
}

func runTfvList(cmd *cobra.Command, args []string) error {
	if err := validateEnum("status", &tfvListStatus, tollfreeStatuses...); err != nil {
		return err
	}
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
	if err := fetchList(client, client.AccountURL("TollfreeVerification"), q, "objects", &resp); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return listJSON(os.Stdout, resp.Raw(), "objects")
	}
	rows := [][]string{{"UUID", "NUMBER", "USECASE", "VOLUME", "STATUS", "CREATED"}}
	for _, t := range resp.Objects {
		rows = append(rows, []string{t.UUID, t.Number, t.Usecase, t.Volume, t.Status, t.Created})
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
		{"uuid", t.UUID},
		{"profile_uuid", t.ProfileUUID},
		{"number", t.Number},
		{"usecase", t.Usecase},
		{"usecase_summary", t.UsecaseSummary},
		{"message_sample", t.MessageSample},
		{"optin_type", t.OptinType},
		{"optin_image_url", t.OptinImageURL},
		{"volume", t.Volume},
		{"status", t.Status},
		{"error_message", t.ErrorMessage},
		{"created", t.Created},
		{"last_modified", t.LastModified},
	})
}

func runTfvSubmit(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"profile_uuid":              tfvSubmitProfile,
		"number":                    tfvSubmitNumber,
		"usecase":                   tfvSubmitUseCase,
		"usecase_summary":           tfvSubmitUseCaseSummary,
		"message_sample":            tfvSubmitMessageContent,
		"optin_type":                tfvSubmitOptInType,
		"optin_image_url":           tfvSubmitOptInImageURL,
		"volume":                    tfvSubmitMessageVolume,
		"terms_and_conditions_link": tfvSubmitTermsLink,
		"privacy_policy_link":       tfvSubmitPrivacyLink,
	}
	var resp struct {
		api.RawBody
		APIID   string `json:"api_id"`
		UUID    string `json:"uuid"`
		Message string `json:"message"`
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
		{"uuid", resp.UUID},
		{"message", resp.Message},
	})
}
