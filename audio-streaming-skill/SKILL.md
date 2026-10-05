---
name: plivo-audio-streaming
description: "Takes a new or existing Plivo user to a voice agent on real calls through audio streaming (the Stream XML element), using the Plivo CLI. It checks credit, KYC and the number, builds a test agent (an echo test, or Pipecat with OpenAI) or reuses the user's own WebSocket voice agent, exposes it through ngrok or Cloudflare Tunnel, creates an audio-stream application, links the number and places a test call, then covers go-live, handoff and debugging. Use when someone wants to build or try a Plivo voice agent or bot, connect a WebSocket bot to Plivo, or for a caller hearing silence, errors 7011/8011, streamed-call hangup codes, bot-to-human handoff, India KYC for an agent number. Not for SIP trunking (plivo-sip-trunking) or plain Voice XML (plivo-voice-xml)."
license: Apache-2.0
---

# Plivo Audio Streaming: connect a voice agent to calls and go live

Take a developer, or their coding agent, from a Plivo account to a voice agent that answers real calls, then to a monitored production agent. One flow serves new and existing users: it checks what the account already has (credit, KYC, a number) and skips what is done. It builds a test agent (an echo test, or Pipecat's OpenAI voice agent) or reuses the user's own voice agent. This file covers Plivo call control and the WebSocket boundary, not the STT, LLM or TTS inside the voice agent. Related skills, if installed: `plivo skill install voice-xml` (Plivo XML without a stream), `plivo skill install` (the CLI's own skill). Without them, use `plivo <command> --help` and the docs pointers below.

## Rules for every step

- Use the CLI for every Plivo step and `-o json` to read values. `plivo <command> --help` wins on which commands and flags exist; this file wins on how Plivo behaves, including tested behaviour the help text omits. Never invent a flag: where the CLI has none, use `plivo api <METHOD> <path>` or the console, and say so.
- Track the steps in your agent's own task or todo tool: TaskCreate and TaskUpdate in Claude Code (TodoWrite in older versions), the plan tool in Codex CLI. Mark a step done only when its check passes. If your agent has no task tool, print the step list, and print it again with ticks after each step. In Claude Code the task tools can be off: then tell the user that starting Claude Code with `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` turns them on, and do not change their settings yourself.
- Ask every question with your agent's own structured question tool. In Claude Code it is AskUserQuestion: one call takes 1 to 4 questions with 2 to 4 options each, and it adds a free-text "Other" option itself, so do not add one. Without such a tool, ask a numbered list with an "Other" choice and wait for the answer.
- Every write is two commands, never one shell command: first with `--dry-run` (prints the request, sends nothing), then, after you show the preview and the user agrees, the same command with `--yes`. That covers renting, linking a number, application changes, calls, compliance filings and `plivo api` writes. `account applications create`, `account applications update` and `numbers update` write at once without `--dry-run`, so the preview is their only check. If a command rejects `--dry-run`, show the current state from `get` or `list` instead and ask.
- Check the credit before every step: `cash_credits` in `plivo auth whoami -o json`. At 0 or less, tell the user to add credits on the console home page, <https://cx.plivo.com/home>, and go on with the free steps. Only renting a number and calls cost money: they wait until `cash_credits` is above 0.
- Before you link a number to another application, record its current `application` (`plivo numbers get <number> -o json`). Rollback: `plivo numbers update <number> --app-id <previous_app_id>`.
- Never read, print or ask for API keys in the chat: the user writes them into `.env` with an editor. Never run `plivo login` yourself (step 1).
- Writes can print nothing on stdout, even with `-o json`. Check the exit code, then read the result back with a `get` command.
- Report each step as observed, not known or failed. Observed means you saw the evidence in this session. Never say "ready" and never infer account state. A `request_uuid`, an HTTP 2xx, a passing XML check or a WebSocket handshake is not success: a call worked only when the call record, the voice agent's log and a person who heard the audio agree.
- Verdicts: **will break** when a documented rule or an observed failure says so (name the failure); **risky** when plausible or untested (say what to check, name no hangup code); **style** when nothing changes. A rule here with no label is risky.

## Talking to the user

- Say "voice agent", not "bot". The echo option is the "echo test", because it has no AI. File names such as `echo_bot.py` stay as they are.
- Keep CLI words out of questions and the report: dry-run, E.164, answer URL, WebSocket, `.env`, KYC accepted, main account. The questions written out in the steps are already right as they are, and commands and their previews stay as they are.
- Name apps by their names, not their IDs, in anything the user reads. Keep the IDs inside commands.
- Mask phone numbers as `+91 AB CDXX XEFG` (country code, first four digits, X for the next three, last three) and the account ID as its first four and last three characters with `…` between, in questions and the report. Show a number in full only where the user must dial it, and inside commands.
- Show prices only for a number the user rents, never for calls or the balance. Take them from the step 4 search. The API gives no currency, so add `₹` only when you know the account bills in rupees.
- Keep each question to one or two short sentences: what happens, and what it changes. Question headers: Account, Agent, Number, Test call, Country, Buy number, Keys, Answer URL, Tunnel, Install, Create app, Link, Call, Result, Debug, Finish.

## Steps 1 to 10: from the account to the first call

Create these ten tasks first, then run the steps in order. Skip the work the account already has, but run every check. Stop at the first failed check and fix it before you go on. Steps 1 to 8 place no call.

1. Check tools and login
2. Check the credit
3. Ask the setup questions
4. Get the number
5. Get the voice agent ready
6. Start the tunnel
7. Create the application
8. Link the number
9. Make a test call
10. Finish and report

### Step 1: check tools and login

```bash
plivo --version                  # if missing: brew install plivo/tap/plivo, or the install.sh one-liner from the Plivo docs
plivo auth whoami -o json        # exit 2 with AUTH_MISSING: the user logs in (below), then run this again
uv --version                     # the test voice agents run with uv; if missing, ask the user to install it (https://docs.astral.sh/uv/)
command -v ngrok cloudflared     # tunnel tools for step 6
```

Check: `whoami` shows the account the user expects.

- Login opens a browser, so the user runs it. On their own computer: `plivo login` in a terminal (in Claude Code, `! plivo login`). In a container, over SSH or in WSL, the browser cannot reach the CLI: the user runs `plivo login` in a terminal on that machine, approves in the browser, then copies the full URL from the browser's address bar and pastes it into that terminal (v1.1.3 or later). `! plivo login` cannot take that paste.
- Wrong organization: `plivo auth list`, then `plivo auth use <profile>`. A `plivo login` for another organization saves one profile for each organization.

### Step 2: check the credit

Check: `cash_credits` in the step 1 output is above 0. If it is not, send the user to <https://cx.plivo.com/home> to add credits (in India, at least ₹2,000 by card), and read `whoami` again after they say it is done. Only renting a number and calls cost money, so the other steps can run while they recharge.

### Step 3: ask the setup questions

First list the account's voice numbers: `plivo numbers list --services voice -o json` (`data.objects[]` has `number` and `country`). Then ask in one question-tool call:

| Header | Question | Options |
|---|---|---|
| Account | You're logged in as <first name> <initial>. (<masked account ID>). Use this account? | Yes · Use another account: "I'll show your other logins, or you can log in again." |
| Agent | Which voice agent should answer the test call? | Echo test (recommended): "No AI. Repeats what you say, to check the line works. No API key needed." · OpenAI voice agent: "A real AI conversation. Needs your OpenAI key." · My own voice agent: "You already have one with a WebSocket URL." |
| Number | Which number should your voice agent answer on? Its calls will go to the agent until you switch it back. | Up to two of the account's numbers, each "<masked number>: <city>. Now used by <app name>." · Buy a new number: "About <monthly price> a month." |
| Test call | How do you want to test it? | I'll call the number (recommended): "Dial it from a phone in the same country as the number." · Call my phone: "Plivo rings you." |

- Account: use the name (first name and initial) and the masked `auth_id` from `whoami`. "Use another account": `plivo auth list`, then `plivo auth use <profile>`, or the user logs in again (step 1). Then run steps 1 and 2 again and ask the setup questions again: the numbers and the country belong to the account.
- Number: prefer numbers on no application, and name the application each offered number is on now, because linking takes the number away from it. Fill in the monthly price only when you have it from a search; otherwise leave it out.
- If the account has no voice number, ask Country in place of Number. If the user picks "Buy a new number", ask Country next: "Which country should the new number be in?", with the account's country (recommended) and the other of India and the United States. The account's country is the country of its numbers, or India when it has an accepted India KYC application; with neither, India. No account field gives it.
- My own voice agent: ask in plain text for its WebSocket URL, and whether it already has a URL that gives Plivo the call's instructions (the answer URL) and its method.

### Step 4: get the number

**A number the account has.** Check: `plivo numbers get <number> -o json` shows it on this account with `voice_enabled` true. Record its `application` (step 8 needs it for rollback). It ends with `/Application/<app_id>/`, or with `/Zentrunk/Trunk/<id>/` for a SIP trunk, where the rollback is `--trunk-id <id>`. An Indian number also needs the KYC check below.

**A new number:**

1. India: rent only after KYC is accepted. Check: `plivo numbers compliance list --country IN --number-type local --status accepted -o json` lists an application. If it lists nothing, stop. Tell the user that Indian numbers need accepted KYC, and send them to <https://cx.plivo.com/phone-numbers?tab=compliance> (or file it from the CLI: "KYC from the CLI"). `submitted` is not `accepted`; automated review of 080 and 022 numbers takes about 5 minutes. Run the check again when they say it is done.
2. Search: `plivo numbers search --country IN --type local --limit 10 -o json` (another country: its ISO code). Keep numbers with `voice_enabled` true. India: landline numbers (a city code such as 080 or 022), the series for service and transactional calls; if a number has a `restriction`, show its `restriction_text`. Other countries: only numbers whose `restriction` is null.
3. Ask "Pick a number:" (header Buy number) with up to three numbers as options, each "<masked number>: <city> · <setup price> setup + <monthly price> a month" from `setup_rate` and `monthly_rental_rate`.
4. Preview `plivo numbers buy <number> --dry-run`, then ask: "Buy <masked number> for <setup price> now and <monthly price> a month? You can release it anytime." · Buy it / Not now. On yes, run `plivo numbers buy <number> --yes`.
5. Check: `plivo numbers get <number> -o json` shows it on this account with `voice_enabled` true. In India, the accepted KYC application links to the number at purchase.

- An empty Indian search although the KYC check passed: stop and show both outputs. `compliance_application_id is required` on `buy`: see "KYC from the CLI".
- A number at another carrier: forward it to a Plivo number, or have the carrier send SIP to `sip:<app_id>@app.plivo.com` with SIP authentication: `plivo docs show voice/use-cases/connect-external-numbers` (<https://plivo.com/docs/voice/use-cases/connect-external-numbers>).

### Step 5: get the voice agent ready

Every voice agent needs two endpoints: an answer URL that returns the call's XML, and a WebSocket URL that the XML's `<Stream>` points at. The checks here run on this machine and place no call.

**Echo test.** Create `echo_bot.py` in the user's working directory. It serves the answer XML on `/answer` (any method) and echoes the caller's audio on `/ws`:

```python
# /// script
# requires-python = ">=3.11"
# dependencies = ["aiohttp>=3.9"]
# ///
import json
import os
from xml.sax.saxutils import escape

from aiohttp import WSMsgType, web

XML = ('<Response><Stream bidirectional="true" keepCallAlive="true" '
       'contentType="audio/x-mulaw;rate=8000">{url}</Stream></Response>')


async def answer(request):
    url = os.environ.get("STREAM_URL") or f"wss://{request.host}/ws"
    return web.Response(text=XML.format(url=escape(url)), content_type="application/xml")


async def echo(request):
    ws = web.WebSocketResponse()
    await ws.prepare(request)
    async for msg in ws:
        if msg.type != WSMsgType.TEXT:
            continue
        event = json.loads(msg.data)
        if event.get("event") == "media":
            await ws.send_json({
                "event": "playAudio",
                "media": {
                    "contentType": "audio/x-mulaw",
                    "sampleRate": 8000,
                    "payload": event["media"]["payload"],
                },
            })
    return ws


app = web.Application()
app.add_routes([web.route("*", "/answer", answer), web.get("/ws", echo)])
web.run_app(app, host="127.0.0.1", port=int(os.environ.get("PORT", "8765")))
```

Start it in the background with `uv run --script echo_bot.py` (port 8765; set `PORT` for another) and keep it running. Check:

```bash
curl -s -X POST http://127.0.0.1:8765/answer        # <Response><Stream ...>wss://127.0.0.1:8765/ws</Stream></Response>
plivo voice streams test --to ws://127.0.0.1:8765/ws --bidirectional --duration 3 -o json   # frames_read_back above 0
```

The application uses `https://<tunnel>/answer` with POST.

**OpenAI voice agent,** from the Pipecat example that the Plivo docs use:

```bash
{ [ -d pipecat-examples ] || git clone --depth 1 https://github.com/pipecat-ai/pipecat-examples.git; } &&
  cd pipecat-examples/plivo-chatbot/inbound && uv sync &&
  { [ -f .env ] || cp env.example .env; }      # safe to re-run: keeps the clone and a filled .env
```

Ask "Which AI keys do you have?" (header Keys) · OpenAI only: "I'll switch the agent to OpenAI's speech-to-speech model." · OpenAI, Deepgram and Cartesia: "The example as it is." Then ask for exactly the values that choice needs, plus the two Plivo values: "Open .env in your editor and paste your OpenAI key after OPENAI_API_KEY= (and your Deepgram and Cartesia keys after DEEPGRAM_API_KEY= and CARTESIA_API_KEY=), and your Plivo auth ID and auth token from the console after PLIVO_AUTH_ID= and PLIVO_AUTH_TOKEN=. Tell me when it's saved. Don't paste them here." Then:

- OpenAI, Deepgram and Cartesia: run the example as it is.
- OpenAI only: change `bot.py` to one speech-to-speech service in place of the separate speech-to-text, language model and text-to-speech services. Import `OpenAIRealtimeLLMService` from `pipecat.services.openai.realtime.llm`, and build the pipeline as `transport.input()`, the user aggregator, the realtime service, `transport.output()` and the assistant aggregator. Take the settings from Pipecat's own `examples/realtime/realtime-openai.py` for the version that `uv sync` installed, not from memory.
- `PLIVO_AUTH_ID` and `PLIVO_AUTH_TOKEN` feed the example's Plivo serializer. The CLI keeps its token in the operating system's keychain; never read it from there.

Start it in the background with `PYTHONUNBUFFERED=1 uv run uvicorn server:app --host 127.0.0.1 --port 7860` (the answer XML on `GET /`, the voice agent on `/ws`). This accepts connections from this computer only, like the echo test; `uv run server.py` listens on every network, so anyone on the same Wi-Fi could reach it and spend the user's keys. Check:

```bash
curl -s http://127.0.0.1:7860/                       # <Stream ...>wss://127.0.0.1:7860/ws</Stream>
plivo voice streams test --to ws://127.0.0.1:7860/ws --bidirectional --duration 10 -o json   # frames_read_back above 0
```

The first connection loads Pipecat, so it can be slow. If `frames_read_back` is 0, read the server output, fix the error it shows (a missing key is common), and test once more. Restart the server after any change to `.env` or the code. To see which keys are set without their values: `awk -F= '/^[A-Z_]+=/ {print $1, (length($2) ? "set" : "EMPTY")}' .env`. The application uses `https://<tunnel>/` with GET.

**Your own voice agent.** Build nothing: check what the user has, and reuse it.

- WebSocket: `plivo voice streams test --to <wss URL> --bidirectional --duration 5 -o json`. Check: `frames_read_back` above 0. This proves only that the endpoint accepts a connection and replies. The test sends no signature, so a voice agent that rejects unsigned upgrades fails here: test a local copy with the check off, or report the WebSocket as not known. Its frames are reduced (no `sequenceNumber`, `extra_headers`, `start.tracks` or media `streamId`, and `chunk` is a string), and with `--bidirectional` it drops the TCP connection at the end with no `stop` frame. If the voice agent supports both codecs, run `--codec mulaw` and `--codec l16 --rate 16000` separately.
- Answer URL: probe it the way Plivo calls it, with its method:

  ```bash
  curl -s -i -X POST <answer URL> \
    -d 'CallUUID=readiness&From=%2B910000000000&To=%2B<number>&Direction=inbound&CallStatus=ringing&Event=StartApp'
  ```

  Check: HTTP 200, `Content-Type` `application/xml` or `text/xml`, a body that passes "Check the XML before you serve it" with a `<Stream>` that points at the voice agent, and a reply well inside Plivo's 15-second limit. The URL must work without Basic or bearer credentials. If it validates Plivo signatures, this unsigned probe gets a 401 that proves nothing: read the body Plivo fetched in the console debug logs after the test call instead.
- No answer URL yet: ask "Plivo needs a URL that tells it to connect calls to your voice agent. Where should that live?" (header Answer URL) · On my server (recommended): "I'll give you the code to add." · Run a small one here for the test. On my server: compose document A from "The XML" with the agent's `wss://` URL and codec, the user adds a route that returns it, and you probe it as above. A small one here: run the echo test script with `STREAM_URL=<wss URL> uv run --script echo_bot.py`; its `/answer` then points calls at the user's voice agent. It always asks for `audio/x-mulaw;rate=8000`, so offer it only for a voice agent that takes μ-law at 8 kHz.
- On a public `https` host, skip step 6. On this machine, step 6 tunnels the port that serves the answer URL; a WebSocket on another port needs its own tunnel.

Before you leave step 5, note the answer path and method and the WebSocket URL: steps 6, 7 and 10 use them. Echo test: `/answer` with POST, and `wss://<host>/ws`. Pipecat: `/` with GET, and `wss://<host>/ws`. Your own voice agent: what you probed.

### Step 6: start the tunnel

Plivo must reach the answer URL and the WebSocket over the internet. Skip this step for a voice agent on a public `https` host. Otherwise ask "Your voice agent runs on this computer. How should Plivo reach it?" (header Tunnel) · Cloudflare (recommended): "Free, no account." · ngrok: "Uses your ngrok account."

- Offer ngrok only when it is installed and has an authtoken: `ngrok config check` prints the config file's path, and `grep -q authtoken <path>` tells you whether the token is there without printing it.
- `cloudflared` not installed: ask "Install Cloudflare's tunnel tool (cloudflared) with Homebrew? About 25 MB." (header Install) · Install / I'll do it. macOS: `brew install cloudflared`. Linux: say "download" in place of "with Homebrew", and download `cloudflared-linux-amd64` or `cloudflared-linux-arm64` from <https://github.com/cloudflare/cloudflared/releases/latest> into `~/.local/bin` as `cloudflared`, and make it executable.

Start the tunnel in the background on the agent's port (8765 for the echo test, 7860 for Pipecat) and read its URL:

```bash
cloudflared tunnel --config /dev/null --url http://127.0.0.1:<port> > cloudflared.log 2>&1
grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' cloudflared.log

ngrok http <port> --log stdout > ngrok.log
curl -s http://127.0.0.1:4040/api/tunnels           # tunnels[].public_url
```

- `--config /dev/null` matters: with a named tunnel's `~/.cloudflared/config.yml` present, a quick tunnel answers 404.
- Wait about 20 seconds after the URL appears before the first request. A lookup made before the new name exists can be cached as "no such host" for minutes. If that happens anyway, wait and retry, or start a new tunnel.
- Every restart of a quick tunnel, or of ngrok without a reserved domain, gives a new URL: update the application's answer URL after each restart (step 10).

Check, through the tunnel, with the answer path, method and WebSocket URL from step 5:

```bash
curl -s -X <method> https://<tunnel><answer path>    # echo test: curl -s -X POST https://<tunnel>/answer
plivo voice streams test --to <wss URL in that XML> --bidirectional --duration 3 -o json
```

The XML's `<Stream>` must point at a `wss://` URL that Plivo can reach (`wss://<tunnel>/ws` for the echo test and Pipecat; with `STREAM_URL` set, the user's voice agent), and `frames_read_back` must be above 0. The Pipecat example's `/ws` checks no Plivo signature, so anyone who finds the URL can drive the voice agent on the user's keys: keep the tunnel up only while testing.

### Step 7: create the application

Name it `audio-stream-<bot>-<n>`: `<bot>` is `echo`, `openai` or `custom` (the user's own voice agent), and `<n>` is one more than the highest number already used, starting at 1. The fixed `audio-stream-` prefix lets the console filter these applications. The API matches `app_name` by prefix, so this lists the ones in use:

```bash
plivo api GET /Application/ --query app_name=audio-stream-<bot>- --query limit=20 -o json   # while meta.next is set, add --query offset=20, 40, ...
```

Count only names that match `audio-stream-<bot>-<digits>` exactly. If one exists, ask "Reuse <latest app name>, or create <next app name>?" (header Create app) · Reuse <latest app name> (recommended): "I'll point it at this test." · Create <next app name>. Before you offer Reuse, record the app's `answer_url` and `answer_method`, and list the numbers on it: read every page of `plivo numbers list -o json` and keep those whose `application` ends with `/Application/<app_id>/`. Recommend Reuse only when no number other than the one chosen in step 3 is on it; otherwise recommend Create, because an update reroutes every number on the app at once. To reuse, preview `plivo account applications update <app_id> --answer-url <url> --answer-method <method> --dry-run` and run it again without `--dry-run`. To create, preview, ask "Create a Plivo app called <app name>? It tells Plivo where to send calls. Free, and nothing changes on your number yet." · Create it / Stop, and run it again without `--dry-run`:

```bash
plivo account applications create --app-name audio-stream-<bot>-<n> \
  --answer-url https://<tunnel><answer path> --answer-method <method> --dry-run
```

Use the tunnel URL with the answer path and method from step 5: `https://<tunnel>/answer` with POST for the echo test, `https://<tunnel>/` with GET for Pipecat, the user's own for their voice agent. Check: read `app_id` from the output (or use the reused one's), then `plivo account applications get <app_id> -o json` shows that answer URL and method.

### Step 8: link the number

Linking moves all of the number's inbound calls and messages to the app. Show the preview, then ask "Send calls on <masked number> to the <echo test or voice agent>? Its calls and SMS stop going to <old app name> until you switch back. I'll do that for you at the end." (header Link) · Yes, switch it / Keep it on <old app name>. For a number on no app, replace the part about where its calls go now with: "Once linked, it can move to another app later, but it can't go back to having no app."

```bash
plivo numbers update <number> --app-id <app_id> --dry-run
```

Run it again without `--dry-run`. Check: `application` in `plivo numbers get <number> -o json` ends with `/Application/<app_id>/`. The rollback is the `application` you recorded in step 4.

### Step 9: make a test call

Check the credit first. Then place the call the way the user chose in step 3. They talk for 10 seconds or more: the echo test repeats them, the OpenAI voice agent answers them.

- **The user calls in:** ask them to call the number, in full. For an Indian number, from an Indian phone: Indian calls stay within India.
- **Plivo calls the user:**
  1. Ask in plain text: "Which phone should I call? Use a mobile in <the number's country>, for example <its format, such as +91 98XXX XXXXX for India>." Pass it to the CLI in `+<country code>...` form.
  2. Preview `plivo voice calls make --from <number> --to <phone> --answer-url <answer URL> --answer-method <method> --dry-run` with the app's answer URL and method (`calls make` defaults to GET).
  3. Ask "Call <masked phone> now? When it rings, answer and talk for 10 seconds. You should hear yourself repeated." (header Call) · Call me / Not now. For the OpenAI or your own voice agent, the last sentence becomes "Talk to your voice agent as you would to a person." On yes, run it with `--yes`. The `request_uuid` it returns means only that Plivo accepted the request; it is also the call's UUID (seen on every test call).
  4. A 403 `Calls to this destination region are barred` means the account's geo permissions block that country: Voice, Geo Permissions in the console. Professional accounts can allow only India and the US; other countries need Enterprise. Offer that, or the user calls in.
  5. `calls make` sets no time limit. If the call is still up after about 2 minutes (`plivo api GET /Call/<request_uuid>/ --query status=live -o json` returns it), preview `plivo voice calls hangup <request_uuid> --yes --dry-run`, ask, then run it without `--dry-run`.

Find the call record. A Plivo call: its UUID is the `request_uuid`. A call in: before it, note the UUIDs in `plivo voice calls list --to <number> --direction inbound --limit 5 -o json`; after it, list again and take the new UUID. If more than one is new, take the one whose `from_number` is the user's phone. The list shows completed calls only, so retry after a few seconds if a call is not there yet. Then:

```bash
plivo voice calls get <call_uuid> -o json        # call_duration, hangup_cause_code, hangup_cause_name, hangup_source
```

After the call, ask (header Result) for the echo test "Did you hear your voice repeated?" · Yes, clearly / Yes, but choppy or delayed / No, silence / The call didn't come. For the OpenAI or your own voice agent: "Did your voice agent talk with you?" · Yes, it worked / Yes, but slow or choppy / No, silence / The call didn't come.

Check: the answer is "Yes, clearly" or "Yes, it worked", `call_duration` is 10 seconds or more, and `hangup_cause_name` is `Normal Hangup` or `End Of XML Instructions` (4010, the normal end of a keepCallAlive stream) after a real conversation. If the check fails, or the user says something did not work, ask "Something went wrong. Want me to find out why? Plivo's call debugger takes a few minutes." (header Debug) · Yes, debug it (recommended) / Not now. On yes, run `plivo voice calls diagnose <call_uuid>` once and tell the user its answer in plain words. Then: choppy or slow, Call Insights (layer 5 of "When a call fails"); silence, "Silence, then 4010"; no call, the call record, then the 403 note above; otherwise "When a call fails".

### Step 10: finish and report

If the number came from another app or a SIP trunk (step 4 recorded one), ask "Done testing. What should happen to <masked number>?" (header Finish) · Switch it back to <old app or trunk name> (recommended) · Keep it on the voice agent. To switch back, preview the step 4 rollback (`--app-id` or `--trunk-id`) and run it. Otherwise leave the app and the number as they are.

Then report, short, in these words where they fit:

- Headline. Echo test: "The line works: you heard yourself on a real call. The call record and the agent log agree." OpenAI or your own voice agent: "Your voice agent works: it talked with you on a real call. The call record and the agent log agree."
- A missed check, in one line: "The call lasted <N> s (the check asks for 10 s). You heard the echo, so I didn't place a second call."
- Agent log: "Plivo connected to your voice agent." Leave out harmless log warnings.
- Setup: "Number <masked number> (<city>)", "App <app name>", and "Previously on <old app or trunk name>" for a moved number. For a reused app, also its previous answer URL, so the user can put it back.
- Costs: only for a number rented in this run: "New number: <setup price> setup, <monthly price> a month."
- Running here: "Running on this computer: the echo test and a Cloudflare tunnel. The number answers only while both run. Each time the tunnel restarts it gets a new link, so update the app with:" then `plivo account applications update <app_id> --answer-url https://<new tunnel><answer path> --answer-method <method>` (preview first). Only the host changes.
- A moved number the user kept on the agent: "To switch it back later, ask me, or run:" then the rollback command in full.
- "Security: anyone with the tunnel link can reach your voice agent. Keep the tunnel running only while you test."
- "To go live without this computer: host your voice agent on your own domain (Mumbai for India), check Plivo's signature, and add a statusCallbackUrl so you hear about dropped calls." Details: "Go live".
- "Next: try the OpenAI voice agent, or connect your own. Tell me which, and I'll use the same number."

## Go live

- Your own domain with a valid certificate, near the callers (Mumbai for India, US East or West for the US; the docs target under 1 s end to end). A stopped or rotated tunnel turns every call into a 7011.
- Run the step 5 checks against the production host. Then `plivo account applications update <app_id> --answer-url https://PROD/plivo/answer --hangup-url https://PROD/plivo/hangup --dry-run`, show the current values being replaced, and apply after approval. `applications update` has no fallback flag: `plivo api POST /Application/<app_id>/ --body '{"fallback_answer_url":"https://PROD/plivo/fallback"}' --dry-run`, then `--yes`.
- The Pipecat example: its `Dockerfile` builds only `bot.py`, for Pipecat Cloud (`pcc-deploy.toml`). Host `server.py`, which serves the answer XML, separately with `ENV=production`, `AGENT_NAME` and `ORGANIZATION_NAME` set, as its README says, or run `server.py` with `ENV=local` on one HTTPS host. Its `/ws` checks no Plivo signature: add validation first ("Callbacks, signature validation, timeouts").
- Set `statusCallbackUrl` on `<Stream>`: it is the only push signal for `DroppedStream` and `DegradedStream`.
- Make every callback handler idempotent, because Plivo retries: key stream callbacks on `StreamID` plus the event, call callbacks on `CallUUID`.
- Alert on the 7011 rate: an answer URL that fails under load keeps producing 7011s long after launch. Plivo's Voice Alerts also email you when callback failures pass 5%: `plivo docs show voice/concepts/voice-alerts` (<https://plivo.com/docs/voice/concepts/voice-alerts>).
- Make one test call (step 9) against production before the first external caller.

## One app for many customers

- One application can serve every customer: route on `To` (the number dialled) and build the `wss://` URL per call on your server, with `CallUUID` or a short id in it.
- Use a subaccount per customer only when they need their own numbers, their own signing token (`X-Plivo-Signature-V3` is signed with the token of the subaccount that owns the number) or separate call records. Subaccounts share the parent's balance and inherit its geo permissions, which can be overridden per subaccount.
- The same number can serve people and a voice agent: the answer URL decides per call whether to return `<Dial>` or `<Stream>`.
- India resellers file one compliance application per end customer ("KYC from the CLI").
- The docs give no architecture guidance beyond this: say so if asked.

## Hand off to a human

Your backend calls `plivo voice calls transfer <call_uuid> --legs aleg --aleg-url https://PROD/plivo/transfer/<call_uuid>`, and that URL returns document E below.

- Call the Transfer API first, then close the socket, and keep `keepCallAlive="true"`: the transfer XML is subsequent XML, so it runs only when the stream ends. To stop the stream through the API instead (`plivo voice calls streams stop <call_uuid> --yes`), transfer first, then stop: stopping first lets the leg finish its current document, which can end the call.
- In the same document instead: a `<Dial>` or `<Redirect>` after `<Stream>` runs when the socket closes.
- The Dial `action` URL receives `DialStatus` (`completed`, `busy`, `failed`, `cancel`, `timeout`, `no-answer`), `DialRingStatus`, `DialHangupCause`, `DialALegUUID` and `DialBLegUUID`. The human leg's `DialBLegHangupCauseCode`, `DialBLegHangupCauseName` and `DialBLegHangupSource` go only to the Dial `callbackUrl`.
- Contact centres: prefer SIP, `<User sipAuthUsername="..." sipAuthPassword="...">sip:queue@cc.example.com</User>`; failures are 4240 (auth failed) and 4250 (auth timeout). The centre must allow Plivo's SIP signalling and RTP ranges: `plivo docs show voice/concepts/firewall-network-configuration` (<https://plivo.com/docs/voice/concepts/firewall-network-configuration>). Set `dialMusic` so the caller does not hear silence.
- Many human legs never answer: handle `DialStatus` and keep a `<Stream>` or `<Speak>` after `<Dial>`. A busy human is a handoff outcome, not a stream failure.
- Observed when: one test transfer reports `DialStatus=completed`, caller and human hear each other, and the voice agent's audio has stopped. A failing transfer URL shows as 7013 or 8013.
- An AI as a MultiPartyCall participant (`role="ai-agent"`) is documented (`plivo docs show voice/xml/multiparty-call`, <https://plivo.com/docs/voice/xml/multiparty-call>; `plivo docs show voice/api/multiparty-calls`, <https://plivo.com/docs/voice/api/multiparty-calls>), but its runtime behaviour is not verified and the docs do not say what `to` should be for a participant reached over a WebSocket: ask Plivo before you build on it. Also not documented: whether Plivo dials or bills that leg, what the AI hears (the whole room or one party), the order for holding and muting a party, and whether the XML form opens the socket the way the REST form does. `participant add --role` has no `ai-agent`, so it needs `plivo api POST /MultiPartyCall/name_<room>/Participant/`.

## Operate it

| Watch | How |
|---|---|
| Failed calls | `plivo voice calls list --limit 20 -o json` (20 is the API's per-page maximum: page with `--offset`; the API searches the last 7 days by default). Read `hangup_cause_code`, `hangup_cause_name`, `hangup_source` and the duration. Do not filter out 4010: a refused or crashed stream ends exactly that way, so look for 4010s that last seconds |
| One failure | "When a call fails" below |
| Stream drops | `DroppedStream` and `DegradedStream` on your `statusCallbackUrl`; log the raw `Event` (the docs use two naming schemes) |
| Callers hanging up in seconds | Common on streamed calls and not proof of a voice agent fault. Speak first and fast; keep any `<Speak>` before `<Stream>` short |
| A ceiling | Calls ending 4010 at the same second: `streamTimeout` or your own timer |
| Recordings | `plivo voice recordings list --call-uuid <uuid>` |
| Rehearse before launch | Silence, barge-in, DTMF, a voice agent timeout, a refused socket and one dropped mid-call, malformed `playAudio`, a busy human, recording on and off, the step 8 rollback |

## When a call fails

When a call fails or the user says something did not work, first ask whether they want you to debug it (the Debug question in step 9), and ask again before another run. On yes, start at layer 1; on no, start at layer 2. Run each layer once, keep the evidence, and do not repeat a billed call until the failed layer passes. First check what the number points at: `application` in `plivo numbers get <number> -o json`. The most common first-call failure is a number on another application, so your answer URL is never fetched; a console flow application with no working flow answers with JSON, which the call record reports as 8011.

1. `plivo voice calls diagnose <call_uuid>`: Plivo's AI reads the call record, the SIP and media trace and your answer-URL responses. Allow 30 to 120 s; the answer is free text, not a schema; own account only. It shares a small per-account rate limit with `plivo ask`, so do not loop it.
2. `plivo voice calls get <call_uuid> -o json`: `hangup_cause_code`, `hangup_cause_name`, `hangup_source`, `answer_time`, `bill_duration`; after a handoff read the human's leg too. `plivo api GET /Call/<call_uuid>/` has the full record.
3. Re-run the step 5 answer URL `curl` against the URL that failed: the answer URL, or the action, transfer or redirect URL for 7012/8012, 7013/8013 or 7014/8014 (those documents need no `<Stream>`). Headers show 401, 404, 405 or 530; the XML check finds body faults.
4. Re-run the step 5 `streams test` against the stream URL.
5. Console: Voice, Logs, Calls, the call. Audio Streams has the stream's debug logs (events, the `DroppedStream` error text); Call Insights flags audio problems per leg: one-way audio (packet count, audio level), broken audio (packet loss), robotic audio (jitter), audio lag (round-trip time), high connect time (post-dial delay); quality data is not available for every PSTN call, and the docs do not say how these apply to the WebSocket leg (`plivo docs show voice/call-insights`, <https://plivo.com/docs/voice/call-insights>). Then `plivo ask --call-uuid <uuid> "<question>"`, or Plivo support with every leg's call UUID, UTC times, the code, name and source per leg, the answer URL's host only, HTTP status and latency from your logs, the exact XML body (redacted), the stream callbacks received, the WebSocket close code, and whether the step 5 `streams test` passes. Never send tokens or caller audio.

Silence, then 4010 within seconds: the call record does not say who closed the socket, but the console's Audio Streams log gives each stream's hangup reason (API request, call hangup, connection error, stream timeout). Most likely first:

1. The XML: no `keepCallAlive` or `bidirectional`, or the number on another or a stale application.
2. The socket never connected: tunnel, TLS or path, or the voice agent rejected the upgrade by checking its signature over `wss://`, not `http://`.
3. The voice agent crashed on start or on the first `media` frame.
4. Nothing playable came back: wrong `contentType` or `sampleRate`, a WAV header inside the base64, or audio not paced in real time.
5. A fixed ceiling: `streamTimeout` or the voice agent's own idle timer.

A 7011 on a voice agent behind a tunnel: the voice agent or the tunnel stopped, or the tunnel restarted with a URL the application does not have. Start both, then update the answer URL (step 10).

| Code or signal | Meaning on a streamed call | Next |
|---|---|---|
| 7011 Error Reaching Answer URL | No usable HTTP response: 404, 401 or 403 (your auth), 405 (method), 502 or 530 (dead tunnel), a timeout | Layer 3 above; no credentials on the URL; a fallback URL |
| 8011 Invalid Answer XML | A 200 that is not Plivo XML: empty, JSON (often a console flow application: the number is on another application), HTML, malformed, an unsupported `<Speak language>`, Twilio `<Reject/>` | The number's application; the XML check on the exact body |
| 4010 End Of XML Instructions | The normal end of a keepCallAlive stream. Within seconds of answer, also how a refused or crashed stream ends | "Silence, then 4010" above |
| 4000 Normal Hangup | A person hung up. Source `Answer XML` means your `<Hangup/>` ran | Nothing |
| 3020 / 3010, source Answer XML, 0 s | `<Hangup reason="rejected"/>` or `reason="busy"` ran first (seen in call records; no docs page maps it). A bare `<Hangup/>` does not produce these | Intended? |
| 7012/8012, 7013/8013, 7014/8014 | The action, transfer or redirect URL was unreachable, or returned bad XML | Layer 3 above, against that URL |
| 6020 Media Timeout | No media for 60 s; the code does not say which side | Both media paths, the carrier, Call Insights |
| 2070 / 5030 | India only: a leg or your server is outside India / any region: over the concurrency limit, rejected at once | "India" / raise the limit |
| 3030 / 2030 | The caller ID is neither rented on this account nor a verified caller ID / the destination is barred by geo permissions | Your Plivo number; Voice, Geo Permissions |
| 9100 Machine Detected | `machine_detection=hangup` met a voicemail | Expected |
| 1000 Canceled / 1010 Canceled (Out Of Credits) | Your Hangup or Cancel API ended the call before answer / the balance ran out | Intended? / add credits, turn on auto-recharge |
| 4020 / 4030 | The MultiPartyCall ended, or a participant was removed | Was it intended? |
| 2020 / 9000 / 9110 (handoff) | The human's SIP endpoint is offline / another parallel leg answered first (normal for simultaneous dial) / the human did not press the `confirmKey` | Fall back to a number / nothing / check the confirm prompt |
| 4210 | An external carrier sending SIP to your application failed the IP ACL or credential check | `plivo docs show voice/use-cases/connect-external-numbers` |
| 3130 / 3140 | The carrier blocked the call as spam / the caller ID is on a Do Not Originate list | A Plivo number you rent (STIR/SHAKEN A), Caller Reputation / another number |
| `DroppedStream` (stream callback) | The socket failed to connect or died mid-call; a `DegradedStream` before it means too slow | Server, tunnel, pacing |

Read `hangup_source` before you blame the socket: `Caller` or `Callee` ("Call recipient" on the hangup-causes page) is a person, `API Request` is your Hangup or Cancel API, `Answer XML` is your `<Hangup>`, and `Plivo`, `Carrier`, `Error` or `Unknown` need the code. Every other code, including the carrier codes an outbound campaign sees daily: `plivo docs show voice/troubleshooting/hangup-causes` (<https://plivo.com/docs/voice/troubleshooting/hangup-causes>). A carrier code, a busy human, an out-of-credit cancel or a person hanging up is not a WebSocket defect.

## The XML: compose it, check it

Ask only what is unknown: the direction; the `wss://` URL; the codec (`audio/x-mulaw;rate=8000` unless the speech model wants `audio/x-l16;rate=16000`); whether to record and where the recording URL goes; anything before the agent (a greeting, a recording notice, a keypad menu); the handoff (none, a number, a SIP address, a URL that decides, a room) and whether the voice agent takes the caller back; a stream status callback URL (strongly recommended). Do not ask about `keepCallAlive`, `streamTimeout`, `audioTrack`, `noiseCancellation` or `extraHeaders`: use the attribute table.

Copy the closest document. `{{CallUUID}}` and `{{To}}` are for your server to fill in before it returns the XML; Plivo substitutes nothing. A `<Redirect>` or `<Hangup/>` placed after the `<Stream>` runs when the socket closes.

**A. Stream only**, the shape most deployments run.

```xml
<Response>
  <Stream bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
</Response>
```

**B. Record, then stream.** `<Record>` must come first: with keepCallAlive, anything after `<Stream>` runs only once the stream ends.

```xml
<Response>
  <Record recordSession="true" fileFormat="mp3" maxLength="3600" callbackUrl="https://voice.example.com/plivo/recording" callbackMethod="POST"/>
  <Stream bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
</Response>
```

**C. Greeting and keypad menu, then stream.** Everything before `<Stream>` delays the voice agent's first word. `redirect` defaults to `true` on `<GetDigits>`, `<GetInput>`, `<Record>`, `<Dial>` and `<Conference>`: a caller who responds then gets whatever the `action` URL returns and skips the rest of this document, while a caller who does not falls through after `retries`. `redirect="false"` keeps everyone on this document; the menu URL still gets the `Digits`, must answer 200 fast, and is where you store the choice for the voice agent (key it on `CallUUID`). To route each key to a different voice agent instead, keep the default, put a `<Speak>` and a `<Hangup/>` after `<GetDigits>` for the no-input path, and have the menu URL return document A or B.

```xml
<Response>
  <Speak voice="Polly.Aditi" language="en-IN">Welcome to Acme. This call may be recorded for quality and training.</Speak>
  <GetDigits action="https://voice.example.com/plivo/menu" method="POST" redirect="false" numDigits="1" timeout="5" retries="2" validDigits="12"><Speak voice="Polly.Aditi" language="en-IN">For sales, press 1. For support, press 2.</Speak></GetDigits>
  <Record recordSession="true" fileFormat="mp3" maxLength="3600" callbackUrl="https://voice.example.com/plivo/recording" callbackMethod="POST"/>
  <Stream bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
</Response>
```

**D. Stream plus a MultiPartyCall room.** No `keepCallAlive` here: the room must run after the stream starts.

```xml
<Response>
  <Stream bidirectional="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
  <MultiPartyCall role="Customer" coachMode="true" maxDuration="900" maxParticipants="10" record="false" recordParticipantTrack="true" statusCallbackEvents="mpc-state-changes,participant-state-changes" statusCallbackUrl="https://voice.example.com/plivo/mpc-status" statusCallbackMethod="POST">room-{{CallUUID}}</MultiPartyCall>
</Response>
```

**E. Transfer document** ("Hand off to a human"), served from the transfer URL. The `<Stream>` after `<Dial>` brings the caller back to the voice agent when the human does not answer.

```xml
<Response>
  <Record recordSession="true" fileFormat="mp3" maxLength="3600" callbackUrl="https://voice.example.com/plivo/recording" callbackMethod="POST"/>
  <Dial callerId="{{To}}" timeout="30" redirect="false" action="https://voice.example.com/plivo/dial-result" method="POST"><Number>+91XXXXXXXXXX</Number></Dial>
  <Stream bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
</Response>
```

**F. Stream, then hand the call to a URL that decides.** When the voice agent closes the socket, Plivo fetches the `<Redirect>` URL and runs what it returns: a `<Dial>` to a person, another `<Stream>`, or a `<Hangup/>`. A handoff with no Transfer API call.

```xml
<Response>
  <Stream bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
  <Redirect method="POST">https://voice.example.com/plivo/after-agent</Redirect>
</Response>
```

### Check the XML before you serve it

Check the exact body the URL returns. A pass proves the document only, not that Plivo can fetch it or that a call connects.

**Will break.** Fix before dialling:

- An empty body: 8011 if the server answered 200, 7011 if it answered non-2xx or not at all, so read the status before you name a code.
- JSON, HTML or XML that is not well formed (including two concatenated documents, or a comment containing `--`): 8011. JSON you did not write usually means a console flow application owns the number: check the number's `application`.
- A root other than `<Response>`, or an empty `<Response/>` (answered, then ended at once with 4010).
- A top-level Twilio `<Reject/>`: observed to fail with 8011; the Plivo form is `<Hangup reason="rejected"/>`. Plivo's elements are Response, Record, Stream, Speak, Play, GetDigits, GetInput, Dial, Number, User, Conference, MultiPartyCall, Redirect, Wait, Hangup, PreAnswer, DTMF and Message. Hold music is the `agentHoldMusicUrl` and `customerHoldMusicUrl` attributes of `<MultiPartyCall>`, not an element.
- An answer document that can never reach a `<Stream>` (or a `MultiPartyCall role="ai-agent"`), directly or through an action URL that returns one. Action, transfer and redirect documents need no `<Stream>`.
- Only `<Hangup/>`: the call answers and ends gracefully at once, and the record looks like a normal finish. Use a `<Speak>` placeholder while building; deliberate screening is `<Hangup reason="rejected"/>` or `<Hangup reason="busy"/>`.
- A talking voice agent's `<Stream>` without `keepCallAlive="true"`, except before a MultiPartyCall (document D). Without it the next element runs at once (documented), and with nothing after the stream the call ends within seconds with 4010 (observed).
- A `<Stream>` with no URL, a scheme other than `wss://` or `ws://`, a localhost host, or a URL over 2048 characters.
- `audioTrack` other than `inbound`, `outbound` or `both`, or `outbound` or `both` together with `bidirectional="true"` (the docs forbid it).
- A method attribute other than GET or POST; `extraHeaders` over 512 bytes.
- An action, callback, hold-music or `<Redirect>` URL that is not absolute http(s), still holds a placeholder, or points at localhost; an empty `<Redirect>`; an empty `<Number>` or `<User>`, or a `<Dial>` with neither; a `<GetInput>` without `action` (on `<GetDigits>` it is optional).
- A `<Speak language>` the chosen voice does not support: a `<Speak language="hi-IN" voice="WOMAN">` logged 8011. `WOMAN` and `MAN` cover da-DK, nl-NL, en-AU, en-GB, en-US, fr-FR, fr-CA, de-DE, it-IT, pl-PL, pt-PT, pt-BR, ru-RU, es-ES, es-US and sv-SE; other languages need a `Polly.<Name>` voice (`hi-IN` with `Polly.Aditi`, `en-IN` with `Polly.Raveena`; the full list: `plivo docs show voice/concepts/ssml`, <https://plivo.com/docs/voice/concepts/ssml>). Also a `voice` other than `WOMAN`, `MAN` or `Polly.<Name>`, and a `<Play>` holding text instead of an audio URL.

**Risky.** Say what to check; name no hangup code:

- No `bidirectional="true"`: the caller hears nothing from the voice agent (right only for transcription or monitoring).
- `contentType` missing, or not one of the documented values in the attribute table (for example `audio/x-wav` or `audio/x-mulaw;rate=16000`): untested. Set it explicitly.
- `ws://` instead of `wss://`: the guide requires `wss://`; `ws://` streams are seen to connect, but audio and headers cross the internet in clear text. Also `http://` callback URLs, and a temporary tunnel host anywhere (a stopped tunnel means `DroppedStream` or 7011).
- No `statusCallbackUrl`: you will not hear about `DroppedStream`.
- Credentials or tokens in the stream URL, `extraHeaders` or a callback URL: Plivo logs them. Use signatures.
- An element with an `action` URL and `redirect` left `true`, with more than a terminal fallback below it (document C). A terminal `<Speak>`, `<Play>`, `<Wait>` or `<Hangup>` below it is the correct shape.
- Anything after `<Redirect>`: dead code; move it into the redirect target's document.
- `<Record>` after `<Stream>` (it starts after the conversation), or without `recordSession="true"` (it waits for speech, then stops).
- More than one `<Stream>` (one runs per call); `streamTimeout` under 120 s or not a positive integer; `noiseCancellationLevel` outside 60 to 100 or without `noiseCancellation="true"`; `extraHeaders` separated by `;` or holding an item that is not `key=value`.
- Other Twilio or unknown elements (`<Say>`, `<Gather>`, `<Pause>`, `<Parameter>`; a top-level `<Say>` was seen ignored): use `<Speak>`, `<GetInput>` and `<Wait>`. Also stray text in `<Response>`, nesting a parent does not document, empty attribute values, a bare `&` (write `&amp;`), an empty `sendDigits`.
- A `<GetInput>` speech `language` outside en-US, en-GB, en-AU, es-US, es-ES, fr-FR, de-DE, it-IT, pt-BR, ja-JP and zh-CN: the docs call that list common, not complete, so test the code on a call.

**Style.** `<Dial>` creates a second, billed leg. Recording leaves disclosure, consent and retention to you.

Other Voice XML elements: `plivo skill install voice-xml`, which covers the common ones and points to the docs for full attribute tables, or <https://plivo.com/docs/voice/xml/overview>.

## `<Stream>` attributes and the WebSocket protocol

| XML attribute (API parameter) | Use | Why |
|---|---|---|
| `bidirectional` (`bidirectional`) | `true` | Default `false`: the caller hears nothing from the voice agent |
| `keepCallAlive` (none) | `true`, except before a MultiPartyCall | Default `false`. With it the stream runs alone and later XML runs only when it ends |
| `contentType` (`content_type`) | `audio/x-mulaw;rate=8000` | Always set it: the reference gives the default as `audio/x-l16;rate=8000`, the guide says mu-law. Also documented: `audio/x-l16;rate=8000`, `audio/x-l16;rate=16000`, `audio/x-l16;rate=24000`; mu-law is 8 kHz only |
| `statusCallbackUrl`, `statusCallbackMethod` (`status_callback_url`, `status_callback_method`) | Your URL, `POST` | The only push signal for drops |
| `streamTimeout` (`stream_timeout`) | Omit (86400 s) | When it fires the stream stops and, with nothing after it, the call ends 4010. 300 or 600 s cuts real conversations |
| `audioTrack` (`audio_track`) | Omit (`inbound`) | `outbound` and `both` are not allowed with `bidirectional="true"` |
| `extraHeaders` (`extra_headers`) | `k1=v1,k2=v2`, no secrets | Max 512 bytes; arrives in `start` as `extra_headers` and is logged. The page also prints a `[A-Za-z0-9]` constraint that its own example (`userId=12345,sessionId=abc123`) breaks; keys with `_` or `-` are seen to work, so test unusual characters rather than reject them |
| `noiseCancellation`, `noiseCancellationLevel` (`noise_cancellation`, `noise_cancellation_level`) | Omit, or `"true"` with 60 to 100 (default 85) | Filters the caller's audio |

Docs: `plivo docs show voice-agents/audio-streaming/xml/stream` (<https://plivo.com/docs/voice-agents/audio-streaming/xml/stream>); the 24 kHz value is on `plivo docs show voice/xml/audio-streaming` (<https://plivo.com/docs/voice/xml/audio-streaming>).

Limits, from <https://plivo.com/docs/voice-agents/audio-streaming/concepts/audio-streaming-guide> and the best-practices page: a stream URL of 2048 characters; one stream per call; WebSocket messages up to 64 KB, audio chunks of 16 KB base64 or less; about 20 ms of audio per `media` frame; a playback buffer of 40 s on the best-practices page (`DegradedStream` at 30, 60 and 90% full) and about 60 s in the guide. If the first connection fails, Plivo tries twice more, then drops the stream. Plivo closes the socket when the call ends.

Plivo sends JSON text frames, never binary:

| `event` | Key fields |
|---|---|
| `start` | Once: `sequenceNumber` (from 1), `start.callId`, `start.streamId`, `start.accountId`, `start.tracks`, `start.mediaFormat.encoding`, `start.mediaFormat.sampleRate`, `extra_headers` |
| `media` | `streamId`, `media.track`, `media.timestamp`, `media.chunk`, `media.payload` (base64 raw audio, no WAV header) |
| `dtmf` | `dtmf.digit` (`0-9`, `*`, `#`, `A-D`), `dtmf.track`, `dtmf.timestamp` |
| `playedStream` | `name` of a checkpoint that has played |
| `clearedAudio` | `streamId`, after your `clearAudio` |

The voice agent sends:

| `event` | Body |
|---|---|
| `playAudio` | `media.contentType` (`audio/x-mulaw` or `audio/x-l16`, no `;rate=`), `media.sampleRate` matching the stream, `media.payload` (base64 raw audio, never a WAV or MP3 container) |
| `checkpoint` | `streamId`, `name`; `playedStream` comes back when playback reaches it |
| `clearAudio` | `streamId`; drops the queued audio (barge-in) |
| `sendDTMF` | `dtmf`, a `0-9*#A-D` string |

- Read the codec from `start.mediaFormat`, not from the XML you think you returned.
- The protocol reference documents no JSON `stop` event, although the Stream page lists "Stop": end on the socket close, and accept a `stop` if one arrives.
- Send audio at real-time pace. Bursting fills the buffer (`buffer_overflow`) and makes `clearAudio` late; use `checkpoint` to learn when a sentence has played.
- The Stream page's example quotes `sampleRate` as a string and the reference shows a number: test what your SDK sends.
- `extra_headers` is metadata, not authentication. Only noise cancellation is documented, not echo cancellation: if the voice agent hears itself, gate STT while it speaks.
- Keep one state machine per `streamId`, and bound both queues (caller audio in, voice agent audio out). When the voice agent falls behind, drop or fail on purpose rather than let the delay grow.
- Time to first word: answer, then Plivo fetches the answer URL, parses the XML, opens the socket and sends `start`, then your first `playAudio`. Read the first steps in the console call debug log and the last in your own log. Plivo publishes no figure for this setup time. The overview's latency budget is per reply, not for setup: under 1 s end to end, with the WebSocket round trip under 100 ms, speech-to-text under 200 ms, the model under 500 ms and text-to-speech under 200 ms (`plivo docs show voice-agents/audio-streaming/overview`, <https://plivo.com/docs/voice-agents/audio-streaming/overview>).
- `ForwardedFrom` (the original number on a forwarded call, when the carrier sends it) arrives on the answer URL request, not in the `start` event.
- Full schemas: `plivo docs show voice-agents/audio-streaming/concepts/audio-streaming-reference` (<https://plivo.com/docs/voice-agents/audio-streaming/concepts/audio-streaming-reference>).

Stream status callbacks: the troubleshooting and best-practices pages and the console debug logs use the `Event` values `StartStream`, `StopStream`, `DroppedStream` and `DegradedStream`; the guide and the reference describe `started`, `stopped` and `failed`. Log the raw value. Common fields: `CallUUID`, `StreamID`, `Timestamp`, `From`, `To`, `Direction`. Acknowledge with a 200 (any body). Symptoms and error strings (`connection_failed`, `authentication_failed`, `invalid_content_type`, `buffer_overflow`): `plivo docs show voice-agents/audio-streaming/troubleshooting/troubleshooting` (<https://plivo.com/docs/voice-agents/audio-streaming/troubleshooting/troubleshooting>).

To start a stream over REST instead of XML: `plivo voice calls streams start <call_uuid> --url wss://... --bidirectional --content-type "audio/x-mulaw;rate=8000"`. Quote the content type (a bare `;` ends the shell command) and always pass it (the CLI default is `audio/x-l16;rate=16000`). Its `--stream-status-callback` sends `stream_status_callback_url`, a field the API does not document, so for status callbacks use `plivo api POST /Call/<call_uuid>/Stream/ --body '{"service_url":"wss://...","bidirectional":true,"content_type":"audio/x-mulaw;rate=8000","status_callback_url":"https://..."}' --dry-run`, then `--yes`.

Plivo moves raw audio only: STT, the model and the voice are yours, and Plivo TTS exists only as `<Speak>` before or after the stream. A streamed call is billed as the underlying call; quote no prices.

## Callbacks, signature validation, timeouts

- Answer, fallback and `action` URLs must return Plivo XML; `hangup_url`, `ring_url`, `callbackUrl` and `statusCallbackUrl` need only a 200. Plivo retries, so make handlers idempotent: the keys are listed in `plivo docs show voice/concepts/callbacks` (<https://plivo.com/docs/voice/concepts/callbacks>).
- Per-URL timeouts, retries and edge region go in a URL fragment such as `#ct=2000&rt=5000&rc=2&er=mumbai`: `plivo docs show voice/concepts/callback-configurations` (<https://plivo.com/docs/voice/concepts/callback-configurations>). A `<Stream>` `statusCallbackUrl` is not among the URLs it applies to. Still answer with XML well inside 15 s.
- Every Plivo request carries `X-Plivo-Signature-V3`, `X-Plivo-Signature-Ma-V3` and `X-Plivo-Signature-V3-Nonce`. Validate with your SDK's helper (Python `plivo.utils.validate_v3_signature`, Node `plivo.validateV3Signature`, Ruby `Plivo::Utils.valid_signatureV3?`, PHP `validateV3Signature`, Java `Utils.validateSignatureV3`, Go `plivo.ValidateSignatureV3`): `plivo docs show voice/concepts/signature-validation` (<https://plivo.com/docs/voice/concepts/signature-validation>). Where the page's prose or worked example disagrees with the SDK, follow the SDK. With several active Auth Tokens a header holds a comma-separated list: accept any match. V3 is signed with the token of the account or subaccount that owns the number, Ma-V3 with the main account's.
- The WebSocket upgrade carries the same headers. Validate it as a `GET` over your stream URL with `wss://` replaced by `http://`, keeping the port and query as dialled: on a live call only that form matched, not `wss://` or `https://`.
- Behind a proxy, validate against the URL Plivo dialled (its public host, port, path and query, and `https://` for HTTP callbacks), not a rewritten private one. If validation fails and you return no XML, the call ends 7011 or 8011, so reject only what is surely not Plivo's. Never log the token.
- Before go-live, test the validation: a real GET and a real POST pass; a changed form value fails; a changed host, scheme, port or path fails; a missing signature or nonce fails; both tokens pass while you rotate them; a repeated callback has no second side effect; the token never reaches your logs.
- Before recommending a Plivo stream or SDK package, check it is published for the user's language on that language's package index; otherwise use the raw protocol, which is fully documented.
- Firewalls: Plivo calls your URLs from regional edge IPs, listed on the firewall page ("Hand off to a human"). Without an allow-list, rely on signatures.

## India: what a voice agent needs before its first call

Never infer country rules from a phone prefix; check the account. Report each item as observed, not known or failed:

1. **Organisation.** An India data-region organisation; the region is fixed at creation. The docs disagree on how: `rent-india-numbers` says the console's organisation switcher (no new signup), `india-calling` a second account with another email. Only India-registered businesses can rent Indian numbers and call on domestic routes; INR accounts can call only within India.
2. **KYC.** An `accepted` compliance application: `plivo numbers compliance list --country IN --status accepted -o json`. `submitted` is not `accepted`. A number's `compliance_status` in `numbers get` is not in the published schema, so treat a missing field as not known.
3. **Series.** Landline (022, 080) for service and transactional calls; 140-series for promotional calls only; 160-series for BFSI only. The wrong series makes every complaint count as UCC, even with consent. 140 and 160 numbers are provisioned offline (Tata DLT registration, a NOC, header and template approval, days to weeks, no CLI): `plivo docs show voice/concepts/140-series-provisioning` (<https://plivo.com/docs/voice/concepts/140-series-provisioning>) and `plivo docs show voice/concepts/160-series-provisioning` (<https://plivo.com/docs/voice/concepts/160-series-provisioning>). Which series allow `<Stream>` is not documented.
4. **Media anchoring.** Both legs and your voice agent's server stay in India, or the call fails with 2070.
5. **Consent.** Cold calling is prohibited. TRAI counts a pre-recorded or AI-generated voice call as A2P. Service calls (about what the customer already has) and transactional ones (within 30 minutes of their transaction) need no explicit consent; one that supports an ongoing purchase or use needs it, valid 7 days (renewable) or until revoked. A UCC complaint needs opt-in proof within 5 business days: the business logo, the complainant's number, and an opt-in date within 6 months of the complaint. No proof in time blocks the compliance ID for 15 days (valid proof lifts it). 5 or more complaints from different people in any 10 days suspends it for 15 days; a second such violation means TRAI blacklisting, with all telecom resources in India barred for a year. Numbers on a suspended application cannot place calls. Remove complainants at once. Rules: `plivo docs show voice/concepts/ucc-management` (<https://plivo.com/docs/voice/concepts/ucc-management>). Complaints are readable with `plivo api GET /Ucc/`; proof is a multipart upload the CLI cannot send, so upload it on the console UCC dashboard.
6. **Capacity.** Professional accounts start at 50 concurrent calls. Over the limit a call is rejected at once with 5030 and Make Call returns HTTP 403. Raise the limit with Request Enterprise under Organization settings > Account limits before a campaign: `plivo docs show voice/concepts/account-limits` (<https://plivo.com/docs/voice/concepts/account-limits>).
7. **Caller ID.** A Plivo-rented Indian number; Verified Caller ID does not apply in India.

Before the first call, write down each item with where you saw it; anything you cannot show is not known:

1. The organisation and its data region (console only).
2. The legal entity behind the KYC (India-registered, or a reseller acting for one).
3. The caller ID, rented on this account (`plivo numbers get <number> -o json`).
4. The compliance application's id and `accepted` status.
5. The call purpose (service, transactional, promotional, BFSI) and a number series that matches it.
6. The consent record, and a check against complainants and opt-outs.
7. Where both legs and the voice agent's server are, each in India.
8. Approved 140 or 160 header and template references, where they apply.
9. Who owns the rollback, and who handles UCC complaints.

Eligibility and calling rules: `plivo docs show voice/concepts/india-calling` (<https://plivo.com/docs/voice/concepts/india-calling>).

### KYC from the CLI

You run the commands; the user supplies the certificate files and the exact legal details, and says "go" before anything is filed. Never fill in a value yourself.

```bash
plivo numbers compliance requirements --country IN --number-type local --user-type business -o json   # what to supply, now
plivo numbers compliance create --data @app.json --file 'documents[0].file=@cert.pdf' --dry-run       # then --yes -o json after "go"
plivo numbers compliance get <compliance_id> --expand documents -o json   # poll: 080/022 review is automated, typically about 5 minutes
plivo numbers buy <number> --dry-run                                       # accepted: direct brands get it attached at purchase
plivo numbers compliance link --link +91XXXXXXXXXX=<compliance_id> --dry-run   # numbers you already had
```

- `requirements` is the source of truth: one document, and one `--file`, per returned type. The pages disagree on the count, so never hard-code it. A business PAN alone is not accepted, the same file in two slots is rejected, and the first application must be sealed and signed by an authorised signatory.
- `app.json`: copy the India example under "Create" in `plivo docs show numbers/compliance` (<https://plivo.com/docs/numbers/compliance>). The legal name goes in `end_user.name` and `data_fields.business_name` exactly as printed on the certificate; the CIN, Udyam number or GSTIN comes from the documents. Resellers file one application per customer, named in `alias`.
- `rejected`: read `rejection_reason`, fix it, and run `plivo numbers compliance update` (valid only on `rejected`; it replaces every document, so re-attach every file).
- `compliance_application_id is required` on `buy` (a reseller, or nothing to attach): `numbers buy` has no flag for it, so `plivo api POST /PhoneNumber/<number>/ --body '{"compliance_application_id":"<compliance_id>"}' --dry-run`, then `--yes`.
- Exact errors, as Plivo returns them: `from parameter is invalid. +91... is not associated with your account` (the caller ID is not rented here); `Compliance application must be in 'accepted' status. Current status: 'submitted'.` (too early: keep polling); `Compliance application must be in 'rejected' status` (`update` works only on `rejected`); `Number not found on your account. Only rented numbers can be linked.`; `from number +91... cannot place calls as its compliance application is 'suspended'` (unresolved UCC complaints: upload proof on the console UCC dashboard).
- `calls make` from a number whose application is not `accepted` fails with `cannot place calls as its compliance application is not in 'accepted' status`; `suspended` means unresolved UCC complaints.

Documents, statuses and rejection reasons: `plivo docs show numbers/rent-india-numbers` (<https://plivo.com/docs/numbers/rent-india-numbers>).

## Outbound

- Prove inbound, or at least the step 5 checks, first.
- Answering-machine detection is asynchronous: `calls make --machine-detection true` (or `hangup`) delivers `Machine=true` to `machine_detection_url` after your `<Stream>` has started. The CLI has no flag for that URL or the timing parameters, so use `plivo api POST /Call/` (preview first). On a machine, hang up, transfer to a voicemail URL, or tell the voice agent. Parameters: `plivo docs show voice/concepts/machine-detection` (<https://plivo.com/docs/voice/concepts/machine-detection>).
- US: use a Plivo number rented on this account as the caller ID; it is the only way to get STIR/SHAKEN attestation A. Do not hang up on every voicemail: short calls count against the quality thresholds and draw surcharges. Calls above your CPS are queued, so pace your own requests. Thresholds and CPS: `plivo docs show voice-agents/audio-streaming/deploy/us-call-quality-and-cps` (<https://plivo.com/docs/voice-agents/audio-streaming/deploy/us-call-quality-and-cps>); concurrency: the account-limits page above.
- Professional plans can call only the US and India; other countries need Enterprise, and barred destinations fail with 2030: `plivo docs show voice/concepts/geo-permissions` (<https://plivo.com/docs/voice/concepts/geo-permissions>).
- A US-region trial organisation may see "Voice capability is currently disabled for this account" on outbound calls: request access in the console (not in the docs).
- Size CPS from your peak: concurrent calls divided by the average call length in seconds (100 three-minute calls at once need under 1 CPS). Queued calls dial late, which hurts appointment-window calls. The API itself allows 300 requests per 5 seconds, then answers 429.
- Have the voice agent say who it is and why it is calling in its first sentence: people who hang up in the first seconds create short calls.
- Verified Caller ID (showing a number you own outside Plivo) and Caller Reputation (US spam-label registration) have no CLI commands: use `plivo api` with `/VerifiedCallerId/`, and `enable_caller_reputation` on the 10DLC profile. 10DLC registration itself is for SMS, not voice. Docs: `plivo docs show voice/concepts/verified-caller-id`, `plivo docs show voice/concepts/caller-reputation`.
- Measure your own answer and connect rates by destination, list and time window.

## When this skill does not have the answer

Do not guess from other platforms. Search the docs first, which needs no login: `plivo docs search <keywords>`, then `plivo docs show <path>`. When piped, `plivo docs show` prints a JSON envelope, so add `-o table`. In `plivo docs search`, quoted words match as a phrase; unquoted words must all appear. Next, `plivo ask "<question>"`, which can also see the account; it shares the small rate limit with `diagnose` (on `RATE_LIMITED`, wait as told). Treat its answer as evidence and prefer the docs where they conflict. Without the CLI, ask the assistant in the Plivo console. The docs do not answer the price of a streamed call, Plivo's setup latency, data retention, HIPAA or BAA, which India series allow `<Stream>`, `<Stream>` on SIP-trunk legs, or the longest accepted answer URL: say so and route to Plivo support or the account manager. Out of scope: SIP-connected agent platforms (`plivo skill install sip-trunking`), Plivo's hosted AI Agents, SMS, browser and SIP endpoints, and the inside of the voice agent.
