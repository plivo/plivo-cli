package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// authCmd hosts the profile-management subcommands (list / use / remove /
// whoami). The login/logout verbs are top-level (`plivo login` /
// `plivo logout`); see cmd/login.go.
var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage credential profiles (list / use / remove / whoami)",
	Args:  cobra.NoArgs,
	RunE:  groupRunE,
}

var authListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured profiles",
	Args:  cobra.NoArgs,
	RunE:  runAuthList,
}

var authUseCmd = &cobra.Command{
	Use:   "use <profile|org>",
	Short: "Set the active profile (by profile name, organization name or auth ID)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAuthUse,
}

var authRemoveCmd = &cobra.Command{
	Use:   "remove <profile>",
	Short: "Remove a profile (use `plivo logout` for the active profile)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAuthRemove,
}

var authWhoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Verify credentials and print active account",
	Args:  cobra.NoArgs,
	RunE:  runAuthWhoami,
}

func init() {
	registerExplainFlag(authWhoamiCmd)
	authCmd.AddCommand(authListCmd, authUseCmd, authRemoveCmd, authWhoamiCmd)
	rootCmd.AddCommand(authCmd)
}

func runAuthList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if effectiveFormat() == output.FormatJSON {
		type item struct {
			Name    string `json:"name"`
			AuthID  string `json:"auth_id"`
			OrgName string `json:"org_name"`
			Active  bool   `json:"active"`
		}
		items := []item{}
		for _, name := range sortedProfileNames(cfg) {
			p := cfg.Profiles[name]
			items = append(items, item{Name: name, AuthID: p.AuthID, OrgName: p.OrgName, Active: name == cfg.Active})
		}
		return output.JSONSuccess(os.Stdout, items, nil)
	}
	rows := [][]string{{"ACTIVE", "NAME", "ORG", "AUTH_ID"}}
	for _, name := range sortedProfileNames(cfg) {
		p := cfg.Profiles[name]
		active := ""
		if name == cfg.Active {
			active = "*"
		}
		rows = append(rows, []string{active, name, p.OrgName, p.AuthID})
	}
	if len(rows) == 1 {
		fmt.Fprintln(os.Stderr, "no profiles configured. Run `plivo login`.")
		return nil
	}
	return output.Table(os.Stdout, rows)
}

func runAuthUse(cmd *cobra.Command, args []string) error {
	var name, org string
	if err := updateConfig(func(cfg *config.Config) error {
		var err error
		if name, err = pickProfile(cfg, args[0]); err != nil {
			return err
		}
		cfg.Active = name
		org = cfg.Profiles[name].OrgName
		return nil
	}); err != nil {
		return err
	}
	if org != "" {
		org = " (" + org + ")"
	}
	if dryRunFlag {
		fmt.Fprintf(os.Stderr, "[dry-run] Would set the active profile to %s%s\n", name, org)
		return nil
	}
	fmt.Fprintf(os.Stderr, "Active profile: %s%s\n", name, org)
	return nil
}

// sortedProfileNames lists the profile names in a fixed order; ranging over the
// map gave a different order on every run.
func sortedProfileNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// matchProfiles returns the profiles arg selects: the one named arg, else every
// profile with auth ID arg, else every one whose organization name is arg
// (ignoring case). Sorted, so a list of candidates reads the same every time.
func matchProfiles(cfg *config.Config, arg string) []string {
	if _, ok := cfg.Profiles[arg]; ok {
		return []string{arg}
	}
	var byID, byOrg []string
	for _, name := range sortedProfileNames(cfg) {
		p := cfg.Profiles[name]
		switch {
		case p.AuthID != "" && p.AuthID == arg:
			byID = append(byID, name)
		case p.OrgName != "" && strings.EqualFold(p.OrgName, arg):
			byOrg = append(byOrg, name)
		}
	}
	if len(byID) > 0 {
		return byID
	}
	return byOrg
}

// pickProfile resolves arg to exactly one profile. It never guesses: no match
// and several matches are both errors, the second naming each candidate.
func pickProfile(cfg *config.Config, arg string) (string, error) {
	names := matchProfiles(cfg, arg)
	switch len(names) {
	case 0:
		e := clierr.BadInput(fmt.Sprintf("profile %q not found", arg))
		e.Hint = "Run `plivo auth list` to see the saved profiles; pass a profile name, an organization name or an auth ID."
		return "", e
	case 1:
		return names[0], nil
	}
	lines := make([]string, len(names))
	for i, n := range names {
		lines[i] = n + " (" + describeProfile(cfg.Profiles[n]) + ")"
	}
	e := clierr.BadInput(fmt.Sprintf("%q matches %d profiles: %s", arg, len(names), strings.Join(lines, "; ")))
	e.Hint = "Pass the profile name instead."
	return "", e
}

// alsoKeychain names, for a dry run, the keychain token removing profile name
// deletes too: only when the keychain holds one, not a token kept in the config.
func alsoKeychain(name string) string {
	if tok, err := config.GetToken(name); err != nil || tok == "" {
		return ""
	}
	return " and delete its keychain token"
}

func runAuthRemove(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := updateConfig(func(cfg *config.Config) error {
		if _, ok := cfg.Profiles[name]; !ok {
			return fmt.Errorf("profile %q not found", name)
		}
		delete(cfg.Profiles, name)
		if cfg.Active == name {
			cfg.Active = ""
		}
		return nil
	}); err != nil {
		return err
	}
	if dryRunFlag {
		fmt.Fprintf(os.Stderr, "[dry-run] Would remove profile %q%s\n", name, alsoKeychain(name))
		return nil
	}
	// Best-effort removal from the OS keychain (no-op if the token lived in
	// the config file instead), once the profile is gone.
	_ = config.DeleteToken(name)
	fmt.Fprintf(os.Stderr, "Removed profile %q\n", name)
	return nil
}

func runAuthWhoami(cmd *cobra.Command, args []string) error {
	client, source, err := getClient()
	if err != nil {
		return err
	}
	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will GET %s to verify credentials\n", client.AccountURL())
	}
	var acct api.Account
	apiErr, err := client.Do("GET", client.AccountURL(), nil, nil, &acct)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		// source is local (which profile or env supplied the creds), so it
		// stays in meta while data carries the upstream body verbatim.
		return output.JSONSuccess(os.Stdout, acct.Raw(), map[string]string{"source": source})
	}
	return output.KV(os.Stdout, [][2]string{
		{"name", acct.Name},
		{"auth id", acct.AuthID},
		{"credits", acct.CashCredits},
		{"plan", acct.AccountType},
	})
}
