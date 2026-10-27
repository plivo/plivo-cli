package cmd

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

const zeroUUID = "00000000-0000-0000-0000-000000000000"

// streamCommands are the commands that write the assistant's event stream
// straight to stdout: ask always, diagnose with -o jsonl (its other formats
// print one result through output.JSONSuccess).
var streamCommands = [][]string{
	{"ask", "what does error 30007 mean?"},
	{"voice", "calls", "diagnose", zeroUUID},
	{"sip", "calls", "diagnose", zeroUUID},
	{"messaging", "sms", "diagnose", zeroUUID},
	{"messaging", "whatsapp", "diagnose", zeroUUID},
	{"messaging", "mms", "diagnose", zeroUUID},
}

// Every command either honours each output format or rejects it with
// BAD_FLAG before running. ask is a stream: it takes json and jsonl (the same
// event stream) and refuses yaml, csv and --query. diagnose has one result, so
// it takes them all except --query on its -o jsonl event stream. Everything
// else takes them all, because its JSON goes through output.JSONSuccess/JSONRaw,
// which encode every format (TestOutputFormats_listCommandsHonourEveryFormat
// runs them). cobra's help and completion print text, not data, so they are
// left out.
func TestOutputFormats_everyCommandHonoursOrRejects(t *testing.T) {
	t.Cleanup(func() { outputFormat, queryFlag = "", ""; _ = output.Configure("", "") })

	wantStreams := map[string]bool{"plivo ask": true}
	var gotStreams []string
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Name() == "help" || c.Name() == "completion" {
			return
		}
		for _, child := range c.Commands() {
			walk(child)
		}
		if !c.Runnable() {
			return
		}
		stream := wantStreams[c.CommandPath()]
		diagnose := c.Name() == "diagnose"
		if streamsEvents(c) {
			gotStreams = append(gotStreams, c.CommandPath())
		}
		for _, f := range []string{"", "json", "jsonl", "yaml", "csv", "table"} {
			for _, q := range []string{"", "data"} {
				outputFormat, queryFlag = f, q
				err := configureOutput(c)
				reject := f == "table" && q != "" ||
					stream && (f == "yaml" || f == "csv" || q != "") ||
					diagnose && f == "jsonl" && q != ""
				switch {
				case reject && !isBadFlag(err):
					t.Errorf("%s -o %q --query %q: want BAD_FLAG, got %v", c.CommandPath(), f, q, err)
				case !reject && err != nil:
					t.Errorf("%s -o %q --query %q: want accepted, got %v", c.CommandPath(), f, q, err)
				case !reject && q != "" && f == "" && outputFormat != "json":
					t.Errorf("%s --query: should imply -o json, got -o %q", c.CommandPath(), outputFormat)
				}
			}
		}
	}
	walk(rootCmd)

	sort.Strings(gotStreams)
	var want []string
	for p := range wantStreams {
		want = append(want, p)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(gotStreams, want) {
		t.Errorf("stream commands = %v, want %v", gotStreams, want)
	}
}

func isBadFlag(err error) bool {
	return err != nil && strings.Contains(err.Error(), "BAD_FLAG")
}

// listPageBody answers every request in the list sweep below.
const listPageBody = `{"api_id":"` + zeroUUID + `","meta":{"limit":20,"offset":0,"total_count":2},` +
	`"objects":[{"id":"a","name":"first","tags":["x"]},{"id":"b","count":2}]}`

