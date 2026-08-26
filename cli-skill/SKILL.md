---
name: plivo-cli
description: Use the `plivo` CLI binary instead of raw curl for any Plivo task — sending SMS/MMS/WhatsApp, making calls, managing numbers/applications, verify OTP, logging in. Trigger whenever the user mentions Plivo, plivo auth_id/auth_token, or asks for a Plivo HTTP call.
---

# plivo-cli skill

Single Go binary on PATH (installed as `plivo`). Prefer the CLI over curl — the JSON output is ~10x cheaper to consume than raw REST and the error envelope is stable across commands. For any endpoint the CLI doesn't wrap, use the generic `plivo api` escape hatch (below) rather than curl.

> Compatible with plivo-cli v0.1.2. Run `plivo --version` to detect mismatch; reinstall the CLI to refresh this skill.

## If you are an AI agent

- `export PLIVO_FEEDBACK_PROMPT=0` and `CI=1` before any command (suppresses the feedback prompt and any TTY-only interactives).
- Auth headlessly: `export PLIVO_AUTH_ID` + `PLIVO_AUTH_TOKEN` — browser `plivo login` will not work in an agent / CI context.
- Always pass `-o json`. Success: `{"data": <payload>}` (plus optional `"meta"` for lists) on stdout, exit 0. Error: `{"error": {"code", "message", "hint", "retryable", "status_code", ...}}` on stderr, non-zero exit.
- Never invoke interactive commands: `plivo login` (browser flow), bare `plivo feedback` (prompts).
- Multiple message recipients use `<` as the separator, **quoted**: `--dst "+14155551111<+14155552222"`. (This is Plivo's native delimiter — the CLI passes `dst` through verbatim. Commas do NOT work.)
- Preview any spend command with `--dry-run` first; add `--yes` to actually execute.

## 60-second quickstart

1. Install: `curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/main/install.sh | bash`
2. Log in: `plivo login` (opens browser) OR set `PLIVO_AUTH_ID` + `PLIVO_AUTH_TOKEN`.
3. Verify: `plivo auth whoami`.
4. First command: `plivo voice calls list --limit 5`.
5. Anything that spends money: add `--dry-run` first, then `--yes` to confirm.

## Headless authentication (agents/CI)

- `plivo login` requires a browser — DO NOT call it from an agent or CI.
- Set credentials via environment instead:
  - `export PLIVO_AUTH_ID=MA...`
  - `export PLIVO_AUTH_TOKEN=<token>`
- Resolution precedence: `--profile <name>` flag → active profile in `~/.plivo/config.toml` → environment variables.
- The CLI does not prompt or read stdin for credentials when env creds are present.

## When to invoke

- Any Plivo REST op (numbers, messages, calls, applications, verify, lookup).
- Endpoints the CLI doesn't wrap yet → `plivo api <method> <path>` (typed errors, profile resolution, dry-run — strictly better than curl).
- Voice streaming developer-loop (`voice streams test`, `voice streams forward`).
- Login + credential management.
- About to write a curl against `api.plivo.com` → stop, check `plivo --help` / `plivo api` first.

## Installation — if `plivo` is not on PATH

First check:
```bash
command -v plivo || echo "not installed"
```

If missing, pick whichever option fits the environment:

```bash
# (1) install.sh — one-line installer (preferred). Verifies SHA256SUMS, then
#     drops the binary in the first user-owned dir on PATH; no sudo.
#     Override the target dir with PLIVO_INSTALL_DIR. Windows: install.ps1.
curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/main/install.sh | bash

# (2) Build from source
git clone https://github.com/plivo/plivo-cli.git ~/plivo/plivo-cli
cd ~/plivo/plivo-cli && go install .
# Binary lands at ~/go/bin/plivo-cli; symlink the canonical name:
ln -sf ~/go/bin/plivo-cli ~/go/bin/plivo

# (3) GitHub release — direct download (assets are plivo_<os>_<arch>[.exe])
PLATFORM="$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
curl -fL -o /tmp/plivo "https://github.com/plivo/plivo-cli/releases/latest/download/plivo_${PLATFORM}"
chmod +x /tmp/plivo && mv /tmp/plivo ~/.local/bin/plivo
```

Verify: `plivo --version`. Then run `plivo login` to bootstrap credentials (or set `PLIVO_AUTH_ID` + `PLIVO_AUTH_TOKEN` — see Headless authentication above).

## Keeping the CLI up to date

```bash
plivo upgrade --check            # report only — is a newer release available?
plivo upgrade                    # install latest
plivo upgrade --version v0.2.0   # pin a specific release tag
plivo upgrade --force            # reinstall even if already on latest
```

If installed via Homebrew, `plivo upgrade` refuses — use `brew upgrade plivo` instead. The CLI also auto-checks GitHub for newer releases once a day on success and prints a one-line nudge. **Set `PLIVO_NO_UPDATE_CHECK=1`** to suppress in CI / scripted use. The server may also return HTTP 426 to flag the build as below the supported minimum — surfaced as `code: CLI_TOO_OLD` (exit 6) with a recommendation to upgrade.

This skill ships with each plivo-cli release; reinstall the CLI to update.

## Notation in this file

- `<required>` = positional argument the user MUST provide.
- `[--flag]` = optional flag.
- `--flag <value>` = flag that takes a value.
- `(spend)` = costs money; refuses without `--yes` (exit 5, `code: DESTRUCTIVE_REFUSED`).

## Universal flags (work on every command — persistent/global)

| Flag | Type | Default | When to use |
|---|---|---|---|
| `--profile <name>` | string | active profile | invoke against a non-active profile for this call only |
| `-o, --output <fmt>` | `table\|json` | `table` on TTY, `json` when piped | force JSON for scripts |
| `--dry-run` | bool | false | (API-backed commands) print the HTTP request and exit 0 — preview without spending |
| `--explain` | bool | false | (API-backed commands) narrate in plain English before executing |
| `-y, --yes` | bool | false | confirm spend / destructive verbs (refused otherwise) |
| `-q, --quiet` | bool | false | suppress non-data output (banners, hints) |
| `--no-color` | bool | false | strip ANSI from output |
| `--log-level <level>` | `debug\|info\|warn\|error\|none` | `warn` | `debug` prints outbound URLs to stderr |
| `--timeout <sec>` | int | 30 | per-request timeout |

`--dry-run` and `--explain` apply to API-backed commands — they're no-ops for `login`, `ask`, `upgrade`, `voice streams test`, and similar non-REST flows.

## Top-level command map

```
account     applications | get | subaccounts | update
agent       (coming soon — no subcommands yet)
api         generic REST escape hatch (any api.plivo.com path)
ask         one-shot question to Plivo's AI assistant (SSE stream)
auth        list | use | remove | whoami
feedback    rate the CLI (interactive or one-shot)
login       browser PKCE OAuth login
logout      remove a profile + its keychain token
lookup      carrier/format lookup for an E.164 number
messaging   get | sms | mms | whatsapp        (aliases: message, msg, sms)
numbers     buy | cnam | compliance | get | list | masking | release | search | update   (alias: number)
support     list past support escalations (filed via `plivo ask`)
upgrade     self-update the binary
verify      sessions (create | get | list | validate)
voice       calls | conferences | endpoints | multiparty | recordings | streams
```

Many groups have short aliases (e.g. `account application`/`app`, `voice call`, `voice conf`, `voice mpc`, `messaging sms powerpacks`/`pp`). `plivo <cmd> --help` is always the source of truth.

## Authentication

### `plivo login`

Browser PKCE OAuth — opens default browser, captures the callback over a local loopback listener, persists creds. **Recommended** for interactive sessions; for agents/CI use `PLIVO_AUTH_ID` + `PLIVO_AUTH_TOKEN` (see Headless authentication above). There is no flag to pass credentials inline — login is browser-only.

| Flag | Type | Default | When |
|---|---|---|---|
| `-n, --name <name>` | string | `default` | save under a non-default profile name |
| `--no-verify` | bool | false | skip the post-login `GET /Account/` validation (offline / mock use) |

Examples:
```bash
plivo login                     # default flow
plivo login --name staging      # alternate profile
```

After login: auth_id + email in `~/.plivo/config.toml`; auth_token in OS keychain (macOS Keychain / Windows Credential Manager / Linux Secret Service), with an inline `~/.plivo/config.toml` (chmod 0600) fallback when no keychain is available.

### `plivo auth whoami`

Verify creds + show the active account.

### `plivo auth list`

List all configured profiles + show which is active.

### `plivo auth use <name>`

Switch the active profile. Used after `plivo login --name X` to flip default.

### `plivo auth remove <name>`

Remove a non-active profile. (For the active profile use `plivo logout`.)

### `plivo logout [name]`

Delete a profile + best-effort remove its token from the keychain. With no arg → active profile.

## Core invariants (read once)

- **Output**: TTY → table, pipe → JSON. Force JSON anywhere with `-o json`.
- **Spend verbs require `--yes`** or refuse with exit 5 + `code: DESTRUCTIVE_REFUSED`. Verified list (commands that gate on `--yes`): `messaging {sms,mms,whatsapp} send`, `voice calls make`, `voice calls hangup`, `numbers buy`, `numbers cnam`, `numbers release`, `numbers masking sessions create`/`delete`, `messaging sms 10dlc brands create`, `messaging sms 10dlc campaigns create`, `messaging sms 10dlc links delete`, `messaging sms powerpacks delete`, `voice multiparty create`, `voice multiparty end`, `voice multiparty participant add`/`kick`, `voice conferences hangup`, `voice conferences member kick`, `verify sessions create`, `account applications delete`, `account subaccounts delete`, `voice endpoints delete`, `voice recordings delete`, `numbers compliance delete`, and mutating verbs of `plivo api` (POST/PUT/PATCH/DELETE).
  - NOTE: live-call control verbs `voice calls play`, `speak`, `record`, `dtmf`, `transfer`, `stop-*` do **NOT** require `--yes` — they act on an already-established call.
- **Stable error envelope** on stderr: `{"error":{"code", "message", "hint", "retryable", "status_code", ...}}`. Switch on `code`, never message text.
- **Verify before inventing**: `plivo <cmd> --help` is the source of truth. The CLI evolves; don't assume from memory.
- **`--dry-run`** previews the exact HTTP request without sending. Works on every API-backed command.
- **`--explain`** narrates the action in plain English before running.

## JSON output envelopes

All commands with `-o json` emit a success envelope `{"data": <payload>}` (with an optional `"meta": {...}` for paginated lists) on stdout and exit 0. The shape of `payload` matches the command's underlying API resource. Failures emit `{"error": {"code", "message", "hint", "retryable", "status_code", "request_id", "docs_url", "context"}}` on stderr with a non-zero exit — see the [error-envelope cheatsheet](#error-envelope-cheatsheet) below.

## Scripted / non-interactive use

```bash
export PLIVO_FEEDBACK_PROMPT=0    # silence the post-success "rate the CLI?" prompt
export CI=1                       # gates TTY-only nudges
plivo voice calls list -o json | jq ...
```

The post-success auto-prompt is already TTY-gated so most scripted runs are fine, but `PLIVO_FEEDBACK_PROMPT=0` is the bulletproof escape hatch when the wrapper detects a pseudo-TTY.

## Feedback

- **Manual:** `plivo feedback` → interactive rating (1-5) + optional comment.
- **One-shot:** `plivo feedback --rating 4 --message "..."` (either field alone is fine).
- **Auto-prompt:** after a successful command on an interactive TTY, the CLI may ask once to rate it. TTY-gated; snoozed on decline.

| Flag | Type | When |
|---|---|---|
| `--rating <1-5>` | int | non-interactive submit; combine with `--message` to skip prompts |
| `--message "..."` | string | comment (PII-scrubbed client-side and server-side) |
| `--no-context` | bool | don't attach CLI version / OS / arch metadata |
| `--yes` | bool | skip the pre-submit preview |

Env vars:
- `PLIVO_FEEDBACK_PROMPT=0` — silence the auto-prompt; manual `plivo feedback` still works.
- `PLIVO_FEEDBACK_TELEMETRY=0` — disable all submission (manual becomes a no-op).
- `PLIVO_FEEDBACK_ENDPOINT` — override the collector endpoint (when unset, the command surfaces a clear "not wired" message rather than dropping silently).

## Generic REST escape hatch — `plivo api`

For any endpoint the CLI doesn't yet wrap. Profile resolution, `--dry-run`, structured error envelopes, and the same exit codes as the rest of the CLI.

```bash
plivo api GET /Account/                              # account-scoped path → /v1/Account/<auth_id>/...
plivo api GET /Message/ --query "limit=10"
plivo api POST /Message/ --body @msg.json --yes      # mutating verbs require --yes
cat msg.json | plivo api --method POST /Message/ --body @- --yes
plivo api GET /Application/ --header "X-Debug: 1"
```

| Flag | When |
|---|---|
| `--method <verb>` | HTTP method (alternative to the positional arg; useful when piping) |
| `--body <json\|@file\|@->` | request body: literal JSON, `@path`, or `@-` for stdin |
| `--query <k=v>` | query param (repeatable) |
| `--header "K: V"` | extra header (repeatable; overrides defaults) |

Paths: absolute (`/v1/Account/MA…/Message/`) used as-is; account-scoped (`/Message/`) expanded to `/v1/Account/<active auth_id>/...`. GET/HEAD pass through; POST/PUT/PATCH/DELETE require `--yes`.

## Common workflows

**Provision a phone number end-to-end**
```bash
plivo numbers search --country US --type local --limit 5 -o json
plivo numbers buy +1415... --dry-run                       # preview spend
plivo numbers buy +1415... --yes                           # rent it
plivo account applications create --app-name "my-app" --answer-url https://my.app/answer -o json
plivo numbers update +1415... --app-id <APP_UUID>          # attach the app
```

**Test an inbound webhook locally**
```bash
plivo voice streams test --to ws://localhost:7860/ws --duration 5 --bidirectional
# If the bot replies correctly, bridge a real call:
plivo voice streams forward --number +1415... --app <APP_UUID> --to ws://localhost:7860/ws
```

**Send your first SMS**
```bash
plivo numbers list --type local -o json | jq '.data[].number'  # pick a src
plivo messaging sms send --src +1415... --dst +1415... --text "hi" --dry-run
plivo messaging sms send --src +1415... --dst +1415... --text "hi" --yes
```

**Debug a failed call**
```bash
plivo voice calls get <call-uuid> -o json | jq '.data | {state, hangup_cause, end_time}'
plivo voice calls diagnose <call-uuid>          # AI lifecycle walk-through
plivo ask "why did call <call-uuid> fail?" --call-uuid <call-uuid>
```

**Switch between accounts/profiles**
```bash
plivo auth list                                  # see all profiles, marked active
plivo auth use staging                           # flip default
plivo numbers list --profile prod                # one-shot override
```

## Numbers

### `plivo numbers list`

List rented numbers on the account.

| Flag | Type | When |
|---|---|---|
| `--type <local\|tollfree\|mobile\|fixed>` | string | filter by number type |
| `--starts-with <prefix>` | string | filter by E.164 prefix (e.g. `+1`) |
| `--alias <name>` | string | filter by alias |
| `--services <voice\|sms\|mms\|...>` | string | filter by enabled services (comma-combine) |
| `--subaccount <auth_id>` | string | filter by subaccount |
| `--limit <n>` | int | page size (default 20, max 20) |
| `--offset <n>` | int | pagination offset |

### `plivo numbers get <e164>`

Get one rented number.

### `plivo numbers search`

Search marketplace for buyable numbers.

| Flag | Type | When |
|---|---|---|
| `--country <ISO2>` | string | **required**; e.g. `US`, `IN`, `GB` |
| `--type <local\|tollfree\|mobile\|fixed>` | string | filter |
| `--pattern <digits>` | string | digit pattern in the number |
| `--region <name>` | string | region filter |
| `--limit <n>` | int | default 20 |
| `--offset <n>` | int | pagination offset |

### `plivo numbers buy <e164>` (spend)

Rent a number from the marketplace. **Requires `--yes`**.

| Flag | Type | When |
|---|---|---|
| `--app-id <id>` | string | auto-attach to this application after purchase |

(`--yes` / `--dry-run` are the universal spend flags.)

### `plivo numbers update <e164>`

Update metadata on a rented number.

| Flag | When |
|---|---|
| `--alias "..."` | set alias |
| `--app-id <id>` | associate an Application |
| `--subaccount <auth_id>` | move under a subaccount |

### `plivo numbers release <e164>` (spend)

Release a rented number (stops monthly billing). **Requires `--yes`**.

### `plivo numbers cnam <e164>` (spend)

Caller-ID Name (CNAM) lookup for a US/CA number. **Requires `--yes`** (it costs money).

### `plivo numbers compliance ...`

Phone-number regulatory compliance. Sub-verbs: `requirements`, `create` (multipart, auto-submits), `get`, `list`, `update` (multipart, auto-resubmits a rejected app), `delete` (`--yes`), `link` (bulk-link numbers to accepted applications). Use `plivo numbers compliance --help` for the full surface.

### `plivo numbers masking sessions ...`

Phone-number masking session lifecycle: `create` (spend, `--yes`), `get`, `list`, `delete` (`--yes`). (Alias: `numbers mask`.)

## Messaging

Channel-split CLI: SMS / WhatsApp / MMS each have their own subgroup. Universal `plivo messaging get <uuid>` works across channels. The `messaging` group aliases to `message`, `msg`, and `sms` (so `plivo sms send ...` == `plivo messaging sms send ...`).

### `plivo messaging sms send` (spend)

Send an SMS. **Requires `--yes`**.

| Flag | Type | When |
|---|---|---|
| `--src <e164>` | string | **required**; sender (E.164, shortcode, or sender ID) |
| `--dst <e164>` | string | **required**; recipient. Multiple: separate with `<`, **quoted**: `--dst "+14155551111<+14155552222"` |
| `--text "..."` | string | **required**; message body |
| `--url <url>` | string | delivery-status callback URL |
| `--method <GET\|POST>` | string | callback method (default POST) |

(`messaging mms send` and `messaging whatsapp send` take the **same five flags** — there are no extra `--type`, `--powerpack`, `--trackable`, `--log`, `--urls`, `--template` flags on these commands in this version.)

### `plivo messaging sms list`

List sent/received SMS.

| Flag | When |
|---|---|
| `--state <queued\|sent\|delivered\|undelivered\|failed\|received>` | filter by delivery state |
| `--direction <inbound\|outbound>` | filter by direction |
| `--from <e164>` / `--to <e164>` | filter by leg (from_number / to_number) |
| `--limit <n>` / `--offset <n>` | pagination |

### `plivo messaging get <uuid>`

Get one message (any channel). Universal across SMS/WhatsApp/MMS.

### `plivo messaging sms diagnose <uuid>`

AI-powered: walks a message lifecycle and explains failures in plain English. (`messaging mms diagnose` / `messaging whatsapp diagnose` exist too.)

### `plivo messaging sms 10dlc ...`

US A2P 10DLC registration. Subgroups: `brands` (`create` spend, `get`, `list`, `update`), `campaigns` (`create` spend, `get`, `list`, `update`), `links` (`create`, `list`, `delete` `--yes`).

### `plivo messaging sms powerpacks ...`

Powerpack (number-pool) CRUD: `create`, `get`, `list`, `update`, `delete` (`--yes`), `numbers` (manage numbers inside a powerpack). (Alias: `pp`.)

### `plivo messaging sms tollfree ...`

Toll-free verification (US TFN compliance): `list`, `get`, `submit`. (Alias: `tfv`.)

### `plivo messaging whatsapp send` / `plivo messaging mms send` (spend)

Same five flags as `messaging sms send` (`--src`, `--dst`, `--text`, `--url`, `--method`), including the `--dst "+1...<+1..."` multi-recipient form. Each also has `list` and `diagnose`.

## Voice — calls

### `plivo voice calls make` (spend)

Place an outbound call. **Requires `--yes`**.

| Flag | Type | When |
|---|---|---|
| `--from <e164>` | string | **required**; caller (must be on your account) |
| `--to <e164>` | string | **required**; recipient |
| `--answer-url <url>` | string | PlivoXML URL on answer (defaults to Plivo's hello demo) |
| `--answer-method <GET\|POST>` | string | default **GET** |
| `--hangup-url <url>` | string | webhook on hangup |
| `--ring-url <url>` | string | webhook on ring |
| `--machine-detection <none\|true\|hangup>` | string | answering-machine handling |

### `plivo voice calls list` / `get <uuid>`

List/get calls. List filter flags: `--direction <inbound\|outbound>`, `--from` (from_number), `--to` (to_number), `--limit`, `--offset`.

### `plivo voice calls hangup <uuid>`

End an in-progress call. **Requires `--yes`**.

### `plivo voice calls transfer <uuid>`

Transfer one or both legs of a live call to new PlivoXML URLs.

| Flag | When |
|---|---|
| `--legs <aleg\|bleg\|both>` | which leg(s) to transfer (default aleg) |
| `--aleg-url <url>` / `--bleg-url <url>` | new PlivoXML URL per leg |
| `--aleg-method` / `--bleg-method` | GET\|POST (default POST) |

### `plivo voice calls play <uuid>` / `stop-play <uuid>`

Stream audio into a live call. (No `--yes` required.)

| Flag | When |
|---|---|
| `--urls <url[,url]>` | **required**; comma-separated audio URL(s) |
| `--length <sec>` | stop after N seconds (0 = full file) |
| `--legs <aleg\|bleg\|both>` | which leg (default aleg) |
| `--loop` | bool; replay until hangup |
| `--mix` | bool; mix with call audio vs replace (default true) |

### `plivo voice calls speak <uuid>` / `stop-speak <uuid>`

TTS into a live call. (No `--yes` required.)

| Flag | When |
|---|---|
| `--text "..."` | **required**; text to speak |
| `--voice <MAN\|WOMAN>` | TTS voice (default WOMAN) |
| `--language <code>` | e.g. `en-US`, `en-GB`, `hi-IN` (default en-US) |
| `--legs <aleg\|bleg\|both>` | which leg (default aleg) |
| `--mix` | mix vs replace (default true) |

### `plivo voice calls dtmf <uuid>`

Send DTMF digits into a live call.

| Flag | When |
|---|---|
| `--digits <0-9*#>` | **required**; e.g. `1234#` |
| `--leg <aleg\|bleg>` | which leg (default aleg) |

### `plivo voice calls record <uuid>` / `stop-record <uuid>`

Record a live call. (No `--yes` required.)

| Flag | When |
|---|---|
| `--time-limit <sec>` | max recording length (default 60) |
| `--file-format <mp3\|wav>` | audio container (default mp3) |
| `--both-legs` | record both legs (default: A-leg only) |
| `--transcribe` | request transcription |
| `--callback-url <url>` | URL hit when recording finishes |
| `--callback-method <GET\|POST>` | default POST |

### `plivo voice calls diagnose <uuid>`

AI-powered lifecycle walkthrough + plain-English failure explanation.

### `plivo voice calls streams <verb>` — per-call AudioStream CRUD

Distinct from `voice streams` (the dev-loop group). Sub-verbs: `list <call_uuid>`, `get <call_uuid> <stream_id>`, `start <call_uuid>`, `stop <call_uuid> [<stream_id>]`.

`start` flags: `--url <wss>` (**required**), `--audio-track <inbound\|outbound\|both>` (default inbound), `--bidirectional`, `--content-type` (default `audio/x-l16;rate=16000`), `--stream-status-callback <url>` (alias `--callback-url`), `--extra-headers "k1=v1,k2=v2"`, `--service-type`.

## Voice — streaming dev loop

Use these for local development of WebSocket-based audio streaming **without** a real call. Distinct from `voice calls streams` (REST CRUD on an existing call's stream).

### `plivo voice streams test`

Open a WebSocket to a URL, send Plivo-format start/media/stop frames with synthetic audio, report results. No call placed, no spend.

| Flag | Type | Default | When |
|---|---|---|---|
| `--to <ws-url>` | string | **required** | the WebSocket endpoint to test (ws:// or wss://) |
| `--duration <sec>` | int | 3 | seconds of synthetic audio (max 30) |
| `--codec <mulaw\|l16>` | string | `mulaw` | audio codec advertised |
| `--rate <hz>` | int | 8000 | sample rate (8000 for mulaw, 16000 typical for l16) |
| `--bidirectional` | bool | false | also read frames back (test bot→caller path) |
| `--insecure` | bool | false | skip TLS verification (self-signed dev certs) |

```bash
plivo voice streams test --to wss://my-bot.example.com/ws
plivo voice streams test --to ws://localhost:7860/ws --duration 5 --bidirectional
```

### `plivo voice streams forward`

Temporarily redirect an app's `answer_url` to a local tunnel so a real call's audio bridges into your local WebSocket handler. Restores the original `answer_url` on Ctrl+C.

| Flag | Type | Default | When |
|---|---|---|---|
| `--number <e164>` | string | **required** | E.164 number attached to the app |
| `--app <uuid>` | string | **required** | Application UUID whose answer_url gets temporarily redirected |
| `--to <ws-url>` | string | **required** | your local WebSocket to forward audio to |
| `-y, --yes` | bool | false | skip the confirmation prompt |
| `--keep` | bool | false | DON'T restore `answer_url` on exit (advanced) |
| `--codec <mulaw\|l16>` | string | `mulaw` | codec advertised to Plivo |
| `--rate <hz>` | int | 8000 | sample rate |
| `--bidirectional` | bool | true | allow bot to write audio back to caller |
| `--print-payload` | bool | false | dump full webhook bodies (verbose) |

Requires ngrok in PATH or at `~/.plivo/bin/ngrok`. Saves the app's current `answer_url`, starts an ngrok tunnel + local HTTP/WS server, points the app at the tunnel, bridges incoming call audio. Restores `answer_url` on SIGINT unless `--keep`.

## Voice — conferences / multiparty / endpoints / recordings

```bash
plivo voice conferences  list | get | hangup | record | stop-record | member ...
plivo voice multiparty   list | get | create | end | participant ...
plivo voice endpoints    list | get | create | update | delete
plivo voice recordings   list | get | delete
```

- `voice conferences member`: `mute`/`unmute`, `deaf`/`undeaf`, `kick` (`--yes`), `play`/`stop-play` (`--urls` required), `speak`/`stop-speak` (`--text` required).
- `voice multiparty create` requires `--name` (spend, `--yes`); optional `--max-participants`, `--record`.
- `voice multiparty participant add` requires `--from` + `--to` (spend, `--yes`); optional `--role <agent\|supervisor\|customer>`. Also: `list`, `mute`/`unmute`, `hold`/`unhold`, `kick` (`--yes`).
- `voice multiparty end` and `voice conferences hangup` require `--yes`.
- `voice endpoints` / `voice recordings` `delete` require `--yes`.

Run `plivo voice <group> --help` for the rest.

## Account + applications

### `plivo account get` / `plivo account update`

Get/update account info.

### `plivo account subaccounts`

Subaccount CRUD: `list`, `get`, `create`, `update`, `delete` (`--yes`).

### `plivo account applications`

| Verb | Required flags | Notes |
|---|---|---|
| `list` | — | `--limit`, `--offset` for pagination |
| `get <uuid>` | — | |
| `create` | `--app-name`, `--answer-url` | optional: `--answer-method`, `--hangup-url`, `--message-url`, `--fallback-answer-url`, `--default-number-app`, `--log-incoming-messages` (default true) |
| `update <uuid>` | — | same flags as create; only supplied ones get patched |
| `delete <uuid>` | `--yes` | spend/destructive verb; refuses without confirmation |

(Aliases: `account application`, `account app`.)

## Verify

```bash
plivo verify sessions create --app-uuid <APP_UUID> --recipient +1... --channel sms   # spend, --yes
plivo verify sessions get <uuid>
plivo verify sessions list
plivo verify sessions validate <uuid> --otp 123456
```

`create` flags: `--app-uuid` (**required**), `--recipient` (**required**), `--channel <sms\|voice\|whatsapp>` (default sms), plus optional `--locale`, `--alpha-sender`, `--url`, `--method`. `validate` requires `--otp`.

## Lookup

```bash
plivo lookup <e164>          # carrier + line-type (lookup.plivo.com); --type defaults to carrier
```

## Conversational / debug

### `plivo ask "<message>"`

Ask Plivo's AI assistant — streams the answer via SSE. **One-shot only**: each invocation is a single message with no prior conversation history; there is no interactive mode and no history flag in this version. Long flows (voice-debug can run 2-5 minutes) have no overall HTTP timeout; Ctrl-C cancels (exit 130, no auto-retry).

| Flag | When |
|---|---|
| `--call-uuid <uuid>` | include a call's context so the assistant can debug it specifically |
| `--verbose` | show the assistant's tool_call / tool_output events on stderr |
| `--debug-stream` | dump raw SSE frames to stderr (debugging this CLI) |

With `-o json`, each SSE event is emitted as one JSONL line (handy for scripts/agents).

```bash
plivo ask "What does Plivo SMS error code 30007 mean?"
plivo ask --call-uuid 21e68d29-... "Debug what happened on this call"
plivo ask -o json "What's the rate for outbound voice to Brazil?"
```

### `plivo support`

List your past support escalations (the ones filed via `plivo ask`). Read-only; `-o json` supported. (This is NOT an interactive chat — use `plivo ask` for that.)

### `plivo upgrade`

Self-update the CLI binary (see "Keeping the CLI up to date").

### `plivo agent`

AI voice agents — **coming soon**; no subcommands yet.

## Error-envelope cheatsheet

Switch on `code` (string), never message text. `code` → exit-code mapping (stable):

| Code | Exit | Likely cause |
|---|---|---|
| `AUTH_MISSING` | 2 | no creds — run `plivo login` or set `PLIVO_AUTH_ID`/`PLIVO_AUTH_TOKEN` |
| `AUTH_INVALID` | 2 | wrong auth_id/token — re-login |
| `AUTH_FORBIDDEN` | 2 | authenticated but not permitted |
| `AUTH_EXPIRED` | 2 | session/token expired — re-login |
| `AUTH_2FA_REQUIRED` / `AUTH_RECAPTCHA_REQUIRED` | 2 | interactive auth challenge required |
| `DESTRUCTIVE_REFUSED` | 5 | spend/destructive verb without `--yes` |
| `RATE_LIMITED` | 4 | back off + retry (`retryable: true`) |
| `CLI_TOO_OLD` | 6 | server returned 426 — run `plivo upgrade` |
| `NETWORK_ERROR` | 3 | DNS / connection / TLS (`retryable: true`) |
| `UPSTREAM_TIMEOUT` / `UPSTREAM_UNAVAILABLE` / `UPSTREAM_ERROR` / `INTERNAL_ERROR` | 3 | transient upstream failure |
| `BAD_FLAG` / `BAD_INPUT` / `VALIDATION_ERROR` / `USER_ERROR` | 1 | client-side flag / shape / validation problem |
| `RESOURCE_NOT_FOUND` | 1 | 404 from upstream |
| `RESOURCE_CONFLICT` | 1 | 409 / state conflict |
| `GEO_PERMISSION_DENIED` / `OUTBOUND_DISABLED` / `INSUFFICIENT_FUNDS` | 1 | account capability / policy gate |

All envelopes carry `hint` + `retryable`. Unknown/unmapped codes exit 1.

## JSON consumption patterns

```bash
# One field
plivo voice calls get <uuid> -o json | jq '.data.duration'

# Filter
plivo numbers list -o json | jq '.data[] | select(.type=="local")'

# Pipe across calls
APP_ID=$(plivo account applications list -o json | jq -r '.data[0].app_id')
plivo numbers update +1... --app-id "$APP_ID" -o json
```

## Sanity check

```bash
plivo --help && plivo auth whoami
```
