package cmd

import (
	"fmt"
	"os"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/spf13/cobra"
)

// login flags — kept intentionally small. Browser PKCE is the only
// supported login method on main; headless callers (CI, agents) set
// PLIVO_AUTH_ID + PLIVO_AUTH_TOKEN env vars instead of running
// `plivo login`.
var (
	loginName     string
	loginNoVerify bool
)

// loginCmd opens the default browser, runs the PKCE loopback OAuth
// handshake against Plivo, and saves the resulting credentials.
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to Plivo (browser-based; saves credentials securely to the OS keychain)",
	Long: `Log in to Plivo via the default browser and save credentials for
subsequent commands.

The flow:
  1. The CLI opens your default browser to the Plivo Console.
  2. You sign in (the password never touches the CLI).
  3. The Console hands the CLI a short-lived authorization code which is
     exchanged for your account's auth_id and auth_token over a local
     loopback listener (PKCE — the token leaves the browser only over
     this transient pipe).
  4. The auth_token is stored in your OS keychain (macOS Keychain,
     Windows Credential Manager, Linux Secret Service). The CLI falls
     back to writing it inline in ~/.plivo/config.toml (chmod 0600) only
     when no keychain is available.

Headless / CI use:
  Set PLIVO_AUTH_ID + PLIVO_AUTH_TOKEN environment variables and skip
  ` + "`plivo login`" + ` entirely — every command picks creds up from the env.

By default the CLI validates credentials with GET /Account/ before
saving. Pass --no-verify to skip (offline / mock use only).`,
	Example: `  plivo login
  plivo login --name staging`,
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
	loginCmd.Flags().StringVarP(&loginName, "name", "n", "default",
		"profile name to save under")
	loginCmd.Flags().BoolVar(&loginNoVerify, "no-verify", false,
		"skip the GET /Account/ validation hit (offline / mock use only)")

	rootCmd.AddCommand(loginCmd, logoutCmd)
}

func runLogin(cmd *cobra.Command, args []string) error {
	return runLoginBrowser()
}

// loginBundle is the full set of fields a login flow hands to persistProfile.
// Lets new flows extend the data set without reworking the signature each
// time. SaveEnv stays on the struct (zero value = prod) so the schema is
// forward-compatible if env switching returns.
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
// Shared by every login method (today only browser; kept generic).
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
