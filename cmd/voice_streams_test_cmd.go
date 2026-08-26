package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/wsproxy"

	"github.com/coder/websocket"
	"github.com/spf13/cobra"
)

var (
	streamsTestTo            string
	streamsTestDuration      int
	streamsTestCodec         string
	streamsTestRate          int
	streamsTestBidirectional bool
	streamsTestInsecure      bool
)

var voiceStreamsTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Pre-flight a WebSocket endpoint with synthetic Plivo audio frames",
	Long: `Open a WebSocket to --to, send a Plivo-format start frame, stream synthetic
audio for --duration seconds, then stop. Reports connection latency,
frame send rate, and any disconnects.

No call is placed and no Plivo backend interaction occurs — this is a
pure-client tool that verifies your handler can accept the Plivo stream
shape before you wire it up to a real number.`,
	Example: `  plivo voice streams test --to wss://my-bot.example.com/ws
  plivo voice streams test --to ws://localhost:7860/ws --duration 5
  plivo voice streams test --to wss://localhost:7860/ws --insecure   # self-signed dev cert
  plivo voice streams test --to wss://my-bot.example.com/ws --bidirectional`,
	RunE: runVoiceStreamsTest,
}

func init() {
	voiceStreamsTestCmd.Flags().StringVar(&streamsTestTo, "to", "", "WebSocket URL of the endpoint to test (ws:// or wss://, required)")
	voiceStreamsTestCmd.Flags().IntVar(&streamsTestDuration, "duration", 3, "seconds of synthetic audio to stream (max 30)")
	voiceStreamsTestCmd.Flags().StringVar(&streamsTestCodec, "codec", "mulaw", "audio codec: mulaw | l16")
	voiceStreamsTestCmd.Flags().IntVar(&streamsTestRate, "rate", 8000, "sample rate in Hz (8000 for mulaw, 16000 typical for l16)")
	voiceStreamsTestCmd.Flags().BoolVar(&streamsTestBidirectional, "bidirectional", false, "also read frames back from the endpoint (test bot→caller path)")
	voiceStreamsTestCmd.Flags().BoolVar(&streamsTestInsecure, "insecure", false, "skip TLS verification (self-signed dev certs only)")
	_ = voiceStreamsTestCmd.MarkFlagRequired("to")

	voiceStreamsCmd.AddCommand(voiceStreamsTestCmd)
}

func runVoiceStreamsTest(cmd *cobra.Command, _ []string) error {
	if streamsTestTo == "" {
		return clierr.BadFlag("--to", "required (WebSocket URL of the endpoint to test)")
	}
	if streamsTestDuration <= 0 || streamsTestDuration > 30 {
		return clierr.BadFlag("--duration", "must be 1..30 seconds")
	}
	if streamsTestCodec != "mulaw" && streamsTestCodec != "l16" {
		return clierr.BadFlag("--codec", "must be mulaw | l16")
	}

	mediaFormat := wsproxy.MediaFormat{
		Encoding:   codecEncoding(streamsTestCodec),
		SampleRate: streamsTestRate,
		Channels:   1,
	}

	// Build a context that cancels on SIGINT so Ctrl+C tears down cleanly
	// instead of hanging the dialer or read loop.
	ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	httpClient := &http.Client{}
	if streamsTestInsecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	out := cmd.OutOrStdout()

	// --- Phase 1: connect ---
	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	dialStart := time.Now()
	conn, _, err := websocket.Dial(dialCtx, streamsTestTo, &websocket.DialOptions{HTTPClient: httpClient})
	dialCancel()
	if err != nil {
		return clierr.NetworkError(streamsTestTo, err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")
	fmt.Fprintf(out, "✓ Connection established (%dms)\n", time.Since(dialStart).Milliseconds())

	// --- Phase 2: handshake (start frame) ---
	startFrame, err := wsproxy.EncodeStart("test-stream", "test-call", "test-account", mediaFormat)
	if err != nil {
		return clierr.Wrap(fmt.Errorf("encode start frame: %w", err))
	}
	writeCtx, writeCancel := context.WithTimeout(ctx, 2*time.Second)
	err = conn.Write(writeCtx, websocket.MessageText, startFrame)
	writeCancel()
	if err != nil {
		return clierr.NetworkError(streamsTestTo, fmt.Errorf("send start frame: %w", err))
	}
	fmt.Fprintf(out, "✓ Sent Plivo handshake frame\n")

	// --- Phase 3: stream synthetic audio ---
	const frameMs = 20
	totalFrames := streamsTestDuration * 1000 / frameMs
	frames := wsproxy.SyntheticMulaw(totalFrames, frameMs, streamsTestRate)

	sendStart := time.Now()
	var sendErrs int
	for i, audio := range frames {
		mediaCtx, mediaCancel := context.WithTimeout(ctx, 500*time.Millisecond)
		raw, _ := wsproxy.EncodeMedia("inbound", i+1, i*frameMs, audio)
		if err := conn.Write(mediaCtx, websocket.MessageText, raw); err != nil {
			mediaCancel()
			sendErrs++
			if ctx.Err() != nil {
				break
			}
			// Tolerate transient send errors up to 5 then bail.
			if sendErrs > 5 {
				return clierr.NetworkError(streamsTestTo, fmt.Errorf("write frame %d after %d send errors: %w", i+1, sendErrs, err))
			}
			continue
		}
		mediaCancel()
		// 20ms-paced; if we're falling behind, send-as-fast-as-possible
		// instead. The endpoint should drain.
		nextSend := sendStart.Add(time.Duration(i+1) * time.Duration(frameMs) * time.Millisecond)
		if d := time.Until(nextSend); d > 0 {
			select {
			case <-time.After(d):
			case <-ctx.Done():
			}
		}
	}
	fmt.Fprintf(out, "✓ Streamed %ds of synthetic %s %dHz audio (%d frames)\n",
		streamsTestDuration, streamsTestCodec, streamsTestRate, totalFrames)
	if sendErrs > 0 {
		fmt.Fprintf(out, "⚠ %d frame send errors during streaming\n", sendErrs)
	}

	// --- Phase 4: bidirectional read-back (optional) ---
	if streamsTestBidirectional {
		readCtx, readCancel := context.WithTimeout(ctx, time.Duration(streamsTestDuration)*time.Second)
		framesRead := readBidirectionalFrames(readCtx, conn)
		readCancel()
		if framesRead == 0 {
			fmt.Fprintf(out, "⚠ No frames received back from endpoint — bot→caller path may not be wired\n")
		} else {
			fmt.Fprintf(out, "✓ Received %d frames back from endpoint (bot→caller path live)\n", framesRead)
		}
	}

	// --- Phase 5: stop ---
	stop, _ := wsproxy.EncodeStop()
	stopCtx, stopCancel := context.WithTimeout(ctx, 1*time.Second)
	_ = conn.Write(stopCtx, websocket.MessageText, stop)
	stopCancel()

	fmt.Fprintf(out, "\nEndpoint is ready to receive Plivo audio streams.\n")
	return nil
}

// readBidirectionalFrames reads incoming messages until the context fires
// or the connection closes, returning how many it saw.
func readBidirectionalFrames(ctx context.Context, conn *websocket.Conn) int {
	count := 0
	for {
		_, _, err := conn.Read(ctx)
		if err != nil {
			return count
		}
		count++
	}
}

// codecEncoding maps the human-friendly --codec value to the Plivo
// mediaFormat.encoding string.
func codecEncoding(codec string) string {
	switch strings.ToLower(codec) {
	case "l16":
		return "audio/l16"
	default:
		return "audio/x-mulaw"
	}
}
