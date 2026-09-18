package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
)

type sipReq struct {
	method string
	path   string
	body   map[string]any
}

// sipWriteServer records every request with its decoded body, and serves the
// canned GETs the write verbs make (trunk read-back, usage previews).
func sipWriteServer(t *testing.T, trunks string) func() []sipReq {
	t.Helper()
	var mu sync.Mutex
	var reqs []sipReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		reqs = append(reqs, sipReq{r.Method, r.URL.Path, body})
		mu.Unlock()

		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/Zentrunk/Trunk/"):
			_, _ = w.Write([]byte(trunks))
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/Zentrunk/Trunk/"):
			_, _ = w.Write([]byte(`{"api_id":"x","object":{"trunk_id":"T1","name":"n","trunk_direction":"inbound","trunk_status":"enabled","trunk_domain":"T1.zt.plivo.com"}}`))
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/Number/"):
			_, _ = w.Write([]byte(`{"meta":{},"objects":[{"number":"1","application":"/v1/Account/A/Zentrunk/Trunk/T1/"},{"number":"2","application":"/v1/Account/A/Zentrunk/Trunk/T1/"}]}`))
		case r.Method == "POST":
			_, _ = w.Write([]byte(`{"trunk_id":"T1","uri_uuid":"U1","credential_uuid":"C1","ipacl_uuid":"A1","message":"created"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{
		BaseURL: srv.URL, BuddyBaseURL: srv.URL,
		AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{},
	}
	t.Cleanup(func() { clientForTest = nil })
	return func() []sipReq {
		mu.Lock()
		defer mu.Unlock()
		out := make([]sipReq, len(reqs))
		copy(out, reqs)
		return out
	}
}

const trunksUsingU1 = `{"meta":{},"objects":[
 {"trunk_id":"T1","name":"in","trunk_direction":"inbound","primary_uri_uuid":"U1"},
 {"trunk_id":"T2","name":"out","trunk_direction":"outbound","credential_uuid":"C1","ipacl_uuid":"A1"}]}`

func resetWriteFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		trunkCreateName, trunkCreateDirection, trunkCreateURI = "", "", ""
		trunkCreateFallbackURI, trunkCreateCredential, trunkCreateIPACL = "", "", ""
		trunkCreateSecure = false
		trunkUpdateName, trunkUpdateStatus, trunkUpdateURI = "", "", ""
		trunkUpdateFallbackURI, trunkUpdateCredential, trunkUpdateIPACL = "", "", ""
		trunkUpdateSecure = false
		uriCreateName, uriCreateURI, uriCreateUsername, uriCreatePassword = "", "", "", ""
		uriCreateAuthNeeded, uriUpdateAuthNeeded = false, false
		uriUpdateName, uriUpdateURI, uriUpdateUsername = "", "", ""
		credCreateName, credCreateUsername = "", ""
		credUpdateName, credUpdateUsername = "", ""
		credCreatePasswordStdin, credUpdatePasswordStdin = false, false
		aclCreateName, aclUpdateName = "", ""
		aclCreateIPs, aclUpdateIPs = nil, nil
		numberUpdateTrunkID, numberUpdateAppID = "", ""
		readAllStdin = defaultReadAllStdin
	})
}

func post(reqs []sipReq, contains string) *sipReq {
	for i := range reqs {
		if reqs[i].method == "POST" && strings.Contains(reqs[i].path, contains) {
			return &reqs[i]
		}
	}
	return nil
}

// A trunk needs what makes it usable, and the API's own error does not say
// which half is missing. All three refusals must land before any request.
func TestSIPTrunksCreate_refusesAnUnusableTrunkWithoutARequest(t *testing.T) {
	cases := []struct {
		name, want string
		args       []string
	}{
		{"no direction", "inbound or outbound", []string{"sip", "trunks", "create", "--name", "x"}},
		{"bad direction", "inbound or outbound", []string{"sip", "trunks", "create", "--name", "x", "--direction", "sideways"}},
		{"inbound without uri", "--uri", []string{"sip", "trunks", "create", "--name", "x", "--direction", "inbound"}},
		{"outbound without auth", "--credential or --ip-acl", []string{"sip", "trunks", "create", "--name", "x", "--direction", "outbound"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			resetWriteFlags(t)
			reqs := sipWriteServer(t, trunksUsingU1)
			err, _, _ := execCmd(t, tc.args...)
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error should name what is missing (%q), got: %v", tc.want, err)
			}
			if len(reqs()) != 0 {
				t.Errorf("validation must not spend a request, made %d", len(reqs()))
			}
		})
	}
}

// trunk_domain is not on the create response, and it is the value the customer
// pastes into their platform, so create has to read the trunk back.
func TestSIPTrunksCreate_readsBackForTheDomain(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	reqs := sipWriteServer(t, trunksUsingU1)

	err, stdout, _ := execCmd(t, "sip", "trunks", "create", "--name", "x", "--direction", "inbound", "--uri", "U1", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sawGet bool
	for _, r := range reqs() {
		if r.method == "GET" && strings.Contains(r.path, "/Zentrunk/Trunk/T1/") {
			sawGet = true
		}
	}
	if !sawGet {
		t.Error("create did not read the trunk back, so it cannot print trunk_domain")
	}
	if !strings.Contains(stdout, "T1.zt.plivo.com") {
		t.Errorf("trunk_domain missing from output:\n%s", stdout)
	}
}

// An unset boolean must not be sent at all, or every update silently rewrites
// fields the user never mentioned. A passed false must be sent, or nothing can
// ever be turned off.
func TestSIPTrunksUpdate_booleansOnlyTravelWhenPassed(t *testing.T) {
	t.Run("unset secure is absent", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		if err, _, _ := execCmd(t, "sip", "trunks", "update", "T1", "--status", "disabled"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p := post(reqs(), "/Zentrunk/Trunk/T1/")
		if p == nil {
			t.Fatal("no update request")
		}
		if _, ok := p.body["secure"]; ok {
			t.Errorf("secure was sent without being passed: %v", p.body)
		}
		if p.body["trunk_status"] != "disabled" {
			t.Errorf("trunk_status not sent: %v", p.body)
		}
	})

	t.Run("secure=false is sent", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		if err, _, _ := execCmd(t, "sip", "trunks", "update", "T1", "--secure=false"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p := post(reqs(), "/Zentrunk/Trunk/T1/")
		if p == nil {
			t.Fatal("no update request")
		}
		if v, ok := p.body["secure"]; !ok || v != false {
			t.Errorf("--secure=false must reverse the setting, body: %v", p.body)
		}
	})
}

// Deleting a trunk detaches every number routed to it, so say how many first.
func TestSIPTrunksDelete_refusesWithoutYesAndCountsNumbers(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	reqs := sipWriteServer(t, trunksUsingU1)

	err, _, stderr := execCmd(t, "sip", "trunks", "delete", "T1")
	if err == nil {
		t.Fatal("expected a refusal without --yes")
	}
	if !strings.Contains(stderr, "2 number(s)") {
		t.Errorf("should report attached numbers, stderr:\n%s", stderr)
	}
	for _, r := range reqs() {
		if r.method == "DELETE" {
			t.Fatal("nothing may be deleted without --yes")
		}
	}
}

// Deleting a URI, credential or ACL silently breaks whatever trunk points at
// it, so name them before refusing.
func TestSIPDeletes_previewWhichTrunksUseTheObject(t *testing.T) {
	cases := []struct {
		name, uuid, want string
		args             []string
	}{
		{"uri", "U1", "primary URI", []string{"sip", "uris", "delete", "U1"}},
		{"credential", "C1", "credential", []string{"sip", "credentials", "delete", "C1"}},
		{"ip-acl", "A1", "IP ACL", []string{"sip", "ip-acl", "delete", "A1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			resetWriteFlags(t)
			reqs := sipWriteServer(t, trunksUsingU1)
			err, _, stderr := execCmd(t, tc.args...)
			if err == nil {
				t.Fatal("expected a refusal without --yes")
			}
			if !strings.Contains(stderr, tc.want) {
				t.Errorf("preview should name the usage (%q), stderr:\n%s", tc.want, stderr)
			}
			for _, r := range reqs() {
				if r.method == "DELETE" {
					t.Fatal("nothing may be deleted without --yes")
				}
			}
		})
	}
}

// A bare host is a legal origination URI. Requiring a port would reject a
// perfectly good value that the platform resolves itself.
func TestSIPURIsCreate_acceptsEveryDocumentedURIShape(t *testing.T) {
	for _, uri := range []string{
		"sip.example.com",
		"sip.example.com:5060",
		"sip.example.com;transport=tcp",
		"sip:user@sip.example.com",
	} {
		t.Run(uri, func(t *testing.T) {
			setFakeCreds(t)
			resetWriteFlags(t)
			reqs := sipWriteServer(t, trunksUsingU1)
			if err, _, _ := execCmd(t, "sip", "uris", "create", "--name", "n", "--uri", uri); err != nil {
				t.Fatalf("%q was rejected: %v", uri, err)
			}
			p := post(reqs(), "/Zentrunk/URI/")
			if p == nil || p.body["uri"] != uri {
				t.Errorf("uri not sent verbatim: %v", p)
			}
		})
	}
}

// A password in an argument lands in shell history, `ps`, and CI logs. There
// must be no way to pass one except stdin.
func TestSIPCredentials_passwordOnlyEverComesFromStdin(t *testing.T) {
	t.Run("no --password flag exists", func(t *testing.T) {
		if f := sipCredsCreateCmd.Flags().Lookup("password"); f != nil {
			t.Fatal("a --password flag exists; a password must never be an argument")
		}
		if f := sipCredsUpdateCmd.Flags().Lookup("password"); f != nil {
			t.Fatal("update grew a --password flag")
		}
	})

	t.Run("create refuses without --password-stdin", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		// Assert the guard's own wording. The fallback error inside
		// readPasswordStdin also mentions --password-stdin, so a looser match
		// passes even with the guard deleted.
		err, _, _ := execCmd(t, "sip", "credentials", "create", "--name", "c", "--username", "u")
		if err == nil || !strings.Contains(err.Error(), "is required: the password is only ever read from stdin") {
			t.Fatalf("expected the missing-flag refusal, got: %v", err)
		}
		if len(reqs()) != 0 {
			t.Error("must not reach the API without a password")
		}
	})

	t.Run("password is read from stdin and sent", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		readAllStdin = func() ([]byte, error) { return []byte("s3cret"), nil }

		if err, _, _ := execCmd(t, "sip", "credentials", "create", "--name", "c", "--username", "u", "--password-stdin"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p := post(reqs(), "/Zentrunk/Credential/")
		if p == nil || p.body["password"] != "s3cret" {
			t.Fatalf("password not sent: %v", p)
		}
	})

	// `echo` adds a newline and `printf` does not. A password carrying one fails
	// to authenticate later with nothing to explain why.
	t.Run("a trailing newline is stripped", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		readAllStdin = func() ([]byte, error) { return []byte("s3cret\n"), nil }

		if err, _, _ := execCmd(t, "sip", "credentials", "create", "--name", "c", "--username", "u", "--password-stdin"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p := post(reqs(), "/Zentrunk/Credential/")
		if p == nil || p.body["password"] != "s3cret" {
			t.Fatalf("newline survived into the password: %q", p.body["password"])
		}
	})

	t.Run("empty stdin is refused", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		sipWriteServer(t, trunksUsingU1)
		readAllStdin = func() ([]byte, error) { return []byte("\n"), nil }

		err, _, _ := execCmd(t, "sip", "credentials", "create", "--name", "c", "--username", "u", "--password-stdin")
		if err == nil {
			t.Fatal("an empty password must not be sent")
		}
	})

	// The password must not come back out in any rendering.
	t.Run("the password is never echoed", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		sipWriteServer(t, trunksUsingU1)
		readAllStdin = func() ([]byte, error) { return []byte("hunter2"), nil }

		err, stdout, stderr := execCmd(t, "sip", "credentials", "create", "--name", "c", "--username", "u", "--password-stdin", "-o", "table")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(stdout, "hunter2") || strings.Contains(stderr, "hunter2") {
			t.Errorf("the password was printed back:\nstdout: %s\nstderr: %s", stdout, stderr)
		}
	})
}

func TestRiskyCIDR(t *testing.T) {
	for _, tc := range []struct {
		in   string
		warn bool
	}{
		{"0.0.0.0/0", true},
		{"::/0", true},
		{"10.0.0.0/7", true},
		{"10.0.0.0/8", false},
		{"198.51.100.0/24", false},
		{"203.0.113.4", false},
		{"2001:db8::/32", false},
		{"not-an-ip", false},
	} {
		got := riskyCIDR(tc.in) != ""
		if got != tc.warn {
			t.Errorf("riskyCIDR(%q) warned=%v, want %v", tc.in, got, tc.warn)
		}
	}
}

// Warn, never block: an open range is occasionally deliberate, and refusing it
// pushes people to the console, which warns about nothing.
func TestSIPACLCreate_warnsOnAnOpenRangeButStillCreates(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	reqs := sipWriteServer(t, trunksUsingU1)

	err, _, stderr := execCmd(t, "sip", "ip-acl", "create", "--name", "a", "--ip", "0.0.0.0/0")
	if err != nil {
		t.Fatalf("an open range must warn, not block: %v", err)
	}
	if !strings.Contains(stderr, "Warning") {
		t.Errorf("no warning shown, stderr:\n%s", stderr)
	}
	if post(reqs(), "/Zentrunk/IPAccessControlList/") == nil {
		t.Error("the list should still have been created")
	}
}

// The API takes a trunk in app_id; --trunk-id is the readable spelling of that.
func TestNumbersUpdate_trunkID(t *testing.T) {
	t.Run("sends the trunk in app_id", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		if err, _, _ := execCmd(t, "numbers", "update", "15551234567", "--trunk-id", "T1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p := post(reqs(), "/Number/15551234567/")
		if p == nil || p.body["app_id"] != "T1" {
			t.Fatalf("trunk not sent as app_id: %v", p)
		}
	})

	t.Run("rejects both flags at once", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		err, _, _ := execCmd(t, "numbers", "update", "15551234567", "--trunk-id", "T1", "--app-id", "A9")
		if err == nil {
			t.Fatal("expected a refusal: both write the same field")
		}
		for _, r := range reqs() {
			if r.method == "POST" {
				t.Fatal("must not send an ambiguous update")
			}
		}
	})
}

// A number receives calls; an outbound trunk has nowhere to send them. The
// attach would succeed and the number would quietly stop answering.
func TestNumbersUpdate_refusesAnOutboundTrunk(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	var mu sync.Mutex
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			mu.Lock()
			posted = true
			mu.Unlock()
		}
		if strings.Contains(r.URL.Path, "/Zentrunk/Trunk/") {
			_, _ = w.Write([]byte(`{"api_id":"x","object":{"trunk_id":"T2","trunk_direction":"outbound"}}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{BaseURL: srv.URL, BuddyBaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })

	err, _, _ := execCmd(t, "numbers", "update", "15551234567", "--trunk-id", "T2")
	if err == nil || !strings.Contains(err.Error(), "outbound") {
		t.Fatalf("expected an outbound refusal, got: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if posted {
		t.Error("must not attach a number to an outbound trunk")
	}
}

// The API rejects authentication_needed without a username, but only after the
// round trip and naming the field rather than the flag.
func TestSIPURIs_authenticationNeedsAUsername(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"create", []string{"sip", "uris", "create", "--name", "n", "--uri", "example.com", "--authentication-needed=true"}},
		{"update", []string{"sip", "uris", "update", "U1", "--authentication-needed=true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			resetWriteFlags(t)
			reqs := sipWriteServer(t, trunksUsingU1)
			err, _, _ := execCmd(t, tc.args...)
			if err == nil || !strings.Contains(err.Error(), "--username") {
				t.Fatalf("expected a refusal naming --username, got: %v", err)
			}
			if post(reqs(), "/Zentrunk/URI/") != nil {
				t.Error("must not spend a request on a body the API will reject")
			}
		})
	}
}

// Rotating a password is the common case, and the API refuses a password with
// no username — so a password-only update is impossible on the wire. The stored
// username is carried across rather than demanded again.
func TestSIPCredentialsUpdate_passwordOnlyRotationCarriesTheUsername(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	var mu sync.Mutex
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{"credential_uuid":"C1","name":"c","username":"stored-user"}`))
			return
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		body = b
		mu.Unlock()
		_, _ = w.Write([]byte(`{"message":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{BaseURL: srv.URL, BuddyBaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })
	readAllStdin = func() ([]byte, error) { return []byte("newpw"), nil }

	if err, _, _ := execCmd(t, "sip", "credentials", "update", "C1", "--password-stdin"); err != nil {
		t.Fatalf("a password-only rotation must work: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if body["password"] != "newpw" {
		t.Errorf("password not sent: %v", body)
	}
	if body["username"] != "stored-user" {
		t.Errorf("stored username not carried across, so the API would reject this: %v", body)
	}
}
