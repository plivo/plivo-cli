package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var orgCmd = &cobra.Command{
	Use:   "org",
	Short: "List and switch between your organizations (list / use / current)",
	Long: `Switch between the organizations you approved in one browser login.

` + "`plivo login`" + ` shows every organization you belong to and lets you tick the ones
this machine may use. ` + "`plivo org use <name>`" + ` then saves one of them as a profile
and makes it active, so every other command runs as that organization, on
the 30-day sign-in the login left (no account key is stored). To add or
remove organizations later, run ` + "`plivo login`" + ` again.`,
	Args: cobra.NoArgs,
	RunE: groupRunE,
}

var orgListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the organizations your login approved",
	Args:  cobra.NoArgs,
	RunE:  runOrgList,
}

var orgUseCmd = &cobra.Command{
	Use:   "use <org>",
	Short: "Switch to an organization (by name or auth ID)",
	Long: `Make an approved organization the active one. It is saved as a profile that
runs on the 30-day sign-in, with no key stored (an existing profile for it is
updated in place, and a key it held is removed), and set active. If the
organization is already saved and the sign-in cannot be used, the saved
profile is activated instead and the command says so.`,
	Example: `  plivo org use "Acme Inc"
  plivo org use <auth id>`,
	Args: cobra.ExactArgs(1),
	RunE: runOrgUse,
}

var orgCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the organization commands run as",
	Args:  cobra.NoArgs,
	RunE:  runOrgCurrent,
}

func init() {
	orgCmd.AddCommand(orgListCmd, orgUseCmd, orgCurrentCmd)
	rootCmd.AddCommand(orgCmd)
}

// profileForAuthID names the first saved profile (in name order) holding authID.
func profileForAuthID(cfg *config.Config, authID string) string {
	for _, name := range sortedProfileNames(cfg) {
		if cfg.Profiles[name].AuthID == authID {
			return name
		}
	}
	return ""
}

func runOrgList(cmd *cobra.Command, args []string) error {
	if dryRunFlag {
		fmt.Fprintln(os.Stderr, "[dry-run] Would list the organizations your login approved")
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client, err := openSession(cfg)
	if err != nil {
		return err
	}
	orgs, err := fetchOrgs(client)
	if err != nil {
		return err
	}
	type item struct {
		OrgName string `json:"org_name"`
		AuthID  string `json:"auth_id"`
		Region  string `json:"region"`
		Profile string `json:"profile"`
		Active  bool   `json:"active"`
	}
	inUse := cfg.Active // as `org current`, --profile names the one in use instead
	if profileFlag != "" {
		inUse = profileFlag
	}
	items := []item{}
	for _, o := range orgs {
		prof, active := profileForAuthID(cfg, o.AuthID), inUse != "" && cfg.Profiles[inUse].AuthID == o.AuthID
		if active {
			prof = inUse // two profiles can hold one org: show the one in use
		}
		items = append(items, item{o.OrgName, o.AuthID, o.Region, prof, active})
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, items, nil)
	}
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "your login approved no organizations. Run `plivo login` and tick some.")
		return nil
	}
	rows := [][]string{{"ACTIVE", "ORG", "AUTH_ID", "REGION", "PROFILE"}}
	for _, it := range items {
		active := ""
		if it.Active {
			active = "*"
		}
		rows = append(rows, []string{active, it.OrgName, it.AuthID, it.Region, it.Profile})
	}
	return output.Table(os.Stdout, rows)
}

// matchOrgs returns the organizations query selects: auth ID first, else name
// (ignoring case).
func matchOrgs(orgs []orgEntry, query string) []orgEntry {
	var byID, byName []orgEntry
	for _, o := range orgs {
		switch {
		case o.AuthID == query:
			byID = append(byID, o)
		case o.OrgName != "" && strings.EqualFold(o.OrgName, query):
			byName = append(byName, o)
		}
	}
	if len(byID) > 0 {
		return byID
	}
	return byName
}

