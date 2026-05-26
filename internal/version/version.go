// Package version exposes the CLI version string.
//
// Override at build time with:
//
//	go build -ldflags "-X github.com/plivo/plivo-cli/internal/version.Value=$(git describe --tags --always)" .
package version

var Value = "0.1.0-dev"
