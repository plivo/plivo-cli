package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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

// StreamSSE opens an SSE connection to fullURL with the given method/body
// and dispatches each decoded event to onEvent. The connection uses the
// client's normal auth path (Bearer for scoped tokens, Basic otherwise).
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
	if c.IsScopedToken() {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	} else {
		req.SetBasicAuth(c.AuthID, c.AuthToken)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", version.UserAgent())
	c.addCLIHeaders(req)

	// SSE needs an unbounded timeout — clone the client to avoid mutating it.
	streamClient := &http.Client{
		Transport: c.HTTP.Transport,
		Timeout:   0,
	}
	_ = time.Now() // keep time import if reused later

	resp, err := streamClient.Do(req)
	if err != nil {
		return fmt.Errorf("SSE transport: %w", err)
	}
	defer resp.Body.Close()

	checkUpgradeWarn(resp)

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("SSE HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
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
