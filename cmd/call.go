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

var callCmd = &cobra.Command{
	Use:     "calls",
	Aliases: []string{"call"},
	Short:   "Make and inspect voice calls",
}

var (
	callListLimit     int
	callListOffset    int
	callListFrom      string
	callListTo        string
	callListDirection string
)

var callListCmd = &cobra.Command{
	Use:   "list",
	Short: "List calls",
	RunE:  runCallList,
}

var callGetCmd = &cobra.Command{
	Use:   "get <call_uuid>",
	Short: "Get a call by UUID",
	Args:  cobra.ExactArgs(1),
	RunE:  runCallGet,
}

var (
	callMakeFrom          string
	callMakeTo            string
	callMakeAnswerURL     string
	callMakeAnswerMethod  string
	callMakeHangupURL     string
	callMakeRingURL       string
	callMakeMachineDetect string
)

var callMakeCmd = &cobra.Command{
	Use:   "make",
	Short: "Make an outbound call (defaults to dry-run; pass --yes to actually dial)",
	RunE:  runCallMake,
}

func init() {
	callListCmd.Flags().IntVar(&callListLimit, "limit", 20, "results per page")
	callListCmd.Flags().IntVar(&callListOffset, "offset", 0, "pagination offset")
	callListCmd.Flags().StringVar(&callListFrom, "from", "", "filter by from_number")
	callListCmd.Flags().StringVar(&callListTo, "to", "", "filter by to_number")
	callListCmd.Flags().StringVar(&callListDirection, "direction", "", "inbound|outbound")

	callMakeCmd.Flags().StringVar(&callMakeFrom, "from", "", "source number (E.164) — must be on your account (required)")
	_ = callMakeCmd.MarkFlagRequired("from")
	callMakeCmd.Flags().StringVar(&callMakeTo, "to", "", "destination number (E.164) (required)")
	_ = callMakeCmd.MarkFlagRequired("to")
	callMakeCmd.Flags().StringVar(&callMakeAnswerURL, "answer-url", "https://s3.amazonaws.com/static.plivo.com/answer.xml",
		"URL returning PlivoXML to play on answer (default: Plivo's hello demo)")
	callMakeCmd.Flags().StringVar(&callMakeAnswerMethod, "answer-method", "GET", "GET|POST")
	callMakeCmd.Flags().StringVar(&callMakeHangupURL, "hangup-url", "", "URL hit when call ends")
	callMakeCmd.Flags().StringVar(&callMakeRingURL, "ring-url", "", "URL hit when call starts ringing")
	callMakeCmd.Flags().StringVar(&callMakeMachineDetect, "machine-detection", "", "none|true|hangup")

	callTransferCmd.Flags().StringVar(&callTransferLegs, "legs", "aleg", "which leg(s) to act on: aleg|bleg|both")
	callTransferCmd.Flags().StringVar(&callTransferAlegURL, "aleg-url", "", "new URL for A-leg (caller side)")
	callTransferCmd.Flags().StringVar(&callTransferBlegURL, "bleg-url", "", "new URL for B-leg (callee side)")
	callTransferCmd.Flags().StringVar(&callTransferAlegMethod, "aleg-method", "POST", "GET|POST")
	callTransferCmd.Flags().StringVar(&callTransferBlegMethod, "bleg-method", "POST", "GET|POST")

	callPlayCmd.Flags().StringVar(&callPlayURLs, "urls", "", "comma-separated list of audio file URLs to play (required)")
	_ = callPlayCmd.MarkFlagRequired("urls")
	callPlayCmd.Flags().IntVar(&callPlayLength, "length", 0, "stop after N seconds (0 = full file)")
	callPlayCmd.Flags().StringVar(&callPlayLegs, "legs", "aleg", "aleg|bleg|both")
	callPlayCmd.Flags().BoolVar(&callPlayLoop, "loop", false, "loop the playback")
	callPlayCmd.Flags().BoolVar(&callPlayMix, "mix", true, "mix with the call audio (else replace)")

	callSpeakCmd.Flags().StringVar(&callSpeakText, "text", "", "text to speak via TTS (required)")
	_ = callSpeakCmd.MarkFlagRequired("text")
	callSpeakCmd.Flags().StringVar(&callSpeakVoice, "voice", "WOMAN", "voice: MAN|WOMAN")
	callSpeakCmd.Flags().StringVar(&callSpeakLang, "language", "en-US", "language code, e.g. en-US, en-GB, hi-IN")
	callSpeakCmd.Flags().StringVar(&callSpeakLegs, "legs", "aleg", "aleg|bleg|both")
	callSpeakCmd.Flags().BoolVar(&callSpeakMix, "mix", true, "mix with the call audio (else replace)")

	callDTMFCmd.Flags().StringVar(&callDTMFDigits, "digits", "", "DTMF digits to send, e.g. 1234# (required)")
	_ = callDTMFCmd.MarkFlagRequired("digits")
	callDTMFCmd.Flags().StringVar(&callDTMFLeg, "leg", "aleg", "aleg|bleg")

	callRecordCmd.Flags().IntVar(&callRecordTimeLimit, "time-limit", 60, "max recording length in seconds")
	callRecordCmd.Flags().StringVar(&callRecordFileFormat, "file-format", "mp3", "mp3|wav")
	callRecordCmd.Flags().StringVar(&callRecordCallbackURL, "callback-url", "", "URL hit when recording finishes")
	callRecordCmd.Flags().StringVar(&callRecordCallbackMethod, "callback-method", "POST", "GET|POST")
	callRecordCmd.Flags().BoolVar(&callRecordTranscribe, "transcribe", false, "request transcription")
	callRecordCmd.Flags().BoolVar(&callRecordBothLegs, "both-legs", false, "record both legs (default: just A-leg)")

	callCmd.AddCommand(callListCmd, callGetCmd, callMakeCmd, callHangupCmd, callTransferCmd,
		callPlayCmd, callStopPlayCmd, callSpeakCmd, callStopSpeakCmd, callDTMFCmd,
		callRecordCmd, callStopRecordCmd)
	voiceCmd.AddCommand(callCmd)
}

