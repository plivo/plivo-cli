package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// ask + support talk to Plivo's customer-facing AI assistant via the
// /v1/aiassist/buddy-ext endpoint (Plivo Basic auth — same creds the
// rest of the CLI uses).

// ask flags
var (
	askCallUUID    string
	askVerbose     bool
	askDebug       bool
	askInteractive bool
)

const (
	// buddyCLIPageURL is a synthetic pageUrl (the server validator requires
	// http/https) that keeps the escalation idempotency key stable per CLI run.
	buddyCLIPageURL = "https://cli.plivo.com/cli"
	// maxHistoryTurns caps how many prior turns we replay per request; the
	// server enforces the same ceiling.
	maxHistoryTurns = 20
)

var askCmd = &cobra.Command{
	Use:   "ask [message]",
	Short: "Ask Plivo's AI assistant (streams the answer as it comes)",
	Long: `Send a message to Plivo's AI assistant and stream the response.

The assistant uses Server-Sent Events: token text streams to stdout as it
arrives, live status appears on stderr and is overwritten in place, and the
final summary lands on stdout. Long flows (voice-debug can run 2–5 minutes)
work fine — there is no overall HTTP timeout. Ctrl-C cancels and exits 130;
no auto-retry (debugger runs are not idempotent).

Use -i for an interactive chat that keeps context across follow-ups: each
turn replays the recent conversation as history (a one-shot ask sends none,
so the assistant can't see your previous questions). In -i, /reset starts a
fresh conversation, /help lists commands, and /exit or Ctrl-D leaves.

Pass --call-uuid for voice-debug context; --verbose to show the assistant's
tool calls; -o json to emit each SSE event as one JSONL line (handy for
scripts and AI agents).`,
	Example: `  plivo ask "What does Plivo SMS error code 30007 mean?"
  plivo ask -i
  plivo ask -i "Debug what happened on this call"
  plivo ask --call-uuid 21e68d29-... "Debug what happened on this call"
  plivo ask -o json "What's the rate for outbound voice to Brazil?"`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAsk,
}

var supportCmd = &cobra.Command{
	Use:     "support",
	Short:   "List your past support escalations (filed via `plivo ask`)",
	Example: "  plivo support\n  plivo support -o json",
	RunE:    runSupport,
}

func init() {
	askCmd.Flags().StringVar(&askCallUUID, "call-uuid", "",
		"voice-debug context: the call UUID the assistant should analyse")
	askCmd.Flags().BoolVar(&askVerbose, "verbose", false,
		"show the assistant's tool_call / tool_output events on stderr")
	askCmd.Flags().BoolVar(&askDebug, "debug-stream", false,
		"log every raw SSE frame to stderr (for debugging this CLI)")
	askCmd.Flags().BoolVarP(&askInteractive, "interactive", "i", false,
		"interactive chat — keep a conversation with follow-ups (history sent each turn)")

	rootCmd.AddCommand(askCmd, supportCmd)
}

// applyBuddyURL resolves the AI-assistant URL with precedence:
//
//	PLIVO_BUDDY_URL env  >  current `--env <X>` (login only)  >
//	[buddy].url config  >  built-in prod default
func applyBuddyURL(c *api.Client) {
	if u := os.Getenv("PLIVO_BUDDY_URL"); u != "" {
		c.BuddyBaseURL = u
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}
	if u := cfg.Buddy.EffectiveURL(); u != "" {
		c.BuddyBaseURL = u
	}
}

