// Package version exposes the CLI version string.
//
// Override at build time with:
//
//	go build -ldflags "-X github.com/plivo/plivo-cli/internal/version.Value=$(git describe --tags --always)" .
package version

var Value = "0.1.0-dev"

// UserAgent is the value sent on every outbound HTTP request from the CLI.
// Set as a request header so api.plivo.com / lookup.plivo.com / hodor edges
// can identify CLI traffic in their logs and rate-limiters — same string for
// both Plivo and CX endpoints.
func UserAgent() string {
	return "Plivo-CLI/" + Value
}
