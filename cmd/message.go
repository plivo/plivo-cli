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

// messageCmd is the top-level messaging group. After the channel-split
// refactor (see cmd/messaging_split_design.md), there is no universal
// `messaging send` or `messaging list` — users pick a channel:
//
//	plivo messaging sms send …
//	plivo messaging whatsapp send …
//	plivo messaging mms send …
//	plivo messaging sms list
//	plivo messaging whatsapp list
//	…
//
// The universal `messaging get <uuid>` survives because the UUID is
// channel-agnostic (the response carries the channel; no need to know
// it up front to look up a record).
var messageCmd = &cobra.Command{
	Use:     "messaging",
	Aliases: []string{"message", "msg", "sms"},
	Short:   "Send messages (SMS / MMS / WhatsApp) — channel-split surface",
}

// ─── Channel subgroups ──────────────────────────────────────────────────────

var messagingSmsCmd = &cobra.Command{
	Use:   "sms",
	Short: "SMS — A2P / P2P short-message-service (incl. 10DLC, powerpacks, toll-free)",
}

var messagingWhatsappCmd = &cobra.Command{
	Use:     "whatsapp",
	Aliases: []string{"wa"},
	Short:   "WhatsApp — Plivo's WhatsApp Business API surface",
}

var messagingMmsCmd = &cobra.Command{
	Use:   "mms",
	Short: "MMS — multimedia messages (US/Canada)",
}

// ─── Per-channel send + list flags (one set per channel; tiny duplication
//     but keeps each command's --help self-contained) ──────────────────────

var (
	smsSendSrc, smsSendDst, smsSendText, smsSendURL, smsSendMethod string

	whatsappSendSrc, whatsappSendDst, whatsappSendText, whatsappSendURL, whatsappSendMethod string

	mmsSendSrc, mmsSendDst, mmsSendText, mmsSendURL, mmsSendMethod string
)

var (
	smsListLimit, smsListOffset                                                int
	smsListState, smsListDirection, smsListFrom, smsListTo                     string
	whatsappListLimit, whatsappListOffset                                      int
	whatsappListState, whatsappListDirection, whatsappListFrom, whatsappListTo string
	mmsListLimit, mmsListOffset                                                int
	mmsListState, mmsListDirection, mmsListFrom, mmsListTo                     string
)

// ─── Send commands (thin wrappers over runMessageSendForChannel) ────────────

var messagingSmsSendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send an SMS (requires --yes; spends money — use --dry-run to preview)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMessageSendForChannel(cmd, "sms", smsSendSrc, smsSendDst, smsSendText, smsSendURL, smsSendMethod)
	},
}

var messagingWhatsappSendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send a WhatsApp message (requires --yes; spends money — use --dry-run to preview)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMessageSendForChannel(cmd, "whatsapp", whatsappSendSrc, whatsappSendDst, whatsappSendText, whatsappSendURL, whatsappSendMethod)
	},
}

var messagingMmsSendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send an MMS (requires --yes; spends money — use --dry-run to preview)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMessageSendForChannel(cmd, "mms", mmsSendSrc, mmsSendDst, mmsSendText, mmsSendURL, mmsSendMethod)
	},
}

// ─── List commands (each filters server-side via message_type) ──────────────

var messagingSmsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List SMS messages",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMessageListForChannel(cmd, "sms",
			smsListLimit, smsListOffset, smsListState, smsListDirection, smsListFrom, smsListTo)
	},
}

var messagingWhatsappListCmd = &cobra.Command{
	Use:   "list",
	Short: "List WhatsApp messages",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMessageListForChannel(cmd, "whatsapp",
			whatsappListLimit, whatsappListOffset, whatsappListState, whatsappListDirection, whatsappListFrom, whatsappListTo)
	},
}

var messagingMmsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List MMS messages",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMessageListForChannel(cmd, "mms",
			mmsListLimit, mmsListOffset, mmsListState, mmsListDirection, mmsListFrom, mmsListTo)
	},
}

// ─── Universal get (UUID is channel-agnostic) ───────────────────────────────

var messageGetCmd = &cobra.Command{
	Use:   "get <message_uuid>",
	Short: "Get a message by UUID (works for SMS, MMS, and WhatsApp)",
	Args:  cobra.ExactArgs(1),
	RunE:  runMessageGet,
}

