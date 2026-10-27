package cmd

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The sweeps run every command (qaSamples) looking for the kinds of bug QA
// found in a few of them. A failure is filed under the finding that owns it,
// and skipped while that finding is pending.

// qaSweepReplies are the API answers the samples need to run through.
var qaSweepReplies = map[string]string{
	"GET Zentrunk/Trunk/{}/":      `{"api_id": "qa", "trunk_id": "21000000000001", "name": "qa", "trunk_direction": "inbound", "trunk_status": "enabled", "primary_uri_uuid": "d1e2f3a4-0000-4000-8000-000000000003"}`,
	"GET Zentrunk/Credential/{}/": `{"api_id": "qa", "credential_uuid": "d1e2f3a4-0000-4000-8000-000000000001", "name": "qa", "username": "qauser01"}`,
	// The diagnose commands' assistant turn.
	"POST /v1/cli/cx/v1/aiassist/buddy-ext/chat": okDiagnoseTurn,
}

// qaVerdict fails t with the problems, listed by the finding that owns them
// ("" for none). When every owner is pending it skips instead.
func qaVerdict(t *testing.T, problems map[string][]string) {
	t.Helper()
	var failing, skipped []string
	owners := make([]string, 0, len(problems))
	for owner := range problems {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		text := strings.Join(problems[owner], "\n")
		if reason, ok := pending[owner]; ok {
			skipped = append(skipped, fmt.Sprintf("pending %s (%s):\n%s", owner, reason, text))
			continue
		}
		if owner != "" {
			text = owner + ":\n" + text
		}
		failing = append(failing, text)
	}
	switch {
	case len(failing) > 0:
		t.Error(strings.Join(append(failing, skipped...), "\n"))
	case len(skipped) > 0:
		t.Skip(strings.Join(skipped, "\n"))
	}
}

// qaControl stops a subtest whose sample no longer runs: a skip while the
// finding whose fix changes the command is pending, a failure otherwise.
func qaControl(t *testing.T, s qaSample, r qaResult) {
	t.Helper()
	if r.exit == 0 && len(r.blocked) == 0 {
		return
	}
	msg := fmt.Sprintf("the sample does not run: exit %d (%v), tried to reach %v", r.exit, r.err, r.blocked)
	if owner := qaAfterFix[s.path].owner; pending[owner] != "" {
		t.Skipf("pending %s (%s): %s", owner, pending[owner], msg)
	}
	t.Fatal(msg)
}

// ─── Odd IDs ─────────────────────────────────────────────────────────────────

// qaOddIDs are IDs shaped like the ones that sent requests to the wrong
// resource, and QA's other fuzz values, each with the finding that owns it.
// Q7Z marks where an ID lands; a dot or empty ID cannot be escaped into a
// path segment, so where the ID is a whole segment it must be refused.
var qaOddIDs = []struct{ id, owner string }{
	{"Q7Z#1", "r3/L2"}, {"Q7Z?1", "r3/L2"}, {"Q7Z/1", "r3/L2"}, {"Q7Z\n1", "r3/L2"},
	{"Q7Z\u00e9\u202e1", ""}, {"Q7Z" + strings.Repeat("x", 1000), ""}, {"-Q7Z1", ""},
	{"..", "r3/O8b"}, {".", "r3/O8b"}, {"", "r3/O8b"},
}

// qaForgedID tries to add a line of its own to the --dry-run preview.
const qaForgedID = "Q7Z\n[dry-run] DELETE /forged"

