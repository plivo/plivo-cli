package cmd

import (
	"fmt"
	"os"
	"strings"

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

Multiple organizations:
  Logging in again for a different org saves a second profile (named
  after that org) instead of overwriting the first. Run ` + "`plivo auth list`" + `
  to see every saved profile, and ` + "`plivo auth use <name>`" + ` to switch. Pass
  -n/--name to choose the profile name yourself.

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
		"profile name to save under; auto-derived from the org when omitted")
	loginCmd.Flags().BoolVar(&loginNoVerify, "no-verify", false,
		"skip the GET /Account/ validation hit (offline / mock use only)")

	rootCmd.AddCommand(loginCmd, logoutCmd)
}

func runLogin(cmd *cobra.Command, args []string) error {
	return runLoginBrowser(cmd.Flags().Changed("name"))
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
	OrgName   string // org display name; drives profile auto-naming + status lines. May be empty (older auth server).
	OrgUUID   string // org's stable identifier, when the auth server sends one. Informational only today.
}

// persistProfile writes the credential bundle to config.toml + keychain.
// Shared by every login method (today only browser; kept generic).
//
// requestedName is the -n/--name flag value; nameExplicit reports whether
// the caller actually passed -n (cmd.Flags().Changed("name")) rather than
// leaving it at its "default" zero value. When not explicit, the profile
// is named after the org instead (slugifyOrgName), falling back to
// "default" when the org name is missing or slugifies to empty.
//
// Never silently overwrites a different org's credentials under a name:
// re-authorizing the SAME org (same auth_id) updates that profile in
// place — the normal token-refresh path. Landing on a name that already
// belongs to a DIFFERENT org either refuses (explicit -n — it's the
// user's naming choice, so we ask rather than guess) or auto-suffixes
// (auto-derived name — the CLI picked the name, so it can also pick a
// free one without bothering the user).
func persistProfile(cfg *config.Config, requestedName string, nameExplicit bool, b loginBundle) error {
	profileName := requestedName
	if !nameExplicit {
		if slug := slugifyOrgName(b.OrgName); slug != "" {
			profileName = slug
		}
	}

	if existing, ok := cfg.Profiles[profileName]; ok && existing.AuthID != "" && existing.AuthID != b.AuthID {
		if nameExplicit {
			return &clierr.Error{
				Code:    clierr.CodeResourceConflict,
				Message: fmt.Sprintf("profile %q already holds different credentials (%s)", profileName, describeProfile(existing)),
				Hint:    fmt.Sprintf("Log in with a different name (-n/--name), or replace it first with `plivo logout %s`.", profileName),
				Context: map[string]any{"profile": profileName},
			}
		}
		profileName = nextAvailableProfileName(cfg, profileName, b.AuthID)
	}

	prof := config.Profile{
		AuthID:  b.AuthID,
		Email:   b.Email,
		Name:    b.Name,
		AomUUID: b.AomUUID,
		Region:  b.Region,
		Env:     b.SaveEnv,
		OrgName: b.OrgName,
		OrgUUID: b.OrgUUID,
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
	fmt.Fprintf(os.Stderr, "✓ %s\n", loginConfirmationLine(profileName, b))
	if storedInKeychain {
		fmt.Fprintf(os.Stderr, "  Profile: %s\n  Token:   OS keychain\n", cfgPath)
	} else {
		fmt.Fprintf(os.Stderr, "  Profile: %s\n  Token:   inline in config (OS keychain unavailable)\n", cfgPath)
	}
	return nil
}

// loginConfirmationLine renders the one-line "who/what got saved where"
// summary. Always names the profile — a second `plivo login` may no
// longer land on "default" — and the auth_id; human name/email and the
// org name are layered in when the auth server sent them.
func loginConfirmationLine(profileName string, b loginBundle) string {
	var who string
	switch {
	case b.Name != "" && b.Email != "":
		who = fmt.Sprintf("Logged in as %s <%s>, profile %q", b.Name, b.Email, profileName)
	case b.Name != "":
		who = fmt.Sprintf("Logged in as %s, profile %q", b.Name, profileName)
	default:
		who = fmt.Sprintf("Saved profile %q", profileName)
	}
	if b.OrgName != "" {
		who += " — " + b.OrgName
	}
	return fmt.Sprintf("%s (%s)", who, b.AuthID)
}

// describeProfile renders a short label for an existing profile in
// conflict messages: the org name when known, else just the auth_id.
func describeProfile(p config.Profile) string {
	if p.OrgName != "" {
		return fmt.Sprintf("%s, %s", p.OrgName, p.AuthID)
	}
	return p.AuthID
}

// nextAvailableProfileName finds a free slot starting at base by trying
// base, base-2, base-3, … A candidate counts as free when no profile owns
// it yet, or when the profile that owns it is already this same org
// (authID). That second case is what makes a re-login idempotent for an
// org that landed on a suffixed name last time: the base slug is
// recomputed from scratch on every login, so it'll find base still taken
// by whoever squats it and walk the same suffixes again — this check
// stops the walk at the org's own slot (e.g. "acme-2") instead of piling
// up "acme-3", "acme-4", … on every refresh.
func nextAvailableProfileName(cfg *config.Config, base, authID string) string {
	if p, ok := cfg.Profiles[base]; !ok || p.AuthID == "" || p.AuthID == authID {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if p, ok := cfg.Profiles[candidate]; !ok || p.AuthID == "" || p.AuthID == authID {
			return candidate
		}
	}
}

// slugifyOrgName turns an organization display name into a profile-name-
// safe slug: lowercase, any run of characters outside [a-z0-9] collapses
// to a single hyphen, leading/trailing hyphens are trimmed, and the
// result is capped at 40 characters. Returns "" for an empty or
// punctuation-only input — callers fall back to "default" in that case.
func slugifyOrgName(name string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if b.Len() > 0 && !prevHyphen {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	slug := strings.TrimRight(b.String(), "-")
	const maxLen = 40
	if len(slug) > maxLen {
		slug = strings.TrimRight(slug[:maxLen], "-")
	}
	return slug
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