func init() {
	// SMS send flags
	registerSendFlags(messagingSmsSendCmd, &smsSendSrc, &smsSendDst, &smsSendText, &smsSendURL, &smsSendMethod)
	registerSendFlags(messagingWhatsappSendCmd, &whatsappSendSrc, &whatsappSendDst, &whatsappSendText, &whatsappSendURL, &whatsappSendMethod)
	registerSendFlags(messagingMmsSendCmd, &mmsSendSrc, &mmsSendDst, &mmsSendText, &mmsSendURL, &mmsSendMethod)

	// SMS list flags
	registerListFlags(messagingSmsListCmd, &smsListLimit, &smsListOffset, &smsListState, &smsListDirection, &smsListFrom, &smsListTo)
	registerListFlags(messagingWhatsappListCmd, &whatsappListLimit, &whatsappListOffset, &whatsappListState, &whatsappListDirection, &whatsappListFrom, &whatsappListTo)
	registerListFlags(messagingMmsListCmd, &mmsListLimit, &mmsListOffset, &mmsListState, &mmsListDirection, &mmsListFrom, &mmsListTo)

	// Wire per-channel verbs onto each subgroup
	messagingSmsCmd.AddCommand(messagingSmsSendCmd, messagingSmsListCmd, sms10dlcCmd)
	messagingWhatsappCmd.AddCommand(messagingWhatsappSendCmd, messagingWhatsappListCmd)
	messagingMmsCmd.AddCommand(messagingMmsSendCmd, messagingMmsListCmd)

	// Wire subgroups + the universal get onto the parent
	messageCmd.AddCommand(messagingSmsCmd, messagingWhatsappCmd, messagingMmsCmd, messageGetCmd)
	rootCmd.AddCommand(messageCmd)
}

// registerSendFlags adds the shared send-flag set to a channel's send
// command. Keeps the per-channel commands' --help output self-contained
// without duplicating 6 lines of MarkFlagRequired bookkeeping inline.
func registerSendFlags(cmd *cobra.Command, src, dst, text, urlFlag, method *string) {
	cmd.Flags().StringVar(src, "src", "", "sender (E.164, shortcode, or sender ID) (required)")
	_ = cmd.MarkFlagRequired("src")
	cmd.Flags().StringVar(dst, "dst", "", "destination, separate multiple with `<` (required)")
	_ = cmd.MarkFlagRequired("dst")
	cmd.Flags().StringVar(text, "text", "", "message body (required)")
	_ = cmd.MarkFlagRequired("text")
	cmd.Flags().StringVar(urlFlag, "url", "", "callback URL for delivery status")
	cmd.Flags().StringVar(method, "method", "POST", "callback method GET|POST")
}

// registerListFlags adds the shared list-flag set (channel filter is set
// internally per command, not exposed as a flag).
func registerListFlags(cmd *cobra.Command, limit, offset *int, state, direction, fromN, toN *string) {
	cmd.Flags().IntVar(limit, "limit", 20, "results per page")
	cmd.Flags().IntVar(offset, "offset", 0, "pagination offset")
	cmd.Flags().StringVar(state, "state", "", "queued|sent|delivered|undelivered|failed|received")
	cmd.Flags().StringVar(direction, "direction", "", "inbound|outbound")
	cmd.Flags().StringVar(fromN, "from", "", "filter by from_number")
	cmd.Flags().StringVar(toN, "to", "", "filter by to_number")
}

// runMessageSendForChannel is the shared body of all three per-channel
// send commands. Hardcodes the message_type per channel; everything else
// flows through the same body shape the Plivo Message API expects.
func runMessageSendForChannel(cmd *cobra.Command, channel, src, dst, text, urlFlag, method string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"src":  src,
		"dst":  dst,
		"text": text,
		"type": channel,
	}
	if urlFlag != "" {
		body["url"] = urlFlag
		body["method"] = method
	}

	// Spend-verb gate: refuse without --yes (DESTRUCTIVE_REFUSED, exit 5);
	// --dry-run alone is still allowed and prints the would-be request.
	proceed, dryRun, gerr := guardSpend(fmt.Sprintf("send %s message from %s to %s", channel, src, dst))
	if !proceed {
		return gerr
	}
	applyDryRun(client, dryRun)

	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will POST %s (type=%s src=%s dst=%s)\n", client.AccountURL("Message"), channel, src, dst)
	}

	var resp api.MessageSendResponse
	apiErr, err := client.Do("POST", client.AccountURL("Message"), body, nil, &resp)
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
		{"message", resp.Message},
		{"uuids", strings.Join(resp.MessageUUID, ", ")},
	})
}

// runMessageListForChannel is the shared body of all three per-channel
// list commands. Always sets message_type=<channel> so the backend
// filters server-side.
func runMessageListForChannel(cmd *cobra.Command, channel string,
	limit, offset int, state, direction, fromN, toN string,
) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("offset", strconv.Itoa(offset))
	q.Set("message_type", channel) // always filtered to this channel
	if state != "" {
		q.Set("message_state", state)
	}
	if direction != "" {
		q.Set("message_direction", direction)
	}
	if fromN != "" {
		q.Set("from", fromN)
	}
	if toN != "" {
		q.Set("to", toN)
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
		return output.JSONRaw(os.Stdout, resp.Raw())
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
		return output.JSONRaw(os.Stdout, m.Raw())
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
