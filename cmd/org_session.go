package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/output"
)

// Everything the CLI assumes about the auth server's organization endpoints is
// in this file: the paths, the wire shapes, and how a refusal is read. Change
// the contract here.
//
// A browser login returns a 30-day sign-in token (access_token + expires_at).
// It is the only credential the login leaves on the machine: a profile made by
// login or `plivo org use` holds no key, and its requests carry the sign-in as a
// Bearer token beside X-Plivo-Org, to the server that issued it and nowhere else.

const (
	cliClient          = "cli"                     // sent as ?client= on the authorize URL
	orgListPath        = "/v1/accounts/cli/orgs"   // GET: the organizations the sign-in covers
	revokePath         = "/v1/accounts/cli/revoke" // POST: ends the sign-in sent as the Bearer token
	sessionTokenPrefix = "stk_"                    // what a sign-in token starts with
)

// orgEntry is one organization in the list. Order is the server's.
type orgEntry struct {
	AuthID  string `json:"auth_id"`
	OrgName string `json:"org_name"`
	OrgUUID string `json:"org_uuid,omitempty"`
	Region  string `json:"region,omitempty"`
}

type orgListEnvelope struct {
	Data struct {
		Organizations []orgEntry `json:"organizations"`
	} `json:"data"`
}

func errNoSession() *clierr.Error {
	return &clierr.Error{
		Code:    clierr.CodeAuthMissing,
		Message: "this machine has no organization sign-in",
		Hint:    "Run `plivo login` and tick the organizations you want to switch between.",
	}
}

// errSessionExpired is the answer for a sign-in that ran out, was revoked or is
// gone from this machine.
func errSessionExpired() *clierr.Error { return clierr.SessionExpired() }

func errIssuerOverridden() *clierr.Error {
	e := clierr.BadInput("the auth server is overridden, so the sign-in was not sent")
	e.Hint = "The sign-in was issued by a different server. Unset PLIVO_BUDDY_URL and [buddy].url, or run `plivo login` against this one."
	return e
}

// errIssuerUnsafe is the answer for a sign-in whose recorded server is not one
// the token may be sent to. The server is not echoed: it may hold a password.
func errIssuerUnsafe() *clierr.Error {
	e := clierr.BadInput("the sign-in's server in ~/.plivo/config.toml must be https, or http to this machine, so the sign-in was not sent")
	e.Hint = "Run `plivo login` to sign in again. If you did not set [user].server or a profile's session_server, remove it."
	return e
}

// liveSession returns the sign-in's token, or why it cannot be used: it has run
// out, its server is not one a credential may be sent to (as for PLIVO_BUDDY_URL),
// or it holds nothing the CLI can send as a Bearer token.
func liveSession(u *config.UserSession) (string, error) {
	if u.Expired(time.Now()) {
		return "", errSessionExpired()
	}
	if !safeBuddyURL(u.Server) {
		return "", errIssuerUnsafe()
	}
	tok, err := u.Secret()
	if err != nil {
		return "", fmt.Errorf("reading the sign-in token from the OS keychain: %w", err)
	}
	if !strings.HasPrefix(tok, sessionTokenPrefix) {
		return "", errSessionExpired()
	}
	return tok, nil
}

// newSession is the sign-in u as the api client sends it, running as org (an
// auth ID; "" sends no X-Plivo-Org). A refusal as invalid_token from its issuer
// drops it from the machine, unless a newer login has replaced it meanwhile.
func newSession(u *config.UserSession, tok, org string) *api.Session {
	issuer := api.Origin(u.Server)
	server, expires := u.Server, u.ExpiresAt
	return &api.Session{
		Token: tok, Org: org, Issuer: issuer, Origins: []string{issuer},
		OnRejected: func() {
			_ = config.Update(func(c *config.Config) error {
				if c.User != nil && c.User.Server == server && c.User.ExpiresAt.Equal(expires) {
					config.ClearSession(c)
				}
				return nil
			})
		},
	}
}

// openSession returns a client carrying the stored sign-in, for the organization
// calls. The token is sent only to the auth server it was issued by: an override
// of that server (PLIVO_BUDDY_URL, [buddy].url) is refused instead of handed
// the token.
func openSession(cfg *config.Config) (*api.Client, error) {
	u := cfg.User
	if u == nil {
		return nil, errNoSession()
	}
	tok, err := liveSession(u)
	if err != nil {
		return nil, err
	}
	// No X-Plivo-Org: the list is the sign-in's own, and naming the active
	// profile's organization would be refused once a new login stopped covering it.
	client := api.New("", "", timeoutFlag)
	client.Session = newSession(u, tok, "")
	client.BuddyBaseURL = u.Server
	if err := applyBuddyURL(client); err != nil {
		return nil, err
	}
	if err := pinToIssuer(client); err != nil {
		return nil, err
	}
	return client, nil
}

