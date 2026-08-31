#!/usr/bin/env bash
# Fail if the Homebrew tap is serving an older version than the latest release.
#
# The tap bump is a manual step (pushing to it is a GitHub API write, which is
# blocked from hosted runners), so the realistic failure is nobody forgetting
# how — it's nobody noticing. Fly.io's own GoReleaser-automated tap sat two
# years behind its real releases because nothing was watching it.
#
# Read-only and unauthenticated: both endpoints are public.
set -uo pipefail

REPO="${REPO:-plivo/plivo-cli}"
TAP_RAW="${TAP_RAW:-https://raw.githubusercontent.com/plivo/homebrew-tap/main/Formula/plivo.rb}"
RELEASES_API="${RELEASES_API:-https://api.github.com/repos/$REPO/releases/latest}"

fetch() { curl -sfL -m 25 "$1" 2>/dev/null; }

latest_json=$(fetch "$RELEASES_API")
if [ -z "$latest_json" ]; then
  echo "⚠ could not read the latest release; skipping freshness check" >&2
  exit 0 # never fail a build over a transient network or rate limit
fi
latest_tag=$(printf '%s' "$latest_json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)

formula=$(fetch "$TAP_RAW")
if [ -z "$formula" ]; then
  echo "⚠ could not read the tap formula; skipping freshness check" >&2
  exit 0
fi
# The formula carries no `version` line (brew scans it from the URL and audit
# rejects stating it twice), so the release tag in the asset URL is the version.
tap_tag=$(printf '%s' "$formula" | sed -n 's|.*/releases/download/\([^/]*\)/plivo_.*|\1|p' | head -1)

if [ -z "$latest_tag" ] || [ -z "$tap_tag" ]; then
  echo "⚠ could not parse versions (release='$latest_tag' tap='$tap_tag'); skipping" >&2
  exit 0
fi

echo "  latest release : $latest_tag"
echo "  homebrew tap   : $tap_tag"

if [ "$latest_tag" = "$tap_tag" ]; then
  echo "✓ tap is current"
  exit 0
fi

cat >&2 <<EOF
✗ the Homebrew tap is stale — brew users would still get $tap_tag, not $latest_tag

  Bump it from a plivo-cli checkout with the release built in dist/:

    make release-tap
    cd ../homebrew-tap && git add -A && git commit -m "plivo $latest_tag" && git push

  This runs locally by necessity: pushing to the tap is a GitHub API write and
  the org IP allow list blocks those from hosted runners.
EOF
exit 1
