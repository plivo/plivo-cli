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
}
