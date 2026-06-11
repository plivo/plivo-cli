---
name: plivo-cli
description: Use the `plivo` CLI binary instead of raw curl for any Plivo or Contacto / Vibe AI agent task — sending SMS, making calls, managing numbers/applications, logging in, and especially creating + running Vibe AI agents. Trigger whenever the user mentions Plivo, Contacto, Vibe agent, PHLO, plivo auth_id/auth_token, or asks for a Plivo HTTP call.
---

# plivo-cli skill

Single Go binary at `~/go/bin/plivo`. Prefer the CLI over curl — the JSON output is ~10x cheaper to consume than raw REST and the error envelope is stable across commands.

## When to invoke

- Any Plivo REST op (numbers, messages, calls, applications, lookup).
- Anything Vibe AI agent (`agent create`, `agent run`, `agent publish`, `agent attach`).
- Voice streaming developer-loop (`voice streams test`, `voice streams forward`).
- Login + credential management.
- About to write a curl against `api.plivo.com` → stop, check `plivo --help` first.

## Notation in this file

- `<required>` = positional argument the user MUST provide.
- `[--flag]` = optional flag.
- `--flag <value>` = flag that takes a value.
- `(spend)` = costs money / mutates state; needs `--yes` or refuses with exit 5.

## Universal flags (work on every command)

| Flag | Type | Default | When to use |
|---|---|---|---|
| `--profile <name>` | string | active profile | invoke against a non-active profile for this call only |
| `-o, --output <fmt>` | `table\|json` | `table` on TTY, `json` when piped | force JSON for scripts |
| `--dry-run` | bool | false | print the HTTP request and exit 0 — preview without spending |
| `--explain` | bool | false | narrate in plain English before executing |
| `-y, --yes` | bool | false | confirm spend / destructive verbs (refused otherwise) |
| `-q, --quiet` | bool | false | suppress non-data output (banners, hints) |
| `--no-color` | bool | false | strip ANSI from output |
| `--log-level <level>` | `debug\|info\|warn\|error\|none` | `warn` | `debug` prints outbound URLs to stderr |
| `--timeout <sec>` | int | 30 | per-request timeout |
| `--all` | bool | false | auto-paginate list ops |

# Authentication

## `plivo login`

Browser PKCE OAuth — opens default browser, captures callback on `127.0.0.1:0`, persists creds. **Recommended** for all sessions.

| Flag | Type | Default | When |
|---|---|---|---|
| `--name <name>` | string | `default` | save under a non-default profile name |
| `--no-verify` | bool | false | skip the post-login `GET /Account/` validation (offline / mock use) |

Examples:
```bash
plivo login                     # default flow
plivo login --name staging      # alternate profile
```

After login: auth_id + email in `~/.plivo/config.toml`; auth_token in OS keychain (inline fallback for headless Linux).

## `plivo auth whoami`

Verify creds + show the account.

```bash
plivo auth whoami
```

## `plivo auth list`

List all configured profiles + show which is active.

## `plivo auth use <name>`

Switch the active profile. Used after `plivo login --name X` to flip default.

## `plivo logout [name]`

Delete a profile + remove its token from the keychain. With no arg → active profile.

# Core invariants (read once)

- **Output**: TTY → table, pipe → JSON. Force JSON anywhere with `-o json`.
- **Spend verbs require `--yes`** or refuse with exit 5 + `code: DESTRUCTIVE_REFUSED`. List: `messaging * send`, `voice calls make`, `voice calls speak`, `voice calls record`, `voice calls play`, `numbers buy`, `numbers release`, `account applications delete`, `agent run`.
- **Stable error envelope** on stderr: `{"error":{"code":..., "hint":..., "retryable":..., "status_code":...}}`. Switch on `code`, never message text.
- **Verify before inventing**: `plivo <cmd> --help` is the source of truth. The CLI evolves; don't assume from memory.
- **`--dry-run`** previews the exact HTTP request without sending. Works on every command.
- **`--explain`** narrates the action in plain English before running.

# Scripted / non-interactive use

```bash
export PLIVO_FEEDBACK_PROMPT=0    # silence the post-success "rate the CLI?" prompt
export CI=1                       # gates TTY-only nudges
plivo voice calls list -o json | jq ...
```

The post-success auto-prompt is already TTY-gated so most scripted runs are fine, but `PLIVO_FEEDBACK_PROMPT=0` is the bulletproof escape hatch when the wrapper detects a pseudo-TTY.

# Feedback

