package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// Session is the 30-day browser sign-in a profile with no key of its own runs
// on. A request carries it as a Bearer token beside X-Plivo-Org, the auth ID of
// the organization it runs as, and never beside Basic.
type Session struct {
	Token string // the sign-in; "" once its issuer has refused it
	Org   string // X-Plivo-Org: the organization the request runs as; "" sends none

	// Issuer is the origin that issued the sign-in. Origins are the only ones it
	// is sent to: the issuer, and a loopback PLIVO_API_URL.
	Issuer  string
	Origins []string

	// OnRejected runs when the issuer answers invalid_token, to drop the stored
	// sign-in.
	OnRejected func()
}

// Auth records how one request proved who it was, to read the answer by. anon is
// a request with no credentials at all, as the login calls are.
type Auth struct{ bearer, issuer, anon bool }

// Origin is the scheme, host and port of a URL, lowercased and without a
// default port: how servers are compared. "" for anything that is not a URL.
func Origin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return originOf(u)
}

func originOf(u *url.URL) string {
	scheme, host := strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	if p := u.Port(); (scheme == "https" && p == "443") || (scheme == "http" && p == "80") {
		host = strings.TrimSuffix(host, ":"+p)
	}
	return scheme + "://" + host
}

// ApplyAuth sets the request's credentials, the one place every request path
// does it. A raw key goes as Basic, exactly as before and with no other header.
// A profile on the sign-in sends Bearer and X-Plivo-Org, only to the origins
// the sign-in may reach, and is refused here, with nothing sent, once it has
// no sign-in left. With neither, the request is anonymous (the login calls).
func (c *Client) ApplyAuth(req *http.Request) (Auth, error) {
	switch s := c.Session; {
	case c.AuthToken != "":
		req.SetBasicAuth(c.AuthID, c.AuthToken)
	case s != nil:
		if s.Token == "" {
			return Auth{}, clierr.SessionExpired()
		}
		origin := originOf(req.URL)
		if !slices.Contains(s.Origins, origin) {
			e := clierr.BadInput("the sign-in is sent only to the server that issued it, so this request was not sent")
			e.Hint = "Unset PLIVO_API_URL, PLIVO_BUDDY_URL and [buddy].url, or run `plivo login` against that server."
			return Auth{}, e
		}
		req.Header.Set("Authorization", "Bearer "+s.Token)
		if s.Org != "" {
			req.Header.Set(headerOrg, s.Org)
		}
		return Auth{bearer: true, issuer: origin == s.Issuer}, nil
	case c.AuthID != "":
		req.SetBasicAuth(c.AuthID, c.AuthToken)
	default:
		return Auth{anon: true}, nil
	}
	return Auth{}, nil
}

const headerOrg = "X-Plivo-Org"

// invalidToken matches the gate's answer to a token it does not accept.
var invalidToken = regexp.MustCompile(`(?i)(?:^|[\s,])error\s*=\s*"?invalid_token"?\s*(?:,|$)`)

// GateError reads the sign-in gate's two answers: 401 with error="invalid_token"
// (the token is dead), and the one fixed 403 whose errors.error is
// organization_not_allowed. Neither is matched on message text. It is nil for
// anything else, which keeps its usual mapping, and for a request that carried a
// key. A request that carried no credentials, the login's own /token call, is
// given only the 403, which is how login learns the account cannot use the CLI
// with that organization. A dead token from the issuer is dropped from this
// client and, through OnRejected, from the machine.
func (c *Client) GateError(a Auth, resp *http.Response, body []byte) *clierr.Error {
	var e *clierr.Error
	switch {
	case !a.bearer && !a.anon:
		return nil
	case a.bearer && resp.StatusCode == http.StatusUnauthorized && slices.ContainsFunc(resp.Header.Values("WWW-Authenticate"), invalidToken.MatchString):
		e = clierr.SessionExpired()
		if s := c.Session; a.issuer && s != nil {
			s.Token = ""
			if s.OnRejected != nil {
				s.OnRejected()
			}
		}
	case resp.StatusCode == http.StatusForbidden && orgNotAllowed(body):
		e = clierr.OrgNotAllowed()
	default:
		return nil
	}
	e.StatusCode, e.RequestID = resp.StatusCode, resp.Header.Get("X-Request-ID")
	return e
}

// orgNotAllowed reports whether a 403 body is the gate's: errors.error is
// organization_not_allowed.
func orgNotAllowed(body []byte) bool {
	var b struct{ Errors struct{ Error string } }
	return json.Unmarshal(body, &b) == nil && b.Errors.Error == "organization_not_allowed"
}
