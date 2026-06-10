//go:build internal

package cmd

import "os"

// The --hodor-server global flag drives the internal-only agent + auth-token
// surfaces. It's registered here so the public v1 build (without the `internal`
// tag) doesn't expose a flag that points at a Plivo-internal service. The
// backing var lives in root.go and stays "" in the public build.
func init() {
	rootCmd.PersistentFlags().StringVar(&adminServer, "hodor-server", os.Getenv("PLIVO_HODOR_SERVER"),
		"base URL for hodor (used by `plivo agent` and `plivo auth token`)")
}
