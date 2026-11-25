---
name: plivo-tts
description: "Gives a Plivo user the right Text-to-Speech setup, from the Plivo CLI. It first asks what you are building: an audio file, prompts to play on a call, or a live voice agent. For files and prompts it picks a voice, synthesizes WAV, MP3, raw PCM or 8 kHz mu-law and A-law, applies pronunciation dictionaries and checks usage. For a voice agent it picks the audio format for Pipecat, LiveKit Agents or your own WebSocket code over Audio Streaming or SIP trunking, explains how to connect Plivo TTS to each, and covers barge-in, word timestamps, latency, Indian and mixed Hindi-English replies and characters per call. Use when someone wants text to speech, a Plivo voice, a realtime TTS socket or a voice for an AI agent. Not for speech inside a live call by Voice XML (plivo-voice-xml) or for connecting an agent to calls (plivo-audio-streaming, plivo-sip-trunking)."
license: Apache-2.0
---

# Plivo Text-to-Speech: make audio from text, or give a voice agent its voice

Take a developer, or their coding agent, from a Plivo account to speech that fits what they are building. The first question decides the path: an audio file (batch, one CLI command), prompts to play on a call, or a live voice agent (a realtime WebSocket that code on their side talks to). This file covers Plivo's TTS API, not the voice agent around it (speech to text, the model, the call). Related skills, if installed: `plivo skill install audio-streaming` (a voice agent on real calls through `<Stream>`), `plivo skill install sip-trunking` (calls over a SIP trunk), `plivo skill install voice-xml` (Plivo XML, including `<Speak>`), `plivo skill install` (the CLI's own skill). Without them, use `plivo <command> --help`. A voice agent whose test call does not work yet belongs to the audio-streaming or sip-trunking skill: finish it there, then come back.

## Rules for every step

- Use the CLI for every Plivo step and `-o json` to read values. `plivo <command> --help` wins on which commands and flags exist; this file wins on how Plivo behaves. Never invent a flag, field, error code, class, import or number: where the CLI has no flag, say so. Inline pronunciations are one such case for the CLI (use a pronunciation dictionary), and realtime is another (use the WebSocket below).
- Track the steps in your agent's own task or todo tool: TaskCreate and TaskUpdate in Claude Code (TodoWrite in older versions), the plan tool in Codex CLI. Mark a step done only when its check passes. With no task tool, print the step list, and print it again with ticks after each step.
- Ask every question with your agent's own structured question tool. In Claude Code it is AskUserQuestion: one call takes 1 to 4 questions with 2 to 4 options each, and it adds a free-text "Other" option itself, so do not add one. Without such a tool, ask a numbered list with an "Other" choice and wait for the answer. Never assume an answer: ask, even when the request seems to imply it.
- Synthesis is billed per character, so it is a write, and so is every test of a realtime reply: two commands, never one. First `plivo tts speak ... --dry-run` (prints the request, sends nothing), then, after you show the preview and the character count and the user agrees, the same command with `--yes`. Without either flag the CLI refuses. A realtime test (the script below, or a test call to a voice agent that speaks) has no `--dry-run`: before it, count the characters it will send, show that number, and ask "Run a test of about <N> characters now? Plivo bills per character." (header Run test). Billing consent comes before every billed call, and an earlier yes does not carry over to a later one. `plivo tts pronunciation-dictionaries delete` also needs `--yes`, after a `--dry-run`. `plivo tts pronunciation-dictionaries create` is free and writes at once, so its `--dry-run` preview is the only check: show it and ask before you run it for real. If a command rejects `--dry-run`, show the current state from `list` or `get` instead and ask.
- Check the credit before every billed step: `cash_credits` in `plivo auth whoami -o json`. At 0 or less, tell the user to add credits on the console home page, <https://cx.plivo.com/home>, and stop before the billed step. The docs list `insufficient_balance` (400) for an empty balance and `usage_limit_exceeded` (403) for a reached monthly usage limit, but check first so the user never meets the error mid-call.
- Never read, print or ask for the Auth Token in the chat: the user writes it and the Auth ID into `.env` with an editor, and code that needs them reads that file (`uv run --env-file .env`). Never read, cat or print `.env`. The `--dry-run` URL carries the Auth ID in its account segment, so when you show a preview, mask it: keep the first 4 and last 3 characters. Never run `plivo login` yourself (step 1).
- Report each step as observed, not known or failed. Observed means you saw the evidence in this session. You cannot hear audio: a file with the right size and type, or a `done` event with `reason: "complete"`, is not proof that it sounds right, so say what you checked and ask the user to listen.
- The framework guidance in this file was checked against Pipecat 1.17.0 and livekit-agents 1.8.13. Neither framework has a Plivo TTS service or plugin. Read the user's installed version (`uv pip show pipecat-ai`, or `livekit-agents`) and compare the names with that version's own Cartesia service; where a name differs, follow the installed version and say so.
- Verdicts: **will break** when a documented rule or an observed failure says so (name the failure); **risky** when plausible or untested (say what to check); **style** when nothing changes. A rule here with no label is risky.

## Talking to the user

- File and prompt branches: say "voice" for a `voice_id` and "language" for its language, and keep CLI words out of questions and the report: dry-run, `voice_id`, mu-law, sample rate, WebSocket. The questions written out in the steps are already right, and commands and their previews stay as they are.
- Voice agent branch: the user is a developer. Say Pipecat, LiveKit, WebSocket, PCM and mu-law freely, but start each question from what it means for the call.
- Show character counts, never prices: the docs give no price, so quote none. Billing is per character, and that is all you say about cost. Cost per call is given in characters per minute (step V8).
- Keep each question to one or two short sentences: what happens, and what it changes. Question headers: Build, Language, Voice, Synthesize, Listen, Framework, Calls, Test call, Latency, Run test, Interrupt.

## Steps 1 to 3: readiness and the first question (every branch)

Create these three tasks first, then run the steps in order. Stop at the first failed check and fix it before you go on. Nothing in steps 1 to 3 costs money.

1. Check tools and login
2. Check the credit
3. Ask what they are building

### Step 1: check tools and login

