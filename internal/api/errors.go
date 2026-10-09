package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// APIError is the legacy alias kept so existing command code compiles. New
// callers should use *clierr.Error directly.
type APIError = clierr.Error

// parseError classifies an upstream HTTP response into a *clierr.Error so
// downstream callers (output renderers, the global error handler) get a
// stable Code, a human Hint, and a Retryable flag — useful for both AI
// agents and humans. Retry-After is kept on the error for callers that retry.
func parseError(status int, header http.Header, body []byte) *APIError {
	e := clierr.FromHTTP(status, header.Get("X-Request-ID"), body)
	e.RetryAfter = retryAfter(header.Get("Retry-After"), time.Now())
	return e
}

// maxRetryAfter bounds a Retry-After so a garbage value cannot overflow.
const maxRetryAfter = 24 * time.Hour

// retryAfter reads a Retry-After header, which is either a number of seconds
// or an HTTP date. Anything else, or a time already past, is 0.
func retryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(min(secs, int64(maxRetryAfter/time.Second))) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return min(max(t.Sub(now), 0), maxRetryAfter)
	}
	return 0
}
