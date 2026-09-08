# Cutting a release

For Plivo maintainers. Every release so far has been cut by hand, and for now
that is the only way to cut one — see [Why this is manual](#why-this-is-manual).

Budget 30 minutes. Steps 5 and 6 need a browser.

## 1. Green on main

```bash
git switch main && git pull
make fmt vet test test-tap test-release-notes
make docs && git diff --exit-code docs/COMMANDS.md   # must be clean
```

`docs/COMMANDS.md` is generated. If that diff is dirty, commit it before
tagging, or the release ships a stale command reference.

## 2. Write the CHANGELOG section

Add `## [X.Y.Z] - YYYY-MM-DD` at the top of `CHANGELOG.md`, under
`Added` / `Changed` / `Fixed`. This text becomes the release notes verbatim,
so write it for someone deciding whether to upgrade: what changed for them,
not which files moved.

Preview exactly what GitHub will show:

```bash
make release-notes VERSION=vX.Y.Z
```

## 3. Merge it

Open a PR (`chore: cut vX.Y.Z`), wait for green, merge. `main` is
branch-protected — never push to it directly.

## 4. Tag, then build

Order matters. The version is baked in from `git describe` at build time
(`Makefile:15`), so building before the tag exists produces a binary that
reports the *previous* version with a commit suffix.

```bash
git switch main && git pull
git tag vX.Y.Z && git push origin vX.Y.Z

make build-all                       # 6 targets into dist/
./dist/plivo_darwin_arm64 --version  # must print exactly vX.Y.Z
cd dist && shasum -a 256 plivo_* > SHA256SUMS && cd ..
```

## 5. Sign it

```bash
make sign-release     # opens a browser
```

Log in with **cx-tech@plivo.com** via Google. Not a personal account: the
identity is pinned in `internal/release/signature.go`, and a binary signed by
anything else fails verification for every user.

This writes `dist/SHA256SUMS.sig` and `dist/SHA256SUMS.pem`.

## 6. Verify before publishing, not after

```bash
cosign verify-blob dist/SHA256SUMS \
  --signature dist/SHA256SUMS.sig --certificate dist/SHA256SUMS.pem \
  --certificate-identity cx-tech@plivo.com \
  --certificate-oidc-issuer https://accounts.google.com

# Negative control: a wrong identity MUST be rejected. If this passes,
# the check above is not actually pinning anything.
cosign verify-blob dist/SHA256SUMS \
  --signature dist/SHA256SUMS.sig --certificate dist/SHA256SUMS.pem \
  --certificate-identity nobody@example.com \
  --certificate-oidc-issuer https://accounts.google.com   # expect: failure

cd dist && shasum -a 256 --check SHA256SUMS && cd ..
```

## 7. Publish all 9 assets

Six binaries, `SHA256SUMS`, and both signature files. Dropping `.sig`/`.pem`
does not fail anything loudly — clients treat a release with no signature as
unsigned and skip the check (`SignatureSkipped`), so the release just quietly
stops being verifiable.

```bash
make release-notes VERSION=vX.Y.Z > /tmp/notes.md
gh release create vX.Y.Z --title vX.Y.Z --notes-file /tmp/notes.md \
  dist/plivo_darwin_amd64 dist/plivo_darwin_arm64 \
  dist/plivo_linux_amd64  dist/plivo_linux_arm64 \
  dist/plivo_windows_amd64.exe dist/plivo_windows_arm64.exe \
  dist/SHA256SUMS dist/SHA256SUMS.sig dist/SHA256SUMS.pem
```

## 8. Bump Homebrew and Scoop

Nothing does this for you, and a stale tap is invisible: `brew install`
keeps working, it just installs the old version.

```bash
make release-tap TAP=../homebrew-tap VERSION=vX.Y.Z
cd ../homebrew-tap && git add -u && git commit -m "plivo vX.Y.Z" && git push
cd - && make check-tap-fresh          # expect: ✓ tap is current
```

## 9. Prove it installs

The one check that exercises what a user actually does. A clean container has
no Go toolchain, no repo, and no cached credentials:

```bash
docker run --rm debian:stable-slim bash -c '
  apt-get update -qq && apt-get install -y -qq curl ca-certificates >/dev/null
  curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/main/install.sh | bash
  plivo --version && plivo skill list'
```

Then a cancelled tag-push run to tidy up:

```bash
gh run list --workflow=release.yml --limit 1     # will be queued; cancel it
```

## Why this is manual

Two independent blockers, both outside this repo:

- **No CI runners.** Workflows target a self-hosted pool that has no
  registered runners, so a tag push queues for 24 hours and then times out.
- **The org IP allow list blocks GitHub API writes** from hosted runners, so
  even a green build could not publish the release.

And one that will not go away by fixing those: **signing needs an interactive
browser login.** Cosign keyless gets its certificate from an OIDC flow, so no
unattended job can produce `SHA256SUMS.sig`. `release.yml` therefore lists 7
assets, not 9 — if it ever does run, it publishes an unsigned release.

`release.yml` is kept working and its body is rendered from `CHANGELOG.md`
(not GitHub's auto-generated commit list) so that a run which wakes up late
against an existing tag rewrites the notes to the same text rather than
replacing them with a list of commit subjects.
