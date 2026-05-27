//go:build internal

// Package contacto provides a thin HTTP client for Contacto-authenticated
// requests: agent CRUD via PHLO config service, vibe-agent SSE via aiassist.
//
// All requests go through the regional Contacto auth-api gateway with headers
// matching what the Contacto Console web UI sends.
package contacto

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
	"time"

	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/version"
)

type Client struct {
	Profile *config.ContactoProfile
	HTTP    *http.Client
}

func New(profile *config.ContactoProfile) *Client {
	return &Client{
		Profile: profile,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// Response is a lightly-parsed response: the raw bytes and status are exposed
// so callers can JSON-decode or render as needed.
type Response struct {
	Status int
	Body   []byte
	Header http.Header
}

// Do performs a Contacto-authenticated request. The path is appended to the
// regional gateway URL (e.g. /v1/contacto-core/contacto-config/phlo).
func (c *Client) Do(ctx context.Context, method, path string, body any) (*Response, error) {
	base := c.Profile.RegionalGatewayURL()
	if base == "" {
		return nil, errors.New("Contacto profile has no region; re-run `plivo contacto login`")
	}
	urlStr := base + path

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	c.applyHeaders(req, body != nil, false)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &Response{Status: resp.StatusCode, Body: raw, Header: resp.Header}, nil
}

// DecodeJSON unmarshals r.Body into out. Returns an error if status is not 2xx,
// surfacing the response body as the error message when possible.
func (c *Client) DecodeJSON(r *Response, out any) error {
	if r.Status < 200 || r.Status >= 300 {
		return fmt.Errorf("HTTP %d: %s", r.Status, strings.TrimSpace(string(r.Body)))
	}
	if len(r.Body) == 0 || out == nil {
		return nil
	}
	if err := json.Unmarshal(r.Body, out); err != nil {
		return fmt.Errorf("decode JSON: %w (body: %s)", err, string(r.Body))
	}
	return nil
}

// SSEEvent represents a decoded server-sent event.
type SSEEvent struct {
	ID    string
	Event string
	Data  string
}

// SSE opens a streaming POST to path and dispatches decoded events to onEvent.
// Returning false from onEvent ends the stream early. The HTTP client used has
// no timeout so long streams are supported.
func (c *Client) SSE(ctx context.Context, path string, body any, onEvent func(SSEEvent) bool) error {
	base := c.Profile.RegionalGatewayURL()
	if base == "" {
		return errors.New("Contacto profile has no region")
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", base+path, bodyReader)
	if err != nil {
		return err
	}
	c.applyHeaders(req, body != nil, true)

	streamClient := &http.Client{Timeout: 0}
	resp, err := streamClient.Do(req)
	if err != nil {
		return fmt.Errorf("SSE http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("SSE HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	reader := bufio.NewReader(resp.Body)
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
			continue
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

// applyHeaders sets the standard Contacto-session headers on a request.
// Mirrors contacto-console's createGlobalHeaders().
func (c *Client) applyHeaders(req *http.Request, hasBody, isSSE bool) {
	req.Header.Set("Authorization", "Token "+c.Profile.AuthToken)
	req.Header.Set("Aom_uuid", c.Profile.AomUUID)
	req.Header.Set("Region", c.Profile.Region)
	req.Header.Set("Client-Type", "web_app")
	if c.Profile.BrowserSessionID != "" {
		req.Header.Set("Browser-Session-Id", c.Profile.BrowserSessionID)
	}
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	if isSSE {
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Cache-Control", "no-cache")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("User-Agent", "plivo-cli/"+version.Value)
}
