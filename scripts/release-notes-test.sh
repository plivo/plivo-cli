#!/usr/bin/env bash
# Tests for release-notes.sh. The bug worth guarding is a body that silently
# carries the wrong version's notes, which reads as a successful release.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

fails=0
check() {
  if [ "$2" = "$3" ]; then printf '  ✓ %s\n' "$1"
  else printf '  ✗ %s\n      want %q\n      got  %q\n' "$1" "$3" "$2"; fails=$((fails + 1)); fi
}

fixture=$(mktemp)
trap 'rm -f "$fixture"' EXIT
printf '# Changelog\n\nBlurb.\n\n## [1.0.10] - 2026-10-01\n\n### Added\n\n- ten\n\n## [1.0.1] - 2026-09-08\n\n### Added\n\n- one\n\n## [1.0.0] - 2026-09-03\n\n- zero\n' > "$fixture"

body=$(CHANGELOG="$fixture" ./scripts/release-notes.sh v1.0.1)
check "picks its own section"        "$(printf '%s' "$body" | grep -c '^- one$')"  "1"
check "stops at the next version"    "$(printf '%s' "$body" | grep -c '^- zero$')" "0"
check "does not match [1.0.10]"      "$(printf '%s' "$body" | grep -c '^- ten$')"  "0"
check "leading v is optional"        "$(CHANGELOG="$fixture" ./scripts/release-notes.sh 1.0.1 | grep -c '^- one$')" "1"
check "no changelog heading in body" "$(printf '%s' "$body" | grep -c '^## \[')"   "0"

# A longer version must not be satisfied by a shorter prefix either.
check "[1.0.10] picks ten, not one"  "$(CHANGELOG="$fixture" ./scripts/release-notes.sh v1.0.10 | grep -c '^- ten$')" "1"

CHANGELOG="$fixture" ./scripts/release-notes.sh v9.9.9 >/dev/null 2>&1
check "unknown version exits 1"      "$?" "1"
CHANGELOG=/nonexistent ./scripts/release-notes.sh v1.0.1 >/dev/null 2>&1
check "missing changelog exits 1"    "$?" "1"

# The install and verify blocks are the reason this is not a plain awk one-liner.
check "names the install one-liner"  "$(printf '%s' "$body" | grep -c 'brew install plivo/tap/plivo')" "1"
check "pins the signing identity"    "$(printf '%s' "$body" | grep -c 'certificate-identity cx-tech@plivo.com')" "1"
check "verify URL pins the tag"      "$(printf '%s' "$body" | grep -c 'releases/download/v1.0.1/')" "1"

# The real CHANGELOG must always be able to produce the current release.
current=$(grep -m1 '^## \[' CHANGELOG.md | sed 's/^## \[\([^]]*\)\].*/\1/')
./scripts/release-notes.sh "$current" >/dev/null 2>&1
check "real CHANGELOG top ($current)" "$?" "0"

if [ "$fails" -ne 0 ]; then echo "release-notes: $fails failed" >&2; exit 1; fi
echo "release-notes: all checks passed"
