package cmd

import (
	"fmt"
	"os"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var conferenceCmd = &cobra.Command{
	Use:     "conferences",
	Aliases: []string{"conf", "conference"},
	Short:   "Inspect and control live audio conferences",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var confListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active conference names",
	Args:  cobra.NoArgs,
	RunE:  runConferenceList,
}

var confGetCmd = &cobra.Command{
	Use:   "get <conference_name>",
	Short: "Get conference details (members, run time)",
	Args:  cobra.ExactArgs(1),
	RunE:  runConferenceGet,
}

var confHangupCmd = &cobra.Command{
	Use:   "hangup <conference_name>",
	Short: "End the entire conference (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runConferenceHangup,
}

// member sub-group
var confMemberCmd = &cobra.Command{
	Use:   "member",
	Short: "Per-member actions inside a conference",
	Args:  cobra.NoArgs,
	RunE:  groupRunE,
}

var confMemberKickCmd = &cobra.Command{
	Use:   "kick <conference_name> <member_id>",
	Short: "Kick a member from the conference (requires --yes)",
	Args:  cobra.ExactArgs(2),
	RunE:  runConferenceMemberKick,
}

var confMemberMuteCmd = &cobra.Command{
	Use:   "mute <conference_name> <member_id>",
	Short: "Mute a member",
	Args:  cobra.ExactArgs(2),
	RunE:  runConferenceMemberMute,
}

var confMemberUnmuteCmd = &cobra.Command{
	Use:   "unmute <conference_name> <member_id>",
	Short: "Unmute a member",
	Args:  cobra.ExactArgs(2),
	RunE:  runConferenceMemberUnmute,
}

var confMemberDeafCmd = &cobra.Command{
	Use:   "deaf <conference_name> <member_id>",
	Short: "Deafen a member (they can't hear)",
	Args:  cobra.ExactArgs(2),
	RunE:  runConferenceMemberDeaf,
}

var confMemberUndeafCmd = &cobra.Command{
	Use:   "undeaf <conference_name> <member_id>",
	Short: "Undeafen a member",
	Args:  cobra.ExactArgs(2),
	RunE:  runConferenceMemberUndeaf,
}

var confMemberPlayURL string

var confMemberPlayCmd = &cobra.Command{
	Use:   "play <conference_name> <member_id>",
	Short: "Play an audio file into a member's channel",
	Args:  cobra.ExactArgs(2),
	RunE:  runConferenceMemberPlay,
}

var confMemberStopPlayCmd = &cobra.Command{
	Use:   "stop-play <conference_name> <member_id>",
	Short: "Stop audio playback to a member",
	Args:  cobra.ExactArgs(2),
	RunE:  runConferenceMemberStopPlay,
}

var (
	confMemberSpeakText  string
	confMemberSpeakVoice string
	confMemberSpeakLang  string
)

var confMemberSpeakCmd = &cobra.Command{
	Use:   "speak <conference_name> <member_id>",
	Short: "Speak TTS text to a member",
	Args:  cobra.ExactArgs(2),
	RunE:  runConferenceMemberSpeak,
}

var confMemberStopSpeakCmd = &cobra.Command{
	Use:   "stop-speak <conference_name> <member_id>",
	Short: "Stop TTS playback to a member",
	Args:  cobra.ExactArgs(2),
	RunE:  runConferenceMemberStopSpeak,
}

// conference-level recording
var (
	confRecordTimeLimit   int
	confRecordFileFormat  string
	confRecordCallbackURL string
	confRecordTransType   string
	confRecordTransURL    string
)

var confRecordCmd = &cobra.Command{
	Use:   "record <conference_name>",
	Short: "Start recording a conference",
	Args:  cobra.ExactArgs(1),
	RunE:  runConferenceRecord,
}

var confStopRecordCmd = &cobra.Command{
	Use:   "stop-record <conference_name>",
	Short: "Stop the active recording on a conference (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runConferenceStopRecord,
}

func init() {
	confMemberPlayCmd.Flags().StringVar(&confMemberPlayURL, "url", "", "URL of the .mp3 or .wav file to play (required)")
	_ = confMemberPlayCmd.MarkFlagRequired("url")

	confMemberSpeakCmd.Flags().StringVar(&confMemberSpeakText, "text", "", "TTS text (required)")
	_ = confMemberSpeakCmd.MarkFlagRequired("text")
	confMemberSpeakCmd.Flags().StringVar(&confMemberSpeakVoice, "voice", "WOMAN", "MAN|WOMAN")
	confMemberSpeakCmd.Flags().StringVar(&confMemberSpeakLang, "language", "en-US", "BCP-47 language code")

	confRecordCmd.Flags().IntVar(&confRecordTimeLimit, "time-limit", 60, "max recording length in seconds")
	confRecordCmd.Flags().StringVar(&confRecordFileFormat, "file-format", "mp3", "mp3|wav")
	confRecordCmd.Flags().StringVar(&confRecordCallbackURL, "callback-url", "", "URL hit when recording finishes")
	confRecordCmd.Flags().StringVar(&confRecordTransType, "transcription-type", "", "transcription type: auto (results go to --transcription-url)")
	confRecordCmd.Flags().StringVar(&confRecordTransURL, "transcription-url", "", "URL the transcription is sent to")
	ignoredFlag(confRecordCmd, "transcribe", true, transcribeNote)

	confMemberCmd.AddCommand(confMemberKickCmd, confMemberMuteCmd, confMemberUnmuteCmd,
		confMemberDeafCmd, confMemberUndeafCmd, confMemberPlayCmd, confMemberStopPlayCmd,
		confMemberSpeakCmd, confMemberStopSpeakCmd)

	conferenceCmd.AddCommand(confListCmd, confGetCmd, confHangupCmd, confRecordCmd, confStopRecordCmd, confMemberCmd)
	voiceCmd.AddCommand(conferenceCmd)
}

func runConferenceList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.ConferenceList
	apiErr, err := client.Do("GET", client.AccountURL("Conference"), nil, nil, &resp)
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
		return listJSON(os.Stdout, resp.Raw(), "conferences")
	}
	if len(resp.Conferences) == 0 {
		fmt.Fprintln(os.Stdout, "(no active conferences)")
		return nil
	}
	rows := [][]string{{"CONFERENCE_NAME"}}
	for _, n := range resp.Conferences {
		rows = append(rows, []string{n})
	}
	return output.Table(os.Stdout, rows)
}

