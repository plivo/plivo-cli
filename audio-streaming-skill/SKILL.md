---
name: plivo-audio-streaming
description: Connect a WebSocket voice bot to phone calls with Plivo Audio Streaming (the <Stream> element) and take it from local testing to production, using the Plivo CLI. Load this whenever a Plivo task involves <Stream>, WebSocket audio, a voice bot or agent, Pipecat, "connect my bot to a number", "caller hears nothing", "invalid answer XML (8011)" on a streamed call, "error reaching answer URL (7011)", a hangup code, "why did this call fail", recording or transfer-to-human for an AI agent, India KYC, 140-series or UCC for a calling agent, or "am I ready for production". Readiness gates first, then eight go-live stages with a check at each, a CLI-first debugging loop (diagnose, get, check the body), a plain-language XML checklist you apply before serving it, and a read-only readiness checklist. Self-contained: it carries everything the WebSocket bot journey needs, and points at plivo-voice-xml and plivo-cli as separate installs for work outside that journey.
license: Apache-2.0
---

# Plivo Audio Streaming: connect a bot to calls and go live

You are guiding a developer, or their coding agent, to a working and monitored voice agent. Go stage by stage. Do not skip a check because the user is confident. Speak plainly. Use the Plivo CLI for every Plivo-side step and `-o json` when you need to read a value. Every CLI flag here was taken from the CLI's own `--help`; when this file and `plivo <command> --help` disagree, the CLI is right. If a step has no CLI command, this file says so and gives the API or console path. This skill covers Plivo call control and the WebSocket boundary; it does not cover the STT, LLM or TTS pipeline inside the bot.

**What this file assumes you have: nothing but this file, the `plivo` CLI and your own bot.** Everything the WebSocket bot journey needs is here: the readiness gates, the Stream XML with documents to copy, the XML checks that matter for a streamed call, the callbacks and signature recipe, the WebSocket protocol, the hangup codes and the India prerequisites. Other Plivo skills are separate single files that you may not have. Install one only if the task moves outside this journey:

- `npx skills add https://www.plivo.com/docs --skill plivo-voice-xml` for general Plivo XML: every element other than `<Stream>` in full, its complete attribute tables, and IVR, conference and voicemail flows that have no stream in them.
- `plivo skill install` for `plivo-cli`, the CLI's own reference (every command, the JSON envelope, exit codes, headless auth). The CLI writes that file out itself.

If neither is installed, do not stall and do not guess: use `plivo <command> --help` for CLI questions and <https://www.plivo.com/docs/voice/xml/overview> for XML questions, and say which source you used.

Why the checks matter: most first calls that fail do so before the agent is involved. The usual causes are a number attached to an application that is not the one serving your XML, an answer URL that rejects Plivo's request, a stopped tunnel, or a socket the bot closes on connect. Each stage below catches one of them for free, before a billed call.

## What a finding licenses you to say

Three tiers, and they decide your verdict.

- **Will break.** A documented rule says so, or a call with this shape is known to have failed. Say the call breaks, and name the failure.
- **Risky.** Plausible, unverified, or seen to work in some deployments. Say what could go wrong and what to check. Name no hangup code.
- **Style.** No functional effect. Say so.

Nothing outside the first tier is a reason to tell someone their call will break. The checklist below is split into those tiers; when a rule elsewhere in this file does not say which tier it is in, it is risky. A true observation about a document is not by itself a verdict: a design risk you would raise in review is still a document that connects a call.

What is in this file, in order: the readiness checklist, prerequisites by country, eight go-live stages, the CLI debugging loop with a hangup-code table, what not to do, how to compose and check the XML with documents you can copy, then the deep sections (India, outbound agents, callbacks and signatures, the WebSocket protocol, hangup causes, questions the docs do not answer).

## Before you start: three questions

1. Inbound (people call you), outbound (you call them), or both?
2. What runs the agent: Pipecat, another framework, a hosted pipeline or speech-to-speech model behind one realtime endpoint, or your own WebSocket server? Is it reachable at a public `wss://` URL yet, or only on a laptop? (Docs have step-by-step Pipecat and Cloudflare guides. The protocol is the same for all.)
3. Which country are the numbers in? Read "Prerequisites by country" before renting anything.

## Readiness: five gates, each reported as observed, unknown or failed

