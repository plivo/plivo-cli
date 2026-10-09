package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/docs"
	"github.com/spf13/cobra"
)

func TestValidateEnum(t *testing.T) {
	t.Run("unset passes", func(t *testing.T) {
		for _, in := range []string{"", "   "} {
			v := in
			if err := validateEnum("direction", &v, directionValues...); err != nil || v != "" {
				t.Fatalf("validateEnum(%q) = %v, value %q; want nil, \"\"", in, err, v)
			}
		}
	})

	t.Run("documented value passes, trimmed", func(t *testing.T) {
		v := "  inbound "
		if err := validateEnum("direction", &v, directionValues...); err != nil || v != "inbound" {
			t.Fatalf("got %v, value %q; want nil, \"inbound\"", err, v)
		}
	})

	t.Run("unknown value is a BAD_FLAG naming the allowed set", func(t *testing.T) {
		v := "sideways"
		var ce *clierr.Error
		if !errors.As(validateEnum("direction", &v, directionValues...), &ce) || ce.Code != clierr.CodeBadFlag {
			t.Fatalf("want BAD_FLAG, got %#v", ce)
		}
		if ce.Context["flag"] != "direction" || ce.Context["value"] != "sideways" {
			t.Errorf("context = %v", ce.Context)
		}
		if !strings.Contains(ce.Hint, `"inbound", "outbound"`) {
			t.Errorf("hint does not list the allowed values: %q", ce.Hint)
		}
	})

	t.Run("wrong case names the right spelling", func(t *testing.T) {
		v := "UPDATE_required"
		var ce *clierr.Error
		if !errors.As(validateEnum("status", &v, tollfreeStatuses...), &ce) {
			t.Fatal("want an error")
		}
		if !strings.Contains(ce.Hint, `case-sensitive: use "UPDATE_REQUIRED"`) {
			t.Errorf("hint = %q", ce.Hint)
		}
	})
}

func TestValidateEnumList(t *testing.T) {
	v := " voice , sms"
	if err := validateEnumList("services", &v, numberServices...); err != nil || v != "voice,sms" {
		t.Fatalf("got %v, value %q; want nil, \"voice,sms\"", err, v)
	}
	for _, bad := range []string{"voice,fax", "voice,,sms", "sms,", ","} {
		v := bad
		var ce *clierr.Error
		if !errors.As(validateEnumList("services", &v, numberServices...), &ce) || ce.Code != clierr.CodeBadFlag {
			t.Errorf("%q: want BAD_FLAG, got %#v", bad, ce)
		}
	}
}

// The whole point is to fail before the API is asked: an unknown value sent
// through comes back as an empty list with exit 0.
func TestListFilters_unknownValueFailsBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		flag string
		args []string
	}{
		{"direction", []string{"sip", "calls", "list", "--direction", "sideways"}},
		{"hangup-source", []string{"sip", "calls", "list", "--hangup-source", "plivo"}},
		{"stir-verification", []string{"sip", "calls", "list", "--stir-verification", "verified"}},
		{"direction", []string{"sip", "trunks", "list", "--direction", "both"}},
		{"direction", []string{"voice", "calls", "list", "--direction", "Inbound"}},
		{"state", []string{"messaging", "sms", "list", "--state", "read"}},
		{"direction", []string{"messaging", "whatsapp", "list", "--direction", "out"}},
		{"state", []string{"messaging", "mms", "list", "--state", "bogus"}},
		{"type", []string{"numbers", "list", "--type", "bogus"}},
		{"services", []string{"numbers", "list", "--services", "voice,fax"}},
		{"type", []string{"numbers", "search", "--country", "US", "--type", "toll-free"}},
		{"status", []string{"voice", "multiparty", "list", "--status", "live"}},
		{"status", []string{"messaging", "sms", "tollfree", "list", "--status", "IN_REVIEW"}},
		{"status", []string{"verify", "sessions", "list", "--status", "pending"}},
		{"state", []string{"agents", "list", "--state", "draft"}},
		{"status", []string{"numbers", "compliance", "list", "--status", "approved"}},
		{"number-type", []string{"numbers", "compliance", "list", "--number-type", "national"}},
		{"user-type", []string{"numbers", "compliance", "list", "--user-type", "company"}},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			setFakeCreds(t)
			_, count := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

			err, _, _ := execCmd(t, tc.args...)
			var ce *clierr.Error
			if !errors.As(err, &ce) || ce.Code != clierr.CodeBadFlag {
				t.Fatalf("want BAD_FLAG, got %v", err)
			}
			if ce.Context["flag"] != tc.flag {
				t.Errorf("flag in context = %v, want %s", ce.Context["flag"], tc.flag)
			}
			if n := count(); n != 0 {
				t.Fatalf("%d request(s) went out before validation", n)
			}
		})
	}
}

