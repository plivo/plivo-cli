package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
)

// TestApplyAPIURL_precedenceAndValidation pins the --api-url / PLIVO_API_URL
// resolution contract: flag wins over env, env wins over the built-in
// default, a trailing slash is trimmed, and a malformed value is a hard
// error rather than a silent fallback to production.
func TestApplyAPIURL_precedenceAndValidation(t *testing.T) {
	// Guard the whole test against an ambient PLIVO_API_URL in the shell
	// (e.g. left over from a manual dev smoke-test session); subtests that
	// need a specific value override this at their own scope.
	t.Setenv("PLIVO_API_URL", "")

	t.Run("unset_leavesDefaultBaseURL", func(t *testing.T) {
		apiURLFlag = ""
		c := api.New("MAxxx", "tok", time.Second)
		want := c.BaseURL
		if err := applyAPIURL(c); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if c.BaseURL != want {
			t.Errorf("BaseURL changed with no override set: got %q, want %q", c.BaseURL, want)
		}
	})

	t.Run("env_winsOverDefault", func(t *testing.T) {
		apiURLFlag = ""
		t.Setenv("PLIVO_API_URL", "https://dev.example.com")
		c := api.New("MAxxx", "tok", time.Second)
		if err := applyAPIURL(c); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if c.BaseURL != "https://dev.example.com" {
			t.Errorf("BaseURL = %q, want https://dev.example.com", c.BaseURL)
		}
	})

	t.Run("flag_winsOverEnv", func(t *testing.T) {
		apiURLFlag = "https://flag.example.com"
		defer func() { apiURLFlag = "" }()
		t.Setenv("PLIVO_API_URL", "https://env.example.com")
		c := api.New("MAxxx", "tok", time.Second)
		if err := applyAPIURL(c); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if c.BaseURL != "https://flag.example.com" {
			t.Errorf("BaseURL = %q, want the flag value (https://flag.example.com)", c.BaseURL)
		}
	})

	t.Run("trailingSlash_trimmed", func(t *testing.T) {
		apiURLFlag = "https://flag.example.com/"
		defer func() { apiURLFlag = "" }()
		c := api.New("MAxxx", "tok", time.Second)
		if err := applyAPIURL(c); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if c.BaseURL != "https://flag.example.com" {
			t.Errorf("trailing slash should be trimmed: got %q", c.BaseURL)
		}
	})

	t.Run("whitespaceOnly_treatedAsUnset", func(t *testing.T) {
		apiURLFlag = "   "
		defer func() { apiURLFlag = "" }()
		c := api.New("MAxxx", "tok", time.Second)
		want := c.BaseURL
		if err := applyAPIURL(c); err != nil {
			t.Fatalf("whitespace-only --api-url should be treated as unset, got err: %v", err)
		}
		if c.BaseURL != want {
			t.Errorf("BaseURL should stay at the default, got %q", c.BaseURL)
		}
	})

	badCases := []string{"not-a-url", "ftp://example.com", "example.com", "://broken"}
	for _, bad := range badCases {
		t.Run("rejects_"+bad, func(t *testing.T) {
			apiURLFlag = bad
			defer func() { apiURLFlag = "" }()
			c := api.New("MAxxx", "tok", time.Second)
			origBase := c.BaseURL
			err := applyAPIURL(c)
			if err == nil {
				t.Fatalf("--api-url %q: expected an error, got nil", bad)
			}
			if !strings.Contains(err.Error(), "BAD_INPUT") {
				t.Errorf("--api-url %q: expected BAD_INPUT, got: %v", bad, err)
			}
			if c.BaseURL != origBase {
				t.Errorf("--api-url %q: BaseURL should stay unchanged on rejection, got %q", bad, c.BaseURL)
			}
		})
	}
}
