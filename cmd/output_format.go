package cmd

import (
	"errors"
	"strings"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// queryFlag backs the persistent --query: a JMESPath expression run on the
// JSON envelope before it is encoded. `plivo api` shadows it with its own
// local --query (URL query parameters), so this one stays "" there.
var queryFlag string

// configureOutput checks --output and --query against the command and hands
// them to the output writers. It runs in PersistentPreRunE, after the
// unsupported-format check, so a bad combination fails before any request.
func configureOutput(cmd *cobra.Command) error {
	format := strings.ToLower(outputFormat)
	if queryFlag != "" {
		switch format {
		case "table":
			err := clierr.BadFlag("query", "filters JSON output, and -o table is not JSON")
			err.Hint = "Drop -o table (--query alone prints JSON), or use -o json, jsonl, yaml or csv."
			err.Context["value"] = queryFlag
			return err
		case "":
			// --query implies JSON, on a terminal too: a filtered result has
			// no table layout. Errors then render as JSON as well.
			outputFormat = "json"
		}
	}
	if streamsEvents(cmd) {
		if format == "yaml" || format == "csv" {
			err := clierr.BadFlag("output", cmd.CommandPath()+" streams one JSON event per line, so it takes -o json or -o jsonl, not "+format)
			err.Hint = "Use -o jsonl (or -o json) for the event stream."
			err.Context["value"] = outputFormat
			return err
		}
		if queryFlag != "" {
			err := clierr.BadFlag("query", cmd.CommandPath()+" streams one JSON event per line, so there is no single result to filter")
			err.Hint = "Drop --query, and filter the -o jsonl stream with a JSON tool instead."
			err.Context["value"] = queryFlag
			return err
		}
	}
	// diagnose prints one result in every other format; its -o jsonl event
	// stream has nothing to filter.
	if cmd.Name() == "diagnose" && format == "jsonl" && queryFlag != "" {
		err := clierr.BadFlag("query", cmd.CommandPath()+" -o jsonl streams one JSON event per line, so there is no single result to filter")
		err.Hint = "Use --query with -o json, yaml or csv to filter the result, or filter the -o jsonl stream with a JSON tool."
		err.Context["value"] = queryFlag
		return err
	}
	if err := output.Configure(outputFormat, queryFlag); err != nil {
		return queryFlagError(err)
	}
	return nil
}

// streamsEvents reports whether cmd writes the assistant's event stream
// straight to stdout instead of one JSON result through output.JSONSuccess.
// Only ask: diagnose prints one result, except with -o jsonl.
func streamsEvents(cmd *cobra.Command) bool {
	return cmd == askCmd
}

// queryFlagError reports a --query that failed to compile or to run as a
// BAD_FLAG. Any other error passes through.
func queryFlagError(err error) error {
	var qe *output.QueryError
	if !errors.As(err, &qe) {
		return err
	}
	e := clierr.BadFlag("query", qe.Error())
	e.Hint = "--query takes a JMESPath expression over the JSON envelope, e.g. --query 'data.objects[].call_uuid'."
	e.Context["value"] = qe.Expr
	return e
}
