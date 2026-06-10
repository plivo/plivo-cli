package api

import (
	"strings"
	"testing"
	"time"
)

func TestNew_defaults(t *testing.T) {
	c := New("MAabc", "tok", 0)
	if c.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, DefaultBaseURL)
	}
	if c.AuthID != "MAabc" || c.AuthToken != "tok" {
		t.Errorf("creds not stored: %q / %q", c.AuthID, c.AuthToken)
	}
	if c.HTTP == nil {
		t.Fatal("HTTP client must be non-nil")
	}
	if c.HTTP.Timeout != 30*time.Second {
		t.Errorf("zero/negative timeout should default to 30s, got %v", c.HTTP.Timeout)
	}
}

func TestNew_customTimeout(t *testing.T) {
	c := New("MAabc", "tok", 5*time.Second)
	if c.HTTP.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", c.HTTP.Timeout)
	}
}

func TestAccountURL(t *testing.T) {
	// BaseURL is the gateway base (no API version segment); the
	// AccountURL builder appends /v1/Account/{id}/.
	c := &Client{BaseURL: "https://hodor.plivo.com/v1/cli/api", AuthID: "MAXYZ123"}

	cases := []struct {
		name  string
		parts []string
		want  string
	}{
		{"no parts", nil, "https://hodor.plivo.com/v1/cli/api/v1/Account/MAXYZ123/"},
		{"single segment", []string{"Number"}, "https://hodor.plivo.com/v1/cli/api/v1/Account/MAXYZ123/Number/"},
		{"two segments", []string{"Number", "+14155551234"}, "https://hodor.plivo.com/v1/cli/api/v1/Account/MAXYZ123/Number/+14155551234/"},
		{"deep nesting", []string{"Conference", "demo", "Member", "42", "Mute"}, "https://hodor.plivo.com/v1/cli/api/v1/Account/MAXYZ123/Conference/demo/Member/42/Mute/"},
		{"10dlc lowercase segment", []string{"10dlc", "Brand"}, "https://hodor.plivo.com/v1/cli/api/v1/Account/MAXYZ123/10dlc/Brand/"},
		{"Masking/Session two-part", []string{"Masking", "Session"}, "https://hodor.plivo.com/v1/cli/api/v1/Account/MAXYZ123/Masking/Session/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := c.AccountURL(tc.parts...)
			if got != tc.want {
				t.Errorf("AccountURL(%v) = %q, want %q", tc.parts, got, tc.want)
			}
		})
	}
}

func TestAccountURL_respectsCustomBase(t *testing.T) {
	c := &Client{BaseURL: "https://staging.example.com/v1/cli/api", AuthID: "MA1"}
	got := c.AccountURL("Number")
	want := "https://staging.example.com/v1/cli/api/v1/Account/MA1/Number/"
	if got != want {
		t.Errorf("custom base not honored: got %q, want %q", got, want)
	}
}

func TestLookupURL(t *testing.T) {
	// LookupURL routes via the CLI gateway base, not direct to lookup.plivo.com.
	c := &Client{BaseURL: "https://hodor.plivo.com/v1/cli/api"}
	cases := []struct {
		number string
		want   string
	}{
		{"+14155551234", "https://hodor.plivo.com/v1/cli/api/v1/Lookup/Number/+14155551234"},
		{"+919876543210", "https://hodor.plivo.com/v1/cli/api/v1/Lookup/Number/+919876543210"},
		{"+442012345678", "https://hodor.plivo.com/v1/cli/api/v1/Lookup/Number/+442012345678"},
	}
	for _, tc := range cases {
		t.Run(tc.number, func(t *testing.T) {
			got := c.LookupURL(tc.number)
			if got != tc.want {
				t.Errorf("LookupURL(%q) = %q, want %q", tc.number, got, tc.want)
			}
		})
	}
	// Critical: must NOT have a trailing slash — the API requires the raw
	// number path so ?type=carrier can be appended cleanly.
	if strings.HasSuffix(c.LookupURL("+1"), "/") {
		t.Error("LookupURL must not return a trailing slash (would break ?type=carrier)")
	}
}

func TestBuddyURL_routing(t *testing.T) {
	c := &Client{BuddyBaseURL: "https://hodor.plivo.com"}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"AI surface goes via /v1/cli/cx",
			"/v1/aiassist/buddy-ext/chat",
			"https://hodor.plivo.com/v1/cli/cx/v1/aiassist/buddy-ext/chat",
		},
		{
			"AI escalations also via /v1/cli/cx",
			"/v1/aiassist/buddy-ext/escalations",
			"https://hodor.plivo.com/v1/cli/cx/v1/aiassist/buddy-ext/escalations",
		},
		{
			"PKCE login token stays at root (unauthenticated route)",
			"/v1/accounts/cli/token",
			"https://hodor.plivo.com/v1/accounts/cli/token",
		},
		{
			"PKCE authorize stays at root",
			"/v1/accounts/cli/authorize",
			"https://hodor.plivo.com/v1/accounts/cli/authorize",
		},
		{
			"path without leading slash still composes",
			"v1/aiassist/buddy-ext/chat",
			"https://hodor.plivo.com/v1/cli/cx/v1/aiassist/buddy-ext/chat",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.BuddyURL(tc.in); got != tc.want {
				t.Errorf("BuddyURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsScopedToken(t *testing.T) {
	cases := []struct {
		token string
		want  bool
	}{
		{"stk_abc123", true},
		{"stk_", true},
		{"stk", false},
		{"", false},
		{"abc", false},
		{"Stk_abc", false}, // case-sensitive prefix per HasPrefix
		{"prefix-stk_abc", false},
	}
	for _, tc := range cases {
		t.Run(tc.token, func(t *testing.T) {
			c := &Client{AuthToken: tc.token}
			if got := c.IsScopedToken(); got != tc.want {
				t.Errorf("IsScopedToken(%q) = %v, want %v", tc.token, got, tc.want)
			}
		})
	}
}
