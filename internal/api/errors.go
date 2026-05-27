package api

import (
	"github.com/plivo/plivo-cli/internal/clierr"
)

// APIError is the legacy alias kept so existing command code compiles. New
// callers should use *clierr.Error directly.
type APIError = clierr.Error

// parseError classifies an upstream HTTP response into a *clierr.Error so
// downstream callers (output renderers, the global error handler) get a
// stable Code, a human Hint, and a Retryable flag — useful for both AI
// agents and humans.
func parseError(status int, requestID string, body []byte) *APIError {
	return clierr.FromHTTP(status, requestID, body)
}
