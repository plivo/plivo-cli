package cmd

import (
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var campaignCmd = &cobra.Command{
	Use:     "campaigns",
	Aliases: []string{"camp", "campaign"},
	Short:   "10DLC campaign registration (use cases for a brand)",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var (
	campListLimit  int
	campListOffset int
	campListBrand  string
)

var campListCmd = &cobra.Command{
	Use:   "list",
	Short: "List campaigns",
	Args:  cobra.NoArgs,
	RunE:  runCampaignList,
}

var campGetCmd = &cobra.Command{
	Use:   "get <campaign_id>",
	Short: "Get a campaign by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runCampaignGet,
}

var (
	campCreateAlias        string
	campCreateBrand        string
	campCreateVertical     string
	campCreateUsecase      string
	campCreateSubUsecases  string
	campCreateDescription  string
	campCreateMessageFlow  string
	campCreateSample1      string
	campCreateSample2      string
	campCreateHelpKeywords string
	campCreateHelpMessage  string
	campCreateOptInKW      string
	campCreateOptInMsg     string
	campCreateOptOutKW     string
	campCreateOptOutMsg    string
	campCreateEmbedLink    bool
	campCreateEmbedPhone   bool
	campCreateAgeGated     bool
	campCreateDirectLend   bool
	campCreateAffiliate    bool
	campCreateSubOptin     bool
	campCreateSubOptout    bool
	campCreateSubHelp      bool
)

var campCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Register a new campaign (spends money — TCR fee, requires --yes)",
	Args:  cobra.NoArgs,
	RunE:  runCampaignCreate,
}

var (
	campUpdateDescription string
	campUpdateMessageFlow string
	campUpdateSample1     string
	campUpdateSample2     string
)

var campUpdateCmd = &cobra.Command{
	Use:   "update <campaign_id>",
	Short: "Update a failed campaign (spends money — carrier review fee, requires --yes)",
	Long: `Update a campaign and send it to the carriers for review again (spends money:
USD 15 per the API reference; requires --yes, or --dry-run to preview).

The API reference allows updates only for a campaign whose registration
failed, registration_status FAILED. Its fields:
plivo docs show messaging/api/10dlc/campaign`,
	Args: cobra.ExactArgs(1),
	RunE: runCampaignUpdate,
}

func init() {
	registerListFlags(campListCmd, &campListLimit, &campListOffset)
	campListCmd.Flags().StringVar(&campListBrand, "brand-id", "", "filter by brand_id")

	campCreateCmd.Flags().StringVar(&campCreateAlias, "alias", "", "human-friendly alias (required)")
	_ = campCreateCmd.MarkFlagRequired("alias")
	campCreateCmd.Flags().StringVar(&campCreateBrand, "brand-id", "", "brand to register under (required)")
	_ = campCreateCmd.MarkFlagRequired("brand-id")
	campCreateCmd.Flags().StringVar(&campCreateVertical, "vertical", "", "industry, e.g. TECHNOLOGY, RETAIL, HEALTHCARE (required)")
	_ = campCreateCmd.MarkFlagRequired("vertical")
	campCreateCmd.Flags().StringVar(&campCreateUsecase, "usecase", "", "primary use case, e.g. MARKETING, MIXED (required)")
	_ = campCreateCmd.MarkFlagRequired("usecase")
	campCreateCmd.Flags().StringVar(&campCreateSubUsecases, "sub-usecases", "", "comma-separated sub use cases")
	campCreateCmd.Flags().StringVar(&campCreateDescription, "description", "", "what this campaign does (required)")
	_ = campCreateCmd.MarkFlagRequired("description")
	campCreateCmd.Flags().StringVar(&campCreateMessageFlow, "message-flow", "", "describe how recipients opt in (required)")
	_ = campCreateCmd.MarkFlagRequired("message-flow")
	campCreateCmd.Flags().StringVar(&campCreateSample1, "sample-message-1", "", "sample message (required)")
	_ = campCreateCmd.MarkFlagRequired("sample-message-1")
	campCreateCmd.Flags().StringVar(&campCreateSample2, "sample-message-2", "", "second sample message (required)")
	_ = campCreateCmd.MarkFlagRequired("sample-message-2")
	campCreateCmd.Flags().StringVar(&campCreateHelpKeywords, "help-keywords", "HELP", "comma-separated help keywords")
	campCreateCmd.Flags().StringVar(&campCreateHelpMessage, "help-message", "", "auto-reply to HELP (required)")
	_ = campCreateCmd.MarkFlagRequired("help-message")
	campCreateCmd.Flags().StringVar(&campCreateOptInKW, "opt-in-keywords", "START", "opt-in keywords")
	campCreateCmd.Flags().StringVar(&campCreateOptInMsg, "opt-in-message", "", "opt-in confirmation message")
	campCreateCmd.Flags().StringVar(&campCreateOptOutKW, "opt-out-keywords", "STOP", "opt-out keywords")
	campCreateCmd.Flags().StringVar(&campCreateOptOutMsg, "opt-out-message", "", "auto-reply to STOP (required)")
	_ = campCreateCmd.MarkFlagRequired("opt-out-message")
	campCreateCmd.Flags().BoolVar(&campCreateEmbedLink, "embedded-link", false, "messages contain links")
	campCreateCmd.Flags().BoolVar(&campCreateEmbedPhone, "embedded-phone", false, "messages contain phone numbers")
	campCreateCmd.Flags().BoolVar(&campCreateAgeGated, "age-gated", false, "campaign targets adult content")
	campCreateCmd.Flags().BoolVar(&campCreateDirectLend, "direct-lending", false, "direct-lending arrangements")
	campCreateCmd.Flags().BoolVar(&campCreateAffiliate, "affiliate-marketing", false, "affiliate marketing campaign")
	// Declarations the API takes only as true: required, so each is made on purpose.
	campCreateCmd.Flags().BoolVar(&campCreateSubOptin, "subscriber-optin", false, "declare that you collect and process opt-ins (required)")
	campCreateCmd.Flags().BoolVar(&campCreateSubOptout, "subscriber-optout", false, "declare that you process opt-outs (required)")
	campCreateCmd.Flags().BoolVar(&campCreateSubHelp, "subscriber-help", false, "declare that a HELP reply tells recipients how to reach you (required)")
	for _, f := range []string{"subscriber-optin", "subscriber-optout", "subscriber-help"} {
		_ = campCreateCmd.MarkFlagRequired(f)
	}

	campUpdateCmd.Flags().StringVar(&campUpdateDescription, "description", "", "new description")
	campUpdateCmd.Flags().StringVar(&campUpdateMessageFlow, "message-flow", "", "new message flow")
	campUpdateCmd.Flags().StringVar(&campUpdateSample1, "sample-message-1", "", "new sample 1")
	campUpdateCmd.Flags().StringVar(&campUpdateSample2, "sample-message-2", "", "new sample 2")

	campaignCmd.AddCommand(campListCmd, campGetCmd, campCreateCmd, campUpdateCmd)
	sms10dlcCmd.AddCommand(campaignCmd)
}

func runCampaignList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(campListLimit))
	q.Set("offset", strconv.Itoa(campListOffset))
	if campListBrand != "" {
		q.Set("brand_id", campListBrand)
	}
	var resp api.Campaign10DLCList
	if err := fetchList(client, client.AccountURL("10dlc", "Campaign"), q, "campaigns", &resp); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return listJSON(os.Stdout, resp.Raw(), "campaigns")
	}
	rows := [][]string{{"CAMPAIGN_ID", "ALIAS", "BRAND_ID", "USECASE", "STATUS"}}
	for _, c := range resp.Campaigns {
		rows = append(rows, []string{c.CampaignID, c.CampaignAlias, c.BrandID, c.Usecase, c.RegistrationStatus})
	}
	return output.Table(os.Stdout, rows)
}

func runCampaignGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.Campaign10DLCResponse
	apiErr, err := client.Do("GET", client.AccountURL("10dlc", "Campaign", id), nil, nil, &resp)
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
	c := resp.Campaign
	return output.KV(os.Stdout, [][2]string{
		{"campaign_id", c.CampaignID},
		{"alias", c.CampaignAlias},
		{"brand_id", c.BrandID},
		{"usecase", c.Usecase},
		{"sub_usecases", c.SubUsecase},
		{"vertical", c.Vertical},
		{"status", c.RegistrationStatus},
		{"error_reason", c.ErrorReason},
		{"description", c.Description},
		{"message_flow", c.MessageFlow},
		{"help_keywords", c.HelpKeywords},
		{"opt_in_keywords", c.OptinKeywords},
		{"opt_out_keywords", c.OptoutKeywords},
		{"created_at", c.CreatedAt},
	})
}

func runCampaignCreate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"campaign_alias":      campCreateAlias,
		"brand_id":            campCreateBrand,
		"vertical":            campCreateVertical,
		"usecase":             campCreateUsecase,
		"description":         campCreateDescription,
		"message_flow":        campCreateMessageFlow,
		"sample1":             campCreateSample1,
		"sample2":             campCreateSample2,
		"help_message":        campCreateHelpMessage,
		"optout_message":      campCreateOptOutMsg,
		"embedded_link":       campCreateEmbedLink,
		"embedded_phone":      campCreateEmbedPhone,
		"age_gated":           campCreateAgeGated,
		"direct_lending":      campCreateDirectLend,
		"affiliate_marketing": campCreateAffiliate,
		"subscriber_optin":    campCreateSubOptin,
		"subscriber_optout":   campCreateSubOptout,
		"subscriber_help":     campCreateSubHelp,
	}
	addIfSet := func(k, v string) {
		if v != "" {
			body[k] = v
		}
	}
	if campCreateSubUsecases != "" {
		body["sub_usecases"] = strings.Split(campCreateSubUsecases, ",")
	}
	addIfSet("help_keywords", campCreateHelpKeywords)
	addIfSet("optin_keywords", campCreateOptInKW)
	addIfSet("optin_message", campCreateOptInMsg)
	addIfSet("optout_keywords", campCreateOptOutKW)

	proceed, dryRun, gerr := guardSpend("register 10DLC campaign " + campCreateAlias)
	if !proceed {
		return gerr
	}
	applyDryRun(client, dryRun)

	var resp struct {
		api.RawBody
		APIID      string `json:"api_id"`
		CampaignID string `json:"campaign_id"`
		Message    string `json:"message"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("10dlc", "Campaign"), body, nil, &resp)
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
		return output.JSONRaw(os.Stdout, resp.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"campaign_id", resp.CampaignID},
		{"message", resp.Message},
	})
}

func runCampaignUpdate(cmd *cobra.Command, args []string) error {
	id := args[0]
	body := map[string]any{}
	if campUpdateDescription != "" {
		body["description"] = campUpdateDescription
	}
	if campUpdateMessageFlow != "" {
		body["message_flow"] = campUpdateMessageFlow
	}
	if campUpdateSample1 != "" {
		body["sample1"] = campUpdateSample1
	}
	if campUpdateSample2 != "" {
		body["sample2"] = campUpdateSample2
	}
	if len(body) == 0 {
		return clierr.BadInput("at least one of --description, --message-flow, --sample-message-1, --sample-message-2 required")
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	proceed, dryRun, gerr := guardSpend("update 10DLC campaign " + id)
	if !proceed {
		return gerr
	}
	applyDryRun(client, dryRun)
	var resp api.Campaign10DLCResponse
	apiErr, err := client.Do("POST", client.AccountURL("10dlc", "Campaign", id), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRun {
		return nil
	}
	human := "Updated campaign " + id
	if status := resp.Campaign.RegistrationStatus; status != "" {
		human += ", registration status " + status
	}
	return emitResponse(human, resp.Raw())
}
