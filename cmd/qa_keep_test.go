package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // TZ=Asia/Kolkata loads on a machine without zoneinfo
)

// keepCases hold what QA confirmed works in v1.3.0, so it keeps working.
// Tokens never printing in --dry-run or --log-level debug output is checked
// by the sweeps, and the SIP trunking paths and names by TestQA_sdkRequestKeys.
func keepCases() []qaCase {
	const call = "d1e2f3a4-0000-4000-8000-000000000001"
	var home string     // the fake home, for checks after the run
	var corrupt []byte  // keep/config-corrupt: the file as it was
	var opened []string // the URLs plivo open handed to the browser
	// day is within the lists' retention; utcRange is it as the Call API reads it.
	day := time.Now().UTC().AddDate(0, 0, -3).Format(time.DateOnly)
	utcRange := "end_time__gte=" + day + "+05%3A30%3A00&end_time__lte=" + day + "+23%3A59%3A59"
	recordOpens := func(t *testing.T) {
		opened = nil
		browserOpener = func(u string) error { opened = append(opened, u); return nil }
	}
	return []qaCase{
		{
			id: "keep/config-mode", title: "the config is saved at 0600 in a 0700 directory, by a rename, whatever their modes were",
			args: args("config set telemetry false"),
			setup: func(t *testing.T) {
				home, _ = os.UserHomeDir()
				dir := filepath.Join(home, ".plivo")
				if err := os.Chmod(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(dir, "config.toml"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			stderr: []string{"telemetry = off"},
			check: func(*qaResult) []string {
				if runtime.GOOS == "windows" {
					return nil
				}
				dir := filepath.Join(home, ".plivo")
				var p []string
				if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
					p = append(p, fmt.Sprintf("%s is %v (%v), want 0700", dir, fi.Mode().Perm(), err))
				}
				if fi, err := os.Stat(filepath.Join(dir, "config.toml")); err != nil || fi.Mode().Perm() != 0o600 {
					p = append(p, fmt.Sprintf("config.toml is %v (%v), want 0600", fi.Mode().Perm(), err))
				}
				left, _ := filepath.Glob(filepath.Join(dir, ".config-*"))
				return append(p, expect(len(left) == 0, "left the temporary files %v", left)...)
			},
		},
		{
			id: "keep/config-corrupt", title: "a config that does not parse is reported, never overwritten",
			args: args("config set telemetry false"),
			setup: func(t *testing.T) {
				home, _ = os.UserHomeDir()
				corrupt = []byte("active = [broken\n")
				qaWriteFile(t, filepath.Join(home, ".plivo", "config.toml"), string(corrupt))
			},
			exit:   ExitUserError,
			stderr: []string{"config.toml"},
			check: func(*qaResult) []string {
				b, err := os.ReadFile(filepath.Join(home, ".plivo", "config.toml"))
				return expect(err == nil && bytes.Equal(b, corrupt), "the config now holds %q (%v)", b, err)
			},
		},
		{
			id: "keep/api-other-host", title: "plivo api refuses a URL to another host and sends nothing",
			args:   args("api GET https://example.com/v1/Account/"),
			exit:   ExitUserError,
			stderr: []string{"BAD_INPUT"},
		},
		{
			id: "keep/open-call", title: "plivo open builds a console URL on cx.plivo.com",
			args:   args("open call " + call),
			setup:  recordOpens,
			stdout: []string{"https://cx.plivo.com/"},
			check: func(*qaResult) []string {
				return expect(len(opened) == 1 && opened[0] == "https://cx.plivo.com/logs/voice/"+call, "opened %q", opened)
			},
		},
		{
			id: "keep/open-docs", title: "plivo open docs keeps a hostile path under plivo.com/docs",
			args:  args("open docs //evil.example/x"),
			setup: recordOpens,
			check: func(r *qaResult) []string {
				if r.exit != 0 {
					return expect(len(opened) == 0, "refused, but opened %q", opened)
				}
				for _, o := range opened {
					if u, err := url.Parse(o); err != nil || u.Scheme != "https" || u.Host != "www.plivo.com" || !qaUnder(u.Path, "/docs/") {
						return []string{fmt.Sprintf("opened %q", o)}
					}
				}
				return expect(len(opened) == 1, "opened %q", opened)
			},
		},
		{
			id: "keep/utc-range", title: "--since and --until are UTC whatever the local time zone",
			args:   []string{"voice", "calls", "list", "--since", day + " 05:30", "--until", day},
			sent:   []string{"GET Call/?" + utcRange + "&limit=20&offset=0"},
			stdout: []string{`"data"`},
			// The run above has this machine's zone; this one has UTC+05:30.
			check: func(*qaResult) []string {
				c := qaCLIProcess("voice", "calls", "list", "--since", day+" 05:30", "--until", day, "--dry-run")
				c.Env = append(c.Env, "TZ=Asia/Kolkata")
				out, err := c.CombinedOutput()
				return expect(err == nil && strings.Contains(string(out), utcRange), "with TZ=Asia/Kolkata: %v\n%s", err, out)
			},
		},
		{
			id: "keep/utc-offset", title: "a --since with an offset is sent as the same instant in UTC",
			args:   args("messaging sms list --since " + day + "T10:00:00+05:30"),
			sent:   []string{"GET Message/?limit=20&message_time__gte=" + day + "+04%3A30%3A00&message_type=sms&offset=0"},
			stdout: []string{`"data"`},
		},
		{
			id: "keep/update-application", title: "an application update sends only the field it changes",
			args:   args("account applications update 31000000000000001 --answer-url https://example.com/answer"),
			sent:   []string{"POST Application/31000000000000001/"},
			stdout: []string{`"data"`},
			check:  func(r *qaResult) []string { return qaBodyIs(r, `{"answer_url": "https://example.com/answer"}`) },
		},
		{
			id: "keep/update-endpoint", title: "an endpoint update sends only the field it changes",
			args:   args("voice endpoints update 12345678 --alias qa2"),
			sent:   []string{"POST Endpoint/12345678/"},
			stdout: []string{`"data"`},
			check:  func(r *qaResult) []string { return qaBodyIs(r, `{"alias": "qa2"}`) },
		},
		{
			id: "keep/update-subaccount", title: "a subaccount update sends only the field it changes",
			args:   args("account subaccounts update SAQASUBACCOUNT1 --enabled=false"),
			sent:   []string{"POST Subaccount/SAQASUBACCOUNT1/"},
			stdout: []string{`"data"`},
			check:  func(r *qaResult) []string { return qaBodyIs(r, `{"enabled": false}`) },
		},
	}
}

// qaBodyIs checks the last request's JSON body is want, no more and no less.
func qaBodyIs(r *qaResult, want string) []string {
	if len(r.reqs) == 0 {
		return []string{"no request to check"}
	}
	var w any
	_ = json.Unmarshal([]byte(want), &w)
	g, _ := json.Marshal(r.reqs[len(r.reqs)-1].Body)
	wb, _ := json.Marshal(w)
	return expect(string(g) == string(wb), "body %s, want %s", g, wb)
}

// qaUnder reports whether a URL path stays below prefix once its dot
// segments are resolved.
func qaUnder(p, prefix string) bool {
	return strings.HasPrefix(path.Clean(p)+"/", prefix)
}
