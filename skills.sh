#!/bin/sh
# install the plivo-cli agent skill — one-line installer (no binary required)
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/main/skills.sh | sh
#
# Fetches SKILL.md — the single-file CLI reference written for LLM coding
# agents — and drops it where the agent auto-loads it. If you already have the
# binary, `plivo skill install` does the same thing offline.
#
# This is POSIX sh and works anywhere curl + sh are available (macOS / Linux /
# WSL / Git Bash). Requires the repo to be public.
#
# Env overrides:
#   PLIVO_SKILL_DIR   where to drop SKILL.md
#                     (default: ~/.claude/skills/plivo-cli — Claude Code.
#                      Point it at another agent's skills directory to install
#                      there instead.)
set -e

REPO="plivo/plivo-cli"
SRC_URL="https://raw.githubusercontent.com/${REPO}/main/cli-skill/SKILL.md"

# ─── Resolve destination ─────────────────────────────────────────────────────
SKILL_DIR="${PLIVO_SKILL_DIR:-$HOME/.claude/skills/plivo-cli}"
TARGET="${SKILL_DIR}/SKILL.md"

# ─── Download into a temp file, then move into place ─────────────────────────
# Stage in a temp file so a failed/partial download never clobbers an existing
# install.
echo "→ Downloading plivo-cli skill..."
TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT

if ! curl -fsSL -o "$TMP" "$SRC_URL"; then
  echo "✗ Download failed: $SRC_URL" >&2
  echo "  Check your connection and that the repo is reachable." >&2
  exit 1
fi

# Guard against a 200-with-empty-body or an HTML error page slipping through.
if [ ! -s "$TMP" ]; then
  echo "✗ Downloaded skill is empty: $SRC_URL" >&2
  exit 1
fi

# ─── Install ─────────────────────────────────────────────────────────────────
echo "→ Installing to ${TARGET}..."
mkdir -p "$SKILL_DIR"
mv "$TMP" "$TARGET"
trap - EXIT

echo
echo "✓ Installed skill: $TARGET"
echo
echo "Agents that read ~/.claude/skills (e.g. Claude Code) will auto-load it."
echo "Other agents: point PLIVO_SKILL_DIR at their skills directory and re-run."
