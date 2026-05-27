//go:build internal

// Gated behind the `internal` build tag: scoped tokens are minted by hodor, a
// Plivo-internal service, so this surface ships only in internal builds.

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// Hodor scoped-token routes (via regional Contacto auth-api gateway).
const (
	authTokenPath = "/v1/agent/token"
)

var authTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Mint, list, and revoke scoped agent tokens (PoC)",
	Long: `plivo auth token — manage scoped tokens (stk_…) for AI agents.

Scoped tokens are short-lived, module-restricted credentials minted by the
hodor /v1/agent/token endpoints. They live alongside (not replacing) the
classic auth_id / auth_token Basic-Auth credentials and let an agent script
hold a least-privilege token instead of full account creds.

Requires a Contacto session — run 'plivo contacto login' first.`,
}

var (
	authTokenMintModules string
	authTokenMintTTL     string
	authTokenMintLabel   string
)

var authTokenMintCmd = &cobra.Command{
	Use:   "mint",
	Short: "Mint a scoped agent token",
	Long: `plivo auth token mint — POST /v1/agent/token.

Available modules:
  number.read, number.write
  application.read, application.write
  message.read, message.send
  call.read, call.make
  agent.read, agent.write
  *   (full access — use sparingly)

TTL accepts Go-style durations plus 'd' for days: 30m, 1h, 6h, 24h, 7d.

Example:
  plivo auth token mint --modules number.read,message.send --ttl 1h --label "demo"`,
	RunE: runAuthTokenMint,
}

var authTokenListCmd = &cobra.Command{
	Use:   "list",
	Short: "List scoped agent tokens on the active org",
	Long: `plivo auth token list — GET /v1/agent/token.

In a TTY, renders as a table:
  ID          LABEL     MODULES                     EXPIRES IN
  stk_abc…    demo      number.read, message.send   42m

Piped / --output json: returns the raw response.`,
	RunE: runAuthTokenList,
}

var authTokenRevokeCmd = &cobra.Command{
	Use:   "revoke <stk_…>",
	Short: "Revoke a scoped agent token by id",
	Long: `plivo auth token revoke — DELETE /v1/agent/token/:id.

Once revoked, requests authenticating with the token start failing
immediately. The id is the same value 'plivo auth token list' shows
(stk_… prefix).`,
	Args: cobra.ExactArgs(1),
	RunE: runAuthTokenRevoke,
}

func init() {
	authTokenMintCmd.Flags().StringVar(&authTokenMintModules, "modules", "",
		"comma-separated module scopes (required) — e.g. number.read,message.send")
	authTokenMintCmd.Flags().StringVar(&authTokenMintTTL, "ttl", "1h",
		"token lifetime: 30m, 1h, 6h, 24h, 7d (Go ParseDuration + 'd' suffix)")
	authTokenMintCmd.Flags().StringVar(&authTokenMintLabel, "label", "",
		"human-readable label attached to the token")
	_ = authTokenMintCmd.MarkFlagRequired("modules")

	authTokenCmd.AddCommand(authTokenMintCmd, authTokenListCmd, authTokenRevokeCmd)
	// Registered here (not in auth.go) so it only attaches in internal builds.
	authCmd.AddCommand(authTokenCmd)
}

// parseTTL parses durations including 'd' suffix (Go's time.ParseDuration only
// goes up to hours). Examples: "30m", "1h", "6h", "24h", "7d".
func parseTTL(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if rest, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(rest)
		if err != nil {
			return 0, fmt.Errorf("invalid day count %q: %w", s, err)
		}
		if n <= 0 {
			return 0, fmt.Errorf("ttl must be positive, got %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w (accepts 30m, 1h, 24h, 7d)", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("ttl must be positive, got %q", s)
	}
	return d, nil
}

// parseModules splits the --modules flag and trims whitespace. Rejects empties.
func parseModules(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		m := strings.TrimSpace(p)
		if m == "" {
			continue
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--modules must contain at least one module")
	}
	return out, nil
}

// mintTokenResponse mirrors hodor's POST /v1/agent/token response.
type mintTokenResponse struct {
	Token     string   `json:"token"`
	Modules   []string `json:"modules"`
	Label     string   `json:"label,omitempty"`
	ExpiresAt string   `json:"expires_at"`
	CreatedAt string   `json:"created_at,omitempty"`
}

func runAuthTokenMint(cmd *cobra.Command, args []string) error {
	modules, err := parseModules(authTokenMintModules)
	if err != nil {
		return clierr.BadFlag("modules", err.Error())
	}
	ttl, err := parseTTL(authTokenMintTTL)
	if err != nil {
		return clierr.BadFlag("ttl", err.Error())
	}

	c, err := getContactoClient()
	if err != nil {
		return err
	}

	body := map[string]any{
		"modules":     modules,
		"ttl_seconds": int(ttl.Seconds()),
	}
	if authTokenMintLabel != "" {
		body["label"] = authTokenMintLabel
	}

	if dryRunFlag {
		fmt.Fprintf(os.Stderr, "[dry-run] POST %s\n", authTokenPath)
		b, _ := json.MarshalIndent(body, "  ", "  ")
		fmt.Fprintf(os.Stderr, "  body:\n  %s\n", string(b))
		return nil
	}

	resp, err := c.Do(cmd.Context(), "POST", authTokenPath, body)
	if err != nil {
		return clierr.NetworkError("hodor", err)
	}
	if resp.Status == 404 {
		return &clierr.Error{
			Code:       clierr.CodeResourceNotFound,
			Message:    "scoped-token endpoint not deployed yet",
			Hint:       "Try again after hodor dev deploys POST /v1/agent/token.",
			StatusCode: 404,
		}
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return clierr.FromHTTP(resp.Status, resp.Header.Get("X-Request-ID"), resp.Body)
	}

	var out mintTokenResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return fmt.Errorf("decode response: %w (body: %s)", err, string(resp.Body))
	}

	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, out, nil)
	}
	return printMintedToken(out, ttl)
}

