package output

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// ─── Resolve ─────────────────────────────────────────────────────────────────

func TestResolve_explicitFormat(t *testing.T) {
	cases := []struct {
		in   string
		want Format
	}{
		{"table", FormatTable},
		{"json", FormatJSON},
		{"JSON", FormatJSON},
		{"Table", FormatTable},
		{"yaml", FormatJSON}, // yaml/tsv aliases route to JSON per current implementation
		{"tsv", FormatJSON},
		{"unknown", FormatTable},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := Resolve(tc.in, nil)
			if got != tc.want {
				t.Errorf("Resolve(%q) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolve_emptyFormatPipedTreatedAsJSON(t *testing.T) {
	// When format is empty and the file is non-TTY (a pipe), Resolve → JSON.
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	got := Resolve("", w)
	if got != FormatJSON {
		t.Errorf("Resolve(\"\", pipe) = %s, want JSON", got)
	}
}

func TestResolve_emptyFormatNilFile_defaultsJSON(t *testing.T) {
	// nil file → not a TTY → JSON. Matches what scripts/AI agents see when
	// they discard or never set stdout.
	got := Resolve("", nil)
	if got != FormatJSON {
		t.Errorf("Resolve(\"\", nil) = %s, want JSON", got)
	}
}

// ─── JSONSuccess ─────────────────────────────────────────────────────────────

func TestJSONSuccess_withoutMeta(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]any{"name": "acme", "id": 42}
	if err := JSONSuccess(&buf, data, nil); err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, buf.String())
	}
	if _, hasMeta := env["meta"]; hasMeta {
		t.Errorf("meta should be absent when nil, got: %v", env)
	}
	d, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("data field missing: %+v", env)
	}
	if d["name"] != "acme" {
		t.Errorf("data.name = %v", d["name"])
	}
}

func TestJSONSuccess_withMeta(t *testing.T) {
	var buf bytes.Buffer
	data := []string{"a", "b"}
	meta := map[string]any{"limit": 20, "offset": 0, "total_count": 2}
	if err := JSONSuccess(&buf, data, meta); err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env["meta"] == nil {
		t.Error("meta should be present")
	}
	arr, ok := env["data"].([]any)
	if !ok {
		t.Fatalf("data wrong shape: %v", env["data"])
	}
	if len(arr) != 2 || arr[0] != "a" {
		t.Errorf("data = %v", arr)
	}
}

func TestJSONSuccess_prettyPrintsWithIndent(t *testing.T) {
	var buf bytes.Buffer
	_ = JSONSuccess(&buf, map[string]any{"a": 1}, nil)
	out := buf.String()
	if !strings.Contains(out, "\n") || !strings.Contains(out, "  ") {
		t.Errorf("expected pretty-print indent; got: %q", out)
	}
}

// ─── JSONError ───────────────────────────────────────────────────────────────

func TestJSONError_includesRequiredFields(t *testing.T) {
	var buf bytes.Buffer
	JSONError(&buf, "AUTH_INVALID", "bad creds", "re-run plivo login",
		"rid-123", "https://example.com/docs", false, 401,
		map[string]any{"flag": "--profile"})

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, buf.String())
	}
	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("no 'error' key: %v", env)
	}
	mustEqual := func(field string, want any) {
		if got := errObj[field]; got != want {
			t.Errorf("error.%s = %v, want %v", field, got, want)
		}
	}
	mustEqual("code", "AUTH_INVALID")
	mustEqual("message", "bad creds")
	mustEqual("hint", "re-run plivo login")
	mustEqual("retryable", false)
	mustEqual("status_code", float64(401))
	mustEqual("request_id", "rid-123")
	mustEqual("docs_url", "https://example.com/docs")
	ctx, ok := errObj["context"].(map[string]any)
	if !ok || ctx["flag"] != "--profile" {
		t.Errorf("context wrong: %v", errObj["context"])
	}
}

func TestJSONError_omitsEmptyOptionalFields(t *testing.T) {
	var buf bytes.Buffer
	JSONError(&buf, "USER_ERROR", "bad", "", "", "", false, 0, nil)
	var env map[string]any
	_ = json.Unmarshal(buf.Bytes(), &env)
	errObj := env["error"].(map[string]any)
	for _, optional := range []string{"hint", "status_code", "request_id", "docs_url", "context"} {
		if _, present := errObj[optional]; present {
			t.Errorf("empty %s should be omitted, got %v", optional, errObj[optional])
		}
	}
	// Required fields stay.
	if errObj["code"] != "USER_ERROR" || errObj["message"] != "bad" {
		t.Errorf("required fields wrong: %v", errObj)
	}
	if errObj["retryable"] != false {
		t.Error("retryable should always be present (even when false)")
	}
}

// ─── PlainError ──────────────────────────────────────────────────────────────

func TestPlainError_humanFormat(t *testing.T) {
	var buf bytes.Buffer
	PlainError(&buf, "AUTH_INVALID", "bad creds", "re-run login",
		"rid-123", "https://docs/x", true, 401)

	out := buf.String()
	for _, want := range []string{"✗ bad creds", "AUTH_INVALID", "401", "re-run login", "rid-123", "https://docs/x"} {
		if !strings.Contains(out, want) {
			t.Errorf("PlainError missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "retryable:") {
		t.Error("retryable=true should be shown")
	}
}

func TestPlainError_skipsBlanks(t *testing.T) {
	var buf bytes.Buffer
	PlainError(&buf, "USER_ERROR", "msg", "", "", "", false, 0)
	out := buf.String()
	for _, banned := range []string{"hint:", "http:", "docs:", "request_id:", "retryable:"} {
		if strings.Contains(out, banned) {
			t.Errorf("PlainError should skip blank fields, but contains %q:\n%s", banned, out)
		}
	}
}

// ─── Table ───────────────────────────────────────────────────────────────────

func TestTable_emptyRows(t *testing.T) {
	var buf bytes.Buffer
	if err := Table(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no results") {
		t.Errorf("empty Table should show '(no results)', got: %q", buf.String())
	}
}

func TestTable_populated(t *testing.T) {
	var buf bytes.Buffer
	rows := [][]string{
		{"UUID", "FROM", "TO"},
		{"abc", "+1", "+2"},
		{"def", "+3", "+4"},
	}
	if err := Table(&buf, rows); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"UUID", "FROM", "TO", "abc", "+1", "+2", "def"} {
		if !strings.Contains(out, want) {
			t.Errorf("Table output missing %q:\n%s", want, out)
		}
	}
}

// ─── KV ──────────────────────────────────────────────────────────────────────

func TestKV_keysAlignedWithColons(t *testing.T) {
	var buf bytes.Buffer
	pairs := [][2]string{
		{"uuid", "abc-123"},
		{"name", "acme"},
		{"status", "active"},
	}
	if err := KV(&buf, pairs); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"uuid:", "name:", "status:", "abc-123", "acme", "active"} {
		if !strings.Contains(out, want) {
			t.Errorf("KV output missing %q:\n%s", want, out)
		}
	}
	// Three lines (one per pair).
	if lines := strings.Count(out, "\n"); lines != 3 {
		t.Errorf("expected 3 lines, got %d:\n%s", lines, out)
	}
}

func TestKV_emptyValueRendersBlank(t *testing.T) {
	var buf bytes.Buffer
	KV(&buf, [][2]string{{"missing", ""}})
	out := buf.String()
	if !strings.Contains(out, "missing:") {
		t.Errorf("empty value key should still render: %q", out)
	}
}
