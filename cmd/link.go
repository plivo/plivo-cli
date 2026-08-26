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

var linkCmd = &cobra.Command{
	Use:     "links",
	Aliases: []string{"link"},
	Short:   "10DLC number-to-campaign linking",
}

var (
	linkListLimit    int
	linkListOffset   int
	linkListCampaign string
	linkListNumber   string
)

var linkListCmd = &cobra.Command{
	Use:   "list",
	Short: "List number→campaign links",
	RunE:  runLinkList,
}

var (
	linkCreateNumber   string
	linkCreateCampaign string
)

var linkCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Link a number to a campaign",
	RunE:  runLinkCreate,
}

var linkDeleteCmd = &cobra.Command{
	Use:   "delete <link_id>",
	Short: "Unlink a number (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runLinkDelete,
}

func init() {
	linkListCmd.Flags().IntVar(&linkListLimit, "limit", 20, "results per page")
	linkListCmd.Flags().IntVar(&linkListOffset, "offset", 0, "pagination offset")
	linkListCmd.Flags().StringVar(&linkListCampaign, "campaign-id", "", "filter by campaign_id")
	linkListCmd.Flags().StringVar(&linkListNumber, "number", "", "filter by number")

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
	if linkListCampaign != "" {
		q.Set("campaign_id", linkListCampaign)
	}
	if linkListNumber != "" {
		q.Set("number", linkListNumber)
	}
	var resp api.NumberLink10DLCList
	apiErr, err := client.Do("GET", client.AccountURL("10dlc", "NumberLinking"), nil, q, &resp)
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
	rows := [][]string{{"LINK_ID", "NUMBER", "CAMPAIGN_ID", "STATUS", "CREATED"}}
	for _, l := range resp.Objects {
		rows = append(rows, []string{l.LinkID, l.Number, l.CampaignID, l.Status, l.CreatedAt})
	}
	return output.Table(os.Stdout, rows)
}

func runLinkCreate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"number":      linkCreateNumber,
		"campaign_id": linkCreateCampaign,
	}
	var resp struct {
		api.RawBody
		APIID   string `json:"api_id"`
		LinkID  string `json:"link_id"`
		Message string `json:"message"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("10dlc", "NumberLinking"), body, nil, &resp)
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
		{"link_id", resp.LinkID},
		{"message", resp.Message},
	})
}

func runLinkDelete(cmd *cobra.Command, args []string) error {
	id := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("unlink " + id)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("10dlc", "NumberLinking", id), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Unlinked %s\n", id)
	return nil
}
