package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// login flags
var (
	loginAuthID         string // when --auth-id supplied with a value, skips the auth_id prompt
	loginAuthIDFlag     bool   // --auth-id (bool form): trigger manual mode interactively
	loginAuthTokenStdin bool
	loginName           string
	loginNoVerify       bool
	loginBrowser        bool
	loginEmail          bool // --email: trigger email+password flow (internal builds only)
	loginEnv            string
)

// loginCmd is the unified entry point for adding/replacing a credential
// profile. Replaces the previous `plivo auth login` and `plivo contacto
// login` commands. v1 ships the auth_id + auth_token method; --email
// (password) and --browser (loopback OAuth) are Phase 2 / Phase 3 and will
// be added once their upstream endpoints land.
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to Plivo (saves credentials securely to the OS keychain)",
	Long: `Log in to Plivo and save credentials for subsequent commands.

The auth_token is stored in your OS keychain (macOS Keychain, Windows
Credential Manager, Linux Secret Service). The CLI falls back to writing
the token inline in ~/.plivo/config.toml (chmod 0600) only when no
keychain is available.

By default, the CLI validates the credentials by calling GET /Account/
before saving — use --no-verify to skip if you're offline or hitting a
local mock.`,
	Example: `  plivo login                                       # opens browser (default)
  plivo login --manual                              # prompts for auth_id + token + email
  plivo login --auth-id MAxxxxxxxxxxxxxxxxxxxx      # pre-supplies auth_id, prompts for the rest
  echo "$TOKEN" | plivo login --auth-id MAxx --auth-token-stdin
  plivo login --email                               # email + password (internal builds only)
  plivo login --name staging                        # save under a named profile`,
	RunE: runLogin,
}

// logoutCmd removes a profile and its keychain entry.
var logoutCmd = &cobra.Command{
	Use:   "logout [profile]",
	Short: "Log out a profile (deletes its entry + removes the auth_token from the OS keychain)",
	Long: `Log out a profile. With no argument, logs out the active profile.

Removes the profile row from ~/.plivo/config.toml and best-effort deletes
the auth_token from the OS keychain. If the active profile was logged
out, no profile becomes active until you ` + "`plivo auth use <name>`" + ` (or
log in again).`,
	Example: `  plivo logout              # active profile
  plivo logout staging      # named profile`,
	Args: cobra.MaximumNArgs(1),
	RunE: runLogout,
}

func init() {
	// --auth-id pre-supplies the auth_id; passing only --auth-id with no
	// value isn't supported (cobra StringVar can't be both empty-string-as-
	// bare-marker and a value-holder without conflicting with `--auth-id VAL`
	// the existing scripted usage). Use --manual to trigger manual mode
	// without supplying a value.
	loginCmd.Flags().StringVar(&loginAuthID, "auth-id", "",
		"Plivo auth_id (triggers manual entry; prompts for token + email)")
	loginCmd.Flags().BoolVar(&loginAuthIDFlag, "manual", false,
		"trigger manual auth_id + auth_token + email entry (interactive)")
	loginCmd.Flags().BoolVar(&loginAuthTokenStdin, "auth-token-stdin", false,
		"read auth_token from stdin (for CI / scripts); hidden interactive prompt otherwise")
	loginCmd.Flags().BoolVar(&loginEmail, "email", false,
		"log in with email + password (internal builds only — dev hodor endpoint)")
	loginCmd.Flags().StringVarP(&loginName, "name", "n", "default",
		"profile name to save under")
	loginCmd.Flags().BoolVar(&loginNoVerify, "no-verify", false,
		"skip the GET /Account/ validation hit (offline / mock use only)")
	loginCmd.Flags().BoolVar(&loginBrowser, "browser", false,
		"no-op (browser is the default); kept for backward compat")
	loginCmd.Flags().StringVar(&loginEnv, "env", "",
		`target environment (default "prod"). Persisted on the profile so subsequent commands inherit it.`)

	rootCmd.AddCommand(loginCmd, logoutCmd)
}

