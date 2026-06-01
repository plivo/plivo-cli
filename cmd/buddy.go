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

// buddyCmd is the `plivo buddy …` service: a customer-facing AI assistant
// hosted at hodor's /v1/aiassist/buddy-ext (Plivo Basic auth — exactly the
// creds the rest of the CLI already uses).
var buddyCmd = &cobra.Command{
	Use:   "buddy",
	Short: "Plivo Buddy — AI assistant for docs, pricing, and debugging",
	Long: `plivo buddy talks to Plivo's customer-facing AI assistant from your terminal.
Ask docs / pricing questions, get a call debugged, or list past escalations —
all signed in with the same auth_id / auth_token the rest of the CLI uses.`,
}

// chat flags
var (
	buddyChatCallUUID string
	buddyChatVerbose  bool
	buddyChatDebug    bool
	buddyURLOverride  string // --buddy-url; overrides env + config + default
)

var buddyChatCmd = &cobra.Command{
	Use:   "chat <message>",
	Short: "Ask Buddy a question (streams the answer as it comes)",
	Long: `Send a single message to Plivo Buddy and stream the response.

Buddy uses Server-Sent Events: token text streams to stdout as it arrives,
live "narration" status appears on stderr and is overwritten in place, and
the final summary lands on stdout. Long flows (voice-debug can run 2–5
minutes) work fine — there is no overall HTTP timeout. Ctrl-C cancels and
exits 130; no auto-retry (debugger runs are not idempotent).

Pass --call-uuid for voice-debug context; --verbose to show Buddy's tool
calls; -o json to emit each SSE event as one JSONL line (handy for scripts
and AI agents).`,
	Example: `  plivo buddy chat "What does Plivo SMS error code 30007 mean?"
  plivo buddy chat --call-uuid 21e68d29-... "Debug what happened on this call"
  plivo buddy chat -o json "What's the rate for outbound voice to Brazil?"`,
	Args: cobra.ExactArgs(1),
	RunE: runBuddyChat,
}

var buddyEscalationsCmd = &cobra.Command{
	Use:     "escalations",
	Short:   "List your past Buddy escalations",
	Example: "  plivo buddy escalations",
	RunE:    runBuddyEscalations,
}

func init() {
	buddyCmd.PersistentFlags().StringVar(&buddyURLOverride, "buddy-url", "",
		"override the Buddy hodor URL (also via PLIVO_BUDDY_URL env, or [buddy].hodor_url in config)")

	buddyChatCmd.Flags().StringVar(&buddyChatCallUUID, "call-uuid", "",
		"voice-debug context: the call UUID Buddy should analyse")
	buddyChatCmd.Flags().BoolVar(&buddyChatVerbose, "verbose", false,
		"show Buddy's tool_call / tool_output events on stderr")
	buddyChatCmd.Flags().BoolVar(&buddyChatDebug, "debug-stream", false,
		"log every raw SSE frame to stderr (for debugging this CLI)")

	buddyCmd.AddCommand(buddyChatCmd, buddyEscalationsCmd)
	rootCmd.AddCommand(buddyCmd)
}

// applyBuddyURL applies the override precedence to c.BuddyBaseURL:
//
//	--buddy-url  >  PLIVO_BUDDY_URL  >  [buddy].hodor_url  >  built-in prod default
//
// (Built-in default is already set by api.New, so it's the no-op fallthrough.)
func applyBuddyURL(c *api.Client) {
	if buddyURLOverride != "" {
		c.BuddyBaseURL = buddyURLOverride
		return
	}
	if u := os.Getenv("PLIVO_BUDDY_URL"); u != "" {
		c.BuddyBaseURL = u
		return
	}
	if cfg, err := config.Load(); err == nil && cfg.Buddy.HodorURL != "" {
		c.BuddyBaseURL = cfg.Buddy.HodorURL
	}
}

