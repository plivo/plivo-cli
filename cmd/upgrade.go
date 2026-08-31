package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/output"
	"github.com/plivo/plivo-cli/internal/release"
	"github.com/plivo/plivo-cli/internal/version"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// upgrade flags
var (
	upgradeCheck   bool
	upgradeForce   bool
	upgradeVersion string
)

// upgradeCmd downloads the matching release binary for the host OS/arch and
// atomically replaces the running binary. The download URL pattern follows
// `make build-all` + the install.sh convention (`plivo_<os>_<arch>[.exe]`).
//
// Self-replacing the running binary is fine on Unix (kernel keeps the
// in-memory copy mapped after the inode is unlinked); on Windows the file
// is locked, so we move-then-rename instead. See atomicReplace.
var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade plivo to the latest release",
	Long: `Upgrade plivo to the latest GitHub release.

Fetches the latest release tag, compares it to the running version, then
downloads the matching binary for your OS/arch and atomically replaces the
current binary in place.

If you installed via Homebrew this command will refuse — use ` + "`brew upgrade plivo`" + ` instead.`,
	Example: `  plivo upgrade                 # install latest
  plivo upgrade --check         # check only, don't install
  plivo upgrade --version v0.2.0
  plivo upgrade --force         # reinstall even if already on latest`,
	RunE: runUpgrade,
}

// upgradeResult is the -o json summary for `plivo upgrade` — one final
// object instead of the stderr progress narration, which is left as-is.
type upgradeResult struct {
	CurrentVersion    string `json:"current_version"`
	LatestVersion     string `json:"latest_version,omitempty"`
	Upgraded          bool   `json:"upgraded"`
	SignatureVerified string `json:"signature_verified"` // verified: <signer> | skipped: <reason> | unsigned | n/a
}

func init() {
	upgradeCmd.Flags().BoolVar(&upgradeCheck, "check", false, "report whether a newer release exists, but don't install")
	upgradeCmd.Flags().BoolVar(&upgradeForce, "force", false, "reinstall even if the running binary is already on the target release")
	upgradeCmd.Flags().StringVar(&upgradeVersion, "version", "", "install a specific release tag instead of latest (e.g. v0.2.0)")
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	jsonOut := effectiveFormat() == output.FormatJSON

	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	exePath, err := resolveExePath()
	if err != nil {
		return fmt.Errorf("locate current binary: %w", err)
	}
	if hint := homebrewInstallHint(exePath); hint != "" {
		return fmt.Errorf("%s", hint)
	}

	// Resolve the target release. Empty --version means latest.
	var rel *release.Release
	if upgradeVersion != "" {
		rel, err = release.FetchByTag(ctx, "", upgradeVersion)
	} else {
		rel, err = release.FetchLatest(ctx, "")
	}
	if err != nil {
		// "No releases yet" is informational for both the default and
		// --check paths — surface it as a friendly message, not exit 1.
		if errors.Is(err, release.ErrNoReleases) {
			fmt.Fprintln(os.Stderr, "No releases published yet — nothing to upgrade to.")
			if jsonOut {
				return output.JSONSuccess(os.Stdout, upgradeResult{
					CurrentVersion:    version.Value,
					Upgraded:          false,
					SignatureVerified: "n/a",
				}, nil)
			}
			return nil
		}
		return err
	}

	target := rel.TagName
	current := version.Value

	// Up-to-date short-circuit (skipped by --force).
	if !upgradeForce && !release.IsNewer(current, target) {
		fmt.Fprintf(os.Stderr, "✓ Already on the latest version (%s)\n", current)
		if jsonOut {
			return output.JSONSuccess(os.Stdout, upgradeResult{
				CurrentVersion:    current,
				LatestVersion:     target,
				Upgraded:          false,
				SignatureVerified: "n/a",
			}, nil)
		}
		return nil
	}

	// --check: report status and exit without touching disk.
	if upgradeCheck {
		fmt.Fprintf(os.Stderr, "A newer version of plivo is available: %s (you have %s)\n", target, current)
		fmt.Fprintf(os.Stderr, "  Run `plivo upgrade` to install\n")
		if jsonOut {
			return output.JSONSuccess(os.Stdout, upgradeResult{
				CurrentVersion:    current,
				LatestVersion:     target,
				Upgraded:          false,
				SignatureVerified: "n/a",
			}, nil)
		}
		return nil
	}

	asset, err := rel.AssetFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "→ Downloading %s (%s, %s)…\n", asset.Name, humanSize(asset.Size), target)
	tmpPath := exePath + ".upgrade.tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return permissionHint(err, exePath)
	}
	n, dlErr := release.DownloadAsset(ctx, asset.BrowserDownloadURL, f)
	cErr := f.Close()
	if dlErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("download failed: %w", dlErr)
	}
	if cErr != nil {
		_ = os.Remove(tmpPath)
		return cErr
	}
	if asset.Size > 0 && n != asset.Size {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("download truncated (%d bytes, expected %d)", n, asset.Size)
	}

	// Integrity: verify against the release-published SHA256SUMS before
	// swapping the binary in. A size match is not an integrity check.
	sigStatus, err := verifyDownload(ctx, rel, asset, tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := atomicReplace(exePath, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return permissionHint(err, exePath)
	}

	// Refresh the passive-nudge cache so the just-installed binary doesn't
	// nag the user that an upgrade is available.
	_ = release.SaveCache(&release.Cache{LatestSeen: target, CheckedAt: time.Now()})

	fmt.Fprintf(os.Stderr, "✓ Upgraded plivo to %s\n", target)
	fmt.Fprintf(os.Stderr, "  Binary: %s\n", exePath)
	if jsonOut {
		return output.JSONSuccess(os.Stdout, upgradeResult{
			CurrentVersion:    current,
			LatestVersion:     target,
			Upgraded:          true,
			SignatureVerified: sigStatus,
		}, nil)
	}
	return nil
}

