// Command sdkparams writes cmd/testdata/sdk/requests.json: every request
// the Plivo Go SDK builds, as its method, its path below the account and the
// names of the parameters it can send. The QA suite holds the CLI's requests
// to it (TestQA_sdkRequestKeys).
//
// Run it from the repository root against a checkout of
// github.com/plivo/plivo-go, with `make sdk-params SDK=<path>`. It reads the
// SDK's source only: every client.NewRequest(method, params, path, ...) call,
// and the struct, map or literal its params argument is built from.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// request is one entry of the output. Keys is nil when the SDK builds the
// params in a way this reader cannot follow, and empty when it sends none.
type request struct {
	Method string   `json:"method"`
	Path   string   `json:"path"` // below /v1/Account/{auth_id}/, no slashes around; {} is one segment
	Params []string `json:"params,omitempty"`
	Keys   []string `json:"keys"`
}

type output struct {
	Source   string    `json:"source"`
	Requests []request `json:"requests"`
}

func main() {
	sdk := flag.String("sdk", "", "path to a github.com/plivo/plivo-go checkout")
	out := flag.String("o", "cmd/testdata/sdk/requests.json", "output file")
	flag.Parse()
	if *sdk == "" {
		fail(fmt.Errorf("-sdk is required"))
	}

	fset := token.NewFileSet()
	paths, err := filepath.Glob(filepath.Join(*sdk, "*.go"))
	if err != nil {
		fail(err)
	}
	var files []*ast.File
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			fail(err)
		}
		files = append(files, f)
	}

	r := &reader{structs: map[string]*ast.StructType{}}
	defined := map[string]string{}
	version := ""
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.TypeSpec:
				switch t := n.Type.(type) {
				case *ast.StructType:
					r.structs[n.Name.Name] = t
				case *ast.Ident: // type X Y: X has Y's fields
					defined[n.Name.Name] = t.Name
				}
			case *ast.ValueSpec:
				if len(n.Names) == 1 && n.Names[0].Name == "sdkVersion" && len(n.Values) == 1 {
					version = unquote(n.Values[0])
				}
			}
			return true
		})
	}
	for name, base := range defined {
		if st, ok := r.structs[base]; ok {
			r.structs[name] = st
		}
	}
	merged := map[string]*request{}
	for _, f := range files {
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				r.function(fn, merged)
			}
		}
	}

	o := output{Source: "github.com/plivo/plivo-go v" + version + " (MIT License, Copyright (C) 2018 Plivo Inc.)"}
	for _, req := range merged {
		sort.Strings(req.Params)
		if req.Keys != nil {
			sort.Strings(req.Keys)
		}
		o.Requests = append(o.Requests, *req)
	}
	sort.Slice(o.Requests, func(i, j int) bool {
		a, b := o.Requests[i], o.Requests[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Method < b.Method
	})
	// One request per line, so a regeneration diffs by request.
	var b strings.Builder
	source, _ := json.Marshal(o.Source)
	fmt.Fprintf(&b, "{\n  \"source\": %s,\n  \"requests\": [\n", source)
	for i, req := range o.Requests {
		line, err := json.Marshal(req)
		if err != nil {
			fail(err)
		}
		sep := ","
		if i == len(o.Requests)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "    %s%s\n", line, sep)
	}
	b.WriteString("  ]\n}\n")
	if err := os.WriteFile(*out, []byte(b.String()), 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("wrote %d requests to %s\n", len(o.Requests), *out)
}

type reader struct {
	structs map[string]*ast.StructType
}

