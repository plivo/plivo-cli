package cmd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/zalando/go-keyring"
)

// gateway stands in for the gateway: it keeps every request and answers as told.
type gateway struct {
	srv        *httptest.Server
	mu         sync.Mutex
	reqs       []*http.Request
	code, post int // the status, and the status of a POST when it differs
	hdr        map[string]string
	body       string
}

func newGateway(t *testing.T) *gateway {
	h := &gateway{code: 200, body: `{"name":"Acme"}`}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.reqs = append(h.reqs, r.Clone(context.Background()))
		for k, v := range h.hdr {
			w.Header().Set(k, v)
		}
		code := h.code
		if r.Method == http.MethodPost && h.post != 0 {
			code = h.post
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(h.body))
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *gateway) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.reqs)
}

const (
	fakeSignIn = "stk_fake_session"
	fakeKey    = "ci-not-a-real-key"
)

// profileOn is a home whose active profile "acme" has the extra TOML given.
func profileOn(t *testing.T, profile string) {
	t.Helper()
	writeProfile(t, setEmptyHome(t), "active = \"acme\"\n\n[profiles.acme]\nauth_id = \"CIFAKEA\"\n"+profile)
}

func onServer(url string) string { return "session_server = \"" + url + "\"\n" }

// signInFrom stores a sign-in issued by server that runs out after ttl.
func signInFrom(t *testing.T, server string, ttl time.Duration) {
	t.Helper()
	if err := config.Update(func(c *config.Config) error {
		config.SetSession(c, fakeSignIn, config.UserSession{ExpiresAt: time.Now().Add(ttl), Server: server})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// keylessOn is a home whose profile "acme" runs on a live sign-in issued by h.
func keylessOn(t *testing.T, h *gateway) {
	t.Helper()
	profileOn(t, onServer(h.srv.URL))
	signInFrom(t, h.srv.URL, time.Hour)
}

// A profile on the sign-in sends Bearer and X-Plivo-Org, never Basic, with
// telemetry on or off, to the server that issued it. A profile that holds a key
// sends exactly the headers it always did: Basic, and nothing new.
func TestKeyless_credentialSchemesThroughTheResolver(t *testing.T) {
	inline := "auth_token = \"" + fakeKey + "\"\n"
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("CIFAKEA:"+fakeKey))
	cases := []struct {
		name string
		set  func(h *gateway)
	}{
		{"keyless", func(h *gateway) { keylessOn(t, h) }},
		{"inline key", func(*gateway) { profileOn(t, inline) }},
		{"keychain key", func(*gateway) { profileOn(t, ""); _ = config.SetToken("acme", fakeKey) }},
		{"key and a live sign-in", func(h *gateway) { profileOn(t, inline); signInFrom(t, h.srv.URL, time.Hour) }},
	}
	// Every header name sent, less X-Plivo-Org, is one the CLI has always sent: no
	// credential adds one.
	sent := []string{"Accept", "Accept-Encoding", "Authorization", "Client-Type", "Client-Version", "User-Agent",
		"X-Plivo-Cli-Arch", "X-Plivo-Cli-Command", "X-Plivo-Cli-Os", "X-Plivo-Cli-Version"}
	want := map[string][]string{"0": sent, "1": slices.Insert(slices.Clone(sent), 7, "X-Plivo-Cli-Auth-Id")}
	for _, telemetry := range []string{"1", "0"} {
		for _, c := range cases {
			h := newGateway(t)
			c.set(h)
			apiURL := h.srv.URL + "/v1/cli/api" // a keyless profile derives its base from its issuer instead
			if c.name == "keyless" {
				apiURL = ""
			}
			t.Setenv("PLIVO_API_URL", apiURL)
			t.Setenv("PLIVO_CLI_TELEMETRY", telemetry)
			if err, _, _ := execCmd(t, "auth", "whoami"); err != nil || h.count() != 1 {
				t.Fatalf("%s: err %v, requests %d", c.name, err, h.count())
			}
			req := h.reqs[0]
			wantAuth, wantOrg := basic, ""
			if c.name == "keyless" {
				wantAuth, wantOrg = "Bearer "+fakeSignIn, "CIFAKEA"
			}
			if got := req.Header.Values("Authorization"); len(got) != 1 || got[0] != wantAuth || req.Header.Get("X-Plivo-Org") != wantOrg ||
				(c.name == "keyless" && !strings.HasPrefix(req.URL.Path, "/v1/cli/api/v1/Account/CIFAKEA/")) {
				t.Errorf("%s, telemetry %s: Authorization %q, X-Plivo-Org %q, path %s", c.name, telemetry, got, req.Header.Get("X-Plivo-Org"), req.URL.Path)
			}
			var set []string
			for k := range req.Header {
				if k != "X-Plivo-Org" {
					set = append(set, k)
				}
			}
			slices.Sort(set)
			if !slices.Equal(set, want[telemetry]) {
				t.Errorf("%s, telemetry %s: headers %q, want %q", c.name, telemetry, set, want[telemetry])
			}
		}
	}
}

// A profile with neither a key nor a sign-in is AUTH_MISSING; one whose sign-in
// is gone, expired or from another server is AUTH_EXPIRED; one whose server is
// not https or local is BAD_INPUT. None sends a request, on any command path.
func TestKeyless_deadSignInFailsLocally(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(h *gateway)
		want clierr.Code
	}{
		{"no key, no sign-in", func(*gateway) { profileOn(t, "") }, clierr.CodeAuthMissing},
		{"expired", func(h *gateway) { profileOn(t, onServer(h.srv.URL)); signInFrom(t, h.srv.URL, -time.Minute) }, clierr.CodeAuthExpired},
		{"not on this machine", func(h *gateway) { profileOn(t, onServer(h.srv.URL)) }, clierr.CodeAuthExpired},
		{"another server's", func(h *gateway) {
			profileOn(t, onServer("https://other.example.test"))
			signInFrom(t, h.srv.URL, time.Hour)
		}, clierr.CodeAuthExpired},
		{"token gone from the keychain", func(h *gateway) { keylessOn(t, h); _ = config.DeleteSessionToken() }, clierr.CodeAuthExpired},
		// A config.toml edit must not send the sign-in to a server a credential may not go to.
		{"server is plain http off this machine", func(*gateway) {
			profileOn(t, onServer("http://auth.example.test"))
			signInFrom(t, "http://auth.example.test", time.Hour)
		}, clierr.CodeBadInput},
	} {
		for _, args := range [][]string{{"auth", "whoami"}, {"api", "GET", "/v1/Account/CIFAKEA/"}, {"--profile", "acme", "auth", "whoami"}} {
			h := newGateway(t)
			tc.set(h)
			if err, _, _ := execCmd(t, args...); !isCode(err, tc.want) || h.count() != 0 {
				t.Errorf("%s, %s: err %v, requests %d; want %s and no request", tc.name, args[0], err, h.count(), tc.want)
			}
		}
	}
}

// Only a 401 that says invalid_token ends the sign-in, and only a request that
// carried it to its issuer can end it; the fixed 403 is the can't-use-the-CLI
// refusal; every other 401 or 403 keeps its usual code.
func TestKeyless_gateAnswers(t *testing.T) {
	invalid := map[string]string{"WWW-Authenticate": `Bearer realm="plivo", error="invalid_token"`}
	for _, tc := range []struct {
		name       string
		code       int
		hdr        map[string]string
		body       string
		want       clierr.Code
		drop, tick bool // the sign-in is dropped; the refusal is the gate's organization one
	}{
		{"invalid_token", 401, invalid, ``, clierr.CodeAuthExpired, true, false},
		{"another 401", 401, nil, `{"error":"nope"}`, clierr.CodeAuthInvalid, false, false},
		{"the fixed 403", 403, nil, `{"errors":{"error":"organization_not_allowed","global_error":"x"}}`, clierr.CodeAuthForbidden, false, true},
		{"403 by message only", 403, nil, `{"errors":{"global_error":"This sign-in cannot use that organization"}}`, clierr.CodeAuthForbidden, false, false},
		{"geo 403", 403, nil, `{"error":"geo-permission denied"}`, clierr.CodeGeoPermissionDenied, false, false},
	} {
		for _, args := range [][]string{{"auth", "whoami"}, {"api", "GET", "/v1/Account/CIFAKEA/"}} {
			h := newGateway(t)
			h.code, h.hdr, h.body = tc.code, tc.hdr, tc.body
			keylessOn(t, h)
			err, _, _ := execCmd(t, args...)
			cfg, _ := config.Load()
			tok, _ := config.GetSessionToken()
			var ce *clierr.Error
			if !errors.As(err, &ce) || ce.Code != tc.want || (cfg.User == nil) != tc.drop || (tok == "") != tc.drop ||
				len(cfg.Profiles) != 1 || strings.Contains(ce.Message, "can't use the CLI") != tc.tick {
				t.Errorf("%s via %s: err %v, sign-in dropped %v; want %s, dropped %v, org refusal %v", tc.name, args[0], err, cfg.User == nil, tc.want, tc.drop, tc.tick)
			}
		}
	}

	// A key profile's request never ends the sign-in, whatever it is answered.
	h := newGateway(t)
	h.code, h.hdr = 401, invalid
	profileOn(t, "auth_token = \""+fakeKey+"\"\n")
	signInFrom(t, h.srv.URL, time.Hour)
	t.Setenv("PLIVO_API_URL", h.srv.URL+"/v1/cli/api")
	err, _, _ := execCmd(t, "auth", "whoami")
	if cfg, _ := config.Load(); cfg.User == nil || !isCode(err, clierr.CodeAuthInvalid) {
		t.Errorf("Basic request answered invalid_token: err %v, sign-in kept %v; want AUTH_INVALID and the sign-in kept", err, cfg.User != nil)
	}

	// The loopback override gets the sign-in, but it is not the issuer: it cannot end it.
	h = newGateway(t)
	h.code, h.hdr = 401, invalid
	profileOn(t, onServer("https://issuer.example.test"))
	signInFrom(t, "https://issuer.example.test", time.Hour)
	t.Setenv("PLIVO_API_URL", h.srv.URL+"/v1/cli/api")
	err, _, _ = execCmd(t, "auth", "whoami")
	if cfg, _ := config.Load(); h.count() != 1 || cfg.User == nil || !isCode(err, clierr.CodeAuthExpired) {
		t.Errorf("loopback override: err %v, requests %d, sign-in kept %v", err, h.count(), cfg.User != nil)
	}

	// A --header that replaces the Authorization is the caller's token, not the sign-in: its 401 cannot end it.
	h = newGateway(t)
	h.code, h.hdr = 401, invalid
	keylessOn(t, h)
	t.Setenv("PLIVO_API_URL", h.srv.URL+"/v1/cli/api")
	_, _, _ = execCmd(t, "api", "GET", "/v1/Account/CIFAKEA/", "--header", "Authorization: Bearer caller-supplied")
	if cfg, _ := config.Load(); h.count() != 1 || h.reqs[0].Header.Get("Authorization") != "Bearer caller-supplied" || cfg.User == nil {
		t.Errorf("replaced Authorization: requests %d, sign-in kept %v; want the caller's token sent and the sign-in kept", h.count(), cfg.User != nil)
	}
}

// The sign-in goes only where it was issued: an assistant URL that is another
// server is refused, with nothing sent to either.
func TestKeyless_issuerPinning(t *testing.T) {
	h, other := newGateway(t), newGateway(t)
	keylessOn(t, h)
	t.Setenv("PLIVO_BUDDY_URL", other.srv.URL)
	for _, args := range [][]string{{"ask", "hello"}, {"support"}} {
		if err, _, _ := execCmd(t, args...); !isCode(err, clierr.CodeBadInput) || h.count()+other.count() != 0 {
			t.Errorf("%s: err %v, requests %d; want BAD_INPUT and none", args[0], err, h.count()+other.count())
		}
	}
}

// Neither the sign-in nor a key is printed, in a dry run, at debug level or in
// an error.
func TestKeyless_neverPrintsTheSignIn(t *testing.T) {
	h := newGateway(t)
	h.code, h.hdr = 401, map[string]string{"WWW-Authenticate": `Bearer error="invalid_token"`}
	for _, args := range [][]string{
		{"auth", "whoami", "--log-level", "debug"},
		{"api", "POST", "/v1/Account/CIFAKEA/Call/", "--body", `{"to":"1"}`, "--dry-run", "--log-level", "debug"},
		{"org", "list", "--log-level", "debug"},
		{"voice", "calls", "get", "abc", "--dry-run", "--log-level", "debug"},
	} {
		keylessOn(t, h)
		err, stdout, stderr := execCmd(t, args...)
		if all := stdout + stderr + fmt.Sprint(err); strings.Contains(all, "stk_") || strings.Contains(all, "Bearer ") {
			t.Errorf("%v printed the sign-in:\n%s", args, all)
		}
	}
}

// auth list and org current say what each profile runs on, from the config
// alone, and a feedback report names the keyless profile's organization.
func TestKeyless_credentialShownInListAndCurrent(t *testing.T) {
	keylessOn(t, newGateway(t))
	if err := config.Update(func(c *config.Config) error {
		c.Profiles["keyed"] = config.Profile{AuthID: "CIFAKEB", AuthToken: fakeKey}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	squash := strings.NewReplacer(" ", "", "\n", "")
	_, list, _ := execCmd(t, "auth", "list", "-o", "json")
	_, table, _ := execCmd(t, "auth", "list", "-o", "table")
	_, cur, _ := execCmd(t, "org", "current", "-o", "json")
	if got := squash.Replace(list); !strings.Contains(got, `"auth_id":"CIFAKEA","org_name":"","active":true,"credential":"session"`) ||
		!strings.Contains(got, `"auth_id":"CIFAKEB","org_name":"","active":false,"credential":"key"`) || !strings.Contains(table, "CREDENTIAL") ||
		!strings.Contains(squash.Replace(cur), `"credential":"session"`) || resolveAuthIDForFeedback() != "CIFAKEA" {
		t.Errorf("list %s\ntable %s\ncurrent %s", list, table, cur)
	}
}

// stderrOf runs fn and returns what it wrote to stderr.
func stderrOf(fn func()) string {
	orig := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	fn()
	_ = w.Close()
	os.Stderr = orig
	return <-done
}

// A login that lands on a profile holding a key removes that profile's own key,
// inline or in the keychain, once the new profile is saved, and says so in one
// line, also with no keychain at all. Another profile's key is never touched,
// only named in one warning.
func TestLogin_replacingAKeyedProfileRemovesOnlyItsKey(t *testing.T) {
	t.Cleanup(keyring.MockInit)
	for _, where := range []string{"inline", "keychain", "inline, no keychain"} {
		mock := newTokenServerMock(t, http.StatusOK, `{"data":{"plivo_auth_id":"MA_FAKE_ID","org_name":"Acme",`+signInFields()+`}}`)
		client, _ := setupBrowserLoginTestEnv(t, mock.srv.URL)
		acme := config.Profile{AuthID: "MA_FAKE_ID", OrgName: "Acme"}
		if where != "keychain" {
			acme.AuthToken = fakeKey
		}
		if err := config.Update(func(c *config.Config) error {
			c.Profiles["acme"], c.Profiles["zeta"], c.Profiles["old"] = acme, config.Profile{AuthID: "MA_ZETA"}, config.Profile{AuthID: "MA_OLD", AuthToken: fakeKey}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		_ = config.SetToken("zeta", "ci-zeta-key")
		if where == "keychain" {
			_ = config.SetToken("acme", fakeKey)
		}
		holders, keychain := "old, zeta.", where != "inline, no keychain"
		if !keychain {
			keyring.MockInitWithError(errors.New("no keychain"))
			holders = "old."
		}
		var err error
		stderr := stderrOf(func() { err = redeemAndPersist(client, "s", "c", "v", "x", false) })
		c, _ := config.Load()
		own, _ := config.GetToken("acme")
		zeta, _ := config.GetToken("zeta")
		if err != nil || c.Profiles["acme"].AuthToken != "" || own != "" || (zeta == "ci-zeta-key") != keychain || c.Profiles["old"].AuthToken != fakeKey ||
			c.Profiles["acme"].Credential() != config.CredentialSession {
			t.Errorf("%s: err %v, profiles %+v, acme key %q, zeta key %q", where, err, c.Profiles, own, zeta)
		}
		if strings.Count(stderr, `"acme": its stored key was removed`) != 1 || strings.Count(stderr, "Still holding a raw key: "+holders) != 1 ||
			strings.Contains(stderr, "could not") || strings.Contains(stderr, fakeKey) || strings.Contains(stderr, "stk_") {
			t.Errorf("%s: stderr: %s", where, stderr)
		}
	}
}

// A login that replaces a sign-in revokes the old one on its server, with the old
// one as Bearer, and says so. Nothing is revoked on a first login, nor when the
// server hands back the sign-in already held.
func TestLogin_revokesTheSignInItReplaces(t *testing.T) {
	for _, c := range []struct {
		name, previous string
		revokes        int
	}{{"first login", "", 0}, {"replaced", "stk_old", 1}, {"same sign-in handed back", "stk_fake", 0}} {
		t.Run(c.name, func(t *testing.T) {
			var revoked []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == revokePath {
					revoked = append(revoked, r.Method+" "+r.Header.Get("Authorization"))
					return
				}
				_, _ = io.WriteString(w, `{"data":{"plivo_auth_id":"MA_FAKE_ID","org_name":"Acme",`+signInFields()+`}}`)
			}))
			t.Cleanup(srv.Close)
			client, _ := setupBrowserLoginTestEnv(t, srv.URL)
			if c.previous != "" {
				_ = config.Update(func(cfg *config.Config) error {
					config.SetSession(cfg, c.previous, config.UserSession{ExpiresAt: time.Now().Add(time.Hour), Server: srv.URL})
					return nil
				})
			}
			var err error
			stderr := stderrOf(func() { err = redeemAndPersist(client, "s", "c", "v", "x", false) })
			tok, _ := config.GetSessionToken()
			if err != nil || tok != "stk_fake" || len(revoked) != c.revokes || strings.Contains(stderr, "previous 30-day sign-in was replaced and ended on the server") != (c.revokes == 1) {
				t.Errorf("err %v, keychain %q, revokes %q, stderr %q; want %d revoke(s)", err, tok, revoked, stderr, c.revokes)
			}
			if c.revokes == 1 && revoked[0] != "POST Bearer stk_old" {
				t.Errorf("revoke = %q, want POST with the old sign-in as Bearer", revoked[0])
			}
		})
	}
}

// The assistant's stream carries the sign-in as well, and a dead one ends it
// as it does anywhere else, instead of reading as a network failure.
func TestKeyless_askCarriesTheSignInAndMapsADeadOne(t *testing.T) {
	h := newGateway(t)
	h.post, h.hdr = 401, map[string]string{"WWW-Authenticate": `Bearer error="invalid_token"`}
	keylessOn(t, h)
	err, _, _ := execCmd(t, "ask", "hello")
	cfg, _ := config.Load()
	if h.count() != 2 || !isCode(err, clierr.CodeAuthExpired) || cfg.User != nil {
		t.Fatalf("err %v, requests %d, sign-in kept %v; want AUTH_EXPIRED after the chat request and the sign-in dropped", err, h.count(), cfg.User != nil)
	}
	if chat := h.reqs[1]; chat.URL.Path != "/v1/cli/cx/v1/aiassist/buddy-ext/chat" || chat.Header.Get("Authorization") != "Bearer "+fakeSignIn || chat.Header.Get("X-Plivo-Org") != "CIFAKEA" {
		t.Errorf("chat request %s, Authorization %q, X-Plivo-Org %q", chat.URL.Path, chat.Header.Get("Authorization"), chat.Header.Get("X-Plivo-Org"))
	}
}

// `plivo api` shares the helper: a sign-in that is gone sends nothing.
func TestDoAPIRequest_deadSignInSendsNothing(t *testing.T) {
	h := newGateway(t)
	c := api.New("CIFAKEA", "", time.Second)
	c.Session = &api.Session{Issuer: api.Origin(h.srv.URL), Origins: []string{api.Origin(h.srv.URL)}}
	if _, _, err := doAPIRequest(c, "GET", h.srv.URL, nil, nil); !isCode(err, clierr.CodeAuthExpired) || h.count() != 0 {
		t.Errorf("err %v, requests %d; want AUTH_EXPIRED and none", err, h.count())
	}
}
