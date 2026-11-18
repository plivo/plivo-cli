# Plivo CLI

**Use Plivo from your terminal.** Rent numbers, send texts, place calls, check call logs, and ask Plivo's AI assistant for help, without opening the console.

## Install

```sh
# macOS / Linux, with Homebrew
brew install plivo/tap/plivo

# macOS, Linux, WSL, Git Bash, without Homebrew
curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/main/install.sh | bash
```

```powershell
# Windows, with Scoop
scoop bucket add plivo https://github.com/plivo/homebrew-tap
scoop install plivo

# Windows, without Scoop
irm https://raw.githubusercontent.com/plivo/plivo-cli/main/install.ps1 | iex
```

Check it with `plivo --version`. Upgrade with `plivo upgrade` (Homebrew: `brew upgrade plivo`).

To pin a version or install folder, set `PLIVO_CLI_VERSION` or `PLIVO_INSTALL_DIR` before running the installer: `export PLIVO_CLI_VERSION=v1.4.1` in a shell, or `$env:PLIVO_CLI_VERSION = 'v1.4.1'` in PowerShell. The installers check checksums and Plivo's signature. To check a download by hand, see [Verifying a release](SECURITY.md#verifying-a-release).

## Get started

```sh
plivo login                  # opens your browser; the sign-in lasts 30 days
plivo auth whoami
plivo numbers list

# Send a text: --dry-run previews it, --yes sends it
plivo messaging sms send --src <your-plivo-number> --dst <your-phone> --text "Hello" --dry-run
plivo messaging sms send --src <your-plivo-number> --dst <your-phone> --text "Hello" --yes

# Place a call (Plivo plays its hello demo when you answer)
plivo voice calls make --from <your-plivo-number> --to <your-phone> --yes

# See what happened
plivo voice calls list
plivo voice calls get <call_uuid>
```

Commands that spend money, such as sending, calling and renting a number, do nothing without `--yes`. `--dry-run` previews any command.

## Common tasks

| I want to | Run |
|---|---|
| Find and rent a number | `plivo numbers search --country US --type local`, then `plivo numbers buy <number> --yes` |
| Route a number's calls to my app | `plivo account applications create --app-name my-app --answer-url https://example.com/answer --yes`, then `plivo numbers update <number> --app-id <app_id> --yes` |
| Turn text into speech | `plivo tts speak --voice <voice_id> --text "Hello" --out hello.wav --yes` (find voices with `plivo tts voices --language en-US`) |
| Send a one-time passcode | `plivo verify sessions create --help` |
| Look up a number's carrier (billed, no `--yes`) | `plivo lookup <number>`; preview with `--dry-run` |
| See what I spent | `plivo account usage` |
| Switch organization | `plivo org list`, then `plivo org use <name>` |

Commands read `plivo <area> <thing> <action>`. Every command is listed in [`docs/COMMANDS.md`](docs/COMMANDS.md).

## Getting help

```sh
plivo voice calls make --help          # add --help to any command
plivo docs search sip trunk            # search the Plivo docs from the terminal
plivo ask "What does SMS error 30007 mean?"
plivo ask --call-uuid <uuid> "Why did this call fail?"
plivo open                             # open the console in your browser
```

If you belong to several organizations, `plivo login` lets you tick the ones this machine may use. Switch with `plivo org use <name>`, or add `--profile <name>` to one command. To add or remove organizations, run `plivo login` again.

## Scripts and automation

A terminal gets tables; piped output is JSON. Pick a format with `-o table|json|jsonl|yaml|csv` and filter with `--query`:

```sh
plivo voice calls list --query 'data.objects[].call_uuid'
```

Exit codes: 0 ok; 1 bad input, or Plivo refused the request; 2 not signed in, or credentials rejected; 3 network or Plivo-side error; 4 rate limited; 5 missing `--yes`; 6 CLI too old (run `plivo upgrade`).

There is no API-key or environment-variable sign-in, so a CI job or server needs `plivo login` on that machine, repeated every 30 days. Set `PLIVO_NO_UPDATE_CHECK=1` and `PLIVO_FEEDBACK_PROMPT=0` to keep the update hint and the feedback prompt out of your logs. Runnable scripts are in [`examples/`](examples/).

## Using an AI coding agent?

Install the Plivo skill so your agent uses the CLI instead of guessing at the API: `npx skills add plivo/plivo-cli`, or `plivo skill install` (it installs to `~/.claude/skills/plivo-cli`; use `--dir <path>` for another agent, and `plivo skill update` after you upgrade). The reference is [`cli-skill/SKILL.md`](cli-skill/SKILL.md).

## Privacy

Each request carries the CLI version, OS, architecture and the name of the command you ran (for example `plivo voice calls make`). When you are signed in it also carries your email, auth ID, region and organization membership ID (AOM UUID), so Plivo can attribute usage to your account. It never carries flag values, argument values or message text. Turn the identity details off with `plivo config telemetry off` (or `export PLIVO_CLI_TELEMETRY=0`). See [`docs/cli/feedback.md`](docs/cli/feedback.md) for what `plivo feedback` collects.

## More

- [`CHANGELOG.md`](CHANGELOG.md) for what changed in each release, and [plivo.com/docs](https://www.plivo.com/docs) for the platform docs
- Questions or bugs: open an [issue](https://github.com/plivo/plivo-cli/issues). Security reports: [SECURITY.md](SECURITY.md). This repository is read-only for users, so please file an issue rather than a pull request ([CONTRIBUTING.md](CONTRIBUTING.md))
- License: [Apache-2.0](LICENSE)