var verb = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE)$`)

// function records every NewRequest call in fn.
func (r *reader) function(fn *ast.FuncDecl, merged map[string]*request) {
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 3 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "NewRequest" {
			return true
		}
		method := unquote(call.Args[0])
		lit, ok := call.Args[2].(*ast.BasicLit) // "" is the account itself
		if !verb.MatchString(method) || !ok || lit.Kind != token.STRING {
			return true
		}
		path := strings.Trim(strings.ReplaceAll(unquote(lit), "%s", "{}"), "/")
		keys, params := r.keys(fn, call.Args[1], method)
		k := method + " " + path
		req := merged[k]
		if req == nil {
			req = &request{Method: method, Path: path}
			merged[k] = req
		}
		if params != "" && !contains(req.Params, params) {
			req.Params = append(req.Params, params)
		}
		if keys != nil {
			if req.Keys == nil {
				req.Keys = []string{}
			}
			for _, key := range keys {
				if !contains(req.Keys, key) {
					req.Keys = append(req.Keys, key)
				}
			}
		}
		return true
	})
}

// keys names the parameters expr sends, and the type they come from; nil
// keys when it cannot tell.
func (r *reader) keys(fn *ast.FuncDecl, expr ast.Expr, method string) ([]string, string) {
	switch e := expr.(type) {
	case *ast.Ident:
		if st, ok := r.structs[e.Name]; ok {
			return r.fields(st, method), e.Name
		}
		if e.Name == "nil" {
			return []string{}, ""
		}
		if t := r.identType(fn, e.Name); t != nil {
			return r.keys(fn, t, method)
		}
	case *ast.UnaryExpr:
		return r.keys(fn, e.X, method)
	case *ast.StarExpr:
		return r.keys(fn, e.X, method)
	case *ast.Ellipsis:
		return r.keys(fn, e.Elt, method)
	case *ast.ArrayType:
		return r.keys(fn, e.Elt, method)
	case *ast.CompositeLit:
		if _, ok := e.Type.(*ast.MapType); ok {
			keys := []string{}
			for _, el := range e.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					if k := unquote(kv.Key); k != "" {
						keys = append(keys, k)
					}
				}
			}
			return keys, ""
		}
		return r.keys(fn, e.Type, method)
	case *ast.StructType:
		return r.fields(e, method), ""
	}
	return nil, ""
}

// identType finds what name stands for in fn: a parameter's type, or the
// value a local variable is given.
func (r *reader) identType(fn *ast.FuncDecl, name string) ast.Expr {
	for _, field := range fn.Type.Params.List {
		for _, n := range field.Names {
			if n.Name == name {
				return field.Type
			}
		}
	}
	var found ast.Expr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		switch s := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == name && i < len(s.Rhs) {
					found = r.valueType(fn, s.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			for i, n := range s.Names {
				if n.Name != name {
					continue
				}
				if i < len(s.Values) {
					found = r.valueType(fn, s.Values[i])
				} else if id, ok := s.Type.(*ast.Ident); ok && id.Name != "interface" {
					if _, isStruct := r.structs[id.Name]; isStruct {
						found = id
					}
				}
			}
		}
		return true
	})
	return found
}

// valueType is the type of a value assigned to a variable: a literal's own,
// or the element of the slice or variadic parameter it indexes.
func (r *reader) valueType(fn *ast.FuncDecl, v ast.Expr) ast.Expr {
	switch e := v.(type) {
	case *ast.CompositeLit:
		return e
	case *ast.UnaryExpr:
		return r.valueType(fn, e.X)
	case *ast.IndexExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			return r.identType(fn, id.Name)
		}
	}
	return nil
}

// fields lists a struct's parameter names as encoding/json and
// go-querystring see them: the url tag for a GET (a query), the json tag
// otherwise, with embedded structs flattened.
func (r *reader) fields(st *ast.StructType, method string) []string {
	var keys []string
	for _, f := range st.Fields.List {
		tag := ""
		if f.Tag != nil {
			tag, _ = strconv.Unquote(f.Tag.Value)
		}
		name := tagName(tag, method)
		if name == "-" {
			continue
		}
		if len(f.Names) == 0 && name == "" { // embedded
			if id, ok := deref(f.Type).(*ast.Ident); ok {
				if inner, ok := r.structs[id.Name]; ok {
					keys = append(keys, r.fields(inner, method)...)
				}
			}
			continue
		}
		if name == "" {
			for _, n := range f.Names {
				if ast.IsExported(n.Name) {
					keys = append(keys, n.Name)
				}
			}
			continue
		}
		keys = append(keys, name)
	}
	if keys == nil {
		keys = []string{}
	}
	return keys
}

func tagName(tag, method string) string {
	st := reflect.StructTag(tag)
	first, second := st.Get("json"), st.Get("url")
	if method == "GET" {
		first, second = second, first
	}
	for _, t := range []string{first, second} {
		if name, _, _ := strings.Cut(t, ","); name != "" {
			return name
		}
	}
	return ""
}

func deref(e ast.Expr) ast.Expr {
	if s, ok := e.(*ast.StarExpr); ok {
		return s.X
	}
	return e
}

func unquote(e ast.Expr) string {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return s
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "sdkparams:", err)
	os.Exit(1)
}
