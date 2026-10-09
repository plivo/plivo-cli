package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
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
		uriCreateName, uriCreateURI, uriCreateUsername = "", "", ""
		uriCreateAuthNeeded, uriUpdateAuthNeeded = false, false
		uriCreatePasswordStdin, uriUpdatePasswordStdin = false, false
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

// Deleting a trunk detaches every number routed to it, so say so first. One
// routed number is enough to refuse, so the check names the first it finds.
func TestSIPTrunksDelete_refusesWithoutYesAndNamesARoutedNumber(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	reqs := sipWriteServer(t, trunksUsingU1)

	err, _, stderr := execCmd(t, "sip", "trunks", "delete", "T1")
	if err == nil {
		t.Fatal("expected a refusal without --yes")
	}
	if !strings.Contains(stderr, "Number 1 is routed to this trunk") {
		t.Errorf("should name a routed number, stderr:\n%s", stderr)
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
		"sip:sip.example.com;transport=tls",
		"sips:sip.example.com",
		"203.0.113.4:5060",
		"[2001:db8::1]:5061;transport=tls",
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

// ── QA blockers and highs ────────────────────────────────────────────────────

// --yes skips the confirmation, never the dependency read. Deleting an in-use
// URI cascade-deletes the trunks pointing at it, so the read is the only thing
// that tells anyone what just happened — and with --yes there is no prompt to
// carry it.
func TestSIPDelete_readsDependentsEvenWithYes(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	yesFlag = true
	t.Cleanup(func() { yesFlag = false })
	reqs := sipWriteServer(t, trunksUsingU1)

	err, _, stderr := execCmd(t, "sip", "uris", "delete", "U1", "--yes", "--force")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr, "primary URI") {
		t.Errorf("dependents were not reported on the --yes path:\n%s", stderr)
	}
	var sawList bool
	for _, r := range reqs() {
		if r.method == "GET" && strings.HasSuffix(r.path, "/Zentrunk/Trunk/") {
			sawList = true
		}
	}
	if !sawList {
		t.Error("no dependency read was performed")
	}
}

// The API rejects any trunk update that omits trunk_direction, so every flag
// except --status and --secure used to 400.
func TestSIPTrunksUpdate_alwaysSendsTrunkDirection(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	reqs := sipWriteServer(t, trunksUsingU1)

	if err, _, _ := execCmd(t, "sip", "trunks", "update", "T1", "--uri", "U9"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := post(reqs(), "/Zentrunk/Trunk/T1/")
	if p == nil {
		t.Fatal("no update request")
	}
	if p.body["trunk_direction"] != "inbound" {
		t.Errorf("trunk_direction missing or wrong, so the API would 400: %v", p.body)
	}
	if p.body["primary_uri_uuid"] != "U9" {
		t.Errorf("the actual change was lost: %v", p.body)
	}
}

// -o json on a write used to print nothing at all and exit 0, which a jq
// pipeline cannot tell apart from an empty result.
func TestSIPWrites_emitJSONOnStdout(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		args       []string
	}{
		{"update", `"updated"`, []string{"sip", "trunks", "update", "T1", "--status", "disabled", "-o", "json"}},
		{"delete", `"deleted"`, []string{"sip", "uris", "delete", "U1", "--yes", "--force", "-o", "json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			resetWriteFlags(t)
			sipWriteServer(t, trunksUsingU1)
			err, stdout, _ := execCmd(t, tc.args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Errorf("stdout carried no JSON envelope:\n%s", stdout)
			}
		})
	}
}

// trunk_domain is the value the customer pastes into their platform. The table
// path read it back; -o json returned only the create response without it.
func TestSIPTrunksCreate_jsonCarriesTrunkDomain(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	sipWriteServer(t, trunksUsingU1)

	err, stdout, _ := execCmd(t, "sip", "trunks", "create", "--name", "x", "--direction", "inbound", "--uri", "U1", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "T1.zt.plivo.com") {
		t.Errorf("trunk_domain absent from -o json:\n%s", stdout)
	}
}

// mustJSON builds the -o json body of the SIP writes, so it must not
// HTML-escape: a URI's & and an alias's <, > come out as the API sent them.
func TestMustJSON_keepsHTMLCharactersLiteral(t *testing.T) {
	got := string(mustJSON(map[string]any{"alias": "<desk>", "uri": "sip:bob@example.com?subject=a&b"}))
	want := `{"alias":"<desk>","uri":"sip:bob@example.com?subject=a&b"}`
	if got != want {
		t.Errorf("mustJSON = %s, want %s", got, want)
	}
}

// Every credential update rewrites the password, so one without a password
// blanks it. The flag is required, not optional.
func TestSIPCredentialsUpdate_requiresThePasswordFlag(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	reqs := sipWriteServer(t, trunksUsingU1)

	err, _, _ := execCmd(t, "sip", "credentials", "update", "C1", "--name", "renamed")
	if err == nil || !strings.Contains(err.Error(), "--password-stdin is required") {
		t.Fatalf("expected the required-flag refusal, got: %v", err)
	}
	if post(reqs(), "/Zentrunk/Credential/") != nil {
		t.Error("must not send an update that would blank the password")
	}
}

// A URI password must never be an argument, exactly as for credentials.
func TestSIPURIs_passwordIsStdinOnly(t *testing.T) {
	if f := sipURIsCreateCmd.Flags().Lookup("password"); f != nil {
		t.Error("uris create has a --password flag; it lands in shell history and ps")
	}
	if f := sipURIsUpdateCmd.Flags().Lookup("password"); f != nil {
		t.Error("uris update has a --password flag")
	}
	if f := sipURIsCreateCmd.Flags().Lookup("password-stdin"); f == nil {
		t.Error("uris create cannot set a password at all")
	}
	if f := sipURIsUpdateCmd.Flags().Lookup("password-stdin"); f == nil {
		t.Error("a URI password cannot be rotated")
	}
}

// --dry-run means "send no writes". A GET is not a write, so the guards built
// on a pre-flight read must still run, or the preview shows a request the real
// run refuses.
func TestNumbersUpdate_dryRunStillRunsTheOutboundGuard(t *testing.T) {
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

	err, _, _ := execCmd(t, "numbers", "update", "15551234567", "--trunk-id", "T2", "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "outbound") {
		t.Fatalf("--dry-run skipped the guard, so the preview lies: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if posted {
		t.Error("dry-run sent a write")
	}
}

// Third instance of one API shape: a write is refused unless it restates a
// field it is not changing. trunk_direction on trunks, username on credentials,
// and both authentication_needed AND username on URIs. Rotating a URI password
// is impossible on the wire without them.
func TestSIPURIsUpdate_passwordRotationRestatesWhatTheAPIDemands(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	var mu sync.Mutex
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{"uri_uuid":"U1","name":"n","uri":"example.com","authentication_needed":true,"username":"stored-user"}`))
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

	if err, _, _ := execCmd(t, "sip", "uris", "update", "U1", "--password-stdin"); err != nil {
		t.Fatalf("a password-only rotation must work: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if body["password"] != "newpw" {
		t.Errorf("password not sent: %v", body)
	}
	if body["authentication_needed"] != true {
		t.Errorf("authentication_needed not restated, so the API would refuse: %v", body)
	}
	if body["username"] != "stored-user" {
		t.Errorf("stored username not carried across: %v", body)
	}
}

// deleteServer serves the reads a SIP delete makes: count numbers generated
// page by page (the one at index routed, when >= 0, routed to trunk T1) and the
// trunks body for every trunk list. fail, when it returns a status, answers a
// request with it instead. Returns every request as "METHOD path?query".
func deleteServer(t *testing.T, count, routed int, trunks string, fail func(r *http.Request) int) func() []string {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		if fail != nil {
			if status := fail(r); status != 0 {
				w.Header().Set("X-Request-ID", "req-placeholder")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"unavailable"}`))
				return
			}
		}
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/Number/"):
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			rows := []string{}
			for i := offset; i < offset+limit && i < count; i++ {
				app := "/v1/Account/A/Application/1/"
				if i == routed {
					app = "/v1/Account/A/Zentrunk/Trunk/T1/"
				}
				rows = append(rows, fmt.Sprintf(`{"number":"1415555%04d","application":%q}`, i, app))
			}
			_, _ = fmt.Fprintf(w, `{"meta":{"total_count":%d},"objects":[%s]}`, count, strings.Join(rows, ","))
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/Zentrunk/Trunk/"):
			_, _ = w.Write([]byte(trunks))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func countRequests(reqs []string, prefix string) int {
	n := 0
	for _, r := range reqs {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

// wantRefused fails unless err is a DESTRUCTIVE_REFUSED (exit 5), and returns
// its envelope for the caller to inspect.
func wantRefused(t *testing.T, err error) clierr.Error {
	t.Helper()
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Code != clierr.CodeDestructiveRefused {
		t.Fatalf("want DESTRUCTIVE_REFUSED (exit 5), got %v", err)
	}
	return *ce
}

// No server filter finds numbers by trunk, so proving none are routed means
// reading every page, with no page cap: an account with more than 2,000
// numbers must still be deletable.
func TestSIPTrunksDelete_readsEveryPageToProveNoneIsRouted(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	reqs := deleteServer(t, 2500, -1, `{}`, nil)

	err, _, stderr := execCmd(t, "sip", "trunks", "delete", "T1", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := countRequests(reqs(), "GET /v1/Account/CIFAKEPLACEHOLDER001/Number/"); n != 125 {
		t.Fatalf("read %d pages of numbers, want all 125", n)
	}
	if countRequests(reqs(), "DELETE ") != 1 {
		t.Fatal("the trunk was not deleted")
	}
	if !strings.Contains(stderr, "None of the 2500 numbers") {
		t.Errorf("no summary of the check on stderr:\n%s", stderr)
	}
}

func TestSIPTrunksDelete_stopsAtTheFirstRoutedNumber(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	reqs := deleteServer(t, 100, 45, `{}`, nil)

	err, _, stderr := execCmd(t, "sip", "trunks", "delete", "T1", "--yes")
	ce := wantRefused(t, err)
	if n := countRequests(reqs(), "GET "); n != 3 {
		t.Fatalf("read %d pages, want 3: the routed number is on the third", n)
	}
	if countRequests(reqs(), "DELETE ") != 0 {
		t.Fatal("deleted a trunk that numbers are routed to")
	}
	if !strings.Contains(stderr, "14155550045") || !strings.Contains(ce.Hint, "--force") {
		t.Errorf("want the number named and --force in the hint; stderr:\n%s\nhint: %s", stderr, ce.Hint)
	}
}

func TestSIPTrunksDelete_forceDeletesARoutedTrunk(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	reqs := deleteServer(t, 30, 0, `{}`, nil)

	if err, _, _ := execCmd(t, "sip", "trunks", "delete", "T1", "--yes", "--force"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if countRequests(reqs(), "DELETE ") != 1 {
		t.Fatal("--yes --force did not delete")
	}
}

// Deleting an in-use URI also deletes its trunks, so --yes alone is not enough.
func TestSIPDeletes_refuseAnInUseObjectWithoutForce(t *testing.T) {
	for _, args := range [][]string{
		{"sip", "uris", "delete", "U1", "--yes"},
		{"sip", "credentials", "delete", "C1", "--yes"},
		{"sip", "ip-acl", "delete", "A1", "--yes"},
	} {
		t.Run(args[1], func(t *testing.T) {
			setFakeCreds(t)
			resetWriteFlags(t)
			reqs := deleteServer(t, 0, -1, trunksUsingU1, nil)

			err, _, _ := execCmd(t, args...)
			ce := wantRefused(t, err)
			if !strings.Contains(ce.Hint, "--force") || ce.Context["dependents"] == nil {
				t.Errorf("want --force in the hint and the dependents in context: %+v", ce)
			}
			if countRequests(reqs(), "DELETE ") != 0 {
				t.Fatal("deleted an object a trunk uses")
			}
		})
	}
}

// Each delete asks the trunk list for exactly the trunks that use the object.
func TestSIPDeletes_askTheTrunkListByFilter(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"sip", "uris", "delete", "U9", "--yes"}, []string{"primary_uri_uuid=U9", "fallback_uri_uuid=U9"}},
		{[]string{"sip", "credentials", "delete", "C9", "--yes"}, []string{"credential_uuid=C9"}},
		{[]string{"sip", "ip-acl", "delete", "A9", "--yes"}, []string{"ipacl_uuid=A9"}},
	}
	for _, tc := range cases {
		t.Run(tc.args[1], func(t *testing.T) {
			setFakeCreds(t)
			resetWriteFlags(t)
			reqs := deleteServer(t, 0, -1, `{"meta":{"total_count":0},"objects":[]}`, nil)

			if err, _, _ := execCmd(t, tc.args...); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var lists []string
			for _, r := range reqs() {
				if strings.HasPrefix(r, "GET ") {
					lists = append(lists, r)
				}
			}
			if len(lists) != len(tc.want) {
				t.Fatalf("trunk lists read = %v, want one per filter %v", lists, tc.want)
			}
			for i, w := range tc.want {
				if !strings.Contains(lists[i], w) {
					t.Errorf("request %s does not filter by %s", lists[i], w)
				}
			}
		})
	}
}

// A filter the server ignored returns trunks that do not use the object; the
// client-side check keeps them out of the dependents.
func TestSIPDeletes_checkEachMatchClientSide(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	reqs := deleteServer(t, 0, -1, trunksUsingU1, nil)

	err, _, stderr := execCmd(t, "sip", "credentials", "delete", "C9", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(stderr, "In use by") || countRequests(reqs(), "DELETE ") != 1 {
		t.Fatalf("an unrelated trunk was counted as a dependent:\n%s", stderr)
	}
}

// A check that cannot finish stops the delete, even with --yes --force:
// deleting blind is how an in-use URI takes its trunks with it. The read's own
// error comes through, so a rejected login still exits 2 and a rate limit 4;
// only the message and hint say that nothing was deleted.
func TestSIPDeletes_stopWhenTheCheckFails(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		code      clierr.Code
		exit      int
		retryable bool
	}{
		{"server error", http.StatusInternalServerError, clierr.CodeUpstreamError, 3, true},
		{"rejected login", http.StatusUnauthorized, clierr.CodeAuthInvalid, 2, false},
		{"rate limited past the retries", http.StatusTooManyRequests, clierr.CodeRateLimited, 4, true},
	}
	for _, tc := range cases {
		for _, args := range [][]string{
			{"sip", "trunks", "delete", "T1", "--yes", "--force"},
			{"sip", "uris", "delete", "U1", "--yes", "--force"},
			{"sip", "credentials", "delete", "C1", "--yes", "--force"},
			{"sip", "ip-acl", "delete", "A1", "--yes", "--force"},
		} {
			t.Run(tc.name+"/"+args[1], func(t *testing.T) {
				setFakeCreds(t)
				resetWriteFlags(t)
				recordWaits(t)
				reqs := deleteServer(t, 10, -1, trunksUsingU1, func(r *http.Request) int {
					if r.Method == "GET" {
						return tc.status
					}
					return 0
				})

				err, _, _ := execCmd(t, args...)
				var ce *clierr.Error
				if !errors.As(err, &ce) || ce.Code != tc.code || ce.ExitCode() != tc.exit || ce.Retryable != tc.retryable {
					t.Fatalf("want %s (exit %d, retryable %v), got %#v", tc.code, tc.exit, tc.retryable, ce)
				}
				if !strings.HasPrefix(ce.Message, "refusing to delete ") ||
					!strings.Contains(ce.Message, ": could not check what depends on it: ") {
					t.Errorf("message does not say what was refused and why: %q", ce.Message)
				}
				if !strings.HasSuffix(ce.Hint, "Nothing was deleted.") || ce.RequestID != "req-placeholder" {
					t.Errorf("hint %q / request id %q not kept from the read", ce.Hint, ce.RequestID)
				}
				if countRequests(reqs(), "DELETE ") != 0 {
					t.Fatal("deleted without a finished check")
				}
			})
		}
	}

	t.Run("no answer at all", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		srv := httptest.NewServer(http.NotFoundHandler())
		srv.Close()
		clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{}}
		t.Cleanup(func() { clientForTest = nil })

		err, _, _ := execCmd(t, "sip", "uris", "delete", "U1", "--yes", "--force")
		var ce *clierr.Error
		if !errors.As(err, &ce) || ce.Code != clierr.CodeNetworkError || ce.ExitCode() != 3 || !ce.Retryable {
			t.Fatalf("want NETWORK_ERROR (exit 3, retryable), got %#v", ce)
		}
		if !strings.HasPrefix(ce.Message, "refusing to delete URI U1: could not check what depends on it: ") {
			t.Errorf("message = %q", ce.Message)
		}
	})
}

