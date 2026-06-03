<!--
This repository is the official, Plivo-maintained mirror of the Plivo CLI and
is read-only for the community: we don't merge external pull requests. If you
found a bug or want a feature, please open an issue instead — see CONTRIBUTING.md.

The template below is for Plivo maintainers.
-->

## Summary

<!-- What does this change do, and why? -->

## Changes

-

## Breaking change?

- [ ] Yes — describe the migration path below and bump CHANGELOG accordingly
- [ ] No

<!-- If yes: what breaks, who's affected, how do users migrate? -->

## Testing

- [ ] `go build ./...`
- [ ] `go test ./... -race`
- [ ] `gofmt -l .` is clean
- [ ] Help snapshots regenerated if the command tree changed (`go test ./cmd/ -update`)
- [ ] Command reference regenerated if commands or flags changed (`make docs`)

## Related issues

<!-- e.g. Closes #123 -->