func runConferenceGet(cmd *cobra.Command, args []string) error {
	name := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var c api.Conference
	apiErr, err := client.Do("GET", client.AccountURL("Conference", name), nil, nil, &c)
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
	_ = output.KV(os.Stdout, [][2]string{
		{"conference_name", c.ConferenceName},
		{"run_time", c.ConferenceRunTime},
		{"member_count", c.ConferenceMemberCount},
	})
	if len(c.Members) == 0 {
		return nil
	}
	rows := [][]string{{"MEMBER_ID", "FROM", "TO", "CALL_UUID", "MUTED", "DEAF"}}
	for _, m := range c.Members {
		rows = append(rows, []string{m.MemberID, m.From, m.To, m.CallUUID, fmt.Sprintf("%v", m.Muted), fmt.Sprintf("%v", m.Deaf)})
	}
	fmt.Fprintln(os.Stdout)
	return output.Table(os.Stdout, rows)
}

func runConferenceHangup(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := confirmDestructive("end conference " + name); err != nil {
		return err
	}
	return doConferenceDelete(name, "")
}

func runConferenceMemberKick(cmd *cobra.Command, args []string) error {
	name, member := args[0], args[1]
	if err := confirmDestructive("kick member " + member + " from " + name); err != nil {
		return err
	}
	// POST .../Kick/: a DELETE on the member hangs up its call instead.
	return doConferenceMemberCall("POST", name, member, "Kick")
}

func runConferenceMemberMute(cmd *cobra.Command, args []string) error {
	return doConferenceMemberPost(args[0], args[1], "Mute", nil, "muted")
}

