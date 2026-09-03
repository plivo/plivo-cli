package cmd

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
)

// stdinTokenFn pipes a string into os.Stdin for the duration of fn — used
// by api_test (POST body from `--body @-`) and any future test that
// exercises a stdin-reading flag.
func stdinTokenFn(t *testing.T, in string, fn func()) {
	t.Helper()
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = origStdin
	}()
	go func() {
		_, _ = io.WriteString(w, in)
		_ = w.Close()
	}()
	fn()
}

// ─── logout coverage ────────────────────────────────────────────────────────
// The login flow itself is covered exhaustively by login_browser_test.go —
// browser PKCE is the only `plivo login` path on main. The logout tests
// below stay because they're cheap, fast, and independently useful.

func TestLogout_noArg_noActive_errors(t *testing.T) {
	setFakeCreds(t)
	// Brand-new HOME → no profiles → no active.
	err, _, _ := execCmd(t, "logout")
	if err == nil || !strings.Contains(err.Error(), "no active profile") {
		t.Errorf("expected 'no active profile' error, got: %v", err)
	}
}

func TestLogout_nonExistentProfile_errors(t *testing.T) {
	setFakeCreds(t)
	err, _, _ := execCmd(t, "logout", "ghost")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

// ─── slugifyOrgName ─────────────────────────────────────────────────────────

func TestSlugifyOrgName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"simple lowercase word", "acme", "acme"},
		{"mixed case", "Acme", "acme"},
		{"spaces and punctuation, mixed case", "  Acme Corp, Inc.  ", "acme-corp-inc"},
		{"already a slug", "already-a-slug", "already-a-slug"},
		{"collapses repeated separators", "Acme---Corp   Inc", "acme-corp-inc"},
		{"punctuation-only slugifies to empty", "!!! ... ***", ""},
		{"empty input", "", ""},
		{"whitespace only", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slugifyOrgName(tc.in)
			if got != tc.want {
				t.Errorf("slugifyOrgName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSlugifyOrgName_capsLength(t *testing.T) {
	got := slugifyOrgName(strings.Repeat("a", 60))
	if len(got) != 40 {
		t.Errorf("len(slug) = %d, want capped at 40", len(got))
	}
	// Cap must not itself leave a trailing hyphen — cut cleanly on a
	// word boundary or trim the dangling separator.
	if strings.HasSuffix(got, "-") {
		t.Errorf("slug %q has a trailing hyphen after the length cap", got)
	}
}

// ─── persistProfile: org-derived naming ─────────────────────────────────────

func TestPersistProfile_derivesNameFromOrg(t *testing.T) {
	setFakeCreds(t)
	cfg := &config.Config{Profiles: map[string]config.Profile{}}

	err := persistProfile(cfg, "default", false, loginBundle{
		AuthID:    "MA_ACME",
		AuthToken: "tok-1",
		OrgName:   "Acme, Inc.",
	})
	if err != nil {
		t.Fatalf("persistProfile: %v", err)
	}
	if _, ok := cfg.Profiles["acme-inc"]; !ok {
		t.Fatalf("expected profile %q, got profiles: %v", "acme-inc", cfg.Profiles)
	}
	if cfg.Active != "acme-inc" {
		t.Errorf("cfg.Active = %q, want %q (first profile wins)", cfg.Active, "acme-inc")
	}
}

func TestPersistProfile_missingOrgName_fallsBackToDefault(t *testing.T) {
	setFakeCreds(t)
	cfg := &config.Config{Profiles: map[string]config.Profile{}}

	// OrgName intentionally empty — simulates an auth server that hasn't
	// shipped org_name yet (or an org with no name).
	err := persistProfile(cfg, "default", false, loginBundle{
		AuthID:    "MA_NOORG",
		AuthToken: "tok-1",
	})
	if err != nil {
		t.Fatalf("persistProfile: %v", err)
	}
	if _, ok := cfg.Profiles["default"]; !ok {
		t.Fatalf(`expected fallback profile "default", got profiles: %v`, cfg.Profiles)
	}
}

func TestPersistProfile_explicitName_overridesOrgDerivedName(t *testing.T) {
	setFakeCreds(t)
	cfg := &config.Config{Profiles: map[string]config.Profile{}}

	err := persistProfile(cfg, "staging", true, loginBundle{
		AuthID:    "MA_ACME",
		AuthToken: "tok-1",
		OrgName:   "Acme Inc",
	})
	if err != nil {
		t.Fatalf("persistProfile: %v", err)
	}
	if _, ok := cfg.Profiles["staging"]; !ok {
		t.Errorf(`explicit -n "staging" should win over the org-derived name, got profiles: %v`, cfg.Profiles)
	}
	if _, ok := cfg.Profiles["acme-inc"]; ok {
		t.Errorf("should not have also created an org-derived profile, got: %v", cfg.Profiles)
	}
}

// ─── persistProfile: never silently overwrite a different org ──────────────

func TestPersistProfile_sameOrg_updatesInPlace(t *testing.T) {
	setFakeCreds(t)
	cfg := &config.Config{
		Active: "acme",
		Profiles: map[string]config.Profile{
			"acme": {AuthID: "MA_ACME", Region: "us-east-1", OrgName: "Acme"},
		},
	}

	// Re-login to the same org (same auth_id, same org name → same derived
	// slug "acme") — the normal token-refresh path.
	err := persistProfile(cfg, "default", false, loginBundle{
		AuthID:    "MA_ACME",
		AuthToken: "tok-refreshed",
		Region:    "ap-south-1",
		OrgName:   "Acme",
	})
	if err != nil {
		t.Fatalf("persistProfile: %v", err)
	}
	if len(cfg.Profiles) != 1 {
		t.Fatalf("expected exactly 1 profile after a same-org re-login, got %d: %v", len(cfg.Profiles), cfg.Profiles)
	}
	got, ok := cfg.Profiles["acme"]
	if !ok {
		t.Fatalf("profile %q missing after same-org re-login", "acme")
	}
	if got.Region != "ap-south-1" {
		t.Errorf("Region = %q, want the refreshed value %q", got.Region, "ap-south-1")
	}
	if cfg.Active != "acme" {
		t.Errorf("cfg.Active = %q, want unchanged %q", cfg.Active, "acme")
	}
}

func TestPersistProfile_differentOrg_explicitName_refuses(t *testing.T) {
	setFakeCreds(t)
	cfg := &config.Config{
		Active: "staging",
		Profiles: map[string]config.Profile{
			"staging": {AuthID: "MA_ACME", OrgName: "Acme Inc"},
		},
	}

	err := persistProfile(cfg, "staging", true, loginBundle{
		AuthID:    "MA_OTHERCO",
		AuthToken: "tok-otherco",
		OrgName:   "OtherCo",
	})
	if err == nil {
		t.Fatal("expected a conflict error, got nil")
	}
	var ce *clierr.Error
	if !errorsAs(err, &ce) {
		t.Fatalf("err type = %T, want *clierr.Error", err)
	}
	if ce.Code != clierr.CodeResourceConflict {
		t.Errorf("Code = %s, want %s", ce.Code, clierr.CodeResourceConflict)
	}
	// The existing profile must survive untouched — no clobbering, no
	// partial write.
	got, ok := cfg.Profiles["staging"]
	if !ok || got.AuthID != "MA_ACME" {
		t.Errorf("existing profile %q was modified: %v", "staging", cfg.Profiles["staging"])
	}
	if len(cfg.Profiles) != 1 {
		t.Errorf("expected no new profile on refusal, got: %v", cfg.Profiles)
	}
}

func TestPersistProfile_differentOrg_derivedName_suffixes(t *testing.T) {
	setFakeCreds(t)
	cfg := &config.Config{
		Active: "acme",
		Profiles: map[string]config.Profile{
			"acme": {AuthID: "MA_ACME", OrgName: "Acme"},
		},
	}

	// A second, different org (different auth_id) whose name normalizes to
	// the same "acme" slug despite looking different on screen, and no -n
	// was given — the CLI picked the name, so it can pick a free one too.
	err := persistProfile(cfg, "default", false, loginBundle{
		AuthID:    "MA_ACME_TWO",
		AuthToken: "tok-2",
		OrgName:   "ACME!!!",
	})
	if err != nil {
		t.Fatalf("persistProfile: %v", err)
	}
	orig, ok := cfg.Profiles["acme"]
	if !ok || orig.AuthID != "MA_ACME" {
		t.Fatalf("original profile %q was clobbered: %v", "acme", cfg.Profiles["acme"])
	}
	second, ok := cfg.Profiles["acme-2"]
	if !ok || second.AuthID != "MA_ACME_TWO" {
		t.Fatalf(`expected the second org under "acme-2", got profiles: %v`, cfg.Profiles)
	}
	if cfg.Active != "acme" {
		t.Errorf("cfg.Active = %q, want unchanged %q (first profile still wins)", cfg.Active, "acme")
	}
}

// TestPersistProfile_reLoginAfterSuffix_reusesSameSuffixedSlot: the
// derived name is recomputed from scratch on every login ("acme", not a
// remembered "acme-2"), so a naive suffix search would walk past its own
// slot and mint "acme-3" on every refresh. It must land back on "acme-2"
// instead.
func TestPersistProfile_reLoginAfterSuffix_reusesSameSuffixedSlot(t *testing.T) {
	setFakeCreds(t)
	cfg := &config.Config{
		Active: "acme",
		Profiles: map[string]config.Profile{
			"acme":   {AuthID: "MA_ACME", OrgName: "Acme"},
			"acme-2": {AuthID: "MA_ACME_TWO", OrgName: "ACME!!!"},
		},
	}

	err := persistProfile(cfg, "default", false, loginBundle{
		AuthID:    "MA_ACME_TWO",
		AuthToken: "tok-refreshed-2",
		OrgName:   "ACME!!!",
	})
	if err != nil {
		t.Fatalf("persistProfile: %v", err)
	}
	if len(cfg.Profiles) != 2 {
		t.Fatalf("expected still exactly 2 profiles, got %d: %v", len(cfg.Profiles), cfg.Profiles)
	}
	if _, ok := cfg.Profiles["acme-3"]; ok {
		t.Errorf("should not have minted acme-3, got profiles: %v", cfg.Profiles)
	}
	got, ok := cfg.Profiles["acme-2"]
	if !ok || got.AuthID != "MA_ACME_TWO" || got.AuthToken != "" {
		t.Fatalf(`expected "acme-2" refreshed in place, got: %+v`, cfg.Profiles["acme-2"])
	}
}

func TestPersistProfile_secondLogin_doesNotStealActive(t *testing.T) {
	setFakeCreds(t)
	cfg := &config.Config{Profiles: map[string]config.Profile{}}

	if err := persistProfile(cfg, "default", false, loginBundle{AuthID: "MA_ONE", AuthToken: "tok"}); err != nil {
		t.Fatalf("persistProfile (first): %v", err)
	}
	if cfg.Active != "default" {
		t.Fatalf("cfg.Active = %q, want %q", cfg.Active, "default")
	}

	if err := persistProfile(cfg, "default", false, loginBundle{AuthID: "MA_TWO", AuthToken: "tok2", OrgName: "Second Org"}); err != nil {
		t.Fatalf("persistProfile (second): %v", err)
	}
	if cfg.Active != "default" {
		t.Errorf("cfg.Active changed to %q after a second login, want unchanged %q", cfg.Active, "default")
	}
}
