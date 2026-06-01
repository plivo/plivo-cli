// Package version exposes the CLI version string.
//
// Override at build time with:
//
//	go build -ldflags "-X github.com/plivo/plivo-cli/internal/version.Value=$(git describe --tags --always)" .
package version

import "runtime"

var Value = "0.1.0-dev"

// ClientType is the value sent in the `Client-Type` request header on every
// outbound HTTP request the CLI makes (except deliberate web-UI impersonation
// like `plivo contacto login` which uses the same value as the Console). Hodor
// / contacto-core / aiassist all log this field structured, so a single
// grep on `client_type=cli` surfaces every CLI request across all hops.
const ClientType = "cli"

// UserAgent is the value sent on every outbound HTTP request from the CLI.
// Set as a request header so api.plivo.com / lookup.plivo.com / hodor edges
// can identify CLI traffic in their logs and rate-limiters — same string for
// both Plivo and CX endpoints. Includes runtime.GOOS so we can tell mac
// vs linux vs windows CLI users apart in aggregate metrics.
func UserAgent() string {
	return "Plivo-CLI/" + Value + " (" + runtime.GOOS + ")"
}