// verifyDownload fetches the release's SHA256SUMS manifest and verifies the
// file at path against the entry for asset.Name. Returns an error (and installs
// nothing) if the manifest is missing, lacks the asset, or the hash mismatches.
// The string result is a signature-verification status for the -o json
// summary; only meaningful when err is nil.
func verifyDownload(ctx context.Context, rel *release.Release, asset *release.Asset, path string) (string, error) {
	fmt.Fprintln(os.Stderr, "→ Verifying SHA-256…")
	sums, err := rel.AssetByName("SHA256SUMS")
	if err != nil {
		return "", fmt.Errorf("release %s publishes no SHA256SUMS; refusing to install an unverified binary", rel.TagName)
	}
	var buf bytes.Buffer
	if _, err := release.DownloadAsset(ctx, sums.BrowserDownloadURL, &buf); err != nil {
		return "", fmt.Errorf("download SHA256SUMS: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := release.VerifyChecksum(buf.String(), asset.Name, f); err != nil {
		return "", err
	}
	return verifyManifestSignature(ctx, rel, buf.String())
}

// verifyManifestSignature checks the signature over SHA256SUMS when the release
// carries one and cosign is available. The hash check above already guarantees
// integrity; this adds provenance — that the manifest came from us.
//
// A missing signature or missing cosign is not an error: releases predating
// signing have none, and we will not make an upgrade impossible over a tool the
// user never installed. A signature that is PRESENT and fails is always fatal.
func verifyManifestSignature(ctx context.Context, rel *release.Release, sums string) (string, error) {
	sigAsset, sigErr := rel.AssetByName("SHA256SUMS.sig")
	certAsset, certErr := rel.AssetByName("SHA256SUMS.pem")
	if sigErr != nil || certErr != nil {
		return "unsigned", nil
	}
	cosign := release.CosignPath()
	if cosign == "" {
		fmt.Fprintln(os.Stderr, "  signature present but cosign is not installed — skipping provenance check")
		fmt.Fprintln(os.Stderr, "  install it with `brew install cosign` to verify who signed this release")
		return "skipped: cosign not installed", nil
	}

	dir, err := os.MkdirTemp("", "plivo-verify-")
	if err != nil {
		return "skipped: could not stage artifacts", nil // the hash check already passed
	}
	defer func() { _ = os.RemoveAll(dir) }()

	sumsPath := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sumsPath, []byte(sums), 0o600); err != nil {
		return "skipped: could not stage artifacts", nil
	}
	sigPath, err := stageAsset(ctx, dir, "SHA256SUMS.sig", sigAsset)
	if err != nil {
		return "skipped: could not download signature", nil
	}
	certPath, err := stageAsset(ctx, dir, "SHA256SUMS.pem", certAsset)
	if err != nil {
		return "skipped: could not download signature", nil
	}

	fmt.Fprintln(os.Stderr, "→ Verifying signature…")
	res, detail, err := release.VerifySignature(cosign, sumsPath, sigPath, certPath)
	switch res {
	case release.SignatureVerified:
		fmt.Fprintf(os.Stderr, "  ✓ signed by %s\n", detail)
		return "verified: " + detail, nil
	case release.SignatureSkipped:
		fmt.Fprintf(os.Stderr, "  signature check skipped: %s\n", detail)
		return "skipped: " + detail, nil
	default:
		return "failed", fmt.Errorf("%w: %s", err, detail)
	}
}

