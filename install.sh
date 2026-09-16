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
# are named plivo_<os>_<arch>[.exe] plus a SHA256SUMS manifest (see
# `make build-all` and the Release workflow).
#
# Env overrides:
#   PLIVO_CLI_VERSION   tag to install (default: latest)
#
# Flags (work with `curl ... | bash -s -- <flag>`):
#   --version <tag>     same as PLIVO_CLI_VERSION. Prefer this over the env
#                       var when piping: `VAR=x curl ... | bash` sets VAR on
#                       curl, not on the bash that runs this script, so the
#                       env var is silently ignored and you get `latest`.
#   PLIVO_INSTALL_DIR   where to drop the binary
#                       (default: first user-owned dir on PATH —
#                        ~/.local/bin / ~/bin / /opt/homebrew/bin —
#                        falling back to ~/.local/bin. ~/bin on windows.
#                        No sudo unless you explicitly point at /usr/local/bin
#                        or similar root-owned location — and even then the
#                        installer refuses to escalate silently.)
set -euo pipefail

REPO="plivo/plivo-cli"
VERSION="${PLIVO_CLI_VERSION:-latest}"

# Flags override the env var. These exist because the natural way to pin a
# version through a pipe -- `PLIVO_CLI_VERSION=v1.0.0 curl ... | bash` --
# puts the variable on curl rather than on bash, so it never reaches this
# script and the user silently gets the latest release instead.
while [ $# -gt 0 ]; do
  case "$1" in
    --version)
      [ $# -ge 2 ] || { echo "✗ --version needs a tag, e.g. --version v1.0.0" >&2; exit 2; }
      VERSION="$2"; shift 2 ;;
    --version=*) VERSION="${1#*=}"; shift ;;
    --dir)
      [ $# -ge 2 ] || { echo "✗ --dir needs a path" >&2; exit 2; }
      PLIVO_INSTALL_DIR="$2"; shift 2 ;;
    --dir=*) PLIVO_INSTALL_DIR="${1#*=}"; shift ;;
    -h|--help)
      echo "usage: install.sh [--version <tag>] [--dir <path>]"
      echo "  piping: curl -fsSL <url> | bash -s -- --version v1.0.0"
      exit 0 ;;
    *) echo "✗ unknown option: $1 (try --help)" >&2; exit 2 ;;
  esac
done

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

# ─── Pick the SHA-256 utility ────────────────────────────────────────────────
# macOS ships `shasum -a 256`; Linux + most BusyBox builds ship `sha256sum`.
# We need *some* checksum tool — refuse to install blind if neither is
# available rather than silently skipping verification.
if command -v sha256sum >/dev/null 2>&1; then
  SHA_CMD="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  SHA_CMD="shasum -a 256"
else
  echo "✗ neither sha256sum nor shasum found — install one and re-run" >&2
  echo "  (needed to verify the release-published SHA256SUMS against the downloaded binary)" >&2
  exit 1
fi

# ─── Resolve download URLs ───────────────────────────────────────────────────
ASSET="plivo_${OS}_${ARCH}${EXT}"
if [ "$VERSION" = "latest" ]; then
  URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"
  SUMS_URL="https://github.com/${REPO}/releases/latest/download/SHA256SUMS"
else
  URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
  SUMS_URL="https://github.com/${REPO}/releases/download/${VERSION}/SHA256SUMS"
fi

# ─── Resolve install dir (per-platform default) ──────────────────────────────
# Prefer a user-owned directory so the install doesn't need sudo. Fall
# back to /usr/local/bin only when no user-owned target is already on
# PATH — that's the long-standing default and the right answer when the
# user actually wants a system-wide install.
pick_user_dir() {
  for d in "${HOME}/.local/bin" "${HOME}/bin" "/opt/homebrew/bin"; do
    [ -d "$d" ] && case ":${PATH}:" in *":${d}:"*) printf '%s' "$d"; return ;; esac
  done
  # No existing user-owned dir on PATH — fall back to ~/.local/bin, which
  # we'll create + hint the user about adding to PATH (existing path-hint
  # logic later in the script handles that).
  printf '%s' "${HOME}/.local/bin"
}
if [ "$OS" = "windows" ]; then
  DEFAULT_DIR="${HOME}/bin"
else
  DEFAULT_DIR="$(pick_user_dir)"
fi
INSTALL_DIR="${PLIVO_INSTALL_DIR:-$DEFAULT_DIR}"
TARGET="${INSTALL_DIR}/plivo${EXT}"

# ─── Download binary + checksum manifest into a temp dir ─────────────────────
echo "→ Downloading plivo for ${OS}/${ARCH} (${VERSION})..."
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT
TMP_BIN="${TMPDIR}/${ASSET}"
TMP_SUMS="${TMPDIR}/SHA256SUMS"

if ! curl -fL --progress-bar -o "$TMP_BIN" "$URL"; then
  echo "✗ Download failed: $URL" >&2
  echo "  Check that a release exists and the asset ${ASSET} is published." >&2
  exit 1
fi

# ─── Verify SHA-256 against the release-published manifest ───────────────────
# Refuse to install if anything's off: missing manifest, missing line for
# this asset, or hash mismatch. Bail BEFORE chmod / mv so we never land a
# tampered binary on PATH.
echo "→ Verifying SHA-256..."
if ! curl -fLs -o "$TMP_SUMS" "$SUMS_URL"; then
  echo "✗ Could not download SHA256SUMS manifest: $SUMS_URL" >&2
  echo "  Aborting — refusing to install an unverified binary." >&2
  exit 1