- **Manual:** `plivo feedback` → interactive rating (1-5) + optional comment.
- **Auto-prompt:** after a successful command on an interactive TTY, the CLI asks once per 24h: `💡 Got 30s to rate the CLI? [y/N]`. Snoozed 24h on `n` / Enter.

| Flag | Type | When |
|---|---|---|
| `--rating <1-5>` | int | non-interactive submit; combine with `--message` to skip prompts |
| `--message "..."` | string | comment (sanitised client-side) |
| `--no-context` | bool | strip OS/arch context from event (privacy-conscious users) |
| `--yes` | bool | skip preview screen |

Env opt-outs:
- `PLIVO_FEEDBACK_PROMPT=0` — silence auto-prompt, manual still works.
- `PLIVO_FEEDBACK_TELEMETRY=0` — disable all submission (manual becomes no-op).

# Numbers

## `plivo numbers list`

List rented numbers on the account.

| Flag | Type | When |
|---|---|---|
| `--type <local\|tollfree\|mobile\|fixed>` | string | filter by number type |
| `--starts-with <prefix>` | string | filter by E.164 prefix (e.g. `+1`) |
| `--limit <n>` | int | page size (default 20) |
| `--offset <n>` | int | pagination offset |

## `plivo numbers get <e164>`

Get one rented number.

## `plivo numbers search`

Search marketplace for buyable numbers.

| Flag | Type | When |
|---|---|---|
| `--country <ISO2>` | string | **required**; e.g. `US`, `IN`, `GB` |
| `--type <local\|tollfree\|mobile\|fixed>` | string | filter |
| `--pattern <regex>` | string | digit pattern in the number |
| `--limit <n>` | int | default 20 |

## `plivo numbers buy <e164>` (spend)

Rent a number from the marketplace.

| Flag | Type | When |
|---|---|---|
| `--yes` | bool | **required** to actually spend |
| `--dry-run` | bool | preview without spending |
| `--app-id <id>` | string | bind to an application at buy time |
| `--alias "..."` | string | set the human-readable label |

## `plivo numbers update <e164>`

Update metadata on a rented number.

| Flag | When |
|---|---|
| `--alias "..."` | new human-readable label |
| `--app-id <id>` | bind/rebind to an Application |
| `--subaccount <auth_id>` | move to a subaccount |

## `plivo numbers release <e164>` (spend-adjacent)

Release a rented number. **Requires `--yes`**.

## `plivo numbers cnam <e164>`

CNAM (caller-name) lookup for a US number.

## `plivo numbers compliance ...`

Phone-number compliance bundle CRUD (KYC docs, address proof, etc.). Sub-verbs: `requirements`, `create`, `get`, `list`, `update`, `delete`, `link`. Use `plivo numbers compliance --help` for full surface.

## `plivo numbers masking sessions ...`

Phone-number masking session CRUD.

# Messaging

Channel-split CLI: SMS / WhatsApp / MMS each have their own subgroup. Universal `plivo messaging get <uuid>` works across channels.

## `plivo messaging sms send` (spend)

Send an SMS. **Requires `--yes`**.

| Flag | Type | When |
|---|---|---|
| `--src <e164>` | string | **required**; sender (E.164, shortcode, or sender ID) |
| `--dst <e164[<e164]>` | string | **required**; one recipient or `<`-delimited multi |
| `--text "..."` | string | **required**; message body |
| `--url <url>` | string | delivery webhook |
| `--method <GET\|POST>` | string | webhook method (default POST) |
| `--type <sms\|mms>` | string | force type |
| `--powerpack <uuid>` | string | route through a powerpack |
| `--trackable` | bool | enable click tracking on links |
| `--log <true\|false>` | bool | server-side message-body logging |
| `--dry-run` | bool | preview without spending |

## `plivo messaging sms list`

List sent/received SMS.

| Flag | When |
|---|---|
| `--state <queued\|sent\|delivered\|failed\|...>` | filter by delivery state |
| `--direction <inbound\|outbound>` | filter by direction |
| `--src <e164>` / `--dst <e164>` | filter by leg |
| `--from-date <YYYY-MM-DD>` / `--to-date` | time window |
| `--limit <n>` / `--offset <n>` | pagination |

## `plivo messaging get <uuid>`

Get one message (any channel). Universal across SMS/WhatsApp/MMS.

## `plivo messaging sms diagnose <uuid>`

Walks a message lifecycle and explains failures in plain English.

## `plivo messaging sms 10dlc ...`

10DLC compliance: `brands`, `campaigns`, `links`. Sub-verbs: `list`, `get`, `create`, `update`.

## `plivo messaging sms powerpacks ...`

