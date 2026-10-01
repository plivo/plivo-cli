---
name: plivo-first-agent
description: "Guides a new user from nothing to a first Plivo AI voice agent on a real call, covering CLI login, a number, an application, an echo bot, then an OpenAI bot over a tunnel. Use when someone wants to build or try a first Plivo voice agent or bot, or set up Plivo end to end. Not for an existing bot or production (plivo-audio-streaming) or SIP platforms (plivo-sip-trunking)."
license: Apache-2.0
---

# Plivo: your first voice agent

You take the user from nothing to a live call with an AI voice agent. The test call reaches a Plivo number (the user calls it, or Plivo calls the user), and Plivo streams the audio to a bot on the user's machine through a tunnel. The run has two bot stages:

1. **Echo bot.** It needs no API key. The caller hears their own voice. This proves the number, the application, the tunnel and the audio in both directions.
2. **OpenAI bot,** from the Pipecat example that the Plivo docs use. It needs the user's own AI keys.

Use the `plivo` CLI for every Plivo step and read values with `-o json`. `plivo <command> --help` decides which commands and flags exist. Where this file describes behavior that differs from the help text, it describes tested behavior: follow this file.

## Done means all of this, with evidence

1. Application `my-first-agent` exists, and the number is attached to it (a number the user already had: only if they call in).
2. **Echo call:** a real call reached the echo bot, and the user confirms they heard their own voice.
3. **OpenAI call:** a real call reached the OpenAI bot, and the user confirms they had a conversation. Skip this item only if the user has no AI key, and say so.
4. **Call records:** for each call, `plivo voice calls get <call_uuid> -o json` shows `call_duration` of 10 seconds or more and a `hangup_cause_name` of `Normal Hangup` or `End Of XML Instructions`.
5. **Resting state:** at the end, the application's answer URL and method are the resting values: the values recorded in step 5, or the deployed URL from step 8. The number is where the user chose to leave it (step 9).
6. **Report to the user:**
   - the number and the application id;
   - what runs on their machine, or where it is deployed;
   - how to start it again;
   - what each paid step cost;
   - how to deploy it, if they did not.

Do not report "done" for an item that you did not see in a command output or hear confirmed by the user.

## Track the run in your task list

At the start, create these nine tasks in your agent's own task or todo tool, and mark each one done only when its check passes. Each agent names the tool differently: for example TaskCreate and TaskUpdate in Claude Code (TodoWrite in older versions), and the plan tool in Codex CLI. Use the one your agent has.

1. Check tools and login
2. Ask the setup questions
3. Test the echo bot (no phone)
4. Get a number
5. Create or reuse my-first-agent
6. Echo call
7. OpenAI bot and call
8. Offer a deploy
9. Leave a safe resting state

If your agent has no task tool, print this list, and print it again with ticks after each task. In Claude Code the task tools can be off by default; if they are missing, tell the user that starting Claude Code with `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` turns them on, and do not change their settings yourself.

## Ask with your question tool

Ask every question with your agent's own structured question tool, if it has one: the setup questions in one call, and one yes/no question for each step that costs money or changes the account, after you show its preview. Each agent names the tool differently. In Claude Code it is AskUserQuestion: one call takes 1 to 4 questions, each with 2 to 4 options, and it adds its own free-text "Other" option, so do not add one. If your agent has no such tool, ask the same questions as a numbered list with an "Other" choice, and wait for the answer.

Setup questions (task 2). Before you ask, look for a previous run: do the step 5 lookup and list the numbers on `my-first-agent` as step 5 does. If a number is on it, add "Use +<number> from the last run" as the first Number option and mark it recommended in place of "Rent a new number", so a second run does not rent a second number.

| Header | Question | Options |
|---|---|---|
| Country | Which country should the number be in? | US (recommended) · Canada · India |
| Number | Do you want to rent a new number? | Rent a new number (recommended) · Use a number I have |
| AI keys | Which AI keys do you have for the second bot? | None yet (echo bot only) · OpenAI only · OpenAI, Deepgram and Cartesia |
| Test call | How do you want to make the test calls? | I call the number (recommended) · Plivo calls my phone |

