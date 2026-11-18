# Plivo CLI

**Use Plivo from your terminal.** Rent phone numbers, send texts, place calls, check what happened on a call, and ask Plivo's AI assistant for help, without opening the console.

- Find, rent and manage phone numbers
- Send SMS, MMS and WhatsApp messages
- Place calls and look back at your call history
- Turn text into speech
- Ask the assistant a question, or have it debug a call
- Switch between your Plivo organizations

## Install

**Homebrew (macOS / Linux):**

```bash
brew install plivo/tap/plivo
```

**Scoop (Windows):**

```powershell
scoop bucket add plivo https://github.com/plivo/homebrew-tap
scoop install plivo
```

**No package manager?** Install the binary directly.

macOS, Linux, WSL or Git Bash:

```bash
curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/main/install.sh | bash
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/plivo/plivo-cli/main/install.ps1 | iex
```

The installers pick the right build for your system. To pin a version or choose where it goes, set `PLIVO_CLI_VERSION` (a release tag such as `v1.4.1`) or `PLIVO_INSTALL_DIR` in your shell first, for example `export PLIVO_CLI_VERSION=v1.4.1`. A variable typed in front of `curl` never reaches the installer.

Check it worked:

```bash
plivo --version
```

If you installed with the script first and later switch to Homebrew, the old copy in `~/.local/bin` can keep winning. Delete `~/.local/bin/plivo` to switch over.

### Upgrade

```bash
plivo upgrade           # install the latest release
plivo upgrade --check   # just check, don't install
```

Homebrew installs upgrade with `brew upgrade plivo` instead. The CLI checks for a newer version at most once a day and prints a short hint while one is out. Set `PLIVO_NO_UPDATE_CHECK=1` to turn the hint off.

<details>
<summary>Verify your download (optional)</summary>

Both installers check the SHA-256 checksums. They also check that Plivo signed the release, when [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) is installed. To check by hand:

```bash
# /releases/latest/ always points at the newest release.
# For a specific release, swap `latest` for `download/vX.Y.Z`.
for f in SHA256SUMS SHA256SUMS.sig SHA256SUMS.pem; do
  curl -fsSLO "https://github.com/plivo/plivo-cli/releases/latest/download/$f"
done

cosign verify-blob SHA256SUMS \
  --signature SHA256SUMS.sig --certificate SHA256SUMS.pem \
  --certificate-identity cx-tech@plivo.com \
  --certificate-oidc-issuer https://accounts.google.com
```

`Verified OK` means the checksums came from Plivo. Keep both the identity and the issuer in the command: without them, any valid Sigstore signature would pass. Releases before v0.3.0 are unsigned, so the check is skipped for them.

</details>

## Get started

**1. Sign in.** Your browser opens, you sign in to Plivo, and the CLI saves the session in your OS keychain. Your password never goes through the CLI.

```bash
plivo login
```

The sign-in lasts 30 days. After that, run `plivo login` again.

**2. Check who you are and what you have.**

```bash
plivo auth whoami
plivo numbers list
```

**3. Send a test text.** Commands that spend money, such as sending a text, placing a call or renting a number, do nothing until you add `--yes`. Start with `--dry-run` to see exactly what would be sent:

```bash
plivo messaging sms send --src <your-plivo-number> --dst <your-phone> --text "Hello from Plivo" --dry-run
plivo messaging sms send --src <your-plivo-number> --dst <your-phone> --text "Hello from Plivo" --yes
```

**4. Place a test call.** Without an `--answer-url`, Plivo plays its hello demo when you pick up:

```bash
plivo voice calls make --from <your-plivo-number> --to <your-phone> --yes
```

**5. See what happened.**

```bash
plivo voice calls list
plivo voice calls get <call_uuid>
```

## Common tasks

Commands read `plivo <area> <thing> <action>`. Add `--help` to any of them to see its options.

| I want to | Run |
|---|---|
| Find a number to rent | `plivo numbers search --country US --type local` |
| Rent it | `plivo numbers buy <number> --yes` |
| Send a text | `plivo messaging sms send --src ... --dst ... --text ... --yes` |
| Place a call | `plivo voice calls make --from ... --to ... --yes` |
| Route a number's calls to my own app | `plivo account applications create --app-name my-app --answer-url https://example.com/answer --yes`, then `plivo numbers update <number> --app-id <app_id> --yes` |
| See my recent calls | `plivo voice calls list` |
| Turn text into speech | `plivo tts speak --voice <voice_id> --text "Hello" --out hello.wav --yes` (find a voice with `plivo tts voices --language en-US`) |
| Send a one-time passcode | `plivo verify sessions create --help` |
| Look up a number's carrier (each lookup is billed and has no `--yes`, so preview with `--dry-run`) | `plivo lookup <number>` |
| See what I spent | `plivo account usage` |
| Switch organization | `plivo org list`, then `plivo org use <name>` |

