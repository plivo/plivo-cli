package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/spf13/cobra"
)

// login flags — kept intentionally small. Browser PKCE is the only way a
// credential enters the CLI.
var (
	loginName     string
	loginNoVerify bool
)

// loginCmd opens the default browser, runs the PKCE loopback OAuth
// handshake against Plivo, and saves the resulting credentials.
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to Plivo (browser-based; keeps a 30-day sign-in in the OS keychain, no account key)",
	Long: `Log in to Plivo via the default browser and save a profile for
subsequent commands.

The flow:
  1. The CLI opens your default browser to the Plivo Console.
  2. You sign in (the password never touches the CLI).
  3. The Console hands the CLI a short-lived authorization code which is
     exchanged for a 30-day sign-in over a local loopback listener (PKCE:
     the sign-in leaves the browser only over this transient pipe).
  4. The sign-in is stored in your OS keychain (macOS Keychain, Windows
     Credential Manager, Linux Secret Service). The CLI falls back to
     writing it inline in ~/.plivo/config.toml (chmod 0600) only when no
     keychain is available.

No account key (auth_token) is stored: commands run on the sign-in, which
the CLI sends only to the server that issued it. It lasts 30 days, then
every profile that runs on it stops until you run ` + "`plivo login`" + ` again. That
includes scripts and CI: they must log in again on that machine every 30
days, as the CLI has no environment variable, flag or paste route for a key.

Multiple organizations:
  If you belong to several organizations, the approval page lists them and
  you tick the ones this machine may use. ` + "`plivo org use <name>`" + ` then switches
  between them without a browser (see ` + "`plivo org --help`" + `). Logging in again
  replaces the sign-in, and revokes the old one on the server (best effort;
  the command says whether it was ended), so it is also how you add or remove
  organizations: a profile for an organization the new sign-in does not cover
  is refused. Each login saves a profile named after the organization (pass
  -n/--name to choose the name); ` + "`plivo auth list`" + ` shows every saved
  profile and ` + "`plivo auth use <name>`" + ` switches between them.

A profile saved by an earlier version holds a key and keeps using it. Logging
in as that organization again, or ` + "`plivo org use`" + ` for it, removes its stored key
after the new sign-in is saved; the other profiles are never touched.

Browser can't reach this terminal:
  When the CLI runs in a container, WSL, a VM or over SSH, the page the
  browser opens after you approve may fail to load. Copy the full URL from
  its address bar and paste it into the terminal; the CLI accepts only
  this login's URL.

Headless / CI use:
  Not supported beyond that: browser OAuth (PKCE) is the only credential
  source, so scripts and CI need a profile that was logged in on that machine
  within the last 30 days.

Inside a coding agent:
  Run inside Claude Code, Cursor or Codex, a login asks once per machine
  whether to install the Plivo skills where that agent reads them, unless
  current copies are there already. Without a terminal to ask on, and in
  each other agent once it has asked, it prints that agent's install
  command instead, once. --yes installs them without asking, and --quiet
  skips the offer.`,
	Example: `  plivo login
  plivo login --name staging`,
	Args: cobra.NoArgs,
	RunE: runLogin,
}

// logoutCmd removes a profile and any key it holds.
var logoutCmd = &cobra.Command{
	Use:   "logout [profile]",
	Short: "Log out a profile (deletes its entry + removes any auth_token from the OS keychain)",
	Long: `Log out a profile. With no argument, logs out the active profile.

Removes the profile row from ~/.plivo/config.toml and best-effort deletes
any auth_token it holds from the OS keychain. The 30-day sign-in the
other profiles run on goes with the last profile, and is revoked on the
server then (best effort; the command says whether it was ended). If the
active profile was logged out, no profile becomes active until you
` + "`plivo auth use <name>`" + ` (or log in again).`,
	Example: `  plivo logout              # active profile
  plivo logout staging      # named profile`,
	Args: cobra.MaximumNArgs(1),
	RunE: runLogout,
}

