package cmd

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/spf13/cobra"
)

// mapDoc and schemaDoc decode the -o json envelopes of --map and --schema.
type mapDoc struct {
	Data commandMap `json:"data"`
}

type schemaDoc struct {
	Data struct {
		commandSchema
		GlobalFlags      []flagSchema  `json:"global_flags"`
		OutputFields     []fieldSchema `json:"output_fields"`
		OutputFieldsNote string        `json:"output_fields_note"`
	} `json:"data"`
}

func flagNames(flags []flagSchema) []string {
	names := make([]string, len(flags))
	for i, f := range flags {
		names[i] = f.Name
	}
	return names
}

func TestMap_describesEveryVisibleCommand(t *testing.T) {
	setFakeCreds(t)
	err, stdout, _ := execCmd(t, "--map", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var doc mapDoc
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("--map -o json is not JSON: %v", err)
	}

	want := []string{"plivo"}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, child := range c.Commands() {
			if !child.Hidden && child.Name() != "help" && child.Name() != "completion" {
				want = append(want, child.CommandPath())
				walk(child)
			}
		}
	}
	walk(rootCmd)
	sort.Strings(want)
	var got []string
	byPath := map[string]commandSchema{}
	for _, c := range doc.Data.Commands {
		got = append(got, c.Path)
		byPath[c.Path] = c
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("--map commands = %v\nwant %v", got, want)
	}

	// Globals are listed once, not on every command.
	globals := flagNames(doc.Data.GlobalFlags)
	for _, name := range []string{"output", "query", "schema", "dry-run", "yes"} {
		if !slices.Contains(globals, name) {
			t.Errorf("global_flags %v lacks --%s", globals, name)
		}
	}
	if !slices.Contains(flagNames(byPath["plivo"].Flags), "map") {
		t.Errorf("root flags %v lack the root-only --map", flagNames(byPath["plivo"].Flags))
	}

	// plivo api's own --query (URL parameters) is a command flag; the
	// global JMESPath one is not repeated there.
	api := byPath["plivo api"]
	if want := []argSchema{{Name: "method", Required: true}, {Name: "path", Required: true}}; !reflect.DeepEqual(api.Args, want) {
		t.Errorf("plivo api args = %+v, want %+v", api.Args, want)
	}
	for _, f := range api.Flags {
		if f.Name == "query" && (f.Type != "stringArray" || !strings.Contains(f.Usage, "key=value")) {
			t.Errorf("plivo api --query = %+v, want the key=value URL parameter flag", f)
		}
		if f.Name == "output" {
			t.Error("plivo api repeats the global --output")
		}
	}

	search := byPath["plivo docs search"]
	if want := []argSchema{{Name: "keywords", Required: true, Repeatable: true}}; !reflect.DeepEqual(search.Args, want) {
		t.Errorf("docs search args = %+v, want %+v", search.Args, want)
	}
	// docs --refresh is persistent on the docs group, so its children take it.
	if !slices.Contains(flagNames(search.Flags), "refresh") {
		t.Errorf("docs search flags %v lack the inherited --refresh", flagNames(search.Flags))
	}
	for _, f := range byPath["plivo voice streams forward"].Flags {
		if f.Name == "to" && !f.Required {
			t.Errorf("voice streams forward --to should be required: %+v", f)
		}
	}
}

func TestMap_tableIsOneLinePerCommand(t *testing.T) {
	setFakeCreds(t)
	err, stdout, _ := execCmd(t, "--map", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"COMMAND", "plivo numbers get <number>", "plivo docs search <keywords...>"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--map table lacks %q", want)
		}
	}
}

// Without --map a bare plivo still prints help and succeeds.
func TestRoot_withoutMapPrintsHelp(t *testing.T) {
	setFakeCreds(t)
	err, stdout, _ := execCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Available Commands:") {
		t.Errorf("bare plivo should print help, got %q", stdout)
	}
	if err, _, _ := execCmd(t, "bogus"); err == nil || !strings.Contains(err.Error(), `unknown command "bogus"`) {
		t.Errorf("an unknown command should still fail, got %v", err)
	}
}

