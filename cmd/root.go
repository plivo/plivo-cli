package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/cliupgrade"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/plivo/plivo-cli/internal/version"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	profileFlag  string
	outputFormat string
	quietFlag    bool
	noColorFlag  bool
	logLevel     string
	yesFlag      bool
	dryRunFlag   bool
	// explainFlag backs --explain. Registered locally (not persistent) by
	// registerExplainFlag, only on the commands that actually read it.
	explainFlag bool
	timeoutSec  int
	adminServer string
	// allFlag backs --all. Registered locally (not persistent) by
	// registerAllFlag — see the removal note on that function for why this
	// is not a persistent flag.
	allFlag bool
)

var rootCmd = &cobra.Command{
	Use:   "plivo",
	Short: "Plivo CLI — manage messaging, voice, numbers, applications",
	Long: `plivo is a command-line interface for the Plivo REST API.

Credentials resolve in order:
  1. --profile flag
  2. PLIVO_AUTH_ID / PLIVO_AUTH_TOKEN env vars
  3. active profile in ~/.plivo/config.toml`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       version.Value,
}

func Execute() {
	cmdErr := rootCmd.Execute()
	if cmdErr != nil {
		handleError(cmdErr)
	}
	firstWord := firstCmdWord(os.Args[1:])
	// Server-driven upgrade nudge (from server warn response headers) wins
	// over the GitHub-cache nudge — when the server has spoken, we trust it
	// and skip the local check.
	if printUpgradeNudge() {
		return
	}
	// Success path only: print a quiet "newer version available" nudge if
	// the upgrade cache has seen a fresher release tag. No-op on errors
	// (don't drown a real failure under nudge noise), in scripts/CI (not a
	// TTY), or when invoked as `plivo upgrade …` itself.
	maybePrintUpdateHint(firstWord)
	// Auto-prompt for feedback once per PromptInterval (24h) on TTY
	// sessions only. Silent no-op for failed commands, scripts, CI,
	// metadata-only invocations (--help/--version/bare plivo),
	// skip-listed commands (feedback/login/logout/upgrade/completion/
	// help/version), and users who haven't yet hit the activity floor.
	if cmdErr == nil {
		maybePromptFeedback(firstWord, os.Args[1:])
	}
}

// commandPath flattens cmd.CommandPath() to a dotted form
// ("plivo voice calls list" → "voice.calls.list") and strips the "plivo"
// root. Lower-cased so analytics dimensions stay consistent.
func commandPath(cmd *cobra.Command) string {
	full := cmd.CommandPath()
	full = strings.TrimPrefix(full, rootCmd.Name())
	full = strings.TrimLeft(full, " ")
	return strings.ReplaceAll(full, " ", ".")
}

// printUpgradeNudge emits the server-driven upgrade-required nudge
// once per invocation. Returns true if it fired (caller skips the
// GitHub-cache nudge). TTY-gated + respects PLIVO_NO_UPDATE_CHECK.
func printUpgradeNudge() bool {
	if os.Getenv("PLIVO_NO_UPDATE_CHECK") != "" {
		return false
	}
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return false
	}
	pending, minVer := cliupgrade.Pending()
	if !pending {
		return false
	}
	if minVer != "" {
		fmt.Fprintf(os.Stderr, "> Your Plivo CLI is below the recommended minimum (%s). Run `plivo upgrade` to update.\n", minVer)
	} else {
		fmt.Fprintln(os.Stderr, "> Your Plivo CLI is out of date. Run `plivo upgrade` to update.")
	}
	return true
}

// firstCmdWord returns the first non-flag arg from argv (without the
// program name). Used to identify which subcommand the user invoked so
// `plivo upgrade` can skip its own nudge.
func firstCmdWord(args []string) string {
	for i, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if i > 0 && valueFlags[args[i-1]] {
			continue
		}
		return a
	}
	return ""
}

// Root returns the fully constructed root command. Used by tools/gendocs to
// render the command reference; it is not part of the runtime path.
func Root() *cobra.Command { return rootCmd }

// valueFlags are the global flags that consume the following token as their
// value. Used by firstCmdWord (powering the post-success update-hint) to
// skip over `--profile staging` so `staging` isn't mistaken for the
// invoked subcommand name.
var valueFlags = map[string]bool{
	"--profile": true, "--output": true, "-o": true,
	"--log-level": true, "--timeout": true,
}

