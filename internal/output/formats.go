package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jmespath/go-jmespath"
	"gopkg.in/yaml.v3"
)

// How JSONSuccess and JSONRaw encode for the rest of the run. Set once by
// Configure, from cmd's PersistentPreRunE, before any command body runs.
var (
	encoding  = "json"
	query     *jmespath.JMESPath
	queryExpr string
)

// QueryError is a --query expression that does not compile, or fails while
// it runs (an unknown function, a function given the wrong type).
type QueryError struct {
	Expr string
	Err  error
}

func (e *QueryError) Error() string { return e.Err.Error() }

func (e *QueryError) Unwrap() error { return e.Err }

// Configure sets the encoding of every success envelope written after it:
// format is the --output value (jsonl, yaml or csv; anything else, including
// "" and "table", means json) and expr a JMESPath expression run on the whole
// envelope first ("" for none). A bad expression returns a *QueryError.
func Configure(format, expr string) error {
	encoding = strings.ToLower(format)
	switch encoding {
	case "jsonl", "yaml", "csv":
	default:
		encoding = "json"
	}
	query, queryExpr = nil, ""
	if expr == "" {
		return nil
	}
	q, err := compileQuery(expr)
	if err != nil {
		return &QueryError{Expr: expr, Err: err}
	}
	query, queryExpr = q, expr
	return nil
}

// compileQuery turns a parser panic into an error: the expression is user
// input, so a malformed one must never crash the CLI.
func compileQuery(expr string) (q *jmespath.JMESPath, err error) {
	defer func() {
		if r := recover(); r != nil {
			q, err = nil, fmt.Errorf("cannot parse the expression: %v", r)
		}
	}()
	return jmespath.Compile(expr)
}

// encode is the slow path of JSONSuccess: --query on the envelope, then the
// configured encoding. Everything is built in memory first, so a failing
// query leaves stdout empty.
func encode(w io.Writer, env map[string]any) error {
	doc, err := Marshal(env)
	if err != nil {
		return err
	}
	if query != nil {
		if doc, err = runQuery(doc); err != nil {
			return err
		}
	}
	var out []byte
	switch encoding {
	case "jsonl":
		out = jsonLines(records(doc, query != nil))
	case "yaml":
		if out, err = yamlDoc(doc); err != nil {
			return err
		}
	case "csv":
		if out, err = csvTable(records(doc, query != nil)); err != nil {
			return err
		}
	default:
		var buf bytes.Buffer
		if err := json.Indent(&buf, doc, "", "  "); err != nil {
			return err
		}
		out = append(buf.Bytes(), '\n')
	}
	_, err = w.Write(out)
	return err
}

// runQuery evaluates --query against the envelope and returns the result as
// compact JSON.
func runQuery(doc []byte) (out []byte, err error) {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, &QueryError{Expr: queryExpr, Err: fmt.Errorf("%v", r)}
		}
	}()
	res, err := query.Search(queryNumbers(v))
	if err != nil {
		return nil, &QueryError{Expr: queryExpr, Err: err}
	}
	return Marshal(res)
}

// queryNumbers turns decoded numbers into float64, the only number type
// JMESPath compares and sorts. An integer float64 cannot hold exactly (an id
// past 2^53) stays a json.Number, so its digits come out unchanged.
func queryNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			t[k] = queryNumbers(e)
		}
	case []any:
		for i, e := range t {
			t[i] = queryNumbers(e)
		}
	case json.Number:
		s := t.String()
		if strings.ContainsAny(s, ".eE") {
			if f, err := t.Float64(); err == nil {
				return f
			}
			return t
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n >= -1<<53 && n <= 1<<53 {
			return float64(n)
		}
	}
	return v
}

// records picks what jsonl and csv write one line per: the query result's
// elements (or the result itself), otherwise the envelope's data.objects[]
// (a list response), the elements of data, or data itself. A null result or
// a null objects list has no records.
func records(doc []byte, queried bool) []json.RawMessage {
	if !queried {
		var env map[string]json.RawMessage
		if json.Unmarshal(doc, &env) == nil {
			doc = env["data"]
		}
		var page map[string]json.RawMessage
		if json.Unmarshal(doc, &page) == nil {
			if objects, ok := page["objects"]; ok && (isArray(objects) || string(objects) == "null") {
				doc = objects
			}
		}
	}
	if len(doc) == 0 || string(doc) == "null" {
		return nil
	}
	var rows []json.RawMessage
	if isArray(doc) && json.Unmarshal(doc, &rows) == nil {
		return rows
	}
	return []json.RawMessage{doc}
}

