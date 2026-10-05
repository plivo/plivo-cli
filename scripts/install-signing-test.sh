#!/usr/bin/env bash
# SA-03: install.sh must refuse an unsigned install of a release that should
# be signed. Previously a failed signature download skipped the whole block
# silently, so the installer proceeded with no signer check and no message.
#
# Tests signing_required() lifted straight out of install.sh, so the boundary
# cannot drift from the Go side (internal/release.SigningRequired).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

eval "$(sed -n '/^signing_required() {/,/^}/p' install.sh)"

fails=0
check() {
  signing_required "$1"; local got=$?   # 0 = signature required, 1 = not
  if [ "$got" = "$2" ]; then printf '  ✓ %-10s %s\n' "${1:-<empty>}" "$3"
  else printf '  ✗ %-10s got=%s want=%s\n' "${1:-<empty>}" "$got" "$2"; fails=$((fails+1)); fi
}

# Genuinely predate signing.
check v0.1.0  1 "unsigned allowed"
check v0.2.0  1 "unsigned allowed"
# Boundary onward must be signed.
check v0.3.0  0 "signature required"
check v0.4.1  0 "signature required"
check v1.0.0  0 "signature required"
check v1.0.1  0 "signature required"
check v2.0.0  0 "signature required"
check v10.0.0 0 "signature required"
# Fail closed: an unreadable version is not a licence to skip the check.
check latest  0 "required (resolves to newest)"
check ""      0 "required (fail closed)"
check garbage 0 "required (fail closed)"
check v1      0 "required (fail closed)"

# The refusal path must exist and mention the override.
grep -q 'refuse_unsigned "Could not download the signature' install.sh \
  || { echo "  ✗ install.sh has no refusal on signature-download failure"; fails=$((fails+1)); }
grep -q 'PLIVO_ALLOW_UNSIGNED' install.sh \
  || { echo "  ✗ install.sh does not document the override"; fails=$((fails+1)); }

if [ "$fails" -ne 0 ]; then echo "install-signing: $fails failed" >&2; exit 1; fi
echo "install-signing: all checks passed"
