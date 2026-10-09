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
// straight to stdout rather than one result through output.JSONSuccess.
var streamCommands = [][]string{
	{"ask", "what does error 30007 mean?"},
	{"voice", "calls", "diagnose", zeroUUID},
	{"sip", "calls", "diagnose", zeroUUID},
	{"messaging", "sms", "diagnose", zeroUUID},
	{"messaging", "whatsapp", "diagnose", zeroUUID},
	{"messaging", "mms", "diagnose", zeroUUID},
}

// Every command either honours each output format or rejects it with
// BAD_FLAG before running. Streams take json and jsonl (the same event
// stream) and refuse yaml, csv and --query; everything else takes them all,
// because its JSON goes through output.JSONSuccess/JSONRaw, which encode
// every format (TestOutputFormats_listCommandsHonourEveryFormat runs them).
// cobra's help and completion print text, not data, so they are left out.
func TestOutputFormats_everyCommandHonoursOrRejects(t *testing.T) {
	t.Cleanup(func() { outputFormat, queryFlag = "", ""; _ = output.Configure("", "") })

	wantStreams := map[string]bool{}
	for _, args := range streamCommands {
		wantStreams["plivo "+strings.Join(args[:len(args)-1], " ")] = true
	}
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
		if streamsEvents(c) {
			gotStreams = append(gotStreams, c.CommandPath())
		}
		for _, f := range []string{"", "json", "jsonl", "yaml", "csv", "table"} {
			for _, q := range []string{"", "data"} {
				outputFormat, queryFlag = f, q
				err := configureOutput(c)
				reject := f == "table" && q != "" ||
					stream && (f == "yaml" || f == "csv" || q != "")
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
		_, _ = w.Write([]byte(listPageBody))
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
			if _, isObject := rows[0].(map[string]any); isObject && len(table) != len(rows)+1 {
				t.Errorf("-o csv: %d lines, want a header and %d rows", len(table), len(rows))
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

// rowsOf mirrors which records jsonl and csv write: a list's objects, an
// array's elements, or the one value.
func rowsOf(data any) []any {
	if page, ok := data.(map[string]any); ok {
		if objects, ok := page["objects"].([]any); ok {
			return objects
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

func TestOutputFormats_streamCommandsRejectYAMLCSVAndQuery(t *testing.T) {
	setFakeCreds(t)
	for _, args := range streamCommands {
		for _, flags := range [][]string{{"-o", "yaml"}, {"-o", "csv"}, {"--query", "data"}, {"-o", "jsonl", "--query", "data"}} {
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

// Until the stream commands get a fixed JSON result, -o jsonl and -o json
// are the same event stream, one JSON event per line.
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
			if asJSONL != asJSON || !strings.Contains(asJSONL, `"event":"final"`) {
				t.Errorf("-o jsonl = %q, want the -o json stream %q", asJSONL, asJSON)
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

	err, stdout, _ := execCmd(t, "numbers", "list", "--query", "lenght(data.objects)")
	var qe *output.QueryError
	if !errors.As(err, &qe) {
		t.Fatalf("want *output.QueryError, got %v", err)
	}
	if stdout != "" {
		t.Errorf("wrote %q before failing", stdout)
	}
	if got := queryFlagError(err); !isBadFlag(got) || !strings.Contains(got.Error(), "lenght") {
		t.Errorf("queryFlagError = %v, want BAD_FLAG naming the function", got)
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
