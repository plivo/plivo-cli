package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/release"
)

// sigServer serves SHA256SUMS.sig and SHA256SUMS.pem, optionally failing one
// of them, so a download error can be reproduced without a network.
func sigServer(t *testing.T, failing string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == failing {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("not-a-real-signature"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func releaseWithSigs(tag, base string, withSigAssets bool) *release.Release {
	r := &release.Release{TagName: tag}
	if withSigAssets {
		r.Assets = []release.Asset{
			{Name: "SHA256SUMS.sig", BrowserDownloadURL: base + "/SHA256SUMS.sig"},
			{Name: "SHA256SUMS.pem", BrowserDownloadURL: base + "/SHA256SUMS.pem"},
		}
	}
	return r
}

// TestSignatureDownloadFailureIsFatal is SA-03. A signature asset that cannot
// be downloaded used to return "skipped: could not download signature" with a
// nil error, so the upgrade proceeded. On a release that must be signed, an
// attacker who can serve a modified binary AND its matching manifest only had
// to break the signature download to remove the signer check.
func TestSignatureDownloadFailureIsFatal(t *testing.T) {
	if release.CosignPath() == "" {
		t.Skip("cosign not installed; the cosign-absent branch is deliberately non-fatal")
	}
	for _, missing := range []string{"SHA256SUMS.sig", "SHA256SUMS.pem"} {
		t.Run(missing, func(t *testing.T) {
			srv := sigServer(t, missing)
			rel := releaseWithSigs("v1.0.1", srv.URL, true)

			status, err := verifyManifestSignature(context.Background(), rel, "deadbeef  plivo_linux_amd64\n")
			if err == nil {
				t.Fatalf("expected a fatal error, got status %q and nil error (this is the fail-open)", status)
			}
			if !strings.Contains(err.Error(), "Refusing to install") {
				t.Errorf("error should say it refused to install, got: %v", err)
			}
		})
	}
}

// A post-boundary release publishing NO signature assets at all must also be
// refused. Previously this returned "unsigned" with a nil error for any tag.
func TestMissingSignatureAssetsFatalAfterBoundary(t *testing.T) {
	rel := releaseWithSigs("v1.0.1", "", false)
	status, err := verifyManifestSignature(context.Background(), rel, "sums")
	if err == nil {
		t.Fatalf("expected refusal for an unsigned v1.0.1, got status %q", status)
	}
	if !strings.Contains(err.Error(), release.FirstSignedRelease) {
		t.Errorf("error should name the signing boundary, got: %v", err)
	}
}

// The legacy exception must still work, or this change breaks installing the
// genuinely-unsigned older releases it exists to permit.
func TestPreBoundaryReleaseStillInstallsUnsigned(t *testing.T) {
	rel := releaseWithSigs("v0.2.0", "", false)
	status, err := verifyManifestSignature(context.Background(), rel, "sums")
	if err != nil {
		t.Fatalf("v0.2.0 predates signing and must still install: %v", err)
	}
	if status != "unsigned" {
		t.Errorf("status = %q, want \"unsigned\"", status)
	}
}

// The override exists so an operator can consciously accept the risk. If it
// does not work, the only escape from a broken signing pipeline is downgrading.
func TestAllowUnsignedOverride(t *testing.T) {
	t.Setenv(allowUnsignedEnv, "1")
	rel := releaseWithSigs("v1.0.1", "", false)
	if _, err := verifyManifestSignature(context.Background(), rel, "sums"); err != nil {
		t.Fatalf("override should permit the install, got: %v", err)
	}
}
