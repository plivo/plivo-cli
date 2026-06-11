//go:build internal

// Helpers shared by internal-tagged commands that talk to the Contacto
// gateway (currently `plivo auth token …`). Lives in its own file so the
// shared utilities survive even when a single internal command is dropped.

package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/contacto"
)

// writeJSONStdout pretty-prints a JSON response body to stdout. Falls back to
// writing the body verbatim if it doesn't parse as JSON.
func writeJSONStdout(body []byte) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		_, _ = os.Stdout.Write(body) // fallback if not JSON
		fmt.Fprintln(os.Stdout)
		return
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		_, _ = os.Stdout.Write(body)
		fmt.Fprintln(os.Stdout)
		return
	}
	_, _ = os.Stdout.Write(pretty)
	fmt.Fprintln(os.Stdout)
}

// getContactoClient loads the session and returns a Contacto HTTP client.
func getContactoClient() (*contacto.Client, error) {
	prof, err := config.LoadContacto()
	if err != nil {
		return nil, err
	}
	return contacto.New(prof), nil
}
