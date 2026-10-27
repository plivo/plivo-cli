package cmd

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	cliskill "github.com/plivo/plivo-cli/cli-skill"
	"github.com/plivo/plivo-cli/internal/tunnel"
)

// r2Cases are the findings of the second QA round, on v1.3.0.
func r2Cases() []qaCase {
	return []qaCase{
		{
			id: "r2/1", title: `the API refuses sip: and sips: URIs ("URI could not be created"), a preset's sips:host:5061 too; host:5061;transport=tls works`,
			args:    []string{"sip", "uris", "create", "--name", "eleven", "--platform", "elevenlabs", "--uri", "sips:sip.rtc.elevenlabs.io", "-o", "json"},
			handler: r2URIAPI,
			sent:    []string{"POST Zentrunk/URI/"},
			stdout:  []string{r2URI},
			check: func(r *qaResult) []string {
				got := r2Body(r, 0, "uri")
				return qaExpect(got == "sip.rtc.elevenlabs.io:5061;transport=tls", "sent uri %v, want it host first", got)
			},
		},
		{
			id: "r2/2", title: "a force-killed voice streams forward left the app on its dead tunnel URL, and the next run took that URL as the original",
			args:    qaArgs("voice streams forward --number +14155550142 --app " + r2App + " --to ws://localhost:7860/ws --yes -o json"),
			replies: map[string]string{"GET Application/" + r2App + "/": r2AppOnDeadTunnel},
			setup:   r2KilledForward,
			// The app read, pointed at this run's tunnel, then restored.
			sent:   []string{"GET Application/" + r2App + "/", "POST Application/" + r2App + "/", "POST Application/" + r2App + "/"},
			stdout: []string{`"restored": true`},
			stderr: []string{"An earlier forward left this app on its stopped tunnel"},
			check: func(r *qaResult) []string {
				answerURL, method := r2Body(r, 2, "answer_url"), r2Body(r, 2, "answer_method")
				return qaExpect(answerURL == r2OwnAnswerURL && method == "GET", "restored %v %v, want the app's own %s GET", answerURL, method, r2OwnAnswerURL)
			},
		},
		{
			id: "r2/3", title: "deleting the URI a trunk used also deleted the trunk (--yes --force), but the result said only that the URI was deleted",
			args: qaArgs("sip uris delete " + uriU1 + " --yes --force -o json"),
			replies: map[string]string{
				"GET Zentrunk/Trunk/*":    `{"api_id": "qa", "meta": {"next": null}, "objects": [{"trunk_id": "101", "primary_uri_uuid": "` + uriU1 + `"}]}`,
				"GET Zentrunk/Trunk/101/": `404 {"api_id": "qa", "error": "not found"}`,
			},
			sent: []string{
				"GET Zentrunk/Trunk/?limit=20&offset=0&primary_uri_uuid=" + uriU1,
				"GET Zentrunk/Trunk/?fallback_uri_uuid=" + uriU1 + "&limit=20&offset=0",
				"DELETE Zentrunk/URI/" + uriU1 + "/",
				"GET Zentrunk/Trunk/101/",
			},
			stdout: []string{`"trunks_deleted": [`, `"101"`},
		},
		{
			id: "r2/4", title: "Codex's skills went stale: skill update never looked in ~/.agents/skills",
			args:   qaArgs("skill update -o json"),
			setup:  r2StaleCodexSkill,
			stdout: []string{filepath.Join(".agents", "skills", "plivo-cli", skillFileName), `"updated"`},
			check: func(*qaResult) []string {
				home, _ := os.UserHomeDir()
				b, _ := os.ReadFile(filepath.Join(home, ".agents", "skills", "plivo-cli", skillFileName))
				return qaExpect(string(b) == cliskill.SkillMD, "the Codex copy is still the older release")
			},
		},
		{
			id: "r2/5a", title: `diagnose -o json exited 3 on a result block with a comment, trailing commas and confidence "Moderate"`,
			args: []string{"voice", "calls", "diagnose", placeholderUUID, "-o", "json"},
			replies: map[string]string{
				"GET Call/" + placeholderUUID + "/":          voiceCallRecord,
				"POST /v1/cli/cx/v1/aiassist/buddy-ext/chat": resultTurn(r2LooseResultBlock),
			},
			// The call record, the account (its balance goes to the assistant) and the turn.
			sent:   []string{"GET Call/" + placeholderUUID + "/", "GET ", "POST /v1/cli/cx/v1/aiassist/buddy-ext/chat"},
			stdout: []string{`"confidence": "medium"`, `"what_happened": "The callee answered and hung up after 32 seconds."`},
		},
		{
			id: "r2/5b", title: "the feedback scrubber let a Bearer token, a PAN and an Aadhaar number through",
			args:   []string{"feedback", "--rating", "2", "--message", "Bearer not-a-real-token-1 failed; PAN ABCPE1234F, Aadhaar 2345 6789 0123", "--dry-run"},
			hidden: []string{"not-a-real-token-1", "ABCPE1234F", "2345 6789 0123"},
			check:  qaShows(`"comment": "Bearer [REDACTED-TOKEN] failed; PAN [REDACTED-PAN], Aadhaar [REDACTED-AADHAAR]"`),
		},
		{
			id: "r2/5c", title: "--schema described no output for diagnose, though -o json prints a fixed result",
			args:   qaArgs("voice calls diagnose --schema -o json"),
			stdout: []string{`"path": "what_happened"`, `"path": "confidence"`, "and no others"},
		},
		{
			id: "r2/5d", title: "no command listed the calls in progress",
			args:    qaArgs("voice calls list --live -o json"),
			replies: map[string]string{"GET Call/?status=live": `{"api_id": "qa", "calls": ["` + placeholderUUID + `", "` + r2LiveCall + `"]}`},
			sent:    []string{"GET Call/?status=live"},
			stdout:  []string{placeholderUUID, r2LiveCall},
		},
		{
			id: "r2/5e", title: "sip test only warned about the host's SRV record and still reported the host unreachable",
			args:   []string{"sip", "test", "--uri", "sip.example.com;transport=tcp", "-o", "json"},
			setup:  r2HostOnlyInSRV,
			stdout: []string{`"result": "reachable"`},
		},
		{
			id: "r2/6a", title: "the SIP trunking skill said sip: URIs and sip:...;transport=tls work",
			args:   qaArgs("skill install sip-trunking --print"),
			check:  qaLineHas("`sip:`", "`sips:`", "rejects"),
			hidden: []string{"`sip:sip.voice.x.ai;transport=tls` is fine too", "the API also accepts `sip:user@host`", "xAI's `sip:` form is fine"},
		},
		{
			id: "r2/6b", title: "the CLI skill said deleting a URI deletes its trunks, true only where it is the primary URI",
			args:   qaArgs("skill install cli --print"),
			check:  qaLineHas("deleting a uri", "primary uri"),
			hidden: []string{"Deleting a URI deletes the trunks that use it."},
		},
		{
			id: "r2/6c", title: "the audio-streaming skill did not read a live call with voice calls get --live and had no India KYC refusal note",
			args:  qaArgs("skill install audio-streaming --print"),
			check: qaChecks(qaLineHas("voice calls get", "--live"), qaLineHas("kyc", "--force")),
		},
		{
			id: "r2/7a", title: "account usage took --until before --since and sent the reversed window",
			args:   qaArgs("account usage --since 2026-10-17 --until 2026-10-01"),
			exit:   1,
			stderr: []string{`"code": "BAD_INPUT"`, "--until (2026-10-01) must be after --since (2026-10-17)"},
		},
		{
			id: "r2/7b", title: `trunks update --direction alone said "nothing to update, pass at least one flag", though a flag was passed`,
			args:   qaArgs("sip trunks update 101 --direction inbound"),
			exit:   1,
			stderr: []string{`"code": "BAD_INPUT"`, "--direction alone sends no change"},
		},
		{
			id: "r2/7c", title: "the skills offer after login was asked once per agent: a login in Codex asked again after one in Cursor had been asked",
			args:    qaArgs("login"),
			replies: map[string]string{"POST /v1/accounts/cli/token": r2TokenBundle},
			setup:   r2LoginInCodexAfterCursor,
			sent:    []string{"POST /v1/accounts/cli/token"},
			stderr:  []string{"Install the Plivo skills for Codex with: plivo skill install all --agent codex"},
			hidden:  []string{"(Y/n)"},
		},
		{
			id: "r2/7d", title: "calls make sent --to hello to the API: only the spend guard stood in the way",
			args:   qaArgs("voice calls make --from +14155550100 --to hello --yes"),
			exit:   1,
			stderr: []string{`"code": "BAD_FLAG"`, "is not an E.164 number or a sip: URI"},
		},
		{
			id: "r2/7e", title: "--timeout took only plain seconds, so 90s was refused",
			args:   qaArgs("sip test --uri sip.example.com --timeout 90s --dry-run"),
			stderr: []string{"within 1m30s"},
		},
	}
}

