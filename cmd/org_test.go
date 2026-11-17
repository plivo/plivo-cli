package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
)

// clierrHint is the hint of a CLI error, "" for any other error.
func clierrHint(err error) string {
	var e *clierr.Error
	if errors.As(err, &e) {
		return e.Hint
	}
	return ""
}

// orgServer stands in for the auth server's organization endpoint.
type orgServer struct {
	srv      *httptest.Server
	mu       sync.Mutex
	hits     []string // "METHOD path, Authorization, org X-Plivo-Org"
	listCode int
	orgs     []orgEntry
	failBody string // body of a failing reply, when set
	gateCode int    // status of every request that is not the list, when set
	onList   func() // runs while the list is answered, when set
}

var testOrgs = []orgEntry{
	{AuthID: "CIFAKEA", OrgName: "Acme Inc", Region: "us"},
	{AuthID: "CIFAKEB", OrgName: "Beta Ltd", Region: "in"},
}

func newOrgServer(t *testing.T) *orgServer {
	s := &orgServer{listCode: 200, orgs: append([]orgEntry(nil), testOrgs...)}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.hits = append(s.hits, r.Method+" "+r.URL.Path+", "+r.Header.Get("Authorization")+", org "+r.Header.Get("X-Plivo-Org"))
		isList := r.URL.Path == "/v1/accounts/cli/orgs"
		if isList && s.onList != nil {
			s.onList()
		}
		if !isList && s.gateCode != 0 {
			w.WriteHeader(s.gateCode)
			_, _ = w.Write([]byte(`{"errors":{"global_error":"This sign-in cannot use that organization","error":"organization_not_allowed"}}`))
			return
		}
		b, _ := json.Marshal(map[string]any{"data": map[string]any{"organizations": s.orgs}})
		body := string(b)
		if s.listCode >= 500 && s.failBody != "" {
			body = s.failBody
		}
		if s.listCode == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", `Bearer realm="plivo", error="invalid_token"`)
		}
		w.WriteHeader(s.listCode)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *orgServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hits)
}

const orgProfiles = "active = \"acme\"\n\n[profiles.acme]\nauth_id = \"CIFAKEA\"\norg_name = \"Acme Inc\"\n\n" +
	"[profiles.beta-old]\nauth_id = \"CIFAKEB\"\norg_name = \"Beta Ltd\"\ndefault_subaccount = \"sub1\"\n"

