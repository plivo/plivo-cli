package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/feedback"
	"golang.org/x/term"
)

// skipPromptCommands is the set of cobra commands that should NEVER
// trigger the post-success auto-prompt. feedback (would double-prompt),
// login/logout (user is mid-auth flow, prompt would land between
// successful login and their next real command), upgrade (maintenance
// task, distracting), completion (shell-init scripts pipe this — a
// hanging y/N read would break the user's rc file sourcing), and
// explicit help/version (the user already wants out of the CLI, not
// into a survey).
var skipPromptCommands = map[string]bool{
	"feedback":   true,
	"login":      true,
	"logout":     true,
	"upgrade":    true,
	"completion": true,
	"help":       true,
	"version":    true,
}

// metadataFlags are top-level flags that turn any invocation into a
// pure read-only metadata print — even `plivo voice calls list --help`
// renders help instead of a real command. The post-success prompt has
// no business firing in those cases.
var metadataFlags = map[string]bool{
	"--help":    true,
	"-h":        true,
	"--version": true,
	"-v":        true,
}

// isMetadataInvocation returns true when the argv is purely a help /
// version print, or no subcommand was provided (bare `plivo`, which
// also auto-prints help). Used to gate the auto-prompt out before we
// even consider state.
func isMetadataInvocation(firstCmd string, args []string) bool {
	if firstCmd == "" {
		return true
	}
	for _, a := range args {
		if metadataFlags[a] {
			return true
		}
	}
	return false
}

// maybePromptFeedback fires once per PromptInterval (24h) at the end of
// a successful command on an interactive TTY. Called from cmd/root.go
// Execute() AFTER the user's command has rendered its output. Silent
// no-op on non-TTY / CI / skip-listed commands / metadata-only
// invocations (--help/--version/bare plivo) / opted-out users / users
// who haven't yet hit the first-prompt activity floor.
func maybePromptFeedback(firstCmd string, args []string) {
	if !isInteractiveFeedbackSession() {
		return
	}
	if skipPromptCommands[firstCmd] {
		return
	}
	if isMetadataInvocation(firstCmd, args) {
		return
	}
	state, err := feedback.LoadState()
	if err != nil {
		return // state load failed; play it safe and skip
	}
	now := time.Now()
	// Stamp the user's first real invocation so the 24h-since-first-run
	// escape-hatch in ShouldPrompt has an anchor.
	if state.FirstRunAt.IsZero() {
		state.FirstRunAt = now
	}
	// Count this successful, non-metadata run toward the first-prompt
	// floor. After the first prompt the SuccessCount stops mattering;
	// ShouldPrompt switches to the interval gates.
	state.SuccessCount++
	if !feedback.ShouldPrompt(state, now) {
		_ = feedback.SaveState(state)
		return
	}

	// Hyphen-space inline prompt — the existing collectRatingAndComment
	// flow takes over if the user says yes.
	fmt.Fprintln(os.Stderr)
	fmt.Fprint(os.Stderr, "💡 Got 30s to rate the CLI? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))

	state.LastPromptedAt = time.Now()
	state.PromptCount++
	if answer != "y" && answer != "yes" {
		state.SnoozeCount++
		_ = feedback.SaveState(state)
		return
	}

	if err := runInlineFeedback(); err != nil {
		// Don't blow up the user's terminal — they just finished a real
		// command. Surface quietly + persist state so we don't loop.
		fmt.Fprintf(os.Stderr, "(feedback submission failed: %v)\n", err)
		_ = feedback.SaveState(state)
		return
	}
	state.LastSubmittedAt = time.Now()
	_ = feedback.SaveState(state)
}

// isInteractiveFeedbackSession gates the auto-prompt on a real terminal.
// Non-TTY (piped / scripted) + CI environments never get prompted —
// otherwise a script running `plivo voice calls list | jq …` would hang
// on the y/N read.
func isInteractiveFeedbackSession() bool {
	if os.Getenv("CI") != "" {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
}

// runInlineFeedback drives the rating + optional comment prompts then
// submits. Mirrors the manual `plivo feedback` path but with the
// daily-prompt trigger so analytics distinguishes user-typed feedback
// from prompt-driven feedback.
func runInlineFeedback() error {
	authID := resolveAuthIDForFeedback()
	event := feedback.NewEvent(authID)
	event.Trigger = feedback.TriggerDailyPrompt

	rating, comment, err := collectRatingAndComment(os.Stdin, os.Stderr)
	if err != nil {
		return err
	}
	if rating == 0 && strings.TrimSpace(comment) == "" {
		// User typed nothing — treat as silent decline, not an error.
		return nil
	}
	event.Rating = rating
	event.SetComment(comment)

	baseURL, headers := resolveFeedbackTransport(authID)
	if err := event.Submit(context.Background(), baseURL, headers); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "✓ Thanks!")
	return nil
}
