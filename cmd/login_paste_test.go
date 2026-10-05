package cmd

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const pasteState = "paste-test-state"

type pasteRun struct {
	code string
	err  error
	out  string
}

func listenLoopback(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

// runAwait calls awaitCallback and captures what it printed.
func runAwait(ln net.Listener, paste io.Reader, timeout time.Duration) pasteRun {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var out bytes.Buffer
	code, err := awaitCallback(ctx, ln, pasteState, paste, &out)
	return pasteRun{code: code, err: err, out: out.String()}
}

// awaitPaste runs awaitCallback with typed as the terminal input; {addr} and
// {port} in typed become the listener's own.
func awaitPaste(t *testing.T, typed string, timeout time.Duration) pasteRun {
	t.Helper()
	ln := listenLoopback(t)
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	typed = strings.NewReplacer("{addr}", ln.Addr().String(), "{port}", port).Replace(typed)
	return runAwait(ln, strings.NewReader(typed), timeout)
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
			run := awaitPaste(t, tc.typed, 3*time.Second)
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
			run := awaitPaste(t, tc.typed+"\n"+good, 3*time.Second)
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
	ln := listenLoopback(t)
	idle, typing := io.Pipe()
	t.Cleanup(func() { _ = typing.Close() })

	done := make(chan pasteRun, 1)
	go func() { done <- runAwait(ln, idle, 5*time.Second) }()

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
	ln := listenLoopback(t)
	stdin, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close() })
	piped := "http://" + ln.Addr().String() + "/?state=" + pasteState + "&code=piped-code\n"
	if _, err := io.WriteString(w, piped); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	run := runAwait(ln, pasteSource(stdin), 50*time.Millisecond)
	if run.code != "" || run.err == nil || !strings.Contains(run.err.Error(), "timed out") {
		t.Fatalf("got (%q, %v), want the browser-only timeout", run.code, run.err)
	}
	if strings.Contains(run.err.Error(), "paste") {
		t.Errorf("timeout suggests pasting without a terminal: %v", run.err)
	}
	if run.out != "" {
		t.Errorf("printed %q", run.out)
	}
	if left, _ := io.ReadAll(stdin); string(left) != piped {
		t.Errorf("stdin was read: %q left of %q", left, piped)
	}
}

// On a terminal the user is told about pasting up front, and again by the
// timeout so the next attempt doesn't hit the same wall.
func TestAwaitCallback_terminalIsToldAboutPasting(t *testing.T) {
	run := awaitPaste(t, "", 50*time.Millisecond)
	if !strings.Contains(run.out, "paste") {
		t.Errorf("no paste hint printed: %q", run.out)
	}
	if run.err == nil || !strings.Contains(run.err.Error(), "timed out") || !strings.Contains(run.err.Error(), "paste") {
		t.Errorf("timeout should mention pasting the URL: %v", run.err)
	}
}