Powerpack (sender-pool) CRUD: `list`, `get`, `create`, `update`, `delete`, `numbers`.

## `plivo messaging sms tollfree ...`

Toll-free verification: `list`, `get`, `submit`.

## `plivo messaging whatsapp send` / `plivo messaging mms send`

Same shape as `messaging sms send`. WhatsApp adds template flags (`--template`, `--template-vars`). MMS adds `--urls`.

# Voice — calls

## `plivo voice calls make` (spend)

Place an outbound call. **Requires `--yes`**.

| Flag | Type | When |
|---|---|---|
| `--from <e164>` | string | **required**; caller |
| `--to <e164[<e164]>` | string | **required**; one or `<`-delimited multi recipients |
| `--answer-url <url>` | string | PlivoXML URL on answer |
| `--answer-method <GET\|POST>` | string | default POST |
| `--hangup-url <url>` | string | webhook on hangup |
| `--ring-url <url>` | string | webhook on ring |
| `--time-limit <sec>` | int | hard cap on call duration |
| `--caller-name "..."` | string | display name |
| `--machine-detection <true\|hangup\|none>` | string | answering-machine handling |
| `--record-call` | bool | record A-leg by default |

## `plivo voice calls list` / `get <uuid>`

List/get calls. List flags: `--status`, `--direction`, `--from-number`, `--to-number`, `--from-date`, `--to-date`, `--limit`, `--offset`.

## `plivo voice calls hangup <uuid>`

End an in-progress call.

## `plivo voice calls transfer <uuid>`

Transfer an active call.

| Flag | When |
|---|---|
| `--leg <aleg\|bleg\|both>` | which leg to transfer |
| `--aleg-url <url>` / `--bleg-url <url>` | PlivoXML for the new leg |

## `plivo voice calls play <uuid>` (spend) / `stop-play <uuid>`

Stream audio into a call. **Requires `--yes`**.

| Flag | When |
|---|---|
| `--urls <url[,url]>` | **required**; audio URL(s) |
| `--length <sec>` | max play length |
| `--loop` | bool; replay until hangup |
| `--mix <true\|false>` | mix vs replace current stream |

## `plivo voice calls speak <uuid>` (spend) / `stop-speak <uuid>`

TTS into a call. **Requires `--yes`**.

| Flag | When |
|---|---|
| `--text "..."` | **required**; text to speak |
| `--voice <name>` | TTS voice (e.g. `WOMAN`, `MAN`, AWS Polly voices) |
| `--language <code>` | e.g. `en-US`, `hi-IN` |

## `plivo voice calls dtmf <uuid>`

Send DTMF digits into a call.

| Flag | When |
|---|---|
| `--digits <0-9*#>` | **required** |
| `--leg <aleg\|bleg\|both>` | which leg |

## `plivo voice calls record <uuid>` (spend) / `stop-record <uuid>`

Record a call. **Requires `--yes`**.

| Flag | When |
|---|---|
| `--time-limit <sec>` | max record duration |
| `--file-format <mp3\|wav>` | audio container |
| `--transcription-url <url>` | webhook for transcript |
| `--transcription-method <GET\|POST>` | |

## `plivo voice calls diagnose <uuid>`

Lifecycle walkthrough + plain-English failure explanation.

## `plivo voice calls streams <verb>` — per-call AudioStream CRUD

Distinct from `voice streams` (the dev-loop group). Sub-verbs:

| Verb | When |
|---|---|
| `list <call_uuid>` | list streams attached to a call |
| `get <call_uuid> <stream_id>` | fetch one stream |
| `start <call_uuid>` | open a new stream — flags: `--url <wss>`, `--track <inbound\|outbound\|both>`, `--bidirectional`, `--audio-track`, `--content-type`, `--sample-rate` |
| `stop <call_uuid> [<stream_id>]` | close one or all streams on the call |

# Voice — streaming dev loop

