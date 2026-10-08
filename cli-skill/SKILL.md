---
name: plivo-cli
description: Runs Plivo tasks with the plivo CLI instead of raw curl, covering SMS/MMS/WhatsApp, calls and call diagnosis, numbers, applications, Verify OTP, login and profiles. Use for any Plivo API request or plivo command, or when a Plivo auth_id or auth_token comes up. Not for answer-URL XML, WebSocket bots or SIP design (see plivo-voice-xml, plivo-audio-streaming, plivo-sip-trunking).
---

# plivo-cli

`plivo` is a single binary. Prefer it to curl: it authenticates from a saved login, previews with `--dry-run`, and returns a stable JSON envelope with typed errors. For an endpoint it does not wrap, use `plivo api <METHOD> <path>`.

`plivo <command> --help` is the source of truth for commands and flags: read it before running a command you have not used, and never guess a flag. `plivo docs search <keywords>` and `plivo docs show <url, path or title>` read the Plivo docs (API parameters, XML, error codes) from the terminal, with no login.

## Installation

Not on PATH: `brew install plivo/tap/plivo`, or `curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/main/install.sh | bash`, then `plivo --version`. This skill ships inside the binary: `plivo skill list` shows whether the installed copy is stale, and `plivo skill install` refreshes it.

## Rules

- **Auth.** Browser login (`plivo login`) is the only credential source: the CLI reads no `PLIVO_AUTH_ID` or `PLIVO_AUTH_TOKEN` and has no flag for them. Do not run `plivo login` yourself; if `plivo auth whoami` exits 2, ask the human to run it in their own terminal. Never print, echo, export or store an auth token, including one the user pastes. `--profile <name>` selects another saved profile.
- **Preview, then confirm.** Run any change with `--dry-run` first: it prints the request on stderr and exits 0 without sending a write. Spend and destructive commands then need `--yes`: run that as a separate command, only once the human has approved that spend or deletion. Other writes (creates, updates, live-call verbs such as `play`, `speak` and `transfer`) have no `--yes` gate and act as soon as they run.
  - Spend commands (`calls make`, `messaging * send`, `numbers buy|cnam`, `masking sessions create`, 10DLC `brands|campaigns create`, `verify sessions create`, `multiparty participant add`, mutating `plivo api` verbs) preview with `--dry-run` alone.
  - Destructive commands (`numbers release`, `calls hangup`, `conferences hangup`, `calls streams stop`, `powerpacks numbers remove`, and every `delete`, `kick` and `end`) refuse `--dry-run` alone with exit 5, although the hint suggests it. Preview them with `--yes --dry-run` (`--dry-run` wins). For `sip * delete`, run it without `--yes`: it names what it would detach, then refuses.
  - `plivo lookup` is billed per lookup and has no `--yes` gate: ask first.
  - `--dry-run` only holds back Plivo API writes. Commands that change local state or talk to something else ignore it and act for real: `login`, `logout`, `auth use`, `auth remove`, `config set`, `config telemetry`, `feedback` (submits), `upgrade` (installs; use `upgrade --check`) and `voice streams test` (connects to the WebSocket; no call). `skill install --dry-run` is the exception: it writes nothing.
- **Output.** Pass `-o json`. Reads and creates print `{"data": <API response verbatim>}` on stdout; list rows are at `data.objects`, paging at `data.meta`. Many writes (`numbers update|release`, `calls hangup|transfer`, `account applications update`, `multiparty participant add`) print nothing on stdout: check the exit code, read the result back with a `get`, and never retry a spend command because stdout was empty.
- **Errors** go to stderr as `{"error": {"code", "message", "hint", "retryable", "status_code"}}` with a non-zero exit. Switch on `code` (exit codes below), never on message text.
- **No prompts.** Never run bare `plivo feedback` or `plivo ask -i`, and pass `-y` to `voice streams forward`. `export PLIVO_FEEDBACK_PROMPT=0 CI=1 PLIVO_NO_UPDATE_CHECK=1` silences the rating prompt and the update hint.

## Top-level command map

```
account     applications | get | subaccounts | update
agents      list | get | create | update | publish | pause | resume | delete | nodes | runs
api         generic REST escape hatch (any api.plivo.com path)
ask         one-shot question to Plivo's AI assistant
auth        list | use | remove | whoami
config      get | set | telemetry
docs        list | search | show
feedback    rate the CLI
login       browser login (PKCE)
logout      remove a profile and its keychain token
lookup      carrier lookup for an E.164 number
messaging   get | sms | mms | whatsapp   (aliases: message, msg, sms)
numbers     buy | cnam | compliance | get | list | masking | release | search | update
open        console | calls | call <uuid> | sip-call <uuid> | numbers | docs [path]   (v1.2.0+)
sip         calls | credentials | ip-acl | trunks | uris
skill       install | list
support     past support escalations
upgrade     self-update
verify      sessions (create | get | list | validate)
voice       calls | conferences | endpoints | multiparty | recordings | streams
```

## Behaviour `--help` does not show

