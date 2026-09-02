package docs

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Shaped like the real exports: "# Title" then "Source: url" then body.
const fullFixture = `# Account API
Source: https://plivo.com/docs/account/api/account
Retrieve and update the account.
The auth_id identifies the account.

# Audio Streams
Source: https://plivo.com/docs/voice/api/audio-streams
| ` + "`bidirectional`" + ` | boolean | Whether stream supports two-way audio |
Streaming uses a WebSocket.

# Messaging Overview
Source: https://plivo.com/docs/messaging/overview
Send SMS and MMS. WebSocket is not used here.
`

const indexFixture = `# Plivo Documentation

## Voice
- [Audio Streams](https://plivo.com/docs/voice/api/audio-streams): streaming reference
- [Calls](https://plivo.com/docs/voice/api/calls): call management

## Messaging
- [Messaging Overview](https://plivo.com/docs/messaging/overview): sms and mms
not-a-link line
`

func newTestFetcher(t *testing.T, hits *int32) *Fetcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		if strings.Contains(r.URL.Path, "full") {
			_, _ = w.Write([]byte(fullFixture))
			return
		}
		_, _ = w.Write([]byte(indexFixture))
	}))
	t.Cleanup(srv.Close)
	return &Fetcher{
		HTTP:      srv.Client(),
		CacheDir:  t.TempDir(),
		BaseIndex: srv.URL + "/llms.txt",
		BaseFull:  srv.URL + "/llms-full.txt",
	}
}

func TestIndex_parsesOnlyLinkLines(t *testing.T) {
	pages, err := newTestFetcher(t, nil).Index(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 3 {
		t.Fatalf("expected 3 indexed pages, got %d: %+v", len(pages), pages)
	}
	if pages[0].Title != "Audio Streams" || !strings.HasSuffix(pages[0].Source, "/audio-streams") {
		t.Errorf("first page wrong: %+v", pages[0])
	}
}

func TestPages_splitsOnTitleAndSource(t *testing.T) {
	pages, err := newTestFetcher(t, nil).Pages(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 3 {
		t.Fatalf("expected 3 pages, got %d", len(pages))
	}
	if pages[0].Title != "Account API" {
		t.Errorf("title = %q", pages[0].Title)
	}
	if pages[0].Source != "https://plivo.com/docs/account/api/account" {
		t.Errorf("source = %q", pages[0].Source)
	}
	// The Source line is metadata, not content.
	if strings.Contains(pages[0].Body, "Source:") {
		t.Error("Source line leaked into the body")
	}
	if !strings.Contains(pages[0].Body, "auth_id identifies") {
		t.Errorf("body missing content: %q", pages[0].Body)
	}
}

// A page matches only when it contains every keyword, which is what stops a
// two-word query returning everything that mentions either word.
func TestSearch_requiresAllKeywords(t *testing.T) {
	f := newTestFetcher(t, nil)

	got, err := f.Search([]string{"websocket"}, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 pages mentioning websocket, got %d", len(got))
	}

	got, err = f.Search([]string{"websocket", "bidirectional"}, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Audio Streams" {
		t.Fatalf("expected only Audio Streams, got %+v", got)
	}
}

func TestSearch_matchesFieldNamesAndReportsLine(t *testing.T) {
	got, err := newTestFetcher(t, nil).Search([]string{"bidirectional"}, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(got))
	}
	if got[0].Line == 0 {
		t.Error("no line number reported")
	}
	if !strings.Contains(got[0].Snippet, "bidirectional") {
		t.Errorf("snippet missing the match: %q", got[0].Snippet)
	}
}

func TestSearch_limitCaps(t *testing.T) {
	got, err := newTestFetcher(t, nil).Search([]string{"a"}, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 1 {
		t.Fatalf("limit ignored: %d results", len(got))
	}
}

func TestSearch_noKeywordsIsAnError(t *testing.T) {
	if _, err := newTestFetcher(t, nil).Search(nil, 10, false); err == nil {
		t.Error("expected an error with no keywords")
	}
}

// A specific reference must not lose to a longer page that merely contains it.
func TestGet_prefersExactSourceOverSubstring(t *testing.T) {
	f := newTestFetcher(t, nil)

	p, err := f.Get("https://plivo.com/docs/messaging/overview", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Messaging Overview" {
		t.Errorf("got %q", p.Title)
	}

	p, err = f.Get("voice/api/audio-streams", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Audio Streams" {
		t.Errorf("path fragment resolved to %q", p.Title)
	}

	p, err = f.Get("Account API", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Account API" {
		t.Errorf("title lookup resolved to %q", p.Title)
	}
}

func TestGet_unknownRefSuggestsSearch(t *testing.T) {
	_, err := newTestFetcher(t, nil).Get("nope-not-here", false)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "docs search") {
		t.Errorf("error should point at search, got: %v", err)
	}
}

// The full export is ~4MB, so a second search must not re-download it.
func TestFetch_usesCacheOnSecondCall(t *testing.T) {
	var hits int32
	f := newTestFetcher(t, &hits)

	if _, err := f.Pages(false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Pages(false); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected 1 fetch, got %d — cache not used", got)
	}
}

func TestFetch_refreshBypassesCache(t *testing.T) {
	var hits int32
	f := newTestFetcher(t, &hits)

	if _, err := f.Pages(false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Pages(true); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("--refresh should re-fetch, got %d fetches", got)
	}
}

func TestFetch_staleCacheIsRefetched(t *testing.T) {
	var hits int32
	f := newTestFetcher(t, &hits)

	if _, err := f.Pages(false); err != nil {
		t.Fatal(err)
	}
	// Age the cache past the TTL.
	old := time.Now().Add(-2 * cacheTTL)
	if err := os.Chtimes(filepath.Join(f.CacheDir, "llms-full.txt"), old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Pages(false); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("stale cache should be re-fetched, got %d fetches", got)
	}
}

// Losing the network should not lose the docs you already have.
func TestFetch_fallsBackToStaleCacheWhenOffline(t *testing.T) {
	f := newTestFetcher(t, nil)
	if _, err := f.Pages(false); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * cacheTTL)
	_ = os.Chtimes(filepath.Join(f.CacheDir, "llms-full.txt"), old, old)

	f.BaseFull = "http://127.0.0.1:1/gone" // nothing listening
	pages, err := f.Pages(false)
	if err != nil {
		t.Fatalf("should have served the stale cache: %v", err)
	}
	if len(pages) != 3 {
		t.Errorf("stale cache returned %d pages", len(pages))
	}
}

func TestFetch_httpErrorIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	f := &Fetcher{HTTP: srv.Client(), CacheDir: t.TempDir(), BaseFull: srv.URL, BaseIndex: srv.URL}

	if _, err := f.Pages(false); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("expected the status in the error, got: %v", err)
	}
}