func runAsk(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	applyBuddyURL(client)
	url := client.BuddyURL("/v1/aiassist/buddy-ext/chat")

	if askInteractive {
		first := ""
		if len(args) == 1 {
			first = args[0]
		}
		return runInteractiveAsk(client, url, first)
	}

	if len(args) != 1 {
		return clierr.BadInput("ask needs a message — e.g. `plivo ask \"...\"`, or use -i for an interactive chat")
	}
	message := args[0]
	// `callUUID` is dropped server-side (extra: ignore), so the routing signal
	// comes from the message-text suffix.
	if askCallUUID != "" {
		message = fmt.Sprintf("%s (call_uuid: %s)", message, askCallUUID)
	}
	body := api.BuddyChatRequest{
		Message:     message,
		UserContext: buildBuddyUserContext(client),
		PageURL:     buddyCLIPageURL,
	}

	// --dry-run: print what would be sent, don't open the SSE stream.
	if dryRunFlag {
		pretty, _ := json.MarshalIndent(body, "  ", "  ")
		fmt.Fprintf(os.Stderr, "[dry-run] POST %s (SSE)\n  body:\n  %s\n", url, pretty)
		return nil
	}

	// SIGINT/SIGTERM → cancel the SSE context → http body close → exit 130.
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-streamCtx.Done():
		}
	}()

	r := newBuddyRenderer(effectiveFormat() == output.FormatJSON)
	sseErr := client.StreamSSE(streamCtx, "POST", url, body, func(ev api.SSEEvent) bool {
		if askDebug {
			fmt.Fprintf(os.Stderr, "[sse] event=%q data=%s\n", ev.Event, ev.Data)
		}
		return r.handle(ev)
	})
	// Reap the spinner on every exit path (cancel, error, or a stream that ends
	// without a final/error event). Idempotent — a normal final already stopped it.
	r.stopSpinner()

	// Ctrl-C: print whatever we've accumulated so the user sees a partial
	// answer, then exit 130.
	if streamCtx.Err() == context.Canceled {
		if !r.jsonMode {
			if r.streamed {
				fmt.Fprintln(os.Stdout) // close the streamed line
			} else if r.answerBuf.Len() > 0 {
				fmt.Fprintln(os.Stdout, strings.TrimRight(r.answerBuf.String(), "\n"))
			}
		}
		fmt.Fprintln(os.Stderr, "(cancelled)")
		os.Exit(130)
	}

	if sseErr != nil {
		// An HTTP error status from the server is not a connectivity problem —
		// classify by status; only genuine transport failures are network errors.
		var httpErr *api.SSEHTTPError
		if errors.As(sseErr, &httpErr) {
			return clierr.FromHTTP(httpErr.StatusCode, "", httpErr.Body)
		}
		return clierr.NetworkError("buddy", sseErr)
	}
	if r.errorSeen {
		// A server-emitted error event is a service-side error, not bad user input.
		return clierr.Upstream(r.errorMsg)
	}
	return nil
}

// buildBuddyUserContext fetches best-effort account context (balance). Failure
// is non-fatal — an empty userContext is valid. The server's BuddyUserContext
// is a strict model (plan is an enum; a bare account_type 400s), so we only
// send balance.
func buildBuddyUserContext(client *api.Client) api.BuddyUserContext {
	uctx := api.BuddyUserContext{}
	var acct api.Account
	if apiErr, gerr := client.Do("GET", client.AccountURL(), nil, nil, &acct); gerr == nil && apiErr == nil {
		uctx.Balance = acct.CashCredits
	}
	return uctx
}

// newBuddyRenderer wires a renderer to stdout/stderr with the current
// tty/color/verbose settings.
func newBuddyRenderer(jsonMode bool) *buddyRenderer {
	tty := term.IsTerminal(int(os.Stdout.Fd()))
	return &buddyRenderer{
		out:       os.Stdout,
		err:       os.Stderr,
		jsonMode:  jsonMode,
		useANSI:   tty && !noColorFlag,
		verbose:   askVerbose,
		startedAt: time.Now(),
	}
}

// buddySession holds an interactive conversation. It accumulates turns and
// replays the most recent ones as `history` so the assistant keeps context
// across follow-ups — which is what lets clarifying questions (the server's
// ask_user flow) work from the CLI. A one-shot `ask` sends no history.
type buddySession struct {
	client  *api.Client
	url     string
	uctx    api.BuddyUserContext
	history []api.BuddyTurn
}

// historyForRequest returns the most recent turns, capped at maxHistoryTurns.
func (s *buddySession) historyForRequest() []api.BuddyTurn {
	if len(s.history) <= maxHistoryTurns {
		return s.history
	}
	return s.history[len(s.history)-maxHistoryTurns:]
}

