package cmd

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
)

// The QA suite runs commands in-process against a local server and checks
// what each sends, its exit code and its output:
//
//   - TestQA_regressions replays one case per QA finding, named round/item as
//     QA filed it (qa_r1_test.go, qa_r2_test.go).
//
// Every run also checks its exit code is the one its error's code maps to.

// pending holds the findings whose fix is not on main yet, each with what is
// still wrong. Their regression cases are skipped, and so is a sweep failure
// one of them owns. The PR that fixes a finding deletes its entry here;
// `go test ./cmd -run TestQA -qa-pending` runs the pending cases and fails
// every one that passes, so an entry cannot outlive its fix.
var pending = map[string]string{
	"r2/5e": "sip test probes the host it was given, never the target of its SRV record (open)",
}

var qaRunPending = flag.Bool("qa-pending", false, "also run the QA cases listed in pending, failing those that pass")

// qaAuthID is the account every request goes to and qaToken its auth token;
// qaProfileToken is the one setFakeCreds saves in the profile. Not
// MA-prefixed, so secret scanners leave them alone.
const (
	qaAuthID       = "CIFAKEPLACEHOLDER001"
	qaToken        = "qa-not-a-real-token"
	qaProfileToken = "ci-only-not-a-real-token"
)

// qaCase replays one finding: the command QA ran, what the API answers and
// what must happen now. Every case states its requests (none when sent is
// empty), its exit code (0 unless exit says otherwise) and something about
// its output.
type qaCase struct {
	id    string // round/item, e.g. "r1/C1"
	title string // what QA saw, in one line
	args  []string
	stdin string
	// replies answers requests by "METHOD path" (see qaReply); anything else
	// gets an empty list or {"message": "ok"}.
	replies map[string]string
	// handler, when set, sees each request first and answers it by returning
	// true: for a stream, a hang or an answer that depends on the request.
	handler func(w http.ResponseWriter, r *http.Request) bool
	// setup prepares anything else, such as a file or a stubbed function.
	setup func(t *testing.T)
	// offline means the case stands for a network that is down: a try to
	// leave the machine fails, as it always does here, and is no problem.
	offline bool

	exit   int
	sent   []string // every request in order, as qaReq.String(); a trailing * matches any rest
	stdout []string // must appear on stdout
	stderr []string // must appear on stderr
	hidden []string // must appear on neither
	// check returns what else is wrong, nil when nothing is.
	check func(r *qaResult) []string
}

// qaResult is one run of a command.
type qaResult struct {
	err            error
	exit           int
	stdout, stderr string
	reqs           []qaReq
	blocked        []string // addresses outside the machine the command tried
}

// qaReq is one request the server saw.
type qaReq struct {
	Method string
	// Path is the escaped path as sent, below /v1/Account/<auth_id>/ for an
	// account API ("Call/abc/"), whole otherwise ("/v1/Lookup/Number/x").
	Path  string
	Query string // raw query, as sent
	Body  map[string]any
	Raw   string
}

func (r qaReq) String() string {
	s := r.Method + " " + r.Path
	if r.Query != "" {
		s += "?" + r.Query
	}
	return s
}

// TestQA_regressions replays every finding. A case whose id is in pending is
// skipped (with -qa-pending it runs, and fails if it passes).
func TestQA_regressions(t *testing.T) {
	var cases []qaCase
	for _, round := range [][]qaCase{r1Cases(), r2Cases()} {
		cases = append(cases, round...)
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.id] {
			t.Errorf("two cases are named %s", c.id)
		}
		seen[c.id] = true
		t.Run(c.id, func(t *testing.T) {
			reason, isPending := pending[c.id]
			if isPending && !*qaRunPending {
				t.Skipf("pending: %s", reason)
			}
			problems := c.run(t)
			switch {
			case isPending && len(problems) == 0:
				t.Errorf("%s passes now: delete it from pending", c.id)
			case isPending:
				t.Skipf("pending: %s\n%s", reason, strings.Join(problems, "\n"))
			case len(problems) > 0:
				t.Errorf("%s (%s):\n%s", c.id, c.title, strings.Join(problems, "\n"))
			}
		})
	}
	for id := range pending {
		if !seen[id] {
			t.Errorf("pending names %s, which has no case", id)
		}
	}
}

