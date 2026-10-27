package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/plivo/plivo-cli/internal/config"
)

// r3Cases are the findings of the third QA round, on v1.3.0, under the ids of
// QA's checklist: L seen on a live account, O on the local machine, W on the
// wire to a local server, D in --dry-run against the API docs. F and links
// came up while the fixes were checked, F4 when this suite first ran. A
// check with two separate repros has a case for each (O8a, O8b).
func r3Cases() []qaCase {
	const call = "d1e2f3a4-0000-4000-8000-000000000001"
	var outside string         // O4: the file a link in the repository points at
	var elsewhere *qaElsewhere // W4, W5: where the redirect points
	return []qaCase{
		{
			id: "r3/L1", title: "applications delete --cascade=false sent no cascade, so the API's default deleted the app's endpoints",
			args:   qaArgs("account applications delete 31000000000000001 --cascade=false --yes"),
			sent:   []string{"DELETE Application/31000000000000001/?cascade=false"},
			stdout: []string{`"data"`},
		},
		{
			id: "r3/L2", title: "endpoints get '<id>#zz' read endpoint <id>: the # cut the path",
			args:   []string{"voice", "endpoints", "get", "12345678#zz", "-o", "json"},
			sent:   []string{"GET Endpoint/12345678%23zz/"},
			stdout: []string{`"data"`},
		},
		{
			id: "r3/L3", title: "subaccounts list -o json printed every subaccount's live auth_token",
			args:    qaArgs("account subaccounts list -o json"),
			replies: map[string]string{"GET Subaccount/*": `{"api_id": "qa", "meta": {"limit": 20, "offset": 0, "total_count": 1, "next": null, "previous": null}, "objects": [{"auth_id": "SAQASUBACCOUNT1", "auth_token": "qa-leak-auth-token", "new_auth_token": "qa-leak-new-auth-token", "name": "qa"}]}`},
			sent:    []string{"GET Subaccount/?limit=20&offset=0"},
			stdout:  []string{"SAQASUBACCOUNT1"},
			hidden:  []string{"qa-leak-auth-token", "qa-leak-new-auth-token"},
		},
		{
			id: "r3/L4", title: "10DLC brands get showed every field blank: the API nests the brand and names its fields differently",
			args:    qaArgs("messaging sms 10dlc brands get BQAFAKE1 -o table"),
			replies: map[string]string{"GET 10dlc/Brand/BQAFAKE1/": `{"api_id": "qa", "brand": {"brand_id": "BQAFAKE1", "brand_type": "STARTER", "company_name": "QA Widgets", "registration_status": "COMPLETED"}}`},
			sent:    []string{"GET 10dlc/Brand/BQAFAKE1/"},
			stdout:  []string{"QA Widgets", "COMPLETED"},
		},
		{
			id: "r3/L5", title: "endpoints get -o json printed the endpoint's password hash",
			args:    qaArgs("voice endpoints get 12345678 -o json"),
			replies: map[string]string{"GET Endpoint/12345678/": `{"api_id": "qa", "endpoint_id": "12345678", "username": "qauser01", "alias": "qa", "password": "qa-leak-endpoint-password"}`},
			sent:    []string{"GET Endpoint/12345678/"},
			stdout:  []string{"qauser01"},
			hidden:  []string{"qa-leak-endpoint-password"},
		},
		{
			id: "r3/L6", title: "endpoints create took --alias as optional; the API requires it",
			args:   qaArgs("voice endpoints create --username qauser01 --password QaPass1!"),
			exit:   ExitUserError,
			stderr: []string{"alias"},
		},
		{
			id: "r3/L7", title: "numbers search dropped the reason a 200 body gave in error, so it read as no numbers",
			args:    qaArgs("numbers search --country US"),
			replies: map[string]string{"GET PhoneNumber/*": `{"api_id": "qa", "error": "We are currently unable to offer your requested numbers", "meta": {"limit": 20, "offset": 0, "total_count": 0, "next": null, "previous": null}, "objects": []}`},
			sent:    []string{"GET PhoneNumber/?country_iso=US&limit=20&offset=0"},
			stderr:  []string{"We are currently unable to offer your requested numbers"},
		},
		{
			id: "r3/L8", title: "compliance list refused --number-type national, which the API takes",
			args:   qaArgs("numbers compliance list --number-type national"),
			sent:   []string{"GET PhoneNumber/Compliance/?limit=20&number_type=national&offset=0"},
			stdout: []string{`"data"`},
		},
		{
			id: "r3/L9", title: "endpoints list read app_id, which the API never sends (it sends application, a URI)",
			args:    qaArgs("voice endpoints list -o table"),
			replies: map[string]string{"GET Endpoint/*": `{"api_id": "qa", "meta": {"limit": 20, "offset": 0, "total_count": 1, "next": null, "previous": null}, "objects": [{"endpoint_id": "12345678", "username": "qauser01", "alias": "qa", "application": "/v1/Account/CIFAKEPLACEHOLDER001/Application/31000000000000001/"}]}`},
			sent:    []string{"GET Endpoint/?limit=20&offset=0"},
			stdout:  []string{"31000000000000001"},
		},
		{
			id: "r3/O1", title: "auth use --dry-run switched the profile (auth remove --dry-run deleted one)",
			args:   qaArgs("auth use other --dry-run"),
			setup:  qaTwoProfiles,
			stderr: []string{"[dry-run]"},
			check: func(r *qaResult) []string {
				cfg, err := config.Load()
				return qaExpect(err == nil && cfg.Active == "test", "the active profile is %q after --dry-run (err %v)", cfg.Active, err)
			},
		},
		{
			id: "r3/O2", title: "config set and auth use run at once lost one of the two changes",
			args:  qaArgs("config set telemetry false"),
			setup: qaTwoProfiles,
			check: func(r *qaResult) []string { return qaRaceConfig(8) },
		},
		{
			id: "r3/O3", title: "docs --refresh with the network down showed the old cache as if fresh",
			args:    qaArgs("docs list --refresh"),
			offline: true,
			setup: func(t *testing.T) {
				home, _ := os.UserHomeDir()
				qaWriteFile(t, filepath.Join(home, ".plivo", "cache", "llms.txt"), "- [Make a call](https://docs.plivo.com/docs/voice/api/call/make-a-call)\n")
			},
			stdout: []string{"Make a call"},
			stderr: []string{"refresh failed"},
		},
		{
			id: "r3/O4", title: "skill install --project followed a SKILL.md link out of the repository and created that file",
			args: qaArgs("skill install cli --project"),
			setup: func(t *testing.T) {
				repo := t.TempDir()
				outside = filepath.Join(t.TempDir(), "elsewhere.md")
				dir := filepath.Join(repo, ".claude", "skills", "plivo-cli")
				if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(dir, "SKILL.md")); err != nil {
					t.Fatal(err)
				}
				t.Chdir(repo)
			},
			exit:   ExitUserError,
			stderr: []string{"BAD_INPUT", "link"},
			check: func(r *qaResult) []string {
				_, err := os.Lstat(outside)
				return qaExpect(os.IsNotExist(err), "created %s, outside the repository", outside)
			},
		},
		{
			id: "r3/O5", title: "a --query that repeats its input ([@,@] | [@,@] | ...) grew without limit",
			args:   []string{"--map", "--query", strings.Repeat("[@,@] | ", 6) + "[@,@]"},
			exit:   ExitUserError,
			stderr: []string{"BAD_FLAG", "query"},
		},
		{
			id: "r3/O6", title: "lookup is billed per request (USD 0.004) but said nothing about it",
			args:   qaArgs("lookup +14155550100"),
			sent:   []string{"GET /v1/Lookup/Number/+14155550100?type=carrier"},
			stderr: []string{"USD 0.004"},
		},
		{
			id: "r3/O7", title: "PLIVO_BUDDY_URL took any plain-http host, so ask sent the account's credentials there",
			args:   qaArgs("ask hello"),
			setup:  func(t *testing.T) { t.Setenv("PLIVO_BUDDY_URL", "http://example.com") },
			exit:   ExitUserError,
			stderr: []string{"BAD_INPUT", "PLIVO_BUDDY_URL"},
		},
		{
			id: "r3/O8a", title: "required flags took blank values: sms send --src ' ' --dst '' --text '' built a request",
			args:   []string{"messaging", "sms", "send", "--src", " ", "--dst", "", "--text", "", "--dry-run"},
			exit:   ExitUserError,
			stderr: []string{"BAD_FLAG"},
			hidden: []string{"[dry-run]"},
		},
		{
			id: "r3/O8b", title: "calls hangup '' built DELETE /Call//",
			args:   []string{"voice", "calls", "hangup", "", "--yes"},
			exit:   ExitUserError,
			stderr: []string{"BAD_INPUT"},
		},
		{
			id: "r3/O9", title: "a newline in an ID added a forged line to the --dry-run preview",
			args: []string{"voice", "calls", "hangup", "abc\n[dry-run] DELETE https://forged.example/", "--dry-run"},
			check: func(r *qaResult) []string {
				for _, line := range strings.Split(r.stderr, "\n") {
					if strings.HasPrefix(line, "[dry-run] DELETE https://forged.example/") {
						return []string{"the preview shows the forged line " + line}
					}
				}
				return qaExpect(strings.Contains(r.stderr, "Call/abc%0A"), "the preview lacks the escaped ID:\n%s", r.stderr)
			},
		},
		{
			id: "r3/W1", title: "a # in a conference name cut the path: hangup 'team#2' ended conference team",
			args:   []string{"voice", "conferences", "hangup", "team#2", "--yes"},
			sent:   []string{"DELETE Conference/team%232/"},
			stdout: []string{`"data"`},
		},
		{
			id: "r3/W2", title: "conferences stop-record took no --yes; stop-record '#' sent DELETE /Conference/, ending every conference",
			args:   []string{"voice", "conferences", "stop-record", "#"},
			exit:   ExitRefused,
			stderr: []string{"DESTRUCTIVE_REFUSED"},
		},
		{
			id: "r3/W3", title: "subaccounts update '#' --name fz posted to /Subaccount/, creating a subaccount",
			args:   []string{"account", "subaccounts", "update", "#", "--name", "fz"},
			sent:   []string{"POST Subaccount/%23/"},
			stdout: []string{`"data"`},
		},
		{
			id: "r3/W4", title: "a 307 to another port of the same host was followed, and that port got the credentials",
			args:  qaArgs("account get"),
			setup: func(t *testing.T) { elsewhere = qaListenElsewhere(t) },
			handler: func(w http.ResponseWriter, r *http.Request) bool {
				http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect)
				return true
			},
			sent:   []string{"GET "},
			exit:   ExitAPIError,
			stderr: []string{"UPSTREAM_ERROR", "307"},
			check: func(*qaResult) []string {
				n := elsewhere.hits.Load()
				return qaExpect(n == 0, "the other port got %d request(s), with the credentials", n)
			},
		},
		{
			id: "r3/W5", title: "a 307 to another host was followed, and that host got the new SIP password",
			args:  qaArgs("sip credentials create --name qa --username qauser01 --password-stdin"),
			stdin: "QaPass1!\n",
			setup: func(t *testing.T) { elsewhere = qaListenElsewhere(t) },
			handler: func(w http.ResponseWriter, r *http.Request) bool {
				to := strings.Replace(elsewhere.URL, "127.0.0.1", "localhost", 1) + r.URL.Path
				http.Redirect(w, r, to, http.StatusTemporaryRedirect)
				return true
			},
			sent:   []string{"POST Zentrunk/Credential/"},
			exit:   ExitAPIError,
			stderr: []string{"UPSTREAM_ERROR", "307"},
			check: func(*qaResult) []string {
				n := elsewhere.hits.Load()
				return qaExpect(n == 0, "the other host got %d request(s), with the password", n)
			},
		},
		{
			id: "r3/D1", title: "10DLC brands create never sent the profile_uuid the API requires",
			args: qaArgs("messaging sms 10dlc brands create --profile-uuid " + call + " --alias qa --yes"),
			sent: []string{"POST 10dlc/Brand/"},
			check: func(r *qaResult) []string {
				return qaBodyHas(r, "profile_uuid", call)
			},
		},
		{
			id: "r3/D2", title: "10DLC campaigns create sent sample_message_1 and opt_in_* keys and no vertical; the API takes sample1, optin_*",
			args: qaArgs("messaging sms 10dlc campaigns create --alias qa --brand-id BQAFAKE1 --vertical TECHNOLOGY --usecase MARKETING " +
				"--description qa --message-flow qa --sample-message-1 one --sample-message-2 two --help-message help " +
				"--opt-out-message stop --subscriber-optin --subscriber-optout --subscriber-help --yes"),
			sent: []string{"POST 10dlc/Campaign/"},
			check: func(r *qaResult) []string {
				p := qaBodyHas(r, "sample1", "one")
				p = append(p, qaBodyHas(r, "vertical", "TECHNOLOGY")...)
				return append(p, qaBodyLacks(r, "sample_message_1", "opt_out_message")...)
			},
		},
		{
			id: "r3/D3", title: "tollfree submit sent business_name, use_case and numbers_list; the API takes profile_uuid, number, usecase and volume",
			args: qaArgs("messaging sms tollfree submit --profile-uuid " + call + " --number +18005550100 --use-case MARKETING " +
				"--use-case-summary qa --production-message-content qa --opt-in-type VERBAL --opt-in-image-url https://example.com/optin.png " +
				"--message-volume 1000 --terms-and-conditions-link https://example.com/terms --privacy-policy-link https://example.com/privacy -o table"),
			replies: map[string]string{"POST TollfreeVerification/": `{"api_id": "qa", "message": "created", "uuid": "d1e2f3a4-0000-4000-8000-0000000000f1"}`},
			sent:    []string{"POST TollfreeVerification/"},
			stdout:  []string{"d1e2f3a4-0000-4000-8000-0000000000f1"},
			check: func(r *qaResult) []string {
				p := qaBodyHas(r, "profile_uuid", call)
				p = append(p, qaBodyHas(r, "usecase", "MARKETING")...)
				return append(p, qaBodyLacks(r, "business_name", "use_case", "numbers_list", "message_volume")...)
			},
		},
		{
			id: "r3/D4", title: "multiparty mute posted to /Participant/{id}/Mute/; the API sets mute on the participant",
			args:   qaArgs("voice multiparty participant mute " + call + " 11"),
			sent:   []string{"POST MultiPartyCall/uuid_" + call + "/Participant/11/"},
			stdout: []string{`"data"`},
			check:  func(r *qaResult) []string { return qaBodyHas(r, "mute", true) },
		},
		{
			id: "r3/D5", title: "conference member kick hung the member up (DELETE /Member/{id}/) instead of kicking",
			args:   qaArgs("voice conferences member kick room 11 --yes"),
			sent:   []string{"POST Conference/room/Member/11/Kick/"},
			stdout: []string{`"data"`},
		},
		{
			id: "r3/D6", title: "conference member play sent urls, loop and mix; the API takes one url",
			args:   qaArgs("voice conferences member play room 11 --url https://example.com/a.mp3"),
			sent:   []string{"POST Conference/room/Member/11/Play/"},
			stdout: []string{`"data"`},
			check: func(r *qaResult) []string {
				return append(qaBodyHas(r, "url", "https://example.com/a.mp3"), qaBodyLacks(r, "urls", "loop", "mix")...)
			},
		},
		{
			id: "r3/D7a", title: "streams start sent stream_status_callback_url; the API reads status_callback_url",
			args: qaArgs("voice calls streams start " + call + " --url wss://example.com/s --stream-status-callback https://example.com/cb"),
			sent: []string{"POST Call/" + call + "/Stream/"},
			check: func(r *qaResult) []string {
				return append(qaBodyHas(r, "status_callback_url", "https://example.com/cb"), qaBodyLacks(r, "stream_status_callback_url")...)
			},
		},
		{
			id: "r3/D7b", title: "streams start took --bidirectional with --audio-track both; the API allows inbound only",
			args:   qaArgs("voice calls streams start " + call + " --url wss://example.com/s --bidirectional --audio-track both"),
			exit:   ExitUserError,
			stderr: []string{"BAD_FLAG", "audio-track"},
		},
		{
			id: "r3/D8a", title: "calls make sent answer_method GET with any answer URL; the API's default is POST",
			args: qaArgs("voice calls make --from +14155550100 --to +14155550101 --answer-url https://example.com/answer --yes"),
			sent: []string{"POST Call/"},
			check: func(r *qaResult) []string {
				return append(qaBodyHas(r, "answer_url", "https://example.com/answer"), qaBodyLacks(r, "answer_method")...)
			},
		},
		{
			id: "r3/D8b", title: "calls make took --machine-detection none; the API takes only true or hangup",
			args:   qaArgs("voice calls make --from +14155550100 --to +14155550101 --machine-detection none --yes"),
			exit:   ExitUserError,
			stderr: []string{"BAD_FLAG", "machine-detection"},
		},
		{
			id: "r3/F1", title: "sip uris create took a user part (sip:user@host), which the API refuses",
			args:   qaArgs("sip uris create --name qa --uri sip:qauser@sip.example.com"),
			exit:   ExitUserError,
			stderr: []string{"BAD_FLAG"},
		},
		{
			id: "r3/F2", title: "--timeout 0 silently became 30s",
			args:   qaArgs("voice calls get " + call + " --timeout 0"),
			exit:   ExitUserError,
			stderr: []string{"timeout"},
		},
		{
			id: "r3/F4", title: "plivo api reported a 404 as UPSTREAM_ERROR, exit 3; its help promises the exit codes of the rest of the CLI",
			args:    qaArgs("api GET /Subaccount/SAQASUBACCOUNT1/"),
			replies: map[string]string{"GET Subaccount/{}/": `404 {"api_id": "qa", "error": "not found"}`},
			sent:    []string{"GET Subaccount/SAQASUBACCOUNT1/"},
			exit:    ExitUserError,
			stderr:  []string{"RESOURCE_NOT_FOUND"},
		},
		{
			id: "r3/links", title: "10DLC links called /10dlc/NumberLinking/; links live under /10dlc/Campaign/{id}/Number/",
			args:   qaArgs("messaging sms 10dlc links create --campaign-id CQAFAKE1 --number +14155550100"),
			sent:   []string{"POST 10dlc/Campaign/CQAFAKE1/Number/"},
			stdout: []string{`"data"`},
			check: func(r *qaResult) []string {
				return qaBodyHas(r, "numbers", []any{"+14155550100"})
			},
		},
	}
}