- **India:** Plivo expects KYC at signup, so assume an India data-region organization with an accepted KYC application, and check it: `plivo numbers compliance list --country IN --number-type local --status accepted -o json`. If that lists no application, stop and explain that KYC comes first (`plivo skill install audio-streaming`, India section). Otherwise continue, with the India notes in steps 4 and 6.
- **Another country (from "Other"):** the Compliance API covers India only, so it cannot tell you what another country needs. Use the search result in step 4 instead: rent only a number whose `restriction` is null. If every result has a `restriction`, stop and explain its `restriction_text` (for example, an address proof).
- **Money:** ask about each paid step separately with your question tool, after you show its preview. There are two: renting a number, and each call. Never pass `--yes` unless the user said yes to that exact step.

## Rules for every change to the account

- **Preview first, in its own command.** Run the preview with `--dry-run`, show the output to the user, and ask. Then run the real command. Never run the preview and the write in one shell command.
- **Some writes have no `--yes` gate.** `account applications create`, `account applications update` and `numbers update` write as soon as they run without `--dry-run`. The preview is your only safety check.
- **Record every value before you change it.** For example, read the number's current `application` first; that is the rollback value.
- **Never leave the number on a URL that does not answer.** The resting answer URL for a new application is Plivo's demo document. It returns valid XML on GET, so a caller hears a short demo message, not an error.
- **Check the answer URL before every `forward` start.** Before you start `streams forward`, check that the application's `answer_url` and `answer_method` equal the recorded resting values. `forward` saves whatever it finds and restores that on exit. So a dead tunnel URL left behind by a killed run would be saved and restored again.
- **Run a background `forward` with `-o table`.** When stdout is not a terminal, `forward` defaults to JSON output. In JSON mode it prints nothing until it exits, not even its `✓ Ready.` line.
- **Keys:**
  - Never read, print or ask for API keys in the chat. The user writes them into `.env` with an editor.
  - Never run `plivo login` yourself; it needs a browser. Ask the user to run it; in Claude Code they type `! plivo login`.
- **Writes can print nothing on stdout,** even with `-o json`. Check the exit code, then read the result back with a `get` command.
- **Number formats:**
  - Pass numbers to `numbers` commands as digits, exactly as `numbers search` or `numbers list` returns them (for example `14155551234`).
  - `forward --number` only prints the number, so write it in E.164 form (`+14155551234`).

## Step 1: check tools and login

```bash
plivo --version                  # if missing: brew install plivo/tap/plivo, or the install.sh one-liner from the Plivo docs
plivo auth whoami -o json        # exit 2 with AUTH_MISSING: ask the user to run `plivo login`, then run this again
uv --version                     # the bots run with uv; if missing, ask the user to install it (https://docs.astral.sh/uv/)
ssh -V                           # `forward` uses ngrok when it is installed, otherwise localhost.run over ssh
```

Check: `plivo --version` is v1.1.3 or later, `whoami` shows the account the user expects, and `cash_credits` is above 0.

- **Version gate:** rent no number and place no call until this check passes. Earlier releases cannot carry the call (v1.1.0 to v1.1.2 end it at once with 4010). `plivo upgrade --check` reports whether a newer release exists; ask, then run `plivo upgrade` (Homebrew installs: `brew upgrade plivo`). A dev build (`-dev`, as in `0.1.0-dev`, or `vX.Y.Z-N-g<sha>`) is unknown: ask the user, or treat it as unsupported.
- **ngrok without an authtoken:** if ngrok is installed but has no authtoken, `forward` fails. Run it again with `--tunnel localhost.run`.
- **Trial accounts:** the docs require a verified sandbox number to make calls from a trial account, and they say nothing about inbound calls. If a call fails on a trial account, ask the user to verify the phone they call from in the Plivo console.

## Step 2: ask the setup questions

Use the table above.

## Step 3: test the echo bot, no phone

