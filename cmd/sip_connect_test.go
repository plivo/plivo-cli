package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// Number records shaped like the live API's, with placeholder numbers and ids.
const (
	usNumberRecord = `{"number":"14155551234","country":"United States","voice_enabled":true,` +
		`"application":"/v1/Account/CIFAKEPLACEHOLDER001/Application/00000000000000000000/"}`
	indiaNumberRecord = `{"number":"910000000000","country":"India","voice_enabled":true,` +
		`"application":"","compliance_application_id":null}`
	indiaNumberWithApp = `{"number":"910000000000","country":"India","voice_enabled":true,` +
		`"compliance_application_id":"` + zeroUUID + `"}`
)

func decodeConnectPlan(t *testing.T, stdout string) connectPlan {
	t.Helper()
	var env struct {
		Data connectPlan `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not the JSON plan: %v\n%s", err, stdout)
	}
	return env.Data
}

func checkStatus(p connectPlan, name string) string {
	for _, c := range p.Checks {
		if c.Name == name {
			return c.Status
		}
	}
	return ""
}

// readOnly fails the test when the plan sent anything but a GET.
func readOnly(t *testing.T, reqs []string) {
	t.Helper()
	for _, r := range reqs {
		if !strings.HasPrefix(r, "GET ") {
			t.Errorf("the plan must only read, but sent %s", r)
		}
	}
}

func TestSIPConnectPlan_printsEveryStepInOrder(t *testing.T) {
	setFakeCreds(t)
	reqs := complianceServer(t, complianceFixture{number: usNumberRecord})

	err, stdout, _ := execCmd(t, "sip", "connect", "plan", "--number", "+14155551234", "--platform", "livekit", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	readOnly(t, reqs())
	plan := decodeConnectPlan(t, stdout)
	for name, want := range map[string]string{"number": "pass", "voice": "pass", "country": "info", "routing": "info"} {
		if got := checkStatus(plan, name); got != want {
			t.Errorf("check %s = %q, want %q (%+v)", name, got, want, plan.Checks)
		}
	}
	if checkStatus(plan, "compliance") != "" || checkStatus(plan, "platform") != "" {
		t.Errorf("India checks ran on a US number: %+v", plan.Checks)
	}
	want := []string{
		"plivo sip uris create --name livekit-in --platform livekit --uri <project>.sip.livekit.cloud --dry-run",
		`printf '%s' "$SIP_PASSWORD" | plivo sip credentials create --name livekit-out --username <username> --password-stdin --dry-run`,
		"plivo sip trunks create --name livekit-in --direction inbound --platform livekit --uri <uri_uuid> --dry-run",
		"plivo sip trunks create --name livekit-out --direction outbound --platform livekit --credential <credential_uuid> --dry-run",
		"plivo numbers update 14155551234 --trunk-id <inbound_trunk_id> --dry-run",
	}
	if len(plan.Requests) != len(want) {
		t.Fatalf("got %d steps, want %d: %+v", len(plan.Requests), len(want), plan.Requests)
	}
	for i, r := range plan.Requests {
		if r.Step != i+1 || r.Command != want[i] || r.Method != "POST" {
			t.Errorf("step %d = %+v, want command %q", i+1, r, want[i])
		}
	}
	actions := strings.Join(plan.NextActions, "\n")
	for _, s := range []string{"recommended: add --secure to step 4", "dispatch rule", "integration-guides/livekit", "<project> is the subdomain"} {
		if !strings.Contains(actions, s) {
			t.Errorf("next_actions missing %q:\n%s", s, actions)
		}
	}
}

func TestSIPConnectPlan_vapiAuthenticatesByIPList(t *testing.T) {
	setFakeCreds(t)
	complianceServer(t, complianceFixture{number: usNumberRecord})

	err, stdout, _ := execCmd(t, "sip", "connect", "plan", "--number", "14155551234", "--platform", "vapi", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	plan := decodeConnectPlan(t, stdout)
	if got := plan.Requests[1].Command; got != "plivo sip ip-acl create --name vapi-out --platform vapi --dry-run" {
		t.Errorf("step 2 = %q", got)
	}
	if got := plan.Requests[3].Command; !strings.Contains(got, "--ip-acl <ipacl_uuid>") {
		t.Errorf("step 4 = %q", got)
	}
	if actions := strings.Join(plan.NextActions, "\n"); strings.Contains(actions, "--secure") || strings.Contains(actions, "SIP_PASSWORD") {
		t.Errorf("Vapi's guide uses neither secure trunking nor a credential:\n%s", actions)
	}
}

// An India number gets C2's compliance rule and the platform's India support,
// and the URI step uses the host Indian numbers need.
func TestSIPConnectPlan_indiaNumbers(t *testing.T) {
	for _, tc := range []struct {
		platform, uriStep string
	}{
		{"livekit", "--uri <project>.india.sip.livekit.cloud --dry-run"},
		{"elevenlabs", "--uri sip.rtc.in.residency.elevenlabs.io --dry-run"},
		{"retell", "plivo sip uris create --name retell-in --platform retell --dry-run"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			setFakeCreds(t)
			reqs := complianceServer(t, complianceFixture{number: indiaNumberRecord})
			err, stdout, _ := execCmd(t, "sip", "connect", "plan", "--number", "+"+indiaNumber, "--platform", tc.platform, "-o", "json")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			readOnly(t, reqs())
			plan := decodeConnectPlan(t, stdout)
			if checkStatus(plan, "platform") != "warn" || checkStatus(plan, "compliance") != "warn" {
				t.Errorf("want platform and compliance warnings: %+v", plan.Checks)
			}
			if !strings.HasSuffix(plan.Requests[0].Command, tc.uriStep) {
				t.Errorf("step 1 = %q, want it to end %q", plan.Requests[0].Command, tc.uriStep)
			}
			if !strings.Contains(strings.Join(plan.NextActions, "\n"), "India: Indian numbers on") {
				t.Errorf("next_actions should carry the India requirement: %v", plan.NextActions)
			}
		})
	}
}

// Combinations that cannot work stop the plan, exit non-zero, and print no
// steps. The Vapi refusal needs no compliance read.
func TestSIPConnectPlan_stopsAtTheFirstFailedCheck(t *testing.T) {
	for _, tc := range []struct {
		name, platform, failed string
		fx                     complianceFixture
		code                   clierr.Code
	}{
		{"not on the account", "livekit", "number",
			complianceFixture{numberStatus: http.StatusNotFound, number: `{"error":"not found"}`}, clierr.CodeResourceNotFound},
		{"voice disabled", "livekit", "voice",
			complianceFixture{number: `{"number":"14155551234","voice_enabled":false}`}, clierr.CodeBadInput},
		{"india on vapi", "vapi", "platform",
			complianceFixture{number: indiaNumberRecord}, clierr.CodeBadInput},
		{"india with a rejected application", "livekit", "compliance",
			complianceFixture{number: indiaNumberWithApp, app: appRejected}, clierr.CodeValidation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			reqs := complianceServer(t, tc.fx)
			number := "+14155551234"
			if strings.Contains(tc.fx.number, indiaNumber) {
				number = "+" + indiaNumber
			}
			err, stdout, _ := execCmd(t, "sip", "connect", "plan", "--number", number, "--platform", tc.platform, "-o", "json")
			var ce *clierr.Error
			if !errors.As(err, &ce) || ce.Code != tc.code || ce.ExitCode() == 0 {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
			plan := decodeConnectPlan(t, stdout)
			if last := plan.Checks[len(plan.Checks)-1]; last.Name != tc.failed || last.Status != "fail" {
				t.Errorf("last check = %+v, want %s failed", last, tc.failed)
			}
			if plan.Requests == nil || len(plan.Requests) != 0 || plan.NextActions == nil {
				t.Errorf("a stopped plan has no steps, as [] not null: %+v", plan)
			}
			readOnly(t, reqs())
			if tc.failed == "platform" && hit(reqs(), "GET", "/PhoneNumber/Compliance/") {
				t.Error("the Vapi refusal should not need a compliance read")
			}
		})
	}
}

func TestSIPConnectPlan_badFlagsSpendNoRequest(t *testing.T) {
	for _, tc := range []struct {
		flag string
		args []string
	}{
		{"number", []string{"--platform", "livekit"}},
		{"number", []string{"--number", "+1415555abcd", "--platform", "livekit"}},
		{"number", []string{"--number", "+1234567890123456", "--platform", "livekit"}},
		{"platform", []string{"--number", "+14155551234"}},
		{"platform", []string{"--number", "+14155551234", "--platform", "xai"}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			setFakeCreds(t)
			reqs := complianceServer(t, complianceFixture{number: usNumberRecord})
			err, _, _ := execCmd(t, append([]string{"sip", "connect", "plan"}, tc.args...)...)
			var ce *clierr.Error
			if !errors.As(err, &ce) || ce.Code != clierr.CodeBadFlag || ce.Context["flag"] != tc.flag {
				t.Fatalf("want BAD_FLAG on --%s, got %v", tc.flag, err)
			}
			if len(reqs()) != 0 {
				t.Errorf("validation must not spend a request: %v", reqs())
			}
		})
	}
}

// --dry-run changes nothing a plan does: it only reads, and its reads still
// run. A US number keeps the compliance check, which reads on its own, out of
// the way: the voice check can only pass if the plan read the number itself.
func TestSIPConnectPlan_dryRunStillReads(t *testing.T) {
	setFakeCreds(t)
	reqs := complianceServer(t, complianceFixture{number: usNumberRecord})
	clientForTest.DryRun = true // as getClient sets it under --dry-run

	err, stdout, _ := execCmd(t, "sip", "connect", "plan", "--number", "+14155551234", "--platform", "retell", "--dry-run", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hit(reqs(), "GET", "/Number/14155551234/") {
		t.Errorf("the number was not read under --dry-run: %v", reqs())
	}
	for _, want := range []string{"voice    pass", "United States", "Steps to connect", "5. plivo numbers update 14155551234", "Then:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table missing %q:\n%s", want, stdout)
		}
	}
}
