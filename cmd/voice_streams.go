package cmd

import "github.com/spf13/cobra"

// voiceStreamsCmd is the workflow-command group, peer of `voice calls` /
// `voice conferences` / etc. It hosts the streaming-developer-loop verbs
// (`test`, `forward`) — distinct from `voice calls streams` which is the
// AudioStream REST resource CRUD on a specific call.
//
// Naming: plural `streams` matches the existing `calls` / `conferences` /
// `recordings` convention. Don't introduce a singular elsewhere.
var voiceStreamsCmd = &cobra.Command{
	Use:   "streams",
	Short: "Voice media streaming — local-dev WebSocket workflows",
	Long: `Voice streaming workflow commands for local development.

Distinct from 'plivo voice calls streams' (which is CRUD on a call's
AudioStream resource). The verbs here operate on the streaming layer
itself — they don't act on an existing call-bound stream.

  test      Pre-flight: dial a WebSocket endpoint with synthetic audio
            frames to verify it accepts the Plivo stream format.
  forward   Temporarily redirect an app's answer_url to a local tunnel
            so a real call's audio streams into your local WebSocket
            handler. Restores the original answer_url on Ctrl+C.`,
}

func init() {
	voiceCmd.AddCommand(voiceStreamsCmd)
}