func stageAsset(ctx context.Context, dir, name string, a *release.Asset) (string, error) {
	var buf bytes.Buffer
	if _, err := release.DownloadAsset(ctx, a.BrowserDownloadURL, &buf); err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	return p, os.WriteFile(p, buf.Bytes(), 0o600)
}

// resolveExePath returns the fully symlink-resolved path of the running
// binary. Falls back to the unresolved path if EvalSymlinks fails — we'd
// rather attempt the replace at the original path than abort.
func resolveExePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p, nil
	}
	return real, nil
}

// homebrewInstallHint returns a non-empty hint when the binary lives
// inside a Homebrew prefix — replacing it would conflict with Homebrew's
// metadata and break `brew upgrade plivo` later. Returns "" otherwise.
func homebrewInstallHint(exePath string) string {
	prefixes := []string{
		"/opt/homebrew/",     // Apple Silicon brew
		"/usr/local/Cellar/", // Intel brew installs
		"/usr/local/Homebrew/",
		"/home/linuxbrew/", // Linux brew
	}
	for _, p := range prefixes {
		if strings.HasPrefix(exePath, p) {
			return "this binary was installed via Homebrew — use `brew upgrade plivo` instead"
		}
	}
	return ""
}

// atomicReplace moves newPath over exePath. On Unix os.Rename atomically
// replaces (the running process keeps its in-memory mmap of the old
// inode). On Windows the running .exe is locked, so we move it aside
// first and rename the new binary into place; the .old shim cleans up on
// the next upgrade.
func atomicReplace(exePath, newPath string) error {
	if runtime.GOOS == "windows" {
		oldPath := exePath + ".old"
		_ = os.Remove(oldPath) // best-effort cleanup from a prior upgrade
		if err := os.Rename(exePath, oldPath); err != nil {
			return err
		}
		if err := os.Rename(newPath, exePath); err != nil {
			_ = os.Rename(oldPath, exePath) // revert
			return err
		}
		return nil
	}
	return os.Rename(newPath, exePath)
}

// permissionHint wraps EACCES with a clearer message pointing the user
// at sudo / a writable install dir. Returns err unchanged otherwise.
func permissionHint(err error, exePath string) error {
	if err == nil || !os.IsPermission(err) {
		return err
	}
	return fmt.Errorf("permission denied writing to %s\n  Rerun with sudo, or reinstall to a writable path: PLIVO_INSTALL_DIR=~/bin curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/main/install.sh | bash",
		filepath.Dir(exePath))
}

// humanSize renders a byte count for the download progress line.
func humanSize(b int64) string {
	const k = 1024
	switch {
	case b < k:
		return fmt.Sprintf("%d B", b)
	case b < k*k:
		return fmt.Sprintf("%.1f KB", float64(b)/k)
	default:
		return fmt.Sprintf("%.1f MB", float64(b)/(k*k))
	}
}

// maybePrintUpdateHint prints a one-line "newer version available" nudge to
// stderr if the cache has a fresher release than the running version. Also
// refreshes the cache synchronously (1.5s budget) when it's stale.
//
// Called from Execute() after a successful command. No-op when:
//   - user invoked `plivo upgrade` (would be redundant)
//   - stderr is not a TTY (so scripts/CI don't get noise on stderr)
//   - PLIVO_NO_UPDATE_CHECK is set (CI escape hatch)
//
// 1.5s is a soft budget: the cache covers 24h so the network hit happens
// at most once a day per machine. Offline / GitHub-down errors are
// swallowed — the nudge is a nice-to-have, not a feature.
func maybePrintUpdateHint(invokedFirstWord string) {
	if invokedFirstWord == "upgrade" {
		return
	}
	if os.Getenv("PLIVO_NO_UPDATE_CHECK") != "" {
		return
	}
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return
	}

	cache, _ := release.LoadCache()
	if !cache.IsFresh() {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		r, err := release.FetchLatest(ctx, "")
		if err != nil {
			return // offline / API hiccup — stay quiet
		}
		cache = &release.Cache{LatestSeen: r.TagName, CheckedAt: time.Now()}
		_ = release.SaveCache(cache)
	}

	if !release.IsNewer(version.Value, cache.LatestSeen) {
		return
	}
	fmt.Fprintf(os.Stderr, "\nA newer version of plivo is available: %s (you have %s)\n",
		cache.LatestSeen, version.Value)
	fmt.Fprintln(os.Stderr, "  Run `plivo upgrade` to install")
}