Create `echo_bot.py` in the user's working directory:

```python
# /// script
# requires-python = ">=3.11"
# dependencies = ["websockets>=14"]
# ///
import asyncio
import json

import websockets


async def echo(ws):
    try:
        async for raw in ws:
            msg = json.loads(raw)
            if msg.get("event") == "media":
                await ws.send(json.dumps({
                    "event": "playAudio",
                    "media": {
                        "contentType": "audio/x-mulaw",
                        "sampleRate": 8000,
                        "payload": msg["media"]["payload"],
                    },
                }))
    except websockets.ConnectionClosed:
        pass


async def main():
    async with websockets.serve(echo, "127.0.0.1", 8765):
        print("echo bot on ws://127.0.0.1:8765/ws", flush=True)
        await asyncio.Future()


asyncio.run(main())
```

Start it in the background with `uv run --script echo_bot.py`, and keep it running. Then run:

```bash
plivo voice streams test --to ws://127.0.0.1:8765/ws --bidirectional --duration 3 -o json
```

Check: `frames_read_back` is above 0. If it is 0, the bot is not running or listens on another port: read its output and start it again. This step places no call and changes nothing on the account.

## Step 4: get a number

To use a number the user already has:

1. List the numbers:

   ```bash
   plivo numbers list --services voice -o json          # pick one from data.objects[].number
   ```

2. Attach it to `my-first-agent` only if the user calls in: a Plivo call (`voice calls make`) carries its own answer URL.
3. With the step 5 attach preview, warn the user: `numbers update --app-id` moves the number's whole application link, so its inbound calls and messages go to `my-first-agent` until you restore it. Get an explicit yes.

To rent a new number:

```bash
plivo numbers search --country <ISO code> --type local --limit 5 -o json
plivo numbers buy <number> --dry-run
```

Pick one whose `voice_enabled` is true and whose `restriction` is null. Show the number, `setup_rate`, `monthly_rental_rate` and `voice_rate` from the search result. Ask the user, then run `plivo numbers buy <number> --yes`.

**India:** search with `--country IN --type local` and pick a landline number (a city code such as 022 or 080); landline numbers are for service and transactional calls. The accepted KYC application links to the number at purchase. If a number has a `restriction`, show its `restriction_text` and ask before you rent it. If the search is empty although the KYC check passed, stop and show both outputs to the user. If `buy` fails with `compliance_application_id is required`, follow the audio-streaming skill's India section.

Either way, read the number and record its current `application`. This is the rollback value:

```bash
plivo numbers get <number> -o json
```

If it ends in `/Zentrunk/Trunk/<id>/`, the number is on a SIP trunk, and the rollback is `--trunk-id <id>`, not `--app-id`.

## Step 5: create or reuse my-first-agent

The API matches `app_name` by prefix, so check for the exact name yourself:

```bash
plivo api GET /Application/ --query app_name=my-first-agent -o json
```

**If an object in `data.objects` has `app_name` equal to `my-first-agent`, reuse it:**

1. Read it with `plivo account applications get <app_id> -o json`.
2. Record its `answer_url` and `answer_method` as this run's **resting values**.
3. Check that the recorded URL answers. Send `curl -s -i` with the recorded method and look for status 200 and a body that starts with `<Response` or `<?xml`. If it does not answer, set the demo resting URL (the step 9 update command, after the user agrees) and record that instead.
4. List the numbers on the app. The API refuses to filter numbers by application (HTTP 400), so read every page and keep the objects whose `application` ends with `/Application/<app_id>/`:

   ```bash
   plivo numbers list -o json        # 20 per page; while data.meta.next is set, run it again with --offset 20, 40, ...
   ```

   `forward` redirects every number on the app. If numbers other than the user's are attached, ask before you continue.

**If no object matches, create the application with the demo resting URL.** Preview it first, and run it again without `--dry-run` after the user agrees:

```bash
plivo account applications create --app-name my-first-agent \
  --answer-url https://s3.amazonaws.com/static.plivo.com/answer.xml --answer-method GET --dry-run
```