// orgEnv gives a temp home with orgProfiles and a live sign-in for the fake server.
func orgEnv(t *testing.T, expires time.Duration) *orgServer {
	t.Helper()
	home := setEmptyHome(t)
	s := newOrgServer(t)
	t.Setenv("PLIVO_BUDDY_URL", s.srv.URL)
	writeProfile(t, home, orgProfiles)
	if err := config.Update(func(c *config.Config) error {
		config.SetSession(c, "stk_fake_session", config.UserSession{
			ExpiresAt: time.Now().Add(expires), Server: s.srv.URL, Email: "pat@example.test", Name: "Pat"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOrgList_marksActiveAndNeverPrintsATokenOrSendsItElsewhere(t *testing.T) {
	s := orgEnv(t, time.Hour)
	err, stdout, _ := execCmd(t, "-o", "json", "org", "list")
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Data []struct {
			AuthID, Profile string
			Active          bool
		} `json:"data"`
	}
	if json.Unmarshal([]byte(strings.ReplaceAll(stdout, `"auth_id"`, `"AuthID"`)), &env) != nil || len(env.Data) != 2 ||
		!env.Data[0].Active || env.Data[1].Profile != "beta-old" || env.Data[1].Active {
		t.Fatalf("list = %s", stdout)
	}
	if strings.Contains(stdout, "stk_") || strings.Contains(stdout, "token") {
		t.Errorf("list output carries a token: %s", stdout)
	}
	if len(s.hits) != 1 || s.hits[0] != "GET "+orgListPath+", Bearer stk_fake_session, org " {
		t.Errorf("requests = %q", s.hits)
	}
}

func TestOrgUse_savesTheOrgAsAProfileAndActivatesIt(t *testing.T) {
	orgEnv(t, time.Hour)
	// Not saved yet: a new profile, named after the org.
	cfgPath := func() *config.Config { c, _ := config.Load(); return c }
	if err := config.Update(func(c *config.Config) error { delete(c.Profiles, "beta-old"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err, _, _ := execCmd(t, "org", "use", "beta ltd"); err != nil {
		t.Fatal(err)
	}
	c := cfgPath()
	p := c.Profiles["beta-ltd"]
	if tok, _ := config.GetToken("beta-ltd"); c.Active != "beta-ltd" || p.AuthID != "CIFAKEB" || p.Email != "pat@example.test" ||
		p.Region != "in" || p.SessionServer != c.User.Server || p.Credential() != config.CredentialSession || tok != "" || p.AuthToken != "" {
		t.Errorf("active %q, profile %+v, keychain token %q: want a keyless profile on the sign-in", c.Active, p, tok)
	}
}

func TestOrgUse_refreshesASavedOrgInPlace(t *testing.T) {
	orgEnv(t, time.Hour)
	if err, _, _ := execCmd(t, "org", "use", "CIFAKEB"); err != nil {
		t.Fatal(err)
	}
	c, _ := config.Load()
	if p := c.Profiles["beta-old"]; c.Active != "beta-old" || len(c.Profiles) != 2 || p.DefaultSubaccount != "sub1" || p.Credential() != config.CredentialSession {
		t.Errorf("active %q, profiles %+v: want beta-old moved to the sign-in in place, nothing added or lost", c.Active, c.Profiles)
	}
}

// When the sign-in cannot be used, a saved org still switches, as it is, and the
// command says so; the sign-in is dropped only when its issuer called it dead.
func TestOrgUse_degradesToTheSavedProfile(t *testing.T) {
	for _, c := range []struct {
		name string
		code int
	}{
		{"server error", 503},
		{"sign-in rejected", 401},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := orgEnv(t, time.Hour)
			s.listCode = c.code
			if err := config.Update(func(cfg *config.Config) error { // beta-old runs on the sign-in
				cfg.Profiles["beta-old"] = config.Profile{AuthID: "CIFAKEB", OrgName: "Beta Ltd", SessionServer: s.srv.URL}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			err, _, stderr := execCmd(t, "org", "use", "Beta Ltd")
			cfg, _ := config.Load()
			if err != nil || cfg.Active != "beta-old" || !strings.Contains(stderr, "could not be used") {
				t.Fatalf("err %v, active %q, stderr %q: want the saved profile activated with a warning", err, cfg.Active, stderr)
			}
			// "Fails until you run plivo login" is true only for a sign-in that is gone, not a server that is down.
			if dead := strings.Contains(stderr, "fail until you run `plivo login`"); dead != (c.code == 401) {
				t.Errorf("stderr %q: the log-in-again line is %v, want it only for a rejected sign-in", stderr, dead)
			}
			if gone := cfg.User == nil; gone != (c.code == 401) || len(cfg.Profiles) != 2 {
				t.Errorf("sign-in dropped = %v (want only on 401), profiles %d (want 2)", gone, len(cfg.Profiles))
			}
		})
	}
}

func TestOrgUse_changesNothingWhenNoServerAnswerOrAnOrgIsNotCovered(t *testing.T) {
	for _, c := range []struct {
		name, arg string
		setup     func(*orgServer)
	}{
		{"not covered, though saved", "CIFAKEB", func(s *orgServer) { s.orgs = s.orgs[:1] }},
		{"not saved and the server is down", "Gamma", func(s *orgServer) { s.listCode = 503 }},
		{"not among the approved orgs", "Gamma", func(*orgServer) {}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := orgEnv(t, time.Hour)
			c.setup(s)
			before, _ := config.Load()
			if err, _, _ := execCmd(t, "org", "use", c.arg); err == nil {
				t.Fatal("want an error")
			}
			after, _ := config.Load()
			if after.Active != before.Active || len(after.Profiles) != len(before.Profiles) || after.User == nil {
				t.Errorf("state changed: active %q -> %q, profiles %d -> %d", before.Active, after.Active, len(before.Profiles), len(after.Profiles))
			}
		})
	}
}

// The sign-in token goes to the server that issued it and nowhere else, and not
// at all once it has expired.
func TestOrgSession_notSentWhenExpiredOrServerOverridden(t *testing.T) {
	s := orgEnv(t, -time.Minute) // expired
	if err, _, _ := execCmd(t, "org", "list"); err == nil {
		t.Error("org list with an expired sign-in: want an error")
	}
	if err, _, stderr := execCmd(t, "org", "use", "Beta Ltd"); err != nil || !strings.Contains(stderr, "expired") {
		t.Errorf("saved org, expired sign-in: err %v, stderr %q; want a local switch", err, stderr)
	}
	if s.count() != 0 {
		t.Errorf("an expired sign-in made %d request(s)", s.count())
	}

	live := orgEnv(t, time.Hour)
	other := newOrgServer(t)
	t.Setenv("PLIVO_BUDDY_URL", other.srv.URL) // not the server that issued it
	if err, _, _ := execCmd(t, "org", "list"); err == nil {
		t.Error("org list against another server: want a refusal")
	}
	if other.count() != 0 || live.count() != 0 {
		t.Errorf("token sent: other server %d, issuing server %d", other.count(), live.count())
	}
}

func TestOrgUse_dryRunMakesNoCallAndChangesNothing(t *testing.T) {
	s := orgEnv(t, time.Hour)
	before, _ := config.Load()
	for _, args := range [][]string{{"org", "use", "CIFAKEB"}, {"org", "list"}} {
		if err, _, stderr := execCmd(t, append(args, "--dry-run")...); err != nil || !strings.HasPrefix(stderr, "[dry-run] Would ") {
			t.Errorf("%v --dry-run: %v, %q", args, err, stderr)
		}
	}
	after, _ := config.Load()
	if s.count() != 0 || after.Active != before.Active || len(after.Profiles) != len(before.Profiles) {
		t.Errorf("--dry-run made %d call(s) or changed the config", s.count())
	}
}

func TestOrgUse_namesEachCandidateWhenAmbiguous(t *testing.T) {
	orgEnv(t, time.Hour)
	if err := config.Update(func(c *config.Config) error {
		c.Profiles["beta-2"] = config.Profile{AuthID: "CIFAKEC", OrgName: "Beta Ltd"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err, _, _ := execCmd(t, "org", "use", "Beta Ltd"); err == nil || !strings.Contains(err.Error(), "beta-2") || !strings.Contains(err.Error(), "beta-old") {
		t.Errorf("err = %v, want both profiles named", err)
	}
}

func TestOrgCurrent_isLocal(t *testing.T) {
	s := orgEnv(t, time.Hour)
	err, stdout, _ := execCmd(t, "-o", "json", "org", "current")
	if err != nil || !strings.Contains(stdout, `"org_name": "Acme Inc"`) && !strings.Contains(stdout, `"org_name":"Acme Inc"`) || s.count() != 0 {
		t.Errorf("err %v, out %s, calls %d", err, stdout, s.count())
	}
}

// The sign-in ends with the last profile, not before: other saved orgs still
// need it to switch, and a dry run removes nothing. Config and keychain are
// checked apart, through both commands that remove a profile. It is revoked on
// its server then, once.
func TestLogout_endsTheSignInWithTheLastProfileOnly(t *testing.T) {
	for _, last := range [][]string{{"logout", "acme"}, {"auth", "remove", "acme"}} {
		t.Run(last[0], func(t *testing.T) {
			s := orgEnv(t, time.Hour)
			state := func() (inConfig, inKeychain bool) {
				c, _ := config.Load()
				tok, _ := config.GetSessionToken()
				return c.User != nil, tok != ""
			}
			if err, _, stderr := execCmd(t, "logout", "beta-old"); err != nil || !strings.Contains(stderr, "still on this machine") {
				t.Fatalf("logout of one of two profiles: err %v, stderr %q; want a note that the sign-in remains", err, stderr)
			}
			if a, b := state(); !a || !b {
				t.Fatalf("one of two profiles removed: sign-in in config %v, keychain %v; want both kept", a, b)
			}
			if err, _, stderr := execCmd(t, append(last, "--dry-run")...); err != nil || !strings.Contains(stderr, "the 30-day sign-in would end too") {
				t.Fatalf("dry run of the last profile: err %v, stderr %q; want it to say the sign-in ends", err, stderr)
			}
			if a, b := state(); !a || !b {
				t.Fatalf("dry run removed the sign-in: config %v, keychain %v", a, b)
			}
			if s.count() != 0 {
				t.Fatalf("%d request(s) before the last profile went; the sign-in is still in use", s.count())
			}
			err, _, stderr := execCmd(t, last...)
			if a, b := state(); err != nil || a || b || !strings.Contains(stderr, "ended on the server") ||
				len(s.hits) != 1 || s.hits[0] != "POST "+revokePath+", Bearer stk_fake_session, org " {
				t.Errorf("last profile removed: err %v, sign-in in config %v, keychain %v, stderr %q, requests %q; want it gone from both and one revoke with the sign-in as Bearer", err, a, b, stderr, s.hits)
			}
		})
	}
}

// Revoking is best effort: whatever the server answers, the sign-in is gone
// from this machine, and the one line says whether it was ended there. An
// expired sign-in needs no call.
func TestLogout_signInLeavesTheMachineWhateverTheServerAnswers(t *testing.T) {
	for _, c := range []struct {
		name    string
		code    int
		expires time.Duration
		want    string
		calls   int
	}{
		{"server error", 503, time.Hour, "could not be ended on the server (HTTP 503): it stays valid there until", 1},
		{"already dead", 401, time.Hour, "removed from this machine and ended on the server", 1},
		{"expired", 200, -time.Minute, "had already expired", 0},
		{"unreachable", 0, time.Hour, "but the server could not be reached: it may still be valid there until", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := orgEnv(t, c.expires)
			s.listCode = c.code
			_, _, _ = execCmd(t, "logout", "beta-old")
			if c.code == 0 {
				s.srv.Close()
			}
			err, _, stderr := execCmd(t, "logout", "acme")
			cfg, _ := config.Load()
			tok, _ := config.GetSessionToken()
			if err != nil || cfg.User != nil || tok != "" || !strings.Contains(stderr, c.want) || s.count() != c.calls {
				t.Errorf("err %v, sign-in kept %v/%v, stderr %q, requests %d; want it removed, %q and %d request(s)", err, cfg.User != nil, tok != "", stderr, s.count(), c.want, c.calls)
			}
		})
	}
}

// Two saved profiles can hold one org: the one named is the one refreshed and
// activated, and the list marks the one in use.
func TestOrgUse_twinProfilesAndPreservedSettings(t *testing.T) {
	orgEnv(t, time.Hour)
	if err := config.Update(func(c *config.Config) error {
		c.Profiles["beta-old"] = config.Profile{AuthID: "CIFAKEB", OrgName: "Beta Ltd", Env: "dev", DefaultSubaccount: "sub1", Email: "old@example.test"}
		c.Profiles["a-twin"] = config.Profile{AuthID: "CIFAKEB", OrgName: "Beta Ltd"}
		c.Active = "a-twin"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err, _, _ := execCmd(t, "org", "use", "beta-old"); err != nil {
		t.Fatal(err)
	}
	c, _ := config.Load()
	if c.Active != "beta-old" || c.Profiles["beta-old"].SessionServer == "" || c.Profiles["a-twin"].SessionServer != "" {
		t.Errorf("active %q, profiles %+v: want beta-old moved to the sign-in and active, a-twin untouched", c.Active, c.Profiles)
	}
	if p := c.Profiles["beta-old"]; p.Env != "dev" || p.DefaultSubaccount != "sub1" {
		t.Errorf("profile after refresh = %+v, want env and default_subaccount kept", p)
	}
	_, stdout, _ := execCmd(t, "-o", "json", "org", "list")
	if !strings.Contains(stdout, `"profile": "beta-old"`) && !strings.Contains(stdout, `"profile":"beta-old"`) {
		t.Errorf("org list should name the profile in use for the org, got %s", stdout)
	}
}

// A name that more than one organization answers to is never picked for the
// caller: not when two approved orgs share it, and not when a saved profile
// of that name holds a different org than an approved org of that name.
func TestOrgUse_ambiguousNameIsAnErrorEvenWhenOneIsSaved(t *testing.T) {
	for _, c := range []struct{ name, query string }{
		{"two approved orgs share a name", "Acme Inc"},
		{"profile name equals another org's name", "acme"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := orgEnv(t, time.Hour)
			s.orgs = append(s.orgs, orgEntry{AuthID: "CIFAKEA2", OrgName: "Acme Inc"}, orgEntry{AuthID: "CIFAKEQ", OrgName: "acme"})
			before, _ := config.Load()
			err, _, _ := execCmd(t, "org", "use", c.query)
			after, _ := config.Load()
			if err == nil || !strings.Contains(err.Error(), "organizations") || after.Active != before.Active {
				t.Errorf("err = %v, active %q -> %q; want an ambiguity error and no switch", err, before.Active, after.Active)
			}
		})
	}
}

// A stored token the CLI cannot send as a Bearer is not sent at all.
func TestOrgSession_refusesANonBearerToken(t *testing.T) {
	s := orgEnv(t, time.Hour)
	if err := config.SetSessionToken("not-a-session-token"); err != nil {
		t.Fatal(err)
	}
	if err, _, _ := execCmd(t, "org", "list"); err == nil || s.count() != 0 {
		t.Errorf("err %v, requests %d: want a refusal before any request", err, s.count())
	}
}

// Text the server supplies is printed escaped: an org name in a switch, in a
// local fallback, and the cause of that fallback.
func TestOrgUse_escapesServerText(t *testing.T) {
	const evil = "Evil\x1b[2J Inc"
	check := func(t *testing.T, err error, stderr string) {
		t.Helper()
		if err != nil || !strings.Contains(stderr, "Evil") || strings.ContainsRune(stderr, 0x1b) {
			t.Errorf("err %v, stderr %q: want success with the name printed and no escape byte", err, stderr)
		}
	}
	t.Run("new org from the list", func(t *testing.T) {
		s := orgEnv(t, time.Hour)
		s.orgs[1].OrgName = evil
		err, _, stderr := execCmd(t, "org", "use", "CIFAKEB")
		check(t, err, stderr)
	})
	t.Run("saved org after the server failed", func(t *testing.T) {
		s := orgEnv(t, time.Hour)
		s.listCode = 503
		s.failBody = `{"global_error":"down \u001b[2J\u001b]0;x\u0007"}`
		if err := config.Update(func(c *config.Config) error {
			c.Profiles["acme"] = config.Profile{AuthID: "CIFAKEA", OrgName: evil}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		err, _, stderr := execCmd(t, "org", "use", "CIFAKEA")
		check(t, err, stderr)
	})
}

// The sign-in ends on a 401 that says invalid_token; the profiles are never touched.
func TestOrgList_invalidTokenDropsTheSignInButProfilesAreKept(t *testing.T) {
	s := orgEnv(t, time.Hour)
	s.listCode = 401
	err, _, _ := execCmd(t, "org", "list")
	c, _ := config.Load()
	if tok, _ := config.GetSessionToken(); err == nil || !strings.Contains(err.Error(), "AUTH_EXPIRED") || c.User != nil || tok != "" || len(c.Profiles) != 2 {
		t.Errorf("err %v, sign-in in config %v, keychain %q, profiles %d; want it dropped from both and both profiles kept", err, c.User != nil, tok, len(c.Profiles))
	}
}

// An entry with no auth ID can never be switched to, and is not listed.
func TestOrgUse_ignoresAnOrgWithNoAuthID(t *testing.T) {
	s := orgEnv(t, time.Hour)
	s.orgs = append(s.orgs, orgEntry{OrgName: "Ghost"})
	before, _ := config.Load()
	if err, _, _ := execCmd(t, "org", "use", "Ghost"); err == nil {
		t.Error("org use Ghost: want an error")
	}
	if after, _ := config.Load(); len(after.Profiles) != len(before.Profiles) {
		t.Errorf("profiles %d -> %d: an org with no auth ID was saved", len(before.Profiles), len(after.Profiles))
	}
}

// A failed save must put the keychain's sign-in back, not leave a new token
// beside the old server's name.
func TestRestoreSessionToken(t *testing.T) {
	setEmptyHome(t)
	_ = config.SetSessionToken("stk_new")
	restoreSessionToken("stk_old")
	if tok, _ := config.GetSessionToken(); tok != "stk_old" {
		t.Errorf("restored token = %q", tok)
	}
	restoreSessionToken("")
	if tok, _ := config.GetSessionToken(); tok != "" {
		t.Errorf("token after restoring none = %q", tok)
	}
}

func TestServerKey_dropsUserinfoAndTrailingSlash(t *testing.T) {
	if got := serverKey("https://user:pw@auth.example.test/"); got != "https://auth.example.test" {
		t.Errorf("serverKey = %q", got)
	}
}

// An org the sign-in does not cover, though saved, names the way to use the saved
// profile as it is, unless that profile runs on the sign-in too.
func TestOrgUse_forbiddenHintNamesAuthUse(t *testing.T) {
	s := orgEnv(t, time.Hour)
	s.orgs = s.orgs[:1]
	if err, _, _ := execCmd(t, "org", "use", "CIFAKEB"); err == nil || !strings.Contains(clierrHint(err), "plivo auth use beta-old") {
		t.Errorf("err = %v, want a hint naming `plivo auth use beta-old`", err)
	}
	_ = config.Update(func(c *config.Config) error {
		c.Profiles["beta-old"] = config.Profile{AuthID: "CIFAKEB", SessionServer: s.srv.URL}
		return nil
	})
	if err, _, _ := execCmd(t, "org", "use", "CIFAKEB"); err == nil || strings.Contains(clierrHint(err), "auth use") {
		t.Errorf("keyless profile: err = %v, want no `plivo auth use` hint", err)
	}
}

// org list marks the organization --profile names as the one in use, as org current does.
func TestOrgList_honoursTheProfileFlag(t *testing.T) {
	orgEnv(t, time.Hour)
	_, stdout, _ := execCmd(t, "-o", "json", "--profile", "beta-old", "org", "list")
	compact := strings.NewReplacer(" ", "", "\n", "").Replace(stdout)
	if !strings.Contains(compact, `"auth_id":"CIFAKEB","region":"in","profile":"beta-old","active":true`) {
		t.Errorf("want beta-old marked active, got %s", stdout)
	}
}

// org use moves a saved profile that holds a key onto the sign-in and removes that
// profile's own key after the save, with one line saying so. Another profile's
// key is never touched. It asks for the list and nothing else: no credentials
// are fetched.
func TestOrgUse_convertsAKeyedProfileAndRemovesOnlyItsKey(t *testing.T) {
	s := orgEnv(t, time.Hour)
	_ = config.SetToken("acme", "ci-other-key")
	_ = config.SetToken("beta-old", fakeKey)
	err, stdout, stderr := execCmd(t, "org", "use", "CIFAKEB")
	c, _ := config.Load()
	own, _ := config.GetToken("beta-old")
	other, _ := config.GetToken("acme")
	if err != nil || own != "" || other != "ci-other-key" || s.count() != 2 || c.Profiles["beta-old"].Credential() != config.CredentialSession ||
		strings.Count(stderr, `Profile "beta-old": its stored key was removed`) != 1 || strings.Contains(stdout+stderr, fakeKey) {
		t.Errorf("err %v, own key %q, other key %q, requests %q, stderr %q", err, own, other, s.hits, stderr)
	}
	if got := s.hits[1]; !strings.Contains(got, "/v1/cli/api/") || !strings.HasSuffix(got, "org CIFAKEB") {
		t.Errorf("the key was removed without a request as the organization through the gate: %q", got)
	}
}

// The list is only what was ticked: when the gate refuses the organization (a
// role that lost API credentials, or Require SSO), the profile keeps its key.
func TestOrgUse_aGateRefusalKeepsTheKeyAndTheProfile(t *testing.T) {
	s := orgEnv(t, time.Hour)
	s.gateCode = http.StatusForbidden
	_ = config.SetToken("beta-old", fakeKey)
	err, _, stderr := execCmd(t, "org", "use", "CIFAKEB")
	c, _ := config.Load()
	own, _ := config.GetToken("beta-old")
	if !isCode(err, clierr.CodeAuthForbidden) || own != fakeKey || c.Profiles["beta-old"].Credential() != config.CredentialKey ||
		c.Active != "acme" || strings.Contains(stderr, "removed") {
		t.Errorf("err %v, own key kept %v, credential %q, active %q, stderr %q", err, own == fakeKey, c.Profiles["beta-old"].Credential(), c.Active, stderr)
	}
}

// A login that replaces the sign-in while org use runs may not cover the
// organization: nothing is saved against it.
func TestOrgUse_aLoginThatReplacesTheSignInMeanwhileSavesNothing(t *testing.T) {
	s := orgEnv(t, time.Hour)
	_ = config.SetToken("beta-old", fakeKey)
	s.onList = func() {
		_ = config.Update(func(c *config.Config) error {
			config.SetSession(c, "stk_newer_session", config.UserSession{
				ExpiresAt: time.Now().Add(48 * time.Hour), Server: s.srv.URL, Email: "pat@example.test", Name: "Pat"})
			return nil
		})
	}
	err, _, _ := execCmd(t, "org", "use", "CIFAKEB")
	c, _ := config.Load()
	own, _ := config.GetToken("beta-old")
	if !isCode(err, clierr.CodeAuthExpired) || own != fakeKey || c.Profiles["beta-old"].Credential() != config.CredentialKey || c.Active != "acme" {
		t.Errorf("err %v, key kept %v, credential %q, active %q", err, own == fakeKey, c.Profiles["beta-old"].Credential(), c.Active)
	}
}
