//go:build internal

// Gated behind `internal`: the email+password flow hits a hodor endpoint
// (/v1/accounts/login-cli) that's gated to dev hodor — public hodor
// doesn't have the route, and even if it did, public CLI users wouldn't
// be able to satisfy the reCAPTCHA requirement that the prod login enforces.
// Keeping this option out of the public picker prevents a confusing
// "third option that doesn't work" experience for end users.

package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"golang.org/x/term"
)

func init() {
	// Public build's runLoginEmailDispatch refuses with "internal-only"; the
	// internal build swaps in the real handler so `plivo login --email` works.
	runLoginEmailDispatch = runLoginEmailPassword
}

func runLoginEmailPassword(saveEnv string) error {
	// Endpoint is dev-only. Without --env <non-prod>, we'd POST to prod
	// hodor which 404s — fail fast with the right hint.
	if saveEnv == "" {
		return clierr.BadInput("email/password login is dev-only — re-run with --env dev")
	}
	hodorBase, ok := resolveLoginEnv(saveEnv)
	if !ok {
		return clierr.BadInput(fmt.Sprintf("unknown env %q for email/password login", saveEnv))
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return clierr.BadInput("email/password login requires an interactive terminal (stdin must be a TTY)")
	}

	email, err := readLine(os.Stdin, os.Stderr, "Email: ")
	if err != nil {
		return err
	}
	password, err := readPasswordMasked(os.Stderr, "Password: ")
	if err != nil {
		return err
	}

	resp, err := postLoginCLI(hodorBase, email, password)
	if err != nil {
		return err
	}
	if resp.Data.PlivoAuthID == "" || resp.Data.PlivoAuthToken == "" {
		// Successful 200 but no creds = 2FA / org-switch required. We don't
		// implement the multi-step dance yet; point them at the browser flow.
		return clierr.BadInput("this account needs 2FA or org selection — re-run with --browser instead")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return persistProfile(cfg, loginName, loginBundle{
		AuthID:    resp.Data.PlivoAuthID,
		AuthToken: resp.Data.PlivoAuthToken,
		Email:     emailFromResponseOr(resp.Data.Email, email),
		Name:      resp.Data.Name,
		AomUUID:   resp.Data.AomUUID,
		SaveEnv:   saveEnv,
		// Region intentionally empty — login-cli response doesn't carry it
		// today; if hodor starts returning it, just add the field here.
	})
}

// emailFromResponseOr prefers the server-confirmed email (catches the
// "user typed Foo@example.com, hodor normalised to foo@example.com" case)
// and falls back to what the user typed when hodor's reply omits it.
func emailFromResponseOr(serverEmail, typedEmail string) string {
	if serverEmail != "" {
		return serverEmail
	}
	return typedEmail
}

// loginCLIResponse mirrors the LoginResponse envelope hodor returns:
// {"api_id":"...","message":"Authentication successful","data":{...}}
// Only the fields we use are declared.
type loginCLIResponse struct {
	Data struct {
		Name           string `json:"name"`
		Email          string `json:"email"`
		PlivoAuthID    string `json:"plivo_auth_id"`
		PlivoAuthToken string `json:"plivo_auth_token"`
		AomUUID        string `json:"aom_uuid,omitempty"` // populated by hodor once it learns to ship it back; empty on older hodor builds
	} `json:"data"`
	Errors  interface{} `json:"errors,omitempty"`
	Message string      `json:"message,omitempty"`
}

func postLoginCLI(hodorBase, email, password string) (*loginCLIResponse, error) {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	url := strings.TrimRight(hodorBase, "/") + "/v1/accounts/login-cli"

	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	r, err := client.Do(req)
	if err != nil {
		return nil, clierr.NetworkError(url, err)
	}
	defer r.Body.Close()

	raw, _ := io.ReadAll(r.Body)
	if r.StatusCode >= 400 {
		// Hodor wraps errors in the same envelope; show what we got back so
		// the user knows whether it was wrong password vs 5xx.
		return nil, clierr.BadInput(fmt.Sprintf("login failed (HTTP %d): %s", r.StatusCode, string(raw)))
	}
	var parsed loginCLIResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode login response: %w", err)
	}
	return &parsed, nil
}

// readLine prompts on out and returns a single trimmed line from in.
// Shared with the picker pattern — keeps the interactive UX consistent.
func readLine(in *os.File, out *os.File, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// readPasswordMasked prompts on out and reads a password from stdin
// without echoing it. Falls back to plain readLine if stdin isn't a TTY
// (shouldn't happen in practice — runLoginEmailPassword guards on TTY —
// but defensive in case the guard slips).
func readPasswordMasked(out *os.File, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Read unmasked — caller already checked TTY, this is a safety net.
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		return strings.TrimSpace(line), err
	}
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(out)
	if err != nil {
		return "", err
	}
	return string(pw), nil
}
