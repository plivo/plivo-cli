---
name: plivo-cli
description: Runs Plivo tasks with the plivo CLI instead of raw curl, as a short conversation that asks what the user wants when the request is vague, previews every write and asks before it changes or spends anything; routes voice agents, XML and SIP to their own skills. Covers SMS/MMS/WhatsApp, calls and call diagnosis, numbers, applications, Verify OTP, login and profiles. Use for any Plivo API request or plivo command, or when a Plivo auth_id or auth_token comes up. Not for answer-URL XML, WebSocket bots or SIP design (see plivo-voice-xml, plivo-audio-streaming, plivo-sip-trunking).
---

# plivo-cli

`plivo` is a single binary. Prefer it to curl: it authenticates from a saved login, previews with `--dry-run`, and returns a stable JSON envelope with typed errors. For an endpoint it does not wrap, use `plivo api <METHOD> <path>`.

`plivo <command> --help` is the source of truth for commands and flags: read it before running a command you have not used, and never guess a flag. To survey many commands (v1.2.0+), `plivo --map -o json` prints every command with its arguments and flags (large: narrow it with `--query`) and `plivo <command> --schema` prints one command's arguments, flags and output fields: use them instead of repeated `--help`. `plivo docs search <keywords>` and `plivo docs show <url, path or title>` read the Plivo docs (API parameters, XML, error codes) from the terminal, with no login.

## Installation

Not on PATH: `brew install plivo/tap/plivo`, or `curl -fsSL https://raw.githubusercontent.com/plivo/plivo-cli/main/install.sh | bash`, then `plivo --version`. This skill ships inside the binary: `plivo skill list` shows whether the installed copy is stale, and `plivo skill update` (v1.2.0+) or `plivo skill install` refreshes it.

## Rules

- **Auth.** Browser login (`plivo login`) is the only credential source: the CLI reads no `PLIVO_AUTH_ID` or `PLIVO_AUTH_TOKEN` and has no flag for them. Do not run `plivo login` yourself; if `plivo auth whoami` exits 2, ask the human to run it in their own terminal. Never print, echo, export or store an auth token, including one the user pastes. `--profile <name>` selects another saved profile.
- **Preview, then confirm.** Run any change with `--dry-run` first: it prints the request on stderr and exits 0 without sending a write. Spend and destructive commands then need `--yes`: run that as a separate command, only once the human has approved that spend or deletion. Other writes (creates, updates, live-call verbs such as `play`, `speak` and `transfer`) have no `--yes` gate and act as soon as they run.
  - Spend commands (`calls make`, `messaging * send`, `numbers buy|cnam`, `masking sessions create`, 10DLC `brands|campaigns create`, `verify sessions create`, `multiparty participant add`, mutating `plivo api` verbs) preview with `--dry-run` alone.
  - Destructive commands (`numbers release`, `calls hangup`, `conferences hangup`, `calls streams stop`, `powerpacks numbers remove`, and every `delete`, `kick` and `end`) preview with `--dry-run` alone from v1.2.1. Older versions refuse that with exit 5, although the hint suggests it; preview them there with `--yes --dry-run` (`--dry-run` wins). For `sip * delete`, run it without `--yes`: it names what it would detach, then refuses.
  - `plivo lookup` is billed per lookup and has no `--yes` gate: ask first.
  - `--dry-run` only holds back Plivo API writes. Commands that change local state or talk to something else ignore it and act for real: `login`, `logout`, `auth use`, `auth remove`, `config set`, `config telemetry`, `upgrade` (installs; use `upgrade --check`) and `voice streams test` (connects to the WebSocket; no call); before v1.2.0 also `feedback` (submits). `skill install --dry-run` is the exception, and from v1.2.0 so are `skill update` and `feedback`: they write and send nothing.