fi
EXPECTED=$(awk -v asset="$ASSET" '$2 == asset || $2 == "*"asset { print $1; exit }' "$TMP_SUMS")
if [ -z "$EXPECTED" ]; then
  echo "✗ SHA256SUMS has no entry for ${ASSET}" >&2
  echo "  Manifest contents:" >&2
  sed 's/^/    /' "$TMP_SUMS" >&2
  exit 1
fi
ACTUAL=$(${SHA_CMD} "$TMP_BIN" | awk '{print $1}')
if [ "$EXPECTED" != "$ACTUAL" ]; then
  echo "✗ SHA-256 mismatch for ${ASSET}" >&2
  echo "    expected: $EXPECTED" >&2
  echo "    actual:   $ACTUAL" >&2
  echo "  Aborting — binary was not chmod'd or moved into PATH." >&2
  exit 1
fi
echo "✓ Checksum verified"

# ─── Provenance: verify the signature over SHA256SUMS ───────────────────────
# The checksum above proves the bytes are intact; this proves they came from us.
#
# Releases from FIRST_SIGNED_RELEASE onward MUST carry a verifiable signature.
# Previously a failed signature download skipped this whole block silently, so
# an attacker who could serve a modified binary and its matching manifest only
# had to break the signature fetch to remove the signer check. What the attacker
# controls (assets present, downloadable, verifying) is now fatal; what only the
# user controls (cosign installed) stays a warning.
FIRST_SIGNED_RELEASE="v0.3.0"
TRUSTED_IDENTITY="cx-tech@plivo.com"
TRUSTED_ISSUERS="https://accounts.google.com https://github.com/login/oauth"
SIG_URL="${SUMS_URL}.sig"
CERT_URL="${SUMS_URL%SHA256SUMS}SHA256SUMS.pem"

# signing_required: 0 when this version predates signing, else 1. "latest"
# always requires it, and an unparseable version fails closed.
signing_required() {
  case "$1" in
    latest|"") return 0 ;;
  esac
  v="${1#v}"
  major="${v%%.*}"; rest="${v#*.}"; minor="${rest%%.*}"
  case "$major$minor" in
    *[!0-9]*|"") return 0 ;;          # unparseable: require a signature
  esac
  if [ "$major" -gt 0 ]; then return 0; fi
  if [ "$major" -eq 0 ] && [ "$minor" -ge 3 ]; then return 0; fi
  return 1
}

MUST_VERIFY=1
if ! signing_required "$VERSION"; then MUST_VERIFY=0; fi
if [ -n "${PLIVO_ALLOW_UNSIGNED:-}" ]; then MUST_VERIFY=0; fi

refuse_unsigned() {
  echo "✗ $1" >&2
  echo "  Releases from ${FIRST_SIGNED_RELEASE} onward must be signed. Refusing to install unverified code." >&2
  echo "  Set PLIVO_ALLOW_UNSIGNED=1 to override if you accept the risk." >&2
  exit 1
}

if curl -fLs -o "${TMPDIR}/SHA256SUMS.sig" "$SIG_URL" 2>/dev/null \
   && curl -fLs -o "${TMPDIR}/SHA256SUMS.pem" "$CERT_URL" 2>/dev/null; then
  if command -v cosign >/dev/null 2>&1; then
    SIG_OK=0
    for iss in $TRUSTED_ISSUERS; do
      if cosign verify-blob "$TMP_SUMS" \
           --signature "${TMPDIR}/SHA256SUMS.sig" \
           --certificate "${TMPDIR}/SHA256SUMS.pem" \
           --certificate-identity "$TRUSTED_IDENTITY" \
           --certificate-oidc-issuer "$iss" >/dev/null 2>&1; then
        SIG_OK=1
        break
      fi
    done
    if [ "$SIG_OK" = "1" ]; then
      echo "✓ Signature verified ($TRUSTED_IDENTITY)"
    else
      echo "✗ The SHA256SUMS signature did NOT verify — refusing to install." >&2
      echo "  Expected signer: $TRUSTED_IDENTITY" >&2
      exit 1
    fi
  else
    # Not fatal: an attacker cannot uninstall the user's cosign, and the
    # checksum still binds the binary to its manifest.
    echo "• Signature published but cosign is not installed — provenance not checked."
    echo "  Install it with: brew install cosign"
  fi
elif [ "$MUST_VERIFY" = "1" ]; then
  refuse_unsigned "Could not download the signature for ${VERSION}."
fi

chmod +x "$TMP_BIN" 2>/dev/null || true

# ─── Install ─────────────────────────────────────────────────────────────────
# Writable target: move. Not writable: refuse to auto-escalate — print a
# clear remediation and exit non-zero. Silent sudo turns a transient typo
# into a credential prompt the user didn't ask for; we'd rather make the
# user pick a path explicitly.
echo "→ Installing to ${TARGET}..."
mkdir -p "$INSTALL_DIR" 2>/dev/null || true
if [ -w "$INSTALL_DIR" ] || [ "$OS" = "windows" ]; then
  mv "$TMP_BIN" "$TARGET"
else
  echo "✗ ${INSTALL_DIR} is not writable by your user." >&2
  echo "  Pick one:" >&2
  echo "    1. Re-run with sudo:" >&2
  echo "         curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo bash" >&2
  echo "    2. Install into a user-owned dir on PATH (no sudo):" >&2
  echo "         curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash -s -- --dir \"\$HOME/.local/bin\"" >&2
  exit 1
fi
trap - EXIT
rm -rf "$TMPDIR"

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