// Every documented value must still reach the API, exactly as spelled there;
// --dry-run must not skip the check.
func TestListFilters_documentedValuesReachTheQuery(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"sip", "calls", "list", "--stir-verification", "Not Verified", "--hangup-source", "zentrunk"},
			[]string{"stir_verification=Not+Verified", "hangup_source=zentrunk"}},
		{[]string{"sip", "trunks", "list", "--direction", "inbound"}, []string{"trunk_direction=inbound"}},
		{[]string{"voice", "calls", "list", "--direction", " outbound "}, []string{"call_direction=outbound"}},
		{[]string{"messaging", "whatsapp", "list", "--state", "received", "--direction", "inbound"},
			[]string{"message_state=received", "message_direction=inbound"}},
		{[]string{"numbers", "list", "--type", "national", "--services", "voice, sms"},
			[]string{"type=national", "services=voice%2Csms"}},
		{[]string{"numbers", "search", "--country", "US", "--type", "national"}, []string{"type=national"}},
		{[]string{"voice", "multiparty", "list", "--status", "initialized"}, []string{"status=initialized"}},
		{[]string{"messaging", "sms", "tollfree", "list", "--status", "UPDATE_REQUIRED"}, []string{"status=UPDATE_REQUIRED"}},
		{[]string{"verify", "sessions", "list", "--status", "in-progress"}, []string{"status=in-progress"}},
		{[]string{"agents", "list", "--state", "PAUSED"}, []string{"state=PAUSED"}},
		{[]string{"numbers", "compliance", "list", "--status", "accepted", "--number-type", "mobile", "--user-type", "individual"},
			[]string{"status=accepted", "number_type=mobile", "user_type=individual"}},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			setFakeCreds(t)
			urls, _ := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

			if err, _, _ := execCmd(t, tc.args...); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := urls()
			if len(got) != 1 {
				t.Fatalf("expected 1 request, got %v", got)
			}
			for _, w := range tc.want {
				if !strings.Contains(got[0], w) {
					t.Errorf("query missing %s\n  got: %s", w, got[0])
				}
			}
		})
	}

	t.Run("dry-run still validates", func(t *testing.T) {
		setFakeCreds(t)
		err, _, _ := execCmd(t, "numbers", "list", "--type", "bogus", "--dry-run")
		var ce *clierr.Error
		if !errors.As(err, &ce) || ce.Code != clierr.CodeBadFlag {
			t.Fatalf("want BAD_FLAG under --dry-run, got %v", err)
		}
	})
}

// The compliance list sends its rows under "compliances", not "objects"; read
// from the wrong key, the table is empty however many applications exist.
func TestComplianceList_tableReadsTheCompliancesKey(t *testing.T) {
	setFakeCreds(t)
	sipServer(t, http.StatusOK, `{"api_id":"x","meta":{"limit":20,"offset":0,"total_count":1},
		"compliances":[{"compliance_id":"00000000-0000-0000-0000-000000000000","alias":"acme-in",
		"status":"accepted","country_iso":"IN","number_type":"local","user_type":"business"}]}`)

	err, stdout, _ := execCmd(t, "numbers", "compliance", "list", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"00000000-0000-0000-0000-000000000000", "acme-in", "accepted"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table missing %q:\n%s", want, stdout)
		}
	}
}

// Every list backed by a paged API holds --limit to 1-20 and --offset to 0 or
// more before any request. Walks the tree, so a list that registers its own
// --limit instead of the shared one fails here.
func TestListFlags_everyPagedListChecksThePageFirst(t *testing.T) {
	var lists []*cobra.Command
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		// docs search is local: its --limit caps search hits, not an API page.
		if c.Flags().Lookup("limit") != nil && c.CommandPath() != "plivo docs search" {
			lists = append(lists, c)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
	if len(lists) < 25 {
		t.Fatalf("found only %d lists with --limit; did the walk break?", len(lists))
	}
	for _, c := range lists {
		path := strings.Fields(c.CommandPath())[1:]
		for range strings.Count(c.Use, "<") {
			path = append(path, "placeholder")
		}
		for _, bad := range [][]string{{"--limit", "21"}, {"--limit", "0"}, {"--offset", "-1"}} {
			args := append(append([]string{}, path...), bad...)
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				setFakeCreds(t)
				_, count := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

				err, _, _ := execCmd(t, args...)
				var ce *clierr.Error
				if !errors.As(err, &ce) || ce.Code != clierr.CodeBadFlag {
					t.Fatalf("want BAD_FLAG, got %v", err)
				}
				if n := count(); n != 0 {
					t.Fatalf("%d request(s) went out before the page check", n)
				}
			})
		}
	}
}

