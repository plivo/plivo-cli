package cmd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/plivo/plivo-cli/internal/plivosig"
	"github.com/plivo/plivo-cli/internal/wsproxy"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var (
	streamsTestTo            string
	streamsTestDuration      int
	streamsTestCodec         string
	streamsTestRate          int
	streamsTestBidirectional bool
	streamsTestExpectAudio   bool
	streamsTestInsecure      bool
)

var voiceStreamsTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Pre-flight a WebSocket endpoint with synthetic Plivo audio frames",
	Long: `Open a WebSocket to --to, send a Plivo-format start frame, stream synthetic
audio (a 1 kHz tone) for --duration seconds, then send stop and close.
Reports the connection time (latency_ms) and any warnings. An endpoint
that closes the connection before the stop frame fails the test (exit 3).

With --bidirectional it also counts what the endpoint sends back, while the
audio streams and for up to --duration seconds after, and times the first
playAudio from the first frame sent (first_response_ms). --expect-audio
does the same and exits 1 when no playAudio comes back.

When a profile is logged in, the upgrade is signed as Plivo signs a
stream: X-Plivo-Signature-V3, keyed with the profile's auth token over the
http:// form of --to without its query, so an endpoint that checks
Plivo's signature lets it in. --profile signs for another account. Logged
out, the upgrade goes unsigned; -o json says which in signed.

No call is placed: this checks that your handler accepts the Plivo stream
shape before you wire it up to a real number.`,
	Example: `  plivo voice streams test --to wss://my-bot.example.com/ws
  plivo voice streams test --to ws://localhost:7860/ws --duration 5
  plivo voice streams test --to wss://localhost:7860/ws --insecure   # self-signed dev cert
  plivo voice streams test --to wss://my-bot.example.com/ws --bidirectional
  plivo voice streams test --to ws://localhost:8765/ws --expect-audio   # exit 1 unless playAudio comes back`,
	Args: cobra.NoArgs,
	RunE: runVoiceStreamsTest,
}

func init() {
	voiceStreamsTestCmd.Flags().StringVar(&streamsTestTo, "to", "", "WebSocket URL of the endpoint to test (ws:// or wss://, required)")
	voiceStreamsTestCmd.Flags().IntVar(&streamsTestDuration, "duration", 3, "seconds of synthetic audio to stream (max 30)")
	voiceStreamsTestCmd.Flags().StringVar(&streamsTestCodec, "codec", "mulaw", "audio codec: mulaw | l16")
	voiceStreamsTestCmd.Flags().IntVar(&streamsTestRate, "rate", 8000, "sample rate in Hz (mulaw: 8000; l16: 8000 or 16000)")
	voiceStreamsTestCmd.Flags().BoolVar(&streamsTestBidirectional, "bidirectional", false, "also read frames back from the endpoint (test bot→caller path)")
	voiceStreamsTestCmd.Flags().BoolVar(&streamsTestExpectAudio, "expect-audio", false, "exit 1 unless playAudio comes back (implies --bidirectional); the test audio is a tone, not speech, so only an echo-style bot or one that speaks first answers")
	voiceStreamsTestCmd.Flags().BoolVar(&streamsTestInsecure, "insecure", false, "skip TLS verification (self-signed dev certs only)")
	_ = voiceStreamsTestCmd.MarkFlagRequired("to")

	voiceStreamsCmd.AddCommand(voiceStreamsTestCmd)
}

