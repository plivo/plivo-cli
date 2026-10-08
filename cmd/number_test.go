package cmd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
)

const zeroUUID = "00000000-0000-0000-0000-000000000000"

// Resellers hold one compliance application per end customer, so renting an
// Indian number for one of them means naming that customer's application.
func TestNumbersBuy_complianceApplicationID(t *testing.T) {
	t.Run("sends the id, trimmed", func(t *testing.T) {
		setFakeCreds(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		err, _, _ := execCmd(t, "numbers", "buy", "+14155551234", "--compliance-application-id", " "+zeroUUID+" ", "--yes")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p := post(reqs(), "/PhoneNumber/")
		if p == nil || p.body["compliance_application_id"] != zeroUUID {
			t.Fatalf("compliance_application_id not sent: %v", p)
		}
	})

	t.Run("omits it when unset", func(t *testing.T) {
		setFakeCreds(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		if err, _, _ := execCmd(t, "numbers", "buy", "+14155551234", "--yes"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p := post(reqs(), "/PhoneNumber/")
		if p == nil {
			t.Fatal("no rent request sent")
		}
		if _, ok := p.body["compliance_application_id"]; ok {
			t.Errorf("sent compliance_application_id nobody asked for: %v", p.body)
		}
	})

	t.Run("dry-run previews it", func(t *testing.T) {
		setFakeCreds(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		err, _, stderr := execCmd(t, "numbers", "buy", "+14155551234", "--compliance-application-id", zeroUUID, "--dry-run")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stderr, `"compliance_application_id": "`+zeroUUID+`"`) {
			t.Errorf("preview does not show the field, stderr:\n%s", stderr)
		}
		if len(reqs()) != 0 {
			t.Errorf("dry-run sent %d requests", len(reqs()))
		}
	})
}

const (
	indiaNumber   = "910000000000"
	numberWithApp = `{"number":"910000000000","compliance_application_id":"` + zeroUUID + `"}`
	numberNoApp   = `{"number":"910000000000","compliance_application_id":null}`
	appRejected   = `{"api_id":"x","compliance":{"compliance_id":"` + zeroUUID + `","status":"rejected"}}`
)

// complianceFixture is what the server holds for one India number: the number
// record and the compliance application it points at. A zero status is 200.
type complianceFixture struct {
	numberStatus int
	number       string
	appStatus    int
	app          string
}

// complianceServer serves fx and records each request as "METHOD path", so a
// test can tell a refused update (no POST) from a sent one.
func complianceServer(t *testing.T, fx complianceFixture) func() []string {
	t.Helper()
	var mu sync.Mutex
	var reqs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqs = append(reqs, r.Method+" "+r.URL.Path)
		mu.Unlock()
		status, body := 0, `{"message":"changed"}`
		switch {
		case r.Method != "GET":
		case strings.Contains(r.URL.Path, "/PhoneNumber/Compliance/"):
			status, body = fx.appStatus, fx.app
		case strings.Contains(r.URL.Path, "/Zentrunk/Trunk/"):
			body = `{"api_id":"x","object":{"trunk_id":"T1","trunk_direction":"inbound"}}`
		case strings.Contains(r.URL.Path, "/Number/"):
			status, body = fx.numberStatus, fx.number
		}
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), reqs...)
	}
}

func hit(reqs []string, method, pathPart string) bool {
	for _, r := range reqs {
		if strings.HasPrefix(r, method+" ") && strings.Contains(r, pathPart) {
			return true
		}
	}
	return false
}

