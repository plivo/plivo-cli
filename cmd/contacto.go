package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var contactoCmd = &cobra.Command{
	Use:   "contacto",
	Short: "Manage Contacto session (used by `plivo agent` for AI agent CRUD)",
	Long: `plivo contacto — handles authentication to the Contacto stack for agent
management commands. Session is stored at ~/.plivo/contacto.toml and consumed by
'plivo agent list/get/create/update/attach'.

A Contacto session is distinct from the Plivo API credentials (auth_id +
auth_token) used by 'plivo number/message/call/application/auth whoami'.`,
}

var (
	contactoLoginEmail    string
	contactoLoginPassword string
	contactoLoginEnv      string
)

var contactoLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to Contacto (POSTs /v1/accounts/login-cli on global hodor)",
	RunE:  runContactoLogin,
}

var contactoWhoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Print the currently logged-in Contacto session",
	RunE:  runContactoWhoami,
}

var contactoLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear the saved Contacto session",
	RunE:  runContactoLogout,
}

func init() {
	contactoLoginCmd.Flags().StringVar(&contactoLoginEmail, "email", "", "Contacto email (prompted if omitted)")
	contactoLoginCmd.Flags().StringVar(&contactoLoginPassword, "password", "", "password (prompted if omitted; reads from stdin without echoing)")
	contactoLoginCmd.Flags().StringVar(&contactoLoginEnv, "env", "dev", "environment: dev|prod")

	contactoCmd.AddCommand(contactoLoginCmd, contactoWhoamiCmd, contactoLogoutCmd)
	rootCmd.AddCommand(contactoCmd)
}

// loginCLIResponse mirrors hodor's HandleLoginCLI envelope:
//
//	{ "api_id": "...", "message": "Authentication successful", "data": {...LoginResponse}, "error": null }
type loginCLIResponse struct {
	APIID   string          `json:"api_id"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Error   json.RawMessage `json:"error"`
}

type contactoLoginData struct {
	AuthToken        string `json:"auth_token"`
	AomUUID          string `json:"aom_uuid"`
	OrganizationName string `json:"organization_name"`
	BrowserSessionID string `json:"browser_session_id"`
	Region           string `json:"region"`
	Email            string `json:"email"`
	Name             string `json:"name"`
	TwoFAEnabled     bool   `json:"two_fa_enabled"`
	RedirectTwoFA    bool   `json:"redirect_two_fa_auth"`
	SessionUUID      string `json:"session_uuid"`

	// CLI-only fields populated by hodor's /v1/accounts/login-cli endpoint
	// so we can land Plivo REST creds in the same round-trip. Older hodor
	// builds leave these blank; the CLI falls back to "log in with `plivo
	// auth login` for REST access" if they're missing.
	PlivoAuthID    string `json:"plivo_auth_id"`
	PlivoAuthToken string `json:"plivo_auth_token"`
}

func runContactoLogin(cmd *cobra.Command, args []string) error {
	email := strings.TrimSpace(contactoLoginEmail)
	if email == "" {
		fmt.Fprint(os.Stderr, "Email: ")
		var line string
		fmt.Scanln(&line)
		email = strings.TrimSpace(line)
	}
	if email == "" {
		return fmt.Errorf("email is required")
	}

	password := contactoLoginPassword
	if password == "" {
		fmt.Fprint(os.Stderr, "Password: ")
		pwBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr)
		password = string(pwBytes)
	}
	if password == "" {
		return fmt.Errorf("password is required")
	}

	stub := &config.ContactoProfile{Environment: contactoLoginEnv}
	hodorBase := stub.GlobalHodorURL()
	loginURL := hodorBase + "/v1/accounts/login-cli"

	body := map[string]any{
		"email":    strings.ToLower(email),
		"password": password,
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", loginURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Client-Type", "web_app")
	req.Header.Set("User-Agent", "plivo-cli")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(rawBody)))
	}
	var envelope loginCLIResponse
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return fmt.Errorf("parse response: %w (body: %s)", err, string(rawBody))
	}

	var data contactoLoginData
	if len(envelope.Data) > 0 && string(envelope.Data) != "null" && string(envelope.Data) != "\"\"" {
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return fmt.Errorf("parse login data: %w (body: %s)", err, string(rawBody))
		}
	}

	if data.AuthToken == "" {
		if data.TwoFAEnabled || data.RedirectTwoFA {
			return fmt.Errorf("account requires 2FA — not yet supported in `plivo contacto login`; disable 2FA or paste localStorage.auth from browser")
		}
		return fmt.Errorf("login succeeded but no auth_token returned (body: %s)", string(rawBody))
	}

	prof := &config.ContactoProfile{
		Email:            data.Email,
		AuthToken:        data.AuthToken,
		AomUUID:          data.AomUUID,
		OrgName:          data.OrganizationName,
		Region:           data.Region,
		BrowserSessionID: data.BrowserSessionID,
		HodorServer:      hodorBase,
		Environment:      contactoLoginEnv,
	}
	if err := config.SaveContacto(prof); err != nil {
		return fmt.Errorf("save session: %w", err)
	}

	path, _ := config.ContactoPath()
	fmt.Fprintf(os.Stderr, "✓ Logged in as %s (region=%s, org=%s)\n", data.Email, data.Region, data.OrganizationName)
	fmt.Fprintf(os.Stderr, "  contacto session  → %s\n", path)

	// If hodor sent the Plivo REST creds along, persist them to config.toml so
	// `plivo auth whoami` / `number list` / `call make` work without a second
	// login. Profile is named "default" and marked active — overwrites any
	// previous default. Use `plivo auth login --name <other>` to keep multiple
	// accounts side-by-side.
	if data.PlivoAuthID != "" && data.PlivoAuthToken != "" {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load existing config.toml: %w", err)
		}
		cfg.Profiles["default"] = config.Profile{
			AuthID:    data.PlivoAuthID,
			AuthToken: data.PlivoAuthToken,
			Region:    data.Region,
		}
		cfg.Active = "default"
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config.toml: %w", err)
		}
		cfgPath, _ := config.Path()
		fmt.Fprintf(os.Stderr, "  plivo REST creds  → %s (profile=default, auth_id=%s)\n", cfgPath, data.PlivoAuthID)
	} else {
		fmt.Fprintln(os.Stderr, "  ℹ  hodor did not return REST creds — run `plivo auth login` separately for REST access.")
	}
	return nil
}

func runContactoWhoami(cmd *cobra.Command, args []string) error {
	prof, err := config.LoadContacto()
	if err != nil {
		return err
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, prof, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"email", prof.Email},
		{"org", prof.OrgName},
		{"aom_uuid", prof.AomUUID},
		{"region", prof.Region},
		{"environment", prof.Environment},
		{"gateway", prof.RegionalGatewayURL()},
		{"logged_in_at", prof.LoggedInAt},
	})
}

func runContactoLogout(cmd *cobra.Command, args []string) error {
	if err := config.ClearContacto(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "✓ Contacto session cleared")
	return nil
}
