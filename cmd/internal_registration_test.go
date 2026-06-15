//go:build internal

package cmd

import (
	"strings"
	"testing"
)

// These assertions only hold in the internal build (`-tags internal`), where
// the auth-token surface is compiled in. The public-build counterparts in
// registration_test.go + safety_test.go deliberately omit it. (`plivo
// contacto …` was retired in favour of the unified `plivo login` flow — see
// cmd/login.go.)

func TestInternal_nestedSurfaces(t *testing.T) {
	nests := map[string][]string{
		"auth token": {"mint", "list", "revoke"},
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

func TestInternal_hodorServerFlagPresent(t *testing.T) {
	if rootCmd.PersistentFlags().Lookup("hodor-server") == nil {
		t.Error("internal build: --hodor-server flag should be registered")
	}
}
