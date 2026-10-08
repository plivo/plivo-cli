package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/feedback"
	"github.com/plivo/plivo-cli/internal/version"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// feedback flags
var (
	feedbackRating    int    // 1-5; 0 = unset
	feedbackMessage   string // free text; "" = unset
	feedbackNoContext bool   // skip auto-attached context
	feedbackYes       bool   // skip pre-submit preview
	feedbackBug       bool   // send a bug report with the last failure
)

// feedbackCmd handles the explicit-channel feedback submission. Future
// contextual auto-prompts (anniversaries, version-upgrade, milestones)
// will reuse internal/feedback under a different cobra parent; this
// file only owns the user-initiated path.
var feedbackCmd = &cobra.Command{
	Use:   "feedback",
	Short: "Share feedback about the Plivo CLI (rating + optional comment)",
	Long: `Share feedback about the Plivo CLI — a 1-5 rating and an optional comment.

Run interactively to be walked through both prompts. Or pass --rating /
--message for a one-shot submission (handy in scripts). Either field
alone is fine — rate without commenting, or comment without rating.

--bug sends a bug report instead: your comment plus the last command that
failed, which the CLI keeps in ~/.plivo/last-error.json (command path, exit
code, error code, request id, CLI version, OS; never your arguments or the
error text). It prints the exact request, headers and body, and asks
before sending: without a terminal pass --yes, or --dry-run to print it
and send nothing. If sending fails, it prints a prefilled GitHub issue
link instead.

Comments are scrubbed client-side for phone numbers, auth tokens,
emails and similar PII patterns before being sent. The collector
re-runs the same scrub server-side.

Feedback goes to the Plivo CLI feedback collector. PLIVO_FEEDBACK_ENDPOINT
points it at another one; PLIVO_FEEDBACK_TELEMETRY=0 stops sending.`,
	Example: `  plivo feedback                              # interactive
  plivo feedback --rating 4                   # one-shot rating only
  plivo feedback --message "..."              # one-shot comment only
  plivo feedback --rating 2 --message "..."   # one-shot both
  plivo feedback --rating 5 --yes             # skip pre-submit preview
  plivo feedback --bug --dry-run              # show a bug report, send nothing
  plivo feedback --bug --message "..." --yes  # report the last failure`,
	RunE: runFeedback,
}

func init() {
	feedbackCmd.Flags().IntVar(&feedbackRating, "rating", 0,
		"one-shot rating (1-5). Skip the interactive prompt.")
	feedbackCmd.Flags().StringVar(&feedbackMessage, "message", "",
		"one-shot comment. Skip the interactive prompt.")
	feedbackCmd.Flags().BoolVar(&feedbackNoContext, "no-context", false,
		"don't auto-attach CLI version / OS / arch metadata")
	feedbackCmd.Flags().BoolVar(&feedbackYes, "yes", false,
		"skip the pre-submit preview / confirmation (default: confirm in interactive)")
	feedbackCmd.Flags().BoolVar(&feedbackBug, "bug", false,
		"report a bug: your comment plus the last failed command, printed in full before sending")
	rootCmd.AddCommand(feedbackCmd)
}

func runFeedback(cmd *cobra.Command, args []string) error {
	if err := validateFeedbackFlags(); err != nil {
		return err
	}
	if feedbackBug {
		return runBugReport(cmd)
	}

	authID := resolveAuthIDForFeedback()
	event := feedback.NewEvent(authID)
	event.Trigger = feedback.TriggerExplicit
	if feedbackNoContext {
		event.Context = stripContextToMinimum(event.Context)
	}

	// Decide rating + comment per the flag/interactive matrix.
	rating, comment, err := collectRatingAndComment(cmd.InOrStdin(), cmd.OutOrStderr())
	if err != nil {
		return err
	}
	if rating == 0 && strings.TrimSpace(comment) == "" {
		fmt.Fprintln(cmd.OutOrStderr(), "Nothing to submit. Run `plivo feedback` again when you have something to share.")
		return nil
	}
	event.Rating = rating
	event.SetComment(comment)

	if !shouldSkipPreview() {
		submit, err := showPreviewAndConfirm(event, cmd.InOrStdin(), cmd.OutOrStderr())
		if err != nil {
			return err
		}
		if !submit {
			return nil
		}
	}

	baseURL, headers := resolveFeedbackTransport(authID)
	if err := event.Submit(context.Background(), baseURL, headers); err != nil {
		if errors.Is(err, feedback.ErrTelemetryDisabled) {
			fmt.Fprintln(cmd.OutOrStderr(), "Feedback telemetry disabled (PLIVO_FEEDBACK_TELEMETRY=0). Nothing sent.")
			return nil
		}
		if errors.Is(err, feedback.ErrEndpointNotConfigured) {
			fmt.Fprintln(cmd.OutOrStderr(),
				"⚠ Could not resolve a feedback endpoint. Set PLIVO_FEEDBACK_ENDPOINT or open an issue at",
				"https://github.com/plivo/plivo-cli/issues.")
			return nil
		}
		return clierr.NetworkError("submitting feedback", err)
	}

	fmt.Fprintln(cmd.OutOrStderr(), "✓ Submitted. Thanks!")
	fmt.Fprintln(cmd.OutOrStderr(), "  Use `plivo feedback` anytime to share more.")
	return nil
}

