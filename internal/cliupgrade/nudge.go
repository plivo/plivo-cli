// Package cliupgrade carries the once-per-session "your CLI is out of date"
// nudge that the server signals via response headers. The flag is set
// from the HTTP layer (api.Client) on every response and consumed after
// rootCmd.Execute() returns. Decoupled from cmd/upgrade.go's GitHub-
// release nudge to keep the print discipline (once, after Execute, never
// from inside a JSON-emitting code path).
package cliupgrade

import "sync"

var (
	mu         sync.Mutex
	warned     bool
	minVersion string
)

// SignalUpgradeRequired is called by the HTTP layer when it sees the
// X-Plivo-CLI-Upgrade-Required: true response header. minVer is the
// server-advertised minimum version (from X-Plivo-CLI-Min-Version);
// may be empty.
func SignalUpgradeRequired(minVer string) {
	mu.Lock()
	defer mu.Unlock()
	warned = true
	if minVer != "" {
		minVersion = minVer
	}
}

// Pending reports whether the server flagged us this session.
// Returns (true, minVersion) if a nudge should fire; minVersion is "" if
// the server didn't include one.
func Pending() (bool, string) {
	mu.Lock()
	defer mu.Unlock()
	return warned, minVersion
}

// Reset is for tests.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	warned = false
	minVersion = ""
}
