# Contributing

Thanks for your interest in the Plivo CLI.

## How to contribute

This repository is the official, Plivo-maintained distribution of the CLI and is **read-only for the community**. We don't accept external pull requests.

We maintain the CLI as a curated surface — each command's shape, output schema, and error envelope is part of a stable contract for AI coding agents and automation scripts. External PRs would fragment that contract, so we triage requests through issues and ship them ourselves.

- **Found a bug?** Open a [bug report](https://github.com/plivo/plivo-cli/issues/new/choose).
- **Want a feature?** Open a [feature request](https://github.com/plivo/plivo-cli/issues/new/choose). Before filing, please check [CHANGELOG.md](CHANGELOG.md) — it may already be shipped.
- **Found a security issue?** Please follow [SECURITY.md](SECURITY.md) — do not file a public issue.

Clear, reproducible issues are the most valuable contribution you can make. Include your `plivo --version`, OS/architecture, the exact command, and the output.

### Triage and release cadence

Issues are triaged weekly. Critical bug fixes ship as patch releases as needed; accepted features ship in the next minor release.

Releases follow [Semantic Versioning](https://semver.org); see [CHANGELOG.md](CHANGELOG.md) for what changed in each one. Now that v1.0 has shipped, breaking changes to the command surface wait for a major version. Release artifacts are built from `main`, signed, and published to GitHub Releases; maintainers cutting one should follow [RELEASING.md](RELEASING.md).

## Development (for Plivo maintainers)

Requirements: Go (see `go.mod` for the pinned version). The CLI is a pure-Go, statically linked binary (`CGO_ENABLED=0`).

```bash
# Build the public binary
go build .

# Test (race detector)
go test ./... -race

# Formatting + vet
gofmt -l .
go vet ./...

# Regenerate command help snapshots after any command-tree change
go test ./cmd/ -update

# Regenerate the public command reference
make docs
```

### Command grammar

User-facing commands follow `plivo <service> <resource> <verb>` (for example `plivo voice calls list`); messaging is the shorter `plivo messaging send` (protocol via `--type`).

By contributing you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md).
