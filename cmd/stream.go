package cmd

import (
	"fmt"
	"os"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var streamCmd = &cobra.Command{
	Use:     "streams",
	Aliases: []string{"stream"},
	Short:   "Live audio streams on a call (WebSocket bridge for transcription / agents)",
}

var streamListCmd = &cobra.Command{
	Use:   "list <call_uuid>",
	Short: "List active streams on a call",
	Args:  cobra.ExactArgs(1),
	RunE:  runStreamList,
}

var streamGetCmd = &cobra.Command{
	Use:   "get <call_uuid> <stream_id>",
	Short: "Get details for one stream on a call",
	Args:  cobra.ExactArgs(2),
	RunE:  runStreamGet,
}

var (
	streamStartURL          string
	streamStartTrack        string
	streamStartBiDi         bool
	streamStartContentType  string
	streamStartCallbackURL  string
	streamStartStatusURL    string
	streamStartExtraHeaders string
	streamStartServiceType  string
)

var streamStartCmd = &cobra.Command{
	Use:   "start <call_uuid>",
	Short: "Start a new audio stream on a call",
	Args:  cobra.ExactArgs(1),
	RunE:  runStreamStart,
}

var streamStopCmd = &cobra.Command{
	Use:   "stop <call_uuid> [stream_id]",
	Short: "Stop one stream (with id) or all streams (without id) on a call",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runStreamStop,
}

func init() {
	streamStartCmd.Flags().StringVar(&streamStartURL, "url", "", "WebSocket URL to receive audio (wss://...) (required)")
	_ = streamStartCmd.MarkFlagRequired("url")
	streamStartCmd.Flags().StringVar(&streamStartTrack, "audio-track", "inbound", "inbound|outbound|both")
	streamStartCmd.Flags().BoolVar(&streamStartBiDi, "bidirectional", false, "let the WebSocket send audio back into the call")
	streamStartCmd.Flags().StringVar(&streamStartContentType, "content-type", "audio/x-l16;rate=16000", "audio codec content-type")
	streamStartCmd.Flags().StringVar(&streamStartCallbackURL, "stream-status-callback", "", "URL hit when stream starts/ends")
	streamStartCmd.Flags().StringVar(&streamStartStatusURL, "callback-url", "", "alias for --stream-status-callback")
	streamStartCmd.Flags().StringVar(&streamStartExtraHeaders, "extra-headers", "", "comma-separated extra WebSocket headers (k1=v1,k2=v2)")
	streamStartCmd.Flags().StringVar(&streamStartServiceType, "service-type", "", "Plivo service type override")

	streamCmd.AddCommand(streamListCmd, streamGetCmd, streamStartCmd, streamStopCmd)
	callCmd.AddCommand(streamCmd)
}

func runStreamList(cmd *cobra.Command, args []string) error {
	callUUID := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.AudioStreamList
	apiErr, err := client.Do("GET", client.AccountURL("Call", callUUID, "Stream"), nil, nil, &resp)
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
	rows := [][]string{{"STREAM_ID", "STATUS", "TRACK", "STARTED"}}
	for _, s := range resp.Objects {
		rows = append(rows, []string{s.StreamID, s.Status, s.AudioTrack, s.StartTime})
	}
	return output.Table(os.Stdout, rows)
}

func runStreamGet(cmd *cobra.Command, args []string) error {
	callUUID, streamID := args[0], args[1]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var s api.AudioStream
	apiErr, err := client.Do("GET", client.AccountURL("Call", callUUID, "Stream", streamID), nil, nil, &s)
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
		return output.JSONRaw(os.Stdout, s.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"stream_id", s.StreamID},
		{"call_uuid", s.CallUUID},
		{"stream_url", s.StreamURL},
		{"status", s.Status},
		{"audio_track", s.AudioTrack},
		{"bidirectional", fmt.Sprintf("%v", s.BiDirectional)},
		{"start_time", s.StartTime},
		{"end_time", s.EndTime},
	})
}

func runStreamStart(cmd *cobra.Command, args []string) error {
	callUUID := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"service_url":   streamStartURL,
		"audio_track":   streamStartTrack,
		"bidirectional": streamStartBiDi,
		"content_type":  streamStartContentType,
	}
	callback := streamStartCallbackURL
	if callback == "" {
		callback = streamStartStatusURL
	}
	if callback != "" {
		body["stream_status_callback_url"] = callback
	}
	if streamStartExtraHeaders != "" {
		body["extra_headers"] = streamStartExtraHeaders
	}
	if streamStartServiceType != "" {
		body["service_type"] = streamStartServiceType
	}

	var resp struct {
		api.RawBody
		APIID    string `json:"api_id"`
		Message  string `json:"message"`
		StreamID string `json:"stream_id"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("Call", callUUID, "Stream"), body, nil, &resp)
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
		{"stream_id", resp.StreamID},
		{"message", resp.Message},
	})
}

func runStreamStop(cmd *cobra.Command, args []string) error {
	callUUID := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("stop stream(s) on call " + callUUID)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	parts := []string{"Call", callUUID, "Stream"}
	if len(args) == 2 {
		parts = append(parts, args[1])
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
	if len(args) == 2 {
		fmt.Fprintf(os.Stderr, "Stopped stream %s on call %s\n", args[1], callUUID)
	} else {
		fmt.Fprintf(os.Stderr, "Stopped all streams on call %s\n", callUUID)
	}
	return nil
}
