package cmd

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/config"
)

// ─── auth list: org column ──────────────────────────────────────────────────
// Profiles gained OrgName once login started deriving profile names from
// the organization (see login.go). `auth list` should surface it per
// profile, in both render modes, without changing the existing shape.

func TestAuthList_showsOrgName_table(t *testing.T) {
	setFakeCreds(t)
	cfg := &config.Config{
		Active: "acme",
		Profiles: map[string]config.Profile{
			"acme":    {AuthID: "MA_ACME", OrgName: "Acme Inc"},
			"default": {AuthID: "MA_NOORG"}, // pre-existing profile, no org known
		},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	err, stdout, _ := execCmd(t, "-o", "table", "auth", "list")
	if err != nil {
		t.Fatalf("auth list: %v", err)
	}
	if !strings.Contains(stdout, "ORG") {
		t.Errorf("table header missing ORG column, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Acme Inc") {
		t.Errorf("table missing org name for the profile with a known org, got:\n%s", stdout)
	}
}

func TestAuthList_showsOrgName_json(t *testing.T) {
	setFakeCreds(t)
	cfg := &config.Config{
		Active: "acme",
		Profiles: map[string]config.Profile{
			"acme": {AuthID: "MA_ACME", OrgName: "Acme Inc"},
		},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	err, stdout, _ := execCmd(t, "-o", "json", "auth", "list")
	if err != nil {
		t.Fatalf("auth list -o json: %v", err)
	}
	var env struct {
		Data []struct {
			Name    string `json:"name"`
			AuthID  string `json:"auth_id"`
			OrgName string `json:"org_name"`
			Active  bool   `json:"active"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if len(env.Data) != 1 {
		t.Fatalf("expected 1 profile, got %d: %+v", len(env.Data), env.Data)
	}
	if env.Data[0].OrgName != "Acme Inc" {
		t.Errorf("org_name = %q, want %q", env.Data[0].OrgName, "Acme Inc")
	}
	if !env.Data[0].Active {
		t.Error("active = false, want true for the active profile")
	}
}

// --dry-run runs a local change's checks, says what it would do and changes
// nothing: not the config, not the keychain.
func TestLocalChanges_dryRunChangesNothing(t *testing.T) {
	home := setEmptyHome(t)
	writeProfile(t, home, "active = \"a\"\n\n[profiles.a]\nauth_id = \"CIFAKEA\"\n\n[profiles.b]\nauth_id = \"CIFAKEB\"\n")
	if err := config.SetToken("a", "tok"); err != nil {
		t.Fatal(err)
	}
	before, _ := config.Load()
	for _, args := range [][]string{
		{"auth", "use", "b"}, {"auth", "remove", "a"}, {"logout"},
		{"config", "set", "telemetry", "off"}, {"config", "telemetry", "off"},
	} {
		if err, _, stderr := execCmd(t, append(args, "--dry-run")...); err != nil || !strings.HasPrefix(stderr, "[dry-run] Would ") {
			t.Errorf("%v --dry-run: %v, stderr %q", args, err, stderr)
		}
	}
	after, _ := config.Load()
	if tok, _ := config.GetToken("a"); !reflect.DeepEqual(after, before) || tok != "tok" {
		t.Errorf("--dry-run changed the config (%+v) or the keychain (token %q)", after, tok)
	}
	if err, _, _ := execCmd(t, "auth", "use", "ghost", "--dry-run"); err == nil {
		t.Error("auth use ghost --dry-run: want the unknown profile refused")
	}
	// Only a token in the keychain is named: b keeps none there.
	_, _, withToken := execCmd(t, "logout", "a", "--dry-run")
	if _, _, without := execCmd(t, "auth", "remove", "b", "--dry-run"); !strings.Contains(withToken, "keychain") || strings.Contains(without, "keychain") {
		t.Errorf("dry runs say %q for a and %q for b; want the keychain named for a alone", withToken, without)
	}
}

// auth use takes a profile name, an organization name or an auth ID, and never
// guesses between two matches.
func TestAuthUse_byOrgNameOrAuthID(t *testing.T) {
	home := setEmptyHome(t)
	writeProfile(t, home, "active = \"a\"\n\n[profiles.a]\nauth_id = \"CIFAKEA\"\norg_name = \"Acme Inc\"\n\n"+
		"[profiles.b]\nauth_id = \"CIFAKEB\"\norg_name = \"Beta Ltd\"\n\n[profiles.b2]\nauth_id = \"CIFAKEC\"\norg_name = \"Beta Ltd\"\n")
	for _, c := range []struct{ arg, want string }{{"b", "b"}, {"CIFAKEA", "a"}, {"acme inc", "a"}} {
		if err, _, _ := execCmd(t, "auth", "use", c.arg); err != nil {
			t.Fatalf("auth use %q: %v", c.arg, err)
		}
		if cfg, _ := config.Load(); cfg.Active != c.want {
			t.Errorf("auth use %q: active = %q, want %q", c.arg, cfg.Active, c.want)
		}
	}
	err, _, _ := execCmd(t, "auth", "use", "Beta Ltd")
	if err == nil || !strings.Contains(err.Error(), "b2") {
		t.Errorf("auth use on two matching orgs: err = %v, want both candidates named", err)
	}
	if cfg, _ := config.Load(); cfg.Active != "a" {
		t.Errorf("an ambiguous auth use changed the active profile to %q", cfg.Active)
	}
}

func TestAuthList_isSortedByName(t *testing.T) {
	home := setEmptyHome(t)
	writeProfile(t, home, "active = \"m\"\n\n[profiles.z]\nauth_id = \"CIFAKEZ\"\n\n[profiles.m]\nauth_id = \"CIFAKEM\"\n\n[profiles.a]\nauth_id = \"CIFAKEA\"\n")
	for i := 0; i < 5; i++ {
		_, stdout, _ := execCmd(t, "-o", "table", "auth", "list")
		if a, m, z := strings.Index(stdout, "CIFAKEA"), strings.Index(stdout, "CIFAKEM"), strings.Index(stdout, "CIFAKEZ"); !(a < m && m < z) {
			t.Fatalf("run %d: rows are not in name order:\n%s", i, stdout)
		}
	}
}
