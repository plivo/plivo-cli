# plivo

The official command-line interface for the Plivo platform.

Single Go binary. Manages numbers, applications, messages, calls, recordings, conferences, multi-party calls, audio streams, verify sessions, lookups, 10DLC registration, powerpacks, and toll-free verification — every resource on the public Plivo REST API.

> **Status:** pre-release. Code lives on the [`beta`](https://github.com/plivo/plivo-cli/tree/beta) branch. `main` will be updated at the v1.0 cut.

## Install

> Pre-release: the one-liners below work once the repo is public and a release
> is published. Until then, build from source (bottom).

**macOS / Linux / WSL / Git Bash** (auto-detects OS + arch):

```bash
curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/beta/install.sh | bash
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/plivo/plivo-cli/beta/install.ps1 | iex
```

Both installers fetch the matching release binary (`darwin`/`linux`/`windows` ×
`amd64`/`arm64`). Override with `PLIVO_CLI_VERSION` (a release tag) and
`PLIVO_INSTALL_DIR` (target directory).

**Build from source** (any platform with Go):

```bash
git clone -b beta https://github.com/plivo/plivo-cli.git
cd plivo-cli
make install        # → $GOPATH/bin/plivo
# or cross-compile every target:
make build-all      # → dist/plivo_<os>_<arch>[.exe]
```

## Quickstart

```bash
# Set credentials (or use `plivo auth login`)
export PLIVO_AUTH_ID=MAxxxxxxxxxxxxxxxxxxxx
export PLIVO_AUTH_TOKEN=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

# Verify
plivo auth whoami

# Common ops
plivo number list
plivo number search --country US --type local --limit 5
plivo message send --src +1... --dst +1... --text "hi" --yes
plivo call list --limit 5
```

## Design contract

- **Single binary** — no Node, no Python, no runtime. ~7 MB stripped.
- **Dual TTY/JSON output** — table when interactive, JSON when piped. Force with `-o json`.
- **`--dry-run` default** on spend verbs (`message send`, `call make`, `number buy`, `verify session create`, `cnam`, `masking session create`, `mpc create`, `brand create`, `campaign create`). Pass `--yes` to actually charge.
- **`--yes` gate** on destructive verbs (`*delete`, `*release`, `*hangup`, `call hangup`, `stream stop`, etc.). Without it: exit code 5 + `DESTRUCTIVE_REFUSED`.
- **Stable error envelope** on stderr — every error includes `code`, `message`, `hint`, `retryable`, `status_code`, `request_id`. AI driver loops can switch on `code` without parsing free text.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | OK |
| 1 | User error (bad input, missing creds, validation) |
| 2 | Auth error (401/403) |
| 3 | Network / upstream error (5xx, transport) |
| 4 | Rate limit (429) |
| 5 | Destructive op refused (no `--yes`) |

## Roadmap

See [ROADMAP.md](ROADMAP.md).

## Reporting issues

Open an issue at [github.com/plivo/plivo-cli/issues](https://github.com/plivo/plivo-cli/issues). For security reports, see [SECURITY.md](SECURITY.md).

This is a read-only repo for users. We don't accept external PRs at this time; please file an issue and we'll triage it.

## License

[Apache-2.0](LICENSE).