// record appends one turn to the running history.
func (s *buddySession) record(role, text string) {
	s.history = append(s.history, api.BuddyTurn{Role: role, Text: text})
}

// reset clears the conversation history.
func (s *buddySession) reset() { s.history = nil }

// sendTurn streams one assistant turn (the renderer prints as it goes) and
// returns the answer text plus ok. On error or Ctrl-C it prints the reason and
// returns ok=false so the REPL stays alive — Ctrl-C cancels only the in-flight
// turn, not the whole session.
func (s *buddySession) sendTurn(message string) (answer string, ok bool) {
	body := api.BuddyChatRequest{
		Message:     message,
		History:     s.historyForRequest(),
		UserContext: s.uctx,
		PageURL:     buddyCLIPageURL,
	}

	turnCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-turnCtx.Done():
		}
	}()

	r := newBuddyRenderer(false) // interactive mode is human-facing; no JSONL
	sseErr := s.client.StreamSSE(turnCtx, "POST", s.url, body, func(ev api.SSEEvent) bool {
		if askDebug {
			fmt.Fprintf(os.Stderr, "[sse] event=%q data=%s\n", ev.Event, ev.Data)
		}
		return r.handle(ev)
	})
	r.stopSpinner() // reap the spinner on every exit path (idempotent)

	switch {
	case turnCtx.Err() == context.Canceled:
		if r.streamed {
			fmt.Fprintln(os.Stdout) // tokens already streamed; just close the line
		} else if r.answerBuf.Len() > 0 {
			fmt.Fprintln(os.Stdout, strings.TrimRight(r.answerBuf.String(), "\n"))
		}
		fmt.Fprintln(os.Stderr, "(cancelled)")
		return "", false
	case sseErr != nil:
		var httpErr *api.SSEHTTPError
		if errors.As(sseErr, &httpErr) {
			fmt.Fprintf(os.Stderr, "buddy error: %s\n", clierr.FromHTTP(httpErr.StatusCode, "", httpErr.Body).Error())
		} else {
			fmt.Fprintf(os.Stderr, "buddy error: %s\n", clierr.NetworkError("buddy", sseErr).Error())
		}
		return "", false
	case r.errorSeen:
		// the renderer already printed "buddy error: ..." for the error event.
		return "", false
	default:
		return r.answerBuf.String(), true
	}
}

// replAction is the parsed intent of one line of REPL input.
type replAction int

const (
	replMessage replAction = iota
	replEmpty
	replExit
	replReset
	replHelp
	replUnknown
)

// classifyREPLInput maps a raw input line to an action. Lines starting with
// "/" are commands; everything else is a message to send.
func classifyREPLInput(line string) (replAction, string) {
	t := strings.TrimSpace(line)
	if t == "" {
		return replEmpty, ""
	}
	if !strings.HasPrefix(t, "/") {
		return replMessage, t
	}
	switch strings.ToLower(strings.Fields(t)[0]) {
	case "/exit", "/quit", "/q":
		return replExit, ""
	case "/reset", "/new":
		return replReset, ""
	case "/help", "/?":
		return replHelp, ""
	default:
		return replUnknown, t
	}
}

func printREPLHelp(w io.Writer) {
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  /reset, /new    start a fresh conversation (clears history)")
	fmt.Fprintln(w, "  /help, /?       show this help")
	fmt.Fprintln(w, "  /exit, /quit    leave (or press Ctrl-D)")
	fmt.Fprintln(w, "Anything else is sent to the assistant; follow-ups keep context.")
}

