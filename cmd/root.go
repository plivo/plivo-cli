package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/plivo/plivo-cli/internal/version"
	"github.com/spf13/cobra"
)

var (
	profileFlag  string
	outputFormat string
	quietFlag    bool
	noColorFlag  bool
	logLevel     string
	yesFlag      bool
	dryRunFlag   bool
	explainFlag  bool
	timeoutSec   int
	allFlag      bool
	hodorServer  string
)

var rootCmd = &cobra.Command{
	Use:   "plivo",
	Short: "Plivo CLI — manage messaging, voice, numbers, applications",
	Long: `plivo is a command-line interface for the Plivo REST API.

Credentials resolve in order:
  1. --profile flag
  2. active profile in ~/.plivo/config.toml
  3. PLIVO_AUTH_ID / PLIVO_AUTH_TOKEN env vars`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       version.Value,
}

func Execute() {
	rootCmd.SetArgs(rewriteLegacyArgs(os.Args[1:]))
	if err := rootCmd.Execute(); err != nil {
		handleError(err)
	}
}

// legacyAlias maps a pre-grammar top-level command (and its short alias) to its
// new path under the `plivo <service> <resource>` grammar. Lets `plivo call
// list` keep working as `plivo voice calls list`. The canonical (unchanged)
// services — numbers, account, verify, auth, lookup, agent — are absent on
// purpose; they resolve natively.
var legacyAlias = map[string][]string{
	"call":        {"voice", "calls"},
	"stream":      {"voice", "calls", "streams"},
	"conference":  {"voice", "conferences"},
	"conf":        {"voice", "conferences"},
	"mpc":         {"voice", "multiparty"},
	"recording":   {"voice", "recordings"},
	"rec":         {"voice", "recordings"},
	"endpoint":    {"voice", "endpoints"},
	"ep":          {"voice", "endpoints"},
	"message":     {"sms", "messages"},
	"msg":         {"sms", "messages"},
	"brand":       {"sms", "10dlc", "brands"},
	"campaign":    {"sms", "10dlc", "campaigns"},
	"camp":        {"sms", "10dlc", "campaigns"},
	"link":        {"sms", "10dlc", "links"},
	"powerpack":   {"sms", "powerpacks"},
	"pp":          {"sms", "powerpacks"},
	"tollfree":    {"sms", "tollfree"},
	"tfv":         {"sms", "tollfree"},
	"number":      {"numbers"},
	"cnam":        {"numbers", "cnam"},
	"masking":     {"numbers", "masking"},
	"mask":        {"numbers", "masking"},
	"subaccount":  {"account", "subaccounts"},
	"sub":         {"account", "subaccounts"},
	"application": {"account", "applications"},
	"app":         {"account", "applications"},
	"compliance":  {"account", "compliance"},
}

// valueFlags are the global flags that consume the following token as their
// value, so the shim doesn't mistake that value for a command word.
var valueFlags = map[string]bool{
	"--profile": true, "--output": true, "-o": true,
	"--log-level": true, "--timeout": true,
}

// rewriteLegacyArgs expands the first legacy command word it finds into its new
// grammar path, leaving everything else untouched. It skips leading global
// flags (and their values) so forms like `plivo --profile prod call list` still
// rewrite. If the first real command word isn't a legacy alias, args pass
// through unchanged.
func rewriteLegacyArgs(args []string) []string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			continue
		}
		if i > 0 && valueFlags[args[i-1]] {
			continue // this token is a value belonging to the previous flag
		}
		repl, ok := legacyAlias[a]
		if !ok {
			return args // first command word is already new-grammar (or unknown)
		}
		out := make([]string, 0, len(args)+len(repl)-1)
		out = append(out, args[:i]...)
		out = append(out, repl...)
		out = append(out, args[i+1:]...)
		return out
	}
	return args
}

func init() {
	rootCmd.PersistentFlags().StringVar(&profileFlag, "profile", "", "named profile from ~/.plivo/config.toml")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "", "output format: table|json (default: table for TTY, json otherwise)")
	rootCmd.PersistentFlags().BoolVarP(&quietFlag, "quiet", "q", false, "suppress non-data output")
	rootCmd.PersistentFlags().BoolVar(&noColorFlag, "no-color", false, "disable colored output")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "warn", "log level: debug|info|warn|error|none")
	rootCmd.PersistentFlags().BoolVarP(&yesFlag, "yes", "y", false, "skip confirmation prompts")
	rootCmd.PersistentFlags().BoolVar(&dryRunFlag, "dry-run", false, "print the HTTP request without sending")
	rootCmd.PersistentFlags().BoolVar(&explainFlag, "explain", false, "explain what the command will do before executing")
	rootCmd.PersistentFlags().IntVar(&timeoutSec, "timeout", 30, "request timeout in seconds")
	rootCmd.PersistentFlags().BoolVar(&allFlag, "all", false, "auto-paginate through all pages")
	// --hodor-server is registered only in internal builds (cmd/internal_flags.go),
	// since the agent + auth-token surfaces that use it are internal-only. The
	// backing var lives below and stays "" in the public v1 build.
}

// getClient resolves credentials and returns a configured API client.
func getClient() (*api.Client, string, error) {
	p, name, err := config.Resolve(profileFlag)
	if err != nil {
		return nil, "", err
	}
	c := api.New(p.AuthID, p.AuthToken, time.Duration(timeoutSec)*time.Second)
	c.HodorBaseURL = hodorServer
	c.DryRun = dryRunFlag
	if logLevel == "debug" {
		c.LogRequest = func(method, url string, body []byte) {
			fmt.Fprintf(os.Stderr, "[%s] %s\n", method, url)
			if len(body) > 0 {
				fmt.Fprintf(os.Stderr, "  body: %s\n", string(body))
			}
		}
	}
	return c, name, nil
}

// effectiveFormat returns the resolved output format for stdout.
func effectiveFormat() output.Format {
	return output.Resolve(outputFormat, os.Stdout)
}

// handleError is the single place every command error is rendered. It picks
// JSON (stable schema for AI/scripts when stdout is piped) vs plain (human
// terminal output), then exits with a category-stable exit code.
func handleError(err error) {
	f := output.Resolve(outputFormat, os.Stderr)

	// Convert any error into a *clierr.Error so we render a structured
	// envelope no matter what the source was.
	apiErr, ok := err.(*api.APIError)
	if !ok {
		apiErr = clierr.Wrap(err)
	}

	if f == output.FormatJSON {
		output.JSONError(os.Stderr,
			string(apiErr.Code),
			apiErr.Message,
			apiErr.Hint,
			apiErr.RequestID,
			apiErr.DocsURL,
			apiErr.Retryable,
			apiErr.StatusCode,
			apiErr.Context,
		)
	} else {
		output.PlainError(os.Stderr,
			string(apiErr.Code),
			apiErr.Message,
			apiErr.Hint,
			apiErr.RequestID,
			apiErr.DocsURL,
			apiErr.Retryable,
			apiErr.StatusCode,
		)
	}
	os.Exit(exitCodeForAPI(apiErr))
}
