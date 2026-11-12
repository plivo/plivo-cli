---
name: plivo-tts
description: "Turns text into speech with Plivo Text-to-Speech (the TTS models API) from the Plivo CLI: picks a voice, synthesizes audio to a file (WAV, MP3, raw PCM, 32-bit float, or 8 kHz mu-law and A-law for phone calls, raw or in a WAV file), applies pronunciation lexicons or inline rules, checks usage, and shows how to stream live speech over the realtime WebSocket. Use when someone wants to convert text to speech, generate or synthesize audio, pick a Plivo voice or language (English, Hindi, Bengali, Tamil, Telugu, Marathi, Gujarati, Kannada), make telephony audio for a call or a voice agent, fix how a word is pronounced, or build a realtime TTS socket. Not for speech spoken inside a live call by Voice XML (plivo-voice-xml), or for wiring a voice agent to calls (plivo-audio-streaming)."
license: Apache-2.0
---

# Plivo Text-to-Speech: make audio from text with the CLI, or stream it live

Take a developer, or their coding agent, from a Plivo account to speech audio: a voice picked, a file synthesized in the format the destination needs, custom pronunciations where a word is said wrong, and, for a live voice agent, a realtime WebSocket. Batch (one request, one complete file) is a CLI command. Realtime is a WebSocket you write in code. This file covers Plivo's TTS API (the Koyal model), not the voice agent around it. Related skills, if installed: `plivo skill install audio-streaming` (a voice agent on real calls), `plivo skill install voice-xml` (Plivo XML, including the `<Speak>` element), `plivo skill install` (the CLI's own skill). Without them, use `plivo <command> --help`.

## Rules for every step

- Use the CLI for every Plivo step and `-o json` to read values. `plivo <command> --help` wins on which commands and flags exist; this file wins on how Plivo behaves. Never invent a flag: where the CLI has none, say so. Realtime is one such case (use the WebSocket below), and before v1.3.4 inline pronunciations were another (use a lexicon).
- Track the steps in your agent's own task or todo tool: TaskCreate and TaskUpdate in Claude Code (TodoWrite in older versions), the plan tool in Codex CLI. Mark a step done only when its check passes. With no task tool, print the step list, and print it again with ticks after each step.
- Ask every question with your agent's own structured question tool. In Claude Code it is AskUserQuestion: one call takes 1 to 4 questions with 2 to 4 options each, and it adds a free-text "Other" option itself, so do not add one. Without such a tool, ask a numbered list with an "Other" choice and wait for the answer.
- Synthesis is billed per character, so it is a write: two commands, never one. First `plivo tts speak ... --dry-run` (prints the request, sends nothing), then, after you show the preview and the character count and the user agrees, the same command with `--yes`. Without either flag the CLI refuses. `plivo tts lexicons delete` also needs `--yes`, after a `--dry-run`. `plivo tts lexicons create` is free and writes at once, so its `--dry-run` preview is the only check: show it and ask before you run it for real. If a command rejects `--dry-run`, show the current state from `list` or `get` instead and ask.
- Check the credit before every synthesis: `cash_credits` in `plivo auth whoami -o json`. At 0 or less, tell the user to add credits on the console home page, <https://cx.plivo.com/home>, and stop before the billed step. The docs do not say what a request returns on an empty balance, so never rely on the error to find out.
- Never read, print or ask for the Auth Token in the chat: the user writes it and the Auth ID into `.env` with an editor, and the realtime script reads that file when run with `uv run --env-file .env`. Never read, cat or print `.env`. The `--dry-run` URL carries the Auth ID in its account segment, so when you show a preview, mask it: keep the first 4 and last 3 characters. Never run `plivo login` yourself (step 1).
- Report each step as observed, not known or failed. Observed means you saw the evidence in this session. You cannot hear audio: a file with the right size and type is not proof that it sounds right, so say what you checked and ask the user to listen.
- Verdicts: **will break** when a documented rule or an observed failure says so (name the failure); **risky** when plausible or untested (say what to check); **style** when nothing changes. A rule here with no label is risky.

## Talking to the user

- Say "voice" for a `voice_id` and "language" for its language. Keep CLI words out of questions and the report: dry-run, `voice_id`, mu-law, sample rate, WebSocket. The questions written out in the steps are already right, and commands and their previews stay as they are.
- Show character counts, never prices: the docs give no price, so quote none. Billing is per character, and that is all you say about cost.
- Keep each question to one or two short sentences: what happens, and what it changes. Question headers: Language, Use, Voice, Synthesize, Listen.