// runInteractiveAsk runs the `ask -i` read-eval loop: each message is sent with
// the running conversation history so the assistant keeps context. Ctrl-C
// cancels the in-flight turn; Ctrl-D or /exit leaves. An optional firstMsg
// seeds the first turn (`plivo ask -i "..."`).
func runInteractiveAsk(client *api.Client, url, firstMsg string) error {
	// Only refuse an EXPLICIT -o json — the non-TTY default (e.g. piped
	// through `tee`) shouldn't block a session that's still human-driven.
	if strings.EqualFold(outputFormat, "json") {
		return clierr.BadInput("interactive mode (-i) can't be combined with -o json")
	}
	if dryRunFlag {
		return clierr.BadInput("--dry-run isn't supported in interactive mode (-i)")
	}

	sess := &buddySession{client: client, url: url, uctx: buildBuddyUserContext(client)}

	fmt.Fprintln(os.Stderr, "Plivo AI assistant — interactive mode.")
	fmt.Fprintln(os.Stderr, "Type a message and press Enter. Commands: /reset, /help, /exit (or Ctrl-D).")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // tolerate long pastes

	pending := strings.TrimSpace(firstMsg)
	callUUIDApplied := false

	for {
		var line string
		if pending != "" {
			line, pending = pending, ""
			fmt.Fprintf(os.Stderr, "buddy> %s\n", line)
		} else {
			fmt.Fprint(os.Stderr, "buddy> ")
			if !scanner.Scan() {
				fmt.Fprintln(os.Stderr) // newline after ^D
				break
			}
			line = scanner.Text()
		}

		action, arg := classifyREPLInput(line)
		switch action {
		case replEmpty:
			continue
		case replExit:
			return nil
		case replReset:
			sess.reset()
			callUUIDApplied = false
			fmt.Fprintln(os.Stderr, "(conversation reset)")
			continue
		case replHelp:
			printREPLHelp(os.Stderr)
			continue
		case replUnknown:
			fmt.Fprintf(os.Stderr, "unknown command %q — try /help\n", strings.Fields(arg)[0])
			continue
		}

		// replMessage
		msg := arg
		if !callUUIDApplied && askCallUUID != "" {
			msg = fmt.Sprintf("%s (call_uuid: %s)", msg, askCallUUID)
			callUUIDApplied = true
		}
		answer, ok := sess.sendTurn(msg)
		if !ok {
			continue // error/cancel already reported; don't pollute history
		}
		sess.record("user", msg)
		sess.record("assistant", answer)
	}
	return nil
}

// buddyRenderer turns SSE events into terminal output: JSONL per event in json
// mode, otherwise streamed answer tokens (or a one-block final). answerBuf keeps
// the full text for the cancel / -i paths; out/err are injected for tests.
//
// On a TTY (useANSI) a background spinner goroutine animates a single stderr
// line so long, silent backend gaps (e.g. a 1–2 min voice-debug run) never look
// frozen. mu guards every stderr write — the spinner tick and all event-driven
// writes — so they never garble or data-race (go test -race clean).
type buddyRenderer struct {
	out          io.Writer
	err          io.Writer
	jsonMode     bool
	useANSI      bool
	verbose      bool
	startedAt    time.Time
	answerBuf    strings.Builder
	streamed     bool
	hadNarration bool
	errorSeen    bool
	errorMsg     string

	// mu serialises all writes to err (and the spinner state it reads).
	mu sync.Mutex
	// sp is the live spinner; nil when not running. Guarded by mu.
	sp *buddySpinner
}