func runOrgUse(cmd *cobra.Command, args []string) error {
	query := args[0]
	if dryRunFlag { // before any lookup or call: nothing is fetched, saved or sent
		fmt.Fprintf(os.Stderr, "[dry-run] Would switch to the organization %q: save it as a profile on your sign-in and make it active\n", query)
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	saved := matchProfiles(cfg, query)
	if len(saved) > 1 {
		_, err := pickProfile(cfg, query) // names every candidate
		return err
	}
	var local string
	if len(saved) == 1 {
		local = saved[0]
	}

	// Without a usable sign-in, a saved organization still switches, as it is.
	client, err := openSession(cfg)
	if err != nil {
		return switchLocally(local, err)
	}
	orgs, err := fetchOrgs(client)
	if err != nil {
		return switchLocally(local, err)
	}
	authID := cfg.Profiles[local].AuthID
	if query != authID {
		// Anything but an exact auth ID can name more than one organization (a
		// profile name can equal another organization's name), so the approved list
		// decides, with the saved profile counted among the candidates.
		ids := map[string]bool{}
		for _, o := range matchOrgs(orgs, query) {
			ids[o.AuthID] = true
		}
		if local != "" {
			ids[authID] = true
		}
		switch len(ids) {
		case 0:
			e := clierr.BadInput(fmt.Sprintf("no organization %q among the ones your login approved", query))
			e.Hint = "Run `plivo org list`. To add one, run `plivo login` and tick it."
			return e
		case 1:
			for id := range ids {
				authID = id
			}
		default:
			e := clierr.BadInput(fmt.Sprintf("%q matches %d organizations", query, len(ids)))
			e.Hint = "Pass the auth ID instead (see `plivo org list`)."
			return e
		}
	}
	for _, o := range orgs {
		if o.AuthID == authID {
			if convertsKey(cfg, o, local) {
				if err := proveOrg(cfg, o); err != nil {
					return err
				}
			}
			return activateOrg(o, local, cfg.User.Server, cfg.User.ExpiresAt)
		}
	}
	// A saved profile the sign-in does not cover: switch nothing.
	e := clierr.OrgNotAllowed()
	e.Message = "your login does not cover that organization"
	e.Hint = "Run `plivo login` and tick it on the approval page."
	if local != "" && cfg.Profiles[local].Credential() == config.CredentialKey { // a keyless one would get this refusal too
		e.Hint += " To use the saved profile as it is, run `plivo auth use " + local + "`."
	}
	return e
}

// switchLocally activates the saved profile local after the sign-in could not
// be used (cause), and says so. A profile that runs on the sign-in fails until
// the next login only when the sign-in itself is gone or expired, not when the
// server could not be reached. With no saved profile there is nothing to switch
// to, so cause is the answer.
func switchLocally(local string, cause error) error {
	if local == "" {
		return cause
	}
	var org, credential string
	if err := updateConfig(func(cfg *config.Config) error {
		cfg.Active = local
		org, credential = cfg.Profiles[local].OrgName, cfg.Profiles[local].Credential()
		return nil
	}); err != nil {
		return err
	}
	if org != "" {
		org = " (" + output.SafeText(org) + ")"
	}
	fmt.Fprintf(os.Stderr, "Active profile: %s%s\n  The sign-in could not be used, so nothing was checked or changed: %s\n", local, org, output.SafeText(cause.Error()))
	var ce *clierr.Error
	if credential == config.CredentialSession && errors.As(cause, &ce) && (ce.Code == clierr.CodeAuthExpired || ce.Code == clierr.CodeAuthMissing) {
		fmt.Fprintln(os.Stderr, "  This profile runs on the sign-in: its commands fail until you run `plivo login`.")
	}
	return nil
}

// convertsKey reports whether switching to o would convert a profile that holds a
// key, the profile activateOrg picks: local, else the one saved for o's auth ID.
func convertsKey(cfg *config.Config, o orgEntry, local string) bool {
	if p, ok := cfg.Profiles[local]; !ok || p.AuthID != o.AuthID {
		local = profileForAuthID(cfg, o.AuthID)
	}
	p, ok := cfg.Profiles[local]
	return ok && p.Credential() == config.CredentialKey
}

// proveOrg makes one request through the sign-in gate as the organization o,
// before a key is removed for it. The approved list is only what was ticked: the
// gate also applies membership, single sign-on and the API credentials rule, and
// a profile that loses its key to a refusal cannot get it back.
func proveOrg(cfg *config.Config, o orgEntry) error {
	base, err := apiBaseURL()
	if err != nil {
		return err
	}
	c := api.New(o.AuthID, "", timeoutFlag)
	c.BaseURL = base
	if err := runOnSession(c, config.Profile{AuthID: o.AuthID, SessionServer: cfg.User.Server}, o.AuthID); err != nil {
		return err
	}
	var acct api.Account
	apiErr, err := c.Do("GET", c.AccountURL(), nil, nil, &acct)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	return nil
}

// activateOrg saves the organization o as a keyless profile on the sign-in and
// makes it active. local names the saved profile the organization was picked by,
// if any: it keeps its name and settings, and a key it held is removed once the
// change is saved. The sign-in o was listed from is the one (server, expires)
// the config must still hold when it saves: a login that replaced it meanwhile
// may not cover o.
func activateOrg(o orgEntry, local, server string, expires time.Time) error {
	var name string
	var hadKey keyPlace
	if err := config.Update(func(cfg *config.Config) error {
		if cfg.User == nil || cfg.User.Server != server || !cfg.User.ExpiresAt.Equal(expires) {
			return errSessionExpired()
		}
		if _, ok := cfg.Profiles[local]; !ok || cfg.Profiles[local].AuthID != o.AuthID {
			local = profileForAuthID(cfg, o.AuthID)
		}
		b := loginBundle{AuthID: o.AuthID, Region: o.Region, OrgName: o.OrgName, OrgUUID: o.OrgUUID, SessionServer: cfg.User.Server}
		requested, explicit := "default", false
		var keep config.Profile
		if local != "" {
			requested, explicit = local, true
			keep = cfg.Profiles[local]
			b.SaveEnv, b.AomUUID = keep.Env, keep.AomUUID
			if b.Region == "" {
				b.Region = keep.Region
			}
		}
		b.Email, b.Name = keep.Email, keep.Name
		if cfg.User.Email != "" {
			b.Email, b.Name = cfg.User.Email, cfg.User.Name
		}
		var err error
		if name, hadKey, err = persistProfile(cfg, requested, explicit, b); err != nil {
			return err
		}
		p := cfg.Profiles[name]
		p.DefaultSubaccount = keep.DefaultSubaccount
		cfg.Profiles[name] = p
		cfg.Active = name
		return nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ Active organization: %s (%s), profile %q\n", output.SafeText(o.OrgName), output.SafeText(o.AuthID), name)
	removeKey(name, hadKey)
	return nil
}

func runOrgCurrent(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	name := cfg.Active
	if profileFlag != "" {
		name = profileFlag
	}
	p, ok := cfg.Profiles[name]
	if !ok {
		if name == "" {
			return clierr.AuthMissing()
		}
		return clierr.BadInput(fmt.Sprintf("profile %q not found", name))
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, map[string]string{
			"profile": name, "org_name": p.OrgName, "auth_id": p.AuthID, "region": p.Region, "credential": p.Credential(),
		}, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"profile", name}, {"org", p.OrgName}, {"auth id", p.AuthID}, {"region", p.Region}, {"credential", p.Credential()},
	})
}
