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
	publicURL, err := waitForNgrokTunnel(ctx, 10*time.Second)
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

// waitForNgrokTunnel polls 127.0.0.1:4040/api/tunnels until at least one
// HTTPS tunnel appears or the deadline expires. Returns the public_url
// of the first https tunnel.
func waitForNgrokTunnel(ctx context.Context, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	httpClient := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		resp, err := httpClient.Get("http://127.0.0.1:4040/api/tunnels")
		if err == nil {
			url, ok := extractHTTPSURL(resp.Body)
			resp.Body.Close()
			if ok {
				return url, nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return "", fmt.Errorf("timed out polling ngrok API")
}

// extractHTTPSURL pulls the first https public_url out of the ngrok
// /api/tunnels JSON response.
func extractHTTPSURL(body io.Reader) (string, bool) {
	var doc struct {
		Tunnels []struct {
			PublicURL string `json:"public_url"`
		} `json:"tunnels"`
	}
	if err := json.NewDecoder(body).Decode(&doc); err != nil {
		return "", false
	}
	for _, t := range doc.Tunnels {
		if len(t.PublicURL) > 8 && t.PublicURL[:8] == "https://" {
			return t.PublicURL, true
		}
	}
	return "", false
}
