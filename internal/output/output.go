// Package output formats CLI results: tables for interactive shells,
// JSON envelopes when piped or scripted.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"
)

type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
)

// Resolve picks the effective format. Empty input → table for TTY, json otherwise.
//
// Anything Validate() would reject still resolves to JSON here so the error
// renderer in cmd/root.go can produce a structured envelope even when the
// user passed -o yaml or some other unsupported format. The user-visible
// rejection happens earlier in the lifecycle (cmd's PersistentPreRunE),
// before any command body runs.
func Resolve(format string, f *os.File) Format {
	if format == "" {
		if f != nil && term.IsTerminal(int(f.Fd())) {
			return FormatTable
		}
		return FormatJSON
	}
	switch strings.ToLower(format) {
	case "table":
		return FormatTable
	case "json":
		return FormatJSON
	}
	// Unsupported format — fall through to JSON so an error envelope still
	// renders (the input was already rejected by Validate before reaching
	// any RunE).
	return FormatJSON
}

// SupportedFormats lists the formats accepted by --output. Kept tiny on
// purpose — the AI / scripts contract is JSON, the human contract is TABLE.
// Anything else (yaml, tsv, csv, xml, garbage) should be a hard BAD_INPUT
// instead of silently rendering JSON.
var SupportedFormats = []string{"json", "table"}

// Validate returns a non-empty reason string when `format` is set to a value
// that isn't in SupportedFormats. Empty input is always valid (resolves to
// the default per TTY detection). Case-insensitive.
//
// Callers (root.go's PersistentPreRunE) wrap the reason into clierr.BadInput
// so the rejection arrives as the same structured envelope every other
// flag error does.
func Validate(format string) string {
	if format == "" {
		return ""
	}
	switch strings.ToLower(format) {
	case "json", "table":
		return ""
	}
	return "unsupported output format '" + format + "'; supported: " + strings.Join(SupportedFormats, ", ")
}

// JSONSuccess writes {"data": data, "meta": meta?} pretty-printed to w.
func JSONSuccess(w io.Writer, data any, meta any) error {
	env := map[string]any{"data": data}
	if meta != nil {
		env["meta"] = meta
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// JSONRaw writes the upstream response verbatim under "data". json.RawMessage
// marshals itself byte for byte, so nothing is dropped. Falls back to an empty
// object when there was no body.
func JSONRaw(w io.Writer, raw json.RawMessage) error {
	if len(raw) == 0 {
		return JSONSuccess(w, map[string]any{}, nil)
	}
	return JSONSuccess(w, raw, nil)
}

// JSONError writes a structured error envelope. Designed for AI / script
// consumers: stable code, retryable flag, optional hint + context.
//
// Schema:
//
//	{
//	  "error": {
//	    "code":        "AUTH_INVALID",
//	    "message":     "human-readable summary",
//	    "hint":        "actionable next step",
//	    "retryable":   false,
//	    "status_code": 401,
//	    "request_id":  "...",
//	    "docs_url":    "...",
//	    "context":     { ... command-specific ... }
//	  }
//	}
func JSONError(w io.Writer, code, message, hint, requestID, docsURL string, retryable bool, statusCode int, context map[string]any) {
	errObj := map[string]any{
		"code":      code,
		"message":   message,
		"retryable": retryable,
	}
	if hint != "" {
		errObj["hint"] = hint
	}
	if statusCode != 0 {
		errObj["status_code"] = statusCode
	}
	if requestID != "" {
		errObj["request_id"] = requestID
	}
	if docsURL != "" {
		errObj["docs_url"] = docsURL
	}
	if len(context) > 0 {
		errObj["context"] = context
	}
	env := map[string]any{"error": errObj}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(env)
}

// PlainError writes a human-friendly error to w (usually stderr).
//
// Format:
//
//	✗ <Message>
//
//	  code:        AUTH_INVALID
//	  hint:        <actionable next step>
//	  docs:        https://...
//	  request_id:  abc123
//	  retryable:   yes
func PlainError(w io.Writer, code, message, hint, requestID, docsURL string, retryable bool, statusCode int) {
	fmt.Fprintf(w, "✗ %s\n", message)
	fmt.Fprintln(w)
	if code != "" {
		fmt.Fprintf(w, "  code:        %s\n", code)
	}
	if statusCode != 0 {
		fmt.Fprintf(w, "  http:        %d\n", statusCode)
	}
	if hint != "" {
		fmt.Fprintf(w, "  hint:        %s\n", hint)
	}
	if docsURL != "" {
		fmt.Fprintf(w, "  docs:        %s\n", docsURL)
	}
	if requestID != "" {
		fmt.Fprintf(w, "  request_id:  %s\n", requestID)
	}
	if retryable {
		fmt.Fprintf(w, "  retryable:   yes\n")
	}
}

// Table writes a tab-aligned table. rows[0] should be the header row.
func Table(w io.Writer, rows [][]string) error {
	if len(rows) == 0 {
		fmt.Fprintln(w, "(no results)")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	return tw.Flush()
}

// KV writes "key: value" pairs aligned via tabwriter.
func KV(w io.Writer, pairs [][2]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, p := range pairs {
		fmt.Fprintf(tw, "%s:\t%s\n", p[0], p[1])
	}
	return tw.Flush()
}
