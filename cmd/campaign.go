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

var campaignCmd = &cobra.Command{
	Use:     "campaigns",
	Aliases: []string{"camp", "campaign"},
	Short:   "10DLC campaign registration (use cases for a brand)",
}

var (
	campListLimit  int
	campListOffset int
	campListBrand  string
)

var campListCmd = &cobra.Command{
	Use:   "list",
	Short: "List campaigns",
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
	campCreateNumberPool   bool
)

var campCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Register a new campaign (spends money — TCR fee, requires --yes)",
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
	Short: "Update mutable campaign fields",
	Args:  cobra.ExactArgs(1),
	RunE:  runCampaignUpdate,
}

func init() {
	campListCmd.Flags().IntVar(&campListLimit, "limit", 20, "results per page")
	campListCmd.Flags().IntVar(&campListOffset, "offset", 0, "pagination offset")
	campListCmd.Flags().StringVar(&campListBrand, "brand-id", "", "filter by brand_id")

	campCreateCmd.Flags().StringVar(&campCreateAlias, "alias", "", "human-friendly alias (required)")
	_ = campCreateCmd.MarkFlagRequired("alias")
	campCreateCmd.Flags().StringVar(&campCreateBrand, "brand-id", "", "brand to register under (required)")
	_ = campCreateCmd.MarkFlagRequired("brand-id")
	campCreateCmd.Flags().StringVar(&campCreateUsecase, "usecase", "", "primary use case, e.g. MARKETING, MIXED (required)")
	_ = campCreateCmd.MarkFlagRequired("usecase")
	campCreateCmd.Flags().StringVar(&campCreateSubUsecases, "sub-usecases", "", "comma-separated sub use cases")
	campCreateCmd.Flags().StringVar(&campCreateDescription, "description", "", "what this campaign does (required)")
	_ = campCreateCmd.MarkFlagRequired("description")
	campCreateCmd.Flags().StringVar(&campCreateMessageFlow, "message-flow", "", "describe how recipients opt in (required)")
	_ = campCreateCmd.MarkFlagRequired("message-flow")
	campCreateCmd.Flags().StringVar(&campCreateSample1, "sample-message-1", "", "sample message (required)")
	_ = campCreateCmd.MarkFlagRequired("sample-message-1")
	campCreateCmd.Flags().StringVar(&campCreateSample2, "sample-message-2", "", "second sample message")
	campCreateCmd.Flags().StringVar(&campCreateHelpKeywords, "help-keywords", "HELP", "comma-separated help keywords")
	campCreateCmd.Flags().StringVar(&campCreateHelpMessage, "help-message", "", "auto-reply to HELP")
	campCreateCmd.Flags().StringVar(&campCreateOptInKW, "opt-in-keywords", "START", "opt-in keywords")
	campCreateCmd.Flags().StringVar(&campCreateOptInMsg, "opt-in-message", "", "opt-in confirmation message")
	campCreateCmd.Flags().StringVar(&campCreateOptOutKW, "opt-out-keywords", "STOP", "opt-out keywords")
	campCreateCmd.Flags().StringVar(&campCreateOptOutMsg, "opt-out-message", "", "opt-out confirmation message")
	campCreateCmd.Flags().BoolVar(&campCreateEmbedLink, "embedded-link", false, "messages contain links")
	campCreateCmd.Flags().BoolVar(&campCreateEmbedPhone, "embedded-phone", false, "messages contain phone numbers")
	campCreateCmd.Flags().BoolVar(&campCreateAgeGated, "age-gated", false, "campaign targets adult content")
	campCreateCmd.Flags().BoolVar(&campCreateDirectLend, "direct-lending", false, "direct-lending arrangements")
	campCreateCmd.Flags().BoolVar(&campCreateAffiliate, "affiliate-marketing", false, "affiliate marketing campaign")
	campCreateCmd.Flags().BoolVar(&campCreateNumberPool, "number-pool", false, "campaign uses a number pool")

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
	apiErr, err := client.Do("GET", client.AccountURL("10dlc", "Campaign"), nil, q, &resp)
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
	rows := [][]string{{"CAMPAIGN_ID", "ALIAS", "BRAND_ID", "USECASE", "STATUS"}}
	for _, c := range resp.Campaigns {
		rows = append(rows, []string{c.CampaignID, c.CampaignAlias, c.BrandID, c.Usecase, c.CampaignStatus})
	}
	return output.Table(os.Stdout, rows)
}

func runCampaignGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var c api.Campaign10DLC
	apiErr, err := client.Do("GET", client.AccountURL("10dlc", "Campaign", id), nil, nil, &c)
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
		return output.JSONRaw(os.Stdout, c.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"campaign_id", c.CampaignID},
		{"alias", c.CampaignAlias},
		{"brand_id", c.BrandID},
		{"usecase", c.Usecase},
		{"sub_usecases", strings.Join(c.SubUsecases, ", ")},
		{"status", c.CampaignStatus},
		{"description", c.Description},
		{"message_flow", c.MessageFlow},
		{"help_keywords", c.HelpKeywords},
		{"opt_in_keywords", c.OptInKeywords},
		{"opt_out_keywords", c.OptOutKeywords},
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
		"usecase":             campCreateUsecase,
		"description":         campCreateDescription,
		"message_flow":        campCreateMessageFlow,
		"sample_message_1":    campCreateSample1,
		"embedded_link":       campCreateEmbedLink,
		"embedded_phone":      campCreateEmbedPhone,
		"age_gated":           campCreateAgeGated,
		"direct_lending":      campCreateDirectLend,
		"affiliate_marketing": campCreateAffiliate,
		"number_pool":         campCreateNumberPool,
	}
	addIfSet := func(k, v string) {
		if v != "" {
			body[k] = v
		}
	}
	if campCreateSubUsecases != "" {
		body["sub_usecases"] = strings.Split(campCreateSubUsecases, ",")
	}
	addIfSet("sample_message_2", campCreateSample2)
	addIfSet("help_keywords", campCreateHelpKeywords)
	addIfSet("help_message", campCreateHelpMessage)
	addIfSet("opt_in_keywords", campCreateOptInKW)
	addIfSet("opt_in_message", campCreateOptInMsg)
	addIfSet("opt_out_keywords", campCreateOptOutKW)
	addIfSet("opt_out_message", campCreateOptOutMsg)

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
		body["sample_message_1"] = campUpdateSample1
	}
	if campUpdateSample2 != "" {
		body["sample_message_2"] = campUpdateSample2
	}
	if len(body) == 0 {
		return clierr.BadInput("at least one of --description, --message-flow, --sample-message-1, --sample-message-2 required")
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("10dlc", "Campaign", id), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Updated campaign %s: %s\n", id, resp.Message)
	return nil
}
