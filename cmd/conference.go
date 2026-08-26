package cmd

import (
	"fmt"
	"os"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var conferenceCmd = &cobra.Command{
	Use:     "conferences",
	Aliases: []string{"conf", "conference"},
	Short:   "Inspect and control live audio conferences",
}

var confListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active conference names",
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

var (
	confMemberPlayURLs   string
	confMemberPlayLength int
	confMemberPlayLoop   bool
	confMemberPlayMix    bool
)

var confMemberPlayCmd = &cobra.Command{
	Use:   "play <conference_name> <member_id>",
	Short: "Play audio file(s) into a member's channel",
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
	confRecordTranscribe  bool
)

var confRecordCmd = &cobra.Command{
	Use:   "record <conference_name>",
	Short: "Start recording a conference",
	Args:  cobra.ExactArgs(1),
	RunE:  runConferenceRecord,
}

var confStopRecordCmd = &cobra.Command{
	Use:   "stop-record <conference_name>",
	Short: "Stop the active recording on a conference",
	Args:  cobra.ExactArgs(1),
	RunE:  runConferenceStopRecord,
}

func init() {
	confMemberPlayCmd.Flags().StringVar(&confMemberPlayURLs, "urls", "", "comma-separated audio file URLs (required)")
	_ = confMemberPlayCmd.MarkFlagRequired("urls")
	confMemberPlayCmd.Flags().IntVar(&confMemberPlayLength, "length", 0, "stop after N seconds")
	confMemberPlayCmd.Flags().BoolVar(&confMemberPlayLoop, "loop", false, "loop playback")
	confMemberPlayCmd.Flags().BoolVar(&confMemberPlayMix, "mix", true, "mix with conference audio")

	confMemberSpeakCmd.Flags().StringVar(&confMemberSpeakText, "text", "", "TTS text (required)")
	_ = confMemberSpeakCmd.MarkFlagRequired("text")
	confMemberSpeakCmd.Flags().StringVar(&confMemberSpeakVoice, "voice", "WOMAN", "MAN|WOMAN")
	confMemberSpeakCmd.Flags().StringVar(&confMemberSpeakLang, "language", "en-US", "BCP-47 language code")

	confRecordCmd.Flags().IntVar(&confRecordTimeLimit, "time-limit", 60, "max recording length in seconds")
	confRecordCmd.Flags().StringVar(&confRecordFileFormat, "file-format", "mp3", "mp3|wav")
	confRecordCmd.Flags().StringVar(&confRecordCallbackURL, "callback-url", "", "URL hit when recording finishes")
	confRecordCmd.Flags().BoolVar(&confRecordTranscribe, "transcribe", false, "request transcription")

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
		return output.JSONRaw(os.Stdout, resp.Raw())
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
	if !yesFlag {
		return clierr.DestructiveRefused("end conference " + name)
	}
	return doConferenceDelete(name, "")
}

func runConferenceMemberKick(cmd *cobra.Command, args []string) error {
	name, member := args[0], args[1]
	if !yesFlag {
		return clierr.DestructiveRefused("kick member " + member + " from " + name)
	}
	return doConferenceMemberDelete(name, member, "")
}

func runConferenceMemberMute(cmd *cobra.Command, args []string) error {
	return doConferenceMemberPost(args[0], args[1], "Mute", nil, "muted")
}

func runConferenceMemberUnmute(cmd *cobra.Command, args []string) error {
	return doConferenceMemberDelete(args[0], args[1], "Mute")
}

func runConferenceMemberDeaf(cmd *cobra.Command, args []string) error {
	return doConferenceMemberPost(args[0], args[1], "Deaf", nil, "deafened")
}

func runConferenceMemberUndeaf(cmd *cobra.Command, args []string) error {
	return doConferenceMemberDelete(args[0], args[1], "Deaf")
}

func runConferenceMemberPlay(cmd *cobra.Command, args []string) error {
	body := map[string]any{
		"urls": confMemberPlayURLs,
		"loop": confMemberPlayLoop,
		"mix":  confMemberPlayMix,
	}
	if confMemberPlayLength > 0 {
		body["length"] = confMemberPlayLength
	}
	return doConferenceMemberPost(args[0], args[1], "Play", body, "playing audio")
}

func runConferenceMemberStopPlay(cmd *cobra.Command, args []string) error {
	return doConferenceMemberDelete(args[0], args[1], "Play")
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
	return doConferenceMemberDelete(args[0], args[1], "Speak")
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
		"transcribe":  confRecordTranscribe,
	}
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
		fmt.Fprintf(os.Stderr, "Ended conference %s\n", name)
	} else {
		fmt.Fprintf(os.Stderr, "Stopped %s on conference %s\n", sub, name)
	}
	return nil
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
	fmt.Fprintf(os.Stderr, "%s: %s/%s — %s\n", label, name, member, resp.Message)
	return nil
}

func doConferenceMemberDelete(name, member, sub string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	parts := []string{"Conference", name, "Member", member}
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
		fmt.Fprintf(os.Stderr, "Kicked member %s from %s\n", member, name)
	} else {
		fmt.Fprintf(os.Stderr, "Cleared %s on %s/%s\n", sub, name, member)
	}
	return nil
}
