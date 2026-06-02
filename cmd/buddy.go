package cmd

import (
	"context"
	"encoding/json"
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

// ask + support talk to Plivo's customer-facing AI assistant (internally
// "Buddy") hosted at hodor's /v1/aiassist/buddy-ext (Plivo Basic auth — same
// creds the rest of the CLI uses).

// ask flags
var (
	askCallUUID      string
	askVerbose       bool
	askDebug         bool
	buddyURLOverride string // --buddy-url; overrides env + config + default
)

var askCmd = &cobra.Command{
	Use:   "ask <message>",
	Short: "Ask Plivo's AI assistant (streams the answer as it comes)",
	Long: `Send a single message to Plivo's AI assistant and stream the response.

The assistant uses Server-Sent Events: token text streams to stdout as it
arrives, live status appears on stderr and is overwritten in place, and the
final summary lands on stdout. Long flows (voice-debug can run 2–5 minutes)
work fine — there is no overall HTTP timeout. Ctrl-C cancels and exits 130;
no auto-retry (debugger runs are not idempotent).

Pass --call-uuid for voice-debug context; --verbose to show the assistant's
tool calls; -o json to emit each SSE event as one JSONL line (handy for
scripts and AI agents).`,
	Example: `  plivo ask "What does Plivo SMS error code 30007 mean?"
  plivo ask --call-uuid 21e68d29-... "Debug what happened on this call"
  plivo ask -o json "What's the rate for outbound voice to Brazil?"`,
	Args: cobra.ExactArgs(1),
	RunE: runAsk,
}

var supportCmd = &cobra.Command{
	Use:     "support",
	Short:   "List your past support escalations (filed via `plivo ask`)",
	Example: "  plivo support\n  plivo support -o json",
	RunE:    runSupport,
}

func init() {
	// --buddy-url is operator territory (dev/staging override); attach to both
	// user-facing commands so it works on either invocation.
	for _, c := range []*cobra.Command{askCmd, supportCmd} {
		c.Flags().StringVar(&buddyURLOverride, "buddy-url", "",
			"override the assistant's hodor URL (also via PLIVO_BUDDY_URL env, or [buddy].hodor_url in config)")
	}

	askCmd.Flags().StringVar(&askCallUUID, "call-uuid", "",
		"voice-debug context: the call UUID the assistant should analyse")
	askCmd.Flags().BoolVar(&askVerbose, "verbose", false,
		"show the assistant's tool_call / tool_output events on stderr")
	askCmd.Flags().BoolVar(&askDebug, "debug-stream", false,
		"log every raw SSE frame to stderr (for debugging this CLI)")

	rootCmd.AddCommand(askCmd, supportCmd)
}

// applyBuddyURL applies the override precedence to c.BuddyBaseURL:
//
//	--buddy-url  >  PLIVO_BUDDY_URL  >  [buddy].hodor_url  >  built-in prod default
//
// (Built-in default is already set by api.New, so it's the no-op fallthrough.)
// applyBuddyURL resolves the hodor edge URL with precedence:
//
//	--buddy-url flag  >  PLIVO_BUDDY_URL env  >  active profile's Env  >
//	[buddy].hodor_url config  >  built-in prod default (already on c.BuddyBaseURL)
//
// The "active profile's Env" step lets `plivo login --env dev` once and
// have every subsequent command (ask, support, login --browser …) hit
// the right edge without further flags. Only recognised envs apply —
// unknown profile env values fall through.
func applyBuddyURL(c *api.Client) {
	if buddyURLOverride != "" {
		c.BuddyBaseURL = buddyURLOverride
		return
	}
	if u := os.Getenv("PLIVO_BUDDY_URL"); u != "" {
		c.BuddyBaseURL = u
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}
	if cfg.Active != "" {
		if prof, ok := cfg.Profiles[cfg.Active]; ok && prof.Env != "" {
			if u, ok := resolveLoginEnv(prof.Env); ok {
				c.BuddyBaseURL = u
				return
			}
		}
	}
	if cfg.Buddy.HodorURL != "" {
		c.BuddyBaseURL = cfg.Buddy.HodorURL
	}
}

func runAsk(cmd *cobra.Command, args []string) error {
	message := args[0]

	client, _, err := getClient()
	if err != nil {
		return err
	}
	applyBuddyURL(client)

	// Build userContext. The server's BuddyUserContext is a strict Pydantic
	// model — `plan` is an enum (free_trial/professional/enterprise) and a
	// bare account_type like "standard" 400s the request, so we only send
	// balance (best-effort; failure is non-fatal — empty userContext works).
	// `callUUID` is silently dropped server-side (extra: ignore), so the
	// routing signal comes solely from the message-text suffix below.
	uctx := api.BuddyUserContext{}
	if askCallUUID != "" {
		message = fmt.Sprintf("%s (call_uuid: %s)", message, askCallUUID)
	}
	var acct api.Account
	if apiErr, gerr := client.Do("GET", client.AccountURL(), nil, nil, &acct); gerr == nil && apiErr == nil {
		uctx.Balance = acct.CashCredits
	}

	body := api.BuddyChatRequest{
		Message:     message,
		UserContext: uctx,
		// Server validator requires http/https on pageUrl. A synthetic CLI URL
		// keeps the escalation idempotency key stable per CLI session without
		// pretending we're a real Console page.
		PageURL: "https://cli.plivo.com/cli",
	}

	url := client.BuddyURL("/v1/aiassist/buddy-ext/chat")

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

	jsonMode := effectiveFormat() == output.FormatJSON
	tty := term.IsTerminal(int(os.Stdout.Fd()))
	r := &buddyRenderer{
		out:       os.Stdout,
		err:       os.Stderr,
		jsonMode:  jsonMode,
		useANSI:   tty && !noColorFlag,
		verbose:   askVerbose,
		startedAt: time.Now(),
	}

	sseErr := client.StreamSSE(streamCtx, "POST", url, body, func(ev api.SSEEvent) bool {
		if askDebug {
			fmt.Fprintf(os.Stderr, "[sse] event=%q data=%s\n", ev.Event, ev.Data)
		}
		return r.handle(ev)
	})

	// Ctrl-C: print whatever we've accumulated so the user sees a partial
	// answer, then exit 130 (skip handleError which would print as a generic
	// error).
	if streamCtx.Err() == context.Canceled {
		if !r.jsonMode && r.answerBuf.Len() > 0 {
			fmt.Fprintln(os.Stdout, strings.TrimRight(r.answerBuf.String(), "\n"))
		}
		fmt.Fprintln(os.Stderr, "(cancelled)")
		os.Exit(130)
	}

	if sseErr != nil {
		return clierr.NetworkError("buddy", sseErr)
	}
	if r.errorSeen {
		return clierr.BadInput("buddy returned an error event")
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
		return false
	}
	return true
}

// buddyInner unwraps Buddy's SSE envelope. Each SSE frame's data field
// carries a {"type":"<event>","data":{...}} object — the outer `type` is
// redundant with the `event:` SSE line, but it's what aiassist emits via
// `format_sse_event`. We want the inner `data` payload for per-event
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
	rows := [][]string{{"UUID", "SUMMARY", "STATUS", "CREATED", "PYLON TICKET"}}
	for _, e := range items {
		rows = append(rows, []string{e.UUID, e.EscalationSummary, e.Status, e.CreatedAt, e.PylonTicketID})
	}
	return output.Table(os.Stdout, rows)
}
