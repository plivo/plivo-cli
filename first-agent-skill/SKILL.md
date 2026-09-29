---
name: plivo-first-agent
description: "Take a new user from nothing to a first AI voice agent on a real phone call with Plivo, using the Plivo CLI and a WebSocket bot on their own machine. Use when someone wants to build, set up or try their first Plivo voice agent or voice bot, wants to call a phone number and talk to an AI, or asks to set up Plivo end to end. The run creates or reuses one application named my-first-agent, proves an echo bot and then an OpenAI bot on live calls, leaves the number in a safe state and offers a deploy step. Not for SIP platforms such as LiveKit, ElevenLabs, Retell or Vapi (use plivo-sip-trunking), and not for numbers in India, which need KYC first (use plivo-audio-streaming)."
license: Apache-2.0
---

# Plivo: your first voice agent

You take the user from nothing to a live call with an AI voice agent. The call comes in on a Plivo number, and Plivo streams the audio to a bot on the user's machine through a tunnel. The run has two bot stages:

1. **Echo bot.** It needs no API key. The caller hears their own voice. This proves the number, the application, the tunnel and the audio in both directions.
2. **OpenAI bot,** from the Pipecat example that the Plivo docs use. It needs the user's own AI keys.

Use the `plivo` CLI for every Plivo step and read values with `-o json`. When this file and `plivo <command> --help` disagree, the CLI is right.

## Done means all of this, with evidence

1. Application `my-first-agent` exists, and the user's number is attached to it for the calls.
2. **Echo call:** a real call reached the echo bot, and the user confirms they heard their own voice.
3. **OpenAI call:** a real call reached the OpenAI bot, and the user confirms they had a conversation. Skip this item only if the user has no AI key, and say so.
4. **Call records:** for each call, `plivo voice calls get <call_uuid> -o json` shows `call_duration` of 10 seconds or more and a `hangup_cause_name` of `Normal Hangup` or `End Of XML Instructions`.
5. **Resting state:** at the end, the application's answer URL is the resting URL you recorded (step 5), and the number is where the user chose to leave it (step 8).
6. **Report to the user:**
   - the number and the application id;
   - what runs on their machine;
   - how to start it again;
   - what each paid step cost;
   - how to deploy it.

Do not report "done" for an item that you did not see in a command output or hear confirmed by the user.

## Track the run in your task list

At the start, create these nine tasks with your agent's task tool:

- **Claude Code:** TaskCreate and TaskUpdate; TodoWrite in older versions.
- **Codex CLI:** its plan tool.

Mark a task done only when its check passes.

1. Check tools and login
2. Ask the setup questions
3. Test the echo bot (no phone)
4. Get a number
5. Create or reuse my-first-agent
6. Echo call
7. OpenAI bot and call
8. Leave a safe resting state
9. Offer a deploy

If you have no task tool, print this list, and print it again with ticks after each task. Claude Code turns the task tools off by default on some newer models. If they are missing, tell the user that starting Claude Code with `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` turns them on. Do not change their settings yourself.

## Ask with your question tool

Ask the setup questions in one call to your agent's structured question tool. In Claude Code this is AskUserQuestion: one call takes 1 to 4 questions, each question takes 2 to 4 options, and the tool adds its own free-text "Other" option, so do not add one. If you have no such tool, ask the same questions as a numbered list with an "Other" choice, and wait for the answers.

Setup questions (task 2):

| Header | Question | Options |
|---|---|---|
| Country | Which country should the number be in? | US (recommended) · Canada · India |
| Number | Do you want to rent a new number? | Rent a new number (recommended) · Use a number I have |
| AI keys | Which AI keys do you have for the second bot? | None yet (echo bot only) · OpenAI only · OpenAI, Deepgram and Cartesia |

- **India:** stop and explain. Indian numbers need an India data-region organisation and an accepted KYC application before rent. Install `plivo skill install audio-streaming` and follow its India section first.
- **Another country (from "Other"):** run `plivo numbers compliance requirements --country <ISO code> --number-type local --user-type business -o json`. If it lists documents, stop and explain that the country needs a compliance application first.
- **Money:** ask about each paid step separately, after you show its preview. There are two: renting a number, and each call. Never pass `--yes` unless the user said yes to that exact step.

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

Check: `whoami` shows the account the user expects, and `cash_credits` is above 0.

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

2. Show the user the number's current `application`, and explain what changes: while this run lasts, calls to that number reach the demo message or this machine.
3. Ask before you move it.

To rent a new number:

```bash
plivo numbers search --country <ISO code> --type local --limit 5 -o json
plivo numbers buy <number> --dry-run
```

Pick one whose `voice_enabled` is true. Show the number, `monthly_rental_rate` and `voice_rate` from the search result. Ask the user, then run `plivo numbers buy <number> --yes`.

Either way, read the number and record its current `application`. This is the rollback value:

```bash
plivo numbers get <number> -o json
```

## Step 5: create or reuse my-first-agent

The API matches `app_name` by prefix, so check for the exact name yourself:

```bash
plivo api GET /Application/ --query app_name=my-first-agent -o json
```

**If an object in `data.objects` has `app_name` equal to `my-first-agent`, reuse it:**

