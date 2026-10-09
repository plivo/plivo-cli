package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

// listPage is a list response as the API sends it: keys in the server's
// order, a nested object, an array, null, numbers, and strings that would
// read back as other types if written bare.
const listPage = `{"api_id":"00000000-0000-0000-0000-000000000000",` +
	`"meta":{"limit":20,"next":null,"offset":0,"total_count":2},` +
	`"objects":[` +
	`{"number":"+14155551234","alias":null,"monthly_rental_rate":"0.80000","voice_enabled":true,"application":{"app_id":"1","name":"a"}},` +
	`{"number":"+14155556789","sms_rate":0.0075,"voice_enabled":false,"tags":["x","y"]}]}`

// render configures the writers, writes body through JSONRaw (the path every
// API-backed command takes) and restores the default afterwards.
func render(t *testing.T, format, query, body string) (string, error) {
	t.Helper()
	if err := Configure(format, query); err != nil {
		t.Fatalf("Configure(%q, %q): %v", format, query, err)
	}
	t.Cleanup(func() { _ = Configure("", "") })
	var buf bytes.Buffer
	err := JSONRaw(&buf, json.RawMessage(body))
	return buf.String(), err
}

func TestEncodings_listResponse(t *testing.T) {
	cases := []struct {
		format string
		want   string
	}{
		{"jsonl", `{"number":"+14155551234","alias":null,"monthly_rental_rate":"0.80000","voice_enabled":true,"application":{"app_id":"1","name":"a"}}
{"number":"+14155556789","sms_rate":0.0075,"voice_enabled":false,"tags":["x","y"]}
`},
		// One row per object; the header is every key in the order first
		// seen, nested values are JSON, null and missing keys are empty.
		{"csv", `number,alias,monthly_rental_rate,voice_enabled,application,sms_rate,tags
+14155551234,,0.80000,true,"{""app_id"":""1"",""name"":""a""}",,
+14155556789,,,false,,0.0075,"[""x"",""y""]"
`},
		// The whole envelope, block style, keys in the server's order, and
		// strings quoted wherever bare YAML would read them as a number.
		{"yaml", `data:
  api_id: 00000000-0000-0000-0000-000000000000
  meta:
    limit: 20
    next: null
    offset: 0
    total_count: 2
  objects:
    - number: "+14155551234"
      alias: null
      monthly_rental_rate: "0.80000"
      voice_enabled: true
      application:
        app_id: "1"
        name: a
    - number: "+14155556789"
      sms_rate: 0.0075
      voice_enabled: false
      tags:
        - x
        - "y"
`},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			got, err := render(t, tc.format, "", listPage)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestEncodings_singleObjectIsOneRecord(t *testing.T) {
	const body = `{"call_uuid":"00000000-0000-0000-0000-000000000000","bill_duration":32,"to_number":"+14155551234"}`
	cases := map[string]string{
		"jsonl": body + "\n",
		"csv":   "call_uuid,bill_duration,to_number\n00000000-0000-0000-0000-000000000000,32,+14155551234\n",
	}
	for format, want := range cases {
		t.Run(format, func(t *testing.T) {
			got, err := render(t, format, "", body)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// A list whose objects are null (an empty list from some endpoints) has no
// records, rather than one record holding the page.
func TestEncodings_nullObjectsHaveNoRecords(t *testing.T) {
	const body = `{"api_id":"x","meta":{"total_count":0},"objects":null}`
	for _, format := range []string{"jsonl", "csv"} {
		got, err := render(t, format, "", body)
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Errorf("-o %s: got %q, want no output", format, got)
		}
	}
}

// Strings that are numbers, booleans, null or empty must stay strings, for
// YAML 1.1 readers too ("on", "no", "1:30"), and control characters (which
// JSON allows raw inside strings, but a YAML parser rejects) must come out
// escaped rather than fail the encode.
func TestYAML_keepsStringsStringsAndEscapesControls(t *testing.T) {
	got, err := render(t, "yaml", "", `{"a":"true","b":"0012","c":"null","d":"","e":"2026-10-08","f":"line one`+"\\n"+`line two","g":"x`+"\u007f"+`y","on":"no","h":"1:30","i":"https://example.com/a"}`)
	if err != nil {
		t.Fatal(err)
	}
	const want = `data:
  a: "true"
  b: "0012"
  c: "null"
  d: ""
  e: "2026-10-08"
  f: |-
    line one
    line two
  g: "x\x7Fy"
  "on": "no"
  h: "1:30"
  i: https://example.com/a
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestQuery_runsOnTheWholeEnvelope(t *testing.T) {
	cases := []struct {
		format, query, want string
	}{
		{"json", "data.objects[].number", "[\n  \"+14155551234\",\n  \"+14155556789\"\n]\n"},
		{"json", "data.meta.total_count", "2\n"},
		// A filter compares numbers, so decoded numbers must be float64.
		{"json", "length(data.objects[?sms_rate < `0.01`])", "1\n"},
		{"jsonl", "data.objects[].number", "\"+14155551234\"\n\"+14155556789\"\n"},
		// A list of values is one bare value per line: no header, no quotes.
		{"csv", "data.objects[].number", "+14155551234\n+14155556789\n"},
		{"csv", "data.objects[].[number, voice_enabled]", "+14155551234,true\n+14155556789,false\n"},
		{"csv", "data.meta", "limit,next,offset,total_count\n20,,0,2\n"},
		{"yaml", "data.objects[0].application", "app_id: \"1\"\nname: a\n"},
		// No match is null, which has no records to write.
		{"json", "data.nope", "null\n"},
		{"csv", "data.nope", ""},
	}
	for _, tc := range cases {
		t.Run(tc.format+" "+tc.query, func(t *testing.T) {
			got, err := render(t, tc.format, tc.query, listPage)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// JMESPath works on float64, which holds integers exactly only up to 2^53.
// Larger ones must come out digit for digit, not rounded.
func TestQuery_keepsLargeIntegersExact(t *testing.T) {
	got, err := render(t, "json", "data.ids", `{"ids":[9007199254740993,123456789012345678901234567890,-7]}`)
	if err != nil {
		t.Fatal(err)
	}
	const want = "[\n  9007199254740993,\n  123456789012345678901234567890,\n  -7\n]\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestConfigure_rejectsAnExpressionThatDoesNotParse(t *testing.T) {
	t.Cleanup(func() { _ = Configure("", "") })
	for _, expr := range []string{"data.objects[", "data..x", "[?", "`"} {
		err := Configure("json", expr)
		var qe *QueryError
		if !errors.As(err, &qe) {
			t.Errorf("Configure(%q): want *QueryError, got %v", expr, err)
			continue
		}
		if qe.Expr != expr {
			t.Errorf("QueryError.Expr = %q, want %q", qe.Expr, expr)
		}
	}
}

// A query can parse yet fail on the data (an unknown function, a wrong
// argument type). That is still the flag's fault, and nothing is written.
func TestQuery_runtimeFailureWritesNothing(t *testing.T) {
	for _, expr := range []string{"lenght(data.objects)", "abs(data.api_id)"} {
		got, err := render(t, "json", expr, listPage)
		var qe *QueryError
		if !errors.As(err, &qe) {
			t.Errorf("%s: want *QueryError, got %v", expr, err)
		}
		if got != "" {
			t.Errorf("%s: wrote %q before failing", expr, got)
		}
	}
}

// Configure with no query and json is the default: JSONSuccess must keep
// writing the exact bytes it always has.
func TestConfigure_defaultKeepsJSONBytes(t *testing.T) {
	got, err := render(t, "json", "", `{"b":1,"a":"<x & y>"}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"data\": {\n    \"b\": 1,\n    \"a\": \"<x & y>\"\n  }\n}\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
