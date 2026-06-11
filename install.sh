#!/usr/bin/env bash
# install plivo cli — cross-platform one-line installer (macOS / Linux / Windows)
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/main/install.sh | bash
#
# On native Windows (PowerShell, no bash), use install.ps1 instead:
#   irm https://raw.githubusercontent.com/plivo/plivo-cli/main/install.ps1 | iex
#
# This bash installer also works on Windows under Git Bash / MSYS2 / Cygwin /
# WSL. WSL reports as Linux and installs the Linux binary; Git Bash/MSYS/Cygwin
# install the native windows .exe.
#
# Requires the repo to be public and a published GitHub release whose assets
# are named plivo_<os>_<arch>[.exe] (see `make build-all`).
#
# Env overrides:
#   PLIVO_CLI_VERSION   tag to install (default: latest)
#   PLIVO_INSTALL_DIR   where to drop the binary
#                       (default: /usr/local/bin on unix, ~/bin on windows)
set -euo pipefail

REPO="plivo/plivo-cli"
VERSION="${PLIVO_CLI_VERSION:-latest}"

# ─── Detect OS (+ executable extension) ──────────────────────────────────────
raw_os=$(uname -s | tr '[:upper:]' '[:lower:]')
EXT=""
case "$raw_os" in
  darwin)                 OS=darwin ;;
  linux)                  OS=linux ;;
  mingw*|msys*|cygwin*|windows*|win32) OS=windows; EXT=".exe" ;;
  *) echo "✗ unsupported OS: $raw_os (need darwin, linux, or windows)" >&2; exit 1 ;;
esac

# ─── Detect architecture ─────────────────────────────────────────────────────
raw_arch=$(uname -m)
case "$raw_arch" in
  x86_64|amd64)   ARCH=amd64 ;;
  arm64|aarch64)  ARCH=arm64 ;;
  *) echo "✗ unsupported arch: $raw_arch (need amd64 or arm64)" >&2; exit 1 ;;
esac

# ─── Resolve download URL ────────────────────────────────────────────────────
ASSET="plivo_${OS}_${ARCH}${EXT}"
if [ "$VERSION" = "latest" ]; then
  URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"
else
  URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
fi

# ─── Resolve install dir (per-platform default) ──────────────────────────────
if [ "$OS" = "windows" ]; then
  DEFAULT_DIR="${HOME}/bin"
else
  DEFAULT_DIR="/usr/local/bin"
fi
INSTALL_DIR="${PLIVO_INSTALL_DIR:-$DEFAULT_DIR}"
TARGET="${INSTALL_DIR}/plivo${EXT}"

echo "→ Downloading plivo for ${OS}/${ARCH} (${VERSION})..."
TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT
if ! curl -fL --progress-bar -o "$TMP" "$URL"; then
  echo "✗ Download failed: $URL" >&2
  echo "  Check that a release exists and the asset ${ASSET} is published." >&2
  exit 1
fi
chmod +x "$TMP" 2>/dev/null || true

# ─── Install ─────────────────────────────────────────────────────────────────
echo "→ Installing to ${TARGET}..."
mkdir -p "$INSTALL_DIR" 2>/dev/null || true
if [ -w "$INSTALL_DIR" ] || [ "$OS" = "windows" ]; then
  mv "$TMP" "$TARGET"
else
  echo "  (need sudo to write to ${INSTALL_DIR})"
  sudo mv "$TMP" "$TARGET"
fi
trap - EXIT

echo
echo "✓ Installed: $("$TARGET" --version 2>/dev/null || echo "plivo (run 'plivo --version')")"
echo "✓ Location:  $TARGET"

# ─── PATH hint ───────────────────────────────────────────────────────────────
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) : ;; # already on PATH
  *)
    echo
    echo "⚠ ${INSTALL_DIR} is not on your PATH. Add it, e.g.:"
    if [ "$OS" = "windows" ]; then
      echo "    setx PATH \"%PATH%;${INSTALL_DIR}\"   # then restart the shell"
    else
      echo "    echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.bashrc && source ~/.bashrc"
    fi
    ;;
esac

echo
echo "Next: plivo login"
