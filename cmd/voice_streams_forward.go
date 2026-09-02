package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/plivo/plivo-cli/internal/tunnel"
	"github.com/plivo/plivo-cli/internal/wsproxy"

	"github.com/coder/websocket"
	"github.com/spf13/cobra"
)

var (
	streamsFwdNumber        string
	streamsFwdAppID         string
	streamsFwdTo            string
	streamsFwdYes           bool
	streamsFwdKeep          bool
	streamsFwdCodec         string
	streamsFwdRate          int
	streamsFwdBidirectional bool
	streamsFwdPrintPayload  bool
	streamsFwdTunnel        string

	// streamsFwdClientForTest is a package-level test hook, mirroring
	// apiClientForTest in cmd/api.go — lets tests inject a client pointed at
	// an httptest server instead of going through getClient().
	streamsFwdClientForTest *api.Client
)

var voiceStreamsForwardCmd = &cobra.Command{
	Use:   "forward",
	Short: "Redirect an app's answer_url to a local tunnel so calls stream into your local handler",
	Long: `One-command local-dev experience for voice streaming.

Saves the app's current answer_url, starts a tunnel + a local
HTTP/WebSocket server, points the app at the tunnel, and forwards
incoming call audio to your local WebSocket handler. Restores the
original answer_url on Ctrl+C.

No setup required: defaults to localhost.run over ssh, which needs no
install and no account. Uses ngrok instead when it is already on PATH
(or at ~/.plivo/bin/ngrok). Force either with --tunnel.

Nothing is purchased, created, or deleted — the only mutation is one
field on one app, restored on exit.`,
	Example: `  plivo voice streams forward \
    --number +14155550142 \
    --app abc-uuid-def-456 \
    --to ws://localhost:7860/ws

  # Don't restore answer_url on exit (advanced):
  plivo voice streams forward --number +14155550142 --app abc --to ws://localhost:7860/ws --keep`,
	RunE: runVoiceStreamsForward,
}

// streamsFwdResult is the -o json summary for `plivo voice streams
// forward` — one final object instead of the run's progress narration.
type streamsFwdResult struct {
	AppID          string `json:"app_id"`
	TunnelURL      string `json:"tunnel_url"`
	Restored       bool   `json:"restored"`
	RestoreError   string `json:"restore_error,omitempty"`
	EventsObserved int64  `json:"events_observed"`
}

func init() {
	voiceStreamsForwardCmd.Flags().StringVar(&streamsFwdNumber, "number", "", "E.164 number attached to --app (required)")
	voiceStreamsForwardCmd.Flags().StringVar(&streamsFwdAppID, "app", "", "Plivo Application UUID whose answer_url will be temporarily redirected (required)")
	voiceStreamsForwardCmd.Flags().StringVar(&streamsFwdTo, "to", "", "local WebSocket URL to forward call audio to, e.g. ws://localhost:7860/ws (required)")
	voiceStreamsForwardCmd.Flags().BoolVarP(&streamsFwdYes, "yes", "y", false, "skip the confirmation prompt")
	voiceStreamsForwardCmd.Flags().BoolVar(&streamsFwdKeep, "keep", false, "do NOT restore the original answer_url on exit (advanced)")
	voiceStreamsForwardCmd.Flags().StringVar(&streamsFwdCodec, "codec", "mulaw", "audio codec advertised to Plivo: mulaw | l16")
	voiceStreamsForwardCmd.Flags().IntVar(&streamsFwdRate, "rate", 8000, "sample rate in Hz (mulaw: 8000; l16: 8000 or 16000)")
	voiceStreamsForwardCmd.Flags().BoolVar(&streamsFwdBidirectional, "bidirectional", true, "allow bot to send audio back to the caller")
	voiceStreamsForwardCmd.Flags().BoolVar(&streamsFwdPrintPayload, "print-payload", false, "dump full webhook bodies to terminal (verbose)")
	voiceStreamsForwardCmd.Flags().StringVar(&streamsFwdTunnel, "tunnel", "auto", "tunnel provider: auto | ngrok | localhost.run")
	_ = voiceStreamsForwardCmd.MarkFlagRequired("number")
	_ = voiceStreamsForwardCmd.MarkFlagRequired("app")
	_ = voiceStreamsForwardCmd.MarkFlagRequired("to")

	voiceStreamsCmd.AddCommand(voiceStreamsForwardCmd)
}

