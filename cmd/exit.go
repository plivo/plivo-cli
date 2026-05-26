package cmd

import "github.com/plivo/plivo-cli/internal/clierr"

// Exit code constants — stable per category so AI driver loops and shell
// scripts can branch on them. The actual mapping from Error.Code lives on
// (*clierr.Error).ExitCode() so it stays in one place.
const (
	ExitOK        = 0
	ExitUserError = 1
	ExitAuthError = 2
	ExitAPIError  = 3
	ExitRateLimit = 4
	ExitRefused   = 5
)

func exitCodeForAPI(e *clierr.Error) int {
	if e == nil {
		return ExitOK
	}
	return e.ExitCode()
}
