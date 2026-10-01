---
name: plivo-audio-streaming
description: "Connects a WebSocket voice bot or AI voice agent (Pipecat or custom) to Plivo calls through the Stream XML element, from local test to go-live, using the Plivo CLI. Use for Plivo audio streaming, a caller hearing silence, errors 7011/8011, streamed-call hangup codes, bot-to-human handoff, India KYC for an agent number. Not for SIP trunking (plivo-sip-trunking) or plain Voice XML (plivo-voice-xml)."
license: Apache-2.0
---

# Plivo Audio Streaming: connect a bot to calls and go live

Take a developer, or their coding agent, from a WebSocket bot to a working, monitored phone agent. This file covers Plivo call control and the WebSocket boundary, not the STT, LLM or TTS inside the bot. Related skills, if installed: `plivo skill install first-agent` (a guided first call from nothing), `plivo skill install voice-xml` (Plivo XML without a stream), `plivo skill install` (the CLI's own skill). Without them, use `plivo <command> --help` and the docs pointers below.

## Rules for every step

- Use the CLI for every Plivo step and `-o json` to read values. `plivo <command> --help` wins on which commands and flags exist; this file wins on how Plivo behaves, including tested behaviour the help text omits. Never invent a flag: where the CLI has none, use `plivo api <METHOD> <path>` or the console, and say so.
- Every write is two commands: first with `--dry-run` (prints the request, sends nothing), then, after you show the preview and the user agrees, the same command with `--yes`. That covers renting, re-pointing a number, application changes, calls, compliance filings and `plivo api` writes. If a command rejects `--dry-run`, show the current state from `get` or `list` instead and ask.
- Before re-pointing a number, record its current `application` (`plivo numbers get <number> -o json`). Rollback: `plivo numbers update <number> --app-id <previous_app_id>`.
- Report each stage below as observed, not known or failed. Observed means you saw the evidence in this session. Never say "ready" and never infer account state. A `request_uuid`, an HTTP 2xx, a passing XML check or a WebSocket handshake is not success: a call worked only when the call record, the callbacks, the bot's log and a person who heard the audio agree.
- Verdicts: **will break** when a documented rule or an observed failure says so (name the failure); **risky** when plausible or untested (say what to check, name no hangup code); **style** when nothing changes. A rule here with no label is risky.

## Known issues (time-sensitive)

- `plivo voice streams forward` on CLI v1.1.0 to v1.1.2 cannot carry a live call: it checks the stream upgrade's signature over `wss://` while Plivo signs it over `http://`, so it answers 403, and its answer XML has no `keepCallAlive`. `--insecure-skip-signature` gets past the 403 but not the early hangup. It is fixed on main for the next release: check `plivo --version`. Until then, put your bot behind a plain tunnel (for example `ngrok http 7860`) and serve your own answer XML.
- `plivo docs show` can cut a page short at a `# ` comment inside a code sample (for example `voice/xml/overview`, `voice/xml/input` and the audio streaming guide). If a page ends mid-example, read its https URL.

## Stages 1 to 8: one flow from account to operations

First ask what is not known yet: inbound, outbound or both; what runs the agent (Pipecat, another framework, a speech-to-speech endpoint, your own server) and whether it has a public `wss://` URL; which country the numbers are in (India: read "India" first). Then run the stages in order and stop at the first failure. Stages 1 to 4 are free: do not place a call to discover configuration. Before launch, a tunnel host, a missing fallback URL or a `<Stream>` without `statusCallbackUrl` count as failures.

### Stage 1: account and number

```bash
plivo auth whoami -o json                  # the account you meant, credits above zero
plivo numbers get <number> -o json         # on this account, voice_enabled true; record `application`
plivo numbers search --country US --type local --limit 5   # no number yet
plivo numbers buy <number> --dry-run       # spends money
```

- `application` is a URI ending `/Application/<app_id>/`; table output shows it as `app_id`.
- Wrong organisation: `plivo auth list`, then `plivo auth use <profile>`. Running `plivo login` for another organisation saves one profile per organisation. Data region is console only.
- A number at another carrier: forward it to a Plivo number, or have the carrier send SIP to `sip:<app_id>@app.plivo.com` with SIP authentication: `plivo docs show voice/use-cases/connect-external-numbers` (<https://plivo.com/docs/voice/use-cases/connect-external-numbers>).
- Observed when: the number is on this account and voice-enabled and, for India, its compliance application is `accepted`.

### Stage 2: an application with answer, fallback and hangup URLs

```bash
plivo account applications create --app-name voice-agent \
  --answer-url https://HOST/plivo/answer --answer-method POST \
  --fallback-answer-url https://HOST/plivo/fallback \
  --hangup-url https://HOST/plivo/hangup --dry-run
plivo numbers update <number> --app-id <app_id> --dry-run
plivo numbers get <number> -o json         # confirm the new application
```

- The most common first-call failure: the number points at another application, so your answer URL is never fetched. A console flow application with no working flow answers with JSON, which the call record reports as 8011. Check what the number points at before you debug your server.
- `applications update` has no fallback flag. Set it at create time, or `plivo api POST /Application/<app_id>/ --body '{"fallback_answer_url":"https://HOST/plivo/fallback"}' --dry-run`, then `--yes`.
- Observed when: the number points at your application and all three URLs are on a real https host (a tunnel only while testing).

### Stage 3: prove the WebSocket, no phone involved

```bash
plivo voice streams test --to ws://localhost:7860/ws --bidirectional --duration 5   # laptop
plivo voice streams test --to wss://HOST/ws --bidirectional --duration 5            # public host
```

Observed when: `Received N frames back` (`frames_read_back` above 0 in JSON). That proves the endpoint accepts a connection and replies, nothing more:

- No call is placed and no signature is sent. A bot that rejects unsigned upgrades fails here: test a local copy with the check off, or report the stage as not known.
- The frames are reduced: no `sequenceNumber`, `extra_headers`, `start.tracks` or media `streamId`, and `chunk` is a string. Any reply counts, not only `playAudio`.
- With `--bidirectional` the CLI streams for `--duration` seconds, reads for another `--duration`, then drops the TCP connection: no `stop` frame and no close frame. Only without `--bidirectional` does it send `{"event":"stop"}` and close normally. Real calls can drop too, so end the stream on a closed or dropped socket, never only on `stop`.
- If you support both codecs, run `--codec mulaw` and `--codec l16 --rate 16000` separately (`--duration` max 30).

A connect failure means not public, not TLS, the wrong path, or a rejected unsigned upgrade. No frames back means the bot never replies or chokes on the reduced frames.

### Stage 4: the answer URL returns valid XML

Compose the document from "The XML" below, then probe what the server really returns:

```bash
curl -s -i -X POST https://HOST/plivo/answer \
  -d 'CallUUID=readiness&From=%2B10000000000&To=%2B<number>&Direction=inbound&CallStatus=ringing&Event=StartApp'
```

Observed when: HTTP 200, `Content-Type` `application/xml` or `text/xml`, a body that passes "Check the XML before you serve it", and a reply well inside Plivo's 15-second XML timeout.

- The URL must accept the application's method without Basic or bearer credentials; require a valid `X-Plivo-Signature-V3` instead. If it validates signatures, this unsigned probe gets a 401 that proves nothing: sign it (see "Callbacks, signature validation, timeouts") or read the body Plivo fetched in the console debug logs.
- No CLI command fetches the answer URL the way Plivo does, and no body check can see a 7011.

### Stage 5: the first real call

Test inbound first: a failed outbound answer URL is a billed call that drops when the callee answers. Dial the number from a phone, or call yourself:

```bash
plivo voice calls make --from <plivo number> --to <your phone> \
  --answer-url https://HOST/plivo/answer --answer-method POST --dry-run
plivo voice calls list --limit 1 -o json
plivo voice calls get <call_uuid> -o json
```

`calls make --answer-method` defaults to GET. A `request_uuid` means Plivo accepted the request, nothing more.

A bot that runs only on a laptop: `plivo voice streams forward --number <n> --app <app_id> --to ws://localhost:7860/ws` (but see "Known issues"). It opens a tunnel (localhost.run over ssh by default, ngrok if installed; `--tunnel` forces one), points the application's answer URL at it, and restores the URL on Ctrl-C. Every number on that application reaches your laptop meanwhile, so use a dedicated test application. It serves its own `<Stream>` with no `statusCallbackUrl`, so it does not test your XML, and it connects to your bot without signature headers. Without a terminal, pass `--yes` and `-o table` (in JSON mode it prints nothing until it exits).

Observed when: a person heard the agent and the agent heard them, the callbacks arrived, and the record ends 4000 (Normal Hangup) or 4010 (End Of XML Instructions, the normal end of a keepCallAlive stream) after a real conversation. A 4010 within seconds of answer: read the bot's connect handler and the stream status callbacks before you blame the bot, because the call record does not say who closed the socket.

### Stage 6: production hosting

- Your own domain with a valid certificate, near the callers (Mumbai for India, US East or West for the US; the docs target under 1 s end to end). A stopped or rotated tunnel turns every call into a 7011.
- Re-run stages 3 and 4 against the production host. Then `plivo account applications update <app_id> --answer-url https://PROD/plivo/answer --hangup-url https://PROD/plivo/hangup --dry-run`, show the current values being replaced, and apply after approval (fallback via `plivo api`, stage 2).
- Set `statusCallbackUrl` on `<Stream>`: it is the only push signal for `DroppedStream` and `DegradedStream`.
- Make every callback handler idempotent, because Plivo retries: key stream callbacks on `StreamID` plus the event, call callbacks on `CallUUID`.
- Alert on the 7011 rate: an answer URL that fails under load keeps producing 7011s long after launch. Plivo's Voice Alerts also email you when callback failures pass 5%: `plivo docs show voice/concepts/voice-alerts` (<https://plivo.com/docs/voice/concepts/voice-alerts>).
- Walk stages 1 to 5 once more against production before the first external caller.

### Stage 7: hand off to a human

Your backend calls `plivo voice calls transfer <call_uuid> --legs aleg --aleg-url https://PROD/plivo/transfer/<call_uuid>`, and that URL returns document E below.

- Call the Transfer API first, then close the socket, and keep `keepCallAlive="true"`: the transfer XML is subsequent XML, so it runs only when the stream ends. To stop the stream through the API instead (`plivo voice calls streams stop <call_uuid> --yes`), transfer first, then stop: stopping first lets the leg finish its current document, which can end the call.
- In the same document instead: a `<Dial>` or `<Redirect>` after `<Stream>` runs when the socket closes.
- The Dial `action` URL receives `DialStatus` (`completed`, `busy`, `failed`, `cancel`, `timeout`, `no-answer`), `DialRingStatus`, `DialHangupCause`, `DialALegUUID` and `DialBLegUUID`. The human leg's `DialBLegHangupCauseCode`, `DialBLegHangupCauseName` and `DialBLegHangupSource` go only to the Dial `callbackUrl`.
- Contact centres: prefer SIP, `<User sipAuthUsername="..." sipAuthPassword="...">sip:queue@cc.example.com</User>`; failures are 4240 (auth failed) and 4250 (auth timeout). The centre must allow Plivo's SIP signalling and RTP ranges: `plivo docs show voice/concepts/firewall-network-configuration` (<https://plivo.com/docs/voice/concepts/firewall-network-configuration>). Set `dialMusic` so the caller does not hear silence.
- Many human legs never answer: handle `DialStatus` and keep a `<Stream>` or `<Speak>` after `<Dial>`. A busy human is a handoff outcome, not a stream failure.
- Observed when: one test transfer reports `DialStatus=completed`, caller and human hear each other, and the bot's audio has stopped. A failing transfer URL shows as 7013 or 8013.
- An AI as a MultiPartyCall participant (`role="ai-agent"`) is documented (`plivo docs show voice/xml/multiparty-call`, <https://plivo.com/docs/voice/xml/multiparty-call>; `plivo docs show voice/api/multiparty-calls`, <https://plivo.com/docs/voice/api/multiparty-calls>), but its runtime behaviour is not verified and the docs do not say what `to` should be for a participant reached over a WebSocket: ask Plivo before you build on it. `participant add --role` has no `ai-agent`, so it needs `plivo api POST /MultiPartyCall/name_<room>/Participant/`.

### Stage 8: operating it

| Watch | How |
|---|---|
| Failed calls | `plivo voice calls list --limit 20 -o json` (20 is the API's per-page maximum: page with `--offset`; the API searches the last 7 days by default). Read `hangup_cause_code`, `hangup_cause_name`, `hangup_source` and the duration. Do not filter out 4010: a refused or crashed stream ends exactly that way, so look for 4010s that last seconds |
| One failure | "When a call fails" below |
| Stream drops | `DroppedStream` and `DegradedStream` on your `statusCallbackUrl`; log the raw `Event` (the docs use two naming schemes) |
| Callers hanging up in seconds | Common on streamed calls and not proof of a bot fault. Speak first and fast; keep any `<Speak>` before `<Stream>` short |
| A ceiling | Calls ending 4010 at the same second: `streamTimeout` or your own timer |
| Recordings | `plivo voice recordings list --call-uuid <uuid>` |
| Rehearse before launch | Silence, barge-in, DTMF, a bot timeout, a refused socket and one dropped mid-call, malformed `playAudio`, a busy human, recording on and off, the stage 2 rollback |

## When a call fails

Run each layer once, keep the evidence, and do not repeat a billed call until the failed layer passes.

1. `plivo voice calls diagnose <call_uuid>`: Plivo's AI reads the call record, the SIP and media trace and your answer-URL responses. Allow 30 to 120 s; the answer is free text, not a schema; own account only. It shares a small per-account rate limit with `plivo ask`, so do not loop it.
2. `plivo voice calls get <call_uuid> -o json`: `hangup_cause_code`, `hangup_cause_name`, `hangup_source`, `answer_time`, `bill_duration`; after a handoff read the human's leg too. `plivo api GET /Call/<call_uuid>/` has the full record.
3. Re-run the stage 4 `curl` against the URL that failed: the answer URL, or the action, transfer or redirect URL for 7012/8012, 7013/8013 or 7014/8014 (those documents need no `<Stream>`). Headers show 401, 404, 405 or 530; the XML check finds body faults.
4. Re-run stage 3 against the stream URL.
5. Console: Voice, Logs, Calls, the call. Audio Streams has the stream's debug logs (events, the `DroppedStream` error text); Call Insights flags one-way, broken or robotic audio and lag. Then `plivo ask --call-uuid <uuid> "<question>"`, or Plivo support with every leg's call UUID, UTC times, the code, name and source per leg, the answer URL's host only, HTTP status and latency from your logs, the exact XML body (redacted), the stream callbacks received, the WebSocket close code, and whether stage 3 passes. Never send tokens or caller audio.

| Code or signal | Meaning on a streamed call | Next |
|---|---|---|
| 7011 Error Reaching Answer URL | No usable HTTP response: 404, 401 or 403 (your auth), 405 (method), 502 or 530 (dead tunnel), a timeout | Step 3; no credentials on the URL; a fallback URL |
| 8011 Invalid Answer XML | A 200 that is not Plivo XML: empty, JSON (often a console flow application, stage 2), HTML, malformed, an unsupported `<Speak language>`, Twilio `<Reject/>` | The number's application; the XML check on the exact body |
| 4010 End Of XML Instructions | The normal end of a keepCallAlive stream. Within seconds of answer, also how a refused or crashed stream ends | Bot logs and stream callbacks |
| 4000 Normal Hangup | A person hung up. Source `Answer XML` means your `<Hangup/>` ran | Nothing |
| 3020 / 3010, source Answer XML, 0 s | `<Hangup reason="rejected"/>` or `reason="busy"` ran first (seen in call records; no docs page maps it). A bare `<Hangup/>` does not produce these | Intended? |
| 7012/8012, 7013/8013, 7014/8014 | The action, transfer or redirect URL was unreachable, or returned bad XML | Step 3 against that URL |
| 6020 Media Timeout | No media for 60 s; the code does not say which side | Both media paths, the carrier, Call Insights |
| 2070 / 5030 | India: a leg or your server is outside India / over the concurrency limit (any region, rejected at once) | "India"; raise the limit |
| 3030 / 2030 | The caller ID is neither rented on this account nor a verified caller ID / the destination is barred by geo permissions | Your Plivo number; Voice, Geo Permissions |
| 9100 Machine Detected | `machine_detection=hangup` met a voicemail | Expected |
| `DroppedStream` (stream callback) | The socket failed to connect or died mid-call; a `DegradedStream` before it means too slow | Server, tunnel, pacing |

Every other code, including the carrier codes an outbound campaign sees daily: `plivo docs show voice/troubleshooting/hangup-causes` (<https://plivo.com/docs/voice/troubleshooting/hangup-causes>). A carrier code, a busy human, an out-of-credit cancel or a person hanging up is not a WebSocket defect.

## The XML: compose it, check it

Ask only what is unknown: the direction; the `wss://` URL; the codec (`audio/x-mulaw;rate=8000` unless the speech model wants `audio/x-l16;rate=16000`); whether to record and where the recording URL goes; anything before the agent (a greeting, a recording notice, a keypad menu); the handoff (none, a number, a SIP address, a URL that decides, a room) and whether the bot takes the caller back; a stream status callback URL (strongly recommended). Do not ask about `keepCallAlive`, `streamTimeout`, `audioTrack`, `noiseCancellation` or `extraHeaders`: use the attribute table.

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

**C. Greeting and keypad menu, then stream.** Everything before `<Stream>` delays the bot's first word. `redirect` defaults to `true` on `<GetDigits>`, `<GetInput>`, `<Record>`, `<Dial>` and `<Conference>`: a caller who responds then gets whatever the `action` URL returns and skips the rest of this document, while a caller who does not falls through after `retries`. `redirect="false"` keeps everyone on this document; the menu URL still gets the `Digits`, must answer 200 fast, and is where you store the choice for the bot (key it on `CallUUID`). To route each key to a different bot instead, keep the default, put a `<Speak>` and a `<Hangup/>` after `<GetDigits>` for the no-input path, and have the menu URL return document A or B.

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
  <MultiPartyCall role="customer" coachMode="true" maxDuration="900" maxParticipants="10" record="false" recordParticipantTrack="true" statusCallbackEvents="mpc-state-changes,participant-state-changes" statusCallbackUrl="https://voice.example.com/plivo/mpc-status" statusCallbackMethod="POST">room-{{CallUUID}}</MultiPartyCall>
</Response>
```

**E. Transfer document** (stage 7), served from the transfer URL. The `<Stream>` after `<Dial>` brings the caller back to the bot when the human does not answer.

```xml
<Response>
  <Record recordSession="true" fileFormat="mp3" maxLength="3600" callbackUrl="https://voice.example.com/plivo/recording" callbackMethod="POST"/>
  <Dial callerId="{{To}}" timeout="30" redirect="false" action="https://voice.example.com/plivo/dial-result" method="POST"><Number>+91XXXXXXXXXX</Number></Dial>
  <Stream bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
</Response>
```

### Check the XML before you serve it

Check the exact body the URL returns. A pass proves the document only, not that Plivo can fetch it or that a call connects.

**Will break.** Fix before dialling:

- An empty body: 8011 if the server answered 200, 7011 if it answered non-2xx or not at all, so read the status before you name a code.
- JSON, HTML or XML that is not well formed (including two concatenated documents, or a comment containing `--`): 8011. JSON you did not write usually means a console flow application owns the number (stage 2).
- A root other than `<Response>`, or an empty `<Response/>` (answered, then ended at once with 4010).
- A top-level Twilio `<Reject/>`: observed to fail with 8011; the Plivo form is `<Hangup reason="rejected"/>`. Plivo's elements are Response, Record, Stream, Speak, Play, GetDigits, GetInput, Dial, Number, User, Conference, MultiPartyCall, Redirect, Wait, Hangup, PreAnswer, DTMF and Message. Hold music is the `agentHoldMusicUrl` and `customerHoldMusicUrl` attributes of `<MultiPartyCall>`, not an element.
- An answer document that can never reach a `<Stream>` (or a `MultiPartyCall role="ai-agent"`), directly or through an action URL that returns one. Action, transfer and redirect documents need no `<Stream>`.
- Only `<Hangup/>`: the call answers and ends gracefully at once, and the record looks like a normal finish. Use a `<Speak>` placeholder while building; deliberate screening is `<Hangup reason="rejected"/>` or `<Hangup reason="busy"/>`.
- A talking bot's `<Stream>` without `keepCallAlive="true"`, except before a MultiPartyCall (document D). Without it the next element runs at once (documented), and with nothing after the stream the call ends within seconds with 4010 (observed).
- A `<Stream>` with no URL, a scheme other than `wss://` or `ws://`, a localhost host, or a URL over 2048 characters.
- `audioTrack` other than `inbound`, `outbound` or `both`, or `outbound` or `both` together with `bidirectional="true"` (the docs forbid it).
- A method attribute other than GET or POST; `extraHeaders` over 512 bytes.
- An action, callback, hold-music or `<Redirect>` URL that is not absolute http(s), still holds a placeholder, or points at localhost; an empty `<Redirect>`; an empty `<Number>` or `<User>`, or a `<Dial>` with neither; a `<GetInput>` without `action` (on `<GetDigits>` it is optional).
- A `<Speak language>` the chosen voice does not support: a `<Speak language="hi-IN" voice="WOMAN">` logged 8011. `WOMAN` and `MAN` cover da-DK, nl-NL, en-AU, en-GB, en-US, fr-FR, fr-CA, de-DE, it-IT, pl-PL, pt-PT, pt-BR, ru-RU, es-ES, es-US and sv-SE; other languages need a `Polly.<Name>` voice (`hi-IN` with `Polly.Aditi`, `en-IN` with `Polly.Raveena`; the full list: `plivo docs show voice/concepts/ssml`, <https://plivo.com/docs/voice/concepts/ssml>). Also a `voice` other than `WOMAN`, `MAN` or `Polly.<Name>`, and a `<Play>` holding text instead of an audio URL.

**Risky.** Say what to check; name no hangup code:

- No `bidirectional="true"`: the caller hears nothing from the bot (right only for transcription or monitoring).
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
| `bidirectional` (`bidirectional`) | `true` | Default `false`: the caller hears nothing from the bot |
| `keepCallAlive` (none) | `true`, except before a MultiPartyCall | Default `false`. With it the stream runs alone and later XML runs only when it ends |
| `contentType` (`content_type`) | `audio/x-mulaw;rate=8000` | Always set it: the reference gives the default as `audio/x-l16;rate=8000`, the guide says mu-law. Also documented: `audio/x-l16;rate=8000`, `audio/x-l16;rate=16000`, `audio/x-l16;rate=24000`; mu-law is 8 kHz only |
| `statusCallbackUrl`, `statusCallbackMethod` (`status_callback_url`, `status_callback_method`) | Your URL, `POST` | The only push signal for drops |
| `streamTimeout` (`stream_timeout`) | Omit (86400 s) | When it fires the stream stops and, with nothing after it, the call ends 4010. 300 or 600 s cuts real conversations |
| `audioTrack` (`audio_track`) | Omit (`inbound`) | `outbound` and `both` are not allowed with `bidirectional="true"` |
| `extraHeaders` (`extra_headers`) | `k1=v1,k2=v2`, no secrets | Max 512 bytes; arrives in `start` as `extra_headers` and is logged. The page also prints a `[A-Za-z0-9]` constraint that its own example (`userId=12345,sessionId=abc123`) breaks; keys with `_` or `-` are seen to work, so test unusual characters rather than reject them |
| `noiseCancellation`, `noiseCancellationLevel` (`noise_cancellation`, `noise_cancellation_level`) | Omit, or `"true"` with 60 to 100 (default 85) | Filters the caller's audio |

Docs: `plivo docs show voice-agents/audio-streaming/xml/stream` (<https://plivo.com/docs/voice-agents/audio-streaming/xml/stream>); the 24 kHz value is on `plivo docs show voice/xml/audio-streaming` (<https://plivo.com/docs/voice/xml/audio-streaming>).

Limits, from <https://plivo.com/docs/voice-agents/audio-streaming/concepts/audio-streaming-guide> (its CLI copy, `plivo docs show voice-agents/audio-streaming/concepts/audio-streaming-guide`, stops before the limits table) and the best-practices page: a stream URL of 2048 characters; one stream per call; WebSocket messages up to 64 KB, audio chunks of 16 KB base64 or less; about 20 ms of audio per `media` frame; a playback buffer of 40 s on the best-practices page (`DegradedStream` at 30, 60 and 90% full) and about 60 s in the guide. If the first connection fails, Plivo tries twice more, then drops the stream. Plivo closes the socket when the call ends.

Plivo sends JSON text frames, never binary:

| `event` | Key fields |
|---|---|
| `start` | Once: `sequenceNumber` (from 1), `start.callId`, `start.streamId`, `start.accountId`, `start.tracks`, `start.mediaFormat.encoding`, `start.mediaFormat.sampleRate`, `extra_headers` |
| `media` | `streamId`, `media.track`, `media.timestamp`, `media.chunk`, `media.payload` (base64 raw audio, no WAV header) |
| `dtmf` | `dtmf.digit` (`0-9`, `*`, `#`, `A-D`), `dtmf.track`, `dtmf.timestamp` |
| `playedStream` | `name` of a checkpoint that has played |
| `clearedAudio` | `streamId`, after your `clearAudio` |

The bot sends:

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
- `extra_headers` is metadata, not authentication. Only noise cancellation is documented, not echo cancellation: if the bot hears itself, gate STT while it speaks.
- Full schemas: `plivo docs show voice-agents/audio-streaming/concepts/audio-streaming-reference` (<https://plivo.com/docs/voice-agents/audio-streaming/concepts/audio-streaming-reference>).

Stream status callbacks: the troubleshooting and best-practices pages and the console debug logs use the `Event` values `StartStream`, `StopStream`, `DroppedStream` and `DegradedStream`; the guide and the reference describe `started`, `stopped` and `failed`. Log the raw value. Common fields: `CallUUID`, `StreamID`, `Timestamp`, `From`, `To`, `Direction`. Acknowledge with a 200 (any body). Symptoms and error strings (`connection_failed`, `authentication_failed`, `invalid_content_type`, `buffer_overflow`): `plivo docs show voice-agents/audio-streaming/troubleshooting/troubleshooting` (<https://plivo.com/docs/voice-agents/audio-streaming/troubleshooting/troubleshooting>).

To start a stream over REST instead of XML: `plivo voice calls streams start <call_uuid> --url wss://... --bidirectional --content-type "audio/x-mulaw;rate=8000"`. Quote the content type (a bare `;` ends the shell command) and always pass it (the CLI default is `audio/x-l16;rate=16000`). Its `--stream-status-callback` sends `stream_status_callback_url`, a field the API does not document, so for status callbacks use `plivo api POST /Call/<call_uuid>/Stream/ --body '{"service_url":"wss://...","bidirectional":true,"content_type":"audio/x-mulaw;rate=8000","status_callback_url":"https://..."}' --dry-run`, then `--yes`.

Plivo moves raw audio only: STT, the model and the voice are yours, and Plivo TTS exists only as `<Speak>` before or after the stream. A streamed call is billed as the underlying call; quote no prices.

## Callbacks, signature validation, timeouts

- Answer, fallback and `action` URLs must return Plivo XML; `hangup_url`, `ring_url`, `callbackUrl` and `statusCallbackUrl` need only a 200. Plivo retries, so make handlers idempotent: the keys are listed in `plivo docs show voice/concepts/callbacks` (<https://plivo.com/docs/voice/concepts/callbacks>).
- Per-URL timeouts, retries and edge region go in a URL fragment such as `#ct=2000&rt=5000&rc=2&er=mumbai`: `plivo docs show voice/concepts/callback-configurations` (<https://plivo.com/docs/voice/concepts/callback-configurations>). A `<Stream>` `statusCallbackUrl` is not among the URLs it applies to. Still answer with XML well inside 15 s.
- Every Plivo request carries `X-Plivo-Signature-V3`, `X-Plivo-Signature-Ma-V3` and `X-Plivo-Signature-V3-Nonce`. Validate with your SDK's helper (Python `plivo.utils.validate_v3_signature`, Node `plivo.validateV3Signature`): `plivo docs show voice/concepts/signature-validation` (<https://plivo.com/docs/voice/concepts/signature-validation>). Where the page's prose or worked example disagrees with the SDK, follow the SDK. With several active Auth Tokens a header holds a comma-separated list: accept any match. V3 is signed with the token of the account or subaccount that owns the number, Ma-V3 with the main account's.
- The WebSocket upgrade carries the same headers. Validate it as a `GET` over your stream URL with `wss://` replaced by `http://`, keeping the port and query as dialled: on a live call only that form matched, not `wss://` or `https://`.
- Behind a proxy, validate against the URL Plivo dialled (its public host, port, path and query, and `https://` for HTTP callbacks), not a rewritten private one. If validation fails and you return no XML, the call ends 7011 or 8011, so reject only what is surely not Plivo's. Never log the token.
- Firewalls: Plivo calls your URLs from regional edge IPs, listed on the firewall page (stage 7). Without an allow-list, rely on signatures.

## India: what a voice agent needs before its first call

Never infer country rules from a phone prefix; check the account. Report each item as observed, not known or failed:

1. **Organisation.** An India data-region organisation. The region is fixed at creation: make a new organisation from the console's organisation switcher (no new signup). Only India-registered businesses can rent Indian numbers and call on domestic routes; INR accounts can call only within India.
2. **KYC.** An `accepted` compliance application: `plivo numbers compliance list --country IN --status accepted -o json`. `submitted` is not `accepted`. A number's `compliance_status` in `numbers get` is not in the published schema, so treat a missing field as not known.
3. **Series.** Landline (022, 080) for service and transactional calls; 140-series for promotional calls only; 160-series for BFSI only. The wrong series makes every complaint count as UCC, even with consent. 140 and 160 numbers are provisioned offline (Tata DLT registration, a NOC, header and template approval, days to weeks, no CLI): `plivo docs show voice/concepts/140-series-provisioning` (<https://plivo.com/docs/voice/concepts/140-series-provisioning>) and `plivo docs show voice/concepts/160-series-provisioning` (<https://plivo.com/docs/voice/concepts/160-series-provisioning>). Which series allow `<Stream>` is not documented.
4. **Media anchoring.** Both legs and your bot's server stay in India, or the call fails with 2070.
5. **Consent.** Cold calling is prohibited. A UCC complaint needs opt-in proof within 5 business days or the compliance ID is blocked; repeated complaints suspend it, and numbers on a suspended application cannot place calls. Remove complainants at once. Rules: `plivo docs show voice/concepts/ucc-management` (<https://plivo.com/docs/voice/concepts/ucc-management>). Complaints are readable with `plivo api GET /Ucc/`; proof is a multipart upload the CLI cannot send, so upload it on the console UCC dashboard.
6. **Capacity.** Professional accounts start at 50 concurrent calls. Over the limit a call is rejected at once with 5030 and Make Call returns HTTP 403. Raise the limit with Request Enterprise under Organization settings > Account limits before a campaign: `plivo docs show voice/concepts/account-limits` (<https://plivo.com/docs/voice/concepts/account-limits>).
7. **Caller ID.** A Plivo-rented Indian number; Verified Caller ID does not apply in India.

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
- `calls make` from a number whose application is not `accepted` fails with `cannot place calls as its compliance application is not in 'accepted' status`; `suspended` means unresolved UCC complaints.

Documents, statuses and rejection reasons: `plivo docs show numbers/rent-india-numbers` (<https://plivo.com/docs/numbers/rent-india-numbers>).

## Outbound

- Prove inbound, or at least stage 3, first.
- Answering-machine detection is asynchronous: `calls make --machine-detection true` (or `hangup`) delivers `Machine=true` to `machine_detection_url` after your `<Stream>` has started. The CLI has no flag for that URL or the timing parameters, so use `plivo api POST /Call/` (preview first). On a machine, hang up, transfer to a voicemail URL, or tell the bot. Parameters: `plivo docs show voice/concepts/machine-detection` (<https://plivo.com/docs/voice/concepts/machine-detection>).
- US: use a Plivo number rented on this account as the caller ID; it is the only way to get STIR/SHAKEN attestation A. Do not hang up on every voicemail: short calls count against the quality thresholds and draw surcharges. Calls above your CPS are queued, so pace your own requests. Thresholds and CPS: `plivo docs show voice-agents/audio-streaming/deploy/us-call-quality-and-cps` (<https://plivo.com/docs/voice-agents/audio-streaming/deploy/us-call-quality-and-cps>); concurrency: the account-limits page above.
- Professional plans can call only the US and India; other countries need Enterprise, and barred destinations fail with 2030: `plivo docs show voice/concepts/geo-permissions` (<https://plivo.com/docs/voice/concepts/geo-permissions>).
- A US-region trial organisation may see "Voice capability is currently disabled for this account" on outbound calls: request access in the console (not in the docs).
- Measure your own answer and connect rates by destination, list and time window.

## When this skill does not have the answer

Do not guess from other platforms. Search the docs first, offline and without credentials: `plivo docs search <keywords>`, then `plivo docs show <path>`. Next, `plivo ask "<question>"`, which can also see the account; it shares the small rate limit with `diagnose` (on `RATE_LIMITED`, wait as told). Treat its answer as evidence and prefer the docs where they conflict. Without the CLI, ask the assistant in the Plivo console. The docs do not answer the price of a streamed call, Plivo's setup latency, data retention, HIPAA or BAA, which India series allow `<Stream>`, `<Stream>` on SIP-trunk legs, or the longest accepted answer URL: say so and route to Plivo support or the account manager. Out of scope: SIP-connected agent platforms (`plivo skill install sip-trunking`), Plivo's hosted AI Agents, SMS, browser and SIP endpoints, and the inside of the bot.
