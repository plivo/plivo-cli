//go:build internal

// Internal-build helpers on Client. Kept in a build-tagged file so the
// strings (flag names, env var names, internal-only error messages) stay
// out of the public binary and out of `strings(1)` output on a released
// binary. Field declarations in client.go are fine to leave in the public
// build — they don't reach the binary unless reflection is used.

package api

import (
	"fmt"
	"strings"
)

// BuddyAdminURL joins the optional admin-override base URL with the given
// path. Only used by internal-tagged commands (e.g. the auth-token surface)
// that target the admin edge directly. Returns an error if no override is
// configured.
func (c *Client) BuddyAdminURL(path string) (string, error) {
	if c.AdminBaseURL == "" {
		return "", fmt.Errorf("admin URL not configured: set --hodor-server or PLIVO_HODOR_SERVER")
	}
	base := strings.TrimRight(c.AdminBaseURL, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path, nil
}

// HodorURL is kept as an alias of BuddyAdminURL so existing internal-build
// callers (cmd/authToken.go) don't have to rename. The canonical name on the
// public Client type stays neutral.
func (c *Client) HodorURL(path string) (string, error) { return c.BuddyAdminURL(path) }
