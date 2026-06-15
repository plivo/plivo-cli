package cmd

import (
	"fmt"
	"os"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
)

// guardSpend enforces the unified spend / destructive-verb contract for
// every command that mutates server state.
//
// Contract (one behaviour, every spend verb):
//
//	--yes              → proceed; live HTTP call
//	--dry-run          → proceed; client.DryRun=true (prints request, no HTTP)
//	--yes + --dry-run  → proceed; client.DryRun=true (preview path wins)
//	neither            → refuse with DESTRUCTIVE_REFUSED (exit 5)
//
// The previous behaviour (silently downgrading missing-flag invocations to
// dry-run with a stderr banner) was misleading for agents reading exit 0
// from `messaging sms send` and assuming the SMS was actually sent. The
// unified contract makes the un-confirmed path a hard error.
//
// Returns:
//
//	proceed  — caller may continue (caller does NOT need to check err if false)
//	dryRun   — caller should set client.DryRun=true and skip terminal rendering
//	err      — *clierr.Error to bubble up unchanged (nil when proceed=true)
//
// `action` is a short human-readable verb fragment ("send sms",
// "make outbound call", "buy number +14155551234") embedded in the
// envelope's Message + Hint.
func guardSpend(action string) (proceed, dryRun bool, err *clierr.Error) {
	switch {
	case dryRunFlag:
		// Preview path. --yes is optional here; --dry-run wins either way.
		return true, true, nil
	case yesFlag:
		return true, false, nil
	default:
		return false, false, clierr.DestructiveRefused(action)
	}
}

// applyDryRun sets client.DryRun when the caller is in the preview path.
// Tiny helper purely to make spend-verb call sites read as a single line.
func applyDryRun(c *api.Client, dryRun bool) {
	if dryRun {
		c.DryRun = true
	}
}

// explainSpend prints the --explain banner when the user asked for it.
// Wrapper so individual spend verbs don't sprinkle `if explainFlag` checks.
func explainSpend(format string, args ...any) {
	if explainFlag {
		fmt.Fprintf(os.Stderr, format, args...)
		if len(format) == 0 || format[len(format)-1] != '\n' {
			fmt.Fprintln(os.Stderr)
		}
	}
}