var callHangupCmd = &cobra.Command{
	Use:   "hangup <call_uuid>",
	Short: "Hang up a live call (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runCallHangup,
}

var (
	callTransferLegs       string
	callTransferAlegURL    string
	callTransferBlegURL    string
	callTransferAlegMethod string
	callTransferBlegMethod string
)

var callTransferCmd = &cobra.Command{
	Use:   "transfer <call_uuid>",
	Short: "Transfer one or both legs of a live call to new PlivoXML URLs",
	Args:  cobra.ExactArgs(1),
	RunE:  runCallTransfer,
}

func runCallHangup(cmd *cobra.Command, args []string) error {
	uuidStr := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("hangup call " + uuidStr)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("Call", uuidStr), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Hung up %s\n", uuidStr)
	return nil
}

func runCallTransfer(cmd *cobra.Command, args []string) error {
	uuidStr := args[0]
	if callTransferAlegURL == "" && callTransferBlegURL == "" {
		return clierr.BadInput("at least one of --aleg-url or --bleg-url required")
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{"legs": callTransferLegs}
	if callTransferAlegURL != "" {
		body["aleg_url"] = callTransferAlegURL
		body["aleg_method"] = callTransferAlegMethod
	}
	if callTransferBlegURL != "" {
		body["bleg_url"] = callTransferBlegURL
		body["bleg_method"] = callTransferBlegMethod
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("Call", uuidStr), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Transferred %s: %s\n", uuidStr, resp.Message)
	return nil
}

var (
	callPlayURLs   string
	callPlayLength int
	callPlayLegs   string
	callPlayLoop   bool
	callPlayMix    bool
)

var callPlayCmd = &cobra.Command{
	Use:   "play <call_uuid>",
	Short: "Play audio file(s) into a live call",
	Args:  cobra.ExactArgs(1),
	RunE:  runCallPlay,
}

var callStopPlayCmd = &cobra.Command{
	Use:   "stop-play <call_uuid>",
	Short: "Stop any audio currently playing in a live call",
	Args:  cobra.ExactArgs(1),
	RunE:  runCallStopPlay,
}

var (
	callSpeakText  string
	callSpeakVoice string
	callSpeakLang  string
	callSpeakLegs  string
	callSpeakMix   bool
)

var callSpeakCmd = &cobra.Command{
	Use:   "speak <call_uuid>",
	Short: "Speak text into a live call via TTS",
	Args:  cobra.ExactArgs(1),
	RunE:  runCallSpeak,
}

var callStopSpeakCmd = &cobra.Command{
	Use:   "stop-speak <call_uuid>",
	Short: "Stop any TTS currently playing in a live call",
	Args:  cobra.ExactArgs(1),
	RunE:  runCallStopSpeak,
}

func runCallPlay(cmd *cobra.Command, args []string) error {
	uuidStr := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"urls": callPlayURLs,
		"legs": callPlayLegs,
		"loop": callPlayLoop,
		"mix":  callPlayMix,
	}
	if callPlayLength > 0 {
		body["length"] = callPlayLength
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("Call", uuidStr, "Play"), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Playing on %s: %s\n", uuidStr, resp.Message)
	return nil
}

func runCallStopPlay(cmd *cobra.Command, args []string) error {
	return runSubresourceDelete(cmd, args[0], "Play", "stopped audio")
}

func runCallSpeak(cmd *cobra.Command, args []string) error {
	uuidStr := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"text":     callSpeakText,
		"voice":    callSpeakVoice,
		"language": callSpeakLang,
		"legs":     callSpeakLegs,
		"mix":      callSpeakMix,
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("Call", uuidStr, "Speak"), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Speaking on %s: %s\n", uuidStr, resp.Message)
	return nil
}

func runCallStopSpeak(cmd *cobra.Command, args []string) error {
	return runSubresourceDelete(cmd, args[0], "Speak", "stopped TTS")
}

var (
	callDTMFDigits string
	callDTMFLeg    string
)

var callDTMFCmd = &cobra.Command{
	Use:   "dtmf <call_uuid>",
	Short: "Send DTMF tones into a live call",
	Args:  cobra.ExactArgs(1),
	RunE:  runCallDTMF,
}

var (
	callRecordTimeLimit      int
	callRecordFileFormat     string
	callRecordCallbackURL    string
	callRecordCallbackMethod string
	callRecordTranscribe     bool
	callRecordBothLegs       bool
)

var callRecordCmd = &cobra.Command{
	Use:   "record <call_uuid>",
	Short: "Start recording a live call",
	Args:  cobra.ExactArgs(1),
	RunE:  runCallRecord,
}

var callStopRecordCmd = &cobra.Command{
	Use:   "stop-record <call_uuid>",
	Short: "Stop the active recording on a live call",
	Args:  cobra.ExactArgs(1),
	RunE:  runCallStopRecord,
}

func runCallDTMF(cmd *cobra.Command, args []string) error {
	uuidStr := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"digits": callDTMFDigits,
		"leg":    callDTMFLeg,
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("Call", uuidStr, "DTMF"), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Sent DTMF on %s: %s\n", uuidStr, resp.Message)
	return nil
}

func runCallRecord(cmd *cobra.Command, args []string) error {
	uuidStr := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"time_limit":  callRecordTimeLimit,
		"file_format": callRecordFileFormat,
		"transcribe":  callRecordTranscribe,
		"both_legs":   callRecordBothLegs,
	}
	if callRecordCallbackURL != "" {
		body["callback_url"] = callRecordCallbackURL
		body["callback_method"] = callRecordCallbackMethod
	}
	var resp struct {
		APIID       string `json:"api_id"`
		Message     string `json:"message"`
		RecordingID string `json:"recording_id"`
		URL         string `json:"url"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("Call", uuidStr, "Record"), body, nil, &resp)
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
		return output.JSONSuccess(os.Stdout, resp, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"recording_id", resp.RecordingID},
		{"url", resp.URL},
		{"message", resp.Message},
	})
}

func runCallStopRecord(cmd *cobra.Command, args []string) error {
	return runSubresourceDelete(cmd, args[0], "Record", "stopped recording")
}

// runSubresourceDelete is a tiny helper for the symmetrical stop-* commands
// (stop-play, stop-speak, stop-record) — they all DELETE the matching subresource.
func runSubresourceDelete(cmd *cobra.Command, uuidStr, sub, doneLabel string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("Call", uuidStr, sub), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "%s on %s\n", doneLabel, uuidStr)
	return nil
}

func runCallMake(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}

	body := map[string]any{
		"from":          callMakeFrom,
		"to":            callMakeTo,
		"answer_url":    callMakeAnswerURL,
		"answer_method": callMakeAnswerMethod,
	}
	if callMakeHangupURL != "" {
		body["hangup_url"] = callMakeHangupURL
	}
	if callMakeRingURL != "" {
		body["ring_url"] = callMakeRingURL
	}
	if callMakeMachineDetect != "" {
		body["machine_detection"] = callMakeMachineDetect
	}

	effectiveDryRun := dryRunFlag || !yesFlag
	if effectiveDryRun {
		client.DryRun = true
		if !dryRunFlag {
			fmt.Fprintln(os.Stderr, "[dry-run] call make defaults to dry-run; pass --yes to actually dial")
		}
	}

	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will POST %s (from=%s to=%s answer_url=%s)\n",
			client.AccountURL("Call"), callMakeFrom, callMakeTo, callMakeAnswerURL)
	}

	var resp struct {
		APIID       string `json:"api_id"`
		Message     string `json:"message"`
		RequestUUID string `json:"request_uuid"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("Call"), body, nil, &resp)
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
		{"request_uuid", resp.RequestUUID},
		{"message", resp.Message},
	})
}

