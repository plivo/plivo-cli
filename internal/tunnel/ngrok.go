// Package tunnel wraps an ngrok subprocess for `plivo voice streams forward`.
// Phase 1 (v1.1): ngrok-subprocess. Phase 2 (future): Plivo-hosted relay.
// The Tunnel surface stays the same when the underlying mechanism upgrades.
package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Tunnel is a live ngrok session: a public URL pointing at a local port,
// plus a Close that tears the subprocess down.
type Tunnel struct {
	PublicURL string // e.g. https://abc123.ngrok-free.dev
	cmd       *exec.Cmd
}

// StartNgrok launches `ngrok http <localPort>` and returns when the
// public URL is reachable via ngrok's local API (typically 1-2 seconds).
// Caller MUST call Close() to stop the subprocess.
//
// Looks for ngrok in PATH first, then in ~/.plivo/bin/ngrok. If missing,
// returns an error with a clear install hint — auto-install is deferred.
func StartNgrok(ctx context.Context, localPort int) (*Tunnel, error) {
	binary, err := findNgrok()
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, binary, "http", fmt.Sprintf("%d", localPort), "--log=stdout")
	// We don't care about stdout; ngrok's local API is the source of truth.
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ngrok: %w", err)
	}

	// Poll ngrok's local API for the public URL. ngrok serves a JSON listing
	// at http://127.0.0.1:4040/api/tunnels once it's bound. Default timeout
	// is 10s; that's plenty for cold-start.
	publicURL, err := waitForNgrokTunnel(ctx, 10*time.Second, localPort, cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("ngrok did not report a tunnel URL: %w (is ngrok already running on :4040?)", err)
	}

	return &Tunnel{PublicURL: publicURL, cmd: cmd}, nil
}

// Close kills the ngrok subprocess. Idempotent; safe to call from a defer.
func (t *Tunnel) Close() error {
	if t == nil || t.cmd == nil || t.cmd.Process == nil {
		return nil
	}
	if err := t.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	_ = t.cmd.Wait()
	return nil
}

// findNgrok returns the absolute path to a usable ngrok binary, or an
// error with install instructions. Looks in PATH first, then
// ~/.plivo/bin/ngrok (where we'd cache an auto-install in a future iter).
func findNgrok() (string, error) {
	if p, err := exec.LookPath("ngrok"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	candidate := filepath.Join(home, ".plivo", "bin", "ngrok")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("ngrok not found in PATH or ~/.plivo/bin/. Install from https://ngrok.com/download and re-run")
}

// waitForNgrokTunnel polls 127.0.0.1:4040/api/tunnels until a tunnel
// forwarding to wantPort appears, the child exits, or the deadline expires.
//
// SA-04: this used to return the first HTTPS tunnel advertised by whatever was
// listening on 4040. That port belongs to whichever ngrok started first, so an
// unrelated instance (a colleague's, a leftover from an earlier run, another
// tool) could hand us its URL, which the caller then writes into the Plivo
// application's answer_url. The account's calls would be routed to a tunnel we
// do not own.
//
// Two conditions now bind discovery to our own process: the tunnel must
// forward to the port we asked for, and our child must still be running.
//
// Scope, stated plainly: this defends against ACCIDENTAL adoption, which is
// the realistic case — a colleague's ngrok, a leftover from an earlier run,
// another tool. It does NOT defend against a hostile local process, which can
// simply report our port alongside its own URL. Closing that means not
// trusting the shared 4040 API at all and reading the URL from our own
// child's stdout instead. That is the right fix and is not attempted here
// because ngrok is not available to verify the log format against.
func waitForNgrokTunnel(ctx context.Context, timeout time.Duration, wantPort int, cmd *exec.Cmd) (string, error) {
	deadline := time.Now().Add(timeout)
	httpClient := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		// If our ngrok died, no amount of polling will help, and whatever is
		// on 4040 now is definitely not ours.
		if cmd != nil && cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return "", fmt.Errorf("ngrok exited before reporting a tunnel")
		}
		resp, err := httpClient.Get("http://127.0.0.1:4040/api/tunnels")
		if err == nil {
			url, ok := extractHTTPSURL(resp.Body, wantPort)
			resp.Body.Close()
			if ok {
				return url, nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return "", fmt.Errorf("timed out waiting for an ngrok tunnel forwarding to port %d", wantPort)
}

// extractHTTPSURL pulls the first https public_url out of the ngrok
// /api/tunnels JSON response.
func extractHTTPSURL(body io.Reader, wantPort int) (string, bool) {
	var doc struct {
		Tunnels []struct {
			PublicURL string `json:"public_url"`
			Config    struct {
				Addr string `json:"addr"`
			} `json:"config"`
		} `json:"tunnels"`
	}
	if err := json.NewDecoder(body).Decode(&doc); err != nil {
		return "", false
	}
	for _, t := range doc.Tunnels {
		if !strings.HasPrefix(t.PublicURL, "https://") {
			continue
		}
		if !forwardsToPort(t.Config.Addr, wantPort) {
			continue
		}
		return t.PublicURL, true
	}
	return "", false
}

// forwardsToPort reports whether an ngrok tunnel's configured local address
// points at wantPort.
//
// addr takes forms like "http://localhost:8080", "localhost:8080" or
// ":8080", so the port is compared rather than the whole string.
func forwardsToPort(addr string, wantPort int) bool {
	if addr == "" {
		return false
	}
	want := strconv.Itoa(wantPort)
	// Strip any scheme, then take the text after the final colon.
	if i := strings.Index(addr, "://"); i >= 0 {
		addr = addr[i+3:]
	}
	addr = strings.TrimSuffix(addr, "/")
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return false
	}
	return addr[i+1:] == want
}
