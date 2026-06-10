# Plivo CLI

**The official Plivo developer CLI — built for the terminal, scriptable for AI coding agents.**

A single static Go binary for provisioning numbers, wiring voice-agent applications, inspecting calls, sending messages, and running everything in the Plivo platform from a terminal — with JSON-native output, stable exit codes, and a command grammar designed to be invoked by both humans and AI coding agents.

**With the CLI, you can:**

- Provision phone numbers, applications, and 10DLC registrations
- Wire numbers to your voice-agent webhook handlers
- Inspect call records, audio streams, recordings, and conferences
- Send messages, run number lookups, manage verify sessions
- Script everything — table for humans, JSON for pipelines, predictable exit codes for retries

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

### Upgrade

```bash
plivo upgrade           # fetch latest release, atomic in-place replace
plivo upgrade --check   # check only, don't install
```

The CLI also prints a one-line "newer version available" hint on stderr when a TTY is attached; the check runs at most once every 24h. Set `PLIVO_NO_UPDATE_CHECK=1` to silence it. Homebrew installs are detected and routed to `brew upgrade plivo` instead.

## Quickstart

Wire up a voice agent in a few minutes:

```bash
# 1. Authenticate (stores a profile in ~/.plivo/config.toml, with the token in the OS keychain)
plivo login

# 2. Confirm your account and see your numbers
plivo auth whoami
plivo numbers list

# 3. Create an application pointing at your bot's answer URL
plivo account applications create \
  --app-name "my-voice-agent" \
  --answer-url https://my-bot.example.com/answer \
  --yes

# 4. Attach a number to the application
plivo numbers update +14155550100 --app-id <app_uuid> --yes

# 5. Make a test call to verify everything's wired
plivo voice calls make --from +14155550100 --to <your-phone> --yes

# 6. Inspect what happened
plivo voice calls get <call_uuid>
```

Once you have a working setup, the same flow scripts cleanly into CI, integration tests, and AI-agent-driven workflows.

## Ask the assistant

Plivo's AI assistant lives in the terminal too — for docs / pricing questions, or to debug a specific call:

```bash
plivo ask "What does Plivo SMS error 30007 mean?"
plivo ask --call-uuid <uuid> "Debug what happened on this call"
plivo support           # list your past support escalations
```

Tokens stream as they arrive; long voice-debug runs (2–5 min) print live status until the root cause lands. Ctrl-C cancels cleanly. `-o json` emits one JSONL event per SSE frame for scripting and AI agents.

You can also pass credentials via environment variables instead of `plivo login`:

```bash
export PLIVO_AUTH_ID=MAxxxxxxxxxxxxxxxxxxxx
export PLIVO_AUTH_TOKEN=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

## Output

Output is a human table on a TTY and JSON when piped, so you can do:

```bash
plivo voice calls list --limit 50 | jq '.[] | select(.hangup_cause != "NORMAL_CLEARING")'
```

Force either format with `-o table|json`. Spend operations default to a dry run and require `--yes` to execute.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | OK |
| 1 | User error (bad input, missing credentials, validation) |
| 2 | Authentication error (401/403) |
| 3 | Network / upstream error (5xx, transport) |
| 4 | Rate limit (429) |
| 5 | Destructive operation refused (missing `--yes`) |

Stable per category — scripts and AI-agent driver loops can branch on them.

## Command grammar

Commands follow `plivo <service> <resource> <verb>` — for example `plivo voice calls list`, `plivo numbers buy`, `plivo account applications create`. Messaging is the shorter `plivo messaging send` (protocol via `--type sms|mms|whatsapp`). Cobra-level aliases (`call` on `voice calls`, `msg`/`sms` on `messaging`) keep familiar short forms.

For the full command reference, see [`docs/COMMANDS.md`](docs/COMMANDS.md) (auto-generated from the live command tree).

## Documentation

- [`docs/COMMANDS.md`](docs/COMMANDS.md) — full command reference
- [`examples/`](examples/) — runnable scripts for common tasks
- [`CHANGELOG.md`](CHANGELOG.md) — release-by-release changes
- [plivo.com/docs](https://www.plivo.com/docs) — platform docs (XML grammar, webhooks, REST API reference)

## Support

Open an issue at [github.com/plivo/plivo-cli/issues](https://github.com/plivo/plivo-cli/issues). For security reports, see [SECURITY.md](SECURITY.md). This repository is read-only for users; please file an issue rather than a pull request — see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[Apache-2.0](LICENSE).