// run runs the case and returns what is wrong.
func (c qaCase) run(t *testing.T) []string {
	t.Helper()
	if c.title == "" || len(c.args) == 0 {
		t.Fatalf("%s needs a title and args", c.id)
	}
	if len(c.stdout)+len(c.stderr)+len(c.hidden) == 0 && c.check == nil {
		t.Fatalf("%s asserts nothing about the output", c.id)
	}
	setFakeCreds(t)
	srv := newQAServer(t, c.replies)
	srv.handler = c.handler
	if c.setup != nil {
		c.setup(t)
	}
	r := srv.run(t, c.stdin, c.args...)

	var p []string
	if len(r.blocked) > 0 && !c.offline {
		p = append(p, fmt.Sprintf("tried to reach %v", r.blocked))
	}
	if r.exit != c.exit {
		p = append(p, fmt.Sprintf("exit %d, want %d (%v)", r.exit, c.exit, r.err))
	}
	p = append(p, sentProblems(r.reqs, c.sent)...)
	for _, s := range c.stdout {
		if !strings.Contains(r.stdout, s) {
			p = append(p, fmt.Sprintf("stdout lacks %q:\n%s", s, clip(r.stdout)))
		}
	}
	for _, s := range c.stderr {
		if !strings.Contains(r.stderr, s) {
			p = append(p, fmt.Sprintf("stderr lacks %q:\n%s", s, clip(r.stderr)))
		}
	}
	for _, s := range c.hidden {
		if strings.Contains(r.stdout+r.stderr, s) {
			p = append(p, fmt.Sprintf("output shows %q", s))
		}
	}
	if c.check != nil {
		p = append(p, c.check(&r)...)
	}
	return p
}

// sentProblems compares the requests with want, in order.
func sentProblems(got []qaReq, want []string) []string {
	match := func(r qaReq, w string) bool {
		if pre, ok := strings.CutSuffix(w, "*"); ok {
			return strings.HasPrefix(r.String(), pre)
		}
		return r.String() == w
	}
	ok := len(got) == len(want)
	for i := 0; ok && i < len(got); i++ {
		ok = match(got[i], want[i])
	}
	if ok {
		return nil
	}
	lines := make([]string, len(got))
	for i, r := range got {
		lines[i] = "  " + r.String()
	}
	return []string{fmt.Sprintf("sent %d request(s), want %q:\n%s", len(got), want, strings.Join(lines, "\n"))}
}

// expect returns the problem when cond is false, for check funcs.
func expect(cond bool, format string, args ...any) []string {
	if cond {
		return nil
	}
	return []string{fmt.Sprintf(format, args...)}
}

func clip(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "…"
	}
	return s
}

// args splits a command line on spaces; write a []string literal for an
// argument that holds a space or is empty.
func args(line string) []string { return strings.Fields(line) }

// qaServer stands in for every Plivo API: it records each request and
// answers from its replies.
type qaServer struct {
	*httptest.Server
	replies []qaReply
	handler func(w http.ResponseWriter, r *http.Request) bool
	rewrite func(req qaReq, body string) string // edits each reply body, for the sweeps
	// realClient leaves the commands to build their client from the profile,
	// aimed at the real API, which the network guard keeps them from reaching.
	realClient bool

	mu      sync.Mutex
	reqs    []qaReq
	blocked []string // connections the network guard refused
}

// qaReply answers the requests whose "METHOD path" matches pattern. In a
// pattern {} stands for one path segment and a trailing * for any rest. A
// body may start with a status, "404 {...}"; the default is 200.
type qaReply struct {
	pattern *regexp.Regexp
	key     string
	status  int
	body    string
}