- **Output.** Pass `-o json`. Reads and creates print `{"data": <API response verbatim>}` on stdout; list rows are at `data.objects`, paging at `data.meta`. From v1.2.0, `-o jsonl|yaml|csv` and `--query '<JMESPath>'` (over the whole envelope, e.g. `--query 'data.objects[].number'`; implies `-o json`) reshape it; `plivo api` keeps its own `--query key=value`, which adds URL parameters. From v1.2.1 every write prints a result too: the API's response or, where the API sends none (deletes, hangups, releases, stops, kicks), the ID and what was done, e.g. `{"data": {"call_uuid": "<id>", "hung_up": true}}`. Older versions print nothing on stdout for many writes (`numbers update|release`, `calls hangup|transfer`, `account applications update`, `multiparty participant add`): check the exit code, read the result back with a `get`, and never retry a spend command because stdout was empty.
- **Lists.** A page holds at most 20 rows. From v1.2.0: `--limit` is 1-20 (more is refused); `--all` reads every page (not with `--offset`) but stops after 100 pages (2,000 rows at `--limit 20`) and sets `data.meta.truncated: true`, so check that before treating a list as complete; an unknown filter value (`--direction`, `--status`, `--type`) is refused before any request. From v1.2.1, `data.meta.next` is null once every row is read, and a page still rate limited after a minute of retries ends `--all` early: the rows read so far print with `truncated: true` and the exit code is 4. From v1.2.0, `voice calls list`, `voice recordings list` and `messaging sms|mms|whatsapp list` take `--since`/`--until` like `sip calls list` (UTC; `--from-date`/`--to-date` are the same flags; `--until` needs `--since`, and from v1.2.1 must be after it); voice call and message ranges start within the last 90 days, and voice call and recording ranges span at most 31.
- **Errors** go to stderr as `{"error": {"code", "message", "hint", "retryable", "status_code"}}` with a non-zero exit. Switch on `code` (exit codes below), never on message text.
- **No prompts.** Never run bare `plivo feedback` or `plivo ask -i`, and pass `-y` to `voice streams forward`. To report a CLI bug (v1.2.0+), `plivo feedback --bug --dry-run` prints the report (a comment plus the last failed command's path and codes, never its arguments) and sends nothing; add `--yes` only once the human has seen it. `export PLIVO_FEEDBACK_PROMPT=0 CI=1 PLIVO_NO_UPDATE_CHECK=1` silences the rating prompt and the update hint.

## Talking to the user

The user may not know the CLI. Run the commands yourself and talk about what they change, in plain words.

- Ask every question with your agent's structured question tool. In Claude Code that is AskUserQuestion: 1 to 4 questions per call, 2 to 4 options each, the recommended option first with "(recommended)" after it, and no "Other" option (the tool adds one). Without such a tool, ask a numbered list with an "Other" choice and wait.
- Keep CLI words out of questions and reports: dry-run, E.164, answer URL, WebSocket, envelope, exit code, `auth_id`. Commands and their previews stay as they are.
- Say "voice agent", not "bot". Name apps by their names, not their IDs, in anything the user reads.
- Mask phone numbers as `+91 AB CDXX XEFG` (country code, first four digits, X for the next three, last three) and the account ID as its first four and last three characters with `…` between. Show a number in full only where the user must dial it, and inside commands.
- Show prices only for a number the user rents (`setup_rate`, `monthly_rental_rate` from `numbers search`), never for calls, messages or the balance. The API gives no currency: add `₹` only when you know the account bills in rupees.
- Keep each question to one or two short sentences: what happens, and what it changes or costs.
- For work with three or more steps, track the steps in your agent's task or todo tool (TaskCreate and TaskUpdate in Claude Code, the plan tool in Codex CLI) and mark a step done only when its check passes.
- Report what you observed in this session, not what you assume. An HTTP 2xx, a `request_uuid` or an empty stdout is not success: read the result back with a `get`. A call or message worked only when its record says so, and for a call, when a person heard it.

### Every write, in three moves

1. **Preview.** Run the command with `--dry-run`. If it rejects `--dry-run`, show the current state from a `get` or `list` instead.
2. **Ask.** One question, in plain words, saying what changes and what it costs. For example (header Buy number): "Buy <masked number> for <setup price> now and <monthly price> a month? You can release it anytime." · Buy it · Not now. For a number move (header Link): "Send calls on <masked number> to <app name>? They stop going to <old app name> until you switch back." · Yes, switch it · Keep it as it is.
3. **Run it**, with `--yes` where the command needs it, then read the result back.

Before anything that spends money (renting a number, a call, a message, a lookup, a Verify OTP), check the credit: `cash_credits` in `plivo auth whoami -o json`. At 0 or less, send the user to <https://cx.plivo.com/home> to add credits and stop the spend. Before you move a number to another app or trunk, record its current `application` from `plivo numbers get <number> -o json`; that is the rollback.

## Start here: what does the user want?

When the request already says what to do ("send an SMS to …", "why did call <uuid> fail?"), skip the intake and go to its row below. When it is vague ("help me with Plivo", "set up my account"), first run `plivo auth whoami -o json` (exit 2: ask the user to run `plivo login`, see Rules), then ask (header Task): "What do you want to do with Plivo?"

- Calls: "Make a call, or find out why one failed."
- Messages: "Send or check SMS, MMS or WhatsApp."
- Numbers: "Find, buy or connect a phone number."
- Account: "Balance, apps, subaccounts or logins."

If the user names a voice agent, an IVR or a SIP platform in "Other", route to the related skill (last row).

| The user wants | Ask next (plain text unless noted) | Run |
|---|---|---|
| Why a call failed | "Which call? Give me its ID, or the number and roughly when." | `voice calls list --to <number> --since <date> -o json` to find it, then `voice calls get <call_uuid> -o json`. Then ask (header Debug) "Want Plivo's call debugger to explain it? It takes about a minute." · Yes (recommended) · No, the record is enough. On yes, `voice calls diagnose <call_uuid>`, and retell its answer in plain words. |
| Make a call | Which number to call from (header Number, up to two of the account's voice numbers, masked) and the phone to ring; what the call should do (an answer URL, or the voice-xml skill to write one) | Credit check, then preview `voice calls make … --answer-method <method> --dry-run`, ask (header Call) "Call <masked phone> now?", run with `--yes`, then `voice calls get <request_uuid> -o json`. |
| Send a message | Which number to send from (header From, from `numbers list --services sms`), the recipient, the text; for WhatsApp, whether the recipient wrote in the last 24 hours | Credit check, preview `messaging <sms\|mms\|whatsapp> send … --dry-run`, ask (header Send) "Send this to <masked number>?", run with `--yes`, then `messaging get <message_uuid> -o json` for its status. |
| A number | Country (header Country), local or toll-free, and what it is for: calls, SMS or both | `numbers search`, then the Buy number question above with up to three numbers as options. India: KYC first (`numbers compliance list --country IN --status accepted`). Linking: the Link question above, then `numbers update <number> --app-id <app_id>`. |
| Account | Nothing more for a read | `auth whoami`, `account get`, `account applications list`, `account subaccounts list`. Report the balance as "you have credit" or "your balance is low", without the amount. |
| A voice agent, an IVR or call XML, a SIP platform | Nothing; hand over | `plivo skill list`. If the matching skill is installed, follow it; if not, ask (header Install) "Install the Plivo <name> skill? It walks you through this step by step. Free, no network." · Install it (recommended) · Just use the CLI. Then `plivo skill install audio-streaming`, `voice-xml` or `sip-trunking`. |

When a step fails, retell the error in plain words ("Plivo says the number isn't on this account"), name the fix, and ask before you try it. Switch on the error `code`, never the message text.

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
sip         calls | connect | credentials | ip-acl | test | trunks | uris   (connect, test: v1.2.0+)
skill       install [--project] | list | update   (update, --project: v1.2.0+)
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
- `voice streams forward` points the app's answer URL, and so every number on that app, at a tunnel while it runs, then restores it on exit. Use a dedicated test app. Preview with `--dry-run` (it shows the URL it would replace) and get approval. Then run it in the background with `-y -o table` and wait for its `Ready` line; in JSON mode it prints nothing, not even the tunnel URL, until it exits. Stop it with SIGINT or SIGTERM: SIGKILL skips the restore. From v1.2.1, when the tunnel process exits it restores the URL and exits 3 (`NETWORK_ERROR`); before that it does not notice, so restart it.
- `account applications update` has no `--fallback-answer-url`: set it with `plivo api POST /Application/<app_id>/`. `account applications delete` also deletes the app's endpoints, with or without `--cascade`: the API cascades by default.
- `sip`: quote `--uri "host;transport=tcp"`. Passwords go only through `--password-stdin`; to preview a password change, pass `--username` too. Deleting a URI deletes the trunks that use it. Route a number to an inbound trunk with `numbers update <number> --trunk-id <trunk_id>`.
- `plivo api` reports every HTTP error as `UPSTREAM_ERROR` (exit 3) with the real status in `status_code`: switch on that. Its `--dry-run`, `--explain` and `--log-level debug` print the request body unredacted, so never send a secret through it. `/Message/` expands to `/v1/Account/<auth_id>/Message/`; a `/v1/...` path is used as-is.
- `plivo ask` and the `diagnose` commands share a small per-account rate limit: on `RATE_LIMITED`, wait as long as the message says. From v1.2.0, `diagnose -o json` prints one result with fixed fields under `data` (`what_happened`, `likely_cause`, `timeline`, `next_steps`, `hangup_cause_code`, `hangup_source`, `confidence`, `answer`) and `-o jsonl` the event stream that `-o json` printed before; an analysis that fails or stops early exits 3. From v1.2.1, `--timeout <seconds>` bounds their stream (exit 3, `UPSTREAM_TIMEOUT`; without it there is no limit), and an `ask` answer cut off before its end exits 3.
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

`1` user, flag, validation, not-found, conflict or account-policy error (`USER_ERROR`, `BAD_FLAG`, `BAD_INPUT`, `VALIDATION_ERROR`, `RESOURCE_NOT_FOUND`, `RESOURCE_CONFLICT`, `GEO_PERMISSION_DENIED`, `OUTBOUND_DISABLED`, `INSUFFICIENT_FUNDS`); typed commands also report a network failure as `USER_ERROR`, with a message starting `http:`. `2` auth (`AUTH_*`). `3` network or upstream, retryable (`NETWORK_ERROR`, `UPSTREAM_*`, `INTERNAL_ERROR`). `4` `RATE_LIMITED`: back off. `5` `DESTRUCTIVE_REFUSED`: refused without confirmation: add the flag the hint names (`--yes`, `--force`, or both; `--force` is v1.2.0+). `6` `CLI_TOO_OLD`: the human should upgrade (`plivo upgrade`, or `brew upgrade plivo`).

## Other Plivo skills

Each is bundled in the binary and installed separately, with no network: `plivo skill install audio-streaming | sip-trunking | voice-xml`.

- `plivo-audio-streaming`: take a voice bot (a test bot or your own WebSocket bot) to real calls with `<Stream>`, from setup to go-live.
- `plivo-sip-trunking`: connect LiveKit, ElevenLabs, Retell, Vapi or another SIP platform over SIP trunking.
- `plivo-voice-xml`: write and fix the XML an answer URL returns.