// r2Body is a field of the body of the i-th request, nil when there is none.
func r2Body(r *qaResult, i int, key string) any {
	if i >= len(r.reqs) {
		return nil
	}
	return r.reqs[i].Body[key]
}

const (
	r2URI      = "22222222-bbbb-4bbb-8bbb-bbbbbbbbbbbb" // the origination URI the API creates
	r2LiveCall = "11111111-0000-4000-8000-000000000001"
)

// r2URIAPI answers an origination URI create as the API does: a URI with a
// sip: or sips: scheme is refused, one written host first is created.
func r2URIAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/Zentrunk/URI/") {
		return false
	}
	var body struct {
		URI string `json:"uri"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	w.Header().Set("Content-Type", "application/json")
	if u := strings.ToLower(body.URI); strings.HasPrefix(u, "sip:") || strings.HasPrefix(u, "sips:") {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"api_id": "qa", "error": "URI could not be created"}`))
		return true
	}
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"api_id": "qa", "message": "created", "uri_uuid": "` + r2URI + `"}`))
	return true
}

const (
	r2App          = "12345678901234567"
	r2OwnAnswerURL = "https://app.example.com/answer"
	r2DeadTunnel   = "https://abc123.lhr.life/answer"
	// The app as the killed run left it: answered by that run's tunnel, by POST.
	r2AppOnDeadTunnel = `{"app_id": "` + r2App + `", "app_name": "demo", "answer_url": "` + r2DeadTunnel + `", "answer_method": "POST"}`
)

// r2KilledForward leaves what a forward killed with SIGKILL leaves: the app on
// its tunnel (the reply) and the original it saved before repointing the app.
// A tunnel and Ctrl+C cannot run in-process: startTunnel hands over a fake
// tunnel, and the run starts interrupted, so it goes straight to the restore.
func r2KilledForward(t *testing.T) {
	path, err := forwardStatePath(r2App)
	if err != nil {
		t.Fatal(err)
	}
	// A pid no process has: the run that saved it is gone.
	killed := forwardState{AnswerURL: r2OwnAnswerURL, AnswerMethod: "GET", TunnelAnswerURL: r2DeadTunnel, PID: math.MaxInt32}
	if err := writeForwardState(path, killed); err != nil {
		t.Fatal(err)
	}
	startTunnel = func(context.Context, int, string) (*tunnel.Tunnel, error) {
		return &tunnel.Tunnel{PublicURL: "https://def456.lhr.life"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prev := voiceStreamsForwardCmd.Context()
	voiceStreamsForwardCmd.SetContext(ctx)
	t.Cleanup(func() { startTunnel = tunnel.Start; voiceStreamsForwardCmd.SetContext(prev) })
}

// r2StaleCodexSkill puts a released, older CLI skill where Codex reads skills
// and runs outside any git repository, so only the home folders are looked in.
func r2StaleCodexSkill(t *testing.T) {
	home, _ := os.UserHomeDir()
	shipVersion(t, "plivo-cli", olderCLISkill)
	writeSkillIn(t, filepath.Join(home, ".agents", "skills"), "plivo-cli", olderCLISkill)
	wd := t.TempDir()
	if repo, ok := findGitRoot(wd); ok {
		t.Skipf("the temp dir is inside a git repository (%s)", repo)
	}
	t.Chdir(wd)
}

// r2LooseResultBlock is a result block a strict decoder refuses: a comment,
// trailing commas and a confidence that is not high, medium or low.
const r2LooseResultBlock = `{"what_happened": "The callee answered and hung up after 32 seconds.", // from the trace
  "likely_cause": "The callee ended the call.", "timeline": [], "next_steps": ["Nothing to fix.",],
  "confidence": "Moderate",}`

// r2HostOnlyInSRV serves sip.example.com through its SIP SRV record alone: the
// host has no address, and its _sip._tcp record points to a listener here.
// DNS cannot run in-process, so the resolvers are stubbed.
func r2HostOnlyInSRV(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	sipLookup = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host == "pbx.example.net" {
			return []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	sipLookupSRV = func(_ context.Context, service, proto, name string) (string, []*net.SRV, error) {
		if service+"."+proto+"."+name != "sip.tcp.sip.example.com" {
			return "", nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
		}
		return "", []*net.SRV{{Target: "pbx.example.net.", Port: port}}, nil
	}
	t.Cleanup(func() { sipLookup, sipLookupSRV = net.DefaultResolver.LookupIPAddr, net.DefaultResolver.LookupSRV })
}

// r2TokenBundle is the auth server's answer to the login's code.
const r2TokenBundle = `{"api_id": "qa", "data": {"plivo_auth_id": "` + qaAuthID + `", "plivo_auth_token": "qa-not-a-real-token"}}`

// r2LoginInCodexAfterCursor runs `plivo login` in Codex, on a terminal, on a
// machine whose login in Cursor was asked about the skills already. A
// terminal cannot run in-process: askable says there is one, and stdin
// answers n to any question.
func r2LoginInCodexAfterCursor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in browser is a shell script")
	}
	home, _ := os.UserHomeDir()
	writeProfile(t, home, "[skills]\noffered = [\"Cursor\"]\n")
	for _, a := range codingAgents {
		name, _, _ := strings.Cut(a.env, "=")
		t.Setenv(name, "")
	}
	t.Setenv("CODEX_THREAD_ID", "thread-1")
	prevAskable, prevLines := askable, stdinLines
	askable = func() bool { return true }
	stdinLines = func() <-chan string { return r2Answers(t, "n") }
	t.Cleanup(func() { askable, stdinLines = prevAskable, prevLines })
	r2SignInWithBrowser(t)
	r2AuthServer(t)
}

// r2SignInWithBrowser stands in for the browser, which cannot run in-process:
// browserOpener, or a script on PATH, saves the URL it is opened on, then this
// does what the browser does once the user signs in, opening the login's
// loopback URL with that login's state and a code.
func r2SignInWithBrowser(t *testing.T) {
	bin := t.TempDir()
	opened := filepath.Join(bin, "opened")
	script := "#!/bin/sh\nprintf '%s\\n' \"$1\" > '" + opened + "'\n"
	for _, name := range []string{"open", "xdg-open"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	prev := browserOpener
	browserOpener = func(u string) error { return os.WriteFile(opened, []byte(u+"\n"), 0o600) }
	t.Cleanup(func() { browserOpener = prev })
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		browser := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
			b, _ := os.ReadFile(opened)
			authorize, ok := strings.CutSuffix(string(b), "\n")
			if !ok {
				continue
			}
			u, err := url.Parse(authorize)
			if err != nil {
				return
			}
			q := u.Query()
			cb := "http://127.0.0.1:" + q.Get("cb_port") + "/?" + url.Values{"state": {q.Get("state")}, "code": {"qa-code"}}.Encode()
			if resp, err := browser.Get(cb); err == nil {
				_ = resp.Body.Close()
			}
			return
		}
	}()
	t.Cleanup(func() { close(stop); <-done })
}

// r2AuthServer puts the login's auth server at a local port nothing listens
// on, and sends what goes there to the QA server.
func r2AuthServer(t *testing.T) {
	const nowhere = "127.0.0.1:1"
	t.Setenv("PLIVO_BUDDY_URL", "http://"+nowhere)
	next := http.DefaultTransport
	http.DefaultTransport = r2RoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == nowhere {
			qa, err := url.Parse(clientForTest.BuddyBaseURL)
			if err != nil {
				return nil, err
			}
			req = req.Clone(req.Context())
			req.URL.Scheme, req.URL.Host, req.Host = qa.Scheme, qa.Host, ""
		}
		return next.RoundTrip(req)
	})
	t.Cleanup(func() { http.DefaultTransport = next })
}

type r2RoundTrip func(*http.Request) (*http.Response, error)

func (f r2RoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// r2Answers is stdin on a terminal where answer is typed again and again, so
// any question put gets it, while a line typed before the question is dropped.
func r2Answers(t *testing.T, answer string) <-chan string {
	lines, stop := make(chan string, 1), make(chan struct{})
	go func() {
		for {
			select {
			case lines <- answer:
			case <-stop:
				return
			}
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	t.Cleanup(func() { close(stop) })
	return lines
}
