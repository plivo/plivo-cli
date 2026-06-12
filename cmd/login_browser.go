package cmd

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
)

// cliTokenEnvelope is the response shape from the auth server's
// /v1/accounts/cli/token endpoint. Standard Plivo envelope (api_id +
// data); we only need the `data` block. The session JWT is intentionally
// NOT part of the bundle.
//
// Keep this as a package-level type — the round-trip test in
// login_browser_test.go uses it to lock in the wire shape and fail fast
// if the envelope wrapper drifts.
type cliTokenEnvelope struct {
	Data struct {
		PlivoAuthID    string `json:"plivo_auth_id"`
		PlivoAuthToken string `json:"plivo_auth_token"`
		AomUUID        string `json:"aom_uuid"`
		Region         string `json:"region"`
		Email          string `json:"email,omitempty"` // populated by the auth server when the dashboard session has it — may be empty
		Name           string `json:"name,omitempty"`  // human display name from the user record
	} `json:"data"`
}

// runLoginBrowser implements `plivo login --browser`: the loopback-OAuth
// (PKCE) flow that hands off to the auth server's
// /v1/accounts/cli/{authorize, exchange, token} endpoints + the Console's
// /cli/authorize consent page.
//
// Sequence:
//  1. Generate PKCE verifier + S256 challenge + state.
//  2. Bind 127.0.0.1:0 (kernel picks an ephemeral port).
//  3. Open the user's default browser to
//     ${buddyBase}/v1/accounts/cli/authorize with the loopback cb URL.
//  4. Wait (5m) for the browser to land back on our local listener with
//     ?state=…&code=…. Matches the auth server's 10-min state TTL with
//     comfortable headroom for login / 2FA / consent click-through.
//  5. Validate state; POST /v1/accounts/cli/token with the verifier.
//  6. Persist the bundle to ~/.plivo/config.toml + OS keychain.
func runLoginBrowser() error {
	verifier, challenge, err := pkcePair()
	if err != nil {
		return fmt.Errorf("generate PKCE pair: %w", err)
	}
	state, err := randomURLToken(32)
	if err != nil {
		return fmt.Errorf("generate state: %w", err)
	}

	// Bind first so we know the port to include in the cb URL.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("bind loopback listener: %w", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	// Auth-server URL: resolve the same way `ask` / `support` do — flag,
	// env, config, default. The CLI's --buddy-url override applies here too
	// since both surfaces share the same edge.
	client := api.New("", "", 30*time.Second) // creds-less; auth happens in /token
	applyBuddyURL(client)

	// Send only the loopback port (see buildAuthorizeURL — dodges the WAF SSRF block).
	authURL := buildAuthorizeURL(client.BuddyBaseURL, port, state, challenge, deviceHint())
	fmt.Fprintln(os.Stderr, "Opening your browser to:")
	fmt.Fprintln(os.Stderr, "  "+authURL)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "If the browser didn't open, copy-paste that URL above.")
	fmt.Fprintln(os.Stderr)
	if err := openBrowser(authURL); err != nil {
		// Non-fatal — the URL is printed above so the user can paste it.
		fmt.Fprintf(os.Stderr, "(could not auto-open the browser: %v)\n", err)
	}

	// Wait up to 5m for the callback from the Console redirect chain.
	// The auth server's state TTL is 10 min — 5 min on the CLI side
	// leaves plenty of room for login + 2FA + consent without the CLI
	// giving up first.
	cbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	code, err := awaitLoopbackCallback(cbCtx, listener, state)
	if err != nil {
		return err
	}

	// Redeem the code for the creds bundle, then persist.
	return redeemAndPersist(client, state, code, verifier, loginName)
}

// redeemAndPersist performs the second half of the loopback-OAuth flow: POST
// /v1/accounts/cli/token with the (state, code, code_verifier) triple, then
// drop the returned bundle into ~/.plivo/config.toml + the OS keychain.
//
// Split out from runLoginBrowser so the wire + persistence half is
// independently testable against an httptest auth-server mock without
// having to drive a real browser. The exact behaviour (error wrapping,
// empty-bundle guard, keychain fallback, "first profile becomes active"
// rule, stderr confirmation) matches what runLoginBrowser used to do
// inline — keep them in sync if you touch one.
func redeemAndPersist(client *api.Client, state, code, verifier, profileName string) error {
	tokenURL := client.BuddyURL("/v1/accounts/cli/token")
	body := map[string]string{
		"state":         state,
		"code":          code,
		"code_verifier": verifier,
	}
	var resp cliTokenEnvelope
	apiErr, gerr := client.Do("POST", tokenURL, body, nil, &resp)
	if gerr != nil {
		return clierr.NetworkError("redeeming CLI token", gerr)
	}
	if apiErr != nil {
		return apiErr
	}
	if resp.Data.PlivoAuthID == "" || resp.Data.PlivoAuthToken == "" {
		return fmt.Errorf("token redemption returned an empty bundle")
	}

	// Shared persistence — same code path as the manual + email flows so
	// storage layer changes don't drift between methods.
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return persistProfile(cfg, profileName, loginBundle{
		AuthID:    resp.Data.PlivoAuthID,
		AuthToken: resp.Data.PlivoAuthToken,
		Email:     resp.Data.Email,
		Name:      resp.Data.Name,
		AomUUID:   resp.Data.AomUUID,
		Region:    resp.Data.Region,
	})
}