Report a gate as observed only when you saw the evidence in this session (a command's output, a log line, a console page). Write "not known" for anything you did not see; never assume it. Never say "ready". Do not place a live call merely to discover basic configuration; gates 1 to 4 are free.

Run this checklist in order. Stop at the first failure and fix it there.

1. `plivo auth whoami -o json`. Look for: the account you meant, and credits above zero.
2. `plivo numbers get <number> -o json`. Look for: the number is on this account and `voice_enabled` is true. For an Indian number also look for `compliance_status`: it should read `accepted`, and `submitted` is not `accepted`. `compliance_status` is returned by the live API but is not in the published phone number schema, so treat a missing field as "not known" rather than as a failure, and confirm with `plivo numbers compliance list --country IN --status accepted -o json`. Note the `app_id`: it is your rollback value.
3. `plivo account applications get <app_id> -o json`. Look for: an answer URL on a real host (https, not `localhost`, not a temporary tunnel host), a fallback answer URL, and a hangup URL. A number with no application attached cannot route a call. A number attached to a different application runs that application, not your XML, so your answer URL is never fetched: check what the number actually points at before you debug your own server.
4. `curl -s -i -X POST <answer-url> -d 'CallUUID=readiness&From=%2B10000000000&To=%2B<number>&Direction=inbound&CallStatus=ringing&Event=StartApp'`. Look for: HTTP 200, a non-empty body, a response in well under 15 s, and no credential prompt. Then apply the XML checklist in "Check the XML before you serve it" to the body.
5. `plivo voice streams test --to <wss url from that XML> --bidirectional --duration 5`. Look for: `Received N frames back`.
6. One real call, then `plivo voice calls get <call_uuid> -o json` and `plivo voice calls diagnose <call_uuid>`.

| Gate | Observed when | Checklist step |
|---|---|---|
| 1. Account and number | the number exists, is voice-enabled, and (India) an accepted compliance application is attached | 1 and 2 |
| 2. Application | the number points at your XML application with answer, fallback and hangup URLs on a real host | 3 |
| 3. Answer URL | one request returned Plivo XML that passes the XML checklist | 4 |
| 4. WebSocket | `plivo voice streams test --bidirectional` received frames back | 5 |
| 5. A real call | an answered call with agent audio both ways, callbacks received, call record read | 6 |

Steps 1 to 5 are static or synthetic. Only gate 5 proves signed HTTP from Plivo's edge, audio both ways, callbacks and the call record. Before launch, tighten the same checklist: a tunnel host, a missing fallback URL and a `<Stream>` without `statusCallbackUrl` all count as failures rather than warnings.

There is no single CLI command that runs the whole checklist. Run the steps by hand, in order.

If your answer URL validates the Plivo signature (recommended), step 4 will be rejected unless you sign the request yourself. Either sign it with the recipe in "Callbacks, signature validation, timeouts", or read the response of a real call from the console instead. A 401 from an unsigned probe proves nothing.

## Prerequisites by country

India first, because nothing works until this is done. Detail and every command: "India: what a voice agent needs before its first call". Docs: <https://www.plivo.com/docs/voice/concepts/india-calling>, <https://www.plivo.com/docs/numbers/rent-india-numbers>, <https://www.plivo.com/docs/numbers/compliance>, <https://www.plivo.com/docs/voice/concepts/140-series-provisioning>, <https://www.plivo.com/docs/voice/concepts/160-series-provisioning>, <https://www.plivo.com/docs/voice/concepts/ucc-management>. Never infer country rules from a phone prefix; check the account.

1. Account. An India data-region organisation is required and cannot be changed later. Create a new org from the console switcher if you are in the US region. Only India-registered businesses may rent Indian numbers. INR accounts can only call within India. `plivo auth whoami -o json` shows the account. There is no CLI for data region.
2. KYC. One rule: run `plivo numbers compliance requirements --country IN --number-type local --user-type business -o json` and supply every document type it returns.
   The public pages disagree on the count, so do not hard-code one or two. The first application must be sealed and signed.
   Review for 080 and 022 numbers is automated, typically about 5 minutes. `submitted` is not `accepted`.
   An agent can run the whole flow from the CLI: `requirements`, `create` with the files, poll `get`, then `buy` or `link`.
   It needs the certificate files and the exact legal details from the user and must not fill any of them in itself. Preview, then ask before every `--yes`.
3. Compliance must be `accepted` to rent and to call. Otherwise rent fails with `compliance_application_id is required` and `calls make` fails with `from number +91... cannot place calls as its compliance application is not in 'accepted' status`. Attach an application to an existing number with `plivo numbers compliance link --link +91...=<compliance_id>`.
4. Number series. Landline (022, 080): service and transactional calls only. 140-series: promotional only (Tata DLT registration, declaration, NOC, header and template approval; about 5 to 10 business days; no CLI). 160-series: BFSI only (about 7 to 14 business days). The wrong series makes every complaint count as UCC even with consent. Which series allow `<Stream>` is not documented.
5. Media anchoring. Both legs of every call must stay in India, your server too. Otherwise the call fails with 2070 `violates_media_anchoring`.
6. Consent. Cold calling is prohibited. A UCC complaint needs opt-in proof within 5 business days, or the compliance ID is blocked for 15 days. Five or more complaints in 10 days means suspension. Never call a complainant again. UCC API via `plivo api` (no typed command).
7. Capacity. Default concurrency limit of 50 (CPS = concurrency / 25). Over the limit is rejected instantly with 5030. Raise it through a support ticket before a campaign.

US. No KYC and no 10DLC for voice. Detail: "Outbound voice agents".

- Use a Plivo number you own as caller ID. It is the only way to get STIR/SHAKEN attestation A. A verified external number may be used but gets no attestation A and no protection from spam labels; Caller Reputation is optional and paid.
- Stay under the quality thresholds: abandoned calls under 20% and short calls (6 s or less) under 10% of monthly volume, or surcharges apply.
- Default 2 CPS; overflow is queued, not rejected. Professional plans can call only US and India.
- US-region trial organisations may see "Voice capability is currently disabled for this account" on outbound. Request access from the console. This is not in the docs.

Other countries: check coverage, geo permissions and caller-ID rules on the public pages. If a rule is absent, say "not known" and ask Plivo support.

## Stage 1: account and number (5 minutes)

```bash
plivo auth whoami -o json                                  # right account? credits > 0?
plivo numbers list --services voice -o json                # a voice-enabled number?
plivo numbers compliance list --country IN --status accepted -o json   # India only: must be non-empty
plivo numbers search --country IN --type local --limit 5
plivo numbers buy <number> --dry-run                       # preview; then, after the user agrees: --yes (spends money)
```

Check: the number exists and `voice_enabled` is true. India: compliance is `accepted`. A number hosted at another carrier can still reach the agent: forward it to a Plivo number, or have the carrier send SIP to `sip:<app_id>@app.plivo.com` with SIP authentication (docs: <https://www.plivo.com/docs/voice/use-cases/connect-external-numbers>).

## Stage 2: an application with all three URLs (5 minutes)

```bash
plivo numbers get <number> -o json      # record the current application id first: it is your rollback
plivo account applications create --app-name voice-agent \
  --answer-url   https://YOUR-HOST/plivo/answer --answer-method POST \
  --fallback-answer-url https://YOUR-HOST/plivo/fallback \
  --hangup-url   https://YOUR-HOST/plivo/hangup --dry-run     # preview, then --yes
plivo numbers update <number> --app-id <app_id> --dry-run     # preview, then --yes
plivo numbers get <number> -o json      # confirm the application points at the new app
```

Check: the number points at your XML application, the one whose answer URL returns `<Response>`. The most common first-call failure is a number still pointing at a different application, so your answer URL is never fetched and the caller hears whatever that application does. A console flow application that has no working flow answers with JSON rather than Plivo XML, which the call record reports as 8011. Never leave the fallback URL empty. `applications update` has no fallback flag: set it at create time, or use the API, previewing first:

```bash
plivo api POST /Application/<app_id>/ --body '{"fallback_answer_url":"https://YOUR-HOST/plivo/fallback"}' --dry-run   # shows the exact request
plivo api POST /Application/<app_id>/ --body '{"fallback_answer_url":"https://YOUR-HOST/plivo/fallback"}' --yes       # only after the user approves
```

Rollback: `plivo numbers update <number> --app-id <previous_app_id>` (preview it with `--dry-run` first).

## Stage 3: prove the WebSocket, no phone involved (2 minutes)

```bash
plivo voice streams test --to ws://localhost:7860/ws --bidirectional --duration 5    # laptop first (loopback only)
plivo voice streams test --to wss://YOUR-HOST/ws --bidirectional --duration 5        # then the public host
```

Check: you see `Received N frames back`. If not, your server accepts the connection but never sends `playAudio`, so callers will hear silence. If the connection fails, the server is not public, not TLS, or not a WebSocket at that path. Fix here; this is free. Two limits worth knowing. The CLI's own help calls this a pure-client pre-flight: no call is placed and Plivo's backend is not involved, so frames coming back prove your endpoint speaks the Plivo stream shape and nothing more. And the client ends the run by sending a JSON `{"event":"stop"}` text frame and then closing the socket, so a bot that waits for `stop` will see it here. Handle both anyway: treat either the `stop` frame or the WebSocket close as the end of the stream, because a real call can drop without a clean stop. Run it with `--codec mulaw` and `--codec l16` separately if you support both, and read the codec your bot actually received from the `start` frame rather than assuming.

What the bot must speak (schemas and handling rules in "The WebSocket protocol"):

- Plivo sends JSON text frames: `start` (callId, streamId, mediaFormat, your `extra_headers`), then about 20 ms `media` chunks (base64 raw audio, no WAV header), `dtmf` on keypress, `playedStream` when a checkpoint plays, `clearedAudio` after a clear. Read the codec from `start.mediaFormat`, not from what you think the XML said.
- The bot sends `playAudio` (contentType and sampleRate must match the XML), `clearAudio` for barge-in, `checkpoint` to learn when a sentence finished, `sendDTMF` to drive an external IVR.
- Send audio at real-time cadence. Bursting fills the playback buffer and makes barge-in late. If the initial WebSocket connection fails, Plivo attempts twice more before disconnecting the stream, and closes the socket when the call ends (<https://www.plivo.com/docs/voice-agents/audio-streaming/concepts/best-practices>, "WSS Socket Connection Failures").

## Stage 4: the XML (5 minutes)

Use the XML section below: answer the questions, copy the closest document, edit it, then check what your server actually returns.

```bash
curl -s -X POST https://YOUR-HOST/plivo/answer \
  -d 'CallUUID=test&From=%2B91...&To=%2B91...&Direction=inbound&Event=StartApp'
```

Check: the body passes every line of "Check the XML before you serve it". That proves the document only: not that Plivo can fetch it (a 7011 leaves no trace in the body), not that a call connects. The document must have `<Stream bidirectional="true" contentType="...">`, and `keepCallAlive="true"` on every document except the one that puts the caller in a MultiPartyCall room, where the room has to run after the stream starts (docs: <https://www.plivo.com/docs/voice-agents/audio-streaming/xml/stream>). Your answer URL must accept POST without Basic or bearer credentials and must not sit behind login middleware that a signed Plivo request cannot pass. Require a valid `X-Plivo-Signature-V3` instead (recipe in "Callbacks, signature validation, timeouts"). The XML overview gives Plivo a 15-second timeout for XML responses, so answer well inside that. Optional: `noiseCancellation="true"`.

No CLI command fetches the answer URL the way Plivo does. Use the `curl` above and the checklist, and remember that `curl` sends no `X-Plivo-Signature-V3`: if your endpoint validates signatures, sign the probe yourself with the recipe in "Callbacks, signature validation, timeouts" or read the body of a real call from the console instead.

## Stage 5: first real call, still on a laptop (10 minutes)

If the agent is only local: `plivo voice streams forward --number <n> --app <app_id> --to ws://localhost:7860/ws` saves the app's current answer URL, starts a tunnel and a local HTTP and WebSocket server, points the app at the tunnel, and restores the original answer URL on Ctrl-C. Its help says it needs no tunnel setup: it defaults to localhost.run over ssh, which needs no install and no account, and uses ngrok only when ngrok is already on the PATH or at `~/.plivo/bin/ngrok`. `--tunnel auto | ngrok | localhost.run` forces the choice. The only mutation is that one field on that one app. Read `plivo voice streams forward --help` before you run it, because it does change your application for the session. Then dial the number from a phone, or:

```bash
plivo voice calls make --from <your number> --to <your phone> --answer-url https://YOUR-HOST/plivo/answer --answer-method POST --dry-run   # preview, then --yes
plivo voice calls list --limit 1 -o json
plivo voice calls diagnose <call_uuid>
```

Check: the call is answered, you hear the agent, and the call record ends with `Normal Hangup` (4000) or `End Of XML Instructions` (4010). 4010 is the normal end of a keepCallAlive stream. A call that ends 4010 within a few seconds of answer is worth taking to the bot's connect handler first, but the call record does not say who closed the socket: confirm in the bot's logs and in the stream status callbacks before you call it a bot fault. A `request_uuid` from `calls make` means Plivo accepted the request, nothing more. Anything else: go to "When a call fails".

## Stage 6: move to production hosting (before anyone else dials)

- Own domain with a valid certificate, hosted near the callers (Mumbai for India, US East or West for the US; the docs' latency budget is under 1 s end to end). Free tunnel URLs are temporary: a stopped or rotated tunnel turns every call into a 7011. Use a stable host before launch.
- Re-run stage 4's `curl` and checklist and stage 3's `streams test` against the production host.
- `plivo account applications update <app_id> --answer-url https://PROD-HOST/plivo/answer --hangup-url https://PROD-HOST/plivo/hangup --dry-run`, show the previewed request and the current values you are replacing, then run the same command with `--yes` once the user approves (fallback via `plivo api`, see stage 2).
- Set `statusCallbackUrl` on `<Stream>`. Callbacks surface `DroppedStream` to your own monitoring; the console's Audio Streams debug logs remain a manual fallback. Make every callback handler idempotent on `CallUUID` (Plivo retries).
- Alert on the 7011 rate. An answer URL that fails under load with no fallback URL produces recurring 7011s long after launch; treat that as an availability incident, not a setup problem.
- Walk the readiness checklist once more against the production host before the first external caller.

## Stage 7: handoff to a human (only if you need it)

Production pattern: your backend calls `plivo voice calls transfer <call_uuid> --legs aleg --aleg-url https://PROD-HOST/plivo/transfer/<call_uuid>`. The transfer URL returns `<Dial callerId action timeout="30"><Number>...</Number></Dial>` or `<User>sip:...</User>`, optionally followed by another `<Record/><Stream>` so the caller returns to the bot if nobody answers. Read `DialStatus` (`completed|busy|failed|cancel|timeout|no-answer`) on the action URL.

Ordering. Both points below are risky rather than documented. No page states either, so verify them once on your own account and write down what you saw.

- Call the Transfer API first, then close the socket. The API answers 202 at once; the transfer URL appears to be fetched when the bot closes the WebSocket.
- Avoid `DELETE .../Stream/` first. With `keepCallAlive` and nothing after `<Stream>`, stopping the stream is the end of the document, and a document that runs out ends the call, so the transfer may find no live call to act on. That is an inference from the documented `keepCallAlive` behaviour, not a rule Plivo publishes, and no page names a hangup code for it.
- Alternatives without the API, in the same document: `<Dial>` after `<Stream>`, or `<Redirect>` after `<Stream>` to a URL that decides what happens next. Closing the socket runs it (docs: keepCallAlive, <https://www.plivo.com/docs/voice-agents/audio-streaming/xml/stream>).

Copy documents F1 and F2 below for the two-document form, or D for the redirect form. The docs recommend SIP over a phone number for contact centres: `<User sipAuthUsername="..." sipAuthPassword="...">sip:queue@your-cc.example.com</User>`. Failures come back as 4240 `sip_auth_failed` or 4250 `sip_auth_timeout`. The `<Dial>` attributes a handoff actually needs are all used in F2 and listed in the defaults table below: `callerId`, `timeout`, `redirect`, `action`, `method`, plus `dialMusic` and `timeLimit`. If you need the complete `<Dial>` attribute table, the simultaneous and sequential dialling shapes, or the full callback parameter list, that is general XML: install `plivo-voice-xml` (`npx skills add https://www.plivo.com/docs --skill plivo-voice-xml`) or read <https://www.plivo.com/docs/voice/xml/routing>. Allow Plivo's media IPs at the contact centre. Set `dialMusic` so the caller does not hear silence. Many human legs never answer (busy, cancelled, SIP endpoint offline 2020, no answer): handle `DialStatus` and put `<Stream>` or `<Speak>` after `<Dial>`. A busy or unanswered B-leg is a handoff outcome, not a Stream failure.

Check: one test transfer, `DialStatus=completed` on your callback, the caller and the human hear each other, and the bot's audio has stopped. A transfer URL that fails shows as 7013 or 8013 on the call.

Warm transfer with the AI inside a MultiPartyCall (`role="ai-agent"`): the API fields are documented, the runtime behaviour is not verified. Read "AI agent as a MultiPartyCall participant" before proposing it.

## Stage 8: operating it

| Watch | How |
|---|---|
| Calls that did not connect to the agent | `plivo voice calls list --limit 50 -o json`, filter `hangup_cause_name` not in `Normal Hangup`, `End Of XML Instructions` |
| Any one failure, explained | `plivo voice calls diagnose <call_uuid>` (see the loop below) |
| Stream drops | your `statusCallbackUrl` receiving `DroppedStream` or `DegradedStream`; log the raw `Event` string, the docs use two naming schemes |
| Humans hanging up in the first seconds | common on streamed calls; speak first and fast, keep any `<Speak>` before `<Stream>` short. A short call is not proof of a bot fault |
| Ceilings cutting conversations | calls ending 4010 at exactly the same second every time: your `streamTimeout` or your own timer |
| Recordings | `plivo voice recordings list --call-uuid <uuid>`; `<Record>` before `<Stream>`, `recordSession="true"` |
| Outbound campaigns | measure your own answer and connect rate by destination, list source and time window; `--machine-detection true` (async: `Machine=true` hits `machine_detection_url`, set via `plivo api`, no CLI flag) and `--ring-url`; do not hang up on every voicemail (short-call surcharges); stay inside your CPS (default 2; India: the concurrency limit) |
| Before launch, rehearse | silence, barge-in, DTMF, bot timeout, socket refused and socket dropped mid-call, malformed `playAudio`, transfer to a busy human, recording on and off, and the stage 2 rollback |

## When a call fails: debug with the CLI, in this order

Run each layer once and keep the evidence (command output, redacted body, log line). Do not repeat a billable call until the failed layer passes.

1. `plivo voice calls diagnose <call_uuid>`. Plivo's AI debugger reads the call record, the SIP and media trace and your answer-URL responses. It usually takes about a minute; allow 30 to 120 s. The answer is AI-generated text, not a stable schema. It shares a small per-account rate limit with `plivo ask`, so do not loop it. Only calls on your own account can be diagnosed.
2. `plivo voice calls get <call_uuid> -o json`. Read `hangup_cause_code`, `hangup_cause_name`, `hangup_source`, `answer_time`, `bill_duration`; for a handoff read the B-leg too. If a field is missing, `plivo api GET /Call/<call_uuid>/` has the full record. Map the code with the table below; the full list is in "Hangup causes and end-of-call patterns".
3. Reproduce the answer URL: `curl -s -i -X POST <answer-url> -d 'CallUUID=x&From=%2B...&To=%2B...&Direction=inbound&Event=StartApp'`, then apply the XML checklist to the body. The headers tell you 401, 405, 404 or 530; the checklist tells you what in the XML would break the call. A 7011 is an HTTP failure: the body alone cannot show it, so read status, method, proxy and signature logs. For 7013 or 8013 do the same against the transfer URL, where no `<Stream>` is required.
4. Reproduce the socket: `plivo voice streams test --to wss://... --bidirectional --duration 5`. No frames back means the bot never sends `playAudio`. A connect failure means not public, not TLS, or wrong path.
5. Still unclear: console Voice, Logs, Calls, the call, Audio Streams, Debug logs (stream events, `DroppedStream` error text) and Call Insights (audio quality flags: one-way, broken, robotic, lag). Then `plivo ask --call-uuid <uuid> "..."`, or Plivo support with the escalation packet in "Hangup causes and end-of-call patterns".

| Code / name | Plain meaning | Fix |
|---|---|---|
| 7011 Error Reaching Answer URL | No usable HTTP response: 404, 401/403 (your auth), 405 (wrong method), 530/502 (dead tunnel), timeout, empty body. Invisible to static XML checks | Step 3; accept POST without Basic or bearer credentials; fallback URL set; answer under 15 s |
| 8011 Invalid Answer XML | Your URL answered but not with Plivo XML: JSON (often from an application that is not the one you wrote), HTML, malformed XML, an unsupported `<Speak language>`, a Twilio element such as `<Reject/>` | Confirm the number points at your own XML application (stage 2); run the checklist on the exact body |
| 4010 End Of XML Instructions | Normal end. With keepCallAlive, the XML ran out when the socket closed | Nothing. A 4010 a few seconds after answer is a reason to read the bot's connect handler and the stream status callbacks; the call record alone does not say who closed the socket |
| 3020 Rejected / 3010 Busy Line, source Answer XML, 0 s | Your XML returned `<Hangup reason="rejected"/>` or `<Hangup reason="busy"/>` first. The routing page documents the audible effect but not the code; this pairing comes from call records, not a docs page | Intended? Fine. A bare `<Hangup/>` has not been seen to produce these codes; it ends the call gracefully |
| 8012 / 7012 Action XML | The second document (a GetDigits, GetInput, Record or Dial action URL) was bad or unreachable | Apply the checklist to it, without the `<Stream>` requirement. An action document follows the same contract: HTTP 200, `application/xml` or `text/xml`, one well-formed `<Response>` |
| 7013 / 8013 Transfer URL | The stage 7 transfer URL failed or returned bad XML | Step 3 against the transfer URL |
| 6020 Media Timeout | No media packets for 60 s (docs). This code alone does not say which side lost media | Check both media paths and the carrier; Call Insights |
| 6000 Scheduled Hangup | Max duration (default 4 h; `time_limit`) | Intentional? |
| 6010 Ring Timeout | Callee never answered (default 120 s) | Outbound: expected; tune `ring_timeout` |
| 1000 Cancelled, source API Request | Your backend hung up via the API | Nothing, if intended |
| 0 Unknown | Undetermined (docs: a known bug with Delete All Calls) | Debug logs; support if it recurs |
| 2070 Violates Media Anchoring / 5030 Concurrency Limit Breached | India: a leg or your server is outside India / over the concurrent-call limit, rejected instantly | India section, media anchoring and capacity |
| 3030 Unknown Caller ID | Caller ID is neither a number rented on this account nor an accepted verified caller ID for this route | Use a Plivo number you rent |
| 2030 Destination Country Barred | Geo permissions (Professional plan: US and India only) | Console, Voice, Geo Permissions |
| 9100 Machine Detected | Voicemail with `machine_detection=hangup` | Expected; mind short-call thresholds |
| 3000 / 3080 / 3070 / 3050 / 2000 | Carrier and destination failures on outbound campaigns | List hygiene, retries with backoff; hangup-causes section |
| 4240 / 4250 sip_auth_failed / timeout | SIP handoff credentials rejected or no response | Check `sipAuthUsername`, `sipAuthPassword`, realm, IP allow-list |
| `DroppedStream` (status callback, not a hangup code) | The socket failed to connect or died mid-call | Server or tunnel went away; `DegradedStream` first means too slow |

## What not to do (each one a common failure in practice)

- Do not let a greeting or a menu break the document. A `<Speak language>` or `<GetInput language>` Plivo does not accept costs you the whole document, and with no fallback URL the caller gets nothing. The documented lists are short, so keep a copy: `<Speak>` with the generic `WOMAN` and `MAN` voices documents `da-DK`, `nl-NL`, `en-AU`, `en-GB`, `en-US`, `fr-FR`, `fr-CA`, `de-DE`, `it-IT`, `pl-PL`, `pt-PT`, `pt-BR`, `ru-RU`, `es-ES`, `es-US`, `sv-SE`, and a `Polly.<Name>` voice covers many more, including `hi-IN` with `Polly.Aditi` and `en-IN` with `Polly.Raveena`. `<GetInput>` speech lists `en-US`, `en-GB`, `en-AU`, `es-US`, `es-ES`, `fr-FR`, `de-DE`, `it-IT`, `pt-BR`, `ja-JP`, `zh-CN` under the heading "common languages include", so that list is explicitly not exhaustive: test any other code before you rely on it (docs: <https://www.plivo.com/docs/voice/xml/audio-output>, <https://www.plivo.com/docs/voice/xml/input>, <https://www.plivo.com/docs/voice/concepts/ssml>). The safe alternative is to let the bot speak the greeting over the stream.
- Do not put tokens or passwords in the WebSocket URL, `extraHeaders` or callback URLs. They appear in Plivo logs. Validate the Plivo signature instead (<https://www.plivo.com/docs/voice/concepts/signature-validation>).
- Do not return `<Hangup/>` as a placeholder. It ends the call gracefully the moment it runs, so the caller gets a call that answers and immediately stops, and in the call record it looks like a document that finished normally. Return `<Speak>` instead while you build. Deliberate screening is `<Hangup reason="rejected"/>` or `<Hangup reason="busy"/>`, which are the forms that give the caller a rejection or busy signal.
- Do not test outbound first. A failed outbound answer URL is a billed call that drops as soon as the callee answers. Prove inbound or `streams test` first.
- Do not rely on the codec default. Set `contentType` explicitly; the default differs between docs pages.
- Do not cold-call in India, and do not hang up on every voicemail in the US. Both are penalised (UCC, short-call surcharges).
- Do not copy Twilio XML. `<Reject/>`, `<Say>`, `<Gather>`, `<Pause>` and `<Parameter>` are not Plivo elements. Answer documents carrying a top-level `<Reject/>` have been observed to fail with 8011; a top-level `<Say>` and a nested `<Parameter>` have been seen to be ignored instead, and the docs do not say which happens, so fix them all and predict a code only for `<Reject/>`. The Plivo forms are `<Hangup reason="rejected"/>`, `<Speak>`, `<GetInput>` and `<Wait>`.
- Do not read a carrier code, a B-leg outcome, an out-of-credit cancel or a human hangup as proof of a WebSocket defect. Each has its own row in the hangup-causes section.

## The XML: compose it, check it

This section is about the document that carries a `<Stream>`: the documents to copy, the defaults to set, and the checks that decide whether a streamed call connects. It is complete for that job. For a document with no stream in it at all, and for the full attribute table of every other element, install `plivo-voice-xml` (`npx skills add https://www.plivo.com/docs --skill plivo-voice-xml`) or read <https://www.plivo.com/docs/voice/xml/overview>.

One rule shapes half these documents, and it is a risk to raise, not a verdict. On `<GetDigits>`, `<GetInput>`, `<Record>`, `<Dial>` and `<Conference>` the `redirect` attribute defaults to `true`, so when that element's `action` URL answers, Plivo runs the document it returns. What the docs do not say is that the elements below are dead. The input page states that after `retries` attempts with no input, execution continues to the next element, and the routing page shows a `<Dial>` followed by a fallback that runs when the dial fails or times out. So the elements below are reachable on the nothing-happened path and skipped on the path where the caller does respond. Raise it as a risk: say the `<Stream>` below may never open for a caller who presses a key, and offer `redirect="false"` or a repeat of the `<Stream>` in the action document. Documents shaped this way run to a normal hangup every day, so never answer that the call breaks. The strongest case in this family is `<Redirect>`, and even that is not a documented failure: the routing page says it transfers call execution to a different URL and Plivo continues the call there, but no page states that siblings below it are skipped, so treat content under a `<Redirect>` as dead code to move rather than a broken call (docs: <https://www.plivo.com/docs/voice/xml/input>, <https://www.plivo.com/docs/voice/xml/routing>, <https://www.plivo.com/docs/voice/xml/record>).

### Ask these questions (skip any already answered)

1. Direction: inbound, outbound, or both?
2. Your WebSocket URL. Should start with `wss://`. `ws://localhost` needs a tunnel first (stage 5).
3. Audio format: `mulaw 8 kHz` (default, native telephony, the most common choice) or `L16 16 kHz` (when the speech model wants 16 k). If unsure, mulaw 8 kHz.
4. Record the call? If yes: where to post the recording URL, mono or stereo.
5. Say anything before the agent joins? A greeting or a recording notice. A keypad menu first?
6. Hand off to a human? Never, dial a number, dial a SIP address, redirect to a URL that decides, or put the caller in a room. Should the bot take the call back if the human does not answer?
7. Where should Plivo report stream problems? A status callback URL. Strongly recommended.

Do not ask about `keepCallAlive`, `streamTimeout`, `audioTrack`, `noiseCancellation` or `extraHeaders` unless the user raises them. Set the defaults below.

### The documents to copy, most common first

Each is one well-formed document. Replace the hosts, the numbers and the placeholders. `{{CallUUID}}`, `{{From}}` and `{{To}}` are placeholders for your server to fill in before it returns the XML: Plivo does not substitute them. Most deployments pass a short id as a query string or a path segment.

A. Stream only. The shape most deployments run.

```xml
<Response>
  <Stream bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
</Response>
```

B. Record, then stream. `<Record>` must come first: with keepCallAlive an element after the stream runs only once the stream ends.

```xml
<Response>
  <Record recordSession="true" fileFormat="mp3" maxLength="3600" callbackUrl="https://voice.example.com/plivo/recording" callbackMethod="POST"/>
  <Stream bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
</Response>
```

C. Greeting and a keypad menu, then stream. Everything before `<Stream>` delays the bot's first word by that much.

`redirect="false"` on the `<GetDigits>` is what makes this reliable. Without it `redirect` defaults to `true`, so a caller who presses a key gets whatever `/plivo/menu` returns and the `<Record>` and `<Stream>` below are skipped for that caller. A caller who presses nothing still falls through to them after `retries` attempts, which is why this shape is a design risk to raise rather than a broken document. With `redirect="false"` Plivo still posts the `Digits` to your menu URL, ignores whatever that URL returns, and carries on to the `<Record>` and the `<Stream>`. Your menu URL then becomes a notification handler: it must still answer HTTP 200 quickly, and it is where you record the caller's choice so the bot can read it (pass the same `CallUUID` through, or key on it).

```xml
<Response>
  <Speak voice="Polly.Aditi">Welcome to Acme. This call may be recorded for quality and training.</Speak>
  <GetDigits action="https://voice.example.com/plivo/menu" method="POST" redirect="false" numDigits="1" timeout="5" retries="2" validDigits="12"><Speak voice="Polly.Aditi">For sales, press 1. For support, press 2.</Speak></GetDigits>
  <Record recordSession="true" fileFormat="mp3" maxLength="3600" callbackUrl="https://voice.example.com/plivo/recording" callbackMethod="POST"/>
  <Stream bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
</Response>
```

C2. The other way to do the same thing: leave `redirect` at its default and let the menu URL return the streaming document. Use this when each key must reach a different bot or a different WebSocket URL. The answer document is then just the greeting and the menu:

```xml
<Response>
  <Speak voice="Polly.Aditi">Welcome to Acme. This call may be recorded for quality and training.</Speak>
  <GetDigits action="https://voice.example.com/plivo/menu" method="POST" numDigits="1" timeout="5" retries="2" validDigits="12"><Speak voice="Polly.Aditi">For sales, press 1. For support, press 2.</Speak></GetDigits>
  <Speak voice="Polly.Aditi">Sorry, we did not get a choice. Goodbye.</Speak>
  <Hangup/>
</Response>
```

and `/plivo/menu` returns document B (or A) with the WebSocket URL for the branch the caller chose. The `<Speak>` and `<Hangup/>` after the `<GetDigits>` are the no-input path: without them the call ends in silence after `retries` attempts.

D. Stream, then hand control back to a URL of your own when the bot closes the socket.

```xml
<Response>
  <Stream bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
  <Redirect method="POST">https://voice.example.com/plivo/after-bot</Redirect>
</Response>
```

E. Stream plus a room. Note there is no `keepCallAlive` here: the MultiPartyCall must run.

```xml
<Response>
  <Stream bidirectional="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
  <MultiPartyCall role="customer" coachMode="true" maxDuration="900" maxParticipants="10" record="false" recordParticipantTrack="true" statusCallbackEvents="mpc-state-changes,participant-state-changes" statusCallbackUrl="https://voice.example.com/plivo/mpc-status" statusCallbackMethod="POST">room-{{CallUUID}}</MultiPartyCall>
</Response>
```

F1. Answer document served before a transfer (stage 7, first of two documents).

```xml
<Response>
  <Record recordSession="true" fileFormat="mp3" maxLength="3600" callbackUrl="https://voice.example.com/plivo/recording" callbackMethod="POST"/>
  <Stream bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
</Response>
```

F2. The transfer document, served from the transfer URL. The `<Stream>` after `<Dial>` is what brings the caller back to the bot when the human does not answer.

```xml
<Response>
  <Record recordSession="true" fileFormat="mp3" maxLength="3600" callbackUrl="https://voice.example.com/plivo/recording" callbackMethod="POST"/>
  <Dial callerId="{{To}}" timeout="30" redirect="false" action="https://voice.example.com/plivo/dial-result" method="POST"><Number>+91XXXXXXXXXX</Number></Dial>
  <Stream bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
</Response>
```

H. Stream, then end the call cleanly when the bot closes the socket.

```xml
<Response>
  <Stream bidirectional="true" keepCallAlive="true" contentType="audio/x-mulaw;rate=8000" statusCallbackUrl="https://voice.example.com/plivo/stream-status" statusCallbackMethod="POST">wss://voice.example.com/ws/{{CallUUID}}</Stream>
  <Hangup/>
</Response>
```

India KYC application body, for `plivo numbers compliance create --data @app.json`. One `documents[]` entry per document type the requirements call returns.

```json
{
  "country_iso": "IN",
  "number_type": "local",
  "alias": "<company or, for resellers, the customer name>",
  "end_user": {
    "type": "business",
    "name": "<LEGAL NAME EXACTLY AS ON THE CERTIFICATE>",
    "email": "<compliance contact email>",
    "address_line1": "<registered address>",
    "city": "<city>",
    "state": "<state>",
    "postal_code": "<pin>",
    "country": "IN",
    "registration_number": "<CIN or Udyam number from the certificate>"
  },
  "documents": [
    {
      "document_type_id": "<one entry per document type returned by: plivo numbers compliance requirements --country IN --number-type local --user-type business>",
      "data_fields": { "business_name": "<LEGAL NAME EXACTLY AS ON THE CERTIFICATE>" }
    }
  ]
}
```

### The defaults, and why

| Setting | Default | Why |
|---|---|---|
| `bidirectional="true"` | always | the documented default is `false`, and then the caller hears nothing from the bot (<https://www.plivo.com/docs/voice-agents/audio-streaming/xml/stream>) |
| `keepCallAlive="true"` | always, except before a MultiPartyCall | documented: with it the stream runs exclusively and subsequent XML executes only after the stream disconnects; without it the following XML runs at once. Risky, not fatal: documents that leave it off run to a normal hangup, so recommend it and do not reject over it |
| `<Record>` before `<Stream>` | when recording | with keepCallAlive, an element after the stream runs only once the stream ends, so a `<Record>` below would start after the conversation |
| `contentType` explicit | always | the XML reference says `audio/x-l16;rate=8000` is the default, the guide says mu-law; do not depend on either |
| `statusCallbackUrl` | always ask | the only push signal for `DroppedStream` and `DegradedStream` (<https://www.plivo.com/docs/voice-agents/audio-streaming/troubleshooting/troubleshooting>) |
| `streamTimeout` | omitted | platform default 86400 s; a short ceiling such as 300 s or 600 s cuts real conversations at that second |
| `extraHeaders` | `k=v,k2=v2`, no secrets, 512 bytes max | comma-separated in the XML reference, the API and the CLI; delivered in every event and logged. The Stream page calls the value custom key-value pairs and shows `userId=12345,sessionId=abc123`. The same page also prints a character constraint of `[A-Z]`, `[a-z]`, `[0-9]`, which cannot be read literally because its own example uses `=` and `,`. Streamed calls whose keys carry `_` or `-` end with a normal hangup, so treat unusual characters as worth testing rather than as a failure |
| After `<Stream>` | nothing | most deployments end the document there; `<Redirect>`, `<Hangup/>` or `<Dial>` after it are the common continuations |
| Handoff | Transfer API, then `<Dial>` | the transfer document is fetched when the socket closes; same-document `<Dial>` or `<Redirect>` is the alternative |
| Credentials in URL or `extraHeaders` | never | validate `X-Plivo-Signature-V3` on the WebSocket upgrade instead (<https://www.plivo.com/docs/voice/concepts/signature-validation>) |

### Check the XML before you serve it

Apply this checklist to the exact body your answer URL returns, not to the file you think it returns. It is static: it cannot see HTTP failures, so a document that passes still does not prove Plivo can fetch it (7011 leaves no trace in the body) or that a call connects. Every rule names its docs page or says it is a common observation.

**Will break the call.** Every line here is backed by a documented rule or by a call with this shape that failed. Fix before dialling:

- The body is empty. There is no usable answer document, so the call cannot proceed. If your server answered 200, the XML overview lists an empty response under invalid answer XML, so expect 8011; if it did not answer, or answered non-2xx, that is 7011. Read the HTTP status before naming a code.
- The body is JSON. Plivo reports 8011, because JSON is not Plivo XML. If the number is attached to a console flow application rather than an XML application, the flow answers instead of your answer URL and you get a JSON body you did not write: attach your own XML application to the number. Any other JSON is usually your framework's error or auth response, which normally also means a non-2xx and so 7011.
- The body is HTML (an error page or a login page): 8011.
- Not well formed XML: 8011. Two documents concatenated (anything after the first `</Response>`) and a comment containing `--` both fail the same way.
- The root element is not `<Response>`.
- `<Reject/>` at the top level. It is a Twilio element, and answer documents carrying it have been observed to fail with 8011. The Plivo form is `<Hangup reason="rejected"/>`. The documented Plivo elements are: Response, Record, Stream, Speak, Play, GetDigits, GetInput, Dial, Number, User, Conference, MultiPartyCall, Redirect, Wait, Hangup, PreAnswer, DTMF, Message. There is no `AgentHoldMusic` or `CustomerHoldMusic` element: hold music is set with the `agentHoldMusicUrl` and `customerHoldMusicUrl` **attributes** on `<MultiPartyCall>` (<https://www.plivo.com/docs/voice/xml/multiparty-call>). SSML tags are allowed inside `<Speak>` only. Any other unrecognised element belongs in the risky tier below: what Plivo does with one is not documented, and a top-level `<Say>` has been seen to be ignored rather than rejected.
- An answer document for a voice agent with no `<Stream>` (and no `MultiPartyCall role="ai-agent"`). A plain `<Speak>` or `<Play>` document plays a message and ends (4000 or 4010). Action, transfer and redirect documents do not need a `<Stream>`.
- `<Response/>` is empty: the call is answered and ends at once (4010).
- Only `<Hangup/>` in an answer document: the call is answered and then ended gracefully at once, so the caller gets nothing and the call record looks like a document that finished normally. That is not a voice agent. Use `<Speak>` as a placeholder. Deliberate screening is `<Hangup reason="rejected"/>` or `<Hangup reason="busy"/>`; `rejected` and `busy` are the only two documented `reason` values, and the routing page documents the audible effect (a rejection tone, a busy signal). Call records for this shape carry 3020 or 3010 with hangup source Answer XML, which is an observation from real traffic and not a mapping any docs page publishes.
- `<Stream>` with no WebSocket URL, a URL whose scheme is neither `wss://` nor `ws://`, a URL pointing at localhost (Plivo's servers cannot reach it), or a URL longer than the documented 2048 characters. Plain `ws://` is not in this tier: see the risky list.
- `audioTrack` that is not `inbound`, `outbound` or `both`; and `audioTrack="both"` or `"outbound"` together with `bidirectional="true"`, which the docs forbid.
- `contentType` outside `audio/x-mulaw;rate=8000`, `audio/x-l16;rate=8000`, `audio/x-l16;rate=16000`. Those three are the documented audio formats. `audio/x-mulaw;rate=16000` is not one of them: mu-law is documented at 8 kHz only.
- `statusCallbackMethod`, `method`, `callbackMethod` or `recordingCallbackMethod` that is not GET or POST.
- `extraHeaders` longer than 512 bytes.
- An action, callback, status-callback, hold-music or `<Redirect>` URL that is not an absolute `http(s)` URL, still holds a placeholder, or points at localhost. `<Redirect>` with no URL at all.
- An empty `<Number>` or `<User>` inside `<Dial>`: there is nothing to dial, and the routing page requires `<Dial>` to contain at least one nested element.
- `<GetInput>` with no `action` URL: that page documents `action` as required, so there is nowhere for the input to go. On `<GetDigits>` the input page gives `action` a default of `-` and does not mark it required, so a `<GetDigits>` without one is legal: execution simply continues to the next element after `retries` attempts. Flag a missing `<GetDigits action>` only when the document clearly expects the digits to be posted somewhere. `retries` is an integer with a documented default of 1 and no published minimum, so do not reject `retries="0"` as invalid; note only that the docs give no behaviour for it (<https://www.plivo.com/docs/voice/xml/input>).
- `<Speak language>` outside the documented set for the voice you chose. A real call confirms this one: a `<Speak language="hi-IN" voice="WOMAN">` document logged 8011. With the generic `WOMAN` and `MAN` voices the set is `da-DK`, `nl-NL`, `en-AU`, `en-GB`, `en-US`, `fr-FR`, `fr-CA`, `de-DE`, `it-IT`, `pl-PL`, `pt-PT`, `pt-BR`, `ru-RU`, `es-ES`, `es-US`, `sv-SE`; a `Polly.<Name>` voice covers many more, including `hi-IN` with `Polly.Aditi` and `en-IN` with `Polly.Raveena` (docs: <https://www.plivo.com/docs/voice/xml/audio-output>, <https://www.plivo.com/docs/voice/concepts/ssml>).
- `<Speak voice>` other than `WOMAN`, `MAN` or `Polly.<Name>`. Those are the documented values; SSML needs a `Polly.` voice.
- `<Play>` containing text instead of an audio file URL.

**Risky.** None of these is a documented failure, and calls with each of these shapes run to a normal hangup. Raise them, say what could go wrong and what to check, and do not name a hangup code for any of them:

- A one-way `<Stream>` (no `bidirectional="true"`). The bot hears the caller and the caller hears nothing from the bot. Valid for transcription or monitoring, wrong for a talking agent.
- Text sitting directly inside `<Response>` outside any element. Only elements belong there; put text in `<Speak>`. The docs do not say what Plivo does with stray text.
- An unrecognised element at the top level other than `<Reject/>`. What Plivo does with one is not documented, and a top-level `<Say>` has been seen to be ignored rather than rejected. Remove it.
- `bidirectional` set to anything that is not `true` or `false`. The docs give it as a boolean defaulting to `false` and say nothing about another value, so it is untested rather than known to fail.
- `noiseCancellation` that is not `true` or `false`, or a `noiseCancellationLevel` that is not an integer. Outside the documented values, with no documented behaviour.
- An empty `sendDigits=""` on a `<Number>` or `<User>`: a template that rendered nothing. The docs give no behaviour for an empty one.
- `<GetInput>` with speech input and a `language` outside `en-US`, `en-GB`, `en-AU`, `es-US`, `es-ES`, `fr-FR`, `de-DE`, `it-IT`, `pt-BR`, `ja-JP`, `zh-CN`. The docs print that list under "common languages include", so it is explicitly not exhaustive and absence from it is not a documented failure. Test the code on a real call before shipping it.
- `ws://` instead of `wss://`. Production accounts stream over `ws://` and those calls end with a normal hangup, so this is never a reason to answer that a document breaks. Prefer `wss://` for the security reason: on `ws://` the caller's audio and anything in the URL or `extraHeaders` cross the internet in clear text, and no certificate proves the server is yours. Every example in the docs uses `wss://`.
- A temporary tunnel host in the stream URL or any callback URL: fine for testing, never for a live number, because a stopped tunnel means `DroppedStream` or 7011.
- Credentials in a URL (`user:password@`) or a token-like query value, in the stream URL or in `extraHeaders`. They appear in Plivo logs. Validate `X-Plivo-Signature-V3` instead.
- `http://` instead of `https://` on a callback URL.
- No `statusCallbackUrl`, or an empty one: you will not be told about `DroppedStream` or `DegradedStream`.
- No `contentType`: the XML reference documents `audio/x-l16;rate=8000` as the default and the guide says mu-law. Set it.
- `audio/x-l16;rate=24000`, which appears only in the Voice API reference and not in the Stream XML page's list of audio formats, and `audio/x-wav`, which is not documented anywhere. Prefer one of the three formats the Stream XML page lists.
- No `keepCallAlive="true"` on a document that has no MultiPartyCall. Documented behaviour: with it the stream runs exclusively and subsequent XML executes only after the stream disconnects, so without it the following XML runs at once. Documents that leave it off run to a normal hangup, so recommend adding it and say what the following element would do early; do not report the call as broken over it.
- A `<GetDigits>`, `<GetInput>`, `<Record>`, `<Dial>` or `<Conference>` with an `action` URL and `redirect` left at its default `true`, with more than a terminal fallback written below it. A caller who responds gets the action document instead of the rest of yours; a caller who does not respond still falls through to it, which the input and routing pages document. Raise the risk, offer `redirect="false"` or a repeat of the content in the action document, and do not call it a broken call. A terminal fallback below the element (`<Speak>`, `<Play>`, `<Wait>`, `<Hangup>`) is the documented correct shape and is not worth mentioning.
- More than one `<Stream>`: only one stream runs per call at a time.
- Anything written after `<Redirect>`. The routing page says `<Redirect>` transfers call execution to a different URL and Plivo continues the call there, but it does not separately say that siblings below are skipped. Treat that content as dead code and move it into the document the redirect URL returns.
- `streamTimeout` under 120 s (it cuts real conversations) or a value that is not a positive integer.
- `noiseCancellationLevel` outside the documented range 60 to 100, or set without `noiseCancellation="true"`. What Plivo does with a value below 60 is not documented; stay in the range.
- `extraHeaders` using `;` as separator, or an item without `key=value`.
- `<Record>` after `<Stream>`: with keepCallAlive it runs only after the stream ends, so it would start recording after the conversation rather than during it. `<Record>` without `recordSession="true"` waits for the caller to speak and then stops.
- A bare `&` in text or an attribute. It is invalid XML, so write `&amp;`. Do not rely on any parser accepting it.
- An empty attribute value (`bidirectional=""`, `statusCallbackUrl=""`, `noiseCancellation=""`). Omit the attribute instead of emitting it empty; what Plivo does with an empty value is not documented.
- A voice the docs do not list for that language (no MAN voice for da-DK, fr-CA, ru-RU, sv-SE; no WOMAN voice for pt-PT), or an empty `<Speak>`.
- `<Dial>` with no `<Number>` or `<User>` child: nothing is dialled.
- Any element nested inside a parent that does not document it as a child. The documented parents are `Response` (any element), `GetDigits` and `GetInput` (`Speak`, `Play`), `Dial` (`Number`, `User`) and `PreAnswer` (`Speak`, `Play`, `Wait`). Whether Plivo ignores or rejects other nesting is not documented; move the element out.

**Style, or worth knowing.** No functional effect. Say so and move on:

- `<Speak>`, `<Play>`, `<GetDigits>`, `<GetInput>` or `<Wait>` before `<Stream>` delays the socket, and so the bot's first word, by that much.
- `<Dial>` creates a second, billed leg. Read `DialStatus` on its action URL.
- Recording is on: disclosure, consent, retention and deletion rules are yours to check.
- `MultiPartyCall role="ai-agent"` over XML: the attributes are documented, but whether this form opens the socket the way the REST form does is not verified either way.
- Everything in this checklist that is not about `<Stream>` is a short form of the general XML rules, chosen for what breaks a streamed call. For the full element by element attribute tables and the complete list of shapes that produce 8011 and 8012, install `plivo-voice-xml` (`npx skills add https://www.plivo.com/docs --skill plivo-voice-xml`) or read <https://www.plivo.com/docs/voice/xml/overview>.

## India: what a voice agent needs before its first call

Facts from these docs pages: <https://www.plivo.com/docs/voice/concepts/india-calling>, <https://www.plivo.com/docs/numbers/rent-india-numbers>, <https://www.plivo.com/docs/numbers/compliance>, <https://www.plivo.com/docs/voice/concepts/140-series-provisioning>, <https://www.plivo.com/docs/voice/concepts/160-series-provisioning>, <https://www.plivo.com/docs/voice/concepts/ucc-management>, <https://www.plivo.com/docs/voice/concepts/india-concurrency>, <https://www.plivo.com/docs/voice/concepts/account-limits>, <https://www.plivo.com/docs/voice/concepts/geo-permissions>, <https://www.plivo.com/docs/voice/concepts/carrier-failover>, <https://www.plivo.com/docs/voice/concepts/verified-caller-id>, <https://www.plivo.com/docs/voice/api/calls> (India Compliance Errors). Where the CLI has no command, the console or API path is given.

### 1. Account

- Indian numbers are only available to India data-region organisations. The data region cannot be changed after creation. If you are in the US region, create a new organisation with the India data region from the organisation switcher in the console. No new signup is needed.
- Only India-registered businesses can rent Indian numbers and use domestic routes. A business outside India must use international routes: international rates, and the caller ID shown is a US or international number.
- If you need both India and international calling, the docs say to run two separate Plivo accounts.
- Geo permissions: INR accounts can only call within India. Professional (pay-as-you-go) accounts can call only US and India.
- Verified Caller ID is not supported for India. The caller ID must be a Plivo-rented Indian number.
- CLI: `plivo auth whoami -o json` shows which account you are on. There is no CLI command for data region or organisation switching.
- Which Indian number series support `<Stream>` and whether streaming to a server in India is permitted is not stated in the docs. Media anchoring (section 5) requires your server to be in India.

### 2. KYC (compliance application): an agent can run this end to end from the CLI

One rule: run `plivo numbers compliance requirements` and supply exactly the document types it returns. The console guide and the API reference have disagreed on how many documents are needed (one vs two). The live requirements response is the source of truth. Do not hard-code a count.

The agent cannot produce the inputs. The certificate files and the exact legal details come from the user. Never fill them in yourself. Never submit without the user saying "go".

Ask the user for:

| Input | Why | Rule |
|---|---|---|
| Certificate files (PDF, JPEG or PNG, 5 MB or less each, filename 99 characters or less) | uploaded as `documents[i].file` | one file per document type returned by `requirements`. Two further rules are commonly repeated but appear on no docs page: that the same file cannot fill two slots, and that a PAN on its own is not enough. Supply a distinct file per type and whatever `requirements` returns, and do not tell a user an application will be rejected on either ground. |
| Legal business name, exactly as printed on the certificates | `end_user.name` and `documents[].data_fields.business_name` | must match across documents and fields, character for character |
| Registration number (CIN or Udyam number), GSTIN | `end_user.registration_number`; the GST certificate carries the GSTIN | copy from the documents; do not guess |
| Contact email, registered address (line, city, state, postal code) | `end_user.*` | |
| Direct brand or reseller? | reseller: one application per customer, named in `alias`; the customer's application id is passed at rent time | |
| Is this the first application on the account? | the documents must be sealed and signed by an authorised signatory (Plivo takes billing address and GST details from it) | tell the user before they upload |

Commands, in order. Every step that writes appears twice: once with `--dry-run`, which prints the request and sends nothing, and once with `--yes`. Show the user the previewed request and wait for them to say go before you run the second form. A compliance application is a regulatory filing; renting a number spends money.

```bash
# 1. What is required right now (read-only; source of truth; do not hard-code document ids or counts)
plivo numbers compliance requirements --country IN --number-type local --user-type business -o json

# 2. Fill the payload from the user's answers (one documents[] entry per returned type), then create and submit
plivo numbers compliance create --data @app.json \
  --file documents[0].file=@first_document.pdf \
  --file documents[1].file=@second_document.pdf --dry-run          # preview the filing; nothing is sent
plivo numbers compliance create --data @app.json \
  --file documents[0].file=@first_document.pdf \
  --file documents[1].file=@second_document.pdf --yes -o json      # only after the user says go; returns compliance_id, status "submitted"

# 3. Poll until it leaves "submitted" (read-only; 080 and 022 numbers: automated review, typically about 5 minutes)
plivo numbers compliance get <compliance_id> --expand documents -o json     # status: accepted | rejected

# 4a. rejected: read rejection_reason, fix the document or a field, resubmit. update REPLACES all documents: re-attach every file
plivo numbers compliance update <compliance_id> --data @app.json \
  --file documents[0].file=@first_document.pdf --file documents[1].file=@second_document.pdf --dry-run   # preview
plivo numbers compliance update <compliance_id> --data @app.json \
  --file documents[0].file=@first_document.pdf --file documents[1].file=@second_document.pdf --yes       # after approval

# 4b. accepted: rent. Direct brands: Plivo attaches the accepted application automatically.
plivo numbers search --country IN --type local --limit 10                   # read-only
plivo numbers buy <number> --dry-run                                        # preview; this one spends money
plivo numbers buy <number> --yes                                            # after approval
#     Reseller, or "compliance_application_id is required": the CLI buy has no flag for it, so use the API:
plivo api POST /PhoneNumber/<number>/ --body '{"compliance_application_id":"<compliance_id>"}' --dry-run
plivo api POST /PhoneNumber/<number>/ --body '{"compliance_application_id":"<compliance_id>"}' --yes

# 5. Numbers you already had: link them. This changes how an existing number is treated, so preview it too.
plivo numbers compliance link --link +9180XXXXXXXX=<compliance_id> --dry-run
plivo numbers compliance link --link +9180XXXXXXXX=<compliance_id> --yes
plivo numbers get <number> -o json                                          # confirm the link
```

If a command rejects `--dry-run` or `--yes`, read its `--help` rather than dropping the preview: run the read-only `get` or `list` for the same resource first, show the user the current state, and get their agreement before you write.

Optional: put `"callback_url": "https://..."` (HTTPS only) in the payload. Plivo POSTs a V3-signed callback when the status changes, instead of polling.

Status: `draft`, then `submitted`, then `accepted` (rent and link) or `rejected` (update, which auto-resubmits). Also `suspended` (unresolved UCC complaints; see section 6) and `expired` (create a new one).

Errors you will see, verbatim: `compliance_application_id is required` (no accepted application to attach at rent). `Compliance application must be in 'accepted' status. Current status: 'submitted'.` (too early; keep polling). `Compliance application must be in 'rejected' status` (update only works on rejected). `Number not found on your account. Only rented numbers can be linked.`

Common rejection reasons (docs): details do not match government records (download a fresh copy from the GST, MCA or Udyam portal), expired document, unaccepted document type, unreadable upload, same file in both slots.

Not covered by this API: 140-series (promotional) and 160-series (BFSI) numbers. Those are a separate provisioning process (Tata DLT registration, declaration forms, NOC, voice header and template approval, 5 to 14 business days) that runs through your Account Manager or a support ticket. See section 4.

### 3. Renting the number

```bash
plivo numbers search --country IN --type local --limit 10
plivo numbers buy <number> --dry-run [--app-id <app_id>]     # preview first: this spends money
plivo numbers buy <number> --yes [--app-id <app_id>]         # only after the user approves
```

- Direct brands: the accepted application links automatically at purchase. Resellers must choose the customer's approved application.
- The CLI `buy` has no `compliance_application_id` flag. If purchase fails with `compliance_application_id is required`, pass it through the Buy a Phone Number API, previewing first: `plivo api POST /PhoneNumber/<number>/ --body '{"compliance_application_id":"<id>"}' --dry-run`, then the same command with `--yes`.

### 4. Number series: pick the right one or every complaint counts as UCC

| Series | Permitted use | Who | Time to provision |
|---|---|---|---|
| Landline (022, 080, ...) | Service and transactional calls only. Promotional content strictly prohibited | Non-BFSI businesses | about 5 min automated KYC |
| 140-series | Promotional voice calls only. Not for transactional or service calls | Any business making promotional calls | about 5 to 10 business days |
| 160-series | Service and transactional calls, BFSI only (RBI, SEBI, IRDAI, PFRDA-regulated). Promotional use leads to disconnection and penalties | BFSI entities | about 7 to 14 business days |

Using the wrong series is itself a violation. Complaints from such calls are treated as UCC regardless of consent.

140 and 160 provisioning is offline. There is no CLI or console flow:

1. Register on the Tata Teleservices DLT portal as Principal Entity (PE) and Telemarketer (TM). Same legal entity for both means Self-Managed; a vendor placing calls for you means Partner-Managed. Only Tata DLT is supported (not Airtel, Vi or BSNL). The TM must be registered in Mumbai or Karnataka.
2. Email your Account Manager or raise a Plivo support ticket with the signed, sealed Aggregator Telemarketer Declaration (140 and 160) plus the BFSI Customer Application Form (160 only).
3. Plivo allocates the number and issues a NOC per number.
4. TM (140) or PE (160) registers the Voice Header on Tata DLT with the NOC. PE uploads the GST certificate (140) or regulator certificate (160). Tata approves in 1 to 2 (140) or 3 to 5 (160) business days.
5. PE registers Voice Templates (the transcript of what the agent says, with placeholders). Approval about 1 business day (140) or 1 to 2 (160). Then the number can go live.

### 5. Media anchoring

Both legs of every call must originate and terminate in India. Inbound: India to India. Outbound: Indian number to Indian destination. Conferences: all participants in India. Violations fail with hangup cause 2070 `Violates Media Anchoring`. The hangup-causes page adds: for India calls the server must be in India; do not mix PSTN and WebRTC in conferences. A US-hosted platform cannot terminate India calls (403 `domestic_anchored_terms_not_met`).

### 6. Consent and UCC (outbound agents)

- Cold calling is prohibited. You need explicit digital consent before any commercial call (TRAI TCCCPR 2025). Calls without it are Unsolicited Commercial Communication (UCC). Applies to landline and 160-series numbers.
- Complaints appear on the console UCC dashboard (Phone Numbers, UCC), in a daily email, and via the UCC API: `plivo api GET /Ucc/` (list; filter `?status=rejected`), `plivo api GET /Ucc/<reference_id>/`, and `plivo api POST /Ucc/<reference_id>/ --dry-run` then `--yes` to submit proof once the user has approved what is being filed. There is no typed CLI command.
- Within 5 business days of a complaint, upload opt-in proof containing all three: business logo, complainant's phone number, opt-in date within the last 6 months. Rejected proof must be re-uploaded inside the same 5-day window.
- Remove the complainant from your list immediately. Calling them again is itself a violation.
- Escalation (tied to your compliance ID): no proof in 5 days blocks the compliance ID for 15 days (proof lifts it). 5 or more unique complaints in any rolling 10 days means immediate suspension, first violation if proofs fail. A second such instance means TRAI blacklisting for 1 year across all Indian operators. A number with no compliance ID mapped puts the whole billing entity at risk.
- You may file a representation with Plivo (decided within 7 business days) or appeal to TRAI.

### 7. Capacity

- India accounts have a concurrency limit, default 50 concurrent calls (inbound plus outbound, Voice API plus SIP trunking). CPS equals concurrency divided by 25 (default 2). Calls over the limit are rejected instantly with 5030 `Concurrency Limit Breached` (in the API response and the hangup callback). No queueing. Hard enforcement since 20 Apr 2026.
- Check usage: console Voice, Call Logs, Export, Export Concurrency Data. Increase by raising a support ticket (minimum step 25 slots, which is +1 CPS). If the 30-day peak is over 80% of the limit, raise it first.
- Abandoned and short-call surcharges exclude calls to India.
- Carrier failover in India needs High Availability (HA) numbers (a second number from another carrier, billed as an extra rental). Hangup callbacks carry `CarrierFailoverTriggered=true` when it fired. Outbound only.

### 8. Exact API error strings (400 Bad Request) and what they mean

| Error text | Meaning | Fix |
|---|---|---|
| `from parameter is invalid. +91... is not associated with your account. Use a phone number rented on your account.` | Caller ID is not a number rented on this account | Rent an India number and call from it |
| `from number +91... cannot place calls as its compliance application is not in 'accepted' status. Refer to https://www.plivo.com/docs/numbers/rent-india-numbers for next steps.` | Application pending, submitted, rejected, expired, or none attached | `plivo numbers compliance list --status accepted`; attach with `compliance link`; or submit or fix the application |
| `from number +91... cannot place calls as its compliance application is 'suspended'. Refer to https://www.plivo.com/docs/voice/concepts/ucc-management for next steps.` | Unresolved UCC complaints | Upload opt-in proof for every open complaint on the UCC dashboard |
| `compliance_application_id is required to rent this number. Refer to https://www.plivo.com/docs/numbers/rent-india-numbers for details.` (on rent) | No accepted application to auto-attach | Submit and wait for approval, or pass an accepted application's UUID in the Buy request |
| `Compliance application must be in 'accepted' status. Current status: 'submitted'.` (on rent) | The application you passed is not approved yet | Follow the status table above, then retry |

### 9. Evidence checklist before the first call

Write each item down with its source (a CLI command's output or a console page). Any item you cannot show is "not known", and the number is not ready.

1. Organisation id and displayed data region (console; no CLI).
2. Legal entity that owns the business eligibility (India-registered, or reseller acting for one).
3. Caller ID number and proof it is rented on this account (`plivo numbers get <number> -o json`).
4. Attached compliance application id and its `accepted` status (`plivo numbers compliance get <id> -o json`). `submitted` is not `accepted`.
5. Call purpose (service, transactional, promotional, BFSI) and the matching number series.
6. Consent record reference and a check against your suppression list (complainants, opt-outs).
7. A drawing of both legs and your media server with each region named (section 5).
8. Approved 140 or 160 header and template references where they apply.
9. A rollback owner and the UCC escalation contact.

## Outbound voice agents: answering-machine detection, US call quality, caller ID

Facts from these docs pages: <https://www.plivo.com/docs/voice/concepts/machine-detection>, <https://www.plivo.com/docs/voice/api/calls>, <https://www.plivo.com/docs/voice-agents/audio-streaming/concepts/best-practices>, <https://www.plivo.com/docs/voice-agents/audio-streaming/deploy/us-call-quality-and-cps>, <https://www.plivo.com/docs/voice/concepts/account-limits>, <https://www.plivo.com/docs/voice/concepts/stir-shaken>, <https://www.plivo.com/docs/voice/concepts/verified-caller-id>, <https://www.plivo.com/docs/voice/concepts/caller-reputation>, <https://www.plivo.com/docs/voice/concepts/geo-permissions>.

### Answering-machine detection (AMD)

| Parameter | Values / default | Effect |
|---|---|---|
| `machine_detection` | `true` or `hangup` | `true`: Plivo notifies `machine_detection_url` and the call continues. `hangup`: Plivo hangs up on detection (hangup cause 9100 Machine Detected) |
| `machine_detection_time` | ms, default `5000` | How long to analyse |
| `machine_detection_url` / `machine_detection_method` | URL, `POST` default | Receives `Machine=true`, `Event=MachineDetection`, `CallUUID`, `From`, `To`, `CallStatus` |
| `machine_detection_maximum_speech_length` | 1000 to 6000 ms | advanced tuning |
| `machine_detection_initial_silence` | 2000 to 10000 ms | advanced tuning |

- Detection is asynchronous. It never blocks the call flow. Your answer URL has already returned `<Stream>` by the time `Machine=true` arrives.
- On `Machine=true` you can hang up (`plivo voice calls hangup <uuid> --yes`), transfer the call to a voicemail-message URL (`plivo voice calls transfer <uuid> --legs aleg --aleg-url https://.../voicemail`), or tell your bot over its own channel to switch to voicemail mode.
- The docs warn: do not hang up on every voicemail. At scale that produces short-duration calls that count against the quality thresholds below. Leave a brief message or schedule a retry.
- CLI: `plivo voice calls make --from ... --to ... --answer-url ... --answer-method POST --machine-detection true|hangup --ring-url ... --hangup-url ... --dry-run`, then the same command with `--yes` once the user approves; every call is billed. Note `--answer-method` defaults to `GET`, so set `POST` explicitly if your endpoint only accepts POST. The CLI has no `--machine-detection-url` or `-time` flags. To set them use `plivo api POST /Call/ --body '{"from":...,"to":...,"answer_url":...,"answer_method":"POST","machine_detection":"true","machine_detection_url":"https://...","machine_detection_time":5000}' --dry-run`, then `--yes`.
- WhatsApp voice calls (`call_type=whatsapp_voice`) do not support machine detection.

### Expectations for an outbound campaign

Measure your own answer rate and connect rate by destination, list source and time window. This skill does not carry a benchmark number. Codes you will see daily are in the carrier and destination table below: 6010 ring timeout, 3000 no answer, 3080 carrier error, 2000 invalid destination, 3050 unallocated, 9100 machine detected.

### US: staying deliverable (applies to US destinations only)

US carriers judge traffic on answer rate and call duration. Plivo enforces monthly thresholds on non-India destinations:

| Call type | Definition | Threshold | Surcharge on excess |
|---|---|---|---|
| Abandoned | 0 s duration (never answered or dropped before connect) | under 20% of volume | USD 0.005 per call |
| Short-duration | answered, 6 s or less | account-limits page: 10%; US deploy page: 20%. Plan for 10% | USD 0.015 per call |

Substantially higher abandonment can trigger an account review. Keep metrics healthy: call opted-in recipients only; pace inside your CPS; do not hang up on every voicemail; make the agent identify itself and the reason in the first sentence (people who hang up in the first seconds create short calls); use a Plivo number you own as caller ID; register Caller Reputation; spread retries and prune dead numbers.

CPS (calls per second): every account starts at 2 CPS outbound (inbound 10). Calls above the limit are queued, not rejected (audio-streaming path), and dial later. That is bad for appointment-window calls, so pace requests yourself. 2 CPS is about 7,200 attempts per hour. Size it as peak concurrent calls divided by average call seconds (100 concurrent 3-minute conversations need under 1 CPS). Higher CPS needs an Enterprise plan, requested from Plivo support in the console. India instead has a hard concurrency limit (see the India section). API rate limit: 300 requests per 5 s (429 when exceeded). Default max call duration 4 h (`time_limit` up to 86400 s).

### Caller ID in the US

- STIR/SHAKEN is automatic. Plivo signs an outbound US call as Verified (attestation A) only when the caller ID is a Plivo number rented by the same account. Anything else is B or C ("Not Verified"). Status appears as `STIRVerification` in answer, fallback and hangup callbacks and in call records (`X-Plivo-Stir-Verification` SIP header). Plivo may stop signing if calls breach fair use, look like robocalls, get traceback requests, or use invalid caller IDs.
- Verified Caller ID (only if you must show a number you own outside Plivo): OTP by SMS or call via console (Voice, Verified Caller ID) or `POST /VerifiedCallerId/` then `POST /VerifiedCallerId/Verification/<uuid>/` with the OTP. No CLI command; use `plivo api`. Primarily US; not applicable in India. Verification lets you use the number as caller ID; it does not give attestation A, guarantee how the number displays, or prevent spam labels.
- Caller Reputation (early-access beta, US local and toll-free): register the business on your 10DLC Business Profile with `enable_caller_reputation=true`, wait up to 2 business days, then set `caller_reputation=enabled` on each number. AT&T USD 12 per business per month, T-Mobile free, Verizon not supported yet. Does not guarantee no spam flag. No CLI command.
- 10DLC registration is for SMS. It is not required for voice calls.
- Geo permissions: Professional (pay-as-you-go) accounts can call only the US and India; other countries need an Enterprise plan. Barred destinations fail with 2030, 2040 or 2050.
- US-region free-trial organisations may see "Voice capability is currently disabled for this account" on `calls make`. Request outbound access from the console. This gating is not in the docs.

### Testing outbound safely

A failed outbound answer URL is a billed call that drops when the callee answers (7011 or 8011). Prove the WebSocket with `plivo voice streams test` and the XML with the checklist first. Then place one call to your own phone: `plivo voice calls make --from <plivo number> --to <your phone> --answer-url https://HOST/plivo/answer --answer-method POST --dry-run` to preview, then `--yes`.

## Callbacks, signature validation, timeouts

Facts from these docs pages: <https://www.plivo.com/docs/voice/concepts/callbacks>, <https://www.plivo.com/docs/voice/concepts/callback-configurations>, <https://www.plivo.com/docs/voice/concepts/signature-validation>, <https://www.plivo.com/docs/voice/xml/overview>, <https://www.plivo.com/docs/voice/api/calls>, <https://www.plivo.com/docs/voice/xml/routing>, <https://www.plivo.com/docs/voice-agents/audio-streaming/concepts/audio-streaming-guide>, <https://www.plivo.com/docs/voice/concepts/firewall-network-configuration>, <https://www.plivo.com/docs/voice/concepts/sip-authentication>, <https://www.plivo.com/docs/voice/use-cases/connect-external-numbers>, <https://www.plivo.com/docs/voice/concepts/voice-alerts>.

### Which URL fires when

This table lists the URLs an agent deployment uses. The general contract, in three lines: an `action` URL expects one Plivo XML document back and Plivo runs it; a `callbackUrl` expects nothing back, so answer HTTP 200 with an empty body or `<Response></Response>`; and `redirect` decides which one owns the rest of the call, defaulting to `true` on `GetDigits`, `GetInput`, `Record`, `Dial` and `Conference` so the action document replaces yours (<https://www.plivo.com/docs/voice/concepts/callbacks>).

| URL | Set where | When | Must return |
|---|---|---|---|
| `answer_url` (Primary Answer URL) | Call API (mandatory for outbound) or application (mandatory for the number) | Call answered (outbound) or arrives (inbound). `Event=StartApp`, `CallStatus=in-progress` | Plivo XML |
| `fallback_url` (Fallback Answer URL) | Call API or application | Answer URL unreachable | Plivo XML |
| `ring_url` | Call API only | Destination starts ringing. `Event=Ring` | 200 |
| `hangup_url` | Call API or application | Call ends. `Event=Hangup`, `CallStatus=completed`, plus `HangupCause`, `Duration`, `BillDuration`, `TotalCost`, `StartTime`, `AnswerTime`, `EndTime`, `CarrierFailoverTriggered=true` only when it happened | 200 |
| `machine_detection_url` | Call API | Answering machine detected. `Machine=true`, `Event=MachineDetection` | 200 |
| `action` (Dial, GetDigits, GetInput, Record, Conference) | XML attribute | Element finished | Plivo XML to continue the call |
| `callbackUrl` (Dial, Record, Conference), `statusCallbackUrl` (Stream, MPC) | XML attribute | Events during the element | 200; no XML expected. JSON is fine here |
| `aleg_url` / `bleg_url` | Transfer API | Transfer requested. Fired when the current element yields; with `<Stream keepCallAlive="true">` that appears to be when the bot closes the socket, which the docs do not state, so verify it once on your own account | Plivo XML |

Common request parameters on answer, fallback and hangup: `CallUUID`, `From`, `To`, `Direction` (`inbound` or `outbound`), `CallStatus` (`ringing`, `in-progress`, `completed`; outbound also `busy`, `failed`, `timeout`, `no-answer`), `Event`, `RequestUUID` (outbound), `ALegUUID`, `ALegRequestUUID`, `ForwardedFrom` (only when the carrier sends it), `CallerName` (SIP), `STIRVerification` (US), `SessionStart`. Custom SIP headers arrive as `X-PH-<Name>`. Their names and values are restricted to `[A-Z]`, `[a-z]` and `[0-9]` so they survive URL encoding (docs: <https://www.plivo.com/docs/voice/use-cases/pass-custom-headers>). The `<Stream>` page prints the same character set as a constraint on `extraHeaders`, but it cannot be read literally there: that page's own example uses `=` and `,`. See the `extraHeaders` row in the attribute table. SIP-authenticated inbound legs add `SIPAuthType`, `SIPAuthUser`, `SIPSourceIP`.

The parameter lists an agent deployment actually reads: Dial `action` sends `DialStatus` (`completed`, `busy`, `failed`, `cancel`, `timeout`, `no-answer`), `DialRingStatus`, `DialHangupCause`, `DialALegUUID` and `DialBLegUUID`; `GetDigits` `action` sends `Digits`; `Record` `action` and `callbackUrl` send `RecordUrl`, `RecordingID` and the durations. For the complete per-element parameter tables, install `plivo-voice-xml` or read <https://www.plivo.com/docs/voice/xml/routing>.

### Response rules for the answer URL

- Return HTTP 200 with `Content-Type: application/xml` or `text/xml` and one well-formed `<Response>` document.
- Anything else gives 7011 (non-2xx, unreachable, empty) or 8011 (not Plivo XML).
- Accept the method configured on the application (`answer_method`; applications default to POST, the CLI's `calls make --answer-method` defaults to GET).
- Do not require Basic or bearer credentials on the URL. Plivo cannot log in. Require a valid `X-Plivo-Signature-V3` instead.
- Answer fast. The XML overview says Plivo waits 15 seconds for XML. The configurable read timeout below defaults to 40 s. Plan for well under 15 s; static XML needs no database call.
- Plivo may deliver a callback more than once (retries). Make handlers idempotent. Keys: `CallUUID` (hangup, ring), `CallUUID` plus element (action), `RecordingID` (recording), `StreamID` plus `Event` (stream status).
- Keep the answer URL short. The console rejects very long URLs; the limit is not published. Carry context through `CallUUID`, custom SIP headers (`X-PH-*`) or a short opaque id, not through long query strings.

### Timeouts, retries and edge region (URL fragments)

Append a fragment to tune one callback URL: `https://host/answer#ct=2000&rt=5000&rc=2&rp=ct,rt&er=mumbai` (<https://www.plivo.com/docs/voice/concepts/callback-configurations>).

| Key | Meaning | Allowed | Default |
|---|---|---|---|
| `ct` | connection timeout, milliseconds | 100 to 10000 | 2000 |
| `rt` | read timeout, milliseconds | 100 to 40000 | 40000 |
| `tt` | total timeout across retries, milliseconds | 100 to 55000 | 55000 |
| `rc` | retry count | 0 to 5 | 1 |
| `rp` | retry policy | `4xx`, `5xx`, `ct`, `rt`, `all`, comma separated | `ct,rt` |
| `er` | edge region | `nearest`, `local`, `n_california`, `n_virginia`, `frankfurt`, `singapore`, `mumbai` | `nearest` |

This budget is separate from the XML response deadline: the XML overview gives Plivo 15 seconds for an XML response, so design the handler for that even though the read timeout defaults higher. The fragments apply to the console application's Primary Answer, Fallback Answer and Hangup URLs, to the Call API's `answer_url`, `ring_url`, `hangup_url`, `fallback_url` and `machine_detection_url`, to the Transfer API's `aleg_url` and `bleg_url`, to recording and transcription URLs, and to XML `action` and `callback` URLs. They do not apply to audio URLs used by `<Play>` or `<PreAnswer>`, which use fixed values. Three more things that matter for an agent:

- The applicable list on the callback configuration page is exhaustive and a `<Stream>` `statusCallbackUrl` is not on it, so do not expect the fragments to change how that callback is retried.
- The fragment is not part of the signed URL, so adding one does not break signature validation.
- Plivo's Voice Alerts email you when callback failures exceed 5% or calls queue for more than 2 minutes (no setup). For a bot whose answer URL flaps under load, that alert is the earliest signal.

### Signature validation (V3)

Every HTTP request from Plivo to your server, and the WebSocket upgrade request to your `<Stream>` URL, carries three headers: `X-Plivo-Signature-V3`, `X-Plivo-Signature-Ma-V3`, `X-Plivo-Signature-V3-Nonce`.

Use your SDK's helper. Every Plivo server SDK ships one, and the manual form is easy to get subtly wrong. Only implement it by hand if your language has no SDK, and then follow the worked example on the signature page exactly rather than this prose:

1. Take the final request URL: scheme, host, port, path and query string.
2. POST only: append a `.`, then every POST parameter as `name` then `value`, sorted alphabetically by name with Unix-style case-sensitive sorting, and no separator between them. On a GET the parameters are already in the query string and this step adds nothing.
3. Append a `.`, then the nonce from `X-Plivo-Signature-V3-Nonce`.
4. HMAC-SHA256 with the Auth Token as the key. Base64-encode.
5. Compare in constant time with `X-Plivo-Signature-V3`.

The separators matter and are visible in the documented worked example. For URL `https://example.com/abcd?foo=bar` with POST parameters `CallUUID`, `Digits`, `From` and `To` and nonce `kjsdhfsd87sd7yisud2`, the assembled string in the docs is `https://example.com/abcd?foo=bar.CallUuid4vbcpem8-0u46-x1ha-9af1-438vc92bf374Digits1234From+15551111111To+15555555555.kjsdhfsd87sd7yisud2`: a `.` between the URL and the sorted parameters, and a `.` before the nonce. If your manual implementation omits those two dots it will compute a different string and reject every genuine request. Read the current page before you ship: <https://www.plivo.com/docs/voice/concepts/signature-validation>.

Notes from the docs:

- `X-Plivo-Signature-V3` is signed with the token of the account or subaccount that owns the number. `X-Plivo-Signature-Ma-V3` is always signed with the main account's token.
- If the account has more than one active Auth Token, the header holds a comma-separated list of signatures. Accept if any matches.
- V2 signatures are deprecated.
- SDK helpers: Python `plivo.utils.validate_v3_signature(method, url, nonce, auth_token, signature[, params])`; Node `plivo.validateV3Signature(method, uri, nonce, authToken, signature[, params])`; Ruby `Plivo::Utils.valid_signatureV3?`; Java `Utils.validateSignatureV3`; Go `plivo.ValidateSignatureV3`; .NET `XPlivoSignatureV3.VerifySignature`.
- For the WebSocket upgrade the method is `GET` and the URI is the full `wss://` URL Plivo dialled. Plivo documents a Node stream package that validates the upgrade signature for you (`PlivoWebSocketServer({ validateSignature: true, authToken })`). Before recommending any stream package to a user, check that it is actually published for their language on the current docs page and on that language's package index; do not assume from the docs alone.
- If validation fails and you return no XML, the call ends with 7011 or 8011. Return 400 or 401 only when you are sure the request is not Plivo's.
- Behind a load balancer or reverse proxy, sign-check the URL Plivo dialled: external scheme, host, port, path and query. A rewritten private URL (`http://`, a different port, a stripped prefix) is the usual cause of a false rejection. Read the forwarded-host headers or configure the public URL explicitly.
- The Audio Streaming protocol page carries a manual JavaScript signing example whose base-string description differs from the signature page. Where the two disagree, follow the signature page and the SDK helper.
- Never print the token to debug a mismatch. Log the method, the URL shape with the host only, which signature header you compared, the SDK version and the CallUUID.

Tests worth running before go-live: a genuine GET and POST pass; one changed form value fails; a changed host, scheme, port or path fails; a missing signature or nonce fails; both signatures pass during a token rotation; a duplicate callback runs no side effect twice; logs show neither the token nor the caller's number in clear text where your policy forbids it.

### Network

- Plivo callbacks come from region-specific edge IPs (San Jose, Ashburn, Frankfurt, Sao Paulo, Sydney, Singapore, Mumbai; list on the firewall page). If your answer URL sits behind an IP allow-list, allow those. Otherwise leave it open and rely on signatures.
- Handing a call to a SIP contact centre: allow Plivo's outbound media IPs for the region there, or the transfer fails before the contact centre sees it. SIP signalling ports 5060, 5061, 5080; RTP 16384 to 32768 UDP.
- Bringing a number from another carrier into the agent: forward it to a Plivo number, or have the carrier send SIP INVITEs to `sip:{app_id}@app.plivo.com` protected by SIP authentication (IP ACL or digest credential on the application). Auth is resolved from the Request-URI, not the To header. 10 failed attempts in 60 s lock the source out for 60 s.

### Multi-tenant notes (docs-backed parts only)

- One application can serve every tenant. Route on `To` (the dialled number) and put the call's `CallUUID` or `To` into the WebSocket URL from your server.
- Use a subaccount per client when the client needs its own numbers, its own callback signature (`X-Plivo-Signature-V3` is signed with the subaccount token) or separate call-record exports. Subaccounts share the parent balance and geo permissions.
- India resellers submit one compliance application per end customer.
- The architecture guidance itself is not in the docs.

## The WebSocket protocol: `<Stream>` attributes, limits, events

Facts from these docs pages: <https://www.plivo.com/docs/voice-agents/audio-streaming/concepts/audio-streaming-reference>, <https://www.plivo.com/docs/voice-agents/audio-streaming/concepts/audio-streaming-guide>, <https://www.plivo.com/docs/voice-agents/audio-streaming/concepts/best-practices>, <https://www.plivo.com/docs/voice-agents/audio-streaming/xml/stream>, <https://www.plivo.com/docs/voice/xml/audio-streaming>, <https://www.plivo.com/docs/voice-agents/audio-streaming/api/audio-streams>, <https://www.plivo.com/docs/voice-agents/audio-streaming/troubleshooting/troubleshooting>, <https://www.plivo.com/docs/voice/xml/multiparty-call>, <https://www.plivo.com/docs/voice/api/multiparty-calls>. Where pages disagree, both values are given and the one this skill follows is marked. Anything marked as an observation has no docs page behind it.

### `<Stream>` attributes (XML) and Start Stream API parameters

| XML attribute | API parameter | Documented default | Notes |
|---|---|---|---|
| `bidirectional` | `bidirectional` | `false` | Required for an agent that talks back. |
| `keepCallAlive` | (none) | `false` | The stream runs exclusively; following XML runs only after the stream ends. Set it, except when a MultiPartyCall follows. |
| `contentType` | `content_type` | `audio/x-l16;rate=8000` (4 reference pages). The Getting Started guide says `audio/x-mulaw;rate=8000`. Always set it. | Values: `audio/x-mulaw;rate=8000` (native telephony, no transcoding), `audio/x-l16;rate=8000`, `audio/x-l16;rate=16000`, `audio/x-l16;rate=24000` (Voice API reference only). mu-law 8 kHz is the most common choice. |
| `audioTrack` | `audio_track` | `inbound` | `inbound`, `outbound`, `both`. With `bidirectional="true"` it cannot be `outbound` or `both` (docs). |
| `streamTimeout` | `stream_timeout` | `86400` s | Max stream duration; the stream ends with reason "Stream timeout" and, with nothing after `<Stream>`, the call ends 4010. Usually left unset; short ceilings such as 300 s or 600 s cut real conversations at that second. |
| `statusCallbackUrl` | `status_callback_url` | (none) | Where stream lifecycle events go. Often left unset, which leaves you blind to `DroppedStream`. |
| `statusCallbackMethod` | `status_callback_method` | `POST` | `GET` or `POST` |
| `extraHeaders` | `extra_headers` | (none) | `k1=v1,k2=v2` in the XML reference, the API and the CLI; the guide and protocol reference show `;`. Max 512 bytes. The Stream page describes the value only as custom key-value pairs and its own example is `userId=12345,sessionId=abc123`. It does also print a constraint of `[A-Z]`, `[a-z]`, `[0-9]`, which cannot be read literally: taken at face value it forbids the `=` and `,` that the same page's example uses. That character set is the documented rule for SIP custom headers (`X-PH-` prefixed), where it exists so the header survives URL encoding, and it does not transfer to this attribute. Streamed calls whose keys carry `_`, `-` or mixed case end with a normal hangup, so treat unusual characters as worth testing rather than as a failure and never reject a document over the character set. Delivered in every event as the string `extra_headers`. Never put secrets here. |
| `noiseCancellation` | `noise_cancellation` | `"false"` | Real-time noise suppression on the inbound audio. |
| `noiseCancellationLevel` | `noise_cancellation_level` | `85` | Documented range 60 to 100. What Plivo does with a value outside that range is not documented; stay inside it. |

Plivo does not substitute `{{CallUUID}}` or similar placeholders in the WebSocket URL. Your server renders the URL before it returns the XML.

REST: `POST/GET/DELETE https://api.plivo.com/v1/Account/{auth_id}/Call/{call_uuid}/Stream/[{stream_id}/]`. Stream object fields: `stream_id`, `call_uuid`, `service_url`, `bidirectional`, `audio_track`, `content_type`, `start_time`, `end_time`, `bill_duration`, `rounded_bill_duration`, `billed_amount`.

CLI: `plivo voice calls streams start <call_uuid> --url wss://... --bidirectional --content-type audio/x-mulaw;rate=8000 --stream-status-callback https://... [--extra-headers k=v,k2=v2]`. The CLI default content type is `audio/x-l16;rate=16000`; pass it explicitly. Also `streams list <call_uuid>`, `streams get <call_uuid> <stream_id>`, `streams stop <call_uuid> [stream_id]`.

Stopping the stream with `DELETE .../Stream/` on a document whose only element is `<Stream keepCallAlive="true">` is expected to end the call, because the stream was the last thing in the document and a document that runs out ends the call. That is an inference from the documented `keepCallAlive` behaviour, not a rule Plivo publishes, and no page names a hangup code for it. Treat it as a risk: anything you then try to do to that call, including a transfer, may find no live call to act on. Order a handoff the other way round, transfer first and then close the socket, and verify it once on your own account.

### Limits

| Limit | Value |
|---|---|
| WebSocket URL length | 2048 characters |
| Concurrent streams per call | 1 |
| Max stream duration | same as the call |
| Playback queue buffer | 40 s (best-practices; `DegradedStream` fires at 30, 60, 90% full). The guide says about 60 s. |
| Max WebSocket message | 64 KB; recommended audio chunk 16 KB base64 or less |
| Connect retries | if the first WebSocket connection fails Plivo attempts twice more, then drops the stream (best-practices, "WSS Socket Connection Failures") |
| Chunk cadence | about 20 ms per `media` event; about 160 bytes at mu-law 8 kHz |
| Disconnect | Plivo closes the stream and socket when the call ends. You do not need to. |

### Events Plivo sends to your server (JSON text frames)

| `event` | When | Key fields |
|---|---|---|
| `start` | once, on connect | `sequenceNumber` (starts at 1), `start.callId`, `start.streamId`, `start.accountId`, `start.tracks` (for example `["inbound"]`), `start.mediaFormat.encoding` (for example `audio/x-mulaw`), `start.mediaFormat.sampleRate`, `extra_headers` |
| `media` | continuously | `streamId`, `media.track` (`inbound`), `media.timestamp` (ms epoch, string), `media.chunk` (per-track counter), `media.payload` (base64 raw audio; decode it, there is no WAV header) |
| `dtmf` | caller presses a key | `dtmf.digit` (`0-9`, `*`, `#`, `A-D`), `dtmf.track`, `dtmf.timestamp` |
| `playedStream` | playback reached a checkpoint you set | `name` (your checkpoint name) |
| `clearedAudio` | the queue was cleared after your `clearAudio` | `streamId` |

`ForwardedFrom` is not in the `start` event. It arrives as an answer-URL parameter, and only when the carrier sends it.

Handling rules from the protocol reference, plus two observations:

- Everything is a JSON text frame; the audio is base64 inside `media.payload`. Plivo does not send binary WebSocket frames.
- Read the negotiated format from `start.mediaFormat`, not from the XML you think you returned. The two can differ when a proxy or a template rewrote the document.
- `extra_headers` is metadata, not authentication. Validate `X-Plivo-Signature-V3` on the upgrade request instead.
- The protocol reference documents no JSON `stop` input event, although the Stream XML page lists a "Stop" event in its WebSocket table. Handle the WebSocket close as the end of the stream, and accept a `stop` event if one arrives. `plivo voice streams test` does send a JSON `{"event":"stop"}` text frame before it closes, so the pre-flight exercises the `stop` path; a dropped call will not, so do not require `stop` to run your cleanup.
- One state machine per `streamId`; bound both queues; when the bot falls behind, drop or fail explicitly rather than let latency grow.
- A completed handshake proves nothing about decoded audio, return audio or a phone call.

### Events your server sends to Plivo

| `event` | Purpose | Body |
|---|---|---|
| `playAudio` | play audio to the caller (bidirectional only) | `media.contentType` (`audio/x-mulaw` or `audio/x-l16`), `media.sampleRate` (must match the stream's `contentType`), `media.payload` (base64 raw audio) |
| `checkpoint` | mark a point in the play queue; you get `playedStream` with the same `name` when it plays | `streamId`, `name` |
| `clearAudio` | drop everything queued. This is how barge-in works | `streamId` |
| `sendDTMF` | send tones into the call (drive an external IVR, enter a PIN) | `dtmf` (`0-9*#A-D` string) |

`playAudio.media.contentType` has no `;rate=` part; the rate goes in `media.sampleRate`. mu-law is `audio/x-mulaw`, not `audio/pcmu`. The Stream XML page's example quotes `sampleRate` as a string and the protocol reference shows a number; test the one your SDK sends. Send raw audio, never a WAV or MP3 container.

Pacing (docs): send audio at real-time cadence, about 20 ms per frame. Sending too fast gives the `buffer_overflow` error and fills the 40 s buffer, so `clearAudio` feels late. Use `checkpoint` to learn when a sentence finished.

Recommended by the docs: support interruption with `clearAudio`; treat `*` as interrupt and `#` as repeat; combine STT end-of-speech with a 300 to 500 ms timeout for turn-taking; aim for under 1 s total response (STT under 200 ms, LLM under 500 ms, TTS under 200 ms, network under 100 ms); host near the callers (US East or West, Frankfurt or London, Singapore or Mumbai). Plivo connects from the edge nearest the caller.

Time to first audio (not in the docs): call answer, then Plivo fetches the answer URL, parses the XML, opens the WebSocket, sends `start`, then your first `playAudio`. Read the first three timestamps from the console call debug log or `plivo voice calls diagnose`; measure the last from your own logs. There is no early-media or pre-answer stream in the docs, and no documented target for Plivo's own setup time.

Echo: the docs describe noise cancellation only. There is no documented echo cancellation on the audio Plivo sends you. If your bot's own speech shows up in the inbound audio, gate STT while the bot speaks or use your STT's echo suppression. That is a bot-side technique, not a documented Plivo feature.

### Stream status callbacks (HTTP to `statusCallbackUrl`)

Two naming schemes appear in the docs. The best-practices and troubleshooting pages, and Plivo's console debug logs use:

| `Event` | Meaning |
|---|---|
| `StartStream` | audio streaming began |
| `StopStream` | streaming stopped (call ended, API stop, or `streamTimeout`) |
| `DroppedStream` | WebSocket connect failed, was terminated mid-call, or was cut for being too slow. Sample debug-log `Error`: `connection disconnected with the remote service` |
| `DegradedStream` | slow connection; buffer 30, 60, 90% full |

The Getting Started guide and protocol reference instead describe `Event` values `started`, `stopped`, `failed` with `StatusReason` and `Duration`. This skill follows the first scheme. Log the raw `Event` string you receive rather than assuming either. Fields common to both: `CallUUID`, `StreamID`, `Timestamp`, `From`, `To`, `Direction`. The XML reference adds the stream's `bidirectional`, `audioTrack`, `streamTimeout`, `contentType`, `extraHeaders`, `keepCallAlive`.

A 200 with any body (JSON is fine) acknowledges a status callback. Console: Voice, Logs, Calls, the call, Audio Streams shows stream UUID, start and end, duration, billed amount, hangup reason (`API request`, `Call hangup`, `Connection error`, `Stream timeout`) and a Debug logs link with the event list. Debug logs are the fallback when no `statusCallbackUrl` is set; they are manual.

### Troubleshooting map (from the troubleshooting page)

| Symptom | Check |
|---|---|
| Connection never establishes | URL is `wss://` (every documented example is; do not depend on plain `ws://`), public, valid non-expired certificate, firewall allows inbound, tunnel still running |
| Drops mid-call | `DegradedStream` callbacks (slow link), server crash (add reconnect and graceful handling), send periodic pings |
| No `media` events | `audioTrack` is `inbound` or `both`; handler registered before start; `start` arrived first |
| Caller hears nothing | `bidirectional="true"`; `playAudio` `contentType` and `sampleRate` match the XML; raw audio, no file headers |
| Garbled audio | sample-rate mismatch; wrong codec; headers left in the payload |
| Slow responses | mu-law 8 kHz, deploy near callers, stream TTS as generated, pool AI connections |
| Error strings seen | `connection_failed` (URL or SSL), `authentication_failed` (signature), `invalid_content_type` (format mismatch), `buffer_overflow` (sending audio too fast) |

### What Plivo does inside `<Stream>`, billing, data kept

Plivo moves raw audio only. Speech recognition, the model and the voice are yours. Plivo TTS exists only as `<Speak>` before or after the stream; it cannot be injected mid-stream. If you want Plivo to run the AI and pick the voice in a console, that is the hosted AI Agents product, not `<Stream>`.

A `<Stream>` call is billed as the underlying Voice API call: from answer, per leg, 60 s minimum increment. The Stream object exposes `bill_duration`, `rounded_bill_duration` and `billed_amount`. Whether the stream itself carries a charge on your plan is not in the docs; the API example shows a non-zero `billed_amount`. Read the live pricing page for your currency. This skill does not quote prices.

Plivo keeps call records, the Stream object (URL, times, billing), stream debug-log events, and recordings only if you add `<Record>`. Anything in the WebSocket URL or `extraHeaders` is logged. Retention periods, HIPAA and BAA are not in the voice docs; ask your account manager.

### AI agent as a MultiPartyCall participant

XML: `<MultiPartyCall role="ai-agent" aiAgentStreamServiceUrl="wss://..." aiAgentStreamContentType="audio/x-mulaw;rate=8000" aiAgentStreamStatusCallbackUrl="..." aiAgentStreamStatusCallbackMethod="POST" aiAgentStreamSamplingRate="..." aiAgentStreamExtraHeaders="...">room</MultiPartyCall>`; default content type `audio/x-l16;rate=8000`.
REST Add Participant: required `role` (`agent`, `supervisor`, `customer` or `ai-agent`), `from` and `to`; then for `role=ai-agent` the optional `ai_agent_stream_service_url`, `ai_agent_stream_content_type`, `ai_agent_stream_status_callback_url`, `ai_agent_stream_status_callback_method` and `ai_agent_stream_extra_headers` (`key=value`). The CLI's `plivo voice multiparty participant add --role` accepts only `agent|supervisor|customer`, so add an AI participant with `plivo api POST /MultiPartyCall/name_<room>/Participant/`.

Add a Participant documents `role`, `from` and `to` as required arguments, and the `ai-agent` role is one of the documented `role` values, so send all three. The `ai_agent_stream_*` parameters are listed as the optional set to use when `role` is `ai-agent`. The skeleton below has every documented field in place, but it is **not runnable as written**: `to` is required and the docs do not say what value it should carry for a participant reached over a WebSocket, so fill that in only once Plivo has confirmed it. Preview it, do not send it blind:

```bash
plivo api POST /MultiPartyCall/name_consult-<id>/Participant/ --dry-run --body '{
  "role": "ai-agent",
  "from": "<a Plivo number on this account>",
  "to": "<the destination the API requires; see the note below>",
  "ai_agent_stream_service_url": "wss://voice.example.com/ws/consult-<id>",
  "ai_agent_stream_content_type": "audio/x-mulaw;rate=8000",
  "ai_agent_stream_status_callback_url": "https://voice.example.com/plivo/mpc-stream-status",
  "start_mpc_on_enter": true }'
```

then the same command with `--yes` once the user approves. Read the previewed body back to them first: this adds a participant to a live room.

Still not documented, so do not state any of it as fact: what `to` should be for an AI participant that is reached over a WebSocket rather than dialled, and whether Plivo dials or bills that leg; what the AI hears, the full room mix or one party; whether hold and mute must be set together to park a party, and the ordering when switching parties; whether the XML form opens the socket the same way the REST form does. Ask Plivo before you build on any of those. Use the documented-attributes-only answer document below, add the AI over REST, then dial the human with `plivo voice multiparty participant add consult-<id> --from <your number> --to <human number> --role agent --dry-run` followed by `--yes`, and park a party with `plivo api POST /MultiPartyCall/name_consult-<id>/Member/<member_id>/ --body '{"mute": true, "hold": true}' --dry-run` then `--yes`. Test each step on a real room before relying on it, and write down what you observe.

```xml
<Response>
  <MultiPartyCall role="customer" startMpcOnEnter="false" customerHoldMusicUrl="https://voice.example.com/hold" statusCallbackUrl="https://voice.example.com/plivo/mpc-status" statusCallbackEvents="participant-state-changes">consult-{{CallUUID}}</MultiPartyCall>
</Response>
```

## Hangup causes and end-of-call patterns

Docs pages: <https://www.plivo.com/docs/voice/troubleshooting/hangup-causes>, <https://www.plivo.com/docs/voice/troubleshooting/call-failures>, <https://www.plivo.com/docs/voice/xml/overview>, <https://www.plivo.com/docs/voice/use-cases/transfer-to-human-agent>, <https://www.plivo.com/docs/voice/xml/routing>, <https://www.plivo.com/docs/voice/concepts/india-concurrency>, <https://www.plivo.com/docs/voice/api/calls>, <https://www.plivo.com/docs/voice/call-insights>. A line marked as an observation describes what streaming deployments run into in practice and has no docs page behind it; a line marked "not verified" is neither documented nor confirmed. Do not repeat either as a documented Plivo rule.

Where to read them: `plivo voice calls get <call_uuid> -o json` gives `hangup_cause_code`, `hangup_cause_name`, `hangup_source`, `answer_time`, `end_time`, `bill_duration`; `plivo api GET /Call/<call_uuid>/` returns the full record. The `hangup_url` callback carries the raw telephony `HangupCause` (`NORMAL_CLEARING`, `USER_BUSY`, `NO_ANSWER`, `CALL_REJECTED`, `UNALLOCATED_NUMBER`, `NETWORK_OUT_OF_ORDER`). The Dial `action` URL carries `DialHangupCause` and `DialBLegHangupCauseCode` for the human leg. Console: Voice, Logs, Calls; each call has Audio Streams and Debug logs tabs. Hangup sources (docs): `Caller`, `Call recipient`, `Plivo`, `Carrier`, `API Request`, `Answer XML`, `Error`, `Unknown`.

Triage order: read the call record for every leg; inspect the exact answer, action or transfer HTTP exchange; check the exact body against the XML checklist; read the stream status callbacks and the bot's close or error log; read Call Insights per leg; retest the WebSocket on its own. Do not repeat a billable call until the failed layer passes.

### Normal ends of an agent call

| Code | Name | What it means on a `<Stream>` call | Check |
|---|---|---|---|
| 4000 | Normal Hangup | Caller or callee hung up. The most common end. | Nothing. |
| 4010 | End Of XML Instructions | The XML ran out. With `keepCallAlive="true"` and nothing after `<Stream>`, this is the normal end when the bot closes the socket. | A call that lasted only a few seconds is a reason to look at the bot's connect handler, the stream status callbacks and the console Audio Streams log. The call record does not say who closed the socket, so do not report the bot as the cause until one of those three confirms it. |
| 1000 | Cancelled, source `API Request` | Your backend hung up with the Hangup API or `plivo voice calls hangup`. A normal way for a bot to end a call. | Nothing, if your code did it. |
| 4020 / 4030 | Multiparty Call Ended / Kicked Out | The MPC room ended or a participant was removed. Expected in the room patterns. | Was the room end intentional? |
| Source `Answer XML`, code 4000 | Your XML ended the call | `<Hangup/>` after `<Stream>` runs when the socket closes and ends the call cleanly. | Nothing. |

Two patterns that look like failures but are usually not:

- The human hangs up within a few seconds of answer (source `Caller` inbound, `Callee` outbound). Read the hangup source before you blame the socket: a caller-sourced hangup is a person, not a defect. Causes the docs point at: silence before the first word, a slow greeting, voicemail. Fix on your side: speak first and fast, keep `<Speak>` before `<Stream>` short or drop it, use answering-machine detection outbound.
- The bot closes the socket a few seconds after answer (code 4010, source `Plivo`). Check the bot's connect handler and `plivo voice streams test --bidirectional`.

### Failures Plivo attributes to your URLs and XML

| Code | Name | Meaning | First check |
|---|---|---|---|
| 7011 | Error Reaching Answer URL | Non-2xx, unreachable, timeout or empty body from the answer URL. Not only a day-one problem: mature deployments whose answer URL fails under load produce it for months. | `curl -s -i -X POST <answer-url> -d 'CallUUID=x&From=%2B1&To=%2B1&Direction=inbound&Event=StartApp'`, then the XML checklist. Set a fallback URL. Alert on the 7011 rate, not just on the first call. |
| 8011 | Invalid Answer XML | The answer URL replied, but not with Plivo XML: JSON (a console flow application with no flow, or an API endpoint), HTML, malformed XML, a `<Speak language>` Plivo does not support, a Twilio element such as `<Reject/>`. | Run the checklist on the body. Console debug logs show the body Plivo saw. |
| 8012 / 7012 | Invalid / Error Reaching Action XML | The second document (GetDigits, GetInput, Record or Dial `action`) was bad or unreachable, typically well into the call. Only fatal when `redirect="true"` (docs). | Check the action handler's response, without the `<Stream>` requirement. |
| 8013 / 7013 | Invalid / Error Reaching Transfer URL | The transfer URL (stage 7) failed or returned bad XML. | `curl` the transfer URL and check it the same way. |
| 8014 / 7014 | Invalid / Error Reaching Redirect XML | A `<Redirect>` target failed. | Same. |
| 7022 to 7034 | Invalid URL / Invalid Method | An action, transfer or redirect URL is not `http(s)://`, or the method is not GET or POST. | Fix the attribute. |
| 3020 / 3010, source `Answer XML`, 0 s | Rejected / Busy Line | Your XML returned `<Hangup reason="rejected"/>` or `<Hangup reason="busy"/>` as the first element. Observed pairing, not a documented one: the routing page gives the audible effect (rejection tone, busy signal) and the hangup-causes table describes 3020 and 3010 from the called party, so this mapping comes from call records. Deliberate call screening for some deployments. | Intended? Then fine. If not, find who added the `reason`. A bare `<Hangup/>` has not been seen to land here: it ends the call gracefully and shows as a normal end from source `Answer XML`. |
| Source `Answer XML`, 4000, no stream | Message-only IVR | `<Speak>` or `<Play>` then `<Hangup/>`: out-of-hours or deflection messages. | Intended? Fine for a non-agent document. |

### Timeouts and ceilings

| Code | Name | Meaning | Check |
|---|---|---|---|
| 4010 at exactly N seconds, source `Plivo` | streamTimeout or another fixed ceiling | `streamTimeout` caps the stream (docs: default 86400 s). When it fires the stream stops and, with nothing after `<Stream>`, the call ends 4010. Calls that all end at the same second (300, 600, 1200 s) point at a configured ceiling. Per call, only a `StopStream` callback with reason "Stream timeout" or the console Audio Streams log proves it. | A 300 s or 600 s ceiling cuts real conversations. Set it to your longest acceptable call or leave the default. Rule out the bot's own timer too. |
| 6000 | Scheduled Hangup | Max call duration (`time_limit` on the API, `timeLimit` on Dial; default 4 h, max 86400 s). | Intentional? |
| 6010 | Ring Timeout Reached | Not answered within `ring_timeout` (default 120 s). Expected on outbound campaigns. | Tune `ring_timeout`. |
| 6020 | Media Timeout | No media packets for 60 seconds (docs). The docs say: check network connectivity. This code alone does not say which side lost media. | Check both media paths and the carrier. Call Insights shows packet counts per leg. |
| 0 | Unknown | Hangup reason undetermined. Docs note a known bug: the Delete All Calls API sets it. | Check the debug logs; contact Plivo support with the UUID if it recurs. |

### Handoff (`<Dial>`) outcomes

The human leg is a separate B-leg with its own call record. A meaningful share of human legs never answer (busy, caller cancelled, SIP endpoint not registered, lost race, no answer). Read `DialStatus` on the Dial action URL: `completed`, `busy`, `failed`, `cancel`, `timeout`, `no-answer`.

| Code | Name | Meaning | Check |
|---|---|---|---|
| 4010 + raw `USER_BUSY` | End of XML, busy target | The human was busy and nothing followed `<Dial>`. | Put `<Stream>` or `<Speak>` after `<Dial>` so the caller is not dropped. |
| 2020 | Endpoint Not Registered | The SIP softphone or endpoint is offline. | Check the agent's registration before dialling; fall back to a number. |
| 9000 | Lost Race | Another parallel Dial leg answered first. Normal for simultaneous dial. | Nothing. |
| 4240 / 4250 | sip_auth_failed / sip_auth_timeout | The SIP contact centre rejected the credentials or did not answer the digest challenge. | Check `sipAuthUsername`, `sipAuthPassword`, realm, IP allow-list. |
| 4210 | sip_auth_failed (inbound) | An external carrier sending SIP to your application failed the IP ACL or credential check. | See <https://www.plivo.com/docs/voice/use-cases/connect-external-numbers>. |
| 9110 | Confirm Key Challenge Failed | `<Dial confirmKey>` was not pressed by the human leg. | Check the confirm prompt and key. |
| A-leg continues after the B-leg ends | Caller returned to the bot or to the next element | This is what a `<Stream>` after `<Dial>` produces. | Make sure the next element is what you want the caller to hear. |

### Carrier and destination codes an outbound agent sees daily

These never start a stream; they come from the carrier or the destination.

| Code | Name | Meaning (docs) | Action |
|---|---|---|---|
| 3000 | No Answer | Destination unavailable or unreachable. | Retry later. |
| 3080 | Carrier-side error | The carrier returned an error. | Retry; if persistent, Plivo support. |
| 2000 | Invalid Destination Address | Not E.164 or invalid. | Fix list hygiene. |
| 3070 | Request Timeout | Carrier did not respond in time. | Retry. |
| 3050 | Unallocated Number | Destination invalid or out of service. | Prune the list. |
| 3110 | Declined | Destination cannot or will not participate. | Verify the destination accepts calls. |
| 5020 | Routing Error | Could not route. | Plivo support with the call UUID. |
| 3040 | Forbidden | Destination rejected or blocked the call. | Recipient may block your number. |
| 3090 | Network Congestion | Carrier overloaded. | Retry with backoff. |
| 5000 | Network Error | Fatal network condition. | Plivo support with the call UUID. |
| 5010 | Platform-side error | Plivo-side error (docs). | Plivo support with the call UUID. |
| 2060 | Loop Detected | The B-leg would redial the A-leg's number. | Fix routing logic. |
| 3130 | Spam block | Carrier rejected on spam reputation. | STIR/SHAKEN A with your own Plivo number, Caller Reputation, list hygiene. |
| 3140 | DNO Caller ID | Caller ID is on a Do-Not-Originate list. | Use another number. |
| 3030 | Unknown Caller ID | Caller ID is neither a number rented on this account nor an accepted verified caller ID for this route. | Use a Plivo number you rent. |
| 2030 / 2040 / 2050 | Destination Country / Number / Prefix Barred | Geo permissions (Professional plan: US and India only). | Console, Voice, Geo Permissions. |
| 2010 / 3100 / 3120 | Destination Out Of Service / Busy Everywhere / User Does Not Exist Anywhere | Destination-side conditions (docs). | Verify the number; retry later or prune. |
| 2070 | Violates Media Anchoring | India: a leg or your server is outside India. | India section, media anchoring. |
| 5030 | Concurrency Limit Breached | India: over the account's concurrent-call limit. Rejected instantly. | Stagger; ask Plivo support to raise it. |
| 1010 | Cancelled (Out Of Credits) | Balance hit zero. | `plivo account get`; auto-recharge. |
| 1020 | Cancelled (Simultaneous dial limit) | Too many concurrent dials to one destination. | Pace the campaign. |
| 9100 | Machine Detected | Voicemail with `machine_detection=hangup`. | Expected; watch short-call thresholds. |

### Audio quality flags (Call Insights)

The console shows "Suspected Issues" per leg. Docs mapping: One-Way Audio: packet count and audio level. Broken Audio: packet loss. Robotic Audio: jitter. High Connect Time: post-dial delay. Audio Lag: round-trip time. Also `LOW_AUDIO_LEVEL`, `MEDIA_IP_NOT_WHITELISTED` (fix your firewall allow-list; never disable the firewall) and `NO_AUDIO`. The docs note that quality statistics are not available for all PSTN calls. What the flags mean for your bot's WebSocket leg is not documented (not verified). Use them to decide whether to look at the carrier leg or at your server.

### Stream-level reasons (not hangup codes)

Stream status callback `Event=DroppedStream` (socket failed to connect, was terminated, or was too slow) and `DegradedStream` (buffer 30, 60, 90% full) tell you the bot side failed while the call itself may have ended 4010. Console Audio Streams log "Hangup reason": `API request`, `Call hangup`, `Connection error`, `Stream timeout`.

Codes not listed in the docs still appear in `hangup_cause_name`; contact Plivo support with the `call_uuid`. SIP trunking and MPC `termination_cause_code` use separate code sets.

### Escalation packet for Plivo support

Send: the call UUID and any parent or B-leg UUIDs; UTC timestamps; hangup code, name and source per leg; region; the answer URL host (never the full URL with query or credentials); HTTP status and response time from your logs; the exact XML body Plivo fetched, redacted; stream status callback events received; the WebSocket close code and reason from your server; Call Insights flags; and whether `plivo voice streams test --bidirectional` passes against the same host. Never include tokens or caller audio.

## Questions customers ask that the docs do not answer

Say so plainly, then point at the source. Do not fill the gap from memory. The recurring gaps: the per-minute price of a streamed call and whether the stream itself is billed (read the live pricing page or `plivo ask`; the Stream object exposes `bill_duration` and `billed_amount`); Plivo's answer-to-`start` setup time and any pre-answer stream (measure from the console debug log); `<Stream>` on SIP-trunk legs, which Indian series allow it, and the answer-URL length limit (ask Plivo support); enablement (no switch exists; what blocks people is US-trial outbound gating and India KYC); data retention, HIPAA and BAA (ask your account manager); echo cancellation (only noise cancellation is documented); which stream packages are actually published for a given language (check the current integration guide and that language's package index before recommending one, and fall back to the raw protocol, which is fully documented). Third-party agent platforms that connect over SIP do not use `<Stream>` at all: that is <https://www.plivo.com/docs/voice-agents/sip-trunking>, and the skill for it is a separate install (`npx skills add https://www.plivo.com/docs --skill plivo-sip-trunking`). Plivo's hosted AI Agents product is a different product again.

| Theme | What the skill can say | Source or boundary |
|---|---|---|
| A. Connect my bot | Return `<Stream>` from the answer URL and speak the WebSocket protocol. Pass a short opaque id in the URL for correlation. Plivo documents a Node stream package that validates the WebSocket signature. Check the current integration guide and the language's package index before recommending a package; the raw protocol is documented and always available. | `voice-agents/audio-streaming/concepts/audio-streaming-guide`, `.../integration-guides/plivo-stream-sdk` |
| B. Answer or XML errors, URL length | 7011 is an HTTP problem; 8011 is a body problem. Capture the exact body and run the XML checklist. For the general XML rules beyond a streamed call, install `plivo-voice-xml` or read the XML overview page. The Stream URL limit is 2048 characters; the answer-URL limit is not published (the console rejects very long ones). Carry context through `CallUUID`, `X-PH-*` SIP headers or a short id. | `voice/troubleshooting/hangup-causes`, `voice-agents/audio-streaming/xml/stream` |
| C. Latency and pacing | Time to first audio = answer, answer-URL fetch, XML parse, WebSocket open, `start`, your first `playAudio`. Read the first steps from the console call debug log; measure the last from your logs. Send 20 ms frames at real-time cadence; bursting fills the 40 s buffer and delays barge-in. Plivo's own setup time and any pre-answer stream are not documented. | `.../concepts/audio-streaming-reference`, `.../concepts/best-practices`, `.../troubleshooting/troubleshooting` |
| D. Human handoff | Transfer API then `<Dial>` in a later document, or `<Dial>` or `<Redirect>` after `<Stream>` in the same document. Order it API first, then close the socket. Never `DELETE .../Stream/` first: on a document that ends at the stream, stopping the stream ends the call. | stage 7; `voice-agents/audio-streaming/xml/stream` (keepCallAlive); `voice/api/calls` (Transfer) |
| E. Audio problems, echo | Match `contentType` in both directions; `bidirectional="true"`; raw audio, no headers. `audio/x-mulaw;rate=16000` is not one of the three documented audio formats; mu-law is documented at 8 kHz only. Only noise cancellation is documented; there is no documented echo cancellation on the audio Plivo sends you. | `voice-agents/audio-streaming/xml/stream`, `voice/call-insights` |
| F. Pricing and billing | Billed as the underlying call, per leg, from answer; the Stream object exposes `bill_duration` and `billed_amount`. Whether the stream itself is charged on a given plan, and the price, are not in the docs: read the live pricing page or `plivo ask`. Quote no number. | `voice/api/calls` (When Billing Starts), `voice-agents/audio-streaming/api/audio-streams` |
| G. Enablement | `<Stream>` has no switch. What blocks people: outbound voice disabled on US-region trial organisations ("Voice capability is currently disabled"; not in the docs), India KYC, and separately enabled products. | `voice-agents/audio-streaming/overview`; India section |
| H. Recording | `<Record recordSession="true">` before `<Stream>`. Disclosure, consent and retention are the customer's to check. | `voice/xml/record`, `voice/api/recordings` |
| I. DTMF and barge-in | `dtmf` events over the socket; `clearAudio` for interruption; `checkpoint` and `playedStream` to learn what played. Keypad menus before the stream use `<GetDigits>`. | `.../concepts/audio-streaming-reference`, `voice/xml/input` |
| J. Callbacks, signatures, data kept | Validate V3 signatures; handlers idempotent on `CallUUID` or `StreamID` plus event. Plivo keeps call records, the Stream object, debug-log events and recordings you asked for; anything in the URL or `extraHeaders` is logged. Retention periods, HIPAA and BAA: not in the voice docs; ask the account manager. | `voice/concepts/signature-validation` |
| K. Multi-tenant | One application can serve every tenant (route on `To`). A subaccount per client only for separate numbers, a separate signature token or separate call records. Architecture guidance itself is not in the docs. | multi-tenant notes above |
| L. Speech and AI inside the stream | Plivo moves raw audio only. STT, the model and the voice are the customer's. Plivo TTS exists only as `<Speak>` before or after the stream. | `.../concepts/audio-streaming-guide` (AI Service Credentials) |
| M. Calls not connecting, same number for humans and AI | The debug loop above (`diagnose`, `get`, the XML checklist, `streams test`). The application decides per call whether to return `<Dial>` or `<Stream>`. `ForwardedFrom` arrives only when the carrier sends it and is not in the `start` event. | hangup causes; `voice/concepts/callbacks` |
| N. India | Data region, KYC, series, media anchoring, consent, UCC, concurrency: the India section. Which series allow `<Stream>` is not documented. | `voice/concepts/india-calling`, `numbers/rent-india-numbers` |
| O. Answering-machine detection | Asynchronous; `Machine=true` reaches `machine_detection_url` after your XML already ran. Not a speech classifier inside the socket. | `voice/concepts/machine-detection` |
| P. SIP-based agent platforms | Agent platforms that connect over SIP trunking do not use `<Stream>`. Do not translate SIP settings into Stream attributes. That is a separate skill and a separate install: `npx skills add https://www.plivo.com/docs --skill plivo-sip-trunking`. | `voice-agents/sip-trunking` |
| Q. Hosted AI agents | A different product surface. This skill cannot answer its access, pricing or model questions. | `voice-agents` overview |
| R. Browser, WebRTC and SIP endpoints | Their own SDKs and docs; out of scope here. | `sdk/client`, `voice/api/endpoints` |
| S. Generic call debugging | Hangup causes and Call Insights; stream-specific checks only if a stream started. | hangup causes above |
| T. Sales, trial, demo | Do not invent eligibility, credits, provisioning times, prices or approval outcomes. Point at the commercial pages or the account manager. | public commercial pages |

Two more items with no docs answer: whether `<Stream>` works on SIP-trunk legs, and the maximum answer-URL length. Route both to Plivo support.

## When this skill does not have the answer

Do not guess, and do not fill the gap from general knowledge of other platforms. In order:

1. **Read the current documentation.** Every page on <https://www.plivo.com/docs> is available as Markdown by adding `.md` to its URL, and <https://www.plivo.com/docs/llms.txt> lists every page. Start at <https://www.plivo.com/docs/voice-agents/audio-streaming/overview>, <https://www.plivo.com/docs/voice-agents/audio-streaming/xml/stream> and <https://www.plivo.com/docs/voice-agents/audio-streaming/troubleshooting/troubleshooting>. From a terminal `plivo docs search <keywords>` searches the full text of every page, `plivo docs list` prints the index and `plivo docs show <path-or-title>` prints one page; those three need no credentials and are not rate limited, so reach for them before the assistant.
2. **Ask Plivo's assistant from the terminal**: `plivo ask "<your question>"`. It reads the documentation and can see the account, so it answers things this file cannot: what a specific call did, whether a compliance application is accepted, what a destination costs. It is limited to five requests per ten minutes per account, so save it for the question you cannot answer another way. `plivo voice calls diagnose <call_uuid>` is the same assistant pointed at one call, and it shares that limit, so do not loop either.
3. **If you have no CLI access**, tell the person you are working with to ask the same question to the assistant in the Plivo console.

Treat the answer as evidence, not as final. If it contradicts the documentation, say that it does and prefer the documentation for published behaviour. If it gives a number the documentation does not publish, repeat it as something the assistant said, not as a documented fact.

For CLI behaviour, `plivo <command> --help` outranks this file: if the two disagree, the CLI is right and this file needs updating, and you should say so. Never invent flag names, XML attributes or hangup codes. Where Plivo's own pages disagree (codec default, `extraHeaders` separator, stream-callback event names, playback buffer size, short-call threshold), this file names both values and which one it follows.

## CANNOT

- Cannot treat an accepted API request as success. `calls make` returning a `request_uuid` means Plivo accepted the request, not that the bot spoke. A checklist pass, an HTTP 2xx or a WebSocket handshake is not success either. A call worked only when the call record, the callbacks, the bot's log and a human who heard the audio agree.
- Cannot spend or reconfigure without a preview. Before `calls make --yes`, `numbers buy --yes` or re-attaching a number, show the `--dry-run` output or the current number-to-application binding, and offer the rollback command (`plivo numbers update <number> --app-id <previous_app_id>`). Never bulk-call from this skill.
- Cannot infer account state. Look numbers, applications, compliance applications and calls up with the CLI. Never assume a number is voice-enabled, compliance-approved, in the right data region, or attached to the right app.
- Cannot fill in or submit KYC on its own. Business name, CIN or Udyam number, GSTIN and address are copied from the documents the user supplies, never inferred. A compliance application is a regulatory filing and is only submitted after the user explicitly says so. KYC, series choice, 140 and 160 provisioning and UCC proof have fixed timelines that cannot be shortcut. Verified Caller ID does not exist for India.
- Cannot decide legal compliance. Consent, disclosure, recording, retention and calling-hours rules need the user's own legal review; this skill states the platform rules only.
- Cannot overstate platform rules. Plivo accepts `application/xml` or `text/xml`. Use HTTPS and `wss://`: every documented example does. Whether Plivo would accept plain `ws://` or `http://` is not documented, so do not claim either way.
- Cannot name a `contentType` default or a stream-callback event name as certain. The docs disagree; set the codec explicitly and log the raw `Event` you receive.
- Cannot invent CLI flags for what the CLI lacks: no `applications update --fallback-answer-url`, no `calls make --machine-detection-url`, no `numbers buy --compliance-application-id`, no `participant add --role ai-agent`, no verified-caller-ID or UCC commands. Use `plivo api <METHOD> <path>` or the console and say so.
- Cannot diagnose calls on another account. `diagnose` and `ask` share a rate limit.
- Cannot declare a number ready. Static and synthetic checks end at "the Plivo side looks right". Only a real call observed in this session closes gate 5.

This skill does not cover: general Plivo XML in full (every element other than `<Stream>`: separate install, `npx skills add https://www.plivo.com/docs --skill plivo-voice-xml`), SIP-trunking agents (separate install, `--skill plivo-sip-trunking`), SMS or WhatsApp, the Browser SDK, WebRTC and SIP endpoints, conferences beyond the room patterns, number masking, SSML depth, pricing, or the inner workings of any bot framework. It cannot see your server's logs or your Plivo account's data; use the CLI commands it names for that.

Sources: the Voice API and Audio Streaming sections of the Plivo docs (pages cited above), and the CLI's own `--help` output. Where a rule is marked as an observation rather than documented, it has no docs page behind it: re-verify it before you rely on it, and re-verify every rule in this file if the platform changes.
