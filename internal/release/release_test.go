package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mockServer returns an httptest server that emulates the GitHub releases
// API for the plivo/plivo-cli repo.
func mockServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchLatest_happyPath(t *testing.T) {
	body := `{
	"tag_name": "v0.2.0",
	"prerelease": false,
	"html_url": "https://github.com/plivo/plivo-cli/releases/tag/v0.2.0",
	"assets": [
		{"name": "plivo_darwin_arm64", "browser_download_url": "https://example.com/d_arm64", "size": 8000000},
		{"name": "plivo_linux_amd64",  "browser_download_url": "https://example.com/l_amd64", "size": 8200000}
	]
}`
	srv := mockServer(t, body, 200)
	rel, err := FetchLatest(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("FetchLatest: %v", err)
	}
	if rel.TagName != "v0.2.0" {
		t.Errorf("tag = %q, want v0.2.0", rel.TagName)
	}
	if len(rel.Assets) != 2 {
		t.Errorf("len(assets) = %d, want 2", len(rel.Assets))
	}
}

func TestFetchLatest_noReleasesYet(t *testing.T) {
	srv := mockServer(t, `{"message":"Not Found"}`, 404)
	_, err := FetchLatest(context.Background(), srv.URL)
	if !errors.Is(err, ErrNoReleases) {
		t.Fatalf("err = %v, want ErrNoReleases", err)
	}
}

