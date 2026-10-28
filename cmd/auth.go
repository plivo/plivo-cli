package cmd

import (
	"fmt"
	"os"

	"github.com/plivo/plivo-cli/internal/api"
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
	Use:   "use <profile>",
	Short: "Set the active profile",
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
		for name, p := range cfg.Profiles {
			items = append(items, item{Name: name, AuthID: p.AuthID, OrgName: p.OrgName, Active: name == cfg.Active})
		}
		return output.JSONSuccess(os.Stdout, items, nil)
	}
	rows := [][]string{{"ACTIVE", "NAME", "ORG", "AUTH_ID"}}
	for name, p := range cfg.Profiles {
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
	name := args[0]
	if err := updateConfig(func(cfg *config.Config) error {
		if _, ok := cfg.Profiles[name]; !ok {
			return fmt.Errorf("profile %q not found", name)
		}
		cfg.Active = name
		return nil
	}); err != nil {
		return err
	}
	if dryRunFlag {
		fmt.Fprintf(os.Stderr, "[dry-run] Would set the active profile to %s\n", name)
		return nil
	}
	fmt.Fprintf(os.Stderr, "Active profile: %s\n", name)
	return nil
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
