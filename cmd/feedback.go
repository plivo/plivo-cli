package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/feedback"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// feedback flags
var (
	feedbackRating    int    // 1-5; 0 = unset
	feedbackMessage   string // free text; "" = unset
	feedbackNoContext bool   // skip auto-attached context
	feedbackYes       bool   // skip pre-submit preview
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

Comments are scrubbed client-side for phone numbers, auth tokens,
emails and similar PII patterns before being sent. The collector
re-runs the same scrub server-side.

The feedback collector endpoint is configured via the PLIVO_FEEDBACK_ENDPOINT
environment variable. When unset, the command surfaces a clear "not yet
wired" message instead of dropping the submission silently.`,
	Example: `  plivo feedback                              # interactive
  plivo feedback --rating 4                   # one-shot rating only
  plivo feedback --message "..."              # one-shot comment only
  plivo feedback --rating 2 --message "..."   # one-shot both
  plivo feedback --rating 5 --yes             # skip pre-submit preview`,
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
	rootCmd.AddCommand(feedbackCmd)
}

func runFeedback(cmd *cobra.Command, args []string) error {
	if err := validateFeedbackFlags(); err != nil {
		return err
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
		if err := showPreviewAndConfirm(event, cmd.InOrStdin(), cmd.OutOrStderr()); err != nil {
			return err
		}
	}

	if err := event.Submit(context.Background()); err != nil {
		if errors.Is(err, feedback.ErrEndpointNotConfigured) {
			fmt.Fprintln(cmd.OutOrStderr(),
				"⚠ Feedback collector endpoint not configured yet (PLIVO_FEEDBACK_ENDPOINT unset).")
			fmt.Fprintln(cmd.OutOrStderr(),
				"  Your feedback was prepared but not sent. The CLI team is wiring this up;")
			fmt.Fprintln(cmd.OutOrStderr(),
				"  for now, please open an issue at https://github.com/plivo/plivo-cli/issues.")
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

// resolveAuthIDForFeedback returns the active profile's auth_id if the
// user is logged in, else "". Never errors — feedback works without
// login (often the user is trying the CLI for the first time and
// bounces off, which is exactly the feedback we most want to capture).
func resolveAuthIDForFeedback() string {
	prof, _, err := config.Resolve("")
	if err != nil {
		return ""
	}
	return prof.AuthID
}

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

// promptComment asks for a free-text comment. Multi-line until Ctrl-D
// (EOF) or two blank lines in a row. Returns "" if the user submits
// nothing.
func promptComment(reader *bufio.Reader, out io.Writer, rating int) (string, error) {
	fmt.Fprintln(out, "")
	if rating > 0 && rating < 4 {
		fmt.Fprintln(out, " What's going wrong? (multi-line; press Enter twice or Ctrl-D to finish)")
	} else if rating >= 4 {
		fmt.Fprintln(out, " Anything to add? Optional. (multi-line; press Enter twice or Ctrl-D to finish)")
	} else {
		fmt.Fprintln(out, " Tell us anything? Optional. (multi-line; press Enter twice or Ctrl-D to finish)")
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
	return strings.TrimSpace(b.String()), nil
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
func showPreviewAndConfirm(event *feedback.Event, in io.Reader, out io.Writer) error {
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
	fmt.Fprint(out, " Submit? [Y/n] ")
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return fmt.Errorf("read confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "" || answer == "y" || answer == "yes" {
		return nil
	}
	return clierr.BadInput("cancelled — nothing sent")
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
