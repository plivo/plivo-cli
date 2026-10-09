package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
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
full investigation since the debugger does live log lookups.` + diagnoseOutputHelp,
	Example: `  plivo voice calls diagnose 01fe1ff8-fd57-4901-a150-d55b8dfd669b
  plivo voice call diagnose 01fe1ff8-fd57-4901-a150-d55b8dfd669b   # alias
  plivo voice calls diagnose <uuid> -o json                        # one JSON result
  plivo voice calls diagnose <uuid> -o jsonl                       # the raw event stream`,
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
  plivo ask "Help me debug this message: <message_uuid>"` + diagnoseOutputHelp,
	Example: `  plivo messaging sms diagnose 788444ec-5bc1-4de0-aafc-a0a06e0b0089
  plivo messaging sms diagnose <uuid> -o json    # one JSON result
  plivo messaging sms diagnose <uuid> -o jsonl   # the raw event stream`,
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
  plivo ask "Help me debug this WhatsApp message: <message_uuid>"` + diagnoseOutputHelp,
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
  plivo ask "Help me debug this MMS: <message_uuid>"` + diagnoseOutputHelp,
	Example: `  plivo messaging mms diagnose 788444ec-…`,
	Args:    cobra.ExactArgs(1),
	RunE:    runDiagnoseMessaging("MMS"),
}

// diagnoseOutputHelp closes every diagnose command's help: the -o json result
// and the exit code of a failed analysis.
const diagnoseOutputHelp = `

With -o json, prints one JSON result when the analysis ends: call_uuid
(message_uuid for a message), what_happened, likely_cause, timeline[]
({at, event, source}), next_steps[], hangup_cause_code and hangup_source (from
the call record; null for a message), confidence (high, medium or low) and
answer (the prose). -o yaml, -o csv and --query work on that result; -o jsonl
streams the assistant's raw events instead. An analysis that fails, escalates
or stops early exits 3.`

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
	record, err := requireResourceExists(cmd, "Call", callUUID, "call")
	if err != nil {
		return err
	}
	askCallUUID = callUUID // runAsk auto-appends "(call_uuid: X)" + populates userContext
	prompt := "Help me debug this call. What happened, and was there anything unusual?" +
		diagnoseClientConstraints
	return runDiagnose(cmd, prompt, diagnoseTarget{label: "call", uuid: callUUID, getCmd: "voice calls get", record: record})
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
//
// It returns the record, which -o json reads the hangup facts from; nil when
// the read failed.
func requireResourceExists(cmd *cobra.Command, segment, uuid, label string) (json.RawMessage, error) {
	client, _, err := getClient()
	if err != nil {
		return nil, err
	}
	record, status := readRecord(client, segment, uuid)
	if status == http.StatusNotFound {
		// A trunk call lives in a different store and a different debugger reads
		// it, so name the right command instead of reporting a bare not-found.
		if segment == "Call" && resourceExists(client, "Zentrunk", "Call", uuid) {
			return nil, &clierr.Error{
				Code:       clierr.CodeBadInput,
				Message:    fmt.Sprintf("%s is a SIP Trunking call, not a Voice call", uuid),
				Hint:       fmt.Sprintf("Run `plivo sip calls diagnose %s`.", uuid),
				StatusCode: http.StatusNotFound,
			}
		}
		return nil, &clierr.Error{
			Code:       clierr.CodeResourceNotFound,
			Message:    fmt.Sprintf("%s %s not found on this account", label, uuid),
			Hint:       fmt.Sprintf("Check the %s id. `plivo %s` lists recent ones.", label, listHintFor(segment)),
			StatusCode: http.StatusNotFound,
		}
	}
	return record, nil
}

// readRecord GETs a record and keeps its body, with the HTTP status (0 when
// the request itself failed). It reads under --dry-run too: a GET is not a
// write, and the preview must not show a turn the real run would refuse.
func readRecord(client *api.Client, parts ...string) (json.RawMessage, int) {
	var record json.RawMessage
	var apiErr *api.APIError
	if err := readThrough(client, func() error {
		var e error
		apiErr, e = client.Do("GET", client.AccountURL(parts...), nil, nil, &record)
		return e
	}); err != nil {
		return nil, 0
	}
	if apiErr != nil {
		return nil, apiErr.StatusCode
	}
	return record, http.StatusOK
}

// resourceExists reports whether a GET on the path returns anything but 404.
// Used only to tell one kind of call from another, so a transport failure
// answers false and the caller falls back to its ordinary not-found.
func resourceExists(client *api.Client, parts ...string) bool {
	_, status := readRecord(client, parts...)
	return status != 0 && status != http.StatusNotFound
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
		// A message has no hangup, so its record is not kept.
		if _, err := requireResourceExists(cmd, "Message", messageUUID, "message"); err != nil {
			return err
		}
		prompt := fmt.Sprintf("Help me debug this %s message: %s. Why did it fail / what's the status?",
			channelLabel, messageUUID) + diagnoseClientConstraints
		return runDiagnose(cmd, prompt, diagnoseTarget{label: "message", uuid: messageUUID, getCmd: "messaging get"})
	}
}

