package cmd

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const pasteState = "paste-test-state"

type pasteRun struct {
	code      string
	err       error
	out       string
	stdinRead bool
}

// readRecorder notes whether anything read from it.
type readRecorder struct {
	io.Reader
	read atomic.Bool
}

func (r *readRecorder) Read(p []byte) (int, error) {
	r.read.Store(true)
	return r.Reader.Read(p)
}

// awaitWithStdin runs awaitCallback on a fresh loopback listener with stdin
// holding typed; {addr} and {port} in typed become the listener's own.
func awaitWithStdin(t *testing.T, typed string, tty bool, timeout time.Duration) pasteRun {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	stdin := &readRecorder{Reader: strings.NewReader(
		strings.NewReplacer("{addr}", ln.Addr().String(), "{port}", port).Replace(typed))}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var out bytes.Buffer
	code, err := awaitCallback(ctx, ln, pasteState, stdin, tty, &out)
	return pasteRun{code: code, err: err, out: out.String(), stdinRead: stdin.read.Load()}
}

// A browser on another machine or network namespace can't reach the
// listener, so the callback URL pasted into the terminal must finish the
// wait with the same outcome the redirect would have had.
func TestAwaitCallback_pastedCallbackEndsTheWait(t *testing.T) {
	cases := []struct {
		name     string
		typed    string
		wantCode string
		wantErr  string
	}{
		{"address bar URL", "http://{addr}/?state=" + pasteState + "&code=pasted-code\n", "pasted-code", ""},
		{"padded, CRLF line end", "  http://{addr}/?code=pasted-code&state=" + pasteState + " \r\n", "pasted-code", ""},
		{"declined in the browser", "http://{addr}/?state=" + pasteState + "&error=access_denied\n", "", "cancelled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := awaitWithStdin(t, tc.typed, true, 3*time.Second)
			if run.code != tc.wantCode {
				t.Errorf("code = %q, want %q (err: %v)", run.code, tc.wantCode, run.err)
			}
			switch {
			case tc.wantErr == "" && run.err != nil:
				t.Errorf("unexpected error: %v", run.err)
			case tc.wantErr != "" && (run.err == nil || !strings.Contains(run.err.Error(), tc.wantErr)):
				t.Errorf("err = %v, want it to mention %q", run.err, tc.wantErr)
			}
		})
	}
}

// Anything that isn't this login's callback URL must be refused with a
// message naming the URL to paste, and must not end the wait: the valid
// paste on the next line still completes the login.
func TestAwaitCallback_badPasteIsRefusedAndWaitingContinues(t *testing.T) {
	cases := []struct {
		name  string
		typed string
	}{
		{"state from another login", "http://{addr}/?code=evil-code&state=someone-elses"},
		{"another port", "http://127.0.0.1:1/?code=evil-code&state=" + pasteState},
		{"another host", "http://example.com:{port}/?code=evil-code&state=" + pasteState},
		{"another path", "http://{addr}/callback?code=evil-code&state=" + pasteState},
		{"no code", "http://{addr}/?state=" + pasteState},
		{"bare code", "evil-code"},
		{"garbage", "paste me %zz"},
	}
	good := "http://{addr}/?code=good-code&state=" + pasteState + "\n"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := awaitWithStdin(t, tc.typed+"\n"+good, true, 3*time.Second)
			if run.err != nil || run.code != "good-code" {
				t.Fatalf("got (%q, %v), want the later valid paste's code", run.code, run.err)
			}
			if !strings.Contains(run.out, "http://127.0.0.1:") {
				t.Errorf("refusal should name the callback URL to paste, printed: %q", run.out)
			}
		})
	}
}

// With the paste path armed, a browser that can reach the listener must
// still finish the login at once, not wait on the idle terminal.
func TestAwaitCallback_browserCallbackWinsWhilePasteIsPending(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stdin, typing := io.Pipe()
	t.Cleanup(func() { _ = typing.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		code, err := awaitCallback(ctx, ln, pasteState, stdin, true, io.Discard)
		done <- result{code, err}
	}()

	time.Sleep(60 * time.Millisecond)
	resp, err := http.Get("http://" + ln.Addr().String() + "/?code=browser-code&state=" + pasteState)
	if err != nil {
		t.Fatalf("browser callback: %v", err)
	}
	_ = resp.Body.Close()

	select {
	case r := <-done:
		if r.err != nil || r.code != "browser-code" {
			t.Errorf("got (%q, %v), want the browser's code", r.code, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("still waiting on the terminal after the browser callback arrived")
	}
}

// Piped stdin (scripts, CI, `curl | bash`) must behave exactly as before the
// paste path existed: never read, nothing printed, same timeout message.
func TestAwaitCallback_nonTerminalStdinIsNeverRead(t *testing.T) {
	run := awaitWithStdin(t, "http://{addr}/?code=piped-code&state="+pasteState+"\n", false, 200*time.Millisecond)
	if run.stdinRead {
		t.Error("read stdin although it is not a terminal")
	}
	if run.code != "" || run.err == nil || !strings.Contains(run.err.Error(), "timed out") {
		t.Fatalf("got (%q, %v), want the browser-only timeout", run.code, run.err)
	}
	if strings.Contains(run.err.Error(), "paste") {
		t.Errorf("timeout suggests pasting without a terminal: %v", run.err)
	}
	if run.out != "" {
		t.Errorf("printed %q", run.out)
	}
}

// On a terminal, the timeout should point at the paste fallback so the
// next attempt doesn't hit the same wall.
func TestAwaitCallback_terminalTimeoutMentionsPasting(t *testing.T) {
	run := awaitWithStdin(t, "", true, 200*time.Millisecond)
	if run.err == nil || !strings.Contains(run.err.Error(), "timed out") {
		t.Fatalf("want a timeout, got (%q, %v)", run.code, run.err)
	}
	if !strings.Contains(run.err.Error(), "paste") {
		t.Errorf("timeout should mention pasting the URL: %v", run.err)
	}
}
