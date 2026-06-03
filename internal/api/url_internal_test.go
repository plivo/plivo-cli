//go:build internal

// Tests for BuddyAdminURL / HodorURL — the internal-build admin URL
// joiner. Moved out of url_test.go so the error-message string literal
// stays out of any public binary built without the `internal` tag.

package api

import (
	"strings"
	"testing"
)

func TestBuddyAdminURL(t *testing.T) {
	t.Run("not configured returns error", func(t *testing.T) {
		c := &Client{}
		_, err := c.BuddyAdminURL("/v1/agent/token")
		if err == nil {
			t.Fatal("expected error when AdminBaseURL is empty")
		}
		if !strings.Contains(err.Error(), "admin URL not configured") {
			t.Errorf("error message doesn't mention configuration: %v", err)
		}
	})

	t.Run("joins base with path", func(t *testing.T) {
		c := &Client{AdminBaseURL: "https://admin.example.com"}
		got, err := c.BuddyAdminURL("/v1/agent/token")
		if err != nil {
			t.Fatal(err)
		}
		want := "https://admin.example.com/v1/agent/token"
		if got != want {
			t.Errorf("BuddyAdminURL = %q, want %q", got, want)
		}
	})

	t.Run("strips trailing slash from base", func(t *testing.T) {
		c := &Client{AdminBaseURL: "https://admin.example.com/"}
		got, _ := c.BuddyAdminURL("/v1/agent/token")
		want := "https://admin.example.com/v1/agent/token"
		if got != want {
			t.Errorf("trailing slash not stripped: %q", got)
		}
	})

	t.Run("adds leading slash to path", func(t *testing.T) {
		c := &Client{AdminBaseURL: "https://admin.example.com"}
		got, _ := c.BuddyAdminURL("v1/agent/token") // no leading slash
		want := "https://admin.example.com/v1/agent/token"
		if got != want {
			t.Errorf("leading slash not added: %q", got)
		}
	})
}

// HodorURL is the legacy alias used by internal callers; verify it's the
// same function so we can't silently divorce the two.
func TestHodorURL_aliasOfBuddyAdminURL(t *testing.T) {
	c := &Client{AdminBaseURL: "https://admin.example.com"}
	a, _ := c.HodorURL("/v1/agent/token")
	b, _ := c.BuddyAdminURL("/v1/agent/token")
	if a != b {
		t.Errorf("HodorURL/BuddyAdminURL disagreed: %q vs %q", a, b)
	}
}
