package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// localhost.run needs no install and no account: it is a plain reverse SSH
// forward, and ssh already ships on macOS and Linux. That makes it the only
// provider that works out of the box, which is why it is the fallback when
// ngrok is absent rather than an error telling the user to go sign up.
const (
	lhrHost    = "nokey@localhost.run"
	lhrTimeout = 25 * time.Second
)

// lhrURL matches the https URL localhost.run prints once the forward is up.
// It announces the tunnel in a banner line such as:
//
//	abc123.lhr.life tunneled with tls termination, https://abc123.lhr.life
var lhrURL = regexp.MustCompile(`https://[a-z0-9-]+\.lhr\.life`)

// knownHostsPath returns the dedicated known-hosts file for tunnel providers.
//
// Deliberately separate from ~/.ssh/known_hosts: this is the CLI's trust
// record, and it must not add entries to, or be confused with, the user's own
// SSH trust store.
func knownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".plivo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "known_hosts_tunnel"), nil
}

// hostIsKnown reports whether path already records a key for localhost.run.
func hostIsKnown(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	host := strings.TrimPrefix(lhrHost, "nokey@")
	return strings.Contains(string(b), host)
}

// startLocalhostRun opens a reverse SSH forward and returns once localhost.run
// has announced the public URL.
//
// SA-02: this used to pass StrictHostKeyChecking=no with
// UserKnownHostsFile=/dev/null, on the reasoning that "there is nothing secret
// in the tunnel to protect". That reasons about confidentiality and misses
// integrity. The server's stdout supplies the URL the CLI then writes into the
// Plivo application's answer_url, so a server that can impersonate
// localhost.run redirects the account's live call handling.
//
// localhost.run publishes no host key fingerprint, so there is nothing to pin
// through an authenticated channel, and ssh-keyscan would just re-learn the key
// from the same party we are trying to authenticate. What is achievable is
// trust on first use with a persisted record: accept-new records an unknown
// host once and REFUSES a changed key thereafter. An attacker must now be
// present at the very first connection rather than at any connection, and any
// later substitution fails loudly instead of silently.
//
// This narrows SA-02 rather than closing it. Prefer ngrok, whose client
// authenticates its own service; Start() already does when it is installed.
func startLocalhostRun(ctx context.Context, localPort int) (*Tunnel, error) {
	if _, err := exec.LookPath("ssh"); err != nil {
		return nil, fmt.Errorf("ssh not found, needed for the localhost.run tunnel: %w", err)
	}

	kh, err := knownHostsPath()
	if err != nil {
		return nil, fmt.Errorf("resolve tunnel known-hosts file: %w", err)
	}
	firstUse := !hostIsKnown(kh)

	cmd := exec.CommandContext(ctx, "ssh",
		// accept-new: record an unknown host once, refuse a CHANGED key.
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile="+kh,
		"-o", "ServerAliveInterval=30",
		"-o", "ExitOnForwardFailure=yes",
		"-T", // no pty; we only want the announcement on stdout
		"-R", fmt.Sprintf("80:localhost:%d", localPort),
		lhrHost,
	)
	if firstUse {
		fmt.Fprintf(os.Stderr,
			"  first connection to %s: its host key will be recorded and any later change refused.\n",
			strings.TrimPrefix(lhrHost, "nokey@"))
		fmt.Fprintln(os.Stderr,
			"  this provider publishes no fingerprint to check it against; install ngrok for a verified tunnel.")
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ssh: %w", err)
	}

	urlCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if m := lhrURL.FindString(sc.Text()); m != "" {
				select {
				case urlCh <- m:
				default:
				}
				return
			}
		}
	}()

	select {
	case u := <-urlCh:
		return &Tunnel{PublicURL: u, cmd: cmd}, nil
	case <-time.After(lhrTimeout):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("localhost.run did not announce a URL within %s", lhrTimeout)
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		return nil, ctx.Err()
	}
}

// Provider names accepted by --tunnel.
const (
	ProviderAuto         = "auto"
	ProviderNgrok        = "ngrok"
	ProviderLocalhostRun = "localhost.run"
)

// Providers lists the selectable values, for help text and validation.
var Providers = []string{ProviderAuto, ProviderNgrok, ProviderLocalhostRun}

// Start brings up a tunnel to localPort using the named provider.
//
// "auto" prefers ngrok when it is already installed — it is the more robust of
// the two and anyone who installed it presumably wants it — and otherwise falls
// back to localhost.run so the command works with no setup at all.
func Start(ctx context.Context, localPort int, provider string) (*Tunnel, error) {
	switch strings.TrimSpace(provider) {
	case "", ProviderAuto:
		if _, err := findNgrok(); err == nil {
			return StartNgrok(ctx, localPort)
		}
		return startLocalhostRun(ctx, localPort)
	case ProviderNgrok:
		return StartNgrok(ctx, localPort)
	case ProviderLocalhostRun:
		return startLocalhostRun(ctx, localPort)
	default:
		return nil, fmt.Errorf("unknown tunnel provider %q (want one of: %s)", provider, strings.Join(Providers, ", "))
	}
}

// Describe names the provider that Start would pick, for the confirm prompt.
func Describe(provider string) string {
	switch strings.TrimSpace(provider) {
	case "", ProviderAuto:
		if _, err := findNgrok(); err == nil {
			return "ngrok (already installed)"
		}
		return "localhost.run (no install or account needed)"
	default:
		return provider
	}
}