func runVoiceStreamsForward(cmd *cobra.Command, _ []string) error {
	if !strings.HasPrefix(streamsFwdTo, "ws://") && !strings.HasPrefix(streamsFwdTo, "wss://") {
		return clierr.BadFlag("to", "must be a WebSocket URL (ws:// or wss://)")
	}
	if err := wsproxy.ValidateCodecRate(streamsFwdCodec, streamsFwdRate); err != nil {
		return clierr.BadFlag("codec", err.Error())
	}

	out := cmd.OutOrStdout()
	jsonOut := effectiveFormat() == output.FormatJSON
	client := streamsFwdClientForTest
	if client == nil {
		c, _, err := getClient()
		if err != nil {
			return err
		}
		client = c
	}

	// --- Read current app + save the answer_url ---
	// Always a real read, even under --dry-run: client.DryRun would otherwise
	// short-circuit this GET too, leaving the preview below blank.
	wasDryRun := client.DryRun
	client.DryRun = false
	var app api.Application
	apiErr, err := client.Do("GET", client.AccountURL("Application", streamsFwdAppID), nil, nil, &app)
	client.DryRun = wasDryRun
	if err != nil {
		return clierr.NetworkError(client.AccountURL("Application", streamsFwdAppID), err)
	}
	if apiErr != nil {
		return apiErr
	}
	originalAnswerURL := app.AnswerURL

	if dryRunFlag {
		if jsonOut {
			return output.JSONSuccess(os.Stdout, map[string]any{
				"dry_run":         true,
				"app_id":          streamsFwdAppID,
				"from_answer_url": originalAnswerURL,
				"to":              streamsFwdTo,
			}, nil)
		}
		fmt.Fprintf(out, "[dry-run] Would redirect app %q (%s) answer_url:\n", app.AppName, streamsFwdAppID)
		fmt.Fprintf(out, "  from: %s\n", originalAnswerURL)
		fmt.Fprintf(out, "  to:   https://<ngrok-tunnel>/answer (via local server)\n")
		fmt.Fprintf(out, "  Tunnel would forward call audio to: %s\n", streamsFwdTo)
		return nil
	}

	// --- Confirm (unless --yes) ---
	if !streamsFwdYes {
		fmt.Fprintf(out, "⚠  About to modify app %q (%s):\n", app.AppName, streamsFwdAppID)
		fmt.Fprintf(out, "   - answer_url: %s\n     → https://<ngrok-tunnel>/answer (your local handler)\n", originalAnswerURL)
		fmt.Fprintf(out, "   - %s\n", numbersAffectedWarning(client, streamsFwdAppID))
		fmt.Fprintf(out, "   - Restored on Ctrl+C (use --keep to skip restore)\n\n")
		if !confirmInteractive(out, "Continue? [y/N] ") {
			return clierr.BadInput("aborted by user")
		}
	}

	// --- Bind local server on random port ---
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return clierr.Wrap(fmt.Errorf("bind local port: %w", err))
	}
	localPort := listener.Addr().(*net.TCPAddr).Port
	if !jsonOut {
		fmt.Fprintf(out, "⠋ Local server on :%d\n", localPort)
	}

	// Cancel context drives a clean teardown end-to-end.
	ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// --- Start the tunnel ---
	if !jsonOut {
		fmt.Fprintf(out, "⠋ Starting tunnel via %s...\n", tunnel.Describe(streamsFwdTunnel))
	}
	tn, err := tunnel.Start(ctx, localPort, streamsFwdTunnel)
	if err != nil {
		listener.Close()
		return clierr.Wrap(err)
	}
	defer tn.Close()
	if !jsonOut {
		fmt.Fprintf(out, "⠋ Tunnel: %s → :%d\n", tn.PublicURL, localPort)
	}

	// --- PATCH app's answer_url to the tunnel ---
	tunnelAnswerURL := tn.PublicURL + "/answer"
	patchBody := map[string]interface{}{
		"answer_url":    tunnelAnswerURL,
		"answer_method": "POST",
	}
	if apiErr, err := client.Do("POST", client.AccountURL("Application", streamsFwdAppID), patchBody, nil, nil); err != nil {
		listener.Close()
		return clierr.NetworkError(client.AccountURL("Application", streamsFwdAppID), err)
	} else if apiErr != nil {
		listener.Close()
		return apiErr
	}
	if !jsonOut {
		fmt.Fprintf(out, "⠋ App answer_url updated → %s\n\n", tunnelAnswerURL)
		fmt.Fprintf(out, "✓ Ready. Dial %s — events stream below.\n\n", streamsFwdNumber)
	}

	// --- Start serving (HTTP for answer webhook, /ws for streaming) ---
	var events atomic.Int64
	wsTunnelURL := strings.Replace(tn.PublicURL, "https://", "wss://", 1) + "/ws"
	srv := buildLocalStreamServer(out, wsTunnelURL, streamsFwdTo, streamsFwdBidirectional, streamsFwdCodec, streamsFwdRate, streamsFwdPrintPayload, jsonOut, &events)
	srvErrCh := make(chan error, 1)
	go func() {
		err := srv.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			srvErrCh <- err
		}
		close(srvErrCh)
	}()

	// --- Wait for SIGINT or fatal server error ---
	select {
	case <-ctx.Done():
		if !jsonOut {
			fmt.Fprintf(out, "\n^C — tearing down...\n")
		}
	case err := <-srvErrCh:
		if !jsonOut {
			fmt.Fprintf(out, "\nlocal server error: %v\n", err)
		}
	}

	// --- Teardown: stop server, restore answer_url, kill ngrok ---
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = srv.Shutdown(shutdownCtx)
	shutdownCancel()

	var restored bool
	var restoreErr string
	if !streamsFwdKeep {
		if !jsonOut {
			fmt.Fprintf(out, "  Restoring answer_url on %q...", app.AppName)
		}
		restoreBody := map[string]interface{}{
			"answer_url":    originalAnswerURL,
			"answer_method": app.AnswerMethod,
		}
		if apiErr, err := client.Do("POST", client.AccountURL("Application", streamsFwdAppID), restoreBody, nil, nil); err != nil {
			restoreErr = err.Error()
			if !jsonOut {
				fmt.Fprintf(out, " ✗ FAILED: %v\n", err)
				fmt.Fprintf(out, "  Manual restore: plivo account applications update %s --answer-url %s\n",
					streamsFwdAppID, originalAnswerURL)
			}
		} else if apiErr != nil {
			restoreErr = apiErr.Message
			if !jsonOut {
				fmt.Fprintf(out, " ✗ %s\n", apiErr.Message)
				fmt.Fprintf(out, "  Manual restore: plivo account applications update %s --answer-url %s\n",
					streamsFwdAppID, originalAnswerURL)
			}
		} else {
			restored = true
			if !jsonOut {
				fmt.Fprintf(out, " done.\n")
			}
		}
	} else if !jsonOut {
		fmt.Fprintf(out, "  --keep set; answer_url left at %s\n", tunnelAnswerURL)
		fmt.Fprintf(out, "  Manual restore: plivo account applications update %s --answer-url %s\n",
			streamsFwdAppID, originalAnswerURL)
	}

	if jsonOut {
		return output.JSONSuccess(os.Stdout, streamsFwdResult{
			AppID:          streamsFwdAppID,
			TunnelURL:      tn.PublicURL,
			Restored:       restored,
			RestoreError:   restoreErr,
			EventsObserved: events.Load(),
		}, nil)
	}
	fmt.Fprintf(out, "✓ All cleaned up.\n")
	return nil
}

