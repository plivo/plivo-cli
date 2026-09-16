package release

import "testing"

// TestSigningRequired covers SA-03: the pre-v0.3.0 legacy exception was
// documented in comments but never implemented, so every release including
// brand-new ones was allowed to be unsigned.
func TestSigningRequired(t *testing.T) {
	cases := []struct {
		tag  string
		want bool
	}{
		// Genuinely predate signing.
		{"v0.1.0", false},
		{"v0.2.0", false},
		{"v0.1.0-beta.1", false},
		// The boundary itself is the first signed release.
		{"v0.3.0", true},
		{"v0.4.1", true},
		{"v1.0.0", true},
		{"v1.0.1", true},
		{"v2.0.0", true},
		// A pre-release of the boundary sorts below it per semver.
		{"v0.3.0-rc.1", false},
		// Unparseable must fail CLOSED: an unreadable tag is not a licence
		// to skip the check.
		{"", true},
		{"garbage", true},
		{"v1.0", true},
		{"latest", true},
	}
	for _, c := range cases {
		if got := SigningRequired(c.tag); got != c.want {
			t.Errorf("SigningRequired(%q) = %v, want %v", c.tag, got, c.want)
		}
	}
}
