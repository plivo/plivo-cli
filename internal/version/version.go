// Package version exposes the CLI version string.
//
// Override at build time with:
//
//	go build -ldflags "-X github.com/plivo/plivo-cli/internal/version.Value=$(git describe --tags --always)" .
package version

var Value = "0.1.0-dev"

// UserAgent is the value sent on every outbound HTTP request from the CLI.
// Set as a request header so Plivo's API edges can identify CLI traffic in
// their logs and rate-limiters.
func UserAgent() string {
	return "Plivo-CLI/" + Value
}
