package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sseServer returns an httptest server that streams the given chunks with
// 1ms delays between them, then closes the response.
func sseServer(t *testing.T, chunks []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStreamSSE_singleEvent(t *testing.T) {
	srv := sseServer(t, []string{
		"event: hello\n",
		"data: world\n",
		"\n",
	})
	c := New("MAabc", "tok", time.Second)

	var got []SSEEvent
	err := c.StreamSSE(context.Background(), "GET", srv.URL+"/stream", nil, func(ev SSEEvent) bool {
		got = append(got, ev)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(got), got)
	}
	if got[0].Event != "hello" || got[0].Data != "world" {
		t.Errorf("event = %+v", got[0])
	}
}

// A non-2xx response must surface as a typed *SSEHTTPError (carrying status +
// body) so callers can classify it by status, not as a transport/network error.
func TestStreamSSE_httpErrorReturnsTypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	t.Cleanup(srv.Close)
	c := New("MAabc", "tok", time.Second)

	err := c.StreamSSE(context.Background(), "GET", srv.URL, nil, func(SSEEvent) bool { return true })
	var httpErr *SSEHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error = %T (%v), want *SSEHTTPError", err, err)
	}
	if httpErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", httpErr.StatusCode)
	}
	if !strings.Contains(string(httpErr.Body), "Not Found") {
		t.Errorf("Body = %q, want it to include the server message", httpErr.Body)
	}
}

func TestStreamSSE_multipleEvents(t *testing.T) {
	srv := sseServer(t, []string{
		"event: first\ndata: a\n\n",
		"event: second\ndata: b\n\n",
		"event: third\ndata: c\n\n",
	})
	c := New("MAabc", "tok", time.Second)

	var got []SSEEvent
	err := c.StreamSSE(context.Background(), "GET", srv.URL+"/stream", nil, func(ev SSEEvent) bool {
		got = append(got, ev)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d events: %+v", len(got), got)
	}
	for i, want := range []string{"first", "second", "third"} {
		if got[i].Event != want {
			t.Errorf("got[%d].Event = %q, want %q", i, got[i].Event, want)
		}
	}
}

func TestStreamSSE_idEventDataFields(t *testing.T) {
	srv := sseServer(t, []string{
		"id: msg-42\n",
		"event: progress\n",
		"data: chunk1\n",
		"\n",
	})
	c := New("MAabc", "tok", time.Second)

	var got SSEEvent
	_ = c.StreamSSE(context.Background(), "GET", srv.URL, nil, func(ev SSEEvent) bool {
		got = ev
		return true
	})
	if got.ID != "msg-42" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.Event != "progress" {
		t.Errorf("Event = %q", got.Event)
	}
	if got.Data != "chunk1" {
		t.Errorf("Data = %q", got.Data)
	}
}

func TestStreamSSE_multiLineData(t *testing.T) {
	srv := sseServer(t, []string{
		"data: line1\n",
		"data: line2\n",
		"data: line3\n",
		"\n",
	})
	c := New("MAabc", "tok", time.Second)

	var got SSEEvent
	_ = c.StreamSSE(context.Background(), "GET", srv.URL, nil, func(ev SSEEvent) bool {
		got = ev
		return true
	})
	want := "line1\nline2\nline3"
	if got.Data != want {
		t.Errorf("multi-line data:\ngot:  %q\nwant: %q", got.Data, want)
	}
}

func TestStreamSSE_ignoresComments(t *testing.T) {
	srv := sseServer(t, []string{
		": this is a keepalive comment\n",
		":another comment\n",
		"data: real\n",
		"\n",
	})
	c := New("MAabc", "tok", time.Second)

	var got []SSEEvent
	_ = c.StreamSSE(context.Background(), "GET", srv.URL, nil, func(ev SSEEvent) bool {
		got = append(got, ev)
		return true
	})
	if len(got) != 1 || got[0].Data != "real" {
		t.Errorf("comments leaked into events: %+v", got)
	}
}

func TestStreamSSE_handlerReturnsFalse_stopsEarly(t *testing.T) {
	srv := sseServer(t, []string{
		"data: one\n\n",
		"data: two\n\n",
		"data: three\n\n",
	})
	c := New("MAabc", "tok", time.Second)

	count := 0
	err := c.StreamSSE(context.Background(), "GET", srv.URL, nil, func(ev SSEEvent) bool {
		count++
		return count < 2 // stop after second event
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("handler called %d times, want exactly 2 (stop on second)", count)
	}
}

func TestStreamSSE_returnsErrorOn4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()
	c := New("MAabc", "wrong-tok", time.Second)

	err := c.StreamSSE(context.Background(), "GET", srv.URL, nil, func(ev SSEEvent) bool {
		t.Error("handler should NOT be called on 401")
		return true
	})
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestStreamSSE_authHeader_basic(t *testing.T) {
	var seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(""))
	}))
	defer srv.Close()
	c := New("MAabc", "regular-tok", time.Second)
	_ = c.StreamSSE(context.Background(), "GET", srv.URL, nil, func(ev SSEEvent) bool { return true })
	if !strings.HasPrefix(seenAuth, "Basic ") {
		t.Errorf("Authorization = %q, want Basic prefix", seenAuth)
	}
}

func TestStreamSSE_authHeader_bearer(t *testing.T) {
	var seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(""))
	}))
	defer srv.Close()
	c := New("MAabc", "stk_scoped", time.Second)
	_ = c.StreamSSE(context.Background(), "GET", srv.URL, nil, func(ev SSEEvent) bool { return true })
	if seenAuth != "Bearer stk_scoped" {
		t.Errorf("Authorization = %q, want Bearer stk_scoped", seenAuth)
	}
}

func TestStreamSSE_marshalError(t *testing.T) {
	c := New("MAabc", "tok", time.Second)
	err := c.StreamSSE(context.Background(), "POST", "http://127.0.0.1:1", map[string]any{"ch": make(chan int)}, func(ev SSEEvent) bool { return true })
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if !strings.Contains(err.Error(), "marshal SSE body") {
		t.Errorf("error should mention marshal: %v", err)
	}
}

func TestStreamSSE_acceptAndCacheControlHeaders(t *testing.T) {
	var accept, cc string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		cc = r.Header.Get("Cache-Control")
	}))
	defer srv.Close()

	c := New("MAabc", "tok", time.Second)
	_ = c.StreamSSE(context.Background(), "GET", srv.URL, nil, func(ev SSEEvent) bool { return true })
	if accept != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", accept)
	}
	if cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
}
