package cmd

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/api"
)

// Spinner tests cover the TTY "working" animation: lifecycle (start→setText→
// stop) must not panic or deadlock, and every stderr write goes through the
// renderer mutex so the goroutine tick can't race event-driven writes. Run with
// `go test ./cmd/ -race` to exercise the concurrency guarantees.

// syncBuffer is a goroutine-safe io.Writer for capturing spinner stderr output
// without tripping the race detector on the buffer itself.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// runWithDeadline fails the test if fn doesn't return within d (catches a
// deadlocked stopSpinner / leaked goroutine).
func runWithDeadline(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("spinner operation did not return within %s (possible deadlock)", d)
	}
}

func TestBuddySpinner_wordArrayHasEnoughEntries(t *testing.T) {
	if len(spinnerWords) < 15 {
		t.Fatalf("spinnerWords has %d entries, want >= 15", len(spinnerWords))
	}
	if len(spinnerFrames) == 0 {
		t.Fatal("spinnerFrames is empty")
	}
}

func TestBuddySpinner_startSetTextStop_noPanicNoDeadlock(t *testing.T) {
	var errBuf syncBuffer
	r := &buddyRenderer{
		out: &bytes.Buffer{}, err: &errBuf,
		useANSI:   true, // TTY path: spinner active
		startedAt: time.Now(),
	}

	runWithDeadline(t, 5*time.Second, func() {
		r.startSpinner()
		// Let the goroutine tick a few times so it actually writes frames.
		time.Sleep(3 * spinnerInterval)
		r.setSpinnerText("checking your account…")
		time.Sleep(2 * spinnerInterval)
		r.stopSpinner()
	})

	// The line must be cleared on stop (ends with the clear escape).
	out := errBuf.String()
	if !strings.Contains(out, "\033[2K") {
		t.Errorf("expected spinner to write/clear the stderr line, got:\n%q", out)
	}
}

func TestBuddySpinner_stopIsIdempotentAndSafeWhenNotStarted(t *testing.T) {
	var errBuf syncBuffer
	r := &buddyRenderer{
		out: &bytes.Buffer{}, err: &errBuf,
		useANSI:   true,
		startedAt: time.Now(),
	}
	runWithDeadline(t, 5*time.Second, func() {
		// stop with no spinner running must be a safe no-op.
		r.stopSpinner()
		r.startSpinner()
		r.stopSpinner()
		r.stopSpinner() // double-stop must not panic or block
	})
}

// TestBuddySpinner_concurrentEventsAndTicks drives event-driven writes against a
// live spinner goroutine. With -race this proves the mutex serialises the tick
// and the event writes.
func TestBuddySpinner_concurrentEventsAndTicks(t *testing.T) {
	var errBuf syncBuffer
	r := &buddyRenderer{
		out: &bytes.Buffer{}, err: &errBuf,
		useANSI: true, verbose: true,
		startedAt: time.Now(),
	}
	runWithDeadline(t, 5*time.Second, func() {
		r.handle(api.SSEEvent{Event: "start", Data: `{"id":"x"}`})
		for i := 0; i < 5; i++ {
			r.handle(api.SSEEvent{Event: "narration", Data: `{"text":"step"}`})
			r.handle(api.SSEEvent{Event: "tool_call", Data: `{"name":"fetch_logs"}`})
			r.handle(api.SSEEvent{Event: "tool_output", Data: `{"name":"fetch_logs","success":true}`})
			time.Sleep(spinnerInterval)
		}
		r.handle(api.SSEEvent{Event: "final", Data: `{"answer":"done","latency_ms":10}`})
	})
}

// TestBuddySpinner_offTTYAndJSON_noSpinner confirms the spinner never starts off
// a TTY or in json mode (current behavior preserved).
func TestBuddySpinner_offTTYAndJSON_noSpinner(t *testing.T) {
	for _, tc := range []struct {
		name     string
		useANSI  bool
		jsonMode bool
	}{
		{"non-tty", false, false},
		{"json", false, true},
		{"json-on-tty", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &buddyRenderer{
				out: &bytes.Buffer{}, err: &bytes.Buffer{},
				useANSI: tc.useANSI, jsonMode: tc.jsonMode,
				startedAt: time.Now(),
			}
			r.startSpinner()
			r.setSpinnerText("hi")
			if r.sp != nil {
				t.Errorf("%s: spinner should not start, but r.sp is non-nil", tc.name)
			}
			r.stopSpinner() // still safe
		})
	}
}
