#!/usr/bin/env bash
# Render the Homebrew formula and Scoop manifest for a published release.
#
# Checksums come from the release's own SHA256SUMS, so this must run AFTER the
# GitHub release exists. Run it locally: the release workflow cannot push to the
# tap (see the note in the README section this generates).
#
# Usage: scripts/gen-tap.sh <tag> <sha256sums-file> <tap-checkout>
#   scripts/gen-tap.sh v0.3.0 dist/SHA256SUMS ../homebrew-tap
set -euo pipefail

TAG="${1:-}"
SUMS="${2:-}"
OUT="${3:-}"
if [ -z "$TAG" ] || [ -z "$SUMS" ] || [ -z "$OUT" ]; then
  echo "usage: $0 <tag> <sha256sums-file> <tap-checkout>" >&2
  exit 2
fi
[ -f "$SUMS" ] || { echo "✗ no such file: $SUMS" >&2; exit 1; }

VERSION="${TAG#v}"
REPO="plivo/plivo-cli"
BASE="https://github.com/${REPO}/releases/download/${TAG}"

# sha256 for an asset, straight out of the manifest we publish.
sum_for() {
  local asset="$1" got
  got=$(awk -v f="$asset" '$2 == f { print $1 }' "$SUMS")
  if [ -z "$got" ]; then
    echo "✗ $asset missing from $SUMS" >&2
    exit 1
  fi
  printf '%s' "$got"
}

DARWIN_ARM=$(sum_for plivo_darwin_arm64)
DARWIN_AMD=$(sum_for plivo_darwin_amd64)
LINUX_ARM=$(sum_for plivo_linux_arm64)
LINUX_AMD=$(sum_for plivo_linux_amd64)
WIN_AMD=$(sum_for plivo_windows_amd64.exe)
WIN_ARM=$(sum_for plivo_windows_arm64.exe)

mkdir -p "$OUT/Formula" "$OUT/bucket"

# ─── Homebrew formula ────────────────────────────────────────────────────────
# A binary formula: Homebrew core rejects binary-only packages, which is why
# this lives in a tap. Each platform block downloads one file, so the install
# glob only ever matches the asset for the running platform.
#
# No explicit `version`: brew scans it from the release URL, and stating it too
# fails `brew audit --strict` as redundant.
cat > "$OUT/Formula/plivo.rb" <<EOF
class Plivo < Formula
  desc "Command-line interface for the Plivo API"
  homepage "https://github.com/${REPO}"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "${BASE}/plivo_darwin_arm64"
      sha256 "${DARWIN_ARM}"
    end
    on_intel do
      url "${BASE}/plivo_darwin_amd64"
      sha256 "${DARWIN_AMD}"
    end
  end

  on_linux do
    on_arm do
      url "${BASE}/plivo_linux_arm64"
      sha256 "${LINUX_ARM}"
    end
    on_intel do
      url "${BASE}/plivo_linux_amd64"
      sha256 "${LINUX_AMD}"
    end
  end

  def install
    bin.install Dir["plivo_*"].first => "plivo"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/plivo --version")
  end
end
EOF

# ─── Scoop manifest ──────────────────────────────────────────────────────────
# The #/plivo.exe fragment is Scoop's rename idiom, so the installed shim is
# `plivo` rather than `plivo_windows_amd64`.
cat > "$OUT/bucket/plivo.json" <<EOF
{
  "version": "${VERSION}",
  "description": "Command-line interface for the Plivo API",
  "homepage": "https://github.com/${REPO}",
  "license": "Apache-2.0",
  "architecture": {
    "64bit": {
      "url": "${BASE}/plivo_windows_amd64.exe#/plivo.exe",
      "hash": "${WIN_AMD}"
    },
    "arm64": {
      "url": "${BASE}/plivo_windows_arm64.exe#/plivo.exe",
      "hash": "${WIN_ARM}"
    }
  },
  "bin": "plivo.exe",
  "checkver": {
    "github": "https://github.com/${REPO}"
  },
  "autoupdate": {
    "architecture": {
      "64bit": {
        "url": "https://github.com/${REPO}/releases/download/v\$version/plivo_windows_amd64.exe#/plivo.exe"
      },
      "arm64": {
        "url": "https://github.com/${REPO}/releases/download/v\$version/plivo_windows_arm64.exe#/plivo.exe"
      }
    },
    "hash": {
      "url": "https://github.com/${REPO}/releases/download/v\$version/SHA256SUMS"
    }
  }
}
EOF

echo "✓ ${TAG} -> $OUT/Formula/plivo.rb"
echo "✓ ${TAG} -> $OUT/bucket/plivo.json"