// diagnoseTarget is the record a diagnose run is about.
type diagnoseTarget struct {
	label  string          // "call" or "message"
	uuid   string          // its id
	getCmd string          // the command that reads the record directly
	record json.RawMessage // the call record from the pre-check; nil for a message or an unread record
}

// diagnoseResultInstruction asks for the -o json result block, after the
// ANALYSIS_INCOMPLETE instruction in diagnoseClientConstraints.
const diagnoseResultInstruction = " Otherwise, end your reply with one fenced ```json block holding exactly these keys:" +
	` "what_happened" (string), "likely_cause" (string), "timeline" (array of {"at": "YYYY-MM-DD HH:MM:SS" or null,` +
	` "event": string}, oldest first), "next_steps" (array of strings) and "confidence" ("high", "medium" or "low").`

// diagnoseCallTimelineNote keeps the record's anchors out of the assistant's
// timeline: the CLI adds them from the call record.
const diagnoseCallTimelineNote = " Leave the call's initiation, answer and end out of timeline: the call record supplies them."

// diagnoseResultMode reports whether diagnose answers with one JSON result:
// -o json, or no -o off a terminal. -o jsonl keeps the raw event stream.
func diagnoseResultMode() bool {
	return effectiveFormat() == output.FormatJSON && !strings.EqualFold(outputFormat, "jsonl")
}

// runDiagnose runs the diagnose turn and judges how it ended. With -o json it
// then prints one fixed result instead of the event stream.
func runDiagnose(cmd *cobra.Command, prompt string, t diagnoseTarget) error {
	resultMode := diagnoseResultMode()
	if resultMode {
		prompt += diagnoseResultInstruction
		if t.label == "call" {
			prompt += diagnoseCallTimelineNote
		}
	}
	askDiscardEvents = resultMode
	defer func() { askDiscardEvents = false }()
	err := diagnoseOutcome(runAsk(cmd, []string{prompt}), t.label, t.uuid, t.getCmd)
	if err != nil || !resultMode || dryRunFlag {
		return err
	}
	res, err := newDiagnoseResult(lastAsk.answer, t)
	if err != nil {
		return err
	}
	return output.JSONSuccess(os.Stdout, res, nil)
}

// diagnoseResult is diagnose's -o json result. The assistant supplies the
// analysis; the hangup facts and the timeline's anchors come from the call's
// own record. A message has no hangup, so those are null for one.
type diagnoseResult struct {
	CallUUID        string          `json:"call_uuid,omitempty"`
	MessageUUID     string          `json:"message_uuid,omitempty"`
	WhatHappened    string          `json:"what_happened"`
	LikelyCause     string          `json:"likely_cause"`
	Timeline        []timelineEntry `json:"timeline"`
	NextSteps       []string        `json:"next_steps"`
	HangupCauseCode *int            `json:"hangup_cause_code"`
	HangupSource    *string         `json:"hangup_source"`
	Confidence      string          `json:"confidence"`
	Answer          string          `json:"answer"`
}

// timelineEntry is one event; source is "call_record" or "assistant".
type timelineEntry struct {
	At     *string `json:"at"`
	Event  string  `json:"event"`
	Source string  `json:"source"`
}

// resultBlockRE finds fenced code blocks; the result is the last one.
var resultBlockRE = regexp.MustCompile("(?is)```[ \\t]*(?:json)?[ \\t]*\\r?\\n(.*?)```")