## Steps 1 to 6: from the account to a checked audio file

Create these six tasks first, then run the steps in order. Stop at the first failed check and fix it before you go on. Only step 5 and the realtime script (below) cost money.

1. Check tools and login
2. Check the credit
3. Ask what to say and where it plays
4. Pick a voice
5. Synthesize
6. Check the file and report

### Step 1: check tools and login

```bash
plivo --version                  # if missing: brew install plivo/tap/plivo, or the install.sh one-liner from the Plivo docs
plivo auth whoami -o json        # exit 2 with AUTH_MISSING: the user logs in (below), then run this again
plivo tts voices --help          # missing: the CLI is too old for this skill; upgrade it (plivo upgrade)
```

Check: `whoami` shows the account the user expects, and `plivo tts` exists.

- Login opens a browser, so the user runs it. On their own computer: `plivo login` in a terminal (in Claude Code, `! plivo login`). In a container, over SSH or in WSL, the browser cannot reach the CLI: the user runs `plivo login` in a terminal on that machine, approves in the browser, then copies the full URL from the browser's address bar and pastes it into that terminal.
- Wrong organization: `plivo auth list`, then `plivo auth use <profile>`. Sub-accounts use their own Auth ID and Auth Token, and the main account is billed.

### Step 2: check the credit

Check: `cash_credits` in the step 1 output is above 0. If it is not, send the user to <https://cx.plivo.com/home> to add credits, and read `whoami` again after they say it is done.

### Step 3: ask what to say and where it plays

Ask in plain text what the speech should say (or which file holds it). Then ask, in one structured call:

- Language: infer it from the text, and ask only if it is unclear. Ask "Which language is the text in?" with English, Hindi and Detect from the text (recommended); the tool's own free-text Other covers Bengali, Tamil, Telugu, Marathi, Gujarati and Kannada. Codes: `en`, `hi`, `bn`, `ta`, `te`, `mr`, `gu`, `kn`. Each voice speaks one language, so this decides the voice list. For Detect from the text, leave `--language` out (it defaults to `auto`) and use the language you inferred to filter the voices in step 4.
- Use: "Where will the audio play?" Play on a call with Voice XML, Stream to a voice agent on Plivo, A file or web.

Each answer to Use is one row, and sets the format:

| Use | `--format` | `--sample-rate` | Extra | File |
|---|---|---|---|---|
| Play on a call with Voice XML | `wav` | `8000` or `16000` | has a header; host it and play it with `<Play>` (see "Use the audio on a call"). **Risky:** not tested against the API | `.wav` |
| Stream to a voice agent on Plivo | `mulaw` | `8000` | raw, no header; see "Use the audio on a call" | `.ulaw` |
| A file or web | `mp3` | `44100` | `--bitrate 128000` (the default at 44100 Hz) | `.mp3` |

If the user picks Other, for their own SIP or telephony stack, ask which format it takes. The CLI also makes `alaw` at 8000 Hz (raw, `.alaw`) and `pcm` at 16000 or 24000 Hz (raw 16-bit, `.pcm`), but no Plivo path plays a raw file, so use them only with that stack. From v1.3.4 the CLI also makes `f32` (raw 32-bit float samples, 24000 Hz by default), `wav-f32` (the same samples in a WAV file), and `wav-mulaw` and `wav-alaw` (8000 Hz G.711 in a WAV file). Use `f32` and `wav-f32` only for a pipeline that wants float samples. Use raw `mulaw` or `alaw` for streaming and `playAudio`, and `wav-mulaw` or `wav-alaw` only for a file someone plays. **Risky:** G.711 WAV is untested with `<Play>`: keep `wav` (16-bit) for it.

Without `--format` the CLI picks the format from the `--out` extension (`.wav`, `.mp3`, `.ulaw` or `.mulaw`, `.alaw`, `.pcm` or `.raw`) and falls back to `wav` at 24000 Hz. The v1.3.4 names (`f32`, `wav-f32`, `wav-mulaw`, `wav-alaw`) have no extension and are never picked from one, so a `.wav` name still means plain `wav`: pass `--format`. Pass `--format` anyway and name the file for it (`.wav` for the three `wav-*` formats, `.f32` for `f32`): a raw file with a `.wav` name does not play. **Risky:** a sample rate that does not fit the destination, for example 24000 Hz into a phone call. From v1.3.4 the CLI refuses, before it sends anything, a `--sample-rate` the format does not offer: 8000, 16000, 22050, 24000, 32000, 44100 or 48000 for `wav`, `pcm`, `f32` and `wav-f32`, and 8000 for `mulaw`, `alaw`, `wav-mulaw` and `wav-alaw`.