// streamsTestResult is the -o json summary for `plivo voice streams test` —
// one final object instead of the human progress narration. Fields are only
// ever added: scripts and the audio-streaming skill read frames_read_back.
type streamsTestResult struct {
	Connected      bool   `json:"connected"`
	HandshakeSent  bool   `json:"handshake_sent"`
	FramesSent     int    `json:"frames_sent"`
	Codec          string `json:"codec"`
	Rate           int    `json:"rate"`
	Bidirectional  bool   `json:"bidirectional"`
	FramesReadBack int    `json:"frames_read_back"`
	Errors         int    `json:"errors"` // kept for scripts: a failed send now ends the run, so always 0 here

	LatencyMs       int64    `json:"latency_ms"`        // WebSocket connect time
	Signed          bool     `json:"signed"`            // the upgrade carried Plivo's signature
	PlayAudioFrames int      `json:"play_audio_frames"` // playAudio frames carrying audio
	FirstResponseMs *int64   `json:"first_response_ms"` // first media frame sent to first playAudio back; null when none
	Warnings        []string `json:"warnings"`
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
	readBack := streamsTestBidirectional || streamsTestExpectAudio
	header, profile, err := streamsTestSignature(streamsTestTo)
	if err != nil {
		return err
	}
	if dryRunFlag {
		signed := "unsigned"
		if profile != "" {
			signed = "signed for profile " + profile
		}
		fmt.Fprintf(os.Stderr, "[dry-run] Would connect to %s (%s) and stream %ds of %s audio at %d Hz\n",
			streamsTestTo, signed, streamsTestDuration, streamsTestCodec, streamsTestRate)
		return nil
	}

	mediaFormat := wsproxy.MediaFormat{
		Encoding:   wsproxy.Encoding(streamsTestCodec),
		SampleRate: streamsTestRate,
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
	result := streamsTestResult{Codec: streamsTestCodec, Rate: streamsTestRate, Bidirectional: readBack, Signed: profile != "", Warnings: []string{}}
	warn := func(msg string) {
		result.Warnings = append(result.Warnings, msg)
		if !jsonOut {
			fmt.Fprintf(out, "⚠ %s\n", msg)
		}
	}

	// --- Phase 1: connect ---
	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	dialStart := time.Now()
	conn, resp, err := websocket.Dial(dialCtx, streamsTestTo, &websocket.DialOptions{HTTPClient: httpClient, HTTPHeader: header})
	latency := time.Since(dialStart)
	dialCancel()
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return streamsTestRefused(resp.StatusCode, profile)
		}
		return clierr.NetworkError(streamsTestTo, err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")
	result.Connected = true
	result.LatencyMs = latency.Milliseconds()
	if !jsonOut {
		signedAs := ""
		if profile != "" {
			signedAs = fmt.Sprintf(", signed with profile %q", profile)
		}
		fmt.Fprintf(out, "✓ Connection established (%dms%s)\n", result.LatencyMs, signedAs)
	}

	// --- Phase 2: handshake (start frame) ---
	streamID := uuid.NewString() // Plivo's stream and call ids are UUIDs
	startFrame, err := wsproxy.EncodeStart(streamID, uuid.NewString(), "test-account", mediaFormat)
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

	// The reader starts before the first media frame, so an answer is read,
	// and timed, as it arrives rather than after the whole stream. It runs in
	// every mode: it is also what sees the endpoint close the connection.
	sendStart := time.Now()
	rb := startStreamsReadBack(conn)
	for i, audio := range frames {
		if ctx.Err() != nil {
			break
		}
		// A write that runs out of time closes the connection, so the
		// bound only catches an endpoint that has stopped reading.
		mediaCtx, mediaCancel := context.WithTimeout(ctx, 2*time.Second)
		raw, _ := wsproxy.EncodeMedia(streamID, i+1, sendStart.UnixMilli()+int64(i*frameMs), audio)
		err := conn.Write(mediaCtx, websocket.MessageText, raw)
		mediaCancel()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			// The socket is gone after any failed write: no point retrying.
			_ = conn.CloseNow()
			rb.stop()
			if e := endpointClosed(rb, fmt.Sprintf("at frame %d of %d", i+1, totalFrames)); e != nil {
				return e
			}
			return clierr.NetworkError(streamsTestTo, fmt.Errorf("send frame %d: %w", i+1, err))
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
	if ctx.Err() != nil {
		warn(fmt.Sprintf("Interrupted after %d of %d frames", result.FramesSent, totalFrames))
	}

	// --- Phase 4: give the endpoint time to answer ---
	if readBack && ctx.Err() == nil {
		rb.waitForAnswer(ctx, time.Duration(streamsTestDuration)*time.Second)
	}

	// --- Phase 5: stop, close, then stop reading ---
	// In that order: coder/websocket drops the connection when a Read's
	// context ends, which used to cut the socket before stop went out.
	stop, _ := wsproxy.EncodeStop()
	stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), 1*time.Second)
	stopErr := conn.Write(stopCtx, websocket.MessageText, stop)
	stopCancel()
	if stopErr != nil {
		// If the endpoint's close failed the write, the reader may still be
		// taking that close in: give it a moment.
		select {
		case <-rb.done:
		case <-time.After(100 * time.Millisecond):
		}
	}
	// Reading ended before our close: the endpoint hung up first, which on a
	// real call drops the stream.
	closedEarly := ctx.Err() == nil && rb.ended()
	_ = conn.Close(websocket.StatusNormalClosure, "test complete")
	rb.stop()
	if closedEarly {
		if e := endpointClosed(rb, "before the stop frame"); e != nil {
			return e
		}
		return clierr.NetworkError(streamsTestTo, fmt.Errorf("%w before the stop frame", rb.err))
	}

	if readBack {
		result.FramesReadBack = rb.frames
		result.PlayAudioFrames = rb.playAudio
		if !rb.firstAudio.IsZero() {
			ms := rb.firstAudio.Sub(sendStart).Milliseconds()
			result.FirstResponseMs = &ms
		}
		if rb.frames == 0 {
			warn("No frames received back from endpoint: the bot→caller path may not be wired")
		} else if !jsonOut {
			fmt.Fprintf(out, "✓ Received %d frames back from endpoint (bot→caller path live)\n", rb.frames)
		}
		if rb.playAudio > 0 {
			if !jsonOut {
				fmt.Fprintf(out, "✓ First playAudio %dms after the first frame (%d playAudio frames)\n",
					*result.FirstResponseMs, rb.playAudio)
			}
		} else if rb.frames > 0 {
			warn(fmt.Sprintf("%d frames came back but none was playAudio carrying audio", rb.frames))
		}
	}
	if stopErr != nil {
		warn("Stop frame not sent: " + writeFailure(rb, stopErr).Error())
	}

	var outcome error
	if streamsTestExpectAudio && result.PlayAudioFrames == 0 {
		outcome = &clierr.Error{
			Code:    clierr.CodeValidation,
			Message: fmt.Sprintf("no playAudio came back from %s", streamsTestTo),
			Hint:    "The test audio is a 1 kHz tone, not speech: only an echo-style bot, or one that speaks first, answers it.",
		}
	}
	if jsonOut {
		if err := output.JSONSuccess(os.Stdout, result, nil); err != nil {
			return err
		}
		return outcome
	}
	if outcome != nil {
		return outcome
	}
	fmt.Fprintf(out, "\nEndpoint is ready to receive Plivo audio streams.\n")
	return nil
}