// resolveLoginEnvFlag validates the --env flag and returns the env name
// that should be persisted on the profile (empty for the default "prod"
// — see Profile.Env). The actual URL override is resolved by
// applyBuddyURL reading loginEnv directly, so we don't need to thread a
// URL through here.
func resolveLoginEnvFlag() (saveEnv string, err error) {
	if loginEnv == "" {
		return "", nil
	}
	normalized := strings.ToLower(loginEnv)
	if _, ok := resolveLoginEnv(normalized); !ok {
		return "", clierr.BadInput(fmt.Sprintf(
			"unknown env %q. This binary supports: %s.",
			loginEnv, strings.Join(loginEnvNames(), ", "),
		))
	}
	// --env prod is a no-op (default); don't persist.
	if normalized == "prod" {
		return "", nil
	}
	return normalized, nil
}

func runLogin(cmd *cobra.Command, args []string) error {
	// --env resolves to a URL override + (for non-prod) the env tag we
	// persist on the saved profile. Validates here so all login methods
	// share the same gate.
	saveEnv, err := resolveLoginEnvFlag()
	if err != nil {
		return err
	}

	// Manual flow is selected explicitly via --auth-id (bool) OR by
	// supplying creds inline (--auth-token-stdin, or older scripts that
	// pre-populated loginAuthID via the now-deprecated string flag).
	// --email triggers the dev-only email/password flow registered by
	// the internal build; out-of-the-public-build callers see a clear
	// error from runLoginEmailDispatch.
	if loginEmail {
		return runLoginEmailDispatch(saveEnv)
	}
	if loginAuthIDFlag || loginAuthID != "" || loginAuthTokenStdin {
		return runLoginManual(saveEnv)
	}
	// Default for everyone else: open the browser. PKCE is the recommended
	// path and now the implicit one — no flag required, no menu.
	return runLoginBrowser(saveEnv)
}

// runLoginEmailDispatch is overridden by cmd/login_email_internal.go in
// internal builds to call the real /v1/accounts/login-cli flow. Public
// builds hit the stub and get a clear "internal-only" error.
var runLoginEmailDispatch = func(saveEnv string) error {
	return clierr.BadInput("--email login isn't available in this build (internal-only flow)")
}

// runLoginManual is the auth_id + auth_token paste flow.
func runLoginManual(saveEnv string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	authID, err := readAuthID()
	if err != nil {
		return err
	}
	authToken, err := readAuthToken(loginAuthTokenStdin)
	if err != nil {
		return err
	}

	// Email helps per-user analytics inside an org. Skip-able (Enter to
	// leave blank) — auth_id+token alone can't resolve which human owns
	// the row, so we ask the user. No identity verification.
	email, err := promptOptional(os.Stdin, os.Stderr, "Email (optional, for analytics — press Enter to skip): ")
	if err != nil {
		return err
	}

	// Validate against the live API unless explicitly skipped. This catches
	// typos and bad tokens immediately rather than failing on the user's
	// next real command — and gives us the account display name for the
	// "Logged in as ..." line.
	accountName := ""
	if !loginNoVerify {
		client := api.New(authID, authToken, 30*time.Second)
		var acct api.Account
		apiErr, gerr := client.Do("GET", client.AccountURL(), nil, nil, &acct)
		if gerr != nil {
			return clierr.NetworkError("validating credentials", gerr)
		}
		if apiErr != nil {
			// 401 / 403 / etc. — surface unchanged so the user sees the
			// real reason (wrong auth_id, expired token, …).
			return apiErr
		}
		accountName = acct.Name
	}

	return persistProfile(cfg, loginName, loginBundle{
		AuthID:    authID,
		AuthToken: authToken,
		Email:     email,
		Name:      accountName,
		SaveEnv:   saveEnv,
		// AomUUID + Region intentionally empty — manual flow can't resolve
		// either from just the auth_id/token pair.
	})
}

