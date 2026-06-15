// Package wsproxy bridges two WebSocket connections, copying message frames
// in both directions until either end closes or context cancels. Used by
// `plivo voice streams forward` to wire Plivo's audio stream to the
// customer's local handler.
//
// The proxy is dumb on purpose: it doesn't parse frame bodies (Plivo's
// audio format), doesn't transcode, doesn't buffer. Each upstream message
// becomes one downstream message and vice versa, byte-for-byte.
package wsproxy

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/coder/websocket"
)

// Bridge copies messages between a and b in both directions. Returns when
// either side closes, ctx is cancelled, or a copy errors. The returned
// error is the first non-nil error seen (often a normal close from one
// side); callers can treat io.EOF / normal-close codes as success.
//
// Both connections MUST be closed by the caller — Bridge doesn't take
// ownership.
func Bridge(ctx context.Context, a, b *websocket.Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	errCh := make(chan error, 2)

	go func() {
		defer wg.Done()
		errCh <- copyOne(ctx, "a→b", a, b)
		cancel() // first side closing cancels the other
	}()
	go func() {
		defer wg.Done()
		errCh <- copyOne(ctx, "b→a", b, a)
		cancel()
	}()

	wg.Wait()
	close(errCh)

	// Return the first non-nil error.
	for err := range errCh {
		if err != nil && !isCleanClose(err) {
			return err
		}
	}
	return nil
}

// copyOne reads messages from src and writes them to dst. Stops on
// context cancel, src close, or read/write error.
func copyOne(ctx context.Context, label string, src, dst *websocket.Conn) error {
	for {
		mt, data, err := src.Read(ctx)
		if err != nil {
			return fmt.Errorf("%s read: %w", label, err)
		}
		if err := dst.Write(ctx, mt, data); err != nil {
			return fmt.Errorf("%s write: %w", label, err)
		}
	}
}

// isCleanClose reports whether err is a normal/expected WebSocket close
// (peer initiated a 1000/1001 close, or our context was cancelled).
func isCleanClose(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		switch ce.Code {
		case websocket.StatusNormalClosure, websocket.StatusGoingAway:
			return true
		}
	}
	return false
}