const (
	qaEmptyList = `{"api_id": "qa", "meta": {"limit": 20, "offset": 0, "total_count": 0, "next": null, "previous": null}, "objects": []}`
	qaOK        = `{"api_id": "qa", "message": "ok"}`
)

// newQAServer starts a server that answers with replies, points every
// command at it, and refuses any connection that is not to the loopback
// address, so nothing in the suite can reach a real API.
func newQAServer(t *testing.T, replies map[string]string) *qaServer {
	t.Helper()
	s := &qaServer{}
	keys := make([]string, 0, len(replies))
	for k := range replies {
		keys = append(keys, k)
	}
	// The most specific pattern wins: fewer wildcards, then longer.
	sort.Slice(keys, func(i, j int) bool {
		wi, wj := strings.Count(keys[i], "{}")+strings.Count(keys[i], "*"), strings.Count(keys[j], "{}")+strings.Count(keys[j], "*")
		if wi != wj {
			return wi < wj
		}
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		re := regexp.QuoteMeta(k)
		re = strings.ReplaceAll(re, `\{\}`, `[^/?]+`)
		if strings.HasSuffix(re, `\*`) {
			re = strings.TrimSuffix(re, `\*`) + ".*"
		}
		reply := qaReply{pattern: regexp.MustCompile("^" + re + "$"), key: k, status: http.StatusOK, body: replies[k]}
		if len(reply.body) > 4 && reply.body[3] == ' ' {
			if _, err := fmt.Sscanf(reply.body[:3], "%d", &reply.status); err == nil {
				reply.body = reply.body[4:]
			}
		}
		s.replies = append(s.replies, reply)
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	opener, transport := browserOpener, http.DefaultTransport
	t.Cleanup(func() { browserOpener, http.DefaultTransport = opener, transport })
	browserOpener = func(string) error { return nil }
	s.guardNetwork(t)
	return s
}

func (s *qaServer) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	path, query, _ := strings.Cut(r.RequestURI, "?")
	path = strings.TrimPrefix(path, "/v1/Account/"+qaAuthID+"/")
	req := qaReq{Method: r.Method, Path: path, Query: query, Raw: string(raw)}
	_ = json.Unmarshal(raw, &req.Body)
	s.mu.Lock()
	s.reqs = append(s.reqs, req)
	s.mu.Unlock()
	if s.handler != nil {
		r.Body = io.NopCloser(strings.NewReader(req.Raw))
		if s.handler(w, r) {
			return
		}
	}

	status, body := http.StatusOK, qaOK
	if r.Method == http.MethodGet {
		body = qaEmptyList
	}
	for _, reply := range s.replies {
		if reply.pattern.MatchString(req.String()) {
			status, body = reply.status, reply.body
			break
		}
	}
	if s.rewrite != nil {
		body = s.rewrite(req, body)
	}
	switch {
	case strings.HasPrefix(body, "event:") || strings.HasPrefix(body, "data:"):
		w.Header().Set("Content-Type", "text/event-stream")
	case strings.HasPrefix(body, "{") || strings.HasPrefix(body, "["):
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// guardNetwork lets the commands reach only loopback addresses (and
// localhost) for the rest of the test, and records any other address they
// try.
func (s *qaServer) guardNetwork(t *testing.T) {
	dialer := &net.Dialer{}
	tr := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(addr)
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			s.mu.Lock()
			s.blocked = append(s.blocked, addr)
			s.mu.Unlock()
			return nil, fmt.Errorf("the QA suite blocks %s", addr)
		}
		return dialer.DialContext(ctx, network, addr)
	}}
	http.DefaultTransport = tr
	t.Cleanup(tr.CloseIdleConnections)
}