// buildLocalStreamServer returns an http.Server handling two routes:
//
//	POST /answer  → returns PlivoXML with <Stream url="wssTunnel"/>
//	GET  /ws      → upgrades to WebSocket and bridges Plivo ↔ customer's --to
//
// Lifecycle events on the answer webhook + WS connect/disconnect are
// printed to out, unless jsonOut suppresses them for a clean summary at
// the end of the run; events is incremented on each one regardless so the
// summary can still report an events_observed count.
func buildLocalStreamServer(out io.Writer, wssTunnelURL, customerWS string, bidir bool, codec string, rate int, printPayload, jsonOut bool, events *atomic.Int64) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/answer", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		events.Add(1)
		if !jsonOut {
			fmt.Fprintf(out, "[%s] answer webhook from %s\n", time.Now().Format("15:04:05"), r.RemoteAddr)
			if printPayload {
				body := make([]byte, 4096)
				n, _ := r.Body.Read(body)
				fmt.Fprintf(out, "  body (%d bytes): %s\n", n, string(body[:n]))
			}
		}
		w.Header().Set("Content-Type", "application/xml; charset=UTF-8")
		bidiAttr := ""
		if bidir {
			bidiAttr = ` bidirectional="true"`
		}
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Stream%s contentType="%s">%s</Stream>
</Response>`, bidiAttr, wsproxy.ContentType(codec, rate), wssTunnelURL)
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		events.Add(1)
		if !jsonOut {
			fmt.Fprintf(out, "[%s] StreamConnect ← Plivo\n", time.Now().Format("15:04:05"))
		}
		plivoConn, err := websocket.Accept(w, r, nil)
		if err != nil {
			events.Add(1)
			if !jsonOut {
				fmt.Fprintf(out, "  WS accept failed: %v\n", err)
			}
			return
		}
		defer plivoConn.Close(websocket.StatusNormalClosure, "")

		dialCtx, dialCancel := context.WithTimeout(r.Context(), 10*time.Second)
		customerConn, _, err := websocket.Dial(dialCtx, customerWS, nil)
		dialCancel()
		if err != nil {
			events.Add(1)
			if !jsonOut {
				fmt.Fprintf(out, "  dial customer WS %s failed: %v\n", customerWS, err)
			}
			plivoConn.Close(websocket.StatusInternalError, "customer endpoint unreachable")
			return
		}
		defer customerConn.Close(websocket.StatusNormalClosure, "")
		events.Add(1)
		if !jsonOut {
			fmt.Fprintf(out, "[%s] StreamForwarding plivo ↔ %s (bidi=%v)\n", time.Now().Format("15:04:05"), customerWS, bidir)
		}

		bridgeStart := time.Now()
		_ = wsproxy.Bridge(r.Context(), plivoConn, customerConn)
		events.Add(1)
		if !jsonOut {
			fmt.Fprintf(out, "[%s] StreamDisconnect (after %s)\n", time.Now().Format("15:04:05"), time.Since(bridgeStart).Round(time.Millisecond))
		}
	})

	return &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// confirmInteractive prints prompt to out and reads y/n from stdin.
// Defaults to no on any other input or read error.
func confirmInteractive(out io.Writer, prompt string) bool {
	fmt.Fprint(out, prompt)
	var answer string
	if _, err := fmt.Fscan(os.Stdin, &answer); err != nil {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

// numbersAffectedWarning reports how many phone numbers are attached to
// appID — every one of them starts routing through the tunnel, not just
// --number, since the redirect is on the app's answer_url. A failure to
// fetch the count degrades to a generic warning instead of blocking the
// command.
func numbersAffectedWarning(client *api.Client, appID string) string {
	q := url.Values{"application": {appID}, "limit": {"1"}}
	var resp api.NumberList
	apiErr, err := client.Do("GET", client.AccountURL("Number"), nil, q, &resp)
	if err != nil || apiErr != nil {
		return "could not determine how many phone numbers are attached to this app — ALL of them will forward through the tunnel while it runs"
	}
	n := resp.Meta.TotalCount
	noun := "phone numbers"
	if n == 1 {
		noun = "phone number"
	}
	return fmt.Sprintf("%d %s attached to this app will forward through the tunnel", n, noun)
}