// validateFeedbackFlags surfaces nonsensical flag combinations early.
func validateFeedbackFlags() error {
	if feedbackRating != 0 && (feedbackRating < 1 || feedbackRating > 5) {
		return clierr.BadFlag("rating", "must be 1-5")
	}
	if len(feedbackMessage) > feedback.MaxCommentChars {
		return clierr.BadFlag("message",
			fmt.Sprintf("must be ≤%d chars (got %d)", feedback.MaxCommentChars, len(feedbackMessage)))
	}
	return nil
}

// resolveAuthIDForFeedback returns the auth_id of the resolved profile
// (honoring --profile if passed), else "". Never errors — feedback works
// without login (often the user is trying the CLI for the first time and
// bounces off, which is exactly the feedback we most want to capture).
func resolveAuthIDForFeedback() string {
	prof, _, err := config.Resolve(profileFlag)
	if err != nil {
		return ""
	}
	return prof.AuthID
}

// resolveFeedbackTransport derives the hodor base URL feedback should
// hit + the headers (email, region, aom_uuid, os, arch, version, auth-id)
// hodor's handler reads to stitch feedback into the per-user PostHog
// dashboards.
//
// Pre-login users get DefaultBaseURL — feedback works without auth, so
// the public route /v1/accounts/cli/feedback responds regardless.
// Logged-in users get whatever Profile.Env resolves to. Honors --profile
// so `plivo --profile X feedback` reads X's identity instead of active.
//
// This is a separate header set from api.Client.addCLIHeaders (feedback
// doesn't go through the client) — Auth-ID/Email/Region/AOM-UUID are
// gated on config.TelemetryEnabled the same way.
func resolveFeedbackTransport(authID string) (string, map[string]string) {
	// Default: hodor prod (anonymous-but-public route).
	base := strings.TrimSuffix(api.DefaultBaseURL, "/v1/cli/api")
	headers := map[string]string{
		"X-Plivo-CLI-Version": versionValue(),
		"X-Plivo-CLI-OS":      runtimeOS(),
		"X-Plivo-CLI-Arch":    runtimeArch(),
		// hodor's own client-identification headers, same as every other CLI
		// request sends. Feedback goes through hodor too, so without these it
		// is logged as client_type "undefined" like the rest used to be.
		"Client-Type":    api.ClientTypeCLI,
		"Client-Version": versionValue(),
	}
	if !config.TelemetryEnabled() {
		return base, headers
	}
	if authID != "" {
		headers["X-Plivo-CLI-Auth-ID"] = authID
	}
	// Honor --profile (matches what resolveAuthIDForFeedback resolved).
	// Without this, a bare 'config.Resolve("")' would pull the active
	// profile even when the user explicitly asked for a different one.
	prof, _, err := config.Resolve(profileFlag)
	if err == nil && prof.AuthID == authID {
		if prof.Email != "" {
			headers["X-Plivo-CLI-Email"] = prof.Email
		}
		if prof.Region != "" {
			headers["X-Plivo-CLI-Region"] = prof.Region
		}
		if prof.AomUUID != "" {
			headers["X-Plivo-CLI-AOM-UUID"] = prof.AomUUID
		}
	}
	return base, headers
}

// versionValue / runtimeOS / runtimeArch wrap the things addCLIHeaders
// reaches for. Kept as named helpers so the test in feedback_test.go can
// monkey-patch them if it ever needs to assert exact header values.
var (
	versionValue = func() string { return version.Value }
	runtimeOS    = func() string { return runtime.GOOS }
	runtimeArch  = func() string { return runtime.GOARCH }
)