// newDiagnoseResult builds the -o json result from the assistant's answer and
// the call record. A missing or invalid result block is an error: the result
// never carries fields the analysis left empty.
func newDiagnoseResult(answer string, t diagnoseTarget) (*diagnoseResult, error) {
	fail := func(reason string) error {
		return &clierr.Error{
			Code:    clierr.CodeUpstreamError,
			Message: fmt.Sprintf("the assistant's answer for %s %s %s", t.label, t.uuid, reason),
			Hint: fmt.Sprintf("Re-run with -o table for the prose answer, or run `plivo %s %s` to read the %s record directly.",
				t.getCmd, t.uuid, t.label),
			Retryable:  true,
			StatusCode: http.StatusBadGateway,
			Context:    map[string]any{"answer": answer},
		}
	}
	blocks := resultBlockRE.FindAllStringSubmatchIndex(answer, -1)
	if len(blocks) == 0 {
		return nil, fail("has no JSON result block")
	}
	last := blocks[len(blocks)-1]
	var b struct {
		WhatHappened string `json:"what_happened"`
		LikelyCause  string `json:"likely_cause"`
		Timeline     []struct {
			At    *string `json:"at"`
			Event string  `json:"event"`
		} `json:"timeline"`
		NextSteps  []string `json:"next_steps"`
		Confidence string   `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(answer[last[2]:last[3]]), &b); err != nil {
		return nil, fail("has a result block that is not valid JSON: " + err.Error())
	}
	res := &diagnoseResult{
		WhatHappened: strings.TrimSpace(b.WhatHappened),
		LikelyCause:  strings.TrimSpace(b.LikelyCause),
		Confidence:   strings.ToLower(strings.TrimSpace(b.Confidence)),
		NextSteps:    []string{},
		Answer:       strings.TrimSpace(answer[:last[0]] + answer[last[1]:]),
	}
	switch {
	case res.WhatHappened == "":
		return nil, fail("has a result block without what_happened")
	case res.LikelyCause == "":
		return nil, fail("has a result block without likely_cause")
	case b.Timeline == nil:
		return nil, fail("has a result block without timeline")
	case b.NextSteps == nil:
		return nil, fail("has a result block without next_steps")
	case res.Confidence != "high" && res.Confidence != "medium" && res.Confidence != "low":
		return nil, fail(fmt.Sprintf("has a result block with confidence %q, not high, medium or low", b.Confidence))
	}
	for _, step := range b.NextSteps {
		if step = strings.TrimSpace(step); step != "" {
			res.NextSteps = append(res.NextSteps, step)
		}
	}

	entries := []timelineEntry{}
	zone := time.UTC
	if t.label == "call" {
		res.CallUUID = t.uuid
		var rec callRecord
		_ = json.Unmarshal(t.record, &rec) // an unread record leaves every fact null
		res.HangupCauseCode = rec.HangupCauseCode
		if src := strings.TrimSpace(rec.HangupSource); src != "" {
			res.HangupSource = &src
		}
		entries = append(entries, rec.anchors()...)
		zone = rec.zone()
	} else {
		res.MessageUUID = t.uuid
	}
	for _, e := range b.Timeline {
		event := strings.TrimSpace(e.Event)
		if event == "" {
			return nil, fail("has a result block with a timeline entry without an event")
		}
		var at *string
		if e.At != nil && strings.TrimSpace(*e.At) != "" {
			at = e.At
		}
		entries = append(entries, timelineEntry{At: at, Event: event, Source: "assistant"})
	}
	res.Timeline = sortTimeline(entries, zone)
	return res, nil
}

// callRecord is what -o json takes from the call's own record. Voice and SIP
// Trunking CDRs share these names.
type callRecord struct {
	HangupCauseCode *int   `json:"hangup_cause_code"`
	HangupCauseName string `json:"hangup_cause_name"`
	HangupSource    string `json:"hangup_source"`
	InitiationTime  string `json:"initiation_time"`
	AnswerTime      string `json:"answer_time"`
	EndTime         string `json:"end_time"`
}

// anchors are the record's own times, as timeline entries.
func (c callRecord) anchors() []timelineEntry {
	ended := "call ended"
	if c.HangupCauseName != "" {
		ended += ": " + c.HangupCauseName
	}
	var out []timelineEntry
	for _, a := range []struct{ at, event string }{
		{c.InitiationTime, "call initiated"},
		{c.AnswerTime, "call answered"},
		{c.EndTime, ended},
	} {
		if at := strings.TrimSpace(a.at); at != "" {
			out = append(out, timelineEntry{At: &at, Event: a.event, Source: "call_record"})
		}
	}
	return out
}

// zone is the record's UTC offset (voice CDRs carry one), so a time the
// assistant gives without an offset is read on the same clock. UTC otherwise:
// SIP Trunking times are UTC.
func (c callRecord) zone() *time.Location {
	if t, err := time.Parse(timelineLayouts[0], strings.TrimSpace(c.InitiationTime)); err == nil {
		return t.Location()
	}
	return time.UTC
}

// timelineLayouts are the call records' timestamp shapes (with and without an
// offset) and RFC 3339.
var timelineLayouts = []string{"2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05", time.RFC3339, "2006-01-02T15:04:05"}

// sortTimeline orders entries oldest first; a time without an offset is read
// in zone. Entries without a readable time follow the dated ones in their own
// order, and on a tie the record's anchor stays first (the sort is stable).
func sortTimeline(entries []timelineEntry, zone *time.Location) []timelineEntry {
	type dated struct {
		entry timelineEntry
		at    time.Time
		ok    bool
	}
	ds := make([]dated, len(entries))
	for i, e := range entries {
		ds[i].entry = e
		if e.At != nil {
			ds[i].at, ds[i].ok = parseTimelineTime(*e.At, zone)
		}
	}
	sort.SliceStable(ds, func(i, j int) bool { return ds[i].ok && (!ds[j].ok || ds[i].at.Before(ds[j].at)) })
	for i := range ds {
		entries[i] = ds[i].entry
	}
	return entries
}

// parseTimelineTime reads at in any of timelineLayouts; a time without an
// offset is read in zone.
func parseTimelineTime(at string, zone *time.Location) (time.Time, bool) {
	for _, layout := range timelineLayouts {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(at), zone); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