// TestQA_sweepOddIDs puts each odd ID in each positional argument of every
// command: it must be refused, or sent escaped as one path segment, and it
// must never forge a line of the --dry-run preview.
func TestQA_sweepOddIDs(t *testing.T) {
	setFakeCreds(t)
	srv := newQAServer(t, qaSweepReplies)
	for _, s := range qaAPISamples() {
		if s.ids == 0 || s.path == "api" { // api's method and path are the caller's own
			continue
		}
		t.Run(s.path, func(t *testing.T) {
			ctl := srv.run(t, qaStdin, append(s.argv, "--yes")...)
			if qaControl(t, s, ctl); len(ctl.reqs) == 0 {
				t.Fatal("the sample sends no request")
			}
			problems := map[string][]string{}
			for i := 0; i < s.ids; i++ {
				whole := qaWholeSegment(ctl.reqs, s.argv[s.words+i])
				for _, odd := range qaOddIDs {
					r := srv.run(t, qaStdin, append(s.withArg(i, odd.id), "--yes")...)
					for _, p := range qaOddIDProblems(r, odd.id, whole) {
						problems[odd.owner] = append(problems[odd.owner], fmt.Sprintf("argument %d = %.40q: %s", i+1, odd.id, p))
					}
				}
				r := srv.run(t, qaStdin, append(s.withArg(i, qaForgedID), "--dry-run")...)
				for _, line := range strings.Split(r.stdout+"\n"+r.stderr, "\n") {
					if strings.HasPrefix(line, "[dry-run] DELETE /forged") {
						problems["r3/O9"] = append(problems["r3/O9"], fmt.Sprintf("argument %d with a newline adds %q to the --dry-run preview", i+1, line))
					}
				}
			}
			qaVerdict(t, problems)
		})
	}
}

// qaWholeSegment reports whether an ID is a whole segment of a request path,
// not part of one (name_<id>) or in the body.
func qaWholeSegment(reqs []qaReq, id string) bool {
	for _, req := range reqs {
		for _, seg := range strings.Split(req.Path, "/") {
			if seg == id || seg == strings.TrimPrefix(id, "+") {
				return true
			}
		}
	}
	return false
}

// qaOddIDProblems checks one run with an odd ID; whole says whether the ID
// it replaced was a whole path segment.
func qaOddIDProblems(r qaResult, odd string, whole bool) []string {
	var p []string
	if len(r.blocked) > 0 {
		p = append(p, fmt.Sprintf("tried to reach %v", r.blocked))
	}
	if !strings.Contains(odd, "Q7Z") {
		if r.exit == 0 && whole {
			p = append(p, "accepted")
		}
		for _, req := range r.reqs {
			if whole && req.Method != http.MethodGet || qaStraySegment(req.Path) {
				p = append(p, "sent "+req.String())
			}
		}
		return p
	}
	escaped := strings.ToLower(url.PathEscape(odd))
	quoted, _ := json.Marshal(odd) // as a JSON body holds it, alone or in a longer string
	inJSON := string(quoted[1 : len(quoted)-1])
	carried := false
	for _, req := range r.reqs {
		uri := strings.ToLower(req.String())
		has := strings.Contains(uri, escaped) || strings.Contains(req.Raw, inJSON)
		switch {
		case strings.Contains(uri, "q7z") && !strings.Contains(uri, escaped):
			p = append(p, "sent it unescaped: "+req.String())
		case req.Method != http.MethodGet && !has:
			p = append(p, "sent a write without it: "+req.String())
		}
		carried = carried || has
	}
	if r.exit == 0 && !carried {
		p = append(p, "succeeded without sending it")
	}
	return p
}

// qaStraySegment reports a path with an empty, "." or ".." segment.
func qaStraySegment(path string) bool {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for _, s := range segs {
		if s == "" || s == "." || s == ".." {
			return true
		}
	}
	return false
}

// ─── --dry-run ───────────────────────────────────────────────────────────────

// qaDryRunOwners are the local commands whose --dry-run a pending finding
// fixes. The sweep skips them, without running them, until it is in.
var qaDryRunOwners = map[string]string{
	"ask": "r3/O1", "auth remove": "r3/O1", "auth use": "r3/O1", "config set": "r3/O1", "config telemetry": "r3/O1",
	"docs list": "r3/O1", "docs search": "r3/O1", "docs show": "r3/O1", "login": "r3/O1", "logout": "r3/O1",
	"voice streams test": "r3/O1",
}