// streamsTestSignature signs the upgrade as Plivo signs a stream, over the
// http:// form of the URL without its query, so an endpoint that checks the
// signature lets the test in. Logged out, the test stays unsigned.
func streamsTestSignature(wsURL string) (http.Header, string, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, "", clierr.BadFlag("to", err.Error())
	}
	p, profile, err := config.Resolve(profileFlag)
	var ce *clierr.Error
	if errors.As(err, &ce) && ce.Code == clierr.CodeAuthMissing {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	nonce := randomHex(10)
	sig, err := plivosig.Compute(p.AuthToken, "http://"+u.Host+u.EscapedPath(), http.MethodGet, nonce, nil)
	if err != nil {
		return nil, "", err
	}
	return http.Header{plivosig.HeaderSignature: {sig}, plivosig.HeaderNonce: {nonce}}, profile, nil
}

// streamsTestRefused reports a 401 or 403 on the upgrade: the endpoint
// refused it, most likely over the signature, so a network hint misleads.
func streamsTestRefused(status int, profile string) *clierr.Error {
	hint := "No profile is logged in, so the upgrade was unsigned and an endpoint that checks Plivo's signature refuses it. Run `plivo login`, then test again."
	if profile != "" {
		hint = fmt.Sprintf("The upgrade was signed like Plivo's, with profile %q's auth token. If the endpoint checks another account's token, test again with --profile set to that account's profile.", profile)
	}
	return &clierr.Error{
		Code:       clierr.CodeAuthForbidden,
		Message:    streamsTestTo + " refused the WebSocket upgrade",
		Hint:       hint,
		StatusCode: status,
		Context:    map[string]any{"target": streamsTestTo, "signed": profile != ""},
	}
}

