package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var messageCmd = &cobra.Command{
	Use:     "message",
	Aliases: []string{"msg"},
	Short:   "Send and inspect SMS/MMS messages",
}

var (
	msgSendSrc    string
	msgSendDst    string
	msgSendText   string
	msgSendType   string
	msgSendURL    string
	msgSendMethod string
)

var messageSendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send an SMS/MMS (defaults to dry-run; pass --yes to actually send)",
	RunE:  runMessageSend,
}

var (
	msgListLimit     int
	msgListOffset    int
	msgListState     string
	msgListDirection string
	msgListType      string
	msgListFrom      string
	msgListTo        string
)

var messageListCmd = &cobra.Command{
	Use:   "list",
	Short: "List messages",
	RunE:  runMessageList,
}

var messageGetCmd = &cobra.Command{
	Use:   "get <message_uuid>",
	Short: "Get a message by UUID",
	Args:  cobra.ExactArgs(1),
	RunE:  runMessageGet,
}

func init() {
	messageSendCmd.Flags().StringVar(&msgSendSrc, "src", "", "sender (E.164, shortcode, or sender ID) (required)")
	_ = messageSendCmd.MarkFlagRequired("src")
	messageSendCmd.Flags().StringVar(&msgSendDst, "dst", "", "destination, separate multiple with `<` (required)")
	_ = messageSendCmd.MarkFlagRequired("dst")
	messageSendCmd.Flags().StringVar(&msgSendText, "text", "", "message body (required)")
	_ = messageSendCmd.MarkFlagRequired("text")
	messageSendCmd.Flags().StringVar(&msgSendType, "type", "sms", "sms|mms|whatsapp")
	messageSendCmd.Flags().StringVar(&msgSendURL, "url", "", "callback URL for delivery status")
	messageSendCmd.Flags().StringVar(&msgSendMethod, "method", "POST", "callback method GET|POST")

	messageListCmd.Flags().IntVar(&msgListLimit, "limit", 20, "results per page")
	messageListCmd.Flags().IntVar(&msgListOffset, "offset", 0, "pagination offset")
	messageListCmd.Flags().StringVar(&msgListState, "state", "", "queued|sent|delivered|undelivered|failed|received")
	messageListCmd.Flags().StringVar(&msgListDirection, "direction", "", "inbound|outbound")
	messageListCmd.Flags().StringVar(&msgListType, "type", "", "sms|mms|whatsapp")
	messageListCmd.Flags().StringVar(&msgListFrom, "from", "", "filter by from_number")
	messageListCmd.Flags().StringVar(&msgListTo, "to", "", "filter by to_number")

	messageCmd.AddCommand(messageSendCmd, messageListCmd, messageGetCmd)
	rootCmd.AddCommand(messageCmd)
}

func runMessageSend(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"src":  msgSendSrc,
		"dst":  msgSendDst,
		"text": msgSendText,
		"type": msgSendType,
	}
	if msgSendURL != "" {
		body["url"] = msgSendURL
		body["method"] = msgSendMethod
	}

	// message send costs money — default to dry-run unless --yes
	effectiveDryRun := dryRunFlag || !yesFlag
	if effectiveDryRun {
		client.DryRun = true
		if !dryRunFlag {
			fmt.Fprintln(os.Stderr, "[dry-run] message send defaults to dry-run; pass --yes to actually send")
		}
	}

	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will POST %s (type=%s src=%s dst=%s)\n", client.AccountURL("Message"), msgSendType, msgSendSrc, msgSendDst)
	}

	var resp api.MessageSendResponse
	apiErr, err := client.Do("POST", client.AccountURL("Message"), body, nil, &resp)
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
		{"message", resp.Message},
		{"uuids", strings.Join(resp.MessageUUID, ", ")},
	})
}

func runMessageList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(msgListLimit))
	q.Set("offset", strconv.Itoa(msgListOffset))
	if msgListState != "" {
		q.Set("message_state", msgListState)
	}
	if msgListDirection != "" {
		q.Set("message_direction", msgListDirection)
	}
	if msgListType != "" {
		q.Set("message_type", msgListType)
	}
	if msgListFrom != "" {
		q.Set("from", msgListFrom)
	}
	if msgListTo != "" {
		q.Set("to", msgListTo)
	}

	var resp api.MessageList
	apiErr, err := client.Do("GET", client.AccountURL("Message"), nil, q, &resp)
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
	rows := [][]string{{"UUID", "FROM", "TO", "STATE", "TYPE", "TIME"}}
	for _, m := range resp.Objects {
		rows = append(rows, []string{m.MessageUUID, m.From, m.To, m.State, m.Type, m.MessageTime})
	}
	return output.Table(os.Stdout, rows)
}

func runMessageGet(cmd *cobra.Command, args []string) error {
	uuid := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var m api.Message
	apiErr, err := client.Do("GET", client.AccountURL("Message", uuid), nil, nil, &m)
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
		{"uuid", m.MessageUUID},
		{"from", m.From},
		{"to", m.To},
		{"text", m.Text},
		{"type", m.Type},
		{"direction", m.Direction},
		{"state", m.State},
		{"time", m.MessageTime},
		{"total_amount", m.TotalAmount},
		{"units", strconv.Itoa(m.Units)},
		{"error_code", m.ErrorCode},
	})
}