// pkcePair generates a PKCE (RFC 7636) verifier + S256 challenge pair.
// Verifier is 43 chars (32 bytes base64url no-pad); challenge is also 43
// chars (SHA256 of the verifier, base64url no-pad).
func pkcePair() (verifier, challenge string, err error) {
	verifier, err = randomURLToken(32)
	if err != nil {
		return "", "", err
	}
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}

// randomURLToken returns N bytes of crypto/rand encoded as base64url (no
// padding). N=32 → 43-char output (the PKCE / state minimum).
func randomURLToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// buildAuthorizeURL composes the GET URL the browser opens. It sends cb_port
// (not a full cb URL) so hodor pins http://127.0.0.1:<port> server-side and the
// AWS WAF won't 403 the loopback URL as SSRF. device is optional (consent label).
func buildAuthorizeURL(buddyBase string, port int, state, challenge, device string) string {
	base := strings.TrimRight(buddyBase, "/")
	q := url.Values{}
	q.Set("cb_port", strconv.Itoa(port))
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	if device != "" {
		q.Set("device", device)
	}
	return base + "/v1/accounts/cli/authorize?" + q.Encode()
}

// deviceHint returns a friendly label for this machine so the Console
// consent screen can render "Mac MacBook-Pro is requesting access" instead
// of the generic "your machine". Best-effort: if os.Hostname() fails or
// returns empty we return "" and Console falls back to the generic copy.
//
// Hostname is locally-resolvable PII — visible to the user clicking the
// consent screen, included in server access logs as a query param. Users
// who'd rather not surface it can rely on the fallback by setting their
// machine's hostname to an empty string (rare).
func deviceHint() string {
	h, _ := os.Hostname()
	h = strings.TrimSuffix(h, ".local") // macOS appends this; strip for readability
	if h == "" {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return "Mac " + h
	case "windows":
		return "Windows " + h
	default:
		return h
	}
}

// awaitLoopbackCallback serves one HTTP request on listener and returns
// the `code` query param, after validating that `state` matches. Times
// out via the context. The browser tab sees a tiny "you can close this"
// confirmation page on success.
func awaitLoopbackCallback(ctx context.Context, listener net.Listener, expectedState string) (string, error) {
	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Anti-favicon noise: don't process /favicon.ico hits.
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		gotState := r.URL.Query().Get("state")
		code := r.URL.Query().Get("code")
		if gotState != expectedState {
			http.Error(w, "state mismatch — possible CSRF; close this tab and retry", http.StatusBadRequest)
			done <- result{err: fmt.Errorf("state mismatch on loopback callback")}
			return
		}
		if code == "" {
			http.Error(w, "missing code in callback", http.StatusBadRequest)
			done <- result{err: fmt.Errorf("missing code in callback URL")}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>Plivo CLI</title></head>` +
			`<body style="font-family: -apple-system, system-ui, sans-serif; padding: 2rem; max-width: 480px; margin: 4rem auto; line-height: 1.5;">` +
			`<h1 style="font-size: 1.5rem;">✓ Authenticated</h1>` +
			`<p>You can close this tab. The CLI now has your credentials.</p>` +
			`</body></html>`))
		done <- result{code: code}
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	select {
	case r := <-done:
		return r.code, r.err
	case <-ctx.Done():
		return "", fmt.Errorf("timed out waiting for browser callback (5m); finish signing in and approving access in the browser, then run `plivo login` again")
	}
}

// openBrowser opens the URL in the user's default browser. Platform-
// specific: open / xdg-open / start. Failure is non-fatal — the caller
// printed the URL beforehand so the user can copy-paste.
func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "linux":
		cmd = exec.Command("xdg-open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		return fmt.Errorf("unsupported GOOS %q for browser open", runtime.GOOS)
	}
	return cmd.Start()
}