func runConferenceMemberUnmute(cmd *cobra.Command, args []string) error {
	return doConferenceMemberCall("DELETE", args[0], args[1], "Mute")
}

func runConferenceMemberDeaf(cmd *cobra.Command, args []string) error {
	return doConferenceMemberPost(args[0], args[1], "Deaf", nil, "deafened")
}

func runConferenceMemberUndeaf(cmd *cobra.Command, args []string) error {
	return doConferenceMemberCall("DELETE", args[0], args[1], "Deaf")
}

// runConferenceMemberPlay sends the one url the member Play API takes; it has
// no urls, length, loop or mix, unlike a call's.
func runConferenceMemberPlay(cmd *cobra.Command, args []string) error {
	return doConferenceMemberPost(args[0], args[1], "Play", map[string]any{"url": confMemberPlayURL}, "playing audio")
}

func runConferenceMemberStopPlay(cmd *cobra.Command, args []string) error {
	return doConferenceMemberCall("DELETE", args[0], args[1], "Play")
}

func runConferenceMemberSpeak(cmd *cobra.Command, args []string) error {
	body := map[string]any{
		"text":     confMemberSpeakText,
		"voice":    confMemberSpeakVoice,
		"language": confMemberSpeakLang,
	}
	return doConferenceMemberPost(args[0], args[1], "Speak", body, "speaking TTS")
}

func runConferenceMemberStopSpeak(cmd *cobra.Command, args []string) error {
	return doConferenceMemberCall("DELETE", args[0], args[1], "Speak")
}

func runConferenceRecord(cmd *cobra.Command, args []string) error {
	name := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"time_limit":  confRecordTimeLimit,
		"file_format": confRecordFileFormat,
	}
	putSet(body, map[string]string{"transcription_type": confRecordTransType, "transcription_url": confRecordTransURL})
	if confRecordCallbackURL != "" {
		body["callback_url"] = confRecordCallbackURL
	}
	var resp struct {
		api.RawBody
		APIID       string `json:"api_id"`
		Message     string `json:"message"`
		RecordingID string `json:"recording_id"`
		URL         string `json:"url"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("Conference", name, "Record"), body, nil, &resp)
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
		{"recording_id", resp.RecordingID},
		{"url", resp.URL},
		{"message", resp.Message},
	})
}

func runConferenceStopRecord(cmd *cobra.Command, args []string) error {
	if err := confirmDestructive("stop recording conference " + args[0]); err != nil {
		return err
	}
	return doConferenceDelete(args[0], "Record")
}

func doConferenceDelete(name, sub string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	parts := []string{"Conference", name}
	if sub != "" {
		parts = append(parts, sub)
	}
	apiErr, err := client.Do("DELETE", client.AccountURL(parts...), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if sub == "" {
		return emitWrite(fmt.Sprintf("Ended conference %s", name), map[string]any{"conference_name": name, "ended": true})
	}
	return emitWrite(fmt.Sprintf("Stopped %s on conference %s", sub, name),
		map[string]any{"conference_name": name, "resource": sub, "stopped": true})
}

func doConferenceMemberPost(name, member, sub string, body map[string]any, label string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("Conference", name, "Member", member, sub), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	return emitResponse(fmt.Sprintf("%s: %s/%s — %s", label, name, member, resp.Message), resp.Raw())
}

// doConferenceMemberCall sends method to the member's sub resource: POST Kick,
// with the empty JSON object the API's docs send, or a DELETE that undoes a
// mute, deafen, play or speak.
func doConferenceMemberCall(method, name, member, sub string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var body any
	if method == "POST" {
		body = map[string]any{}
	}
	apiErr, err := client.Do(method, client.AccountURL("Conference", name, "Member", member, sub), body, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if sub == "Kick" {
		return emitWrite(fmt.Sprintf("Kicked member %s from %s", member, name),
			map[string]any{"conference_name": name, "member_id": member, "kicked": true})
	}
	return emitWrite(fmt.Sprintf("Cleared %s on %s/%s", sub, name, member),
		map[string]any{"conference_name": name, "member_id": member, "resource": sub, "cleared": true})
}
