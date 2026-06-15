package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
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

	// Ctrl-C: print whatever we've accumulated so the user sees a partial
	// answer, then exit 130.
	if streamCtx.Err() == context.Canceled {
		if !r.jsonMode && r.answerBuf.Len() > 0 {
			fmt.Fprintln(os.Stdout, strings.TrimRight(r.answerBuf.String(), "\n"))
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

	switch {
	case turnCtx.Err() == context.Canceled:
		if r.answerBuf.Len() > 0 {
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
	if effectiveFormat() == output.FormatJSON {
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

// buddyRenderer turns SSE events into terminal output. In json mode it emits
// one JSONL line per event; otherwise it buffers tokens/message into a single
// answer block printed once on `final` (more reliable against prompt themes
// that re-render and clobber per-token streaming), while narration still goes
// to stderr live (overwritten in place when ANSI is available) so long flows
// show progress. `out` and `err` are injected (defaults: os.Stdout /
// os.Stderr) so tests can capture output.
type buddyRenderer struct {
	out          io.Writer
	err          io.Writer
	jsonMode     bool
	useANSI      bool
	verbose      bool
	startedAt    time.Time
	answerBuf    strings.Builder
	hadNarration bool
	errorSeen    bool
	errorMsg     string
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
		// nothing visible; the first token will print itself

	case "token":
		var d struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(buddyInner(ev.Data), &d)
		// Buffer tokens; we print the assembled answer in one block on
		// `final`. Streaming per-token interacts badly with prompt themes
		// that re-render after a command exits (Starship, P10k), which can
		// clobber the streamed bytes visually. One clean block at the end
		// is always visible.
		r.answerBuf.WriteString(d.Text)

	case "message":
		var d struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(buddyInner(ev.Data), &d)
		// `message` is the no-stream / debugger-final shape; it REPLACES
		// any accumulated answer rather than appending.
		r.answerBuf.Reset()
		r.answerBuf.WriteString(d.Text)

	case "narration":
		var d struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(buddyInner(ev.Data), &d)
		if r.useANSI {
			fmt.Fprintf(r.err, "\r\033[2K%s", d.Text)
			r.hadNarration = true
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
		r.clearNarrationLine()
		fmt.Fprintf(r.err, "🔧 calling %s\n", d.Name)

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
		fmt.Fprintf(r.err, "  %s %s\n", mark, d.Name)

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
			fmt.Fprintln(r.out, "\n\nSources:")
			for i, s := range d.Sources {
				if s.URL != "" {
					fmt.Fprintf(r.out, "  %d. %s — %s\n", i+1, s.Title, s.URL)
				} else {
					fmt.Fprintf(r.out, "  %d. %s\n", i+1, s.Title)
				}
			}
		}

	case "final":
		var d struct {
			Answer    string `json:"answer"`
			LatencyMs int    `json:"latency_ms"`
		}
		_ = json.Unmarshal(buddyInner(ev.Data), &d)
		r.clearNarrationLine()
		// Print the assembled answer in one block. Prefer the buffered
		// tokens/message; fall back to `final.answer` if neither streamed.
		answer := r.answerBuf.String()
		if answer == "" {
			answer = d.Answer
		}
		if answer != "" {
			fmt.Fprintln(r.out, strings.TrimRight(answer, "\n"))
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

// clearNarrationLine wipes the live narration line on stderr if one is currently
// shown, so subsequent stdout writes don't appear interleaved with it.
func (r *buddyRenderer) clearNarrationLine() {
	if r.hadNarration && r.useANSI {
		fmt.Fprint(r.err, "\r\033[2K")
		r.hadNarration = false
	}
}

func runSupport(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
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
