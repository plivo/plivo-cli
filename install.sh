#!/usr/bin/env bash
# install plivo cli — one-line installer
#
# Usage (while the code lives on the beta branch, pre-v1.0):
#   curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/beta/install.sh | bash
#
# At the v1.0 cut the canonical install.sh moves to the default branch:
#   curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/main/install.sh | bash
#
# Requires the repo to be public and a published GitHub release whose assets
# are named plivo_<os>_<arch> (see `make build-all`).
#
# Env overrides:
#   PLIVO_CLI_VERSION   tag to install (default: latest)
#   PLIVO_INSTALL_DIR   where to drop the binary (default: /usr/local/bin)
set -euo pipefail

REPO="plivo/plivo-cli"
VERSION="${PLIVO_CLI_VERSION:-latest}"
INSTALL_DIR="${PLIVO_INSTALL_DIR:-/usr/local/bin}"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) echo "✗ unsupported arch: $ARCH (need amd64 or arm64)" >&2; exit 1 ;;
esac
case "$OS" in
  darwin|linux) ;;
  *) echo "✗ unsupported os: $OS (need darwin or linux)" >&2; exit 1 ;;
esac

if [ "$VERSION" = "latest" ]; then
  URL="https://github.com/${REPO}/releases/latest/download/plivo_${OS}_${ARCH}"
else
  URL="https://github.com/${REPO}/releases/download/${VERSION}/plivo_${OS}_${ARCH}"
fi

echo "→ Downloading plivo for ${OS}/${ARCH} (${VERSION})..."
TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT
if ! curl -fL --progress-bar -o "$TMP" "$URL"; then
  echo "✗ Download failed: $URL" >&2
  exit 1
fi
chmod +x "$TMP"

echo "→ Installing to ${INSTALL_DIR}/plivo..."
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP" "$INSTALL_DIR/plivo"
else
  echo "  (need sudo for ${INSTALL_DIR})"
  sudo mv "$TMP" "$INSTALL_DIR/plivo"
fi
trap - EXIT

echo
echo "✓ Installed: $("$INSTALL_DIR/plivo" --version)"
echo "✓ Location:  $INSTALL_DIR/plivo"
echo
echo "Next: plivo contacto login --email you@example.com --env dev"