func (r *buddyRenderer) handle(ev api.SSEEvent) bool {
	if r.jsonMode {
		// Pass the raw data through so consumers see the exact buddy-ext payload.
		raw := json.RawMessage(ev.Data)
		if !json.Valid(raw) {
			raw = json.RawMessage(`null`)
		}
		_ = json.NewEncoder(r.out).Encode(map[string]any{
			"event": ev.Event,
			"data":  raw,
		})
		return ev.Event != "final" && ev.Event != "error"
	}

	switch ev.Event {
	case "start":
		// Begin animating so silence before the first real event still moves.
		r.startSpinner()

	case "token":
		var d struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(buddyInner(ev.Data), &d)
		if d.Text == "" {
			return true
		}
		// First token: the answer is arriving, so stop the spinner (clears the
		// line) before streaming. Subsequent tokens: spinner already stopped.
		r.stopSpinner()
		// Clear any leftover narration line (non-spinner path) so it doesn't
		// interleave. answerBuf keeps the text; final won't re-print.
		r.clearNarrationLine()
		fmt.Fprint(r.out, d.Text)
		r.answerBuf.WriteString(d.Text)
		r.streamed = true

	case "message":
		var d struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(buddyInner(ev.Data), &d)
		r.stopSpinner()
		// no-stream / debugger-final shape; replaces the accumulated answer.
		r.answerBuf.Reset()
		r.answerBuf.WriteString(d.Text)

	case "narration":
		var d struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(buddyInner(ev.Data), &d)
		if r.useANSI {
			// Feed the live spinner instead of writing the line directly; the
			// tick redraws it (starting the spinner if "start" never arrived).
			r.setSpinnerText(d.Text)
		} else {
			fmt.Fprintln(r.err, "[buddy] "+d.Text)
		}

	case "tool_call":
		if !r.verbose {
			return true
		}
		var d struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(buddyInner(ev.Data), &d)
		// Clear the spinner/narration line under the lock so this print doesn't
		// garble; the spinner redraws itself on the next tick.
		r.withSpinnerCleared(func() {
			fmt.Fprintf(r.err, "🔧 calling %s\n", d.Name)
		})

	case "tool_output":
		if !r.verbose {
			return true
		}
		var d struct {
			Name    string `json:"name"`
			Success *bool  `json:"success"`
			Error   string `json:"error"`
		}
		_ = json.Unmarshal(buddyInner(ev.Data), &d)
		mark := "✓"
		if (d.Success != nil && !*d.Success) || d.Error != "" {
			mark = "✗"
		}
		r.withSpinnerCleared(func() {
			fmt.Fprintf(r.err, "  %s %s\n", mark, d.Name)
		})

	case "sources":
		var d struct {
			Sources []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
				URL   string `json:"url"`
			} `json:"sources"`
		}
		_ = json.Unmarshal(buddyInner(ev.Data), &d)
		if len(d.Sources) > 0 {
			// sources prints to stdout, but clear the stderr spinner line first
			// (under the lock) so the two streams don't visually collide.
			r.withSpinnerCleared(func() {
				fmt.Fprintln(r.out, "\n\nSources:")
				for i, s := range d.Sources {
					if s.URL != "" {
						fmt.Fprintf(r.out, "  %d. %s — %s\n", i+1, s.Title, s.URL)
					} else {
						fmt.Fprintf(r.out, "  %d. %s\n", i+1, s.Title)
					}
				}
			})
		}

	case "final":
		var d struct {
			Answer    string `json:"answer"`
			LatencyMs int    `json:"latency_ms"`
		}
		_ = json.Unmarshal(buddyInner(ev.Data), &d)
		r.stopSpinner()
		r.clearNarrationLine()
		if r.streamed {
			// Tokens already streamed live to stdout — just close the line.
			fmt.Fprintln(r.out)
		} else {
			// Nothing streamed (a `message`/debugger-final or non-streaming
			// answer) — print the assembled answer as one block; fall back to
			// `final.answer` if neither streamed nor buffered.
			answer := r.answerBuf.String()
			if answer == "" {
				answer = d.Answer
			}
			if answer != "" {
				fmt.Fprintln(r.out, strings.TrimRight(answer, "\n"))
			}
		}
		latency := time.Since(r.startedAt)
		if d.LatencyMs > 0 {
			latency = time.Duration(d.LatencyMs) * time.Millisecond
		}
		fmt.Fprintf(r.err, "(done in %.1fs)\n", latency.Seconds())
		return false

	case "error":
		var d struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(buddyInner(ev.Data), &d)
		r.stopSpinner()
		r.clearNarrationLine()
		fmt.Fprintf(r.err, "\nbuddy error: %s\n", d.Error)
		r.errorSeen = true
		r.errorMsg = d.Error
		return false
	}
	return true
}