// --dry-run reads the dependents for real (a GET is not a write) and shows
// them, so the preview refuses exactly what the real run would.
func TestSIPDeletes_dryRunShowsDependents(t *testing.T) {
	t.Run("in use", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		reqs := deleteServer(t, 0, -1, trunksUsingU1, nil)
		clientForTest.DryRun = true

		err, _, stderr := execCmd(t, "sip", "uris", "delete", "U1", "--yes", "--dry-run")
		wantRefused(t, err)
		if !strings.Contains(stderr, "T1 (in, primary URI)") {
			t.Errorf("dependents not shown under --dry-run:\n%s", stderr)
		}
		if countRequests(reqs(), "GET ") == 0 || countRequests(reqs(), "DELETE ") != 0 {
			t.Fatalf("want real reads and no delete, got %v", reqs())
		}
	})

	t.Run("unused", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		reqs := deleteServer(t, 0, -1, `{"meta":{"total_count":0},"objects":[]}`, nil)
		clientForTest.DryRun = true

		err, _, stderr := execCmd(t, "sip", "uris", "delete", "U9", "--yes", "--dry-run")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stderr, "[dry-run] DELETE") || countRequests(reqs(), "DELETE ") != 0 {
			t.Fatalf("want the delete previewed, not sent; stderr:\n%s", stderr)
		}
	})
}
