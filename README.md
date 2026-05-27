# plivo

**Send a text or make a phone ring — from your terminal, in one command.**

`plivo` is the official command-line interface for the Plivo platform: a single static Go binary that speaks the full public Plivo REST API, with JSON-native output built for scripting, automation, and AI agents.

```bash
# Text someone
plivo sms messages send --src +14155550100 --dst +14155550199 --text "Shipped!" --yes

# Make a call that speaks a message when answered
plivo voice calls make --from +14155550100 --to +14155550199 \
  --answer-url https://example.com/answer.xml --yes
```

Numbers, calls, conferences, multi-party rooms, audio streams, messaging, verify, lookups, 10DLC, powerpacks, toll-free — the whole API, scriptable.

> **Status:** pre-release. Install one-liners activate once the first release is published.

## Install

**macOS / Linux / WSL / Git Bash** — auto-detects OS and architecture:

```bash
curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/beta/install.sh | bash
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/plivo/plivo-cli/beta/install.ps1 | iex
```

Both installers fetch the matching release binary (`darwin`/`linux`/`windows` × `amd64`/`arm64`). Override with `PLIVO_CLI_VERSION` (a release tag) or `PLIVO_INSTALL_DIR` (target directory).

## Quickstart

Authenticate, then start issuing commands.

```bash
# Interactive login — stores a profile in ~/.plivo/config.toml
plivo auth login

# Or pass credentials via environment variables
export PLIVO_AUTH_ID=MAxxxxxxxxxxxxxxxxxxxx
export PLIVO_AUTH_TOKEN=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

# Verify the active account
plivo auth whoami

# Everyday operations — `plivo <service> <resource> <verb>`
plivo numbers list
plivo numbers search --country US --type local --limit 5
plivo sms messages send --src +1... --dst +1... --text "hi" --yes
plivo voice calls list --limit 5
```

Commands follow a `plivo <service> <resource> <verb>` grammar (`voice`, `sms`, `numbers`, `verify`, `account`). The pre-grammar short forms still work as aliases — `plivo call list` resolves to `plivo voice calls list`.

Output is a table on a terminal and JSON when piped; force either with `-o table|json`. Spend operations default to a dry run and require `--yes` to execute.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | OK |
| 1 | User error (bad input, missing credentials, validation) |
| 2 | Authentication error (401/403) |
| 3 | Network / upstream error (5xx, transport) |
| 4 | Rate limit (429) |
| 5 | Destructive operation refused (missing `--yes`) |

## Roadmap

See [ROADMAP.md](ROADMAP.md).

## Support

Open an issue at [github.com/plivo/plivo-cli/issues](https://github.com/plivo/plivo-cli/issues). For security reports, see [SECURITY.md](SECURITY.md). This repository is read-only for users; please file an issue rather than a pull request.

## License

[Apache-2.0](LICENSE).
