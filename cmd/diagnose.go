package cmd

import (
	"fmt"
	"net/http"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/spf13/cobra"
)

// Diagnose subcommands are thin sugar over `plivo ask` — they build a
// structured prompt that asks the AI assistant to debug a specific
// call_uuid or message_uuid, then delegate to runAsk. The assistant
// picks the right debug path from the prompt; the value of this surface
// is discoverability + a stable, copy-pasteable command shape that
// scripts can call.
//
// Wire-side this means zero backend work — we hit /v1/aiassist/buddy-ext/chat
// like `ask` does, and reuse the same SSE renderer.
//
// All three messaging-channel diagnose commands share the same backend
// (the assistant auto-detects channel from the UUID); the per-channel
// surface is for symmetry + discoverability under the channel-split tree.

var voiceCallsDiagnoseCmd = &cobra.Command{
	Use:     "diagnose <call_uuid>",
	Aliases: []string{"diag"},
	Short:   "Diagnose what happened on a call (AI-powered, hits the voice debugger)",
	Long: `Diagnose a call by UUID. Asks Plivo's AI assistant to pull the SIP / media
trace and explain what happened — answer time, drop cause, codec
negotiation issues, anything anomalous.

Equivalent to:
  plivo ask --call-uuid <call_uuid> "Help me debug this call: <call_uuid>"

…just shorter. Streams the answer as it comes; expect 30–120s for the
full investigation since the debugger does live log lookups.`,
	Example: `  plivo voice calls diagnose 01fe1ff8-fd57-4901-a150-d55b8dfd669b
  plivo voice call diagnose 01fe1ff8-fd57-4901-a150-d55b8dfd669b   # alias
  plivo voice calls diagnose <uuid> -o json                        # JSONL`,
	Args: cobra.ExactArgs(1),
	RunE: runDiagnoseVoiceCall,
}

var messagingSmsDiagnoseCmd = &cobra.Command{
	Use:     "diagnose <message_uuid>",
	Aliases: []string{"diag"},
	Short:   "Diagnose what happened with an SMS (AI-powered)",
	Long: `Diagnose an SMS by UUID. Asks Plivo's AI assistant to look up the
message detail record + carrier delivery report and explain the
outcome — delivered? Filtered by carrier? Stuck in queue? Rejected for
a bad sender?

Equivalent to:
  plivo ask "Help me debug this message: <message_uuid>"`,
	Example: `  plivo messaging sms diagnose 788444ec-5bc1-4de0-aafc-a0a06e0b0089
  plivo messaging sms diagnose <uuid> -o json   # JSONL`,
	Args: cobra.ExactArgs(1),
	RunE: runDiagnoseMessaging("SMS"),
}

var messagingWhatsappDiagnoseCmd = &cobra.Command{
	Use:     "diagnose <message_uuid>",
	Aliases: []string{"diag"},
	Short:   "Diagnose what happened with a WhatsApp message (AI-powered)",
	Long: `Diagnose a WhatsApp message by UUID. Asks Plivo's AI assistant to
look up the delivery / read receipt and explain the outcome.

Equivalent to:
  plivo ask "Help me debug this WhatsApp message: <message_uuid>"`,
	Example: `  plivo messaging whatsapp diagnose 788444ec-…
  plivo messaging wa diagnose 788444ec-…           # 'wa' alias for whatsapp`,
	Args: cobra.ExactArgs(1),
	RunE: runDiagnoseMessaging("WhatsApp"),
}

var messagingMmsDiagnoseCmd = &cobra.Command{
	Use:     "diagnose <message_uuid>",
	Aliases: []string{"diag"},
	Short:   "Diagnose what happened with an MMS (AI-powered)",
	Long: `Diagnose an MMS by UUID. Asks Plivo's AI assistant to look up the
message detail record + carrier delivery report and explain the
outcome.

Equivalent to:
  plivo ask "Help me debug this MMS: <message_uuid>"`,
	Example: `  plivo messaging mms diagnose 788444ec-…`,
	Args:    cobra.ExactArgs(1),
	RunE:    runDiagnoseMessaging("MMS"),
}

func init() {
	callCmd.AddCommand(voiceCallsDiagnoseCmd)
	messagingSmsCmd.AddCommand(messagingSmsDiagnoseCmd)
	messagingWhatsappCmd.AddCommand(messagingWhatsappDiagnoseCmd)
	messagingMmsCmd.AddCommand(messagingMmsDiagnoseCmd)
}

// runDiagnoseVoiceCall builds the prompt + delegates to runAsk. Sets
// askCallUUID so userContext.callUUID lands in the BuddyChatRequest
// (the assistant uses that for escalation idempotency + sometimes as a
// disambiguator when the message text is short).
func runDiagnoseVoiceCall(cmd *cobra.Command, args []string) error {
	callUUID := args[0]
	if err := requireResourceExists(cmd, "Call", callUUID, "call"); err != nil {
		return err
	}
	askCallUUID = callUUID // runAsk auto-appends "(call_uuid: X)" + populates userContext
	prompt := "Help me debug this call. What happened, and was there anything unusual?"
	return runAsk(cmd, []string{prompt})
}

// requireResourceExists confirms the uuid is on this account before handing the
// turn to the assistant. Without it, a typo'd id reached the assistant, which
// cannot tell "does not exist" from "lookup failed" and so escalates — turning
// every mistyped id into a support ticket.
//
// The REST lookup is authoritative where the assistant is not: it is scoped to
// the caller's account and 404s only when the record genuinely is not there.
// A non-404 failure is deliberately NOT fatal — losing diagnose because a
// pre-flight read hiccuped would be worse than the ticket it prevents.
func requireResourceExists(cmd *cobra.Command, segment, uuid, label string) error {
	if dryRunFlag {
		return nil // dry-run sends nothing, so there is nothing to pre-check
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var probe api.GenericResponse
	apiErr, err := client.Do("GET", client.AccountURL(segment, uuid), nil, nil, &probe)
	if err != nil {
		return nil // transport trouble: fall through rather than block the diagnose
	}
	if apiErr != nil && apiErr.StatusCode == http.StatusNotFound {
		return &clierr.Error{
			Code:       clierr.CodeResourceNotFound,
			Message:    fmt.Sprintf("%s %s not found on this account", label, uuid),
			Hint:       fmt.Sprintf("Check the %s id. `plivo %s` lists recent ones.", label, listHintFor(segment)),
			StatusCode: http.StatusNotFound,
		}
	}
	return nil
}

// listHintFor names the command that lists the resource, for the not-found hint.
func listHintFor(segment string) string {
	if segment == "Message" {
		return "messaging sms list"
	}
	return "voice calls list"
}

// runDiagnoseMessaging returns a cobra RunE that builds a channel-tagged
// message-debug prompt. All three channel subgroups call this with their
// human-friendly channel label — the backend doesn't care (it auto-
// detects from the UUID) but a labelled prompt produces a slightly
// crisper LLM response.
func runDiagnoseMessaging(channelLabel string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		messageUUID := args[0]
		if err := requireResourceExists(cmd, "Message", messageUUID, "message"); err != nil {
			return err
		}
		prompt := fmt.Sprintf("Help me debug this %s message: %s. Why did it fail / what's the status?", channelLabel, messageUUID)
		return runAsk(cmd, []string{prompt})
	}
}