Read `app_id` from the output, and record the demo URL and `GET` as the resting values.

- The name uses only lowercase letters and hyphens, because the Applications API allows only letters, digits, hyphens and underscores.
- The demo URL answers GET only, so keep `--answer-method GET`.

Attach the number, unless step 4 rules it out. Preview first, then run it again without `--dry-run`:

```bash
plivo numbers update <number> --app-id <app_id> --dry-run
plivo numbers get <number> -o json      # after the update: application ends with /Application/<app_id>/
```

## Step 6: echo call

The echo bot from step 3 must still be running. `streams forward` does four things:

1. It points the application at a tunnel to this machine.
2. It serves the Stream XML itself.
3. It checks Plivo's signature at the tunnel, then connects to the bot.
4. It restores the answer URL and method it found when it stops.

Check the resting values first (see the rules), then preview:

```bash
plivo account applications get <app_id> -o json      # answer_url and answer_method equal the recorded resting values
plivo voice streams forward --number +<number> --app <app_id> --to ws://127.0.0.1:8765/ws --dry-run
```

Show the preview and ask. Then run the same command with `-o table --yes` in place of `--dry-run`, in the background. Without `--yes`, `forward` asks for confirmation, and that prompt fails when no one can type an answer. Watch its output:

- `✓ Ready. Dial …`: the tunnel is up. Place the test call the way the user chose (below). They speak for at least 10 seconds and should hear their own voice.
- `rejected: bad or missing Plivo signature`: the call came from a different account or subaccount than the CLI profile. Run `forward` under the profile that owns the number. If it shows on `/ws` for every call, the CLI is older than v1.1.3 (see step 1).
- `dial customer WS … failed`: the bot is not running or listens on another port.

**The user calls in:** ask them to call the number.

**Plivo calls the user:**

1. Ask for the phone number to call, in E.164 form. On a trial account it must be a verified number.
2. Read the tunnel answer URL that `forward` set: `answer_url` in `plivo account applications get <app_id> -o json` (it ends in `/answer`).
3. Preview the call, and show its price from `plivo api GET /Pricing/ --query country_iso=<ISO code of the phone> -o json`: take the rate of the longest `prefix` in `voice.outbound.rates[]` that matches the phone number (US +1907 costs more than +1415):

   ```bash
   plivo voice calls make --from +<number> --to <phone> --answer-url <answer_url> --answer-method POST --dry-run
   ```

4. Ask, then run it again with `--yes` in place of `--dry-run`.
5. A 403 `Calls to this destination region are barred` means the account's geo permissions block that country. Professional (pay-as-you-go) accounts can allow only the US and India, in the console under Voice, Geo Permissions; other countries need an Enterprise plan. Offer that, or the user calls in.
6. `calls make` sets no time limit. If the call is still up after about 2 minutes, find it: `plivo api GET /Call/ --query status=live -o json` lists only the UUIDs of every live call on the account, so read each with `plivo api GET /Call/<uuid>/ --query status=live -o json` and keep the one whose `to` is the phone and `from` is the number. Preview `plivo voice calls hangup <call_uuid> --yes --dry-run`, ask, then run it without `--dry-run`.

**India:** the test call must use an Indian phone in either direction, because India calls must stay India to India. The docs also require the server to be in India, and they do not say whether a stream to a laptop behind a tunnel counts. If the call ends with 2070 `Violates Media Anchoring`, the tunnel did not count: do not place another tunnel call in step 7. With an AI key, run step 8's one-host path on a server in India. Without one, go to step 9 and report item 2 as not met, with this reason.

Before the call, list the calls once and note their UUIDs: calls to the number (`--direction inbound`) when the user calls in, calls to their phone (`--direction outbound`) when Plivo calls them. After the call, list them again and take the new call UUID. The list shows completed calls, so if it is not there yet, wait a few seconds and list again. If more than one call is new, ask the user: for a call in, match `from_number` to their phone; for a Plivo call, take the one that started when you placed it:

