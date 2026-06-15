package wsproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// echoServer returns an httptest server that accepts a WS connection and
// echoes every incoming message back to the sender.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		for {
			mt, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if err := c.Write(ctx, mt, data); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// sinkServer returns an httptest server that accepts a WS connection,
// reads every message, and appends to the captured slice. Used to verify
// the proxy actually forwarded what it should have.
func sinkServer(t *testing.T) (*httptest.Server, *[][]byte, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	captured := &[][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			mu.Lock()
			*captured = append(*captured, append([]byte(nil), data...))
			mu.Unlock()
		}
	}))
	t.Cleanup(srv.Close)
	return srv, captured, &mu
}

// dialWS opens a client WebSocket to the given httptest server's ws://
// equivalent URL.
func dialWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws://" + strings.TrimPrefix(srv.URL, "http://")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

func TestBridge_forwardsClientToServerAndBack(t *testing.T) {
	// a = client connection to an echo server (acts as the "Plivo" side)
	// b = client connection to a second server that captures what arrives
	// Bridge wires a ↔ b. We write to b; Bridge forwards to a (echo
	// server); the echo returns to a; Bridge forwards back to b.
	echo := echoServer(t)
	sink, captured, mu := sinkServer(t)

	a := dialWS(t, echo)
	defer a.Close(websocket.StatusNormalClosure, "")
	b := dialWS(t, sink)
	defer b.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- Bridge(ctx, a, b) }()

	// Send a few messages through "a" (echo side); each one travels:
	//   we write to echo via a → echo replies on a → Bridge copies to b → sink captures
	for _, payload := range []string{"frame-1", "frame-2", "frame-3"} {
		if err := a.Write(ctx, websocket.MessageBinary, []byte(payload)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	// Allow forwarding round-trip to complete.
	time.Sleep(200 * time.Millisecond)
	cancel()
	_ = <-errCh

	mu.Lock()
	defer mu.Unlock()
	if len(*captured) < 3 {
		t.Fatalf("expected 3 captured frames, got %d: %v", len(*captured), *captured)
	}
	for i, want := range []string{"frame-1", "frame-2", "frame-3"} {
		if string((*captured)[i]) != want {
			t.Errorf("frame %d = %q, want %q", i, (*captured)[i], want)
		}
	}
}

func TestBridge_returnsOnContextCancel(t *testing.T) {
	echo := echoServer(t)
	a := dialWS(t, echo)
	defer a.Close(websocket.StatusNormalClosure, "")
	b := dialWS(t, echo)
	defer b.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Bridge(ctx, a, b) }()

	// Cancel quickly — Bridge should unwind both copy goroutines.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("Bridge did not return after context cancel")
	}
}

func TestBridge_returnsOnPeerClose(t *testing.T) {
	echo := echoServer(t)
	a := dialWS(t, echo)
	b := dialWS(t, echo)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Bridge(ctx, a, b) }()

	// Closing one side should unblock the bridge.
	time.Sleep(50 * time.Millisecond)
	a.Close(websocket.StatusNormalClosure, "test done")

	select {
	case err := <-done:
		// A normal-close on `a` cascades; allow either nil or a close
		// error wrapping. Bridge classifies normal-close codes as clean.
		_ = err
	case <-time.After(2 * time.Second):
		t.Fatal("Bridge did not return after peer close")
	}
	b.Close(websocket.StatusNormalClosure, "")
}