- `messaging` aliases replace the group name only: `plivo sms sms send` works, `plivo sms send` fails with `unknown flag`.
- Recipients: separate several with `<`, quoted (a bare `<` is a shell redirect): `--dst "+14155551111<+14155552222"`. Commas do not work. `numbers` commands take the number as digits without `+`.
- `messaging whatsapp send` sends free text, which WhatsApp allows only within 24 hours of the user's last message. To open a conversation, send a template with `plivo api POST /Message/` and a `template` body.
- `voice calls make`: omit `--machine-detection` to turn detection off; the `none` that help lists is rejected by the API.
- `voice calls streams start`: quote `--content-type "audio/x-mulaw;rate=8000"` (`;` ends a shell command). `--stream-status-callback` sends `stream_status_callback_url`, while the API documents `status_callback_url`: when the callback matters, start the stream with `plivo api POST /Call/<call_uuid>/Stream/`.
- `voice multiparty create` is retired and hidden: an MPC starts when its first participant is added, with `voice multiparty participant add <name> --from <number> --to <number>`.
- `voice streams forward` points the app's answer URL, and so every number on that app, at a tunnel while it runs, then restores it on exit. Use a dedicated test app. Preview with `--dry-run` (it shows the URL it would replace) and get approval. Then run it in the background with `-y -o table` and wait for its `Ready` line; in JSON mode it prints nothing, not even the tunnel URL, until it exits. Stop it with SIGINT or SIGTERM: SIGKILL skips the restore. It does not notice a dropped tunnel, so restart it.
- `account applications update` has no `--fallback-answer-url`: set it with `plivo api POST /Application/<app_id>/`. `account applications delete` also deletes the app's endpoints, with or without `--cascade`: the API cascades by default.
- `sip`: quote `--uri "host;transport=tcp"` (the help example does not). Passwords go only through `--password-stdin`; to preview a password change, pass `--username` too. Deleting a URI deletes the trunks that use it. Route a number to an inbound trunk with `numbers update <number> --trunk-id <trunk_id>`.
- `plivo api` reports every HTTP error as `UPSTREAM_ERROR` (exit 3) with the real status in `status_code`: switch on that. Its `--dry-run`, `--explain` and `--log-level debug` print the request body unredacted, so never send a secret through it. `/Message/` expands to `/v1/Account/<auth_id>/Message/`; a `/v1/...` path is used as-is.
- `plivo ask` and the `diagnose` commands share a small per-account rate limit: on `RATE_LIMITED`, wait as long as the message says.
- `docs show` prints a JSON envelope when piped: add `-o table`, or use `jq -r .data.body`. `docs search` rows are at `data`, not `data.objects`.

## Known issues

- In v1.1.0 to v1.1.2, `voice streams forward` cannot carry a live call: it rejects Plivo's stream (it checks the signature against the `wss://` URL while Plivo signs the `http://` one), and its XML has no `keepCallAlive`, so the call ends at once with hangup cause 4010 (End Of XML Instructions). Fixed in v1.1.3; check `plivo --version`.

## Workflows

Provision a number and attach an app:

```bash
plivo numbers search --country US --type local --limit 5 -o json
plivo numbers buy 14155550100 --dry-run     # show the human, then rerun with --yes once approved
plivo account applications create --app-name my-app --answer-url https://example.com/answer -o json
plivo numbers update 14155550100 --app-id <app_id>
plivo numbers get 14155550100 -o json | jq '.data.application'     # read it back
```

Debug a failed call:

```bash
plivo voice calls get <call_uuid> -o json | jq '.data | {hangup_cause_name, hangup_cause_code, hangup_source}'
plivo voice calls diagnose <call_uuid>     # AI walk-through of the call
plivo docs show voice/troubleshooting/hangup-causes -o table     # what a hangup code means
plivo docs show sip-trunking/troubleshooting/zentrunk-hangup-codes -o table     # the same, for SIP trunk calls
```

Use the table for the call's product: the two code spaces reuse numbers, so 4010 is End Of XML Instructions on Voice but `unauthorized_by_carrier` on Zentrunk.

Pick a sending number: `plivo numbers list --services sms -o json | jq -r '.data.objects[].number'`. Numbers are listed without a leading `+`.

## Exit codes

`1` user, flag, validation, not-found, conflict or account-policy error (`USER_ERROR`, `BAD_FLAG`, `BAD_INPUT`, `VALIDATION_ERROR`, `RESOURCE_NOT_FOUND`, `RESOURCE_CONFLICT`, `GEO_PERMISSION_DENIED`, `OUTBOUND_DISABLED`, `INSUFFICIENT_FUNDS`); typed commands also report a network failure as `USER_ERROR`, with a message starting `http:`. `2` auth (`AUTH_*`). `3` network or upstream, retryable (`NETWORK_ERROR`, `UPSTREAM_*`, `INTERNAL_ERROR`). `4` `RATE_LIMITED`: back off. `5` `DESTRUCTIVE_REFUSED`: needs `--yes`. `6` `CLI_TOO_OLD`: the human should upgrade (`plivo upgrade`, or `brew upgrade plivo`).

## Other Plivo skills

Each is bundled in the binary and installed separately, with no network: `plivo skill install audio-streaming | sip-trunking | voice-xml`.

- `plivo-audio-streaming`: take a voice bot (a test bot or your own WebSocket bot) to real calls with `<Stream>`, from setup to go-live.
- `plivo-sip-trunking`: connect LiveKit, ElevenLabs, Retell, Vapi or another SIP platform over SIP trunking.
- `plivo-voice-xml`: write and fix the XML an answer URL returns.