func init() {
	// Single early hook: capture the cobra command path for analytics, and
	// reject unsupported --output values before any RunE fires. Defined here
	// (rather than inside Execute()) so tests that drive rootCmd.Execute()
	// directly see the same gate humans do.
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		api.CLICommand = commandPath(cmd)
		if reason := output.Validate(outputFormat); reason != "" {
			err := clierr.BadInput(reason)
			err.Hint = "Supported formats: " + strings.Join(output.SupportedFormats, ", ") + " (default: table for TTY, json otherwise)."
			err.Context = map[string]any{"flag": "--output", "value": outputFormat}
			return err
		}
		return nil
	}

	rootCmd.PersistentFlags().StringVar(&profileFlag, "profile", "", "named profile from ~/.plivo/config.toml")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "", "output format: table|json (default: table for TTY, json otherwise)")
	rootCmd.PersistentFlags().BoolVarP(&quietFlag, "quiet", "q", false, "suppress non-data output")
	rootCmd.PersistentFlags().BoolVar(&noColorFlag, "no-color", false, "disable colored output")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "warn", "log level: debug|info|warn|error|none")
	rootCmd.PersistentFlags().BoolVarP(&yesFlag, "yes", "y", false, "skip confirmation prompts")
	rootCmd.PersistentFlags().BoolVar(&dryRunFlag, "dry-run", false, "print the HTTP request without sending")
	rootCmd.PersistentFlags().IntVar(&timeoutSec, "timeout", 30, "request timeout in seconds")
	// Additional admin-only flags are registered in build-tag-gated files;
	// the backing var (adminServer) lives above and stays "" in the public
	// build.
	//
	// --explain is intentionally NOT here. It used to be persistent, so all
	// 172 commands silently accepted it whether or not they implemented it.
	// It's registered locally, per command, via registerExplainFlag — see
	// call sites in api.go, application.go, auth.go, call.go, message.go,
	// number.go, verify.go.
}

// registerExplainFlag adds a local (non-persistent) --explain flag to cmd.
// Call this only on commands whose RunE actually reads explainFlag —
// everything else should reject the flag rather than silently ignore it.
func registerExplainFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&explainFlag, "explain", false, "narrate in plain English before executing")
}

// registerAllFlag adds a local (non-persistent) --all flag to cmd. Call this
// only on list commands whose RunE actually walks every page — --all used to
// be persistent on rootCmd and silently did nothing on the ~172 commands
// (including every list command) that never read it; it was removed rather
// than wired up everywhere at once. Wire it back one list command at a time,
// the same way --explain was scoped down via registerExplainFlag.
func registerAllFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&allFlag, "all", false, "auto-paginate through all pages")
}

// credSource records which source supplied the credentials ("env" or a profile
// name), so a rejected-credentials error can point at the right thing.
var credSource string

// clientForTest is a package-level test hook, mirroring apiClientForTest in
// api.go. When non-nil every command gets this client, so tests can point a
// command at an httptest server without real credentials.
var clientForTest *api.Client

// getClient resolves credentials and returns a configured API client.
func getClient() (*api.Client, string, error) {
	if clientForTest != nil {
		return clientForTest, "test", nil
	}
	p, name, err := config.Resolve(profileFlag)
	if err != nil {
		return nil, "", err
	}
	credSource = name
	c := api.New(p.AuthID, p.AuthToken, time.Duration(timeoutSec)*time.Second)
	c.AdminBaseURL = adminServer
	c.Email = p.Email
	c.Region = p.Region
	c.AomUUID = p.AomUUID
	c.TelemetryEnabled = config.TelemetryEnabled()
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
// credentialHint names the source the rejected credentials actually came from.
// The generic hint used to blame the env vars even when a stored profile was
// used, sending people to check something the CLI never read.
func credentialHint() string {
	switch credSource {
	case "":
		return "No credentials resolved. Run `plivo login`, or set PLIVO_AUTH_ID and PLIVO_AUTH_TOKEN."
	case "env":
		return "PLIVO_AUTH_ID / PLIVO_AUTH_TOKEN were rejected. Re-check them, or run `plivo login`."
	default:
		return fmt.Sprintf("Profile %q was rejected. Run `plivo login --profile %s`, or unset it and use env vars.", credSource, credSource)
	}
}

// nonCredentialAuthHint returns a hint for a 401 that is NOT about the
// credential, or "" to fall back to credentialHint.
//
// Some 401s mean the account could not be resolved server-side rather than
// that the credential was wrong. Telling the user to re-run `plivo login`
// there sends them in a circle: the credential is fine and logging in again
// changes nothing.
func nonCredentialAuthHint(msg string) string {
	if strings.Contains(strings.ToLower(msg), "region resolution") {
		return "Not a credential problem: the account's region could not be resolved server-side. " +
			"Re-running `plivo login` will not change this — quote the request id above to support."
	}
	return ""
}

func handleError(err error) {
	f := output.Resolve(outputFormat, os.Stderr)

	// Convert any error into a *clierr.Error so we render a structured
	// envelope no matter what the source was.
	apiErr, ok := err.(*api.APIError)
	if !ok {
		apiErr = clierr.Wrap(err)
	}
	if apiErr.Code == clierr.CodeAuthInvalid {
		if h := nonCredentialAuthHint(apiErr.Message); h != "" {
			apiErr.Hint = h
		} else {
			apiErr.Hint = credentialHint()
		}
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