```bash
plivo --version                  # if missing: brew install plivo/tap/plivo, or the install.sh one-liner from the Plivo docs
plivo auth whoami -o json        # exit 2 with AUTH_MISSING: the user logs in (below), then run this again
plivo tts pronunciation-dictionaries     # exit 1 with an unknown command: the CLI is too old for this skill; upgrade it (plivo upgrade)
```

Check: `whoami` shows the account the user expects, and `plivo tts pronunciation-dictionaries` exits 0 and prints its help.

- Login opens a browser, so the user runs it. On their own computer: `plivo login` in a terminal (in Claude Code, `! plivo login`). In a container, over SSH or in WSL, the browser cannot reach the CLI: the user runs `plivo login` in a terminal on that machine, approves in the browser, then copies the full URL from the browser's address bar and pastes it into that terminal.
- Wrong organization: `plivo auth list`, then `plivo auth use <profile>`. Sub-accounts use their own Auth ID and Auth Token, and the main account is billed.

### Step 2: check the credit

Check: `cash_credits` in the step 1 output is above 0. If it is not, send the user to <https://cx.plivo.com/home> to add credits, and read `whoami` again after they say it is done.

### Step 3: ask what they are building

Ask "What are you building?" (header Build) · An audio file: "One text turned into a file you keep." · Prompts to play on a call: "Short recordings that Voice XML plays to callers." · A live voice agent: "An AI that speaks to callers as it thinks."

Check: the answer is recorded. Then branch the whole flow:

- An audio file or Prompts to play on a call: the file branch, steps F1 to F4. The answer already says where the audio plays, so F1 does not ask it again.
- A live voice agent: the voice agent branch, steps V1 to V8.
- Anything else (an IVR menu, text spoken inside a call with no file): `<Speak>` in Voice XML does that, so send the user to `plivo skill install voice-xml`.

## File branch: steps F1 to F4, from a text to a checked audio file

Create these four tasks first, then run the steps in order. Only step F3 costs money.

1. Ask what to say
2. Pick a voice
3. Synthesize
4. Check the file and report

### Step F1: ask what to say

Ask in plain text what the speech should say (or which file holds it). Then ask the language, in one structured call:

- Language: infer it from the text, and ask only if it is unclear. Ask "Which language is the text in?" with English, Hindi and Detect from the text (recommended); the tool's own free-text Other covers Bengali, Tamil, Telugu, Marathi, Gujarati and Kannada. Codes: `en-US`, `en-GB`, `en-IN`, `en-AU`, `hi`, `bn`, `ta`, `te`, `mr`, `gu`, `kn`. English has four regional codes and there is no plain `en`: an English voice takes any of the four, the code sets how numbers and dates are read (`en-US` reads millions and the month first, the others lakh and crore and the day first), and the voice keeps its accent. Each voice speaks one language, so this decides the voice list. For Detect from the text, leave `--language` out (it defaults to `auto`, which detects the language of the text) and use the language you inferred to filter the voices in step F2.

The Build answer sets the format, so do not ask "Use" again:

| Build answer | `--format` | `--sample-rate` | Extra | File |
|---|---|---|---|---|
| Prompts to play on a call | `wav` | `8000` or `16000` | has a header; host it and play it with `<Play>` (see "Use the audio on a call"). **Risky:** not tested against the API | `.wav` |
| An audio file | `mp3` | `44100` | `--bitrate 128000` (the default at 44100 Hz) | `.mp3` |

If the user asks for another format, for their own SIP or telephony stack, ask which format it takes. The CLI also makes `mulaw` and `alaw` at 8000 Hz (raw, `.ulaw` and `.alaw`) and `pcm` at 16000 or 24000 Hz (raw 16-bit, `.pcm`), but no Plivo path plays a raw file, so use them only with that stack. A voice agent that speaks live is the other branch (V1 to V8), not a raw file.

Without `--format` the CLI picks the format from the `--out` extension (`.wav`, `.mp3`, `.ulaw` or `.mulaw`, `.alaw`, `.pcm` or `.raw`) and falls back to `wav` at 24000 Hz. Pass `--format` anyway and name the file for it: a raw file with a `.wav` name does not play. **Risky:** a sample rate that does not fit the destination, for example 24000 Hz into a phone call.

### Step F2: pick a voice

```bash
plivo tts voices --language hi -o json     # add --gender male|female to narrow it
plivo tts voices -o json                   # every voice
```