func TestListJSON(t *testing.T) {
	cases := []struct{ name, body, key, want string }{
		{"null rows become []", `{"meta":{},"objects":null}`, "objects", `[]`},
		{"missing rows become []", `{"meta":{}}`, "objects", `[]`},
		{"rows under another key", `{"compliances":null}`, "compliances", `[]`},
		{"rows kept as sent", `{"objects":[{"id":"a"}]}`, "objects", `[{"id":"a"}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := listJSON(&buf, json.RawMessage(tc.body), tc.key); err != nil {
				t.Fatal(err)
			}
			var env struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
				t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
			}
			var got bytes.Buffer
			_ = json.Compact(&got, env.Data[tc.key])
			if got.String() != tc.want {
				t.Fatalf("%s = %s, want %s", tc.key, got.String(), tc.want)
			}
		})
	}

	t.Run("a body that is not an object passes through", func(t *testing.T) {
		var buf bytes.Buffer
		if err := listJSON(&buf, json.RawMessage(`[1,2]`), "objects"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "1,") {
			t.Fatalf("body changed: %s", buf.String())
		}
	})
}

// The commands the empty-list rule was written for: the SIP Trunking ACL list
// sends "objects": null, and the CLI-built lists left their slices nil.
func TestEmptyLists_renderAnEmptyArray(t *testing.T) {
	cases := []struct {
		args []string
		body string
		want string
	}{
		{[]string{"sip", "ip-acl", "list"}, `{"api_id":"x","meta":{"total_count":0},"objects":null}`, `"objects": []`},
		{[]string{"numbers", "compliance", "list"}, `{"api_id":"x","meta":{"total_count":0},"compliances":null}`, `"compliances": []`},
		{[]string{"voice", "conferences", "list"}, `{"api_id":"x"}`, `"conferences": []`},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			setFakeCreds(t)
			sipServer(t, http.StatusOK, tc.body)

			err, stdout, _ := execCmd(t, append(tc.args, "-o", "json")...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Fatalf("want %s in:\n%s", tc.want, stdout)
			}
			if tc.want != `"objects": []` && strings.Contains(stdout, `"objects"`) {
				t.Fatalf("added an objects key to a list that has none:\n%s", stdout)
			}
		})
	}

	t.Run("support with no escalations", func(t *testing.T) {
		setFakeCreds(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"api_id":"x","status":"ok","data":{}}`))
		}))
		t.Cleanup(srv.Close)
		supportClientForTest = &api.Client{BaseURL: srv.URL, BuddyBaseURL: srv.URL, AuthID: "CIFAKEPLACEHOLDER001",
			AuthToken: "tok", AomUUID: "aom-1", HTTP: &http.Client{}}
		t.Cleanup(func() { supportClientForTest = nil })

		err, stdout, _ := execCmd(t, "support", "-o", "json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stdout, `"data": []`) {
			t.Fatalf("want an empty array:\n%s", stdout)
		}
	})

	t.Run("docs list with an empty index", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		t.Cleanup(srv.Close)
		docsFetcherForTest = &docs.Fetcher{HTTP: srv.Client(), CacheDir: t.TempDir(),
			BaseIndex: srv.URL + "/llms.txt", BaseFull: srv.URL + "/llms-full.txt"}
		t.Cleanup(func() { docsFetcherForTest = nil })

		err, stdout, _ := execCmd(t, "docs", "list", "-o", "json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stdout, `"data": []`) {
			t.Fatalf("want an empty array:\n%s", stdout)
		}
	})
}

// The page check goes in front of a pre-run hook the command already has; it
// must not replace it. cobra skips PreRun once PreRunE is set, so a PreRun is
// carried into the chain too.
func TestRegisterListFlags_keepsAnExistingPreRunHook(t *testing.T) {
	var limit, offset int
	var ran []string
	withE := &cobra.Command{Use: "a", PreRunE: func(*cobra.Command, []string) error {
		ran = append(ran, "PreRunE")
		return nil
	}}
	plain := &cobra.Command{Use: "b", PreRun: func(*cobra.Command, []string) { ran = append(ran, "PreRun") }}
	for _, c := range []*cobra.Command{withE, plain} {
		registerListFlags(c, &limit, &offset)
		if err := c.PreRunE(c, nil); err != nil {
			t.Fatalf("%s: unexpected error: %v", c.Use, err)
		}
	}
	if strings.Join(ran, ",") != "PreRunE,PreRun" {
		t.Fatalf("hooks run = %v, want both", ran)
	}

	ran, limit = nil, 0
	var ce *clierr.Error
	if !errors.As(withE.PreRunE(withE, nil), &ce) || ce.Code != clierr.CodeBadFlag || len(ran) != 0 {
		t.Fatalf("a bad page must stop before the hook: err %v, ran %v", ce, ran)
	}
}