func runCallList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(callListLimit))
	q.Set("offset", strconv.Itoa(callListOffset))
	if callListFrom != "" {
		q.Set("from_number", callListFrom)
	}
	if callListTo != "" {
		q.Set("to_number", callListTo)
	}
	if callListDirection != "" {
		q.Set("call_direction", callListDirection)
	}

	var resp api.CallList
	apiErr, err := client.Do("GET", client.AccountURL("Call"), nil, q, &resp)
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
	rows := [][]string{{"UUID", "FROM", "TO", "DIR", "DUR", "TIME", "AMOUNT"}}
	for _, c := range resp.Objects {
		rows = append(rows, []string{c.CallUUID, c.From, c.To, c.Direction, strconv.Itoa(c.BillDuration), c.InitTime, c.TotalAmount})
	}
	return output.Table(os.Stdout, rows)
}

func runCallGet(cmd *cobra.Command, args []string) error {
	uuid := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var c api.Call
	apiErr, err := client.Do("GET", client.AccountURL("Call", uuid), nil, nil, &c)
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
		return output.JSONSuccess(os.Stdout, c, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"uuid", c.CallUUID},
		{"from", c.From},
		{"to", c.To},
		{"direction", c.Direction},
		{"bill_duration", strconv.Itoa(c.BillDuration)},
		{"call_duration", strconv.Itoa(c.CallDuration)},
		{"answer_time", c.AnswerTime},
		{"end_time", c.EndTime},
		{"init_time", c.InitTime},
		{"hangup_cause", c.HangupCause},
		{"hangup_source", c.HangupSource},
		{"total_amount", c.TotalAmount},
	})
}
