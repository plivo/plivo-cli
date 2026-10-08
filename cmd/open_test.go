package cmd

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// fakeBrowser records the URLs plivo open hands to the browser.
func fakeBrowser(t *testing.T, fail error) func() []string {
	t.Helper()
	var mu sync.Mutex
	var opened []string
	browserOpener = func(u string) error {
		mu.Lock()
		defer mu.Unlock()
		opened = append(opened, u)
		return fail
	}
	t.Cleanup(func() { browserOpener = openBrowser })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), opened...)
	}
}

func decodeOpenResult(t *testing.T, stdout string) openResult {
	t.Helper()
	var env struct {
		Data openResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	return env.Data
}

// Each target opens its page; none needs a login.
func TestOpen_targets(t *testing.T) {
	setEmptyHome(t)
	const id = "00000000-0000-0000-0000-000000000000"
	cases := []struct {
		args []string
		want string
	}{
		{nil, "https://cx.plivo.com/home"},
		{[]string{"console"}, "https://cx.plivo.com/home"},
		{[]string{"calls"}, "https://cx.plivo.com/logs/voice"},
		{[]string{"call", id}, "https://cx.plivo.com/logs/voice/" + id},
		{[]string{"sip-call", id}, "https://cx.plivo.com/logs/sip-trunking/" + id},
		{[]string{"docs"}, "https://www.plivo.com/docs/"},
		{[]string{"docs", "voice/api/calls"}, "https://www.plivo.com/docs/voice/api/calls"},
		{[]string{"docs", "/docs/voice/api/calls/"}, "https://www.plivo.com/docs/voice/api/calls"},
		{[]string{"docs", "a b/c?d#e"}, "https://www.plivo.com/docs/a%20b/c%3Fd%23e"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(append([]string{"open"}, tc.args...), " "), func(t *testing.T) {
			opened := fakeBrowser(t, nil)
			err, stdout, _ := execCmd(t, append(append([]string{"open"}, tc.args...), "-o", "json")...)
			if err != nil {
				t.Fatal(err)
			}
			if got := decodeOpenResult(t, stdout); got != (openResult{URL: tc.want, Opened: true}) {
				t.Errorf("got %+v, want %s opened", got, tc.want)
			}
			if got := opened(); len(got) != 1 || got[0] != tc.want {
				t.Errorf("browser got %v, want [%s]", got, tc.want)
			}
		})
	}
}

// For people the URL comes first, alone on stdout; --dry-run stops there.
func TestOpen_tablePrintsTheURLAndDryRunOpensNothing(t *testing.T) {
	setEmptyHome(t)
	opened := fakeBrowser(t, nil)
	err, stdout, _ := execCmd(t, "open", "calls", "-o", "table")
	if err != nil || stdout != "https://cx.plivo.com/logs/voice\n" || len(opened()) != 1 {
		t.Fatalf("err %v, stdout %q, opened %v", err, stdout, opened())
	}
	err, stdout, _ = execCmd(t, "open", "calls", "-o", "table", "--dry-run")
	if err != nil || stdout != "https://cx.plivo.com/logs/voice\n" || len(opened()) != 1 {
		t.Errorf("--dry-run: err %v, stdout %q, opened %v", err, stdout, opened())
	}
	err, stdout, _ = execCmd(t, "open", "--dry-run", "-o", "json")
	if got := decodeOpenResult(t, stdout); err != nil || got.Opened || got.URL != "https://cx.plivo.com/home" {
		t.Errorf("--dry-run -o json: err %v, got %+v", err, got)
	}
}

// No browser is not a failure: the URL is printed, and opened says so.
func TestOpen_noBrowserStillSucceeds(t *testing.T) {
	setEmptyHome(t)
	fakeBrowser(t, errors.New("no display"))
	err, stdout, stderr := execCmd(t, "open", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeOpenResult(t, stdout); got.Opened || got.URL == "" {
		t.Errorf("got %+v, want the URL and opened false", got)
	}
	if !strings.Contains(stderr, "no display") {
		t.Errorf("stderr should say why: %q", stderr)
	}
}

// Bad input fails before anything opens.
func TestOpen_rejectsBadInput(t *testing.T) {
	setEmptyHome(t)
	opened := fakeBrowser(t, nil)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"number", "14155551234"}, `unknown target "number"`},
		{[]string{"call"}, "needs a call UUID"},
		{[]string{"sip-call", "not-a-uuid"}, "needs a call UUID"},
		{[]string{"call", "00000000-0000-0000-0000-000000000000/x"}, "needs a call UUID"},
		{[]string{"console", "extra"}, "takes no argument"},
		{[]string{"docs", "https://example.com/docs/x"}, "not a URL"},
		{[]string{"docs", "a", "b"}, "accepts at most 2 arg(s)"},
	}
	for _, tc := range cases {
		err, stdout, _ := execCmd(t, append([]string{"open"}, tc.args...)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) || stdout != "" {
			t.Errorf("open %v: want an error with %q, got %v / %q", tc.args, tc.want, err, stdout)
		}
		if e, ok := err.(*clierr.Error); ok && e.Code != clierr.CodeBadInput {
			t.Errorf("open %v: code %s, want BAD_INPUT", tc.args, e.Code)
		}
	}
	if got := opened(); len(got) != 0 {
		t.Errorf("bad input still opened %v", got)
	}
}

// Every URL the table can build is https on the console or docs host, and
// the check refuses anything else.
func TestOpen_hostCheck(t *testing.T) {
	for _, target := range openTargets {
		link := target.url("00000000-0000-0000-0000-000000000000")
		if err := checkPlivoURL(link); err != nil {
			t.Errorf("%s builds %s, which fails the host check", target.name, link)
		}
	}
	for _, bad := range []string{"http://cx.plivo.com/home", "https://cx.plivo.com.example.com/", "https://example.com/docs/", "javascript:alert(1)", "https://user@evil.example/"} {
		if checkPlivoURL(bad) == nil {
			t.Errorf("checkPlivoURL(%q) passed", bad)
		}
	}
}
