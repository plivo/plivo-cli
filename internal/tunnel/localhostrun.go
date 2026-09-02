package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
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

// startLocalhostRun opens a reverse SSH forward and returns once localhost.run
// has announced the public URL.
//
// StrictHostKeyChecking is disabled deliberately: the host key rotates and
// there is nothing secret in the tunnel to protect. The audio path itself is
// TLS-terminated by localhost.run.
func startLocalhostRun(ctx context.Context, localPort int) (*Tunnel, error) {
	if _, err := exec.LookPath("ssh"); err != nil {
		return nil, fmt.Errorf("ssh not found, needed for the localhost.run tunnel: %w", err)
	}

	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ServerAliveInterval=30",
		"-o", "ExitOnForwardFailure=yes",
		"-T", // no pty; we only want the announcement on stdout
		"-R", fmt.Sprintf("80:localhost:%d", localPort),
		lhrHost,
	)
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