The full list of commands is in [`docs/COMMANDS.md`](docs/COMMANDS.md).

## Getting help

```bash
plivo --help                  # every command
plivo voice calls make --help # one command in detail
plivo docs search sip trunk   # search the Plivo docs without leaving the terminal
plivo open                    # open the Plivo console in your browser
```

**Ask the assistant.** It answers docs and pricing questions, and can look into a specific call:

```bash
plivo ask "What does Plivo SMS error 30007 mean?"
plivo ask --call-uuid <uuid> "Why did this call fail?"
plivo ask -i                  # chat, with follow-up questions
plivo support                 # your past support escalations
```

Answers stream as they arrive. Debugging a call can take a few minutes, and Ctrl-C cancels cleanly.

## More than one organization

`plivo login` lists every organization you belong to and lets you tick the ones this machine may use. Switch between them without opening the browser again:

```bash
plivo org list
plivo org use <name>
plivo voice calls list --profile <name>   # or pick one for a single command
```

To add or remove organizations later, run `plivo login` again. `plivo auth list` shows the saved profiles.

## Scripts and automation

On a terminal the CLI prints tables. When its output is piped or captured, it prints JSON. Pick a format with `-o table|json|jsonl|yaml|csv`, and filter results with `--query` instead of piping to another tool:

```bash
plivo voice calls list --query 'data.objects[].call_uuid'
```

Exit codes are stable, so a script can react to them:

| Code | Meaning |
|---|---|
| 0 | OK |
| 1 | Bad input, or Plivo refused the request (not found, not enough balance, not allowed) |
| 2 | Not signed in, or Plivo rejected your credentials |
| 3 | Network problem or an error on Plivo's side |
| 4 | Rate limited |
| 5 | Refused because `--yes` was missing |
| 6 | This CLI version is too old for Plivo: run `plivo upgrade` |

Things to know before you automate:

- `plivo login` is the only way to sign in. There is no environment variable or flag for an `auth_id` / `auth_token`, so a CI job or server needs a login on that machine, repeated every 30 days.
- Set `PLIVO_FEEDBACK_PROMPT=0` and `PLIVO_NO_UPDATE_CHECK=1` to keep the "rate the CLI?" prompt and the update hint out of your logs.
- [`examples/`](examples/) has runnable scripts.

## Using an AI coding agent?

Install the Plivo skill once and your agent (Claude Code, Cursor, Codex and others) will use the CLI correctly instead of guessing at the API:

```bash
npx skills add plivo/plivo-cli      # works with 60+ agents, no Plivo install needed
plivo skill install                 # or, with the CLI installed
```

The skill is a single reference file, [`cli-skill/SKILL.md`](cli-skill/SKILL.md), and it only loads when you mention Plivo. It installs to `~/.claude/skills/plivo-cli` by default. For another agent, use `plivo skill install --dir <path>`. Run `plivo skill update` after you upgrade the CLI to refresh it.

## Privacy and telemetry

Each CLI request carries the CLI version, OS and architecture, and the name of the command you ran (for example `plivo voice calls make`). Plivo uses these for the upgrade hint and usage stats. When you are signed in, it also carries your email, auth ID, region and organization membership ID (AOM UUID), so Plivo can attribute usage to your account. It never carries flag values, argument values or message text. Those only go to the Plivo API request you asked for.

Turn the identity details off:

```bash
plivo config telemetry off      # saved in ~/.plivo/config.toml
export PLIVO_CLI_TELEMETRY=0    # or for one shell session or CI job
```

`plivo config telemetry status` shows the current setting. The version, OS, architecture and command name stay on either way. What `plivo feedback` collects is described in [`docs/cli/feedback.md`](docs/cli/feedback.md).

## Documentation

- [`docs/COMMANDS.md`](docs/COMMANDS.md): every command and flag
- [`examples/`](examples/): runnable scripts for common tasks
- [`CHANGELOG.md`](CHANGELOG.md): what changed in each release
- [plivo.com/docs](https://www.plivo.com/docs): platform docs (Voice XML, webhooks, REST API reference)

## Support

Open an issue at [github.com/plivo/plivo-cli/issues](https://github.com/plivo/plivo-cli/issues). For security reports, see [SECURITY.md](SECURITY.md). This repository is read-only for users, so please file an issue rather than a pull request. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[Apache-2.0](LICENSE).