// isArray reports whether compact JSON is an array.
func isArray(raw json.RawMessage) bool { return len(raw) > 0 && raw[0] == '[' }

// jsonLines writes each record as one line of compact JSON. The records are
// slices of Marshal's output, which is already compact.
func jsonLines(rows []json.RawMessage) []byte {
	var buf bytes.Buffer
	for _, r := range rows {
		buf.Write(r)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// csvTable writes records as CSV. When every record is an object the header
// is each key in the order first seen and a missing key is an empty cell;
// otherwise there is no header and an array record is one row of cells.
func csvTable(rows []json.RawMessage) ([]byte, error) {
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	if header, table, ok := objectTable(rows); ok {
		_ = cw.Write(header)
		_ = cw.WriteAll(table)
	} else {
		for _, r := range rows {
			var items []json.RawMessage
			if !isArray(r) || json.Unmarshal(r, &items) != nil {
				items = []json.RawMessage{r}
			}
			cells := make([]string, len(items))
			for i, item := range items {
				cells[i] = csvCell(item)
			}
			_ = cw.Write(cells)
		}
	}
	cw.Flush()
	return buf.Bytes(), cw.Error()
}

// objectTable lays the records out under one header, or reports false when
// any record is not an object.
func objectTable(rows []json.RawMessage) (header []string, table [][]string, ok bool) {
	if len(rows) == 0 {
		return nil, nil, false
	}
	column := map[string]int{}
	objects := make([]map[string]json.RawMessage, len(rows))
	for i, r := range rows {
		keys, fields, isObject := objectFields(r)
		if !isObject {
			return nil, nil, false
		}
		for _, k := range keys {
			if _, seen := column[k]; !seen {
				column[k] = len(header)
				header = append(header, k)
			}
		}
		objects[i] = fields
	}
	for _, fields := range objects {
		row := make([]string, len(header))
		for k, v := range fields {
			row[column[k]] = csvCell(v)
		}
		table = append(table, row)
	}
	return header, table, true
}

// objectFields reads a JSON object's keys in document order (a Go map would
// lose it) and their raw values.
func objectFields(raw json.RawMessage) (keys []string, fields map[string]json.RawMessage, ok bool) {
	if len(raw) == 0 || raw[0] != '{' {
		return nil, nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		return nil, nil, false
	}
	fields = map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, false
		}
		key, _ := tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil, false
		}
		if _, dup := fields[key]; !dup {
			keys = append(keys, key)
		}
		fields[key] = v
	}
	return keys, fields, true
}

// csvCell is one JSON value as CSV text: a string without its quotes, null
// as empty, and numbers, booleans, arrays and objects as their compact JSON.
func csvCell(v json.RawMessage) string {
	if len(v) > 0 && v[0] == '"' {
		var s string
		if json.Unmarshal(v, &s) == nil {
			return s
		}
	}
	if string(v) == "null" {
		return ""
	}
	return string(v)
}

// yamlDoc re-encodes a JSON document as block-style YAML with the same key
// order and two-space indent.
func yamlDoc(doc []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	node, err := yamlNode(dec)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// yamlNode decodes the next JSON value into a yaml.Node. Built from JSON
// tokens rather than by parsing the JSON as YAML, which rejects the raw
// control characters JSON allows inside strings. Numbers, booleans and null
// are written as they came; strings go through yamlString.
func yamlNode(dec *json.Decoder) (*yaml.Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		n := &yaml.Node{Kind: yaml.SequenceNode}
		if t == '{' {
			n.Kind = yaml.MappingNode
		}
		for dec.More() {
			if n.Kind == yaml.MappingNode {
				k, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := k.(string)
				n.Content = append(n.Content, yamlString(key))
			}
			v, err := yamlNode(dec)
			if err != nil {
				return nil, err
			}
			n.Content = append(n.Content, v)
		}
		if _, err := dec.Token(); err != nil { // the closing ] or }
			return nil, err
		}
		return n, nil
	case string:
		return yamlString(t), nil
	case json.Number:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: t.String()}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.FormatBool(t)}, nil
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Value: "null"}, nil
}

// yamlString is a string scalar the encoder quotes whenever it would read
// back as something else. The !!str tag covers YAML 1.2 ("+14155551234",
// "true", "0012"). Encode adds what yaml.v3 also quotes in a Go string for
// YAML 1.1 readers such as PyYAML: booleans like "on" and "no", and base-60
// numbers like "1:30". It costs an encode and a parse, so only strings that
// could be one of those (three bytes or fewer, or holding a colon) pay it.
func yamlString(s string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	if len(s) <= 3 || strings.Contains(s, ":") {
		_ = n.Encode(s)
	}
	return n
}
