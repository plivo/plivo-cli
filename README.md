# Plivo CLI

**The official Plivo developer CLI — built for the terminal, scriptable for AI coding agents.**

A single static Go binary for provisioning numbers, wiring voice-agent applications, inspecting calls, sending messages, and running everything in the Plivo platform from a terminal — with JSON-native output, stable exit codes, and a command grammar designed to be invoked by both humans and AI coding agents.

**With the CLI, you can:**

- Provision phone numbers, applications, and 10DLC registrations
- Wire numbers to your voice-agent webhook handlers
- Inspect call records, audio streams, recordings, and conferences
- Send messages, run number lookups, manage verify sessions
- Script everything — table for humans, JSON for pipelines, predictable exit codes for retries

## Install

**Homebrew (macOS / Linux)** — recommended:

```bash
brew install plivo/tap/plivo
```

**Scoop (Windows):**

```powershell
scoop bucket add plivo https://github.com/plivo/homebrew-tap
scoop install plivo
```

Or install the binary directly, no package manager needed.

**macOS / Linux / WSL / Git Bash** — auto-detects OS and architecture:

```bash
curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/main/install.sh | bash
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/plivo/plivo-cli/main/install.ps1 | iex
```

### Verifying a release

Both installers check the SHA-256 checksums, and also verify who signed them
when [cosign](https://docs.sigstore.dev/cosign/installation/) is present. To
check by hand:

```bash
V=v0.3.0
for f in SHA256SUMS SHA256SUMS.sig SHA256SUMS.pem; do
  curl -fsSLO "https://github.com/plivo/plivo-cli/releases/download/$V/$f"
done

cosign verify-blob SHA256SUMS \
  --signature SHA256SUMS.sig --certificate SHA256SUMS.pem \
  --certificate-identity cx-tech@plivo.com \
  --certificate-oidc-issuer https://accounts.google.com
```

`Verified OK` means the checksums genuinely came from Plivo. Pinning both the
identity and the issuer is the point — without them, any valid Sigstore
signature would pass.

Releases before v0.3.0 are unsigned, so verification is skipped rather than
failed for those.

If you installed with `curl | bash` first and later switch to Homebrew, the
older copy in `~/.local/bin` stays earlier on your `PATH` and keeps winning.
Homebrew warns when it spots this — remove `~/.local/bin/plivo` to switch over.

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

## For LLM agents

If you're an LLM (Claude, Codex, Cursor, Aider, custom agent) and the user has asked you to do anything Plivo-related — **read [`cli-skill/SKILL.md`](cli-skill/SKILL.md) first**. It's the single-file reference written for agent consumption: every command with its required args, flag table, and "when to use" — plus the universal invariants (`--dry-run`, `--yes` for spend verbs, the stable error-envelope codes, JSON-output rules).

**TL;DR for an agent starting cold:**

```bash
# 1. Is the CLI installed?
command -v plivo || echo "Install instructions: cli-skill/SKILL.md → Installation"

# 2. Is there an active credential profile?
plivo auth whoami    # exit 2 + AUTH_MISSING means no creds — point user at `plivo login`

# 3. Force JSON output for parsing (default on non-TTY, explicit for safety)
plivo voice calls list -o json | jq '.data[] | {call_uuid, duration}'

# 4. NEVER spend money without --yes — the CLI refuses with exit 5 anyway,
#    but check with --dry-run first to confirm the request shape.
plivo voice calls make --from +1... --to +1... --dry-run    # preview
plivo voice calls make --from +1... --to +1... --yes        # actually call
```

**Three rules of thumb:**

1. Don't curl `api.plivo.com` directly. The CLI does it 10× cheaper for both you and the user (stable JSON envelope, retry semantics, error mapping).
2. Branch on `error.code` (e.g. `AUTH_MISSING`, `DESTRUCTIVE_REFUSED`, `RATE_LIMITED`), never on `error.message` — message text drifts; codes are committed.
3. For scripted use: set `PLIVO_FEEDBACK_PROMPT=0` and `PLIVO_NO_UPDATE_CHECK=1` so the post-success "rate the CLI?" prompt and update-check banner don't surprise stdin/stderr.

**Install the skill once** so future agent sessions auto-load it — a few ways:

```bash
# Via the open skills ecosystem (skills.sh) — Claude Code, Cursor, Codex, and
# 60+ agents, no Plivo binary required:
npx skills add plivo/plivo-cli

# With the CLI installed — writes the bundled skill, no network needed:
plivo skill install                 # → ~/.claude/skills/plivo-cli/SKILL.md

# Without the binary — one-line installer fetches SKILL.md from GitHub:
curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/main/skills.sh | sh
```

Both default to the Claude Code skills directory (`~/.claude/skills/plivo-cli`). For another agent, point it elsewhere — `plivo skill install --dir <path>` or `PLIVO_SKILL_DIR=<path>` for the installer. To capture the skill for an agent that loads it differently, `plivo skill install --print` writes it to stdout.

The skill file lazy-loads on relevance — it only enters your context window when the user mentions Plivo / `auth_id`, etc.

## Documentation

- [`cli-skill/SKILL.md`](cli-skill/SKILL.md) — single-file reference tuned for LLM agents (recommended starting point even for humans)
- [`docs/COMMANDS.md`](docs/COMMANDS.md) — full command reference (auto-generated)
- [`examples/`](examples/) — runnable scripts for common tasks
- [`CHANGELOG.md`](CHANGELOG.md) — release-by-release changes
- [plivo.com/docs](https://www.plivo.com/docs) — platform docs (XML grammar, webhooks, REST API reference)

## Privacy & telemetry

Every CLI request carries a small set of `X-Plivo-CLI-*` headers: CLI
version / OS / arch (used for the upgrade nudge and usage stats), plus,
when you're logged in, identity headers (email, auth ID, region, AOM
UUID) so Plivo can attribute usage per account. These headers never
carry flag values, argument values, or free-text bodies — those only
travel in the actual Plivo API request you asked the CLI to make.

Turn the identity headers off entirely:

```bash
plivo config telemetry off      # persists in ~/.plivo/config.toml
export PLIVO_CLI_TELEMETRY=0    # or, for one shell session / CI job
```

`plivo config telemetry status` shows the current state. Version/OS/arch
stay on either way — that's how the CLI knows when to nudge an upgrade.
See [`docs/cli/feedback.md`](docs/cli/feedback.md) for what `plivo
feedback` collects specifically.

## Support

Open an issue at [github.com/plivo/plivo-cli/issues](https://github.com/plivo/plivo-cli/issues). For security reports, see [SECURITY.md](SECURITY.md). This repository is read-only for users; please file an issue rather than a pull request — see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[Apache-2.0](LICENSE).