func printMintedToken(t mintTokenResponse, ttl time.Duration) error {
	fmt.Fprintln(os.Stderr, "✓ Token minted")
	fmt.Fprintf(os.Stderr, "  token:    %s\n", t.Token)
	fmt.Fprintf(os.Stderr, "  modules:  %s\n", strings.Join(t.Modules, ", "))
	fmt.Fprintf(os.Stderr, "  expires:  %s  (in %s)\n", t.ExpiresAt, prettyDuration(ttl))
	if t.Label != "" {
		fmt.Fprintf(os.Stderr, "  label:    %s\n", t.Label)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  export PLIVO_AGENT_TOKEN=%s    # to use in scripts\n", t.Token)
	// Also write the token plainly to stdout so scripts can pipe it.
	fmt.Fprintln(os.Stdout, t.Token)
	return nil
}

// prettyDuration formats a duration as the most natural unit (1h, 30m, 7d, …).
func prettyDuration(d time.Duration) string {
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return d.String()
}

// listTokenEntry mirrors one item in hodor's GET /v1/agent/token response.
type listTokenEntry struct {
	ID        string   `json:"id"`
	Modules   []string `json:"modules"`
	Label     string   `json:"label,omitempty"`
	ExpiresAt string   `json:"expires_at"`
	CreatedAt string   `json:"created_at,omitempty"`
}

type listTokenResponse struct {
	Data []listTokenEntry `json:"data"`
}

func runAuthTokenList(cmd *cobra.Command, args []string) error {
	c, err := getContactoClient()
	if err != nil {
		return err
	}

	if dryRunFlag {
		fmt.Fprintf(os.Stderr, "[dry-run] GET %s\n", authTokenPath)
		return nil
	}

	resp, err := c.Do(cmd.Context(), "GET", authTokenPath, nil)
	if err != nil {
		return clierr.NetworkError("hodor", err)
	}
	if resp.Status == 404 {
		return &clierr.Error{
			Code:       clierr.CodeResourceNotFound,
			Message:    "scoped-token endpoint not deployed yet",
			Hint:       "Try again after hodor dev deploys GET /v1/agent/token.",
			StatusCode: 404,
		}
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return clierr.FromHTTP(resp.Status, resp.Header.Get("X-Request-ID"), resp.Body)
	}

	// JSON output: pass through verbatim (preserves any extra fields hodor adds).
	if effectiveFormat() == output.FormatJSON {
		writeJSONStdout(resp.Body)
		return nil
	}

	var out listTokenResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return fmt.Errorf("decode response: %w (body: %s)", err, string(resp.Body))
	}
	return renderTokenList(out.Data)
}

func renderTokenList(items []listTokenEntry) error {
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "(no scoped tokens)")
		return nil
	}
	rows := [][]string{{"ID", "LABEL", "MODULES", "EXPIRES IN"}}
	now := time.Now()
	for _, t := range items {
		modules := strings.Join(t.Modules, ", ")
		if modules == "" {
			modules = "-"
		}
		label := t.Label
		if label == "" {
			label = "-"
		}
		rows = append(rows, []string{
			truncateID(t.ID),
			label,
			modules,
			expiresIn(t.ExpiresAt, now),
		})
	}
	return output.Table(os.Stdout, rows)
}

// truncateID shortens `stk_xxxxxxxxxxxxxxxx` to `stk_xxxx…` for tabular display.
// JSON output keeps the full id.
func truncateID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:8] + "…"
}

// expiresIn renders a relative duration ("42m", "6h", "expired"). Falls back
// to the raw timestamp string if it can't be parsed as RFC3339.
func expiresIn(ts string, now time.Time) string {
	if ts == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	d := t.Sub(now)
	if d <= 0 {
		return "expired"
	}
	return prettyDuration(d.Round(time.Minute))
}

// revokeTokenResponse mirrors hodor's DELETE /v1/agent/token/:id response.
type revokeTokenResponse struct {
	OK bool `json:"ok"`
}

func runAuthTokenRevoke(cmd *cobra.Command, args []string) error {
	id := strings.TrimSpace(args[0])
	if id == "" {
		return clierr.BadInput("token id is required")
	}

	c, err := getContactoClient()
	if err != nil {
		return err
	}

	path := authTokenPath + "/" + id

	if dryRunFlag {
		fmt.Fprintf(os.Stderr, "[dry-run] DELETE %s\n", path)
		return nil
	}

	resp, err := c.Do(cmd.Context(), "DELETE", path, nil)
	if err != nil {
		return clierr.NetworkError("hodor", err)
	}
	if resp.Status == 404 {
		// 404 is ambiguous here — could be "endpoint not deployed yet" or
		// "token id doesn't exist". Surface both possibilities in the hint.
		return &clierr.Error{
			Code:       clierr.CodeResourceNotFound,
			Message:    fmt.Sprintf("token %s not found", id),
			Hint:       "Either the scoped-token endpoint isn't deployed yet, or the id was already revoked. Try `plivo auth token list` to confirm.",
			StatusCode: 404,
		}
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return clierr.FromHTTP(resp.Status, resp.Header.Get("X-Request-ID"), resp.Body)
	}

	// Response body may be empty (204) or {"ok": true}. Accept both.
	var out revokeTokenResponse
	if len(resp.Body) > 0 {
		_ = json.Unmarshal(resp.Body, &out)
	}

	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, map[string]any{"ok": true, "id": id}, nil)
	}
	fmt.Fprintf(os.Stderr, "✓ Revoked token %s\n", id)
	return nil
}