// pinToIssuer refuses an auth server other than the one that issued the
// client's sign-in. A client with no sign-in is not held to one.
func pinToIssuer(c *api.Client) error {
	if s := c.Session; s != nil && api.Origin(c.BuddyBaseURL) != s.Issuer {
		return errIssuerOverridden()
	}
	return nil
}

// runOnSession makes c a keyless profile's client: it carries the sign-in the
// profile names, and talks to the server that issued it. A profile whose
// sign-in is gone, expired, or from another server fails here, with no request.
func runOnSession(c *api.Client, p config.Profile, name string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	u := cfg.User
	if u == nil {
		return errSessionExpired()
	}
	if u.Server != p.SessionServer {
		e := errSessionExpired()
		e.Message = "this profile was created under a sign-in from a different server"
		e.Hint = fmt.Sprintf("Run `plivo login --name %s` to sign it in again.", name)
		return e
	}
	tok, err := liveSession(u)
	if err != nil {
		return err
	}
	c.Session = newSession(u, tok, p.AuthID)
	c.BuddyBaseURL = u.Server
	if os.Getenv("PLIVO_API_URL") == "" {
		c.BaseURL = u.Server + "/v1/cli/api"
	} else {
		c.Session.Origins = append(c.Session.Origins, api.Origin(c.BaseURL))
	}
	return nil
}

// serverKey is how a server is compared and stored: without a trailing slash
// and without any user:password in the URL, which must not reach config.toml.
func serverKey(base string) string {
	if u, err := url.Parse(base); err == nil {
		u.User = nil
		base = u.String()
	}
	return strings.TrimRight(base, "/")
}

// serviceError reports a failed call. A reply the CLI could not decode is not
// echoed: api.Do quotes the whole body in that error, and the body can hold
// credentials.
func serviceError(target string, err error) *clierr.Error {
	if strings.Contains(err.Error(), "decode response") {
		return clierr.Upstream("the server sent a reply the CLI could not read, while " + target)
	}
	return clierr.NetworkError(target, err)
}

// callSession makes one call and returns the server's refusal as a *clierr.Error
// (StatusCode set), a transport failure as a network error.
func callSession(c *api.Client, method, path string, body, out any) error {
	apiErr, err := c.Do(method, c.BuddyURL(path), body, nil, out)
	if err != nil {
		return serviceError("calling the organization service", err)
	}
	if apiErr != nil {
		return apiErr
	}
	return nil
}

func fetchOrgs(c *api.Client) ([]orgEntry, error) {
	var env orgListEnvelope
	if err := callSession(c, "GET", orgListPath, nil, &env); err != nil {
		return nil, err
	}
	// An entry with no auth ID cannot be switched to, and would match an empty reply.
	orgs := make([]orgEntry, 0, len(env.Data.Organizations))
	for _, o := range env.Data.Organizations {
		if o.AuthID != "" {
			orgs = append(orgs, o)
		}
	}
	return orgs, nil
}

// expiryOf reads the sign-in's expiry; false for anything that is not an RFC 3339
// time, which loses the sign-in but never the login it came with.
func expiryOf(raw json.RawMessage) (time.Time, bool) {
	var t time.Time
	if len(raw) == 0 || json.Unmarshal(raw, &t) != nil || t.IsZero() {
		return t, false
	}
	return t, true
}

// sessionFrom checks the sign-in a login returned and gives its expiry. A reply
// with none the CLI can use is UPSTREAM_ERROR: nothing is saved, and the login
// never falls back to a key.
func sessionFrom(d cliTokenEnvelopeData, now time.Time) (time.Time, error) {
	exp, ok := expiryOf(d.ExpiresAt)
	if !strings.HasPrefix(d.AccessToken, sessionTokenPrefix) || !ok || !exp.After(now) {
		return exp, clierr.Upstream("the server returned no usable sign-in, so nothing was saved")
	}
	return exp, nil
}

