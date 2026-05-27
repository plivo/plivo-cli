# Contributing

Thanks for your interest in the Plivo CLI.

## How to contribute

This repository is the official, Plivo-maintained distribution of the CLI and
is **read-only for the community**. We don't accept external pull requests.

- **Found a bug?** Open a [bug report](https://github.com/plivo/plivo-cli/issues/new/choose).
- **Want a feature?** Open a [feature request](https://github.com/plivo/plivo-cli/issues/new/choose).
- **Found a security issue?** Please follow [SECURITY.md](SECURITY.md) — do not file a public issue.

Clear, reproducible issues are the most valuable contribution you can make. Include
your `plivo --version`, OS/architecture, the exact command, and the output.

## Development (for Plivo maintainers)

Requirements: Go (see `go.mod` for the pinned version). The CLI is a pure-Go,
statically linked binary (`CGO_ENABLED=0`).

```bash
# Build the public binary and the internal-only build
go build .
go build -tags internal .

# Test (race detector + both build tags)
go test ./... -race
go test -tags internal ./... -race

# Formatting + vet
gofmt -l .
go vet ./...

# Regenerate command help snapshots after any command-tree change
go test ./cmd/ -update
```

### Command grammar

User-facing commands follow `plivo <service> <resource> <verb>` (for example
`plivo voice calls list`, `plivo sms messages send`). Pre-grammar short forms are
preserved as aliases through a small rewrite shim in `cmd/root.go`; keep that map
in sync when adding or renaming commands.

### Internal vs. public surface

The agent / Contacto / scoped-token surface is gated behind the `internal` build
tag and does not ship in the public binary. Keep public-facing changes out of
`//go:build internal` files.

By contributing you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md).
