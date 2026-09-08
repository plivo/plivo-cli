#!/usr/bin/env bash
# Print the release notes for a version: its CHANGELOG section, plus install
# and verification instructions.
#
# This exists so a hand-cut release and an Actions-cut one produce the same
# body. Every release so far has been cut by hand (release.yml cannot publish
# from a hosted runner: the org IP allow list blocks GitHub API writes), and
# the body was retyped each time. Retyping is how a release ends up with the
# previous version's notes.
#
# Usage: scripts/release-notes.sh v1.0.1
set -euo pipefail

VERSION="${1:?usage: release-notes.sh <version>   e.g. v1.0.1}"
CHANGELOG="${CHANGELOG:-CHANGELOG.md}"
BARE="${VERSION#v}"

if [ ! -f "$CHANGELOG" ]; then
  echo "release-notes.sh: $CHANGELOG not found (run from the repo root)" >&2
  exit 1
fi

# Everything between this version's heading and the next one. Matching the
# heading exactly matters: a prefix match would let [1.0.1] pick up [1.0.10].
section=$(awk -v want="## [$BARE]" '
  index($0, want) == 1 { inside = 1; next }
  inside && /^## \[/    { exit }
  inside                { print }
' "$CHANGELOG")

if [ -z "${section//[[:space:]]/}" ]; then
  echo "release-notes.sh: no '## [$BARE]' section in $CHANGELOG" >&2
  exit 1
fi

# Trim blank lines from both ends of the section.
section=$(printf '%s\n' "$section" | awk 'NF {p=1} p' | awk '{a[NR]=$0} END {last=NR; while (last>0 && a[last]=="") last--; for (i=1;i<=last;i++) print a[i]}')

cat <<EOF
$section

---

### Installing

\`\`\`bash
# macOS and Linux
brew install plivo/tap/plivo        # or: brew upgrade plivo

# any POSIX shell, no Homebrew
curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/main/install.sh | bash
\`\`\`

\`\`\`powershell
# Windows
irm https://raw.githubusercontent.com/plivo/plivo-cli/main/install.ps1 | iex
\`\`\`

Already installed? \`plivo upgrade\`.

### Verifying this release

Every release is signed. \`install.sh\` and \`plivo upgrade\` check the
signature automatically; to check it yourself:

\`\`\`bash
for f in SHA256SUMS SHA256SUMS.sig SHA256SUMS.pem; do
  curl -fsSLO "https://github.com/plivo/plivo-cli/releases/download/$VERSION/\$f"
done

cosign verify-blob SHA256SUMS \\
  --signature SHA256SUMS.sig --certificate SHA256SUMS.pem \\
  --certificate-identity cx-tech@plivo.com \\
  --certificate-oidc-issuer https://accounts.google.com

sha256sum --check --ignore-missing SHA256SUMS
\`\`\`
EOF
