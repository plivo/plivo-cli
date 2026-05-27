//go:build internal

package cmd

import (
	"strings"
	"testing"
)

// These assertions only hold in the internal build (`-tags internal`), where
// the agent / contacto / auth-token surfaces are compiled in. The public-build
// counterparts in registration_test.go + safety_test.go deliberately omit them.

func TestInternal_agentContactoGroupsRegistered(t *testing.T) {
	for _, g := range []string{"agent", "contacto"} {
		if findCmdNoFail(g) == nil {
			t.Errorf("internal build: top-level %q not registered", g)
		}
	}
}

func TestInternal_agentSubcommands(t *testing.T) {
	verbs := []string{"list", "get", "create", "update", "publish", "download", "delete", "run", "attach", "session"}
	for _, v := range verbs {
		if findCmdNoFail("agent", v) == nil {
			t.Errorf("internal build: plivo agent %s not registered", v)
		}
	}
}

func TestInternal_nestedSurfaces(t *testing.T) {
	nests := map[string][]string{
		"auth token":    {"mint", "list", "revoke"},
		"agent session": {"show", "clear"},
		"contacto":      {"login", "logout", "whoami"},
	}
	for path, verbs := range nests {
		parts := strings.Fields(path)
		for _, v := range verbs {
			full := append(append([]string{}, parts...), v)
			if findCmdNoFail(full...) == nil {
				t.Errorf("internal build: plivo %s not registered", strings.Join(full, " "))
			}
		}
	}
}

func TestInternal_authTokenMintRequiresModules(t *testing.T) {
	cmd := findCmd(t, "auth", "token", "mint")
	if !isFlagRequired(cmd, "modules") {
		t.Error("internal build: auth token mint --modules should be required")
	}
}

func TestInternal_agentDeleteRefusesWithoutYes(t *testing.T) {
	setFakeCreds(t)
	err, _, _ := execCmd(t, "agent", "delete", "AGENT-UUID")
	if err == nil || !strings.Contains(err.Error(), "DESTRUCTIVE_REFUSED") {
		t.Errorf("internal build: agent delete should return DESTRUCTIVE_REFUSED without --yes, got: %v", err)
	}
}

func TestInternal_hodorServerFlagPresent(t *testing.T) {
	if rootCmd.PersistentFlags().Lookup("hodor-server") == nil {
		t.Error("internal build: --hodor-server flag should be registered")
	}
}