// buddyInner unwraps Buddy's SSE envelope. Each SSE frame's data field
// carries a {"type":"<event>","data":{...}} object — the outer `type` is
// redundant with the `event:` SSE line, but that's the envelope shape
// the backend emits. We want the inner `data` payload for per-event
// unmarshalling. If the envelope is missing (defensive), fall back to the
// raw bytes so old/test frames with a flat shape still parse.
func buddyInner(raw string) []byte {
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &env) == nil && len(env.Data) > 0 {
		return env.Data
	}
	return []byte(raw)
}

// clearNarrationLine wipes a leftover live narration line on stderr if one is
// shown, so subsequent stdout writes don't appear interleaved with it. On a TTY
// the spinner owns the stderr line (it clears on stop), so hadNarration is only
// set on the non-spinner path; this stays as a defensive no-op for ANSI.
// Lock-guarded so it can't race the spinner goroutine's writes.
func (r *buddyRenderer) clearNarrationLine() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hadNarration && r.useANSI {
		fmt.Fprint(r.err, "\r\033[2K")
		r.hadNarration = false
	}
}

// --- working spinner ---------------------------------------------------------
//
// On a TTY, ask animates a single stderr line during backend silence so a long,
// quiet run (e.g. voice-debug) never looks frozen. A goroutine ticks every
// spinnerInterval and redraws `<frame> <text> <elapsed>`; the renderer's mu
// guards every stderr write (tick + event-driven) so they never garble.

const (
	// spinnerInterval is how often the frame/line is redrawn.
	spinnerInterval = 120 * time.Millisecond
	// spinnerWordInterval is how long each fallback word shows before the next.
	spinnerWordInterval = 1500 * time.Millisecond
)

// spinnerFrames cycles on the LEFT of the line (braille dots).
var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// spinnerWords are shown (one at a time, advancing ~every spinnerWordInterval)
// until a real backend narration arrives. The order is shuffled once per run.
var spinnerWords = []string{
	"Thinking", "Checking", "Working", "Digging in", "Crunching",
	"Looking into it", "Untangling", "Connecting the dots", "Pulling threads",
	"Scanning", "Cross-referencing", "Tracing it", "Piecing it together",
	"Consulting the docs", "Reasoning it out", "Investigating", "Parsing",
	"Almost there", "Hold tight", "On it",
}

// buddySpinner holds the live animation state. All mutable fields are read and
// written only while the owning buddyRenderer's mu is held.
type buddySpinner struct {
	startedAt time.Time
	words     []string // shuffled copy of spinnerWords, each with "…" appended
	frame     int      // index into spinnerFrames, advances every tick
	text      string   // latest backend narration; "" → show a fallback word
	stop      chan struct{}
	done      chan struct{}
}

// startSpinner begins the animation (TTY only). No-op off-TTY, in json mode, or
// if a spinner is already running.
func (r *buddyRenderer) startSpinner() {
	if !r.useANSI || r.jsonMode {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startSpinnerLocked()
}

// startSpinnerLocked starts the spinner; caller must hold mu. No-op if running.
func (r *buddyRenderer) startSpinnerLocked() {
	if r.sp != nil {
		return
	}
	words := make([]string, len(spinnerWords))
	for i, w := range spinnerWords {
		words[i] = w + "…"
	}
	rand.Shuffle(len(words), func(i, j int) { words[i], words[j] = words[j], words[i] })

	sp := &buddySpinner{
		startedAt: time.Now(),
		words:     words,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	r.sp = sp
	go r.runSpinner(sp)
}

// runSpinner drives the animation until sp.stop is closed. It locks mu only to
// render a frame, never while blocked on the ticker, so stopSpinner can take mu
// freely.
func (r *buddyRenderer) runSpinner(sp *buddySpinner) {
	defer close(sp.done)
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-sp.stop:
			return
		case <-ticker.C:
			r.mu.Lock()
			// Guard against a stop that raced in between ticks.
			if r.sp == sp {
				r.drawSpinnerLocked(sp)
			}
			r.mu.Unlock()
		}
	}
}