// --schema describes a command without its arguments or required flags, and
// sends nothing.
func TestSchema_describesWithoutRunning(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startCapturingHTTPServer(t, http.StatusOK, `{}`)
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "MAFAKEFORTEST", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })

	cases := []struct {
		args       []string
		path       string
		hasFields  bool
		someField  fieldSchema
		wantFlag   string
		wantArgLen int
	}{
		{[]string{"numbers", "get", "--schema"}, "plivo numbers get", true, fieldSchema{"number", "string"}, "", 1},
		{[]string{"numbers", "list", "--schema"}, "plivo numbers list", true, fieldSchema{"objects[].number", "string"}, "limit", 0},
		{[]string{"sip", "calls", "list", "--schema", "--limit", "5"}, "plivo sip calls list", true, fieldSchema{"meta.total_count", "number"}, "limit", 0},
		// Not a get or list: no registered output type.
		{[]string{"numbers", "update", "--schema"}, "plivo numbers update", false, fieldSchema{}, "app-id", 1},
		// Required flags (--number, --app, --to) are not demanded either.
		{[]string{"voice", "streams", "forward", "--schema"}, "plivo voice streams forward", false, fieldSchema{}, "to", 0},
		{[]string{"--schema"}, "plivo", false, fieldSchema{}, "map", 0},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			err, stdout, _ := execCmd(t, append(tc.args, "-o", "json")...)
			if err != nil {
				t.Fatal(err)
			}
			var doc schemaDoc
			if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
				t.Fatalf("not JSON: %v\n%s", err, stdout)
			}
			d := doc.Data
			if d.Path != tc.path || len(d.Args) != tc.wantArgLen {
				t.Errorf("path %q args %+v, want %q with %d args", d.Path, d.Args, tc.path, tc.wantArgLen)
			}
			if tc.wantFlag != "" && !slices.Contains(flagNames(d.Flags), tc.wantFlag) {
				t.Errorf("flags %v lack --%s", flagNames(d.Flags), tc.wantFlag)
			}
			if !slices.Contains(flagNames(d.GlobalFlags), "output") {
				t.Errorf("global_flags %v lack --output", flagNames(d.GlobalFlags))
			}
			switch {
			case tc.hasFields && !containsField(d.OutputFields, tc.someField):
				t.Errorf("output_fields lack %+v: %+v", tc.someField, d.OutputFields)
			case tc.hasFields && !strings.Contains(d.OutputFieldsNote, "subset of the raw API response"):
				t.Errorf("output_fields_note = %q, want it labelled a subset of the raw response", d.OutputFieldsNote)
			case !tc.hasFields && (d.OutputFields != nil || d.OutputFieldsNote == ""):
				t.Errorf("unregistered: want null output_fields and a note, got %+v / %q", d.OutputFields, d.OutputFieldsNote)
			}
			if !tc.hasFields && !strings.Contains(stdout, `"output_fields": null`) {
				t.Errorf("unregistered output_fields should be null in the JSON")
			}
		})
	}
	if got := hits(); len(got) != 0 {
		t.Errorf("--schema sent requests: %v", got)
	}
}

func containsField(fields []fieldSchema, want fieldSchema) bool {
	for _, f := range fields {
		if f == want {
			return true
		}
	}
	return false
}

// The schema is not the command's own output, so a stream command's schema
// takes any format, and a table describes it for people.
func TestSchema_formats(t *testing.T) {
	setFakeCreds(t)
	err, stdout, _ := execCmd(t, "ask", "--schema", "-o", "yaml")
	if err != nil || !strings.Contains(stdout, "path: plivo ask") {
		t.Fatalf("ask --schema -o yaml: err %v, out %q", err, stdout)
	}
	err, stdout, _ = execCmd(t, "numbers", "get", "--schema", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"plivo numbers get <number>", "FIELD", "monthly_rental_rate"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--schema table lacks %q:\n%s", want, stdout)
		}
	}
	err, stdout, _ = execCmd(t, "numbers", "get", "--schema", "--query", "data.args[0].name")
	if err != nil || strings.TrimSpace(stdout) != `"number"` {
		t.Errorf("--schema --query: err %v, out %q", err, stdout)
	}
	if err, _, _ := execCmd(t, "numbers", "get", "--schema", "-o", "tsv"); err == nil || !strings.Contains(err.Error(), "BAD_INPUT") {
		t.Errorf("--schema -o tsv: want BAD_INPUT, got %v", err)
	}
}

