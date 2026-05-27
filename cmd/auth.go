package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage credentials and profiles",
}

var authLoginName string

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Add or update a credential profile",
	RunE:  runAuthLogin,
}

var authListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured profiles",
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
	Short: "Remove a profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runAuthRemove,
}

var authWhoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Verify credentials and print active account",
	RunE:  runAuthWhoami,
}

func init() {
	authLoginCmd.Flags().StringVarP(&authLoginName, "name", "n", "default", "profile name")
	authCmd.AddCommand(authLoginCmd, authListCmd, authUseCmd, authRemoveCmd, authWhoamiCmd)
	// `auth token` is registered in authToken.go (internal build tag only).
	rootCmd.AddCommand(authCmd)
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)

	fmt.Fprint(os.Stderr, "auth_id: ")
	authID, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	authID = strings.TrimSpace(authID)

	fmt.Fprint(os.Stderr, "auth_token: ")
	tokBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr)
	authToken := strings.TrimSpace(string(tokBytes))

	if authID == "" || authToken == "" {
		return fmt.Errorf("auth_id and auth_token are required")
	}

	prof := config.Profile{AuthID: authID}
	storedInKeychain := true
	if err := config.SetToken(authLoginName, authToken); err != nil {
		// No usable OS keychain (e.g. a headless Linux box with no Secret
		// Service). Fall back to storing the token in config.toml (0600).
		prof.AuthToken = authToken
		storedInKeychain = false
	}
	cfg.Profiles[authLoginName] = prof
	if cfg.Active == "" {
		cfg.Active = authLoginName
	}
	if err := config.Save(cfg); err != nil {
		return err
	}

	path, _ := config.Path()
	if storedInKeychain {
		fmt.Fprintf(os.Stderr, "Saved profile %q to %s (auth token stored in your OS keychain)\n", authLoginName, path)
	} else {
		fmt.Fprintf(os.Stderr, "Saved profile %q to %s (OS keychain unavailable — auth token stored in the config file)\n", authLoginName, path)
	}
	return nil
}

func runAuthList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if effectiveFormat() == output.FormatJSON {
		type item struct {
			Name   string `json:"name"`
			AuthID string `json:"auth_id"`
			Active bool   `json:"active"`
		}
		items := []item{}
		for name, p := range cfg.Profiles {
			items = append(items, item{Name: name, AuthID: p.AuthID, Active: name == cfg.Active})
		}
		return output.JSONSuccess(os.Stdout, items, nil)
	}
	rows := [][]string{{"ACTIVE", "NAME", "AUTH_ID"}}
	for name, p := range cfg.Profiles {
		active := ""
		if name == cfg.Active {
			active = "*"
		}
		rows = append(rows, []string{active, name, p.AuthID})
	}
	if len(rows) == 1 {
		fmt.Fprintln(os.Stderr, "no profiles configured. Run `plivo auth login`.")
		return nil
	}
	return output.Table(os.Stdout, rows)
}

func runAuthUse(cmd *cobra.Command, args []string) error {
	name := args[0]
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if _, ok := cfg.Profiles[name]; !ok {
		return fmt.Errorf("profile %q not found", name)
	}
	cfg.Active = name
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Active profile: %s\n", name)
	return nil
}

func runAuthRemove(cmd *cobra.Command, args []string) error {
	name := args[0]
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if _, ok := cfg.Profiles[name]; !ok {
		return fmt.Errorf("profile %q not found", name)
	}
	delete(cfg.Profiles, name)
	// Best-effort removal from the OS keychain (no-op if the token lived in
	// the config file instead).
	_ = config.DeleteToken(name)
	if cfg.Active == name {
		cfg.Active = ""
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
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
		return output.JSONSuccess(os.Stdout, acct, map[string]string{"source": source})
	}
	return output.KV(os.Stdout, [][2]string{
		{"profile", source},
		{"auth_id", acct.AuthID},
		{"name", acct.Name},
		{"account_type", acct.AccountType},
		{"billing_mode", acct.BillingMode},
		{"cash_credits", acct.CashCredits},
	})
}