// promptOptional prints prompt to out, reads one line from in, returns
// the trimmed value. Empty input returns "" without error.
func promptOptional(in *os.File, out *os.File, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// loginBundle is the full set of fields a login flow hands to persistProfile.
// Lets new flows extend the data set (Email, Name, AomUUID, Region) without
// reworking the signature each time.
type loginBundle struct {
	AuthID    string
	AuthToken string
	Email     string // human email — sent as X-Plivo-CLI-Email per request
	Name      string // display name shown in "Logged in as …"
	AomUUID   string // account-org-member UUID; ties human identity to this org
	Region    string // resolved login region (use1 / aps1 / …)
	SaveEnv   string // non-prod env tag persisted on the profile; empty = prod
}

// persistProfile writes the credential bundle to config.toml + keychain.
// Shared by every login method (manual, browser, email/password).
func persistProfile(cfg *config.Config, profileName string, b loginBundle) error {
	prof := config.Profile{
		AuthID:  b.AuthID,
		Email:   b.Email,
		Name:    b.Name,
		AomUUID: b.AomUUID,
		Region:  b.Region,
		Env:     b.SaveEnv,
	}
	storedInKeychain := true
	if err := config.SetToken(profileName, b.AuthToken); err != nil {
		// Headless Linux without Secret Service, or other keychain miss.
		// Fall back to storing the token inline in config.toml (file is 0600).
		prof.AuthToken = b.AuthToken
		storedInKeychain = false
	}
	cfg.Profiles[profileName] = prof
	if cfg.Active == "" {
		cfg.Active = profileName
	}
	if err := config.Save(cfg); err != nil {
		return err
	}

	cfgPath, _ := config.Path()
	switch {
	case b.Name != "" && b.Email != "":
		fmt.Fprintf(os.Stderr, "✓ Logged in as %s <%s> (%s)\n", b.Name, b.Email, b.AuthID)
	case b.Name != "":
		fmt.Fprintf(os.Stderr, "✓ Logged in as %s (%s)\n", b.Name, b.AuthID)
	default:
		fmt.Fprintf(os.Stderr, "✓ Saved profile %q (%s)\n", profileName, b.AuthID)
	}
	if storedInKeychain {
		fmt.Fprintf(os.Stderr, "  Profile: %s\n  Token:   OS keychain\n", cfgPath)
	} else {
		fmt.Fprintf(os.Stderr, "  Profile: %s\n  Token:   inline in config (OS keychain unavailable)\n", cfgPath)
	}
	return nil
}

func runLogout(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	name := ""
	if len(args) > 0 {
		name = args[0]
	} else {
		name = cfg.Active
		if name == "" {
			return clierr.BadInput("no active profile to log out — pass a profile name")
		}
	}
	if _, ok := cfg.Profiles[name]; !ok {
		return clierr.BadInput(fmt.Sprintf("profile %q not found", name))
	}

	// Best-effort keychain removal (no-op if the token lived inline).
	_ = config.DeleteToken(name)
	delete(cfg.Profiles, name)
	if cfg.Active == name {
		cfg.Active = ""
	}
	if err := config.Save(cfg); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "✓ Logged out profile %q.\n", name)
	if cfg.Active == "" && len(cfg.Profiles) > 0 {
		fmt.Fprintln(os.Stderr, "  No active profile — pick one with `plivo auth use <name>`.")
	}
	return nil
}

// readAuthID returns the auth_id from --auth-id or a visible stdin prompt.
func readAuthID() (string, error) {
	if v := strings.TrimSpace(loginAuthID); v != "" {
		return v, nil
	}
	fmt.Fprint(os.Stderr, "auth_id: ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read auth_id: %w", err)
	}
	authID := strings.TrimSpace(line)
	if authID == "" {
		return "", clierr.BadInput("auth_id is required")
	}
	return authID, nil
}

// readAuthToken returns the auth_token from --auth-token-stdin (piped) or a
// hidden interactive prompt. We deliberately don't expose a --auth-token
// flag — passing a secret on the command line leaks it into shell history
// and `ps`.
func readAuthToken(fromStdin bool) (string, error) {
	if fromStdin {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read auth_token from stdin: %w", err)
		}
		authToken := strings.TrimSpace(string(b))
		if authToken == "" {
			return "", clierr.BadInput("auth_token (stdin) was empty")
		}
		return authToken, nil
	}
	fmt.Fprint(os.Stderr, "auth_token: ")
	tokBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", fmt.Errorf("read auth_token: %w", err)
	}
	fmt.Fprintln(os.Stderr)
	authToken := strings.TrimSpace(string(tokBytes))
	if authToken == "" {
		return "", clierr.BadInput("auth_token is required")
	}
	return authToken, nil
}