### Step 4: pick a voice

```bash
plivo tts voices --language hi -o json     # add --gender male|female to narrow it
plivo tts voices -o json                   # every voice
```

Each voice has `voice_id`, `name`, `gender`, `languages`, `accent_type`, `description` and, when Plivo has set one, `sample_url`, a link to a short sample (the CLI leaves it out while it is empty). When the user wants to hear voices before choosing, show the `sample_url` of each option that has one, or point them to Voice Studio in the console (https://cx.plivo.com/voice-studio/explore). The list holds Plivo's public voices and any voices the organization owns, so take it from the CLI and do not hardcode one. Public voice IDs start with `r4_` (for example `r4_riya` speaks Hindi and `r4_ashley` speaks English); a voice marked legacy still works, but prefer the others.

Ask "Which voice?" (header Voice) with up to three options, each "<name>: <gender>, <accent>" for a voice whose `languages` contain the language. Check: the chosen `voice_id` appears in the list with that language. **Will break:** text in a language the voice does not speak, which returns `400 language_not_supported`.

### Step 5: synthesize

Put the text in the command, or in a file for anything long (`--file text.txt`). `--file -` reads stdin once, so keep it for a one-shot pipe, never for the `--dry-run` then `--yes` pair: the second command would find nothing to read. The limit is 5,000 characters per request: for more, split at sentence ends and synthesize each part, and ask the consent question below once, for the total number of characters across all the parts. Long text takes longer to synthesize: pass a longer global `--timeout` in seconds (the default is 30), for example `--timeout 120`. Raw formats (`mulaw`, `alaw`, `pcm`, and from v1.3.4 `f32`) join byte for byte; `wav`, `mp3` and the v1.3.4 `wav-*` formats need a tool such as `ffmpeg`.

```bash
plivo tts speak --voice r4_riya --language hi --text "आपका पैकेज कल पहुंचेगा।" \
  --format mulaw --sample-rate 8000 --out reminder.ulaw --dry-run
```

The preview prints the character count on its first line (`[dry-run] <N> characters, ...`): quote that number and do not count bytes yourself. Show the preview, then ask: "Synthesize <N> characters now? Plivo bills per character." Synthesize / Not now (header Synthesize). On yes, take a reading with `plivo tts usage -o json`, then run the same command with `--yes` in place of `--dry-run`.

If a billed run fails with any `UPSTREAM_*` code (`UPSTREAM_TIMEOUT`, `UPSTREAM_UNAVAILABLE`, and `UPSTREAM_ERROR`, which includes a 200 answer that is not audio), with `NETWORK_ERROR`, or with an error whose hint says the request was or may have been billed, do not run it again. In those cases ignore `retryable: true` and any "retry" hint: the request may have been billed. Read `plivo tts usage -o json` again and compare it with the reading from before the send. If it did not change and the failure was a 502 or 503 (`backend_error`, `at_capacity`, `draining`, `auth_unavailable`), the request was refused before it ran: wait a second and send it once more. In every other case, such as a timeout or a network error, tell the user what changed (or that nothing did) and ask before you send again, because a request that timed out may still finish and bill. Take a usage reading before every billed run.

Optional flags:

| Flag | Range | Use |
|---|---|---|
| `--language` | `auto`, `en`, `hi`, `bn`, `ta`, `te`, `mr`, `gu`, `kn` | Default `auto`; the voice must speak it |
| `--pace` | 0.7 to 1.2 (default 1.0) | Speaking speed |
| `--volume` | 0.5 to 2.0 (default 1.0) | Loudness |
| `--lexicon <id>` | a saved lexicon, `lx_...` | Custom pronunciations (below) |
| `--seed N` | integer | Same text, voice and seed give identical audio, for tests |
| `--text-normalization=false` | v1.3.4+; write the `=` | Read numbers and dates as written, not as words |
| `--pronounce word=value` | v1.3.4+; up to 100, needs `--pronounce-kind` | Inline pronunciation rules for this request (below) |

Numbers, currency and dates are normalized for you ("1,250" is spoken as an amount, not digits). From v1.3.4, `--text-normalization=false` turns that off, for text already written as words or a rule that needs the raw token; `--text-normalization false` fails, so write the `=`. The docs do not say whether normalization or rule matching comes first. `--out -` writes the audio to stdout for a pipe; the CLI refuses to print audio to a terminal.

Check: the command exits 0 and `--out` exists.

### Step 6: check the file and report

```bash
ls -l reminder.ulaw      # bytes: mulaw and alaw at 8000 Hz are 8000 bytes per second; 24000 Hz pcm is 48000, f32 (v1.3.4+) 96000
ffprobe -v error -f mulaw -ar 8000 -show_entries format=duration -of csv=p=0 reminder.ulaw   # seconds; alaw: -f alaw; pcm: -f s16le -ar <rate>; f32: -f f32le -ar <rate>
```

To listen to a raw file, the user runs `ffplay -autoexit -f mulaw -ar 8000 reminder.ulaw`. For a `.wav` or `.mp3`, `file out.wav` reports "RIFF ... WAVE audio" (or MP3 audio) and `afplay out.wav` plays it on macOS (`ffplay -autoexit` elsewhere). Use `file` and `afplay` for those two only: a raw file has no header to report. The v1.3.4 `wav-f32`, `wav-mulaw` and `wav-alaw` files are WAV files too, and `ffprobe -v error -show_entries stream=codec_name,sample_rate -of csv=p=0 out.wav` prints their encoding (`pcm_f32le`, `pcm_mulaw` or `pcm_alaw`) and rate.

Report: the file path, its size, the format flags you used, and the character count. Ask "Does it sound right?" (header Listen), since you cannot hear it. Then read `plivo tts usage -o json` again and compare it with the reading from before the run (see "Billing and usage").

## Pronunciation and lexicons

A lexicon is a saved list of rules that change how words are said. Use one when a brand, name or term comes out wrong. Before v1.3.4, `plivo tts speak` has no inline-rule flag: save the rules as a lexicon, then pass `--lexicon`. From v1.3.4, `--pronounce word=value` (repeatable) with one `--pronounce-kind` gives the rules for a single request, with no lexicon to save or delete (below).

| `--kind` | A rule's `value` is written as |
|---|---|
| `respell` | plain respelling, for example `plee-voh` |
| `ipa` | International Phonetic Alphabet |
| `arpa` | ARPAbet |

```bash
# rules.json: [{"word": "Plivo", "value": "plee-voh"}]
plivo tts lexicons create --name brands --kind respell --file rules.json --dry-run   # preview, then run it again without --dry-run
plivo tts lexicons create --name brands --kind respell --rule "Plivo=plee-voh"       # or the rules inline; --rule repeats
plivo tts lexicons list -o json
plivo tts lexicons get lx_... -o json
plivo tts speak --voice r4_ashley --text "Welcome to Plivo" --format mp3 --sample-rate 44100 \
  --bitrate 128000 --lexicon lx_... --out welcome.mp3 --dry-run
plivo tts lexicons delete lx_... --dry-run      # then --yes, after the user agrees
```

`--file` and `--rule` can be combined: the rules are merged, and `--rule` repeats. The create command prints the lexicon with its `lexicon_id` (starts with `lx_`): record it. Limits: 100 lexicons per organization, 100 rules per lexicon, a name up to 100 characters, a `word` up to 100 and a `value` up to 120. **Will break:** synthesizing with a deleted `lexicon_id` returns `422 invalid_request` with `param: lexicon_id`; `get` or `delete` on an unknown ID returns `404 not_found`. Deleting a lexicon cannot be undone: ask first.

Inline rules, from v1.3.4:

```bash
plivo tts speak --voice r4_ashley --text "Welcome to Plivo" --pronounce "Plivo=plee-voh" \
  --pronounce-kind respell --format mp3 --out welcome.mp3 --dry-run     # the preview shows the pronunciations
```

`--pronounce-kind` (`ipa`, `arpa` or `respell`, lowercase) says how every `--pronounce` value is written. It is required with `--pronounce` and has no default, and the CLI refuses either flag without the other. A request takes up to 100 rules, and the CLI refuses inline rules together with `--lexicon`: use one or the other. **Risky:** the docs do not say how a rule matches (case, whole words only, a word of several words, a word given twice), nor which rule wins when the same word is given twice. The rules are tested in English only. Ask the user to listen for the word, and say that you could not check it.

## Use the audio on a call

- **Play a file.** Synthesize `wav` (`--sample-rate 8000` or `16000`) or `mp3`, host it at an HTTPS URL, and play it from the answer XML with `<Play>https://.../message.mp3</Play>`. `<Play>` takes mp3 or wav up to 10 MB, 8 or 16 kHz mono recommended; it does not take raw mu-law. A raw `.ulaw` file is for the stream below. To speak plain text inside a call with no file, use the `<Speak>` element instead (`plivo skill install voice-xml`).
- **Send it down an audio stream.** For a voice agent on `<Stream>` with `bidirectional="true"` and `contentType="audio/x-mulaw;rate=8000"`, synthesize raw mu-law at 8000 Hz (`--format mulaw --sample-rate 8000`). Base64 the bytes in pieces of 12,000 raw bytes or fewer (16,000 base64 characters, 75 frames of 160 bytes) and send each as `{"event": "playAudio", "media": {"contentType": "audio/x-mulaw", "sampleRate": 8000, "payload": "<base64>"}}`. Send at real-time pace: bursting overflows the playback buffer (see the audio-streaming skill). The stream's XML, events and debugging are in `plivo skill install audio-streaming`.
- **Live replies from a model.** Use the realtime WebSocket below with the same raw mu-law 8000 `output_format`, and forward each audio frame's bytes as a `playAudio` payload.

## Realtime: speak text as it is produced (WebSocket)

Use it when text arrives piece by piece, as a model writes it, and the audio must start before the text ends. It is not a CLI command. Endpoint: `wss://api.plivo.com/v1/Account/<auth_id>/TTS/Realtime/`, with the Auth ID and Auth Token as HTTP Basic auth on the upgrade request. A bad credential is refused with an HTTP status, so the socket never opens. There are no query parameters: every option goes in the first `text` message of a reply.

A runnable start. Save it as `realtime_tts.py`. The user puts `PLIVO_AUTH_ID`, `PLIVO_AUTH_TOKEN` and `VOICE_ID` in `.env`; never read, cat or print that file. Running the script bills per character, like step 5: do the credit check (step 2) and take a `plivo tts usage -o json` reading, then ask "Run a <N>-character test now? Plivo bills per character." (header Synthesize; the script as written sends 16 characters, so count the new text if you change it), and run it only on yes. `uv run --script` alone does not load `.env`, so run:

```bash
uv run --env-file .env --script realtime_tts.py
```

The script:

```python
# /// script
# requires-python = ">=3.10"
# dependencies = ["websockets>=14"]
# ///
import asyncio
import base64
import json
import os

import websockets

AUTH_ID = os.environ["PLIVO_AUTH_ID"]
BASIC = base64.b64encode(f"{AUTH_ID}:{os.environ['PLIVO_AUTH_TOKEN']}".encode()).decode()
URL = f"wss://api.plivo.com/v1/Account/{AUTH_ID}/TTS/Realtime/"


async def main():
    audio = bytearray()
    # websockets 14 or later: additional_headers (before 14 it was extra_headers)
    async with websockets.connect(URL, additional_headers={"Authorization": f"Basic {BASIC}"}) as ws:
        await ws.send(json.dumps({
            "type": "text",
            "context_id": "c1",
            "voice_id": os.environ["VOICE_ID"],
            "text": "Hello from Plivo",
            "finish": True,
            "output_format": {"container": "raw", "encoding": "pcm_mulaw", "sample_rate": 8000},
        }))
        async for message in ws:
            if isinstance(message, bytes):
                id_len = int.from_bytes(message[:2], "little")
                audio += message[2 + id_len:]
                continue
            event = json.loads(message)
            if event["type"] == "done":
                print("billed characters:", event.get("characters"))
                break
            if event["type"] == "error":
                raise SystemExit(f"{event.get('code')}: {event.get('message')} (param {event.get('param')})")
    with open("hello.ulaw", "wb") as f:
        f.write(audio)


asyncio.run(main())
```

Messages you send (JSON text frames, up to 64 KiB each):

| `type` | Fields | Meaning |
|---|---|---|
| `text` | `context_id`, `text` (up to 5,000 characters), `finish` (default `false`), `timestamps` (default `false`); on a reply's first message also `voice_id` (required), `language`, `output_format`, `pace`, `volume`, `text_normalization`, `pronunciations`, `lexicon_id`, `continuation`, `seed` | A new `context_id` opens a reply. Send the text in pieces, and `finish: true` on the last |
| `flush` | `context_id` | Speak everything received so far for that reply; the server answers `flush_done` |
| `cancel` | optional `context_id` | Stop that reply now; with no `context_id`, stop all replies |

Messages you receive:

| Frame | Meaning |
|---|---|
| binary | A 2-byte little-endian length, then that many bytes of `context_id`, then audio in that reply's `output_format` |
| `timestamps` | Character timings for audio already sent, only when you set `timestamps: true` |
| `flush_done` | A flush has been spoken |
| `done` | The reply's audio is complete; carries the billed `characters`. Sent once per reply |
| `warning` | A problem that does not stop the reply |
| `error` | `type`, `code`, `message`, `param`, `context_id`, `connection_id` |

`output_format` takes the same fields the batch request sends. These are the values behind each CLI `--format`; `--sample-rate` replaces the rate, and mp3 adds a `bitrate`, which only some rates accept (below the table):

| CLI `--format` | `container` | `encoding` | `sample_rate` |
|---|---|---|---|
| `wav` | `wav` | `pcm_s16le` | `24000` |
| `mp3` | `mp3` | `mp3` | `44100` |
| `mulaw` | `raw` | `pcm_mulaw` | `8000` |
| `alaw` | `raw` | `pcm_alaw` | `8000` |
| `pcm` | `raw` | `pcm_s16le` | `24000` |
| `f32` (v1.3.4+) | `raw` | `pcm_f32le` | `24000` |
| `wav-f32` (v1.3.4+) | `wav` | `pcm_f32le` | `24000` |
| `wav-mulaw` (v1.3.4+) | `wav` | `pcm_mulaw` | `8000` |
| `wav-alaw` (v1.3.4+) | `wav` | `pcm_alaw` | `8000` |

mp3 takes a bitrate only in these pairs: 44100 and 48000 Hz take 32000, 64000, 96000, 128000 or 192000; 24000 Hz takes 48000; 16000 Hz takes 32000 or 64000; 8000 and 22050 Hz take 32000. The CLI picks a valid default for the rate (128000, 48000, 64000 or 32000), so leave `--bitrate` out unless the user asks. A pair that is not offered returns `400 unsupported_format` and bills nothing.

Only `raw` `pcm_mulaw` at 8000 is exercised by the script above; **risky:** the other combinations are untested on the socket, among them the `wav` container over the socket and the wav-wrapped formats. A `seed` repeats the audio only with `continuation` set to `false`, which is what `--seed` sends. `pronunciations` takes the batch request's shape: an array of up to 100 `{"word": ..., "value": ..., "kind": "ipa" | "arpa" | "respell"}` objects, sent on a reply's first `text` message; **risky:** untested here, and how it combines with `lexicon_id` is not documented, so send one of them. `text_normalization` is a boolean (default `true`) with the batch request's meaning; **risky:** untested on the socket.

Rules that matter: keep reading the socket, because a client that reads nothing for 30 s is dropped. One socket carries up to 8 replies at once, each with its own `context_id`. The realtime messages use the same field names as the batch request, so `pace`, `volume` and `lexicon_id` mean what they mean for `plivo tts speak`; the batch-only `--file`, `--out` and `--format` flags have no counterpart. Omit an optional field rather than send `null`, which returns `422`. Close codes: `1013` is a retryable error (reconnect with a short, growing wait), `1011` is a server (5xx) error, `1008` is any other error (read the `error` message first). The India regional host `wss://in.api.plivo.com` serves the same paths; the docs do not say which accounts need it, so use it only when the default host refuses the account and the user confirms.

## Limits

| Limit | Value |
|---|---|
| Open connections per account | 50, then `429 concurrency_limit` |
| Text per request or `text` message | 5,000 characters |
| Replies (contexts) per socket | 8 |
| Request body | 1 MiB |
| WebSocket frame | 64 KiB |
| Slow reader | dropped after 30 s without reading |
| Pace, volume | 0.7 to 1.2, 0.5 to 2.0 |
| Lexicons per organization, rules per lexicon | 100, 100 |
| Inline pronunciations per request (v1.3.4+) | 100 |

## Billing and usage

Billing is per character: each `done` event and the usage report give the billed `characters`. Quote no price, because the docs give none. `plivo tts usage -o json` returns totals split by route: `http` is batch (`plivo tts speak`) and `live` is realtime. Each route has `requests` (batch requests or realtime replies), `characters` (billed), `spoken_chars` (characters spoken after normalization), `audio_seconds` and `tokens`. The report holds account totals with no time window, so what one run cost is the difference between a reading taken before the run and one taken after. Use it to check that a run cost what you expected, not to predict a bill.

## When a request fails

With the CLI, add `-o json`: the error envelope prints on stderr. Its `error.code` is the CLI's own code (`VALIDATION_ERROR`, `RESOURCE_NOT_FOUND`, `UPSTREAM_TIMEOUT` and so on), not Plivo's. Plivo's answer is at `error.context.upstream.body`: its `code`, the text in `error` (not `message`), a `param` when a field is at fault, and `api_id`. Read that body first, and key the table on `upstream.body.code`. These are the documented codes:

| `upstream.body.code` (HTTP status) | Meaning | Fix |
|---|---|---|
| `unauthorized` (401) | Bad or missing Auth ID or Auth Token | `plivo whoami`, then log in again |
| `forbidden` (403) | The credentials are not for the Auth ID in the request path | Use the account that owns the credentials |
| `voice_not_found` (404) | Unknown voice, or one the account may not use | `plivo tts voices` |
| `language_not_supported` (400) | The voice does not speak the text's language | Pick a voice from `plivo tts voices --language <code>`, or set `--language` |
| `empty_text`, `text_too_long` (400) | Nothing to speak, or over 5,000 characters | Send 1 to 5,000 characters; split longer text |
| `unsupported_format` (400) | That format, rate or bitrate pair is not offered; nothing is billed | Fix `--format`, `--sample-rate` and `--bitrate` (mp3 pairs are above) |
| `invalid_request` (422; 400 if not JSON, 413 over 1 MiB) | A bad value; `param` names the field (`lexicon_id` for a deleted lexicon). A `null` for an optional field also returns it | Fix that field, or omit it |
| `not_found` (404), `method_not_allowed` (405) | A wrong path or method, or a lexicon ID that does not exist | `plivo tts lexicons list` |
| `concurrency_limit` (429) | More than 50 open connections on the account | Close idle sockets, wait 1 s, then retry |
| `at_capacity`, `draining`, `auth_unavailable` (503) | Busy right now | Wait 1 s, then retry (see the billed-run rule in step 5) |
| `backend_error` (502) | The engine failed | Wait 1 s, then retry (same rule) |
| `internal_error` (500) | A bug on Plivo's side | Give support the `api_id` |
| `cancelled` (499) | The request was cancelled or the client left | Nothing to fix |

A realtime WebSocket client has no CLI envelope: its `error` frame carries `code`, `message` and `param` at the top level.

For any other code, show the user the whole `upstream.body` and do not guess a cause. To get help, give Plivo support `upstream.body.api_id` and `error.request_id`, with the UTC time and the command (not the text, if it is private). A direct HTTP or WebSocket client, such as the realtime script, has no envelope: every response carries a `nimble-connection-id` header that matches `api_id` in an error body, so give support that.

Check in this order: the voice and language pair (`plivo tts voices`); the text length (5,000 characters) and the `--pace` and `--volume` ranges; for mp3, the `--sample-rate` and `--bitrate` pair; `cash_credits`; then the error body.

## When this skill does not have the answer

Do not guess from other platforms. The docs for Plivo Text-to-Speech may not be published yet: try `plivo docs search text-to-speech` first (it needs no login), then `plivo docs show <path>` for a hit; when piped, add `-o table`. A hit counts only if it is about the TTS models API (voices, batch synthesis, lexicons, the realtime socket): `<Speak>`, SSML, voice use-case and integration pages are a different product, so ignore them. If nothing counts, this file is the reference. Next, `plivo ask "<question>"`, which can also see the account; treat its answer as evidence and prefer the docs where they conflict. The docs do not give a price, per-voice latency, data retention or a list of which accounts need the India host: say so and route to Plivo support or the account manager. Out of scope: speech to text, the model behind a voice agent, SIP trunking (`plivo skill install sip-trunking`), and the `<Speak>` element's own voices (`plivo skill install voice-xml`).