// run runs one command line against the server with stdin as its input (not
// a terminal) and returns what happened.
func (s *qaServer) run(t *testing.T, stdin string, argv ...string) qaResult {
	t.Helper()
	s.mu.Lock()
	s.reqs, s.blocked = nil, nil
	s.mu.Unlock()

	// getClient hands out clientForTest as is, so it is built here the way
	// getClient builds one, with --dry-run and --timeout read from argv.
	timeout := 30 * time.Second
	dryRun := false
	for i, a := range argv {
		switch {
		case a == "--dry-run" || a == "--dry-run=true":
			dryRun = true
		case a == "--timeout" && i+1 < len(argv):
			_ = secondsOrDuration{&timeout}.Set(argv[i+1])
		case strings.HasPrefix(a, "--timeout="):
			_ = secondsOrDuration{&timeout}.Set(strings.TrimPrefix(a, "--timeout="))
		}
	}
	if !s.realClient {
		c := api.New(qaAuthID, qaToken, timeout)
		c.BaseURL, c.BuddyBaseURL, c.DryRun = s.URL, s.URL, dryRun
		clientForTest = c
		defer func() { clientForTest = nil }()
	}

	in, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.WriteString(stdin); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = in
	defer func() { os.Stdin = orig; _ = in.Close() }()

	runErr, stdout, stderr := execCmd(t, argv...)
	r := qaResult{err: runErr, stdout: stdout, stderr: stderr}
	if runErr != nil {
		envelope, exit := qaReport(t, runErr)
		r.stderr += envelope
		r.exit = exit
	}
	if p := qaExitProblems(r); len(p) > 0 {
		t.Errorf("plivo %s: %s", strings.Join(argv, " "), strings.Join(p, "; "))
	}
	s.mu.Lock()
	r.reqs = append(r.reqs, s.reqs...)
	r.blocked = append(r.blocked, s.blocked...)
	s.mu.Unlock()
	return r
}

// qaReport hands err to reportError, as Execute does before it exits, and
// returns what it printed on stderr (off a terminal: the JSON envelope) and
// the exit code.
func qaReport(t *testing.T, err error) (string, int) {
	t.Helper()
	f, ferr := os.CreateTemp(t.TempDir(), "stderr")
	if ferr != nil {
		t.Fatal(ferr)
	}
	defer f.Close()
	orig := os.Stderr
	os.Stderr = f
	exit := reportError(nil, err)
	os.Stderr = orig
	b, ferr := os.ReadFile(f.Name())
	if ferr != nil {
		t.Fatal(ferr)
	}
	return string(b), exit
}

// qaEnvelope finds an error envelope, {"error": {"code": ...}}.
var qaEnvelope = regexp.MustCompile(`"error":\s*\{\s*"code":\s*"([^"]*)"`)

// qaExitProblems checks a run's exit code against its error: a failure
// carries one of the codes clierr declares and exits with the code it maps
// to, and a success prints no error.
func qaExitProblems(r qaResult) []string {
	if r.err == nil {
		if m := qaEnvelope.FindStringSubmatch(r.stdout + r.stderr); m != nil {
			return []string{fmt.Sprintf("exit 0, but it prints a %s error", m[1])}
		}
		return nil
	}
	m := qaEnvelope.FindStringSubmatch(r.stderr)
	if m == nil {
		return []string{fmt.Sprintf("exit %d with no error envelope: %v", r.exit, r.err)}
	}
	code := clierr.Code(m[1])
	if !qaDeclaredCodes()[code] {
		return []string{fmt.Sprintf("error code %s is not one clierr declares", code)}
	}
	if want := (&clierr.Error{Code: code}).ExitCode(); r.exit != want {
		return []string{fmt.Sprintf("exit %d, but %s maps to %d", r.exit, code, want)}
	}
	return nil
}

// qaDeclaredCodes are the codes declared in internal/clierr, read from its
// source so a new one needs no list here.
var qaDeclaredCodes = sync.OnceValue(func() map[clierr.Code]bool {
	src, err := os.ReadFile(filepath.Join("..", "internal", "clierr", "clierr.go"))
	if err != nil {
		panic(err)
	}
	codes := map[clierr.Code]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*Code\w*\s+Code\s*=\s*"([A-Z0-9_]+)"`).FindAllStringSubmatch(string(src), -1) {
		codes[clierr.Code(m[1])] = true
	}
	return codes
})