// Runs every list command that needs no argument, in every format, against
// one canned page, and checks each output is that command's -o json output
// re-encoded: jsonl and csv one record per object, yaml the same document,
// --query the same data.
func TestOutputFormats_listCommandsHonourEveryFormat(t *testing.T) {
	setFakeCreds(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := listPageBody // rows under "objects", or under the list's own key
		for suffix, key := range map[string]string{"/Brand/": "brands", "/Campaign/": "campaigns", "/Compliance/": "compliances", "/Verify/Session/": "sessions"} {
			if strings.HasSuffix(r.URL.Path, suffix) {
				body = strings.Replace(body, `"objects"`, `"`+key+`"`, 1)
			}
		}
		if strings.HasSuffix(r.URL.Path, "/Masking/Session/") { // meta and rows nested under response
			body = strings.Replace(listPageBody, `"meta"`, `"response":{"meta"`, 1) + "}"
		}
		if strings.HasSuffix(r.URL.Path, "/Conference/") {
			body = `{"api_id":"` + zeroUUID + `","conferences":["first","second"]}`
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{BaseURL: srv.URL, BuddyBaseURL: srv.URL, AuthID: "MAFAKEFORTEST", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })

	paths := listCommandPaths()
	if len(paths) < 20 {
		t.Fatalf("found only %d list commands: %v", len(paths), paths)
	}
	for _, path := range paths {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			run := func(flags ...string) string {
				t.Helper()
				err, stdout, _ := execCmd(t, append(append([]string(nil), path...), flags...)...)
				if err != nil {
					t.Fatalf("%v: %v", flags, err)
				}
				return stdout
			}
			var env map[string]any
			if err := json.Unmarshal([]byte(run("-o", "json")), &env); err != nil {
				t.Fatalf("-o json is not JSON: %v", err)
			}
			rows := rowsOf(env["data"])

			lines := strings.Split(strings.TrimSuffix(run("-o", "jsonl"), "\n"), "\n")
			if len(lines) != len(rows) {
				t.Fatalf("-o jsonl: %d lines for %d records", len(lines), len(rows))
			}
			for i, line := range lines {
				var got any
				if err := json.Unmarshal([]byte(line), &got); err != nil || !reflect.DeepEqual(got, rows[i]) {
					t.Errorf("-o jsonl line %d = %s, want %v", i, line, rows[i])
				}
			}

			table, err := csv.NewReader(strings.NewReader(run("-o", "csv"))).ReadAll()
			if err != nil {
				t.Fatalf("-o csv does not parse: %v", err)
			}
			want := len(rows)
			if _, isObject := rows[0].(map[string]any); isObject {
				want++ // the header
			}
			if len(table) != want {
				t.Errorf("-o csv: %d lines, want %d for %d rows", len(table), want, len(rows))
			}

			var doc any
			if err := yaml.Unmarshal([]byte(run("-o", "yaml")), &doc); err != nil {
				t.Fatalf("-o yaml does not parse: %v", err)
			}
			if !reflect.DeepEqual(viaJSON(t, doc), any(env)) {
				t.Errorf("-o yaml differs from -o json")
			}

			var data any
			if err := json.Unmarshal([]byte(run("--query", "data")), &data); err != nil || !reflect.DeepEqual(data, env["data"]) {
				t.Errorf("--query data = %v, want %v", data, env["data"])
			}
		})
	}
}

// notSwept are list commands the sweep cannot run: docs list reads the
// public docs site, not the API, and the internal build's auth token list
// needs a session of its own.
var notSwept = map[string]bool{"plivo docs list": true, "plivo auth token list": true}

// listCommandPaths finds every list command that runs with no argument and
// no required flag, minus notSwept.
func listCommandPaths() [][]string {
	var out [][]string
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, child := range c.Commands() {
			walk(child)
		}
		if c.Name() != "list" || !c.Runnable() || notSwept[c.CommandPath()] {
			return
		}
		if c.Args != nil && c.Args(c, nil) != nil {
			return
		}
		required := false
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if len(f.Annotations[cobra.BashCompOneRequiredFlag]) > 0 {
				required = true
			}
		})
		if !required {
			out = append(out, strings.Fields(c.CommandPath())[1:])
		}
	}
	walk(rootCmd)
	return out
}

// rowsOf mirrors which records jsonl and csv write: a list's rows (its page's
// one array), an array's elements, or the one value.
func rowsOf(data any) []any {
	if page, ok := data.(map[string]any); ok {
		if nested, ok := page["response"].(map[string]any); ok {
			page = nested
		}
		for _, v := range page {
			if rows, ok := v.([]any); ok {
				return rows
			}
		}
	}
	if items, ok := data.([]any); ok {
		return items
	}
	return []any{data}
}

