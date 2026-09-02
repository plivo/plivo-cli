#!/usr/bin/env bash
# Sign dist/SHA256SUMS with cosign keyless, producing the .sig and .pem that
# install.sh and `plivo upgrade` verify.
#
# Keyless: no private key is created or stored. cosign opens a browser, you log
# in, and the resulting short-lived certificate binds the signature to that
# verified identity. Sign as the shared releases identity, not a personal
# account — the identity is written permanently into a public transparency log,
# and changing it later strands installed binaries that only trust the old one.
#
# Run AFTER dist/SHA256SUMS exists and BEFORE (or right after) publishing, then
# upload both outputs as release assets.
set -euo pipefail

IDENTITY="${PLIVO_SIGN_IDENTITY:-cx-tech@plivo.com}"
SUMS="${1:-dist/SHA256SUMS}"

command -v cosign >/dev/null 2>&1 || {
  echo "✗ cosign not found — install it with: brew install cosign" >&2
  exit 2
}
[ -f "$SUMS" ] || { echo "✗ no such file: $SUMS (run 'make build-all' first)" >&2; exit 2; }

echo "→ Signing $SUMS as $IDENTITY"
echo "  A browser will open; log in as $IDENTITY, not your personal account."
# cosign 3.x defaults to the new bundle format, which requires --bundle. Pin it
# off: the detached .sig/.pem pair is what install.sh and `plivo upgrade` verify.
# The bundle format will eventually be the only one, so this needs revisiting
# alongside the verifier.
cosign sign-blob --yes --new-bundle-format=false --use-signing-config=false "$SUMS" \
  --output-signature   "${SUMS}.sig" \
  --output-certificate "${SUMS%SHA256SUMS}SHA256SUMS.pem"

echo "✓ ${SUMS}.sig"
echo "✓ ${SUMS%SHA256SUMS}SHA256SUMS.pem"
echo
echo "Upload both with the release, then verify from a clean machine:"
echo "  cosign verify-blob SHA256SUMS \\"
echo "    --signature SHA256SUMS.sig --certificate SHA256SUMS.pem \\"
echo "    --certificate-identity $IDENTITY \\"
echo "    --certificate-oidc-issuer https://accounts.google.com"