Each voice has `voice_id`, `is_owner` (true for a voice the organization owns), `name`, `gender`, `languages`, `accent_type`, `description`, `created_at` and, when Plivo has set one, `sample_url`, a link to a short sample that stops working after about an hour, so fetch the list again instead of saving it (the CLI leaves it out while it is empty). `plivo tts voice <voice_id>` shows one voice, and `plivo tts voices --language en` lists every English voice. When the user wants to hear voices before choosing, show the `sample_url` of each option that has one, or point them to Voice Studio in the console (https://cx.plivo.com/voice-studio/explore). The list holds Plivo's public voices and any voices the organization owns, so take it from the CLI and do not hardcode one. Voice IDs are opaque strings (for example `6uWzc4xFj31t4jNJUaED` speaks Hindi and `lC7qzMoRE7SSjaNCVeff` speaks US English), and the catalogue changes, so never reuse an ID from this page without checking it is in the list.

Ask "Which voice?" (header Voice) with up to three options, each "<name>: <gender>, <accent>" for a voice whose `languages` contain the language. Check: the chosen `voice_id` appears in the list with that language. **Will break:** a `--language` the voice does not speak, which returns `400 language_not_supported`.

### Step F3: synthesize

Put the text in the command, or in a file for anything long (`--file text.txt`). `--file -` reads stdin once, so keep it for a one-shot pipe, never for the `--dry-run` then `--yes` pair: the second command would find nothing to read. The limit is 20,000 characters per request: for more, split at sentence ends and synthesize each part, and ask the consent question below once, for the total number of characters across all the parts. Without `--timeout`, `speak` waits long enough for the text's length (30 seconds plus one second for every 50 characters); a global `--timeout` sets your own limit. Raw formats (`mulaw`, `alaw`, `pcm`) join byte for byte; `wav` and `mp3` need a tool such as `ffmpeg`.

```bash
plivo tts speak --voice 6uWzc4xFj31t4jNJUaED --language hi --text "आपका पैकेज कल पहुंचेगा।" \
  --format mp3 --sample-rate 44100 --bitrate 128000 --out reminder.mp3 --dry-run
```

The preview prints the character count on its first line (`[dry-run] <N> characters, ...`): quote that number and do not count bytes yourself. Show the preview, then ask: "Synthesize <N> characters now? Plivo bills per character." Synthesize / Not now (header Synthesize). On yes, take a reading with `plivo tts usage -o json`, then run the same command with `--yes` in place of `--dry-run`.

If a billed run fails with any `UPSTREAM_*` code (`UPSTREAM_TIMEOUT`, `UPSTREAM_UNAVAILABLE`, and `UPSTREAM_ERROR`, which includes a 200 answer that is not audio), with `NETWORK_ERROR`, or with an error whose hint says the request was or may have been billed, do not run it again. In those cases ignore `retryable: true` and any "retry" hint: the request may have been billed. Read `plivo tts usage -o json` again and compare its `characters` with the reading from before the send (`requests` counts a failed request too, so it says nothing here). If `characters` did not change and the failure was a 502 or 503 (`backend_error`, `at_capacity`, `draining`, `auth_unavailable`), the request was refused before it ran: wait a second and send it once more. In every other case, such as a timeout or a network error, tell the user what changed (or that nothing did) and ask before you send again, because a request that timed out may still finish and bill. Take a usage reading before every billed run.

Optional flags:

| Flag | Range | Use |
|---|---|---|
| `--language` | `auto`, `en-US`, `en-GB`, `en-IN`, `en-AU`, `hi`, `bn`, `ta`, `te`, `mr`, `gu`, `kn` | Default `auto`; the voice must speak it. A plain `en` is refused |
| `--pace` | 0.7 to 1.2 (default 1.0) | Speaking speed |
| `--volume` | 0.5 to 2.0 (default 1.0) | Loudness |
| `--pronunciation-dictionary <id>` | a saved pronunciation dictionary: `pd_...`, or `lx_...` for one created as a lexicon | Custom pronunciations (below) |
| `--seed N` | integer | Same text, voice and seed give identical audio, for tests |

Numbers, currency and dates are normalized for you ("1,250" is spoken as an amount, not digits). `--out -` writes the audio to stdout for a pipe; the CLI refuses to print audio to a terminal.

Check: the command exits 0 and `--out` exists.

### Step F4: check the file and report

```bash
ls -l reminder.mp3       # a raw file: mulaw and alaw at 8000 Hz are 8000 bytes per second; 24000 Hz pcm is 48000
ffprobe -v error -show_entries format=duration -of csv=p=0 reminder.mp3   # seconds; a raw file needs its format: -f mulaw -ar 8000, -f alaw -ar 8000, or -f s16le -ar <rate>
```

To listen to a raw file, the user runs `ffplay -autoexit -f mulaw -ar 8000 reminder.ulaw`. For a `.wav` or `.mp3`, `file out.wav` reports "RIFF ... WAVE audio" (or MP3 audio) and `afplay out.wav` plays it on macOS (`ffplay -autoexit` elsewhere). Use `file` and `afplay` for those two only: a raw file has no header to report.

Report: the file path, its size, the format flags you used, and the character count. Ask "Does it sound right?" (header Listen), since you cannot hear it. Then read `plivo tts usage -o json` again and compare it with the reading from before the run (see "Billing and usage").

## Pronunciation dictionaries

A pronunciation dictionary is a saved list of rules that change how words are said. Use one when a brand, name or term comes out wrong. `plivo tts speak` has no inline-rule flag, and the API takes no inline rules: save the rules as a pronunciation dictionary, then pass `--pronunciation-dictionary`. To set one word in one request, write it in the text as a phoneme span in braces: `{kəmˈpjutɚ}` (IPA) or `{K AH0 M P Y UW1 T ER0}` (ARPAbet).

Each rule has a `word`, a `value` and a `kind`, which says how the value is written:

| `kind` | A rule's `value` is written as |
|---|---|
| `respell` | plain respelling, for example `plee-voh` |
| `ipa` | International Phonetic Alphabet |
| `arpa` | ARPAbet |

```bash
# rules.json: [{"word": "Plivo", "kind": "respell", "value": "plee-voh"}]
plivo tts pronunciation-dictionaries create --name brands --file rules.json --dry-run            # preview, then run it again without --dry-run
plivo tts pronunciation-dictionaries create --name brands --kind respell --rule "Plivo=plee-voh"   # or the rules inline; --rule repeats
plivo tts pronunciation-dictionaries list -o json
plivo tts pronunciation-dictionaries get pd_... -o json
plivo tts speak --voice lC7qzMoRE7SSjaNCVeff --text "Welcome to Plivo" --format mp3 --sample-rate 44100 \
  --bitrate 128000 --pronunciation-dictionary pd_... --out welcome.mp3 --dry-run
plivo tts pronunciation-dictionaries delete pd_... --dry-run      # then --yes, after the user agrees
```

`--file` and `--rule` can be combined: the rules are merged, and `--rule` repeats. `--kind` is the kind of every `--rule`, and of a rule in the file that names none; a rule in the file can also set `"case_sensitive": true` to match the word only as written. The create command prints the dictionary with its `pronunciation_dictionary_id` (starts with `pd_`): record it. A dictionary created as a lexicon, before the rename, keeps its `lx_` ID and still works. Each create makes a new dictionary, also with a name already in use, so do not run it twice. The CLI has no command that edits a dictionary: create a new one, then delete the old one. Limits: 100 dictionaries per organization, 100 rules per dictionary, a name up to 100 characters, a `word` up to 100 and a `value` up to 120. **Will break:** synthesizing with a deleted `pronunciation_dictionary_id` returns `404 not_found` with `param: pronunciation_dictionary_id`; `get` or `delete` on an unknown ID returns `404 not_found`. Deleting a dictionary cannot be undone: ask first.

## Use the audio on a call (file branch)

- **Play a file.** Synthesize `wav` (`--sample-rate 8000` or `16000`) or `mp3`, host it at an HTTPS URL, and play it from the answer XML with `<Play>https://.../message.mp3</Play>`. `<Play>` takes mp3 or wav up to 10 MB, 8 or 16 kHz mono recommended; it does not take raw mu-law. To speak plain text inside a call with no file, use the `<Speak>` element instead (`plivo skill install voice-xml`).
- **A voice agent that speaks live** does not play files: use the voice agent branch below.

## Voice agent branch: steps V1 to V8, from a working call to a voice that holds a conversation

Create these eight tasks first, then run the steps in order. Stop at the first failed check and fix it before you go on. Steps V4 to V8 bill per character when they run a reply, so each test follows the billing rule above.

1. Ask the setup questions
2. Pick a voice and plan the languages
3. Set the audio format
4. Connect Plivo TTS to the framework
5. Make interruptions work
6. Add word timestamps
7. Set and check latency
8. Estimate the cost and report

### Step V1: ask the setup questions

Ask in one structured call:

| Header | Question | Options |
|---|---|---|
| Framework | Which framework runs your voice agent? | Pipecat · LiveKit Agents · My own WebSocket code: "You handle the call audio yourself." |
| Calls | How do calls reach your voice agent? | Plivo Audio Streaming (recommended): "Plivo sends the call's audio to your WebSocket." · SIP trunking: "Plivo hands the call to a SIP endpoint, such as LiveKit." |
| Test call | Does a test call to your voice agent already work? | Yes, it works · Not yet: "I'll point you to the skill that gets a call working first." |
| Language | Which language will the voice agent speak? | English · An Indian language · Mixed, for example Hindi and English |

For an Indian language, ask in plain text which one (Hindi, Bengali, Tamil, Telugu, Marathi, Gujarati or Kannada). Then ask the target in a second call: "How fast should the voice agent's first sound start once it has a sentence to say?" (header Latency) · Fast: "About 300 ms. Needs a ready connection and a short first sentence." · Balanced (recommended): "About 600 ms." · Relaxed: "About 1 second." These are budgets for the user's own agent, not Plivo promises: the docs publish no latency number.

The standard paths are Pipecat with Audio Streaming (Pipecat's Plivo serializer talks to `<Stream>`), LiveKit Agents with SIP trunking (LiveKit's SIP service takes the call) and your own WebSocket code with Audio Streaming (your handler is the `<Stream>` WebSocket). LiveKit with Audio Streaming is not a LiveKit path: offer SIP trunking. Pipecat with SIP trunking is **risky**: it needs a transport this file does not cover, so ask which one and check the audio rate it takes. Your own code with SIP trunking means your own SIP and media stack, with G.711 at 8 kHz (step V3).

If Test call is "Not yet": stop. Send the user to `plivo skill install audio-streaming` (Audio Streaming) or `plivo skill install sip-trunking` (SIP trunking), tell them to come back when a test call connects, and leave the later tasks pending. If it is "Yes", you can confirm the setup without placing a call: `plivo voice streams readiness <number> -o json` (v1.3.2 or later, Audio Streaming only; `fail` lines come first, `unknown` is not a pass).

Check: all five answers are recorded, and either a test call works or the user has been sent to the skill that makes it work.

### Step V2: pick a voice and plan the languages

```bash
plivo tts voices --language hi -o json      # the language code from V1; en-IN is Indian-accented English
```

Each voice speaks one language, so the voice decides the language code, and `language` stays fixed for the whole reply (a reply's options are fixed when it opens). For a phone call, read each voice's `description` and `accent_type`, offer up to three that read as conversational, and give each `sample_url`. A phone line carries 8 kHz audio, so the real test is a call: a voice that sounds best in a browser may not on a phone. Ask "Which voice?" (header Voice).

- English only: an English voice with its own accent code (`en-US`, `en-GB`, `en-IN`, `en-AU`). There is no plain `en`.
- One Indian language: a voice for it and its code (`hi`, `bn`, `ta`, `te`, `mr`, `gu`, `kn`). Leave `auto` out for a call: it detects the language of the text, and a wrong guess returns `language_not_supported`.
- Mixed, for example Hindi with English words: the docs do not say how a voice handles it, so everything here is **risky** until the user has heard it. Use one Hindi voice with `language` set to `hi`, keep English names and terms in Latin script inside the Hindi text (the docs' own example reply reads "नमस्ते, Plivo में आपका स्वागत है।"), and never change voice or language inside one reply. Fix a wrong word with a `respell` rule in a pronunciation dictionary.

For a mixed plan, ask for two real replies, one pure and one mixed, count the characters, and ask the Run test question. On yes, run them through the script in "Realtime socket reference" with `"language": "<the code>"` added to its first message, then ask "Does it sound right?" (header Listen).

Check: the chosen `voice_id` appears in the list with the language, and for a mixed plan the user has listened to a mixed sample.

### Step V3: set the audio format

The format goes in the first message of every reply as `output_format`, and it depends on who receives the audio next:

| Framework and calls | Ask Plivo for | Why |
|---|---|---|
| Pipecat on Audio Streaming | `{"container": "raw", "encoding": "pcm_s16le", "sample_rate": 16000}` | Pipecat's `PlivoFrameSerializer` takes 16-bit PCM, resamples it to 8 kHz and converts it to mu-law itself. Mu-law from Plivo would be encoded twice and come out as noise |
| LiveKit Agents on SIP trunking | `{"container": "raw", "encoding": "pcm_s16le", "sample_rate": 24000}` (16000 also works) | LiveKit works in 24 kHz mono and resamples to the phone leg itself, and its audio emitter takes 16-bit PCM: never mu-law |
| Your own WebSocket code on Audio Streaming | `{"container": "raw", "encoding": "pcm_mulaw", "sample_rate": 8000}` | `<Stream contentType="audio/x-mulaw;rate=8000">` takes raw mu-law, sent as `playAudio` at real-time pace (step V4) |
| Your own SIP and media stack | `pcm_mulaw` (US, India) or `pcm_alaw` (Europe), `raw`, 8000 | G.711 over RTP |

Do not use `wav` or `mp3` on a call path. The docs list `pcm_s16le` at 16000 and 24000 in their format table; PCM at 8000 appears only in a summary line, so prefer 16000 or 24000.

Check: the format row for this combination is chosen and recorded. Noise or static on the first test call (step V4) means the wrong format reached the wrong receiver, most often mu-law into a framework that expects PCM.

### Step V4: connect Plivo TTS to the framework

First the user adds `PLIVO_AUTH_ID`, `PLIVO_AUTH_TOKEN` and `VOICE_ID` (the voice from V2) to their `.env` with an editor, as in the rules. These facts hold for every integration (the tables are in "Realtime socket reference"):

- One socket for the whole call. Open it when the call connects and reuse it for every reply. There is no fixed number of replies per socket: each open reply counts toward the organization's concurrency limit of 50.
- One reply is one `context_id`, used once per socket: a new one for every turn. A reused id returns an `error` with `invalid_request` and `param: context_id`.
- The first `text` message of a reply carries `voice_id`, `language`, `output_format` and `timestamps`. Later messages carry only `type`, `context_id`, `text` and `finish`: an option repeated with a changed value returns `invalid_request`. `"finish": true` ends the reply.
- Plivo speaks each sentence as soon as it ends and holds an unfinished one, so a few words with no sentence end make no sound until a `flush` or `finish`.
- Keep reading the socket: a client that stops reading for 30 s is dropped. Audio comes as binary frames (2-byte little-endian length, the `context_id`, then audio) and everything else as JSON.

Neither Pipecat nor LiveKit Agents has a Plivo TTS service, so the user writes a small one over this socket, modelled on the framework's own Cartesia service. Tell them it is their own code, not an official Plivo package. This file gives the shape, not the code: read the installed version first (`uv pip show pipecat-ai` or `livekit-agents`; checked against 1.17.0 and 1.8.13) and follow its names where they differ, and say so.

- **Pipecat** (Python 3.11 or later): start from the Plivo example `pipecat-examples/plivo-chatbot/inbound` (see `plivo skill install audio-streaming`, step 5) and replace only the `tts = ...` block of `bot.py`. Subclass `WebsocketTTSService` with one Plivo reply per Pipecat turn, `push_start_frame=True`, `append_trailing_space=True` (Plivo joins the text it receives) and `can_generate_metrics()` returning true, and add `enable_metrics=True` to the pipeline params, or no TTFB is reported. Open the socket in `setup`, not at the first reply. `run_tts(text, context_id)` sends the `text` message and yields `None`. The receive loop turns each binary frame into a `TTSAudioRawFrame` for `append_to_audio_context` (dropping frames of a context that is gone), a `done` into a `TTSStoppedFrame` and `remove_audio_context`, and an `error` into `push_error`. `flush_audio` sends an empty final `text` with `"finish": true`, and `on_audio_context_interrupted` sends `cancel` (step V5).
- **LiveKit Agents** (1.8.13 or later): LiveKit has no guide for custom TTS plugins, so the source of its Cartesia plugin is the model. Subclass `tts.TTS` (with a `utils.ConnectionPool` of sockets) and `tts.SynthesizeStream`. In `_run(output_emitter)`, call `output_emitter.initialize(...)` with `mime_type="audio/pcm"` and `stream=True`, take a new `context_id` per stream, and run one task that sends text and one that receives, calling `start_segment` and `push` for each audio frame and `end_input` on `done`. A flush sentinel sends `flush`, the end of the input sends the empty final `text` with `"finish": true`, and a cancelled stream sends `cancel`. Use it as `AgentSession(tts=PlivoTTS(...))`.
- **Your own WebSocket code:** the TTS socket is a second WebSocket next to the `<Stream>` one. Use the script in "Realtime socket reference" for a first test. Then, per reply, send one `text` message with `"finish": true` and the mu-law `output_format` from step V3, and forward each audio frame of that `context_id` as `{"event": "playAudio", "streamId": "<id>", "media": {"contentType": "audio/x-mulaw", "sampleRate": 8000, "payload": "<base64>"}}`. Send pieces of 1,600 bytes (200 ms; **risky:** untested, and one piece takes at most 12,000), and wait the piece's length divided by 8000 seconds between sends: bursting overflows the playback buffer and makes `clearAudio` late (see the audio-streaming skill).

The docs show no empty final `text`, so that message is **risky** in both frameworks: on the first test every reply must end with a `done` event. If the socket answers with an `error` instead, tell Plivo support and send the last sentence with `"finish": true` instead (**untested**). A socket with no message and no open reply for 5 minutes closes with `1000`, so let the receive loop reconnect when it ends. A `1008` close is an error to read, not to reconnect past: log the last `error` event first. The socket closes with the call, and until then it counts one toward the concurrency limit of 50.

Before the first test: do the credit check, take a `plivo tts usage -o json` reading, count the characters the test will send, and ask the Run test question from the rules. Then place the test call.

Check: on a test call the voice agent speaks in the chosen voice, the agent's log shows a `done` event with `reason: "complete"` for each reply, and the user hears speech and not noise. Ask "Did the voice agent speak clearly?" (header Listen) · Yes, clearly / It is noisy or garbled / Silence / The call did not connect. Noisy: the format (step V3). Silence: read the agent's log for an `error` event first. Do not repeat a billed test until the failed layer is fixed, and read the usage report to see what the failed run billed.

### Step V5: make interruptions work (barge-in)

When the caller talks over the voice agent, three things must stop, in this order:

1. The reply at Plivo: send `{"type": "cancel", "context_id": "<the reply's id>"}` (no `context_id` cancels every open reply). Each cancelled reply ends with a `done` whose `reason` is `cancelled`. Billing still counts all the text the reply received (`characters`); `spoken_chars` is what was spoken before the cancel.
2. The audio already queued for the caller (Audio Streaming only): send `{"event": "clearAudio", "streamId": "<the stream's id>"}` on the `<Stream>` WebSocket. Plivo drops the queue and answers `clearedAudio`. SIP trunking has no such event: the SIP side and the framework clear their own buffers.
3. Your own buffer: discard every frame that still arrives for the cancelled `context_id`. The docs do not say whether frames in flight can arrive after a cancel, so assume they can.

The next reply uses a new `context_id` on the same socket. A cancelled reply counts as open until its `done` (**risky:** the docs do not say when the slot frees, so wait for `done`). Pace the audio you send to the call in real time, so the queue stays short and `clearAudio` takes effect at once.

- **Pipecat:** both stops are wired. `PlivoFrameSerializer` sends `clearAudio` when an `InterruptionFrame` passes, and the service's `on_audio_context_interrupted` sends `cancel`. Voice activity detection (Silero in the example) raises the interruption.
- **LiveKit Agents:** LiveKit stops its own playback and cancels the stream's `_run`, where the plugin sends `cancel`. There is no `clearAudio` on SIP. How easily a caller interrupts is set with `turn_handling=TurnHandlingOptions(...)` on `AgentSession` (`interruption` of `"adaptive"` or `"vad"`, `min_duration`, `min_words`); the bare `turn_detection` and `allow_interruptions` arguments are deprecated in 1.8.13.
- **Your own code:** on your voice activity or speech-to-text "speech started" event, run the three stops in that order, from the handler that plays the audio.

Ask "Did it stop speaking when you interrupted?" (header Interrupt) · Yes, right away · It kept talking · It stopped, then said old words. Check: the answer is "Yes, right away". If it kept talking: was `cancel` sent with the id of the reply that is playing, was `clearAudio` sent with the right `streamId`, and is audio sent faster than real time? If it said old words: frames for the cancelled `context_id` are not being discarded. Test it on a call, with a test reply long enough to interrupt: that costs characters, so ask the Run test question first.

### Step V6: add word timestamps

Word timings keep the transcript in step with the audio. In Pipecat they decide which words reach the conversation history, so after an interruption it holds the words that were spoken; in LiveKit they only feed the transcript the agent shows. Turn them on with `timestamps: true` in the reply's first message (not in later ones). Plivo then sends `timestamps` events, each holding whole words, as three parallel arrays with one entry per character, spaces and punctuation included: `chars`, `start_ms` and `duration_ms`. Times are milliseconds from the start of the reply's audio and keep counting, so no offset is needed. Group the characters into words at whitespace, punctuation staying with its word.

- **Pipecat:** create the service with word timestamps on. It then sends `timestamps: true`, calls `add_word_timestamps(words, context_id)` for each event, and sets `push_text_frames=False`, so the spoken text reaches the conversation aligned with the audio instead of the whole unspoken reply.
- **LiveKit Agents:** declare `aligned_transcript=True` in the plugin's `TTSCapabilities` and push a `TimedString` per word with `push_timed_transcript`. LiveKit uses them only when the session sets `use_tts_aligned_transcript` (off by default). The docs do not say they change how history is cut after an interruption, so do not claim it.

The docs do not say how `chars` line up with the text after normalization (digits and currency are rewritten for speech) or for Hindi, and not whether a `timestamps` event arrives before or after its sentence's audio. **Risky:** check with a real reply. Check: for one test reply, print the words with their times, and confirm that they come in order, that the first word starts near 0 and that the last word's end is not past the reply's `audio_seconds` in its `done` event. A test reply bills per character, so ask the Run test question first.

### Step V7: set and check latency

The docs publish no latency figure, so the target from V1 is the user's own budget. Each `done` event carries `ttfb_ms`. The docs show it without a description, so read it as milliseconds to first audio on Plivo's side (**risky:** its start point is not documented), and also timestamp your own first send and first audio frame, which includes the network. Collect at least 10 replies from a test call and compute the median and the worst. The call bills per character: estimate the characters (the average per reply from the earlier `done` events times 10), say so, and ask the Run test question first. Check: the median is within the target. If not, change one thing at a time and measure again:

1. Reuse the socket: a new connection per reply adds the handshake to every reply.
2. Send text as the model produces it, not after the whole answer: audio can start once the first sentence ends.
3. Flush early. Which characters end a sentence is not documented (for Hindi, whether `।` counts): test it, and if the first sound waits for the next sentence, send `flush` where your tokenizer sees a sentence end.
4. Tell the model to start each answer with a short first sentence (**style**: it only helps the first sound).
5. Ask for the right format (step V3): PCM, never mp3, and no extra conversion on your side.
6. Keep the agent near `api.plivo.com`. The docs name no TTS host in India or any other region.

No minimum amount of text before audio starts is documented: the first sentence starts it. A few big messages, or one at the end, is the common cause of a slow first sound.

### Step V8: estimate the cost and report

Plivo bills per character and gives no price in its docs, so the estimate is characters per minute and no price. Take the numbers from the agent's own `done` events: each has `characters` (billed) and `audio_seconds` per reply, which stays exact when other traffic shares the account. As a cross-check, `data.routes.live` in `plivo tts usage -o json` is realtime: read it before and after a test call of known length and subtract (other realtime traffic on the account breaks that, so say so). The test call bills per character: estimate the characters (the V7 average per reply times the replies you expect in about 2 minutes), say so, and ask the Run test question first.

```bash
plivo tts usage -o json --query 'data.routes.live'    # before the call, and again after it
```

Characters per call minute is the characters difference divided by the call's minutes; characters per spoken minute is that difference divided by the `audio_seconds` difference over 60. Cancelled replies still bill all the text they received, so interruptions raise the figure: say how many were cancelled. For money, the user asks their Plivo account manager or support for the current per-character rate: never quote one.

Check: you have characters per call minute and per spoken minute from the `done` events, and the usage difference as a cross-check when the account was quiet.

Report, short, in this order: the framework and call path; the voice and language code; the audio format used; whether the first test call sounded right (the user's answer, not yours); the interruption result; the median and worst `ttfb_ms` against the target; characters per call minute and per spoken minute; what was not verified (for instance the empty final message, Hindi sentence ends, timestamps for normalized text); and the next step, such as `plivo skill install audio-streaming` for go-live and monitoring.

## Realtime socket reference

Use it when text arrives piece by piece, as a model writes it, and the audio must start before the text ends. It is not a CLI command. Endpoint: `wss://api.plivo.com/v1/Account/<auth_id>/TTS/Realtime/`, with the Auth ID and Auth Token as HTTP Basic auth on the upgrade request. A bad credential is refused with an HTTP status, so the socket never opens. There are no query parameters: one returns `400 invalid_request` and the socket does not open, and a plain GET with no upgrade returns `426`. Every option goes in the first `text` message of a reply.

A runnable start. Save it as `realtime_tts.py`. The user puts `PLIVO_AUTH_ID`, `PLIVO_AUTH_TOKEN` and `VOICE_ID` in `.env`; never read, cat or print that file. Running the script bills per character, like step F3: do the credit check (step 2) and take a `plivo tts usage -o json` reading, then ask "Run a test of about <N> characters now? Plivo bills per character." (header Run test; the script as written sends 16 characters, so count the new text if you change it), and run it only on yes. `uv run --script` alone does not load `.env`, so run:

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
                print("billed characters:", event.get("characters"), "ttfb_ms:", event.get("ttfb_ms"))
                break
            if event["type"] == "error":
                raise SystemExit(f"{event.get('code')}: {event.get('message')} (param {event.get('param')})")
    with open("hello.ulaw", "wb") as f:
        f.write(audio)


asyncio.run(main())
```

Messages you send (JSON text frames, up to 1 MiB each):

| `type` | Fields | Meaning |
|---|---|---|
| `text` | `context_id`, `text` (up to 20,000 characters a reply), `finish` (default `false`); on a reply's first message also `voice_id` (required), `model_id`, `language`, `text_normalization`, `output_format`, `pace`, `volume`, `pronunciation_dictionary_id`, `continuation`, `seed`, `timestamps` (default `false`), `use_normalized_timestamps` (default `false`) | A new `context_id` opens a reply, and each `context_id` can be used once per socket. Send the text in pieces, and `finish: true` on the last. A later message for the same reply may repeat the first message's options with the same values; a changed value ends the reply with an `error` of `invalid_request` and the field in `param` |
| `flush` | `context_id`, optional `flush_id` | Speak everything received so far for that reply, including a sentence that has not ended, and keep the reply open. With a `flush_id` the server answers `flush_done` with the same id; without one it sends none. A flush on a reply that was never opened returns `invalid_request` |
| `cancel` | optional `context_id` | Stop that reply now; with no `context_id`, stop all replies. A `context_id` that is not open is ignored |

Messages you receive:

| Frame | Meaning |
|---|---|
| binary | A 2-byte little-endian length, then that many bytes of `context_id`, then audio in that reply's `output_format` |
| `timestamps` | Character timings, in events that each hold whole words, only when the reply's first message set `timestamps: true`: `chars`, `start_ms` and `duration_ms`, one entry per character, in milliseconds from the start of the reply's audio |
| `flush_done` | A flush that carried a `flush_id` has been generated |
| `done` | The reply ended without an error, after its last audio frame or when you cancel it. It carries `reason` (`complete` or `cancelled`), `continued`, `normalized_text`, the billed `characters`, `spoken_chars`, `tokens`, `audio_seconds`, `ttfb_ms` and `api_id`. A reply that fails ends with its `error` and gets no `done` |
| `warning` | A problem that does not end the reply: `code` (`text_too_long` for text dropped past the reply's limit, `invalid_request` for a phoneme span that cannot be read or text sent after `finish`), `number`, `message`, `param`, `context_id` and `api_id` |
| `error` | `type`, `code`, `number`, `status_code` (the status the same error has over HTTP), `message`, `param`, `context_id`, `api_id`. It does not close the socket and ends only the reply named in `context_id`; `context_id` is left out when the error is not about one reply |

`output_format` takes the same fields the batch request sends. These are the values behind each CLI `--format`; `--sample-rate` replaces the rate, and mp3 adds a `bitrate`, which only some rates accept (below the table):

| CLI `--format` | `container` | `encoding` | `sample_rate` |
|---|---|---|---|
| `wav` | `wav` | `pcm_s16le` | `24000` |
| `mp3` | `mp3` | `mp3` | `44100` |
| `mulaw` | `raw` | `pcm_mulaw` | `8000` |
| `alaw` | `raw` | `pcm_alaw` | `8000` |
| `pcm` | `raw` | `pcm_s16le` | `24000` |

mp3 takes a bitrate only in these pairs (from the CLI's own table, not the docs): 44100 and 48000 Hz take 32000, 64000, 96000, 128000 or 192000; 24000 Hz takes 48000; 16000 Hz takes 32000 or 64000; 8000 and 22050 Hz take 32000. The CLI picks a valid default for the rate (128000, 48000, 64000 or 32000), so leave `--bitrate` out unless the user asks. A pair that is not offered returns `400 unsupported_format`.

The docs' format table lists these uses: a phone call in the US or India `raw` `pcm_mulaw` 8000, a phone call in Europe `raw` `pcm_alaw` 8000, a wideband agent or web `raw` or `wav` `pcm_s16le` at 16000 or 24000, and a download `mp3` at 44100. Only `raw` `pcm_mulaw` at 8000 is exercised by the script above; **risky:** the other combinations are untested on the socket, and the docs do not say how a `wav` or `mp3` reply is framed on it. A `seed` repeats the audio only with `continuation` set to `false`, which is what `--seed` sends; a continued reply builds on the reply before it, so the same seed and text can sound different.

Rules that matter: keep reading the socket, because a client that reads nothing for 30 s is dropped. One socket carries many replies at once, each with its own `context_id`, and each open reply counts toward the organization's concurrency limit. A reply with no message and no audio for 60 s ends as if its `finish` came, and a socket with no message and no open reply for 5 minutes is closed with `1000`. The realtime messages use the same field names as the batch request, so `pace`, `volume` and `pronunciation_dictionary_id` mean what they mean for `plivo tts speak`; the batch-only `--file`, `--out` and `--format` flags have no counterpart. Omit an optional field rather than send `null`, which returns `400`. Close codes: `1009` is a message over 1 MiB, `1013` is a retryable error (reconnect), `1011` is a server (5xx) error (reconnect with backoff), `1008` is any other error (read the last `error` event first, and do not reconnect past it).

## Limits

| Limit | Value |
|---|---|
| Concurrency per organization | 50, then `429 concurrency_limit`: each batch request and each open realtime reply counts one, and a socket with no open reply counts one |
| Text per batch request, or per realtime reply | 20,000 characters |
| Replies (contexts) per socket | No fixed number: each open reply counts toward concurrency |
| Request body | 1 MiB |
| WebSocket frame | 1 MiB |
| Slow reader | dropped after 30 s without reading |
| Pace, volume | 0.7 to 1.2, 0.5 to 2.0 |
| Pronunciation dictionaries per organization, rules per dictionary | 100, 100 |

Not documented, so never state them as fact: a maximum lifetime for a socket, and a rate limit on messages.

## Billing and usage

Billing is per character: each `done` event and the usage report give the billed `characters`. Quote no price, because the docs give none. `plivo tts usage -o json` returns totals split by route: `http` is batch (`plivo tts speak`) and `live` is realtime. Each route has `requests` (batch requests or realtime replies), `characters` (billed), `spoken_chars` (characters spoken after normalization), `audio_seconds` and `tokens`. The report holds account totals and the docs show no time window, so what one run cost is the difference between a reading taken before the run and one taken after. Use it to check that a run cost what you expected, not to predict a bill.

## When a request fails

With the CLI, add `-o json`: the error envelope prints on stderr. Its `error.code` is the CLI's own code (`VALIDATION_ERROR`, `RESOURCE_NOT_FOUND`, `UPSTREAM_TIMEOUT` and so on), not Plivo's. Plivo's answer is at `error.context.upstream.body`: its `code` and the code's `number`, the text in `error` (not `message`), a `param` when a field is at fault, and `api_id`. Read that body first, and key the table on `upstream.body.code`. These are the documented codes:

| `upstream.body.code` (HTTP status) | Meaning | Fix |
|---|---|---|
| `unauthorized` (401) | Bad or missing Auth ID or Auth Token | `plivo auth whoami`, then log in again |
| `forbidden` (403) | The credentials are not for the Auth ID in the request path | Use the account that owns the credentials |
| `insufficient_balance` (400) | Insufficient credits in the account | Add credits at <https://cx.plivo.com/home> |
| `usage_limit_exceeded` (403) | The account has reached its configured monthly usage limit | Raise the limit at <https://cx.plivo.com/account-settings>, or ask your account manager |
| `voice_not_found` (404) | Unknown voice, or one the account may not use | `plivo tts voices` |
| `model_not_found` (404) | Unknown `model_id`: `koyal-v1` is the only model | Leave `model_id` out |
| `language_not_supported` (400) | The `--language` given is one the voice does not speak (`param: language`) | Leave `--language` out, or pick a voice from `plivo tts voices --language <code>` |
| `empty_text`, `text_too_long` (400) | Nothing to speak, or over 20,000 characters | Send 1 to 20,000 characters; split longer text |
| `unsupported_format` (400) | That format, rate or bitrate pair is not offered; nothing is billed | Fix `--format`, `--sample-rate` and `--bitrate` (mp3 pairs are above) |
| `invalid_request` (400; 408 for a slow upload, 413 over 1 MiB, 426 for a plain GET on the realtime URL) | A bad value or a field the API does not know; `param` names it (`language` for a plain `en` or an unknown code, `context_id` for a reused reply id, or a query parameter, which only the voice list takes). A `null` for an optional field also returns it | Fix that field, or omit it |
| `not_found` (404), `method_not_allowed` (405) | A wrong path or method, or a pronunciation dictionary ID that does not exist (`param: pronunciation_dictionary_id`) | `plivo tts pronunciation-dictionaries list` |
| `concurrency_limit` (429) | The organization's batch requests and open realtime replies are at its limit of 50; a socket with no open reply counts one | Close idle sockets, wait 1 s, then retry |
| `at_capacity`, `draining`, `auth_unavailable` (503) | Busy right now | Wait 1 s, then retry (see the billed-run rule in step F3) |
| `backend_error` (502) | The service failed this request | Wait 1 s, then retry (same rule) |
| `internal_error` (500) | A bug on Plivo's side | Give support the `api_id` |
| `cancelled` (499) | The request was cancelled or the client left | Nothing to fix |

A realtime WebSocket client has no CLI envelope: its `error` frame carries `code`, `message` and `param` at the top level.

For any other code, show the user the whole `upstream.body` and do not guess a cause. To get help, give Plivo support `upstream.body.api_id` and `error.request_id`, with the UTC time and the command (not the text, if it is private). A direct HTTP or WebSocket client, such as the realtime script, has no envelope: every response carries an `api-id` header that matches `api_id` in an error body and in a realtime event, so give support that. A request may also carry a `client-ref` header (1 to 64 letters, digits, `_` or `-`): the id then starts with it.

Check in this order: the voice and language pair (`plivo tts voices`); the text length (20,000 characters) and the `--pace` and `--volume` ranges; for mp3, the `--sample-rate` and `--bitrate` pair; `cash_credits`; then the error body.

## When this skill does not have the answer

Do not guess from other platforms. The Plivo Text-to-Speech docs are published: read the page for the topic, and add `.md` to a page's URL for plain Markdown.

The pages are under https://www.plivo.com/docs/models/text-to-speech/: `overview`, `quickstart`, `ai-coding-agent`, `concepts/voices`, `api/voices`, `concepts/pronunciation`, `api/pronunciation-dictionaries`, `concepts/realtime`, `api/realtime`, `api/overview`, `api/batch`, `api/usage` and `troubleshooting/errors`.

`plivo docs search` does not reliably reach these pages: `text-to-speech` finds the overview, the API overview and the batch page, and a phrase such as `text-to-speech realtime` finds nothing, so open the page above instead of searching (`plivo docs show models/text-to-speech/api/realtime` works for a known page). A hit counts only if it is about the TTS models API: `<Speak>`, SSML, voice use-case and integration pages are a different product, so ignore them. Next, `plivo ask "<question>"`, which can also see the account; treat its answer as evidence and prefer the docs where they conflict. The docs do not give a price, a latency figure, a TTS host in India, how a voice handles mixed-language text, or whether an empty final `text` with `finish: true` is accepted: say so and route to Plivo support or the account manager. Out of scope: speech to text, the model behind a voice agent, the call itself (`plivo skill install audio-streaming`, `plivo skill install sip-trunking`), and the `<Speak>` element's own voices (`plivo skill install voice-xml`).
