#!/bin/sh
# install the plivo-cli agent skill — one-line installer (no binary required)
#
# Usage:
#   curl -fsSL .../skills.sh | sh              # the CLI skill (default)
#   curl -fsSL .../skills.sh | sh -s agents    # the CX Agents skill
#   curl -fsSL .../skills.sh | sh -s all       # both
#
#   (full URL: https://raw.githubusercontent.com/plivo/plivo-cli/main/skills.sh)
#
# Fetches SKILL.md — a single-file reference written for LLM coding agents — and
# drops it where the agent auto-loads it. If you already have the binary,
# `plivo skill install [cli|agents|all]` does the same thing offline.
#
# Available skills:
#   cli      use the `plivo` CLI instead of raw curl
#   agents   build Plivo CX agent flows through the public Agents API
#
# This is POSIX sh and works anywhere curl + sh are available (macOS / Linux /
# WSL / Git Bash). Requires the repo to be public.
#
# Env overrides:
#   PLIVO_SKILL_DIR   where to drop SKILL.md
#                     (default: ~/.claude/skills/<skill> — Claude Code.
#                      Point it at another agent's skills directory to install
#                      there instead. Only valid when installing ONE skill.)
set -e

REPO="plivo/plivo-cli"
RAW="https://raw.githubusercontent.com/${REPO}/main"

# ─── Resolve which skill(s) ──────────────────────────────────────────────────
# Each entry is "selector:source-dir:install-dir".
CLI_SKILL="cli:cli-skill:plivo-cli"
AGENTS_SKILL="agents:agents-skill:plivo-cx-agents"

case "${1:-cli}" in
  cli)    WANTED="$CLI_SKILL" ;;
  agents) WANTED="$AGENTS_SKILL" ;;
  all)    WANTED="$CLI_SKILL $AGENTS_SKILL" ;;
  *)
    echo "✗ Unknown skill: $1" >&2
    echo "  Available: cli, agents, all" >&2
    exit 1
    ;;
esac

# PLIVO_SKILL_DIR names one directory, so it cannot apply to two skills.
if [ -n "${PLIVO_SKILL_DIR:-}" ] && [ "${1:-cli}" = "all" ]; then
  echo "✗ PLIVO_SKILL_DIR installs a single skill; name it instead of \"all\"." >&2
  exit 1
fi

# ─── Download + install each ─────────────────────────────────────────────────
# Stage in a temp file so a failed/partial download never clobbers an existing
# install.
INSTALLED=""
for entry in $WANTED; do
  SELECTOR=$(echo "$entry" | cut -d: -f1)
  SRC_DIR=$(echo "$entry" | cut -d: -f2)
  DEST_NAME=$(echo "$entry" | cut -d: -f3)

  SRC_URL="${RAW}/${SRC_DIR}/SKILL.md"
  SKILL_DIR="${PLIVO_SKILL_DIR:-$HOME/.claude/skills/$DEST_NAME}"
  TARGET="${SKILL_DIR}/SKILL.md"

  echo "→ Downloading ${SELECTOR} skill..."
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

  echo "→ Installing to ${TARGET}..."
  mkdir -p "$SKILL_DIR"
  mv "$TMP" "$TARGET"
  trap - EXIT
  INSTALLED="${INSTALLED}  ${TARGET}
"
done

echo
echo "✓ Installed:"
printf '%s' "$INSTALLED"
echo
echo "Agents that read ~/.claude/skills (e.g. Claude Code) will auto-load them."
echo "Other agents: point PLIVO_SKILL_DIR at their skills directory and re-run."
