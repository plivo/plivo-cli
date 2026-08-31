// Package docs reads the Plivo documentation from the docs site's own
// machine-readable exports, so `plivo docs` needs no API credentials and no
// server-side search.
//
// Two sources, both public:
//
//	llms.txt       ~18KB  section headings + one line per page (the index)
//	llms-full.txt  ~4MB   every page's full text, as "# Title" / "Source: url"
//
// The full export is cached under ~/.plivo/cache so a search does not pull 4MB
// every time.
package docs

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	indexURL = "https://docs.plivo.com/docs/llms.txt"
	fullURL  = "https://docs.plivo.com/docs/llms-full.txt"

	// The docs change far slower than a day, and a stale hit is harmless: the
	// Source URL is always printed so the reader can see the live page.
	cacheTTL = 24 * time.Hour

	// Refuse an implausibly large body rather than filling the disk.
	maxDownload = 64 << 20
)

// Page is one documentation page from the full export.
type Page struct {
	Title  string `json:"title"`
	Source string `json:"source"`
	Body   string `json:"body,omitempty"`
}

// Match is a search hit: the page plus the line that matched.
type Match struct {
	Title   string `json:"title"`
	Source  string `json:"source"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// Fetcher retrieves and caches the docs exports.
type Fetcher struct {
	HTTP     *http.Client
	CacheDir string
	// BaseIndex and BaseFull are overridable for tests.
	BaseIndex string
	BaseFull  string
}

// New returns a Fetcher writing to ~/.plivo/cache.
func New() *Fetcher {
	dir := ""
	if home, err := os.UserHomeDir(); err == nil {
		dir = filepath.Join(home, ".plivo", "cache")
	}
	return &Fetcher{
		HTTP:      &http.Client{Timeout: 60 * time.Second},
		CacheDir:  dir,
		BaseIndex: indexURL,
		BaseFull:  fullURL,
	}
}

func (f *Fetcher) cachePath(name string) string {
	if f.CacheDir == "" {
		return ""
	}
	return filepath.Join(f.CacheDir, name)
}

// fetch returns the document body, from cache when it is fresh.
func (f *Fetcher) fetch(url, cacheName string, refresh bool) (string, error) {
	path := f.cachePath(cacheName)
	if !refresh && path != "" {
		if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < cacheTTL {
			if b, err := os.ReadFile(path); err == nil {
				return string(b), nil
			}
		}
	}

	resp, err := f.HTTP.Get(url)
	if err != nil {
		// A stale cache beats no docs at all when the network is down.
		if b, rerr := os.ReadFile(path); path != "" && rerr == nil {
			return string(b), nil
		}
		return "", fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching %s: HTTP %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", url, err)
	}

	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
			// Write via a temp file so a killed download cannot leave a
			// truncated cache that later reads treat as complete.
			tmp := path + ".tmp"
			if werr := os.WriteFile(tmp, b, 0o600); werr == nil {
				_ = os.Rename(tmp, path)
			}
		}
	}
	return string(b), nil
}

var indexLine = regexp.MustCompile(`^- \[([^\]]+)\]\((https?://[^)]+)\)`)

// Index returns every page listed in llms.txt, in document order.
func (f *Fetcher) Index(refresh bool) ([]Page, error) {
	body, err := f.fetch(f.BaseIndex, "llms.txt", refresh)
	if err != nil {
		return nil, err
	}
	var out []Page
	for _, line := range strings.Split(body, "\n") {
		if m := indexLine.FindStringSubmatch(line); m != nil {
			out = append(out, Page{Title: m[1], Source: m[2]})
		}
	}
	return out, nil
}

// Pages returns every page from the full export, bodies included.
func (f *Fetcher) Pages(refresh bool) ([]Page, error) {
	body, err := f.fetch(f.BaseFull, "llms-full.txt", refresh)
	if err != nil {
		return nil, err
	}
	return parsePages(body), nil
}

// parsePages splits the full export on its "# Title" / "Source: url" headers.
func parsePages(body string) []Page {
	var out []Page
	var cur *Page
	var sb strings.Builder
	flush := func() {
		if cur != nil {
			cur.Body = strings.TrimSpace(sb.String())
			out = append(out, *cur)
			sb.Reset()
		}
	}
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "# "):
			flush()
			cur = &Page{Title: strings.TrimSpace(line[2:])}
		case cur != nil && cur.Source == "" && strings.HasPrefix(line, "Source: "):
			cur.Source = strings.TrimSpace(line[len("Source: "):])
		case cur != nil:
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	flush()
	return out
}

// Search returns pages whose text contains every keyword, ranked by how often
// the keywords appear. Case-insensitive, substring match, so partial words and
// API field names both hit.
func (f *Fetcher) Search(keywords []string, limit int, refresh bool) ([]Match, error) {
	pages, err := f.Pages(refresh)
	if err != nil {
		return nil, err
	}
	if len(keywords) == 0 {
		return nil, fmt.Errorf("pass at least one keyword")
	}
	lower := make([]string, len(keywords))
	for i, k := range keywords {
		lower[i] = strings.ToLower(k)
	}

	type scored struct {
		Match
		score int
	}
	var hits []scored
	for _, p := range pages {
		hay := strings.ToLower(p.Title + "\n" + p.Body)
		score := 0
		ok := true
		for _, k := range lower {
			n := strings.Count(hay, k)
			if n == 0 {
				ok = false
				break
			}
			score += n
		}
		if !ok {
			continue
		}
		line, snippet := firstHit(p.Body, lower[0])
		hits = append(hits, scored{
			Match: Match{Title: p.Title, Source: p.Source, Line: line, Snippet: snippet},
			score: score,
		})
	}
	// Highest score first; ties keep document order so results are stable.
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].score > hits[j-1].score; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Match, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Match)
	}
	return out, nil
}

// firstHit returns the 1-based line number and trimmed text of the first line
// containing needle.
func firstHit(body, needle string) (int, string) {
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(strings.ToLower(line), needle) {
			s := strings.TrimSpace(line)
			if len(s) > 240 {
				s = s[:240] + "…"
			}
			return i + 1, s
		}
	}
	return 0, ""
}

// Get returns the page whose Source URL or title best matches ref. ref may be a
// full URL, a path fragment such as "voice/api/call", or a title.
func (f *Fetcher) Get(ref string, refresh bool) (*Page, error) {
	pages, err := f.Pages(refresh)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.Trim(ref, "/"))
	// Exact source or title first, then a path-fragment match, so a specific
	// ref never loses to a longer page that merely contains it.
	for _, p := range pages {
		if strings.ToLower(p.Source) == needle || strings.ToLower(p.Title) == needle {
			return &p, nil
		}
	}
	for _, p := range pages {
		if strings.Contains(strings.ToLower(p.Source), needle) {
			return &p, nil
		}
	}
	for _, p := range pages {
		if strings.Contains(strings.ToLower(p.Title), needle) {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("no doc matching %q — try `plivo docs search %s`", ref, ref)
}