Use these for local development of WebSocket-based audio streaming **without** a real call. Distinct from `voice calls streams` (which is REST CRUD on an existing call's stream).

## `plivo voice streams test`

Open a WebSocket to a URL, send Plivo-format start/media/stop frames with synthetic audio, report results. No call placed, no spend.

| Flag | Type | Default | When |
|---|---|---|---|
| `--to <ws-url>` | string | **required** | the WebSocket endpoint to test |
| `--duration <sec>` | int | 3 | seconds of synthetic audio (max 30) |
| `--codec <mulaw\|l16>` | string | `mulaw` | audio codec advertised |
| `--rate <hz>` | int | 8000 | sample rate (8000 for mulaw, 16000 typical for l16) |
| `--bidirectional` | bool | false | also read frames back (test bot→caller path) |
| `--insecure` | bool | false | skip TLS verification (self-signed dev certs) |

```bash
plivo voice streams test --to wss://my-bot.example.com/ws
plivo voice streams test --to ws://localhost:7860/ws --duration 5 --bidirectional
```

## `plivo voice streams forward`

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

# Voice — conferences / multiparty / endpoints / recordings

```bash
plivo voice conferences list / get / hangup / record / stop-record / member ...
plivo voice multiparty       list / get / create / end / participant ...
plivo voice endpoints        list / get / create / update / delete
plivo voice recordings       list / get / delete
```

`plivo voice multiparty create` requires `--name`. `plivo voice multiparty participant add` requires `--from` + `--to`. `plivo voice conferences member play` / `speak` require `--urls` / `--text`. Run `plivo voice <group> --help` for the rest.

# Account + applications

## `plivo account get` / `plivo account update`

Get/update account info.

## `plivo account subaccounts`

Subaccount CRUD: `list`, `get`, `create`, `update`, `delete`.

## `plivo account applications`

| Verb | Required flags | Notes |
|---|---|---|
| `list` | — | `--limit`, `--offset` for pagination |
| `get <uuid>` | — | |
| `create` | `--app-name` | optional: `--answer-url`, `--answer-method`, `--hangup-url`, `--message-url`, `--public-uri`, `--default-number-app` |
| `update <uuid>` | — | same flags as create; only supplied ones get patched |
| `delete <uuid>` | `--yes` | spend-adjacent; refuses without confirmation |

# Verify

```bash
plivo verify sessions create --recipient +1... --channel sms
plivo verify sessions get <uuid>
```

# Lookup

```bash
plivo lookup <e164>          # carrier + line-type
```

# Vibe AI agents

| Verb | Required | Notes |
|---|---|---|
| `agent list` | — | `--limit`, `--offset` |
| `agent get <uuid>` | — | |
| `agent create` | `--name` | optional: `--template <id>` |
| `agent update <uuid>` | — | patch metadata |
| `agent copy <uuid>` | — | duplicate from a template / existing agent |
| `agent generate` | `--prompt "..."` | Vibe SSE generation — streams reasoning + final spec |
| `agent publish <uuid>` | — | promote draft → live |
| `agent run <uuid>` (spend) | `--phone-number <e164>` + `--yes` | dials a test call to that number |
| `agent attach <uuid>` | `--number <e164>` | wire a real inbound number to this agent |
| `agent delete <uuid>` | `--yes` | |
| `agent token list / mint / revoke` | — | scoped tokens for embedding the agent in external apps |

# Conversational / debug

## `plivo ask "<message>"`

Ask Plivo's AI assistant — streams answer via SSE.

| Flag | When |
|---|---|
| `--call-uuid <uuid>` | include a call's context so the assistant can debug it specifically |
| `--verbose` | show tool calls + narration (debug) |
| `--debug-stream` | dump raw SSE frames to stderr |
| `--history @file.json` | seed prior conversation history |

## `plivo support`

Open a support chat session (interactive SSE).

## `plivo upgrade`

Self-update the CLI binary to the latest release.

# Error-envelope cheatsheet

| Code | Exit | Likely cause |
|---|---|---|
| `AUTH_MISSING` | 2 | no profile / env var creds — run `plivo login` |
| `AUTH_INVALID` | 2 | wrong auth_id/token — re-login |
| `BAD_INPUT` | 3 | flag value / shape problem |
| `DESTRUCTIVE_REFUSED` | 5 | spend verb without `--yes` |
| `RESOURCE_NOT_FOUND` | 4 | 404 from upstream |
| `RATE_LIMITED` | 4 | back off + retry (`retryable: true`) |
| `NETWORK_ERROR` | 6 | DNS / connection / TLS — `retryable: true` |
| `USER_ERROR` | 3 | misc client-side validation |
| `CLI_TOO_OLD` | 8 | server said upgrade — run `plivo upgrade` |

All envelopes have `hint` + `retryable` fields. Switch on `code`, never message text.

# JSON consumption patterns

```bash
# One field
plivo voice calls get <uuid> -o json | jq '.data.duration'

# Filter
plivo numbers list -o json | jq '.data[] | select(.type=="local")'

# Pipe across calls
APP_ID=$(plivo account applications list -o json | jq -r '.data[0].app_id')
plivo numbers update +1... --app-id "$APP_ID" -o json
```

# Sanity check

```bash
plivo --help && plivo auth whoami
```

License: MIT.
