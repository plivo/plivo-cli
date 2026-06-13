// Package release talks to GitHub Releases for self-upgrade and the
// passive "newer version available" nudge.
//
// Three concerns:
//   - FetchLatest / FetchByTag — read release metadata from GitHub
//   - DownloadAsset           — stream a release binary to a writer
//   - IsNewer / Cache         — semver compare + 24h cache so the nudge
//     doesn't hit GitHub on every command
package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultAPIBase is api.github.com. Tests inject httptest URLs instead.
	DefaultAPIBase = "https://api.github.com"
	Owner          = "plivo"
	Repo           = "plivo-cli"

	// CacheTTL is how long the passive-nudge cache is reused before the
	// CLI hits GitHub again. One day matches the cadence at which users
	// reasonably care about new versions.
	CacheTTL = 24 * time.Hour

	// fetchUA identifies the upgrade-checker in GitHub access logs and
	// rate-limit headers separately from the buddy/login surfaces.
	fetchUA = "Plivo-CLI-upgrade"
)

// ErrNoReleases means the repo has no releases yet (HTTP 404 from the
// /releases/latest endpoint). The CLI treats this as "nothing to upgrade
// to" rather than an error.
var ErrNoReleases = errors.New("no releases published yet")

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type Release struct {
	TagName    string  `json:"tag_name"`
	Prerelease bool    `json:"prerelease"`
	HTMLURL    string  `json:"html_url"`
	Assets     []Asset `json:"assets"`
}

// FetchLatest returns the most-recent non-draft release. apiBase is the
// GitHub API base (defaults to DefaultAPIBase if empty); tests override.
func FetchLatest(ctx context.Context, apiBase string) (*Release, error) {
	return fetchRelease(ctx, apiBase, "latest")
}

// FetchByTag returns the release named tag (e.g. "v0.2.0"). The leading
// "v" is added if missing.
func FetchByTag(ctx context.Context, apiBase, tag string) (*Release, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil, fmt.Errorf("empty tag")
	}
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	return fetchRelease(ctx, apiBase, "tags/"+tag)
}

func fetchRelease(ctx context.Context, apiBase, suffix string) (*Release, error) {
	if apiBase == "" {
		apiBase = DefaultAPIBase
	}
	url := fmt.Sprintf("%s/repos/%s/%s/releases/%s", apiBase, Owner, Repo, suffix)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", fetchUA)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNoReleases
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github returned HTTP %d for %s", resp.StatusCode, url)
	}

	var r Release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	return &r, nil
}

// AssetFor returns the asset matching goos/goarch. Asset naming follows
// the `make build-all` convention: `plivo_<os>_<arch>[.exe]`.
func (r *Release) AssetFor(goos, goarch string) (*Asset, error) {
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	return r.AssetByName(fmt.Sprintf("plivo_%s_%s%s", goos, goarch, ext))
}

// AssetByName returns the release asset with the given name (e.g. "SHA256SUMS").
func (r *Release) AssetByName(name string) (*Asset, error) {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i], nil
		}
	}
	return nil, fmt.Errorf("no asset named %q in release %s", name, r.TagName)
}

// DownloadAsset streams the asset at url into w, returning bytes written.
// Caller owns w (must close + fsync).
func DownloadAsset(ctx context.Context, url string, w io.Writer) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", fetchUA)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download HTTP %d for %s", resp.StatusCode, url)
	}
	return io.Copy(w, resp.Body)
}

// VerifyChecksum hashes data with SHA-256 and checks it against the entry for
// assetName in a SHA256SUMS manifest. Errors if the manifest has no line for
// the asset or the digest doesn't match. Comparison is case-insensitive.
func VerifyChecksum(manifest, assetName string, data io.Reader) error {
	expected := sha256ForAsset(manifest, assetName)
	if expected == "" {
		return fmt.Errorf("SHA256SUMS has no entry for %s", assetName)
	}
	h := sha256.New()
	if _, err := io.Copy(h, data); err != nil {
		return err
	}
	if actual := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(actual, expected) {
		return fmt.Errorf("SHA-256 mismatch for %s (expected %s, got %s)", assetName, expected, actual)
	}
	return nil
}

// sha256ForAsset returns the hex digest for name from a SHA256SUMS manifest.
// Lines are "<hash>  <filename>"; the filename may carry a '*' binary-mode prefix.
func sha256ForAsset(manifest, name string) string {
	for _, line := range strings.Split(manifest, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.TrimPrefix(fields[1], "*") == name {
			return fields[0]
		}
	}
	return ""
}

// IsNewer returns true if releaseTag is a newer version than the
// currently-running binary. Inputs may have a leading "v" or not.
//
// A current version containing "-dev" always loses to a release tag (so a
// dev build always considers itself out-of-date). An unparseable release
// tag returns false (don't upgrade towards garbage).
func IsNewer(currentVersion, releaseTag string) bool {
	if strings.Contains(strings.ToLower(currentVersion), "-dev") {
		// Don't upgrade a -dev binary toward an even-older release.
		if rel := normalize(releaseTag); rel != "" {
			return true
		}
		return false
	}
	cur := normalize(currentVersion)
	rel := normalize(releaseTag)
	if rel == "" {
		return false
	}
	if cur == "" {
		return true
	}
	return cmpSemver(rel, cur) > 0
}

// normalize returns X.Y.Z[-suffix] (no "v" prefix), or "" if the input
// can't be parsed as a three-part semver.
func normalize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, "-", 2)
	dots := strings.Split(parts[0], ".")
	if len(dots) < 3 {
		return ""
	}
	for _, p := range dots[:3] {
		if _, err := strconv.Atoi(p); err != nil {
			return ""
		}
	}
	return s
}

// cmpSemver returns -1/0/+1 for a < b / a == b / a > b. Inputs must be
// already-normalized (X.Y.Z[-suffix], no "v" prefix). Pre-release suffixes
// are considered older than the bare triple per semver §11.
func cmpSemver(a, b string) int {
	aBase, aPre := splitPre(a)
	bBase, bPre := splitPre(b)
	aParts := strings.Split(aBase, ".")
	bParts := strings.Split(bBase, ".")
	for i := range 3 {
		ai, _ := strconv.Atoi(aParts[i])
		bi, _ := strconv.Atoi(bParts[i])
		if ai != bi {
			return cmpInt(ai, bi)
		}
	}
	switch {
	case aPre == "" && bPre == "":
		return 0
	case aPre == "":
		return 1
	case bPre == "":
		return -1
	default:
		return strings.Compare(aPre, bPre)
	}
}

func splitPre(s string) (base, pre string) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// Cache is the on-disk record of the most-recent "latest release" check.
// Lives at ~/.plivo/upgrade-check.json so it sits alongside config.toml.
type Cache struct {
	LatestSeen string    `json:"latest_seen"`
	CheckedAt  time.Time `json:"checked_at"`
}

// IsFresh reports whether the cache was written within CacheTTL.
func (c *Cache) IsFresh() bool {
	return c != nil && time.Since(c.CheckedAt) < CacheTTL
}

// CachePath returns ~/.plivo/upgrade-check.json.
func CachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".plivo", "upgrade-check.json"), nil
}

// LoadCache reads the cache, returning (nil, nil) when the file doesn't
// exist (the common case on a fresh install).
func LoadCache() (*Cache, error) {
	p, err := CachePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("decode %s: %w", p, err)
	}
	return &c, nil
}

// SaveCache writes the cache to ~/.plivo/upgrade-check.json (mode 0600).
func SaveCache(c *Cache) error {
	p, err := CachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}