// An India number is routed only when its attached compliance application is
// accepted, and any read the check needs refuses when it fails. A number with
// no application attached gets a warning, not a refusal: live, routed India
// numbers exist without one.
func TestNumbersUpdate_indiaComplianceRule(t *testing.T) {
	cases := []struct {
		name            string
		number          string
		fx              complianceFixture
		refused, warned bool
		msg             string // the refusal names its reason
	}{
		{"accepted", indiaNumber, complianceFixture{app: `{"api_id":"x","compliance":{"status":"accepted"}}`, number: numberWithApp}, false, false, ""},
		{"accepted, flat body", indiaNumber, complianceFixture{app: `{"status":"accepted"}`, number: numberWithApp}, false, false, ""},
		{"rejected", "+" + indiaNumber, complianceFixture{app: appRejected, number: numberWithApp}, true, false, `"rejected"`},
		{"submitted", indiaNumber, complianceFixture{app: `{"compliance":{"status":"submitted"}}`, number: numberWithApp}, true, false, `"submitted"`},
		{"no application attached", indiaNumber, complianceFixture{number: numberNoApp}, false, true, ""},
		{"application unreadable", indiaNumber, complianceFixture{appStatus: 404, app: `{"error":"application gone"}`, number: numberWithApp}, true, false, "application gone"},
		{"application without a status", indiaNumber, complianceFixture{app: `{"api_id":"x"}`, number: numberWithApp}, true, false, "status"},
		{"number unreadable", indiaNumber, complianceFixture{numberStatus: 500, number: `{"error":"number lookup broke"}`}, true, false, "number lookup broke"},
		{"number record without the field", indiaNumber, complianceFixture{number: `{"number":"910000000000"}`}, true, false, "compliance_application_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			reqs := complianceServer(t, tc.fx)
			err, _, stderr := execCmd(t, "numbers", "update", tc.number, "--app-id", "A1")
			if posted := hit(reqs(), "POST", "/Number/"); posted == tc.refused {
				t.Errorf("update sent = %v, want %v (requests: %v)", posted, !tc.refused, reqs())
			}
			if tc.fx.app != "" && !hit(reqs(), "GET", "/PhoneNumber/Compliance/"+zeroUUID+"/") {
				t.Errorf("the attached application was not read: %v", reqs())
			}
			if warned := strings.Contains(stderr, "Warning:"); warned != tc.warned {
				t.Errorf("warned = %v, want %v; stderr:\n%s", warned, tc.warned, stderr)
			}
			if !tc.refused {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var ce *clierr.Error
			if !errors.As(err, &ce) {
				t.Fatalf("want a refusal, got: %v", err)
			}
			if !strings.Contains(ce.Message, tc.msg) {
				t.Errorf("refusal should say why (%q), got: %s", tc.msg, ce.Message)
			}
			for _, want := range []string{"plivo numbers compliance link", "--force"} {
				if !strings.Contains(ce.Hint, want) {
					t.Errorf("hint should carry the KYC steps (%q), got: %s", want, ce.Hint)
				}
			}
		})
	}
}

// The check guards routing changes on India numbers only, and --force skips it.
func TestNumbersUpdate_complianceCheckScope(t *testing.T) {
	cases := []struct {
		name            string
		args            []string
		checked, posted bool
	}{
		{"routing to a trunk", []string{indiaNumber, "--trunk-id", "T1"}, true, false},
		{"--force skips it", []string{indiaNumber, "--app-id", "A1", "--force"}, false, true},
		{"not a routing change", []string{indiaNumber, "--alias", "x"}, false, true},
		{"not an India number", []string{"+14155551234", "--app-id", "A1"}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			reqs := complianceServer(t, complianceFixture{number: numberWithApp, app: appRejected})
			err, _, _ := execCmd(t, append([]string{"numbers", "update"}, tc.args...)...)
			if (err == nil) != tc.posted {
				t.Errorf("err = %v, want refused = %v", err, !tc.posted)
			}
			if got := hit(reqs(), "GET", "/Number/"); got != tc.checked {
				t.Errorf("number read = %v, want %v (requests: %v)", got, tc.checked, reqs())
			}
			if got := hit(reqs(), "POST", "/Number/"); got != tc.posted {
				t.Errorf("update sent = %v, want %v", got, tc.posted)
			}
		})
	}
}

// --dry-run sends no writes but still reads, so the preview refuses and warns
// exactly as the real run would.
func TestNumbersUpdate_dryRunRunsTheComplianceCheck(t *testing.T) {
	t.Run("refuses", func(t *testing.T) {
		setFakeCreds(t)
		reqs := complianceServer(t, complianceFixture{number: numberWithApp, app: appRejected})
		clientForTest.DryRun = true // as getClient sets it under --dry-run
		err, _, _ := execCmd(t, "numbers", "update", indiaNumber, "--app-id", "A1", "--dry-run")
		if err == nil || !strings.Contains(err.Error(), "rejected") {
			t.Fatalf("--dry-run skipped the check: %v", err)
		}
		if hit(reqs(), "POST", "/") {
			t.Error("dry-run sent a write")
		}
	})

	t.Run("warns", func(t *testing.T) {
		setFakeCreds(t)
		reqs := complianceServer(t, complianceFixture{number: numberNoApp})
		clientForTest.DryRun = true
		err, _, stderr := execCmd(t, "numbers", "update", indiaNumber, "--app-id", "A1", "--dry-run")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stderr, "Warning:") || !strings.Contains(stderr, "[dry-run] POST") {
			t.Errorf("want the warning and the previewed update, stderr:\n%s", stderr)
		}
		if hit(reqs(), "POST", "/") {
			t.Error("dry-run sent a write")
		}
	})
}

// A transport failure is a failed read too. The trunk guard lets one through to
// the API; the compliance check must not.
func TestNumbersUpdate_complianceCheckRefusesWhenUnreachable(t *testing.T) {
	setFakeCreds(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })

	err, _, _ := execCmd(t, "numbers", "update", indiaNumber, "--app-id", "A1")
	var ce *clierr.Error
	if !errors.As(err, &ce) || !strings.Contains(ce.Hint, "--force") {
		t.Fatalf("want the compliance refusal, got: %v", err)
	}
}