// "--schema" as another flag's value is that value, not a request for the
// schema, and the command's repeatable flags are not doubled by the look.
func TestSchema_asAFlagValueRunsTheCommand(t *testing.T) {
	setFakeCreds(t)
	err, stdout, stderr := execCmd(t, "api", "POST", "/Message/", "--body", "--schema", "--query", "a=1", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "" || !strings.Contains(stderr, "[dry-run] POST") || !strings.Contains(stderr, "--schema") {
		t.Errorf("want the api dry run with body --schema, got stdout %q stderr %q", stdout, stderr)
	}
	if strings.Count(stderr, "a=1") != 1 {
		t.Errorf("the query parameter should be sent once: %q", stderr)
	}
}

func TestSchema_unknownCommandIsCobrasError(t *testing.T) {
	setFakeCreds(t)
	err, stdout, _ := execCmd(t, "bogus", "--schema")
	if err == nil || !strings.Contains(err.Error(), `unknown command "bogus"`) || stdout != "" {
		t.Errorf("want cobra's unknown command error, got %v / %q", err, stdout)
	}
}

// Every get and list command decodes into an api type and is in
// outputTypes, except these: the ones that read local data rather than the
// API, and numbers compliance get, whose response nests the application in
// a shape its api type does not describe.
func TestOutputTypes_coverEveryGetAndList(t *testing.T) {
	wantGaps := []string{"plivo auth list", "plivo config get", "plivo docs list", "plivo numbers compliance get", "plivo skill list"}
	if c, _, err := rootCmd.Find([]string{"auth", "token", "list"}); err == nil && c.Name() == "list" {
		wantGaps = append(wantGaps, c.CommandPath()) // internal build only; prints its raw body
		sort.Strings(wantGaps)
	}
	var gaps []string
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, child := range c.Commands() {
			walk(child)
		}
		if c.Name() != "get" && c.Name() != "list" {
			return
		}
		if _, ok := outputTypes[c.CommandPath()]; !ok {
			gaps = append(gaps, c.CommandPath())
		}
	}
	walk(rootCmd)
	sort.Strings(gaps)
	if !reflect.DeepEqual(gaps, wantGaps) {
		t.Errorf("get/list commands without an output type: %v, want %v", gaps, wantGaps)
	}
	for path := range outputTypes {
		c, _, err := rootCmd.Find(strings.Fields(path)[1:])
		if err != nil || c.CommandPath() != path {
			t.Errorf("outputTypes names %q, which is not a command", path)
		}
	}
}

func TestOutputFields_followJSONTags(t *testing.T) {
	type inner struct {
		Name string `json:"name"`
	}
	type embedded struct {
		Promoted int `json:"promoted"`
	}
	type sample struct {
		api.RawBody
		embedded
		ID      string            `json:"id"`
		Rate    *float64          `json:"rate"`
		Tags    []string          `json:"tags"`
		Items   []inner           `json:"items"`
		Owner   *inner            `json:"owner"`
		Extra   json.RawMessage   `json:"extra"`
		Labels  map[string]string `json:"labels"`
		Skipped string            `json:"-"`
		Enabled bool              `json:"enabled,omitempty"`
	}
	got := outputFields(reflect.TypeFor[sample]())
	want := []fieldSchema{
		{"promoted", "number"},
		{"id", "string"},
		{"rate", "number"},
		{"tags", "array"}, {"tags[]", "string"},
		{"items", "array"}, {"items[]", "object"}, {"items[].name", "string"},
		{"owner", "object"}, {"owner.name", "string"},
		{"extra", "any"},
		{"labels", "object"},
		{"enabled", "boolean"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("outputFields =\n%+v\nwant\n%+v", got, want)
	}
}