func init() {
	loginCmd.Flags().StringVarP(&loginName, "name", "n", "default",
		"profile name to save under; auto-derived from the org when omitted")
	loginCmd.Flags().BoolVar(&loginNoVerify, "no-verify", false,
		"no effect; accepted so existing scripts keep working")

	rootCmd.AddCommand(loginCmd, logoutCmd)
}

func runLogin(cmd *cobra.Command, args []string) error {
	if dryRunFlag {
		path, _ := config.Path()
		profile := "a profile named after the organization"
		if cmd.Flags().Changed("name") {
			profile = fmt.Sprintf("profile %q", loginName)
		}
		fmt.Fprintf(os.Stderr, "[dry-run] Would open the browser to log in, then save %s in %s\n", profile, path)
		return nil
	}
	if err := runLoginBrowser(cmd.Flags().Changed("name")); err != nil {
		return err
	}
	offerSkills()
	return nil
}

// loginBundle is the full set of fields a login flow hands to persistProfile.
// Lets new flows extend the data set without reworking the signature each
// time. SaveEnv stays on the struct (zero value = prod) so the schema is
// forward-compatible if env switching returns.
type loginBundle struct {
	AuthID        string
	Email         string // human email — sent as X-Plivo-CLI-Email per request
	Name          string // display name shown in "Logged in as …"
	AomUUID       string // account-org-member UUID; ties human identity to this org
	Region        string // resolved login region (use1 / aps1 / …)
	SaveEnv       string // non-prod env tag persisted on the profile; empty = prod
	OrgName       string // org display name; drives profile auto-naming + status lines. May be empty (older auth server).
	OrgUUID       string // org's stable identifier, when the auth server sends one. Informational only today.
	SessionServer string // the server that issued the sign-in the profile runs on
}

// persistProfile adds the profile to cfg, a keyless one: it names the sign-in
// it runs on and holds no key. It returns the name it picked and where the
// profile it replaced held a key, which the caller deletes with removeKey once
// cfg is saved. Shared by every login method (today only browser; kept
// generic). The caller saves cfg under the config lock, so the name is picked
// from the config as it is saved.
//
// requestedName is the -n/--name flag value; nameExplicit reports whether
// the caller actually passed -n (cmd.Flags().Changed("name")) rather than
// leaving it at its "default" zero value. When not explicit, the profile
// is named after the org instead (slugifyOrgName), falling back to
// "default" when the org name is missing or slugifies to empty.
//
// Never silently overwrites a different org's credentials under a name:
// re-authorizing the SAME org (same auth_id) updates that profile in
// place — the normal refresh path. Landing on a name that already
// belongs to a DIFFERENT org either refuses (explicit -n — it's the
// user's naming choice, so we ask rather than guess) or auto-suffixes
// (auto-derived name — the CLI picked the name, so it can also pick a
// free one without bothering the user).
func persistProfile(cfg *config.Config, requestedName string, nameExplicit bool, b loginBundle) (string, keyPlace, error) {
	profileName := requestedName
	if !nameExplicit {
		if slug := slugifyOrgName(b.OrgName); slug != "" {
			profileName = slug
		}
	}

	if existing, ok := cfg.Profiles[profileName]; ok && existing.AuthID != "" && existing.AuthID != b.AuthID {
		if nameExplicit {
			return "", noKey, &clierr.Error{
				Code:    clierr.CodeResourceConflict,
				Message: fmt.Sprintf("profile %q already holds different credentials (%s)", profileName, describeProfile(existing)),
				Hint:    fmt.Sprintf("Log in with a different name (-n/--name), or replace it first with `plivo logout %s`.", profileName),
				Context: map[string]any{"profile": profileName},
			}
		}
		profileName = nextAvailableProfileName(cfg, profileName, b.AuthID)
	}

	had := savedKey(profileName, cfg.Profiles[profileName])
	cfg.Profiles[profileName] = config.Profile{
		AuthID:        b.AuthID,
		Email:         b.Email,
		Name:          b.Name,
		AomUUID:       b.AomUUID,
		Region:        b.Region,
		Env:           b.SaveEnv,
		OrgName:       b.OrgName,
		OrgUUID:       b.OrgUUID,
		SessionServer: b.SessionServer,
	}
	if cfg.Active == "" {
		cfg.Active = profileName
	}
	return profileName, had, nil
}