1. Read it with `plivo account applications get <app_id> -o json`.
2. Record its `answer_url` and `answer_method` as this run's **resting values**.
3. Check that the recorded URL answers. Send `curl -s -i` with the recorded method and look for status 200 and a body that starts with `<Response` or `<?xml`. If it does not answer, set the demo resting URL (the step 8 update command, after the user agrees) and record that instead.
4. List the numbers on the app. This is the filter `streams forward` itself uses:

   ```bash
   plivo api GET /Number/ --query application=<app_id> -o json
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

Attach the number. Preview first, then run it again without `--dry-run`:

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

- `✓ Ready. Dial …`: the tunnel is up. Ask the user to call the number and speak for at least 10 seconds. They should hear their own voice.
- `rejected: bad or missing Plivo signature`: the call came from a different account or subaccount than the CLI profile. Run `forward` under the profile that owns the number.
- `dial customer WS … failed`: the bot is not running or listens on another port.

Before the user dials, list the calls to this number once and note their UUIDs. After the call, list them again and take the new call UUID; the list shows completed calls, so if it is not there yet, wait a few seconds and list again:

```bash
plivo voice calls list --to <number> --direction inbound --limit 5 -o json
plivo voice calls get <call_uuid> -o json        # call_duration, hangup_cause_name, hangup_source
```

## Step 7: OpenAI bot and call

Skip this step if the user has no AI key, and say that item 3 of the definition of done is not met.

```bash
git clone --depth 1 https://github.com/pipecat-ai/pipecat-examples.git
cd pipecat-examples/plivo-chatbot/inbound
uv sync
cp env.example .env
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

Then start the OpenAI bot in the background, and test it without a phone:

```bash
uv run server.py                       # port 7860; serves the answer XML on GET / and the bot on /ws
plivo voice streams test --to ws://127.0.0.1:7860/ws --bidirectional --duration 10 -o json
```

The first connection loads Pipecat, so it can be slow. If `frames_read_back` is 0, read the server output, fix the error it shows (a missing key is common), and run the test once more.

Check the resting values again, then point `forward` at the bot. Preview first, then run it with `-o table --yes` in the background after the user agrees:

```bash
plivo voice streams forward --number +<number> --app <app_id> --to ws://127.0.0.1:7860/ws --dry-run
```

Ask the user to call and talk to the bot. Check the call record as in step 6.

## Step 8: leave a safe resting state

Do this on every exit, including after a failure:

1. Stop `forward` with Ctrl-C or `kill -TERM <pid>`. It restores the answer URL and method.
2. Confirm the restore:

   ```bash
   plivo account applications get <app_id> -o json      # answer_url and answer_method equal the recorded resting values
   ```

3. If they do not match (for example, the process was killed), set them yourself, previewing with `--dry-run` first. For a new application, the command is:

   ```bash
   plivo account applications update <app_id> \
     --answer-url https://s3.amazonaws.com/static.plivo.com/answer.xml --answer-method GET
   ```

4. Ask with your question tool where the number should stay:
   - on `my-first-agent`, where callers hear the resting document;
   - back on its old application: `plivo numbers update <number> --app-id <old_app_id>` (preview it with `--dry-run` first).

   Make "back on its old application" the default for a number the user already had.
5. To stop the monthly rental of a number rented in this run, preview with `plivo numbers release <number> --yes --dry-run` (a release refuses `--dry-run` alone), ask, then run `plivo numbers release <number> --yes`.

## Step 9: offer a deploy

The agent works only while this machine, the bot and `forward` run. To keep it live, the answer URL and the bot need a public HTTPS host. Ask before you deploy anything, because hosting costs money. The example's `Dockerfile` builds only `bot.py`, for Pipecat Cloud. It does not include `server.py`, which serves the answer XML.

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

Call the number and check the call record again. For production hardening, install `plivo skill install audio-streaming` and read:

- stage 6 (production hosting);
- stage 8 (operating it);
- its section "Callbacks, signature validation, timeouts".

## When a call fails

```bash
plivo voice calls get <call_uuid> -o json        # hangup_cause_code, hangup_cause_name, hangup_source
plivo voice calls diagnose <call_uuid>           # AI explanation; it shares a small per-account rate limit with `plivo ask`
```

| You see | Likely cause | Do this |
|---|---|---|
| 7011 Error Reaching Answer URL | `forward` is not running, or its tunnel dropped | Check the resting values, then start `forward` again and wait for `✓ Ready.` |
| 8011 Invalid Answer XML | The answer URL returned something that is not Plivo XML | Run step 8, check the answer URL, and try again |
| The call connects but the caller hears nothing | The bot is not running, or it listens on another port | Run the step 3 or step 7 `streams test` again |
| The call ends within 2 s with End Of XML Instructions | The stream closed at once. `forward` serves its own Stream XML without `keepCallAlive`, and its effect on a live call is not verified | Report it to the user with the call_uuid. For the OpenAI bot, `server.py` serves XML with `keepCallAlive="true"`: run a plain tunnel to port 7860 (for example `ngrok http 7860`, as the Plivo Pipecat guide does), point the application at `https://<tunnel>/` with `--answer-method GET`, and run step 8 when you finish |

For anything deeper, install `plivo skill install audio-streaming`.

## Out of scope

- Numbers in India: KYC and number series come first (`plivo skill install audio-streaming`).
- Outbound calls from the agent, production hosting and monitoring (`plivo skill install audio-streaming`).
- SIP platforms such as LiveKit, ElevenLabs, Retell and Vapi (`plivo skill install sip-trunking`).