// stripContextToMinimum drops the optional context fields when the user
// passes --no-context. CLI version, OS, arch always stay (they're tiny
// and necessary for any aggregate analysis).
func stripContextToMinimum(ctx feedback.Context) feedback.Context {
	return feedback.Context{
		CLIVersion: ctx.CLIVersion,
		OS:         ctx.OS,
		Arch:       ctx.Arch,
		GoVersion:  ctx.GoVersion,
		IsCI:       ctx.IsCI,
		IsTTY:      ctx.IsTTY,
	}
}

// collectRatingAndComment walks the flag/interactive matrix:
//
//	--rating + --message → both from flags, no prompts
//	--rating only        → rating from flag, optional comment prompt
//	--message only       → optional rating prompt, comment from flag
//	neither              → both prompts (or error if no TTY)
func collectRatingAndComment(in io.Reader, out io.Writer) (int, string, error) {
	rating := feedbackRating
	comment := feedbackMessage

	// One-shot path: at least one flag given AND we're not asked to
	// interactively top-up.
	if feedbackRating != 0 && feedbackMessage != "" {
		return rating, comment, nil
	}

	// Interactive needs a TTY. If neither flag set AND no TTY, error.
	stdinTTY := isTTY(in)
	if !stdinTTY {
		if rating == 0 && comment == "" {
			return 0, "", clierr.BadInput(
				"`plivo feedback` needs a terminal for interactive prompts. " +
					"Pass --rating and/or --message to submit non-interactively.")
		}
		return rating, comment, nil
	}

	reader := bufio.NewReader(in)

	if rating == 0 {
		r, err := promptRating(reader, out)
		if err != nil {
			return 0, "", err
		}
		rating = r
	}
	if comment == "" {
		c, err := promptComment(reader, out, rating)
		if err != nil {
			return rating, "", err
		}
		comment = c
	}
	return rating, comment, nil
}

// promptRating asks for a 1-5 number. Enter = skip = 0.
func promptRating(reader *bufio.Reader, out io.Writer) (int, error) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, " How's plivo CLI? (1-5, Enter to skip rating)")
	fmt.Fprintln(out, "   1 = bad    2 = not great    3 = ok    4 = good    5 = love it")
	fmt.Fprint(out, " > ")
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("read rating: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > 5 {
		fmt.Fprintln(out, " (not a 1-5, skipping the rating)")
		return 0, nil
	}
	return n, nil
}

// promptComment asks for a free-text comment. Multi-line until EOF, a
// line of only Ctrl-D characters, or two blank lines in a row. Returns
// "" if the user submits nothing.
func promptComment(reader *bufio.Reader, out io.Writer, rating int) (string, error) {
	fmt.Fprintln(out, "")
	key := endOfInputKey(runtime.GOOS)
	if rating > 0 && rating < 4 {
		fmt.Fprintf(out, " What's going wrong? (multi-line; press Enter twice or %s to finish)\n", key)
	} else if rating >= 4 {
		fmt.Fprintf(out, " Anything to add? Optional. (multi-line; press Enter twice or %s to finish)\n", key)
	} else {
		fmt.Fprintf(out, " Tell us anything? Optional. (multi-line; press Enter twice or %s to finish)\n", key)
	}
	fmt.Fprint(out, " > ")
	var b strings.Builder
	blankCount := 0
	for {
		line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			b.WriteString(line)
			break
		}
		if err != nil {
			return "", fmt.Errorf("read comment: %w", err)
		}
		// Windows consoles pass Ctrl-D through as a literal 0x04; a line of
		// nothing else is the user reaching for end-of-input.
		if t := strings.TrimRight(line, "\r\n"); t != "" && strings.Trim(t, "\x04") == "" {
			break
		}
		// Two consecutive blanks → finish.
		if strings.TrimSpace(line) == "" {
			blankCount++
			if blankCount >= 2 || b.Len() > 0 && blankCount >= 1 {
				break
			}
		} else {
			blankCount = 0
			b.WriteString(line)
		}
		fmt.Fprint(out, " > ")
	}
	// Drop control characters (the \r of CRLF, stray Ctrl-Ds) but keep
	// newlines and tabs.
	comment := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, b.String())
	return strings.TrimSpace(comment), nil
}

// endOfInputKey names the key that finishes the comment prompt on goos
// (a parameter so tests can cover Windows). Windows consoles send Ctrl-D
// as a plain character, so it needs Enter; Ctrl-Z, their EOF, would leave
// the line ending buffered for the next prompt.
func endOfInputKey(goos string) string {
	if goos == "windows" {
		return "Ctrl-D then Enter"
	}
	return "Ctrl-D"
}