func TestFetchByTag_addsVPrefix(t *testing.T) {
	gotPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"tag_name":"v0.1.0"}`))
	}))
	t.Cleanup(srv.Close)
	if _, err := FetchByTag(context.Background(), srv.URL, "0.1.0"); err != nil {
		t.Fatalf("FetchByTag: %v", err)
	}
	wantSuffix := "/releases/tags/v0.1.0"
	if !endsWith(gotPath, wantSuffix) {
		t.Errorf("path = %q, want suffix %q", gotPath, wantSuffix)
	}
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func TestAssetFor(t *testing.T) {
	r := &Release{
		TagName: "v0.2.0",
		Assets: []Asset{
			{Name: "plivo_darwin_arm64", Size: 100},
			{Name: "plivo_linux_amd64", Size: 200},
			{Name: "plivo_windows_amd64.exe", Size: 300},
		},
	}
	cases := []struct {
		goos, goarch string
		wantName     string
		wantErr      bool
	}{
		{"darwin", "arm64", "plivo_darwin_arm64", false},
		{"linux", "amd64", "plivo_linux_amd64", false},
		{"windows", "amd64", "plivo_windows_amd64.exe", false},
		{"darwin", "amd64", "", true}, // not packaged
		{"plan9", "arm64", "", true},  // not packaged
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_%s", tc.goos, tc.goarch), func(t *testing.T) {
			a, err := r.AssetFor(tc.goos, tc.goarch)
			if tc.wantErr {
				if err == nil {
					t.Errorf("err = nil, want err")
				}
				return
			}
			if err != nil {
				t.Fatalf("AssetFor: %v", err)
			}
			if a.Name != tc.wantName {
				t.Errorf("name = %q, want %q", a.Name, tc.wantName)
			}
		})
	}
}

func TestAssetByName(t *testing.T) {
	r := &Release{TagName: "v0.2.0", Assets: []Asset{{Name: "SHA256SUMS"}, {Name: "plivo_linux_amd64"}}}
	if a, err := r.AssetByName("SHA256SUMS"); err != nil || a.Name != "SHA256SUMS" {
		t.Errorf("AssetByName(SHA256SUMS) = %v, %v", a, err)
	}
	if _, err := r.AssetByName("nope"); err == nil {
		t.Error("AssetByName(nope) should error")
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("plivo-cli test binary contents\n")
	sum := sha256.Sum256(data)
	hexsum := hex.EncodeToString(sum[:])
	const asset = "plivo_linux_amd64"
	manifest := hexsum + "  " + asset + "\nffff  plivo_darwin_arm64\n"

	cases := []struct {
		name     string
		manifest string
		assetN   string
		data     []byte
		wantErr  bool
	}{
		{"match", manifest, asset, data, false},
		{"case-insensitive", strings.ToUpper(hexsum) + "  " + asset, asset, data, false},
		{"binary-mode star prefix", hexsum + " *" + asset, asset, data, false},
		{"hash mismatch", manifest, asset, []byte("tampered"), true},
		{"no entry for asset", manifest, "plivo_windows_amd64.exe", data, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyChecksum(tc.manifest, tc.assetN, bytes.NewReader(tc.data))
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected nil, got %v", err)
			}
		})
	}
}

func TestDownloadAsset(t *testing.T) {
	want := bytes.Repeat([]byte("plivo"), 1024) // 5 KiB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(want)
	}))
	t.Cleanup(srv.Close)
	var got bytes.Buffer
	n, err := DownloadAsset(context.Background(), srv.URL, &got)
	if err != nil {
		t.Fatalf("DownloadAsset: %v", err)
	}
	if n != int64(len(want)) {
		t.Errorf("n = %d, want %d", n, len(want))
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Errorf("body mismatch")
	}
}

func TestDownloadAsset_non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", 410)
	}))
	t.Cleanup(srv.Close)
	_, err := DownloadAsset(context.Background(), srv.URL, &bytes.Buffer{})
	if err == nil {
		t.Fatal("err = nil, want non-200 error")
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		cur, rel string
		want     bool
		why      string
	}{
		// dev binary always upgrades to any real release.
		{"0.1.0-dev", "v0.1.0", true, "dev → release"},
		{"0.1.0-dev", "v9.9.9", true, "dev → far newer"},
		{"0.1.0-dev", "v0.0.1", true, "dev → older release still wins (we want non-dev)"},

		// Plain triple compares.
		{"v0.1.0", "v0.1.1", true, "patch bump"},
		{"v0.1.0", "v0.2.0", true, "minor bump"},
		{"v0.1.0", "v1.0.0", true, "major bump"},
		{"v0.2.0", "v0.1.9", false, "older release"},
		{"v1.0.0", "v1.0.0", false, "same version"},

		// Pre-release ordering: rc < release.
		{"v0.2.0-rc.1", "v0.2.0", true, "rc → release"},
		{"v0.2.0", "v0.2.0-rc.1", false, "release > rc"},

		// Malformed inputs degrade gracefully.
		{"v0.1.0", "garbage", false, "bad release tag → don't upgrade"},
		{"weird-thing", "v0.2.0", true, "unparseable current → upgrade"},
	}
	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			got := IsNewer(tc.cur, tc.rel)
			if got != tc.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.cur, tc.rel, got, tc.want)
			}
		})
	}
}

func TestCache_roundTrip(t *testing.T) {
	// Redirect HOME so the cache writes into a temp dir.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if c, _ := LoadCache(); c != nil {
		t.Fatalf("fresh dir should have no cache, got %+v", c)
	}

	now := time.Now().UTC().Truncate(time.Second)
	want := &Cache{LatestSeen: "v0.2.0", CheckedAt: now}
	if err := SaveCache(want); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	got, err := LoadCache()
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if got.LatestSeen != want.LatestSeen {
		t.Errorf("LatestSeen = %q, want %q", got.LatestSeen, want.LatestSeen)
	}
	if !got.CheckedAt.Equal(want.CheckedAt) {
		t.Errorf("CheckedAt = %v, want %v", got.CheckedAt, want.CheckedAt)
	}

	// File should be 0600.
	p, _ := CachePath()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("perm = %o, want 0600", mode)
	}

	// Directory should be 0700.
	dirInfo, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0700 {
		t.Errorf("dir perm = %o, want 0700", mode)
	}
}

func TestCache_isFresh(t *testing.T) {
	cases := []struct {
		age  time.Duration
		want bool
	}{
		{0, true},
		{1 * time.Hour, true},
		{23 * time.Hour, true},
		{25 * time.Hour, false},
		{30 * 24 * time.Hour, false},
	}
	for _, tc := range cases {
		c := &Cache{CheckedAt: time.Now().Add(-tc.age)}
		if got := c.IsFresh(); got != tc.want {
			t.Errorf("age=%v IsFresh()=%v, want %v", tc.age, got, tc.want)
		}
	}
	var nilCache *Cache
	if nilCache.IsFresh() {
		t.Error("nil cache should not be fresh")
	}
}
