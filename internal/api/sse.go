package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/version"
)

// SSEEvent represents one decoded server-sent event.
type SSEEvent struct {
	ID    string
	Event string
	Data  string
}

// SSEHandler receives decoded events. Return false to stop streaming early.
type SSEHandler func(SSEEvent) bool

// SSEHTTPError is returned by StreamSSE when the endpoint responds with an HTTP
// error status (>=400), or a redirect, instead of an event stream — distinct
// from a transport failure. Carries the status + body so callers can classify
// it accurately.
type SSEHTTPError struct {
	StatusCode int
	Body       []byte
	gate       *clierr.Error // the sign-in gate's answer, when it was one
}

// SSEFailure classifies what StreamSSE returned as a response to any other
// request is classified: an HTTP refusal by its status, or the sign-in gate's
// answer, and a request it refused to send as that refusal. nil for a transport
// failure.
func SSEFailure(err error) *clierr.Error {
	var h *SSEHTTPError
	var ce *clierr.Error
	switch {
	case errors.As(err, &h) && h.gate != nil:
		return h.gate
	case errors.As(err, &h):
		return clierr.FromHTTP(h.StatusCode, "", h.Body)
	case errors.As(err, &ce):
		return ce
	}
	return nil
}

func (e *SSEHTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, strings.TrimSpace(string(e.Body)))
}

// StreamSSE opens an SSE connection to fullURL with the given method/body
// and dispatches each decoded event to onEvent. The connection uses the
// client's normal auth path (see ApplyAuth).
//
// The underlying http.Client is used with Timeout=0 to allow long streams.
func (c *Client) StreamSSE(ctx context.Context, method, fullURL string, body any, onEvent SSEHandler) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal SSE body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("new SSE request: %w", err)
	}
	auth, err := c.ApplyAuth(req)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", version.UserAgent())
	c.addCLIHeaders(req)

	// SSE needs an unbounded timeout. A copy of the client, so it keeps the
	// rest, such as the refusal to follow a redirect.
	streamClient := *c.HTTP
	streamClient.Timeout = 0

	resp, err := streamClient.Do(req)
	if err != nil {
		return fmt.Errorf("SSE transport: %w", err)
	}
	defer resp.Body.Close()

	checkUpgradeWarn(resp)

	if resp.StatusCode >= 300 { // a redirect too, as none is followed
		b, _ := io.ReadAll(resp.Body)
		return &SSEHTTPError{StatusCode: resp.StatusCode, Body: b, gate: c.GateError(auth, resp, b)}
	}

	// 1 MiB read buffer — Buddy narration frames can be a few KB; default 4 KiB
	// works (ReadString grows as needed) but a larger buffer is more efficient.
	reader := bufio.NewReaderSize(resp.Body, 1<<20)
	var ev SSEEvent
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			if ev.Data != "" || ev.Event != "" {
				onEvent(ev)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("SSE read: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")

		if line == "" {
			if ev.Data != "" || ev.Event != "" {
				if !onEvent(ev) {
					return nil
				}
			}
			ev = SSEEvent{}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // SSE comment / keepalive
		}
		switch {
		case strings.HasPrefix(line, "id:"):
			ev.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "event:"):
			ev.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimPrefix(data, " ")
			if ev.Data != "" {
				ev.Data += "\n"
			}
			ev.Data += data
		}
	}
}