// keyPlace is where a profile, as saved, holds a raw key.
type keyPlace int

const (
	noKey       keyPlace = iota
	inlineKey            // in config.toml: the save that converts the profile removes it
	keychainKey          // in the OS keychain: removeKey deletes it after that save
)

// savedKey says where profile name, as saved, holds a raw key.
func savedKey(name string, p config.Profile) keyPlace {
	if p.AuthToken != "" {
		return inlineKey
	}
	if tok, err := config.GetToken(name); err == nil && tok != "" {
		return keychainKey
	}
	return noKey
}

// holdsKey reports whether profile name, as saved, holds a raw key: inline in
// the config or in the OS keychain.
func holdsKey(name string, p config.Profile) bool { return savedKey(name, p) != noKey }

// keyHolders names the profiles other than except that hold a raw key.
func keyHolders(cfg *config.Config, except string) []string {
	var names []string
	for _, name := range sortedProfileNames(cfg) {
		if name != except && holdsKey(name, cfg.Profiles[name]) {
			names = append(names, name)
		}
	}
	return names
}

// removeKey deletes the key a login or `org use` replaced, once the new profile
// is saved: the profile now runs on the sign-in. Never before the save, which
// could still fail and leave the old profile with no key. An inline key went
// with the save, so the keychain is cleared only in case a stale copy is there,
// and a machine with no keychain is not reported.
func removeKey(name string, had keyPlace) {
	if had == noKey {
		return
	}
	if err := config.DeleteToken(name); err != nil && had == keychainKey {
		fmt.Fprintf(os.Stderr, "  Profile %q now uses the 30-day sign-in. Its old key is no longer used but could not be removed from the OS keychain (%v): delete the plivo-cli entry %q there.\n", name, err, name)
		return
	}
	fmt.Fprintf(os.Stderr, "  Profile %q: its stored key was removed; it now uses the 30-day sign-in.\n", name)
}

// printProfileSaved confirms which profile a login saved, and where its sign-in
// is.
func printProfileSaved(profileName string, inKeychain bool, expires time.Time, b loginBundle) {
	cfgPath, _ := config.Path()
	if printBanner(os.Stderr) {
		printWelcome(os.Stderr, b.Name, b.Email, b.OrgName)
	}
	fmt.Fprintf(os.Stderr, "✓ %s\n", loginConfirmationLine(profileName, b))
	where := "OS keychain"
	if !inKeychain {
		where = "inline in config (OS keychain unavailable)"
	}
	fmt.Fprintf(os.Stderr, "  Profile: %s\n  Sign-in: %s, valid until %s\n", cfgPath, where, expires.Local().Format("2006-01-02"))
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
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	noneActive := false
	var dropped, kept *config.UserSession
	if err := updateConfig(func(cfg *config.Config) error {
		if len(args) == 0 {
			if name = cfg.Active; name == "" {
				return clierr.BadInput("no active profile to log out — pass a profile name")
			}
		}
		if _, ok := cfg.Profiles[name]; !ok {
			return clierr.BadInput(fmt.Sprintf("profile %q not found", name))
		}
		delete(cfg.Profiles, name)
		if cfg.Active == name {
			cfg.Active = ""
		}
		noneActive = cfg.Active == "" && len(cfg.Profiles) > 0
		dropped = dropSessionWithLastProfile(cfg)
		kept = cfg.User
		return nil
	}); err != nil {
		return err
	}
	if dryRunFlag {
		fmt.Fprintf(os.Stderr, "[dry-run] Would log out profile %q%s%s\n", name, alsoKeychain(name), alsoSignIn(dropped))
		return nil
	}

	// Best-effort keychain removal (no-op if the token lived inline), once
	// the profile is gone.
	_ = config.DeleteToken(name)
	fmt.Fprintf(os.Stderr, "✓ Logged out profile %q.\n", name)
	reportDroppedSession(dropped)
	reportKeptSession(kept)
	if noneActive {
		fmt.Fprintln(os.Stderr, "  No active profile — pick one with `plivo auth use <name>`.")
	}
	return nil
}