func runBuddyChat(cmd *cobra.Command, args []string) error {
	message := args[0]

	client, _, err := getClient()
	if err != nil {
		return err
	}
	applyBuddyURL(client)

	// Build userContext. Email isn't on the public account payload; populate
	// plan/balance/timezone best-effort so Buddy can personalise. Failure here
	// is non-fatal — the chat works with an empty userContext.
	uctx := api.BuddyUserContext{}
	if buddyChatCallUUID != "" {
		uctx.CallUUID = buddyChatCallUUID
		message = fmt.Sprintf("%s (call_uuid: %s)", message, buddyChatCallUUID)
	}
	var acct api.Account
	if apiErr, gerr := client.Do("GET", client.AccountURL(), nil, nil, &acct); gerr == nil && apiErr == nil {
		uctx.Plan = acct.AccountType
		uctx.Balance = acct.CashCredits
	}

	body := api.BuddyChatRequest{
		Message:     message,
		UserContext: uctx,
		PageURL:     "cli://plivo-buddy",
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
		verbose:   buddyChatVerbose,
		startedAt: time.Now(),
	}

	sseErr := client.StreamSSE(streamCtx, "POST", url, body, func(ev api.SSEEvent) bool {
		if buddyChatDebug {
			fmt.Fprintf(os.Stderr, "[sse] event=%q data=%s\n", ev.Event, ev.Data)
		}
		return r.handle(ev)
	})

	// Ctrl-C: exit 130 directly (skip handleError which would print as a generic error).
	if streamCtx.Err() == context.Canceled {
		fmt.Fprintln(os.Stderr) // newline after any in-flight token
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
// one JSONL line per event; otherwise it streams tokens to stdout and keeps
// narration on stderr (overwritten in place when ANSI is available). `out`
// and `err` are injected (defaults: os.Stdout / os.Stderr) so tests can
// capture output.
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
		_ = json.Unmarshal([]byte(ev.Data), &d)
		r.clearNarrationLine()
		fmt.Fprint(r.out, d.Text)
		r.answerBuf.WriteString(d.Text)

	case "message":
		var d struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal([]byte(ev.Data), &d)
		r.clearNarrationLine()
		fmt.Fprint(r.out, d.Text)
		r.answerBuf.Reset()
		r.answerBuf.WriteString(d.Text)

	case "narration":
		var d struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal([]byte(ev.Data), &d)
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
		_ = json.Unmarshal([]byte(ev.Data), &d)
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
		_ = json.Unmarshal([]byte(ev.Data), &d)
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
		_ = json.Unmarshal([]byte(ev.Data), &d)
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
		_ = json.Unmarshal([]byte(ev.Data), &d)
		// Fall back to final.answer if no tokens/message were streamed.
		if r.answerBuf.Len() == 0 && d.Answer != "" {
			r.clearNarrationLine()
			fmt.Fprint(r.out, d.Answer)
		}
		latency := time.Since(r.startedAt)
		if d.LatencyMs > 0 {
			latency = time.Duration(d.LatencyMs) * time.Millisecond
		}
		fmt.Fprintf(r.err, "\n(done in %.1fs)\n", latency.Seconds())
		return false

	case "error":
		var d struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal([]byte(ev.Data), &d)
		r.clearNarrationLine()
		fmt.Fprintf(r.err, "\nbuddy error: %s\n", d.Error)
		r.errorSeen = true
		return false
	}
	return true
}

// clearNarrationLine wipes the live narration line on stderr if one is currently
// shown, so subsequent stdout writes don't appear interleaved with it.
func (r *buddyRenderer) clearNarrationLine() {
	if r.hadNarration && r.useANSI {
		fmt.Fprint(r.err, "\r\033[2K")
		r.hadNarration = false
	}
}

func runBuddyEscalations(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	applyBuddyURL(client)

	url := client.BuddyURL("/v1/aiassist/buddy-ext/escalations")
	var resp []api.BuddyEscalation
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
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, resp, nil)
	}
	if len(resp) == 0 {
		fmt.Fprintln(os.Stderr, "no escalations.")
		return nil
	}
	rows := [][]string{{"ID", "SUBJECT", "STATUS", "CREATED"}}
	for _, e := range resp {
		rows = append(rows, []string{e.ID, e.Subject, e.Status, e.CreatedAt})
	}
	return output.Table(os.Stdout, rows)
}