// shouldSkipPreview returns true if --yes was passed OR if we're in
// non-interactive mode (one-shot flags + no TTY → no point asking for
// confirmation, nothing reads the response).
func shouldSkipPreview() bool {
	if feedbackYes {
		return true
	}
	return !isTTY(os.Stdin)
}

// showPreviewAndConfirm prints a summary of what will be sent and asks
// the user to confirm. Y / Enter / 'y' = submit; anything else cancels.
// Cancelling is the user's choice, not a failure, so it returns false
// with no error and the command exits 0 without an error envelope.
func showPreviewAndConfirm(event *feedback.Event, in io.Reader, out io.Writer) (bool, error) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, " About to submit:")
	if event.Rating > 0 {
		fmt.Fprintf(out, "   Rating:   %d/5\n", event.Rating)
	} else {
		fmt.Fprintln(out, "   Rating:   (none)")
	}
	if event.Comment != "" {
		fmt.Fprintf(out, "   Comment:  %s\n", event.Comment)
		if event.RedactionCount > 0 {
			fmt.Fprintf(out, "             (PII redacted: %d match(es))\n", event.RedactionCount)
		}
	} else {
		fmt.Fprintln(out, "   Comment:  (none)")
	}
	if !feedbackNoContext {
		fmt.Fprintf(out, "   Metadata: CLI %s, %s/%s\n", event.Context.CLIVersion, event.Context.OS, event.Context.Arch)
	}
	return confirmSubmit(bufio.NewReader(in), out, "Submit?")
}

// confirmSubmit asks question; Y / Enter / 'y' = submit, anything else
// cancels, which returns false with no error.
func confirmSubmit(reader *bufio.Reader, out io.Writer, question string) (bool, error) {
	fmt.Fprintf(out, " %s [Y/n] ", question)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "" || answer == "y" || answer == "yes" {
		return true, nil
	}
	fmt.Fprintln(out, "Cancelled, nothing sent.")
	return false, nil
}

// recordLastError saves the failure handleError just rendered, for `feedback
// --bug`: the command path and the error's category, never argument values or
// the message, which can echo what the user typed. Best effort, so a failed
// write changes neither the output nor the exit code. A failing `feedback`
// keeps the failure it was trying to report.
func recordLastError(ran *cobra.Command, e *clierr.Error, exitCode int) {
	if ran == nil || ran == feedbackCmd {
		return
	}
	_ = feedback.SaveLastError(feedback.LastError{
		Command:    ran.CommandPath(),
		ExitCode:   exitCode,
		ErrorCode:  string(e.Code),
		RequestID:  e.RequestID,
		CLIVersion: versionValue(),
		OS:         runtimeOS(),
		Arch:       runtimeArch(),
		Timestamp:  time.Now().UTC(),
	})
}

// runBugReport sends the comment plus the last recorded failure. It prints the
// exact request first; then a terminal is asked, anything else needs --yes,
// and --dry-run stops there.
func runBugReport(cmd *cobra.Command) error {
	in, out := cmd.InOrStdin(), cmd.OutOrStderr()
	reader := bufio.NewReader(in)
	last, _ := feedback.LoadLastError()

	comment := feedbackMessage
	if comment == "" && isTTY(in) {
		c, err := promptComment(reader, out, 1) // a low rating asks "What's going wrong?"
		if err != nil {
			return err
		}
		comment = c
	}
	if last == nil && strings.TrimSpace(comment) == "" {
		fmt.Fprintln(out, "Nothing to report: no failed command is recorded. Describe the bug with --message.")
		return nil
	}

	authID := resolveAuthIDForFeedback()
	event := feedback.NewEvent(authID)
	event.Trigger = feedback.TriggerBugReport
	event.Rating = feedbackRating
	if feedbackNoContext {
		event.Context = stripContextToMinimum(event.Context)
	}
	event.SetComment(comment)
	scrubbed := event.Comment
	// The collector keeps only the comment, so the failure travels in it. It
	// goes in after the scrub, which would read the request id as a token;
	// every field is the CLI's own, none typed by the user.
	if last != nil {
		event.Comment = strings.TrimSpace(scrubbed + "\n\n" + lastErrorText(last))
	}

	baseURL, headers := resolveFeedbackTransport(authID)
	endpoint, err := feedback.Endpoint(baseURL)
	if err != nil {
		return clierr.Wrap(err)
	}
	body, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return clierr.Wrap(err)
	}
	printBugReport(out, endpoint, headers, body)

	if dryRunFlag {
		fmt.Fprintln(out, "Dry run: nothing sent.")
		return nil
	}
	if !feedbackYes {
		if !isTTY(in) {
			e := clierr.DestructiveRefused("send a bug report")
			e.Hint = "Check the report above, then pass --yes to send it. --dry-run prints it without sending."
			return e
		}
		if send, err := confirmSubmit(reader, out, "Send this report?"); err != nil || !send {
			return err
		}
	}

	if err := event.Submit(context.Background(), baseURL, headers); err != nil {
		if errors.Is(err, feedback.ErrTelemetryDisabled) {
			fmt.Fprintln(out, "Feedback sending is off (PLIVO_FEEDBACK_TELEMETRY=0), so nothing was sent. To file it on GitHub:")
			fmt.Fprintln(out, "  "+bugIssueURL(scrubbed, last))
			return nil
		}
		fmt.Fprintln(out, "Could not send the report. To file it on GitHub instead (no account details included):")
		fmt.Fprintln(out, "  "+bugIssueURL(scrubbed, last))
		return clierr.NetworkError("submitting feedback", err)
	}
	fmt.Fprintln(out, "✓ Bug report sent. Thanks!")
	return nil
}