// dropSessionWithLastProfile ends the organization sign-in once the last
// profile is gone: with nothing left to switch from it has no use. The caller
// deletes the keychain entry after saving.
func dropSessionWithLastProfile(cfg *config.Config) *config.UserSession {
	if len(cfg.Profiles) > 0 || cfg.User == nil {
		return nil
	}
	dropped := cfg.User
	cfg.User = nil
	return dropped
}

// isCode reports whether err is a CLI error with the code want.
func isCode(err error, want clierr.Code) bool {
	var ce *clierr.Error
	return errors.As(err, &ce) && ce.Code == want
}

// revokeSession asks the server that issued the sign-in (u, tok) to end it, with
// the sign-in as the Bearer token and to no other server. It is best effort, so
// it waits only briefly. A sign-in the server already calls dead counts as ended.
func revokeSession(u *config.UserSession, tok string) error {
	if !safeBuddyURL(u.Server) {
		return errIssuerUnsafe()
	}
	c := api.New("", "", 5*time.Second)
	c.Session = newSession(u, tok, "")
	c.Session.OnRejected = nil // the sign-in is going from the config anyway
	c.BuddyBaseURL = u.Server
	apiErr, err := c.Do("POST", c.BuddyURL(revokePath), nil, nil, nil)
	if err != nil {
		return err // NETWORK_ERROR or UPSTREAM_TIMEOUT, as endSignIn reads it
	}
	if apiErr == nil || apiErr.Code == clierr.CodeAuthExpired {
		return nil
	}
	return apiErr
}

// endSignIn revokes the sign-in (u, tok) on its server and says in one line
// whether it ended. previous is a sign-in a new login replaced; otherwise it is
// the one logout removed from this machine. The sign-in is gone from here either
// way, so a failure only changes what the line says.
func endSignIn(u *config.UserSession, tok string, previous bool) {
	if u == nil || !strings.HasPrefix(tok, sessionTokenPrefix) {
		return
	}
	what, gone := "The 30-day sign-in", "was removed from this machine"
	if previous {
		what, gone = "The previous 30-day sign-in", "was replaced"
	}
	if u.Expired(time.Now()) {
		fmt.Fprintf(os.Stderr, "  %s %s; it had already expired.\n", what, gone)
		return
	}
	err := revokeSession(u, tok)
	if err == nil {
		fmt.Fprintf(os.Stderr, "  %s %s and ended on the server.\n", what, gone)
		return
	}
	until := u.ExpiresAt.Local().Format("2006-01-02")
	// With no answer the server may have ended it anyway, so it only may be valid.
	switch {
	case isCode(err, clierr.CodeUpstreamTimeout):
		fmt.Fprintf(os.Stderr, "  %s %s, but the server did not answer in time: it may still be valid there until %s.\n", what, gone, until)
		return
	case isCode(err, clierr.CodeNetworkError):
		fmt.Fprintf(os.Stderr, "  %s %s, but the server could not be reached: it may still be valid there until %s.\n", what, gone, until)
		return
	}
	reason := err.Error()
	var ce *clierr.Error
	if errors.As(err, &ce) {
		reason = ce.Message
		if ce.StatusCode != 0 {
			reason = fmt.Sprintf("HTTP %d", ce.StatusCode)
		}
	}
	fmt.Fprintf(os.Stderr, "  %s %s, but could not be ended on the server (%s): it stays valid there until %s.\n",
		what, gone, output.SafeText(reason), until)
}

// reportDroppedSession ends the sign-in that left this machine with the last
// profile, on the server too, and says so. The keychain entry goes only after
// the token is read from it.
func reportDroppedSession(dropped *config.UserSession) {
	if dropped == nil {
		return
	}
	tok, _ := dropped.Secret()
	endSignIn(dropped, tok, false)
	_ = config.DeleteSessionToken()
}

// reportKeptSession says, after a profile is removed, that the sign-in is still
// here: the profiles that run on it keep working until it expires.
func reportKeptSession(kept *config.UserSession) {
	if kept == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "  The 30-day sign-in is still on this machine until %s, for the profiles that run on it. It is removed with the last profile.\n",
		kept.ExpiresAt.Local().Format("2006-01-02"))
}

// restoreSessionToken puts the keychain's sign-in back as it was (prev, "" for
// none) after a login whose config save failed, so a new token never sits
// beside the old server's name.
func restoreSessionToken(prev string) {
	if prev == "" {
		_ = config.DeleteSessionToken()
		return
	}
	_ = config.SetSessionToken(prev)
}
