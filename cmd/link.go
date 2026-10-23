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

var linkCmd = &cobra.Command{
	Use:     "links",
	Aliases: []string{"link"},
	Short:   "10DLC number-to-campaign linking",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var (
	linkListLimit    int
	linkListOffset   int
	linkListCampaign string
	linkListNumber   string
)

var linkListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the numbers linked to a campaign, with each link's status",
	Args:  cobra.NoArgs,
	RunE:  runLinkList,
}

var (
	linkCreateNumber   string
	linkCreateCampaign string
)

var linkCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Link a number to a campaign",
	Args:  cobra.NoArgs,
	RunE:  runLinkCreate,
}

var linkDeleteCmd = &cobra.Command{
	Use:   "delete <campaign_id> <number>",
	Short: "Unlink a number from a campaign (requires --yes)",
	Args:  cobra.ExactArgs(2),
	RunE:  runLinkDelete,
}

func init() {
	registerListFlags(linkListCmd, &linkListLimit, &linkListOffset)
	linkListCmd.Flags().StringVar(&linkListCampaign, "campaign-id", "", "the campaign whose numbers to list (required)")
	_ = linkListCmd.MarkFlagRequired("campaign-id")
	linkListCmd.Flags().StringVar(&linkListNumber, "number", "", "only this number")

	linkCreateCmd.Flags().StringVar(&linkCreateNumber, "number", "", "E.164 number (required)")
	_ = linkCreateCmd.MarkFlagRequired("number")
	linkCreateCmd.Flags().StringVar(&linkCreateCampaign, "campaign-id", "", "10DLC campaign id (required)")
	_ = linkCreateCmd.MarkFlagRequired("campaign-id")

	linkCmd.AddCommand(linkListCmd, linkCreateCmd, linkDeleteCmd)
	sms10dlcCmd.AddCommand(linkCmd)
}

func runLinkList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(linkListLimit))
	q.Set("offset", strconv.Itoa(linkListOffset))
	parts := []string{"10dlc", "Campaign", linkListCampaign, "Number"}
	if linkListNumber != "" { // one number's link: no paging
		parts, q = append(parts, linkListNumber), nil
	}
	var resp api.CampaignNumbers10DLC
	if err := fetchList(client, client.AccountURL(parts...), q, "phone_numbers", &resp); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return listJSON(os.Stdout, resp.Raw(), "phone_numbers")
	}
	rows := [][]string{{"NUMBER", "CAMPAIGN_ID", "STATUS"}}
	for _, n := range resp.PhoneNumbers {
		rows = append(rows, []string{n.Number, resp.CampaignID, n.Status})
	}
	return output.Table(os.Stdout, rows)
}

func runLinkCreate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{"numbers": []string{linkCreateNumber}}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("10dlc", "Campaign", linkCreateCampaign, "Number"), body, nil, &resp)
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
		{"campaign_id", linkCreateCampaign},
		{"number", linkCreateNumber},
		{"message", resp.Message},
	})
}

func runLinkDelete(cmd *cobra.Command, args []string) error {
	campaign, number := args[0], args[1]
	if err := confirmDestructive(fmt.Sprintf("unlink %s from campaign %s", number, campaign)); err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("10dlc", "Campaign", campaign, "Number", number), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	return emitWrite(fmt.Sprintf("Unlinked %s from campaign %s", number, campaign),
		map[string]any{"campaign_id": campaign, "number": number, "unlinked": true})
}
