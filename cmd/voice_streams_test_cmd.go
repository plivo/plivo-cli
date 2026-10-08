package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
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
	voiceStreamsTestCmd.Flags().IntVar(&streamsTestRate, "rate", 8000, "sample rate in Hz (mulaw: 8000; l16: 8000 or 16000)")
	voiceStreamsTestCmd.Flags().BoolVar(&streamsTestBidirectional, "bidirectional", false, "also read frames back from the endpoint (test bot→caller path)")
	voiceStreamsTestCmd.Flags().BoolVar(&streamsTestInsecure, "insecure", false, "skip TLS verification (self-signed dev certs only)")
	_ = voiceStreamsTestCmd.MarkFlagRequired("to")

	voiceStreamsCmd.AddCommand(voiceStreamsTestCmd)
}

// streamsTestResult is the -o json summary for `plivo voice streams test` —
// one final object instead of the human progress narration.
type streamsTestResult struct {
	Connected      bool   `json:"connected"`
	HandshakeSent  bool   `json:"handshake_sent"`
	FramesSent     int    `json:"frames_sent"`
	Codec          string `json:"codec"`
	Rate           int    `json:"rate"`
	Bidirectional  bool   `json:"bidirectional"`
	FramesReadBack int    `json:"frames_read_back"`
	Errors         int    `json:"errors"` // kept for scripts: a failed send now ends the run, so always 0 here
}

func runVoiceStreamsTest(cmd *cobra.Command, _ []string) error {
	if streamsTestTo == "" {
		return clierr.BadFlag("to", "required (WebSocket URL of the endpoint to test)")
	}
	if streamsTestDuration <= 0 || streamsTestDuration > 30 {
		return clierr.BadFlag("duration", "must be 1..30 seconds")
	}
	if err := wsproxy.ValidateCodecRate(streamsTestCodec, streamsTestRate); err != nil {
		return clierr.BadFlag("codec", err.Error())
	}

	mediaFormat := wsproxy.MediaFormat{
		Encoding:   wsproxy.Encoding(streamsTestCodec),
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
	jsonOut := effectiveFormat() == output.FormatJSON
	result := streamsTestResult{Codec: streamsTestCodec, Rate: streamsTestRate, Bidirectional: streamsTestBidirectional}

	// --- Phase 1: connect ---
	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	dialStart := time.Now()
	conn, _, err := websocket.Dial(dialCtx, streamsTestTo, &websocket.DialOptions{HTTPClient: httpClient})
	dialCancel()
	if err != nil {
		return clierr.NetworkError(streamsTestTo, err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")
	result.Connected = true
	if !jsonOut {
		fmt.Fprintf(out, "✓ Connection established (%dms)\n", time.Since(dialStart).Milliseconds())
	}

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
	result.HandshakeSent = true
	if !jsonOut {
		fmt.Fprintf(out, "✓ Sent Plivo handshake frame\n")
	}

	// --- Phase 3: stream synthetic audio ---
	const frameMs = 20
	totalFrames := streamsTestDuration * 1000 / frameMs
	frames := wsproxy.SyntheticAudio(streamsTestCodec, totalFrames, frameMs, streamsTestRate)

	// The reader starts before the first media frame, so an answer is read
	// as it arrives rather than after the whole stream.
	sendStart := time.Now()
	var rb *streamsReadBack
	if streamsTestBidirectional {
		rb = startStreamsReadBack(conn)
	}
	for i, audio := range frames {
		if ctx.Err() != nil {
			break
		}
		// A write that runs out of time closes the connection, so the
		// bound only catches an endpoint that has stopped reading.
		mediaCtx, mediaCancel := context.WithTimeout(ctx, 2*time.Second)
		raw, _ := wsproxy.EncodeMedia("inbound", i+1, i*frameMs, audio)
		err := conn.Write(mediaCtx, websocket.MessageText, raw)
		mediaCancel()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			// The socket is gone after any failed write: no point retrying.
			_ = conn.CloseNow()
			if rb != nil {
				rb.stop()
			}
			return clierr.NetworkError(streamsTestTo, fmt.Errorf("send frame %d: %w", i+1, writeFailure(rb, err)))
		}
		result.FramesSent++
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
	if !jsonOut {
		fmt.Fprintf(out, "✓ Streamed %ds of synthetic %s %dHz audio (%d frames)\n",
			streamsTestDuration, streamsTestCodec, streamsTestRate, result.FramesSent)
	}

	// --- Phase 4: give the endpoint time to answer ---
	if rb != nil && ctx.Err() == nil {
		rb.waitForAnswer(ctx, time.Duration(streamsTestDuration)*time.Second)
	}

	// --- Phase 5: stop, close, then stop reading ---
	// In that order: coder/websocket drops the connection when a Read's
	// context ends, which used to cut the socket before stop went out.
	stop, _ := wsproxy.EncodeStop()
	stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), 1*time.Second)
	_ = conn.Write(stopCtx, websocket.MessageText, stop)
	stopCancel()
	_ = conn.Close(websocket.StatusNormalClosure, "test complete")

	if rb != nil {
		rb.stop()
		result.FramesReadBack = rb.frames
		if !jsonOut {
			if rb.frames == 0 {
				fmt.Fprintf(out, "⚠ No frames received back from endpoint — bot→caller path may not be wired\n")
			} else {
				fmt.Fprintf(out, "✓ Received %d frames back from endpoint (bot→caller path live)\n", rb.frames)
			}
		}
	}

	if jsonOut {
		return output.JSONSuccess(os.Stdout, result, nil)
	}
	fmt.Fprintf(out, "\nEndpoint is ready to receive Plivo audio streams.\n")
	return nil
}

// readBackLimit replaces coder/websocket's 32 KiB cap on one message: a
// second of 16 kHz l16 audio is larger once base64-encoded, and a voice
// agent may send that much in one playAudio.
const readBackLimit = 1 << 20

// streamsReadBack reads everything the endpoint sends during the test. Its
// fields are final once stop returns.
type streamsReadBack struct {
	cancel context.CancelFunc
	done   chan struct{}

	frames int   // every message, as frames_read_back has always counted
	err    error // why reading ended
}

// startStreamsReadBack reads conn until the connection closes. Its context
// is cancelled only by stop, after the stop frame and Close: a deadline on a
// Read would drop the connection.
func startStreamsReadBack(conn *websocket.Conn) *streamsReadBack {
	conn.SetReadLimit(readBackLimit)
	ctx, cancel := context.WithCancel(context.Background())
	rb := &streamsReadBack{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(rb.done)
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				rb.err = err
				return
			}
			rb.frames++
		}
	}()
	return rb
}

// waitForAnswer gives the endpoint up to window after the last media frame
// to answer, returning early if reading has ended.
func (rb *streamsReadBack) waitForAnswer(ctx context.Context, window time.Duration) {
	select {
	case <-time.After(window):
	case <-rb.done:
	case <-ctx.Done():
	}
}

// stop ends the reader and waits for it to return.
func (rb *streamsReadBack) stop() {
	rb.cancel()
	<-rb.done
}

// writeFailure explains a write the endpoint did not take. When the reader
// saw the endpoint's close frame, its code and reason say why; stop rb
// first.
func writeFailure(rb *streamsReadBack, err error) error {
	var ce websocket.CloseError
	if rb == nil || !errors.As(rb.err, &ce) {
		return err
	}
	msg := fmt.Sprintf("the endpoint closed the connection (code %d", int(ce.Code))
	if ce.Reason != "" {
		msg += fmt.Sprintf(", reason %q", ce.Reason)
	}
	return errors.New(msg + ")")
}
