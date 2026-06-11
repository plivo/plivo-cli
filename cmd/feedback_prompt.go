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
// task, distracting), and explicit help/version (the user already wants
// out of the CLI, not into a survey).
var skipPromptCommands = map[string]bool{
	"feedback": true,
	"login":    true,
	"logout":   true,
	"upgrade":  true,
	"help":     true,
	"version":  true,
}

// maybePromptFeedback fires once per PromptInterval (24h) at the end of
// a successful command on an interactive TTY. Called from cmd/root.go
// Execute() AFTER the user's command has rendered its output. Silent
// no-op on non-TTY / CI / skip-listed commands / opted-out users.
func maybePromptFeedback(firstCmd string) {
	if !isInteractiveFeedbackSession() {
		return
	}
	if skipPromptCommands[firstCmd] {
		return
	}
	state, err := feedback.LoadState()
	if err != nil {
		return // state load failed; play it safe and skip
	}
	if !feedback.ShouldPrompt(state, time.Now()) {
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
