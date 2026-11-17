package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// A client whose sign-in is gone sends nothing on any request path, and says to
// log in again (AUTH_EXPIRED, not AUTH_MISSING).
func TestApplyAuth_deadSignInSendsNothingOnAnyPath(t *testing.T) {
	var sent int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { sent++ }))
	t.Cleanup(srv.Close)
	c := New("MAabc", "", time.Second)
	c.Session = &Session{Org: "MAabc", Issuer: Origin(srv.URL), Origins: []string{Origin(srv.URL)}}
	paths := map[string]error{}
	_, paths["Do"] = c.Do("GET", srv.URL, nil, nil, nil)
	_, paths["DoMultipart"] = c.DoMultipart("POST", srv.URL, []byte(`{}`), nil, nil)
	paths["StreamSSE"] = c.StreamSSE(context.Background(), "GET", srv.URL, nil, func(SSEEvent) bool { return true })
	for name, err := range paths {
		if e, ok := err.(*clierr.Error); !ok || e.Code != clierr.CodeAuthExpired || sent != 0 {
			t.Errorf("%s: err %v, requests %d; want AUTH_EXPIRED and no request", name, err, sent)
		}
	}
}

// The gate's two answers are read from the error field and the challenge, never
// from message text, and only for a request that carried the sign-in. The login's
// own request carries nothing yet, and is given the 403 alone.
func TestGateError(t *testing.T) {
	const org403 = `{"errors":{"error":"organization_not_allowed","global_error":"x"}}`
	session, anon, key := Auth{bearer: true, issuer: true}, Auth{anon: true}, Auth{}
	for _, tc := range []struct {
		name      string
		status    int
		challenge string
		body      string
		auth      Auth
		want      clierr.Code // "" = not the gate's answer
		wantDrop  bool
	}{
		{"invalid_token from the issuer", 401, `Bearer realm="plivo", error="invalid_token"`, ``, session, clierr.CodeAuthExpired, true},
		{"invalid_token from another origin", 401, `Bearer error="invalid_token"`, ``, Auth{bearer: true}, clierr.CodeAuthExpired, false},
		{"invalid_token on a Basic request", 401, `Bearer error="invalid_token"`, ``, key, "", false},
		{"invalid_token with no credentials", 401, `Bearer error="invalid_token"`, ``, anon, "", false},
		{"the fixed 403", 403, ``, org403, session, clierr.CodeAuthForbidden, false},
		{"the fixed 403 with no credentials", 403, ``, org403, anon, clierr.CodeAuthForbidden, false},
		{"the fixed 403, flat", 403, ``, `{"error":"organization_not_allowed"}`, session, "", false},
		{"403 with only the message", 403, ``, `{"errors":{"global_error":"This sign-in cannot use that organization"}}`, session, "", false},
		{"the fixed 403 on a Basic request", 403, ``, org403, key, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dropped := false
			c := New("MAabc", "", time.Second)
			c.Session = &Session{Token: "stk_x", OnRejected: func() { dropped = true }}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.challenge != "" {
					w.Header().Set("WWW-Authenticate", tc.challenge)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			resp, err := http.Get(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			e := c.GateError(tc.auth, resp, []byte(tc.body))
			if (e == nil) != (tc.want == "") || (e != nil && e.Code != tc.want) || dropped != tc.wantDrop || (tc.wantDrop && c.Session.Token != "") {
				t.Errorf("error %v, dropped %v; want %q, dropped %v", e, dropped, tc.want, tc.wantDrop)
			}
		})
	}
}
