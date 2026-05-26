package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// ContactoProfile holds a Contacto session credentials bundle obtained via
// `plivo contacto login` against a hodor /v1/accounts/login-cli endpoint.
//
// The CLI uses it to authenticate against the regional Contacto auth-api
// gateway for agent CRUD (PHLO config service) and vibe-agent SSE generation.
type ContactoProfile struct {
	Email            string `toml:"email"`
	AuthToken        string `toml:"auth_token"`
	AomUUID          string `toml:"aom_uuid"`
	OrgName          string `toml:"org_name"`
	Region           string `toml:"region"`
	BrowserSessionID string `toml:"browser_session_id"`
	HodorServer      string `toml:"hodor_server"`
	Environment      string `toml:"environment"` // "dev" or "prod"
	LoggedInAt       string `toml:"logged_in_at"`
}

var ErrNoContactoSession = errors.New("no Contacto session: run `plivo contacto login` first")

func ContactoPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".plivo", "contacto.toml"), nil
}

// LoadContacto returns the saved Contacto session, or ErrNoContactoSession if
// none is present.
func LoadContacto() (*ContactoProfile, error) {
	p, err := ContactoPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoContactoSession
		}
		return nil, err
	}
	var prof ContactoProfile
	if _, err := toml.Decode(string(data), &prof); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p, err)
	}
	if prof.AuthToken == "" {
		return nil, ErrNoContactoSession
	}
	return &prof, nil
}

func SaveContacto(prof *ContactoProfile) error {
	p, err := ContactoPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	if prof.LoggedInAt == "" {
		prof.LoggedInAt = time.Now().UTC().Format(time.RFC3339)
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(prof)
}

func ClearContacto() error {
	p, err := ContactoPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RegionalGatewayURL returns the base URL of the regional Contacto auth-api
// gateway. The contacto-console hits this URL for agent CRUD + vibe SSE.
func (p *ContactoProfile) RegionalGatewayURL() string {
	if p.Region == "" {
		return ""
	}
	if p.Environment == "prod" {
		// best-guess prod pattern; not exercised by hackathon
		return fmt.Sprintf("https://%s-auth-api.contacto.com", p.Region)
	}
	return fmt.Sprintf("https://dev-%s-auth-api.contactodev.com", p.Region)
}

// GlobalHodorURL returns the global hodor URL used for login.
func (p *ContactoProfile) GlobalHodorURL() string {
	if p.HodorServer != "" {
		return p.HodorServer
	}
	if p.Environment == "prod" {
		return "https://global-auth-api.contacto.com"
	}
	return "https://dev-global-auth-api.contactodev.com"
}