// printBugReport shows what Submit will send: the endpoint, the CLI's own
// headers (the identity ones only while telemetry is on) and the JSON body.
func printBugReport(out io.Writer, endpoint string, headers map[string]string, body []byte) {
	names := make([]string, 0, len(headers))
	for k, v := range headers {
		if v != "" {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	fmt.Fprintf(out, "Bug report, as it will be sent:\n\nPOST %s\n", endpoint)
	for _, k := range names {
		fmt.Fprintf(out, "%s: %s\n", k, headers[k])
	}
	fmt.Fprintf(out, "\n%s\n\n", body)
	if headers["X-Plivo-CLI-Auth-ID"] != "" || headers["X-Plivo-CLI-Email"] != "" {
		fmt.Fprintln(out, "The Auth-ID, Email, Region and AOM-UUID headers say who sent it, so we can follow up; "+
			"`plivo config telemetry off` leaves them out.")
	}
}

// lastErrorText renders the recorded failure for the report and the issue.
func lastErrorText(e *feedback.LastError) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Last failed command, as the CLI recorded it:\ncommand: %s\nexit code: %d\nerror code: %s\n",
		e.Command, e.ExitCode, e.ErrorCode)
	if e.RequestID != "" {
		fmt.Fprintf(&b, "request id: %s\n", e.RequestID)
	}
	fmt.Fprintf(&b, "CLI: %s (%s/%s)\nat: %s\n", e.CLIVersion, e.OS, e.Arch, e.Timestamp.Format(time.RFC3339))
	return b.String()
}

// maxIssueURLLen keeps the issue link well inside what browsers and GitHub
// accept; GitHub answers 414 past its own limit.
const maxIssueURLLen = 2000

// bugIssueURL returns a prefilled new-issue link on the CLI's repository: the
// scrubbed comment and the recorded failure, nothing that identifies the user.
// The repository takes issues only from a template, so the link names one, and
// the comment is cut to keep the link under maxIssueURLLen.
func bugIssueURL(comment string, last *feedback.LastError) string {
	title, failure := "Bug report from the CLI", ""
	if last != nil {
		title = fmt.Sprintf("Bug: %s failed (%s)", last.Command, last.ErrorCode)
		failure = "\n\n```\n" + lastErrorText(last) + "```\n"
	}
	build := func(c string) string {
		q := url.Values{}
		q.Set("template", "bug_report.md")
		q.Set("title", title)
		q.Set("body", "**What happened**\n"+c+failure)
		return "https://github.com/plivo/plivo-cli/issues/new?" + q.Encode()
	}
	if link := build(comment); len(link) <= maxIssueURLLen {
		return link
	}
	r := []rune(comment)
	n := sort.Search(len(r)+1, func(i int) bool { return len(build(string(r[:i])+"…")) > maxIssueURLLen }) - 1
	return build(string(r[:max(n, 0)]) + "…")
}

// isTTY returns true if r is a *os.File on a terminal. Defensive: any
// non-*os.File reader (test buffers, pipes) is treated as not-a-TTY so
// tests don't surprise-prompt.
func isTTY(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