// qaElsewhere is a second server, for a redirect to point at. It counts the
// requests that reach it.
type qaElsewhere struct {
	*httptest.Server
	hits atomic.Int32
}

func qaListenElsewhere(t *testing.T) *qaElsewhere {
	e := &qaElsewhere{}
	e.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		e.hits.Add(1)
		_, _ = io.WriteString(w, qaOK)
	}))
	t.Cleanup(e.Close)
	return e
}

// qaTwoProfiles gives the fake home two profiles, test (active) and other.
func qaTwoProfiles(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	writeProfile(t, home, "active = \"test\"\n\n[profiles.test]\nauth_id = \"CIFAKEPLACEHOLDER001\"\n\n[profiles.other]\nauth_id = \"CIFAKEPLACEHOLDER002\"\n")
}

func qaWriteFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// qaBodyHas checks the last request's JSON body holds key = want.
func qaBodyHas(r *qaResult, key string, want any) []string {
	if len(r.reqs) == 0 {
		return []string{"no request to check"}
	}
	got, ok := r.reqs[len(r.reqs)-1].Body[key]
	w, _ := json.Marshal(want)
	g, _ := json.Marshal(got)
	return qaExpect(ok && string(g) == string(w), "body has %s = %s, want %s: %s", key, g, w, r.reqs[len(r.reqs)-1].Raw)
}

