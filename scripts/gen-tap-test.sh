#!/usr/bin/env bash
# Regression test for gen-tap.sh: render against a fixed SHA256SUMS and compare
# with the committed golden output. Catches template breakage without needing
# the tap repo, brew, or a network.
#
# Refresh the golden after an intentional template change:
#   scripts/gen-tap.sh v0.3.0 scripts/testdata/SHA256SUMS.fixture scripts/testdata/golden
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

"$here/gen-tap.sh" v0.3.0 "$here/testdata/SHA256SUMS.fixture" "$tmp" >/dev/null

fail=0
for f in Formula/plivo.rb bucket/plivo.json; do
  if ! diff -u "$here/testdata/golden/$f" "$tmp/$f"; then
    echo "✗ $f drifted from the golden — rerun gen-tap.sh into scripts/testdata/golden if intended" >&2
    fail=1
  else
    echo "✓ $f matches golden"
  fi
done

# The formula is Ruby and the manifest is JSON; a syntax break must not reach
# the tap, where it would take out `brew install` for everyone.
if command -v ruby >/dev/null 2>&1; then
  ruby -c "$tmp/Formula/plivo.rb" >/dev/null && echo "✓ formula is valid Ruby"
else
  echo "- ruby not present, skipping syntax check"
fi
python3 -m json.tool "$tmp/bucket/plivo.json" >/dev/null && echo "✓ manifest is valid JSON"

# Every checksum in the fixture must appear in the rendered output, or a bump
# could silently ship a formula pinned to the wrong bytes.
while read -r sum name; do
  case "$name" in
    *windows*) f="$tmp/bucket/plivo.json" ;;
    *)         f="$tmp/Formula/plivo.rb" ;;
  esac
  grep -q "$sum" "$f" || { echo "✗ $name checksum missing from $(basename "$f")" >&2; fail=1; }
done < "$here/testdata/SHA256SUMS.fixture"
[ "$fail" -eq 0 ] && echo "✓ all 6 checksums present"

exit "$fail"