// readBackLimit replaces coder/websocket's 32 KiB cap on one message: a
// second of 16 kHz l16 audio is larger once base64-encoded, and a voice
// agent may send that much in one playAudio.
const readBackLimit = 1 << 20

// readBackQuiet ends the wait after the stream early: once audio has come
// back and the endpoint has sent nothing for this long, it has answered.
const readBackQuiet = time.Second

// streamsReadBack reads everything the endpoint sends during the test. The
// counters are guarded by mu while the reader runs; after stop they are
// final.
type streamsReadBack struct {
	cancel context.CancelFunc
	done   chan struct{}

	mu         sync.Mutex
	frames     int       // every message, as frames_read_back has always counted
	playAudio  int       // playAudio events carrying audio
	firstAudio time.Time // when the first of those arrived
	last       time.Time // when the latest message arrived
	err        error     // why reading ended
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
			typ, data, err := conn.Read(ctx)
			now := time.Now()
			rb.mu.Lock()
			if err != nil {
				rb.err = err
				rb.mu.Unlock()
				return
			}
			rb.frames++
			rb.last = now
			if typ == websocket.MessageText && isPlayAudio(data) {
				rb.playAudio++
				if rb.firstAudio.IsZero() {
					rb.firstAudio = now
				}
			}
			rb.mu.Unlock()
		}
	}()
	return rb
}

// waitForAnswer gives the endpoint up to window after the last media frame
// to answer. It returns early once audio has come back and the endpoint has
// been quiet for readBackQuiet, or when reading has ended.
func (rb *streamsReadBack) waitForAnswer(ctx context.Context, window time.Duration) {
	deadline := time.After(window)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		rb.mu.Lock()
		answered := rb.playAudio > 0 && time.Since(rb.last) >= readBackQuiet
		rb.mu.Unlock()
		if answered {
			return
		}
		select {
		case <-tick.C:
		case <-deadline:
			return
		case <-rb.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// stop ends the reader and waits for it to return.
func (rb *streamsReadBack) stop() {
	rb.cancel()
	<-rb.done
}

// ended reports whether reading has already stopped on its own.
func (rb *streamsReadBack) ended() bool {
	select {
	case <-rb.done:
		return true
	default:
		return false
	}
}

// isPlayAudio reports whether a message is a playAudio event that carries
// audio: the event a voice agent sends to speak to the caller.
func isPlayAudio(data []byte) bool {
	var ev struct {
		Event string `json:"event"`
		Media struct {
			Payload string `json:"payload"`
		} `json:"media"`
	}
	return json.Unmarshal(data, &ev) == nil && ev.Event == "playAudio" && ev.Media.Payload != ""
}

// closeDetail is the code and reason of the endpoint's close frame, when the
// reader saw one; stop rb first.
func closeDetail(rb *streamsReadBack) (string, bool) {
	var ce websocket.CloseError
	if !errors.As(rb.err, &ce) {
		return "", false
	}
	detail := fmt.Sprintf("code %d", int(ce.Code))
	if ce.Reason != "" {
		detail += fmt.Sprintf(", reason %q", ce.Reason)
	}
	return detail, true
}

// writeFailure explains a write the endpoint did not take: when it sent a
// close frame, its code and reason say why.
func writeFailure(rb *streamsReadBack, err error) error {
	if detail, ok := closeDetail(rb); ok {
		return fmt.Errorf("the endpoint closed the connection (%s)", detail)
	}
	return err
}

// endpointClosed is the error for an endpoint that hung up before the stop
// frame, nil when it sent no close frame. The endpoint was reached, so the
// error gives its close code and reason, not a connectivity hint.
func endpointClosed(rb *streamsReadBack, when string) *clierr.Error {
	detail, ok := closeDetail(rb)
	if !ok {
		return nil
	}
	return &clierr.Error{
		Code:      clierr.CodeNetworkError,
		Message:   fmt.Sprintf("%s closed the stream %s (%s)", streamsTestTo, when, detail),
		Hint:      "The endpoint accepted the stream, then hung up: on a real call that drops the audio. Its logs should say why.",
		Retryable: true,
		Context:   map[string]any{"target": streamsTestTo},
	}
}