```bash
plivo voice calls list --to <number or phone> --direction <inbound or outbound> --limit 5 -o json
plivo voice calls get <call_uuid> -o json        # call_duration, hangup_cause_name, hangup_source
```

## Step 7: OpenAI bot and call

Skip this step if the user has no AI key, and say that item 3 of the definition of done is not met.

```bash
{ [ -d pipecat-examples ] || git clone --depth 1 https://github.com/pipecat-ai/pipecat-examples.git; } &&
  cd pipecat-examples/plivo-chatbot/inbound && uv sync &&
  { [ -f .env ] || cp env.example .env; }      # safe to re-run: keeps the clone and a filled .env
```

Ask the user to edit `.env` in their editor:

- **All three keys:** set `OPENAI_API_KEY`, `DEEPGRAM_API_KEY` and `CARTESIA_API_KEY`, and run the example as it is.
- **OpenAI key only:** set `OPENAI_API_KEY`. Then change `bot.py` to use one speech-to-speech service in place of the separate speech-to-text, language model and text-to-speech services:
  - import `OpenAIRealtimeLLMService` from `pipecat.services.openai.realtime.llm`;
  - build the pipeline as `transport.input()`, the user aggregator, the realtime service, `transport.output()` and the assistant aggregator;
  - take the settings from Pipecat's own `examples/realtime/realtime-openai.py` for the Pipecat version that `uv sync` installed. Do not write them from memory, because they change between versions.
- **Plivo credentials:** the example also reads `PLIVO_AUTH_ID` and `PLIVO_AUTH_TOKEN` for its Plivo serializer. The user copies them from the Plivo console. The CLI keeps its token in the operating system's keychain; never read it from there.

Stop the echo stage:

1. Send Ctrl-C or `kill -TERM <pid>` to the echo `forward` process.
2. Confirm the restore with `plivo account applications get <app_id> -o json`.
3. Stop the echo bot.

Then start the OpenAI bot in the background, unbuffered so its errors show in its output, and test it without a phone:

```bash
PYTHONUNBUFFERED=1 uv run server.py    # port 7860; serves the answer XML on GET / and the bot on /ws
plivo voice streams test --to ws://127.0.0.1:7860/ws --bidirectional --duration 10 -o json
```

The first connection loads Pipecat, so it can be slow. If `frames_read_back` is 0, read the server output, fix the error it shows (a missing key is common), and run the test once more. After any change to `.env` or the bot code, restart the server before you re-test: a running server keeps the env and code it loaded at start. To see which keys are set without printing their values, run `awk -F= '/^[A-Z_]+=/ {print $1, (length($2) ? "set" : "EMPTY")}' .env`.

Check the resting values again, then point `forward` at the bot. Preview first, then run it with `-o table --yes` in the background after the user agrees:

```bash
plivo voice streams forward --number +<number> --app <app_id> --to ws://127.0.0.1:7860/ws --dry-run
```

Place the test call as in step 6, the way the user chose, and ask them to talk to the bot. Check the call record the same way.

## Step 8: offer a deploy

Stop `forward` first with Ctrl-C or `kill -TERM <pid>`, and confirm the restore with `plivo account applications get <app_id> -o json`.

The agent works only while this machine, the bot and `forward` run. To keep it live, the answer URL and the bot need a public HTTPS host. Ask before you deploy anything, because hosting costs money. If the user says no, go to step 9. The example's `Dockerfile` builds only `bot.py`, for Pipecat Cloud. It does not include `server.py`, which serves the answer XML.

**Before you expose the bot:** the example server's `/ws` has no Plivo signature check, so anyone who finds the URL can drive the bot on the user's AI keys. Keep it local, or add signature validation first: Plivo signs the WebSocket upgrade over `http://<host>/<path>`, not the `wss://` URL (recipe: the signature section listed below).

Two paths:

1. **Pipecat Cloud:**
   - Deploy `bot.py` with the example's `Dockerfile` and `pcc-deploy.toml`.
   - Host `server.py` separately with `ENV=production`, `AGENT_NAME` and `ORGANIZATION_NAME` set, as the example's README describes.
