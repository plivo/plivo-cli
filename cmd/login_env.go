package cmd

import (
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
)

// loginEnvURLs maps a `--env` name to the edge URL that named env runs
// at. resolveLoginEnv returns the URL for the named env, or false if
// unknown. The public build only supports "prod".
var loginEnvURLs = map[string]string{
	"prod": api.DefaultBuddyBase,
}

// resolveLoginEnv returns the edge URL for env (case-insensitive) and
// whether the env was recognised. Unknown env → ok=false; the caller
// surfaces a BAD_INPUT explaining which envs are available.
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