// TestQA_sweepDryRun runs every command with --dry-run. It may honour it
// (exit 0) or refuse it (BAD_FLAG naming --dry-run, exit 1); either way it
// must send no write and change no file, keychain entry or browser. A read
// the preview needs may go out.
func TestQA_sweepDryRun(t *testing.T) {
	setFakeCreds(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	t.Chdir(work)
	srv := newQAServer(t, qaSweepReplies)
	opened := 0
	browserOpener = func(string) error { opened++; return nil }
	for _, s := range append(qaAPISamples(), qaCollect(qaLocalSamples)...) {
		t.Run(s.path, func(t *testing.T) {
			if owner := qaDryRunOwners[s.path]; pending[owner] != "" {
				t.Skipf("pending %s: %s", owner, pending[owner])
			}
			writeProfile(t, home, "active = \"test\"\n\n[profiles.test]\nauth_id = \"CIFAKEPLACEHOLDER001\"\n\n[profiles.other]\nauth_id = \"CIFAKEPLACEHOLDER002\"\n")
			if err := config.SetToken("test", "qa-keychain-value"); err != nil {
				t.Fatal(err)
			}
			before, opens := qaSnapshot(t, home, work), opened
			r := srv.run(t, qaStdin, append(s.argv, "--dry-run")...)
			var p []string
			refused := r.exit == ExitUserError && strings.Contains(r.stderr, `"code": "BAD_FLAG"`) && strings.Contains(r.stderr, "dry-run")
			if r.exit != 0 && !refused {
				p = append(p, fmt.Sprintf("exit %d (%v); want 0, or BAD_FLAG refusing --dry-run", r.exit, r.err))
			}
			for _, req := range r.reqs {
				if req.Method != http.MethodGet {
					p = append(p, "sent "+req.String())
				}
			}
			if qaShowsToken(r.stdout + r.stderr) {
				p = append(p, "prints the account's auth token")
			}
			p = append(p, qaSnapshotDiff(before, qaSnapshot(t, home, work))...)
			if tok, err := config.GetToken("test"); err != nil || tok != "qa-keychain-value" {
				p = append(p, "removed the keychain token")
			}
			if opened != opens {
				p = append(p, "opened a browser")
			}
			if len(r.blocked) > 0 {
				p = append(p, fmt.Sprintf("tried to reach %v", r.blocked))
			}
			if len(p) > 0 {
				t.Error(strings.Join(p, "\n"))
			}
		})
	}
}

// qaSnapshot hashes every file below the roots.
func qaSnapshot(t *testing.T, roots ...string) map[string][32]byte {
	t.Helper()
	files := map[string][32]byte{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = sha256.Sum256(b)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func qaSnapshotDiff(before, after map[string][32]byte) []string {
	var p []string
	for path, sum := range after {
		if old, ok := before[path]; !ok {
			p = append(p, "wrote "+path)
		} else if old != sum {
			p = append(p, "changed "+path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			p = append(p, "removed "+path)
		}
	}
	sort.Strings(p)
	return p
}

// ─── Deletes and spends need --yes ───────────────────────────────────────────

// qaSpends match the requests that cost money. `plivo lookup` costs per
// request too, but stays ungated by decision: it states its price instead.
var qaSpends = regexp.MustCompile(`^(POST Message/|POST Call/|POST PhoneNumber/\+?\d+/|GET CnamLookup/[^/]+/|` +
	`POST Verify/Session/|POST Masking/Session/|POST 10dlc/Brand/|POST 10dlc/Campaign/|` +
	`POST MultiPartyCall/[^/]+/Participant/)(\?.*)?$`)

// qaUndoVerbs send a DELETE that only stops what is in progress; nothing is
// lost, so they take no --yes.
var qaUndoVerbs = map[string]bool{
	"voice calls stop-play": true, "voice calls stop-record": true, "voice calls stop-speak": true,
	"voice conferences member stop-play": true, "voice conferences member stop-speak": true,
	"voice conferences member undeaf": true, "voice conferences member unmute": true,
	"voice multiparty participant unhold": true, "voice multiparty participant unmute": true,
}

// TestQA_sweepUnconfirmed runs every command that deletes or spends without
// --yes, off a terminal: it must be refused (exit 5) before that request.
func TestQA_sweepUnconfirmed(t *testing.T) {
	setFakeCreds(t)
	srv := newQAServer(t, qaSweepReplies)
	risky := func(r qaReq) bool { return r.Method == http.MethodDelete || qaSpends.MatchString(r.String()) }
	checked := 0
	for _, s := range qaAPISamples() {
		if qaUndoVerbs[s.path] {
			continue
		}
		t.Run(s.path, func(t *testing.T) {
			confirmed := srv.run(t, qaStdin, append(s.argv, "--yes")...)
			qaControl(t, s, confirmed)
			var acts []string
			for _, req := range confirmed.reqs {
				if risky(req) {
					acts = append(acts, req.String())
				}
			}
			if len(acts) == 0 {
				return
			}
			checked++
			r := srv.run(t, qaStdin, s.argv...)
			var p []string
			if r.exit != ExitRefused {
				p = append(p, fmt.Sprintf("exit %d, want 5 (it sends %v)", r.exit, acts))
			}
			for _, req := range r.reqs {
				if risky(req) {
					p = append(p, "sent "+req.String()+" without --yes")
				}
			}
			if len(p) > 0 {
				owner := ""
				if s.path == "voice conferences stop-record" {
					owner = "r3/W2"
				}
				qaVerdict(t, map[string][]string{owner: p})
			}
		})
	}
	if checked < 25 {
		t.Errorf("only %d commands delete or spend; the samples have stopped reaching the API", checked)
	}
}

// ─── Secrets ─────────────────────────────────────────────────────────────────

// qaSecrets are the fields that must never print without --show-secrets,
// with the finding that reported each. password is an endpoint's.
var qaSecrets = []struct{ key, value, owner string }{
	{"auth_token", "qa-leak-auth-token", "r3/L3"},
	{"new_auth_token", "qa-leak-new-auth-token", "r3/L3"},
	{"password", "qa-leak-endpoint-password", "r3/L5"},
}

// qaShowsTokenOnce may print a token: a new subaccount's is shown once.
var qaShowsTokenOnce = map[string]bool{"account subaccounts create": true}

// TestQA_sweepSecrets answers every command with the secret fields in each
// object, and checks no output format prints them.
func TestQA_sweepSecrets(t *testing.T) {
	setFakeCreds(t)
	srv := newQAServer(t, qaSweepReplies)
	srv.rewrite = qaWithSecrets
	for _, s := range qaAPISamples() {
		if strings.HasPrefix(s.path, "agents ") {
			continue // flows print as sent, so agents get feeds agents update; the agents API masks their secrets
		}
		t.Run(s.path, func(t *testing.T) {
			problems := map[string][]string{}
			for _, format := range []string{"json", "table", "yaml", "csv"} {
				r := srv.run(t, qaStdin, append(s.argv, "--yes", "-o", format)...)
				if format == "json" {
					qaControl(t, s, r)
				}
				for _, secret := range qaSecrets {
					if secret.owner == "r3/L3" && qaShowsTokenOnce[s.path] {
						continue
					}
					if strings.Contains(r.stdout+r.stderr, secret.value) {
						problems[secret.owner] = append(problems[secret.owner], fmt.Sprintf("-o %s prints %s", format, secret.key))
					}
				}
			}
			qaVerdict(t, problems)
		})
	}
}

// qaWithSecrets adds the secret fields to every object of a JSON reply: the
// top level and each object in a top-level array (a list adds one if empty).
// password goes only into endpoints.
func qaWithSecrets(req qaReq, body string) string {
	var doc map[string]any
	if json.Unmarshal([]byte(body), &doc) != nil {
		return body
	}
	add := func(obj map[string]any) {
		for _, s := range qaSecrets {
			if s.key != "password" || strings.HasPrefix(req.Path, "Endpoint/") {
				obj[s.key] = s.value
			}
		}
	}
	add(doc)
	for key, v := range doc {
		list, ok := v.([]any)
		if !ok {
			continue
		}
		if len(list) == 0 && key == "objects" {
			doc[key] = []any{map[string]any{}}
			list = doc[key].([]any)
		}
		for _, item := range list {
			if obj, ok := item.(map[string]any); ok {
				add(obj)
			}
		}
	}
	b, _ := json.Marshal(doc)
	return string(b)
}

// ─── The account's own token ─────────────────────────────────────────────────

// qaShowsToken reports whether out holds either auth token, as is or in a
// Basic authorization header.
func qaShowsToken(out string) bool {
	for _, tok := range []string{qaToken, qaProfileToken} {
		basic := base64.StdEncoding.EncodeToString([]byte(qaAuthID + ":" + tok))
		if strings.Contains(out, tok) || strings.Contains(out, basic) {
			return true
		}
	}
	return false
}

// TestQA_sweepTokens runs every command with its client built from the
// profile, as the CLI builds it, under --dry-run and under --log-level debug
// (whose request then stops at the network guard): neither prints the
// profile's token. No flag takes a token either, where ps would show it.
func TestQA_sweepTokens(t *testing.T) {
	setFakeCreds(t)
	srv := newQAServer(t, qaSweepReplies)
	srv.realClient = true
	walkOwnCommands(func(c *cobra.Command, path []string) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if strings.Contains(f.Name, "token") && f.Value.Type() != "bool" {
				t.Errorf("plivo %s --%s takes a token on the command line, where ps shows it", strings.Join(path, " "), f.Name)
			}
		})
	})
	logged := 0
	for _, s := range qaAPISamples() {
		t.Run(s.path, func(t *testing.T) {
			for _, mode := range [][]string{{"--dry-run"}, {"--log-level", "debug", "--yes"}} {
				r := srv.run(t, qaStdin, append(s.argv, mode...)...)
				if qaShowsToken(r.stdout + r.stderr) {
					t.Errorf("%s prints the account's auth token", strings.Join(mode, " "))
				}
				if mode[0] == "--log-level" && strings.Contains(r.stderr, "] https://") {
					logged++
				}
			}
		})
	}
	if logged < 100 {
		t.Errorf("only %d commands log a request at --log-level debug; the samples have stopped reaching the client", logged)
	}
}

// ─── API errors ──────────────────────────────────────────────────────────────

// qaStatuses are API failures and the code each must be reported as.
var qaStatuses = []struct {
	status int
	code   clierr.Code
}{
	{http.StatusUnauthorized, clierr.CodeAuthInvalid},
	{http.StatusNotFound, clierr.CodeResourceNotFound},
	{http.StatusTooManyRequests, clierr.CodeRateLimited},
	{http.StatusInternalServerError, clierr.CodeUpstreamError},
}

// qaErrorOwners files an API-error failure of a command under its finding.
var qaErrorOwners = map[string]string{"api": "r3/F4"}

// TestQA_sweepErrors answers every request of every command with each API
// failure: the command must report that failure's code, and exit with the
// code it maps to, never succeed or report another.
func TestQA_sweepErrors(t *testing.T) {
	setFakeCreds(t)
	srv := newQAServer(t, qaSweepReplies)
	wait := waitBeforeRetry
	t.Cleanup(func() { waitBeforeRetry, srv.handler = wait, nil })
	waitBeforeRetry = func(time.Duration) {}
	for _, s := range qaAPISamples() {
		t.Run(s.path, func(t *testing.T) {
			var p []string
			for _, st := range qaStatuses {
				srv.handler = func(w http.ResponseWriter, _ *http.Request) bool {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(st.status)
					_, _ = io.WriteString(w, `{"api_id": "qa", "error": "the QA server says no"}`)
					return true
				}
				r := srv.run(t, qaStdin, append(s.argv, "--yes")...)
				if m := qaEnvelope.FindStringSubmatch(r.stderr); m == nil || clierr.Code(m[1]) != st.code {
					p = append(p, fmt.Sprintf("HTTP %d: exit %d, want %s (exit %d): %v", st.status, r.exit, st.code, (&clierr.Error{Code: st.code}).ExitCode(), r.err))
				}
			}
			if len(p) > 0 {
				qaVerdict(t, map[string][]string{qaErrorOwners[s.path]: p})
			}
		})
	}
}
