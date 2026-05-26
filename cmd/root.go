package cmd

import (
	"fmt"
	"os"
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
	if err := rootCmd.Execute(); err != nil {
		handleError(err)
	}
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
	rootCmd.PersistentFlags().StringVar(&hodorServer, "hodor-server", os.Getenv("PLIVO_HODOR_SERVER"),
		"base URL for hodor (used by `plivo agent` and `plivo auth token`)")
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