// qaBodyLacks checks the last request's JSON body has none of keys.
func qaBodyLacks(r *qaResult, keys ...string) []string {
	if len(r.reqs) == 0 {
		return []string{"no request to check"}
	}
	var p []string
	for _, k := range keys {
		if _, ok := r.reqs[len(r.reqs)-1].Body[k]; ok {
			p = append(p, "body still sends "+k)
		}
	}
	return p
}

// qaRaceConfig runs `config set telemetry false` and `auth use other` as two
// processes at once, rounds times, and reports each round that lost a change.
func qaRaceConfig(rounds int) []string {
	home, _ := os.UserHomeDir()
	var p []string
	for i := 0; i < rounds; i++ {
		profiles := "active = \"test\"\n\n[profiles.test]\nauth_id = \"CIFAKEPLACEHOLDER001\"\n\n[profiles.other]\nauth_id = \"CIFAKEPLACEHOLDER002\"\n"
		if err := os.WriteFile(filepath.Join(home, ".plivo", "config.toml"), []byte(profiles), 0o600); err != nil {
			return []string{err.Error()}
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, argv := range [][]string{{"config", "set", "telemetry", "false"}, {"auth", "use", "other"}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if out, err := qaCLIProcess(argv...).CombinedOutput(); err != nil {
					mu.Lock()
					p = append(p, fmt.Sprintf("plivo %s: %v: %s", strings.Join(argv, " "), err, out))
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		cfg, err := config.Load()
		if err != nil {
			return append(p, err.Error())
		}
		if off := cfg.Telemetry.Enabled != nil && !*cfg.Telemetry.Enabled; cfg.Active != "other" || !off {
			p = append(p, fmt.Sprintf("round %d lost a change: active %q, telemetry off %v", i+1, cfg.Active, off))
		}
	}
	return p
}

// qaCLIProcess runs the CLI in a child process: this test binary, re-run as
// TestQA_cliProcess with the arguments in QA_CLI_ARGS.
func qaCLIProcess(argv ...string) *exec.Cmd {
	b, _ := json.Marshal(argv)
	cmd := exec.Command(os.Args[0], "-test.run=^TestQA_cliProcess$")
	cmd.Env = append(os.Environ(), "QA_CLI_ARGS="+string(b))
	return cmd
}

// TestQA_cliProcess is the CLI itself when qaCLIProcess starts it, and a no-op
// otherwise.
func TestQA_cliProcess(t *testing.T) {
	raw := os.Getenv("QA_CLI_ARGS")
	if raw == "" {
		return
	}
	var argv []string
	if err := json.Unmarshal([]byte(raw), &argv); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs(argv)
	ran, err := execute(argv)
	if err != nil {
		os.Exit(reportError(ran, err))
	}
	os.Exit(0)
}