2. **One host:** run `uv run server.py` with `ENV=local` on a machine behind HTTPS. Its answer XML then points at `wss://<host>/ws`.

Before you change the application, check the XML:

```bash
curl -s -i https://<host>/                           # status 200 and <Stream …>wss://…</Stream> in the body
plivo account applications update <app_id> --answer-url https://<host>/ --answer-method GET --dry-run   # preview; then run it again without --dry-run
```

If the number is on `my-first-agent`, a call now reaches the deployed bot. Place the test call as in step 6, the way the user chose; for a Plivo call, use `--answer-url https://<host>/ --answer-method GET`. Check the call record as in step 6. Then record `https://<host>/` and `GET` as the new resting values.

For production hardening, install `plivo skill install audio-streaming` and read:

- stage 6 (production hosting);
- stage 8 (operating it);
- its section "Callbacks, signature validation, timeouts".

## Step 9: leave a safe resting state

Do this on every exit, including after a failure:

1. If `forward` still runs, stop it with Ctrl-C or `kill -TERM <pid>`. It restores the answer URL and method.
2. Confirm the resting values:

   ```bash
   plivo account applications get <app_id> -o json      # answer_url and answer_method equal the resting values
   ```

3. If they do not match (for example, the process was killed), set them yourself, previewing with `--dry-run` first. For the demo resting values, the command is:

   ```bash
   plivo account applications update <app_id> \
     --answer-url https://s3.amazonaws.com/static.plivo.com/answer.xml --answer-method GET
   ```

4. If the number is on `my-first-agent`, ask with your question tool where it should stay:
   - on `my-first-agent`, where callers reach the resting document or the deployed bot;
   - back on its old application: `plivo numbers update <number> --app-id <old_app_id>`, or `--trunk-id <id>` for a trunk (step 4); preview it with `--dry-run` first. Offer this only if the recorded `application` had a value, because `numbers update` cannot clear it.

   For a number the user already had, make "back on its old application" the default, unless they deployed in step 8.
5. To stop the monthly rental of a number rented in this run, preview with `plivo numbers release <number> --yes --dry-run` (a release refuses `--dry-run` alone), ask, then run `plivo numbers release <number> --yes`. After a deploy, offer this only if the user asks.

## When a call fails

```bash
plivo voice calls get <call_uuid> -o json        # hangup_cause_code, hangup_cause_name, hangup_source
plivo voice calls diagnose <call_uuid>           # AI explanation; it shares a small per-account rate limit with `plivo ask`
```

| You see | Likely cause | Do this |
|---|---|---|
| 403 `Calls to this destination region are barred` on `calls make` | The account's geo permissions block the destination country | Allow it in the console (Voice, Geo Permissions): Professional accounts can allow only the US and India, other countries need Enterprise. Or the user calls in |
| 7011 Error Reaching Answer URL | `forward` is not running, or its tunnel dropped. `forward` does not notice a dropped tunnel and leaves the answer URL on it | Stop `forward` (it restores the answer URL), check the resting values, then start it again and wait for `✓ Ready.` |
| 8011 Invalid Answer XML | The answer URL returned something that is not Plivo XML | Run step 9, check the answer URL, and try again |
| 2070 Violates Media Anchoring | India: a call leg, or the bot's server, is outside India. A laptop behind a tunnel may count as outside | Follow the India note in step 6 |
| The call connects but the caller hears nothing | The bot is not running, or it listens on another port | Run the step 3 or step 7 `streams test` again |

For anything deeper, install `plivo skill install audio-streaming`.

## Out of scope

- India KYC itself, and 140 or 160 series numbers (`plivo skill install audio-streaming`).
- Outbound calling campaigns, production hosting and monitoring (`plivo skill install audio-streaming`).
- SIP platforms such as LiveKit, ElevenLabs, Retell and Vapi (`plivo skill install sip-trunking`).