// viaJSON round-trips v through JSON so YAML's ints compare equal to JSON's
// float64s.
func viaJSON(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// ask refuses what a stream cannot encode before sending anything: yaml, csv
// and --query. diagnose has one result, so it refuses only --query on its
// -o jsonl event stream.
func TestOutputFormats_streamCommandsRejectWhatTheyCannotEncode(t *testing.T) {
	setFakeCreds(t)
	for _, args := range streamCommands {
		refused := [][]string{{"-o", "jsonl", "--query", "data"}}
		if args[0] == "ask" {
			refused = append(refused, []string{"-o", "yaml"}, []string{"-o", "csv"}, []string{"--query", "data"})
		}
		for _, flags := range refused {
			name := strings.Join(args[:len(args)-1], " ") + " " + strings.Join(flags, " ")
			t.Run(name, func(t *testing.T) {
				_, paths := diagnoseServer(t, http.StatusOK)
				err, stdout, _ := execCmd(t, append(append([]string(nil), args...), flags...)...)
				if !isBadFlag(err) {
					t.Fatalf("want BAD_FLAG, got %v", err)
				}
				if hits := paths(); len(hits) != 0 {
					t.Errorf("rejected flags still sent requests: %v", hits)
				}
				if stdout != "" {
					t.Errorf("wrote to stdout: %q", stdout)
				}
			})
		}
	}
}

// -o jsonl is the event stream, one JSON event per line. For ask, -o json is
// that same stream; diagnose has a fixed result, so its -o json is one JSON
// document instead.
func TestOutputFormats_streamCommandsTreatJSONLAsTheEventStream(t *testing.T) {
	setFakeCreds(t)
	t.Setenv("PLIVO_BUDDY_URL", "")
	for _, args := range streamCommands {
		t.Run(strings.Join(args[:len(args)-1], " "), func(t *testing.T) {
			diagnoseServer(t, http.StatusOK)
			err, asJSON, _ := execCmd(t, append(append([]string(nil), args...), "-o", "json")...)
			if err != nil {
				t.Fatalf("-o json: %v", err)
			}
			err, asJSONL, _ := execCmd(t, append(append([]string(nil), args...), "-o", "jsonl")...)
			if err != nil {
				t.Fatalf("-o jsonl: %v", err)
			}
			if !strings.Contains(asJSONL, `"event":"final"`) {
				t.Errorf("-o jsonl = %q, want the event stream", asJSONL)
			}
			if args[0] == "ask" {
				if asJSONL != asJSON {
					t.Errorf("ask -o jsonl = %q, want the -o json stream %q", asJSONL, asJSON)
				}
				return
			}
			if data, _ := decodeOne(t, asJSON)["data"].(map[string]any); data["what_happened"] == nil {
				t.Errorf("diagnose -o json = %q, want one result", asJSON)
			}
		})
	}
}

func TestOutputFormats_queryNeedsJSONAndValidSyntax(t *testing.T) {
	setFakeCreds(t)
	cases := []struct {
		name string
		args []string
	}{
		{"with -o table", []string{"numbers", "list", "-o", "table", "--query", "data"}},
		{"unparseable", []string{"numbers", "list", "--query", "data.objects["}},
		{"unparseable with yaml", []string{"sip", "trunks", "list", "-o", "yaml", "--query", "[?"}},
		{"unknown function", []string{"numbers", "list", "--query", "lenght(data.objects)"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, hits := startCapturingHTTPServer(t, http.StatusOK, listPageBody)
			clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "MAFAKEFORTEST", AuthToken: "tok", HTTP: &http.Client{}}
			t.Cleanup(func() { clientForTest = nil })

			err, stdout, _ := execCmd(t, tc.args...)
			if !isBadFlag(err) || !strings.Contains(err.Error(), "--query") {
				t.Fatalf("want BAD_FLAG naming --query, got %v", err)
			}
			if len(hits()) != 0 || stdout != "" {
				t.Errorf("a rejected --query still ran: requests %v, stdout %q", hits(), stdout)
			}
		})
	}
}

// A query that parses but fails on the data surfaces after the request, and
// is still reported as a bad --query.
func TestOutputFormats_queryFailingOnTheDataIsBadFlag(t *testing.T) {
	setFakeCreds(t)
	srv, _ := startCapturingHTTPServer(t, http.StatusOK, listPageBody)
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "MAFAKEFORTEST", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })

	err, stdout, _ := execCmd(t, "numbers", "list", "--query", "abs(data.objects)")
	var qe *output.QueryError
	if !errors.As(err, &qe) {
		t.Fatalf("want *output.QueryError, got %v", err)
	}
	if stdout != "" {
		t.Errorf("wrote %q before failing", stdout)
	}
	if got := queryFlagError(err); !isBadFlag(got) || !strings.Contains(got.Error(), "Invalid type") {
		t.Errorf("queryFlagError = %v, want BAD_FLAG naming the type error", got)
	}
}

