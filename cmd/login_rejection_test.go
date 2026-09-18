package cmd

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// callbackResult drives awaitLoopbackCallback against a real loopback listener
// and returns what the CLI concluded.
func callbackResult(t *testing.T, query string) (string, error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type out struct {
		code string
		err  error
	}
	done := make(chan out, 1)
	go func() {
		c, e := awaitLoopbackCallback(ctx, ln, "expected-state")
		done <- out{c, e}
	}()

	// Give the server a moment to start serving on the listener.
	time.Sleep(60 * time.Millisecond)
	resp, err := http.Get("http://" + ln.Addr().String() + "/?" + query)
	if err == nil {
		_ = resp.Body.Close()
	}
	r := <-done
	return r.code, r.err
}

// TestCallbackRejection covers the thread's ask: a login declined in the
// browser has to reach the CLI. Before this, an error-only callback fell
// through to "missing code in callback URL", and with no callback at all the
// user waited the full five minutes for a timeout telling them to go and
// approve what they had just refused.
func TestCallbackRejection(t *testing.T) {
	_, err := callbackResult(t, "state=expected-state&error=access_denied")
	if err == nil {
		t.Fatal("a declined login returned no error")
	}
	msg := err.Error()
	if !strings.Contains(strings.ToLower(msg), "cancelled") {
		t.Errorf("message should say it was cancelled, got: %s", msg)
	}
	if strings.Contains(strings.ToLower(msg), "missing code") {
		t.Errorf("still reporting the old generic error: %s", msg)
	}
	// It must not tell someone who just declined to go and approve it.
	if strings.Contains(strings.ToLower(msg), "approving access in the browser") {
		t.Errorf("tells the user to approve the thing they declined: %s", msg)
	}
}

// An unrecognised error code is surfaced rather than guessed at.
func TestCallbackUnknownError(t *testing.T) {
	_, err := callbackResult(t, "state=expected-state&error=server_error")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "server_error") {
		t.Errorf("should quote the code it got, said: %s", err)
	}
}

// The success path must be untouched.
func TestCallbackStillAcceptsACode(t *testing.T) {
	code, err := callbackResult(t, "state=expected-state&code=the-code")
	if err != nil {
		t.Fatalf("valid callback failed: %v", err)
	}
	if code != "the-code" {
		t.Errorf("code = %q, want the-code", code)
	}
}

// A state mismatch still wins over the error parameter: a wrong state means we
// cannot trust anything else in the callback, including the rejection.
func TestCallbackStateMismatchBeatsError(t *testing.T) {
	_, err := callbackResult(t, "state=someone-elses&error=access_denied")
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Errorf("want a state mismatch error, got: %v", err)
	}
}
