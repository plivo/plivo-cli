package cmd

import (
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
)

// loginEnvURLs maps a `--env` name to the hodor edge URL that named env
// runs at. The public build only knows "prod" — internal builds extend
// this map (in login_env_internal.go) so dev / staging URLs never end up
// in a shipped public binary.
//
// The CX-service boundary guard in internal/api/url_boundary_test.go is
// what enforces this: it scans non-`//go:build internal` files for
// *.contactodev.com / *.plivops.com URL literals and fails the build if
// any leak in.
var loginEnvURLs = map[string]string{
	"prod": api.DefaultBuddyBase,
}

// resolveLoginEnv returns the hodor edge URL for env (case-insensitive)
// and whether the env was recognised. Unknown env → ok=false; the
// caller surfaces a BAD_INPUT explaining which envs are available.
func resolveLoginEnv(env string) (string, bool) {
	u, ok := loginEnvURLs[strings.ToLower(env)]
	return u, ok
}

// loginEnvNames returns the recognised env names, for help / error text.
func loginEnvNames() []string {
	out := make([]string, 0, len(loginEnvURLs))
	for k := range loginEnvURLs {
		out = append(out, k)
	}
	return out
}