// After a write the API accepted, an error would read as "nothing happened"
// and invite a second send: the result is printed whole, with a warning.
func TestOutputFormats_queryFailingAfterAWritePrintsTheResult(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	sipWriteServer(t, trunksUsingU1)
	err, stdout, stderr := execCmd(t, "sip", "uris", "create", "--name", "n", "--uri", "sip.example.com", "--query", "abs(data)")
	if err != nil || !strings.Contains(stdout, `"uri_uuid": "U1"`) || !strings.Contains(stderr, "do not run it again") {
		t.Errorf("want exit 0, the unfiltered result and a warning, got %v:\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
}

// plivo api keeps its own --query for URL parameters; a JMESPath expression
// there gets a hint saying so instead of a bare "expected key=value".
func TestAPI_queryIsURLParamsWithAHint(t *testing.T) {
	setFakeCreds(t)
	var mu sync.Mutex
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(listPageBody))
	}))
	t.Cleanup(srv.Close)
	apiClientForTest = &api.Client{BaseURL: srv.URL, AuthID: "MAFAKEFORTEST", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { apiClientForTest = nil })
	sent := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), queries...)
	}

	err, _, _ := execCmd(t, "api", "GET", "/Number/", "--query", "data.objects[].number")
	if e, ok := err.(*clierr.Error); !ok || e.Code != clierr.CodeBadFlag || !strings.Contains(e.Hint, "JMESPath") {
		t.Fatalf("want BAD_FLAG with a hint about --query on plivo api, got %#v", err)
	}
	if len(sent()) != 0 {
		t.Errorf("sent requests: %v", sent())
	}

	// key=value is still a URL parameter, and the output formats still apply.
	err, stdout, _ := execCmd(t, "api", "GET", "/Number/", "--query", "limit=2", "-o", "csv")
	if err != nil {
		t.Fatal(err)
	}
	if got := sent(); len(got) != 1 || got[0] != "limit=2" {
		t.Errorf("URL query = %v, want [limit=2]", got)
	}
	if want := "id,name,tags,count\na,first,\"[\"\"x\"\"]\",\nb,,,2\n"; stdout != want {
		t.Errorf("-o csv = %q, want %q", stdout, want)
	}
}

// Endpoint and subaccount tables put each field the API sends in its column:
// app_id comes from the endpoint's application URI.
func TestTables_putTheAPIsFieldsInTheirColumns(t *testing.T) {
	setFakeCreds(t)
	ep := `{"alias":"callme","application":"/v1/Account/CIFAKEPLACEHOLDER001/Application/33406267401237901/","endpoint_id":"32866729519064","sip_uri":"sip:callme140703093944@phone.plivo.com","sub_account":null,"username":"callme140703093944"}`
	sub := `{"auth_id":"SAQASUBACCOUNT1","created":"2017-05-22","enabled":false,"modified":"2017-05-23","name":"shop"}`
	list := func(obj string) string { return `{"meta":{"total_count":1},"objects":[` + obj + `]}` }
	for _, tc := range [][3]string{
		{list(ep), "voice endpoints list", "ENDPOINT_ID USERNAME ALIAS SIP_URI APP_ID 32866729519064 callme140703093944 callme sip:callme140703093944@phone.plivo.com 33406267401237901"},
		{ep, "voice endpoints get 32866729519064", "endpoint_id: 32866729519064 username: callme140703093944 alias: callme sip_uri: sip:callme140703093944@phone.plivo.com app_id: 33406267401237901 sub_account:"},
		{list(sub), "account subaccounts list", "AUTH_ID NAME ENABLED CREATED SAQASUBACCOUNT1 shop false 2017-05-22"},
		{sub, "account subaccounts get SAQASUBACCOUNT1", "auth_id: SAQASUBACCOUNT1 name: shop enabled: false created: 2017-05-22 modified: 2017-05-23"},
	} {
		sipServer(t, http.StatusOK, tc[0])
		err, stdout, _ := execCmd(t, append(strings.Fields(tc[1]), "-o", "table")...)
		if got := strings.Join(strings.Fields(stdout), " "); err != nil || got != tc[2] {
			t.Errorf("%s: %v\n got %s\nwant %s", tc[1], err, got, tc[2])
		}
	}
}
