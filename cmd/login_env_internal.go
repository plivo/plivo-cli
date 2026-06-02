//go:build internal

// Gated behind `internal`: dev / staging hodor URLs only ship in the
// internal binary that Plivo engineers build locally. Public users see
// only "prod" via login_env.go. The CX-service boundary guard
// (internal/api/url_boundary_test.go) skips files with this build
// constraint precisely so these URLs don't leak into the public build.

package cmd

func init() {
	loginEnvURLs["dev"] = "https://dev-global-auth-api.contactodev.com"
	// loginEnvURLs["staging"] = "..."   // add when needed
}