// drawSpinnerLocked renders one frame: `<frame> <text> <elapsed>`. Caller holds
// mu. <text> is the backend narration if set, else the current fallback word.
func (r *buddyRenderer) drawSpinnerLocked(sp *buddySpinner) {
	frame := spinnerFrames[sp.frame%len(spinnerFrames)]
	sp.frame++

	elapsed := time.Since(sp.startedAt)
	text := sp.text
	if text == "" && len(sp.words) > 0 {
		idx := int(elapsed/spinnerWordInterval) % len(sp.words)
		text = sp.words[idx]
	}

	secs := int(elapsed.Seconds())
	clock := fmt.Sprintf("%d:%02d", secs/60, secs%60)
	if r.useANSI {
		// dim the elapsed time so it stays subtle.
		clock = "\033[2m" + clock + "\033[0m"
	}
	fmt.Fprintf(r.err, "\r\033[2K%c %s %s", frame, text, clock)
}

// setSpinnerText sets the line's text to a real backend narration; it takes
// precedence over the fallback words the instant it arrives. Starts the spinner
// if one isn't already running (narration can precede the `start` event).
func (r *buddyRenderer) setSpinnerText(text string) {
	if !r.useANSI || r.jsonMode {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sp == nil {
		r.startSpinnerLocked()
	}
	r.sp.text = text
}

// stopSpinner halts the animation and clears the line. Idempotent and safe to
// call when no spinner is running; never deadlocks or leaks the goroutine.
func (r *buddyRenderer) stopSpinner() {
	r.mu.Lock()
	sp := r.sp
	r.sp = nil
	r.mu.Unlock()
	if sp == nil {
		return
	}
	// Signal stop and wait for the goroutine to exit before clearing, so no
	// late frame can re-dirty the line. We don't hold mu while waiting — the
	// goroutine may need it to finish an in-flight render.
	close(sp.stop)
	<-sp.done
	r.mu.Lock()
	fmt.Fprint(r.err, "\r\033[2K")
	r.mu.Unlock()
}

// withSpinnerCleared runs fn with the spinner line cleared and the mutex held,
// so an out-of-band print (tool_call/tool_output/sources) doesn't garble the
// animated line. The spinner stays running and redraws on its next tick.
func (r *buddyRenderer) withSpinnerCleared(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sp != nil {
		fmt.Fprint(r.err, "\r\033[2K")
	}
	fn()
}

// supportClientForTest is a package-level test hook, mirroring
// apiClientForTest in cmd/api.go.
var supportClientForTest *api.Client

func runSupport(cmd *cobra.Command, args []string) error {
	client := supportClientForTest
	if client == nil {
		c, _, err := getClient()
		if err != nil {
			return err
		}
		client = c
	}
	// "Your past escalations" needs a human identity to scope by — only a
	// browser `plivo login` populates one. PLIVO_AUTH_ID/TOKEN env auth (and
	// older manually-entered profiles) can't be attributed to a person.
	if client.AomUUID == "" {
		return &clierr.Error{
			Code:    clierr.CodeAuthForbidden,
			Message: "support needs a browser-login profile to scope escalations to you",
			Hint:    "Run `plivo login` — env var or manually-entered credentials have no per-user identity to filter by.",
		}
	}
	applyBuddyURL(client)

	url := client.BuddyURL("/v1/aiassist/buddy-ext/escalations")
	// Real response shape (from `data_adapters/buddy_escalation.py`):
	//   { "api_id": "...", "status": "ok", "data": { "escalations": [ ... ] } }
	// Decode the wrapper, then surface the inner slice to the user.
	var resp api.BuddyEscalationsResponse
	apiErr, err := client.Do("GET", url, nil, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	items := resp.Data.Escalations
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, items, nil)
	}
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "no escalations.")
		return nil
	}
	rows := [][]string{{"UUID", "SUMMARY", "STATUS", "CREATED", "SUPPORT TICKET"}}
	for _, e := range items {
		rows = append(rows, []string{e.UUID, e.EscalationSummary, e.Status, e.CreatedAt, e.PylonTicketID})
	}
	return output.Table(os.Stdout, rows)
}
