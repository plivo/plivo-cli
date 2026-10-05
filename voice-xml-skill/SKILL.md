---
name: plivo-voice-xml
description: "Writes and debugs Plivo Voice XML, the document an answer URL returns to control a call: Speak, Play, GetDigits/GetInput IVRs, Dial forwarding, Record and voicemail, Conference, MultiPartyCall. Use for Plivo XML, answer or action URLs, 8011/8012 invalid XML, 7011 URL errors, X-Plivo-Signature-V3 checks. Not for WebSocket voice bots (plivo-audio-streaming) or SIP trunks (plivo-sip-trunking)."
license: Apache-2.0
---

# Plivo Voice XML

Rules here come from the Plivo docs unless marked *observed* (seen on real calls, no docs page). Say a call will break only for a documented rule or a code the evidence shows; raise anything observed or undocumented as a risk. Never invent an element, attribute, value or hangup code. Plivo's handling of an unknown element or attribute is undocumented, so a call that worked does not prove an attribute is real: remove anything undocumented.

When this file runs out: `plivo docs search "<terms>"` and `plivo docs show <path>` (no credentials needed), then `plivo ask "<question>"` (rate limited; it can see the account). Prefer the docs where `plivo ask` disagrees with them. This skill cannot see the user's server: ask for the exact HTTP status and bytes.

Install another skill when the task leaves XML: `plivo skill install audio-streaming` for WebSocket voice bots and `<Stream>`, `plivo skill install sip-trunking` for SIP trunks (no answer URL, no XML). For CLI flags run `plivo <cmd> --help`.

Before writing, pin down: inbound or outbound, what the caller hears first, keypad or speech input, where the call goes, whether it is recorded (and the caller told), and how it ends.

## Validate, fix, repeat

Run this loop on every document you write or change, including every document an `action` or `<Redirect>` URL returns:

1. **Parse it.** `xmllint --noout answer.xml`, or any XML parser. Every `&` inside an attribute must be `&amp;`.
2. **Fetch it the way Plivo will**, with the same method, and read the status line, the `Content-Type` and the first bytes of the body:
   ```bash
   curl -s -i -X POST https://YOUR-HOST/plivo/answer \
     -d 'CallUUID=test&From=%2B10000000000&To=%2B10000000001&Direction=inbound&CallStatus=ringing'
   ```
   Repeat for each `action` URL with that element's parameters (`Digits=1`, `DialStatus=no-answer`, and so on).
3. **Review the flow.** Something after every element that can yield nothing, an exit on every redirect loop, `log="false"` on anything that collects a secret, a Fallback Answer URL on the application.
4. **Place one call, preview first.** A call costs money and rings a real phone.
   ```bash
   plivo voice calls make --from <number> --to <phone> --answer-url https://YOUR-HOST/plivo/answer --answer-method POST --dry-run
   plivo voice calls make --from <number> --to <phone> --answer-url https://YOUR-HOST/plivo/answer --answer-method POST --yes  # only after the user agrees
   plivo voice calls diagnose <call_uuid>
   ```
   `--answer-method` defaults to `GET`. Set `POST` explicitly, or Plivo fetches a route your handler may not serve.
5. **Fix what failed and go back to step 1.** Well-formed XML is not a working call: only the call record and someone who heard the audio confirm it.

## The answer-URL contract

- Plivo requests the answer URL (inbound: the number's application; outbound: `answer_url` on the API call) and runs the one document it returns.
- Reply with `Content-Type: application/xml` or `text/xml`, valid XML, under 100 KB, well inside 15 seconds.
- The root is `<Response>`. Children run top to bottom, one at a time. Element and attribute names are case-sensitive: `<speak>` is not `<Speak>`, and `sip_auth_username` is not `sipAuthUsername`.
- Running out of elements ends the call (4010, normal). An empty `<Response></Response>` ends it at once: a fine acknowledgement to a callback, a dropped call from an answer URL.
- No usable HTTP answer (non-2xx, timeout, dead host) is 7011. A 2xx whose body is not Plivo XML (JSON, HTML, plain text, an empty body) is 8011.
- Every request carries `CallUUID`, `From`, `To`, `CallStatus`, `Direction`. Inbound: `From` is the caller, `To` your number. Outbound: `From` is your caller ID, plus `ALegUUID` and `ALegRequestUUID`. SIP headers arrive as `X-PH-<Name>`.
- Set a Fallback Answer URL on the application (`fallback_url` on an API call) so one failing primary URL does not fail every call.

## Docs pages

Outside a terminal, `docs show` and `docs search` print a JSON envelope: add `-o table`.

| Covers | CLI | Web |
|---|---|---|
| `Speak`, `Play`, `DTMF` | `plivo docs show voice/xml/audio-output` | https://plivo.com/docs/voice/xml/audio-output |
| `GetDigits`, `GetInput` | `plivo docs show voice/xml/input` | https://plivo.com/docs/voice/xml/input |
| `Dial` (`Number`, `User`), `Redirect`, `Hangup`, `Wait`, `PreAnswer` | `plivo docs show voice/xml/routing` | https://plivo.com/docs/voice/xml/routing |
| `Record`, transcription, recording retention | `plivo docs show voice/xml/record` | https://plivo.com/docs/voice/xml/record |
| `Conference` | `plivo docs show voice/xml/conference` | https://plivo.com/docs/voice/xml/conference |
| `MultiPartyCall`, roles, status events, AI agent attributes | `plivo docs show voice/xml/multiparty-call` | https://plivo.com/docs/voice/xml/multiparty-call |
| `Message` (SMS from a call flow) | `plivo docs show messaging/xml/overview` | https://plivo.com/docs/messaging/xml/overview |
| `Stream` (use plivo-audio-streaming) | `plivo docs show voice/xml/audio-streaming` | https://plivo.com/docs/voice/xml/audio-streaming |
| Nesting, request parameters, response rules | `plivo docs show voice/xml/overview` | https://plivo.com/docs/voice/xml/overview |
| `action` versus `callbackUrl`, idempotency | `plivo docs show voice/concepts/callbacks` | https://plivo.com/docs/voice/concepts/callbacks |
| Timeouts, retries, edge region, which URLs they apply to | `plivo docs show voice/concepts/callback-configurations` | https://plivo.com/docs/voice/concepts/callback-configurations |
| Signature validation | `plivo docs show voice/concepts/signature-validation` | https://plivo.com/docs/voice/concepts/signature-validation |
| SSML tags, Polly voices per language | `plivo docs show voice/concepts/ssml` | https://plivo.com/docs/voice/concepts/ssml |
| Every hangup code | `plivo docs show voice/troubleshooting/hangup-causes` | https://plivo.com/docs/voice/troubleshooting/hangup-causes |

## Ordering and nesting

Documented children: `GetDigits` and `GetInput` take `Speak` and `Play`; `Dial` takes `Number` and `User`; `PreAnswer` takes `Speak`, `Play` and `Wait`; everything else is a child of `Response`. `Speak` may also hold SSML tags with a `Polly.` voice. The docs never say other nesting is rejected: call it undocumented and move the element out.

- **`redirect` decides who owns the rest of the call.** It defaults to `true` on `GetDigits`, `GetInput`, `Record`, `Dial` and `Conference`: when the `action` URL answers, Plivo runs the XML it returns, so the elements below may not run for a caller who responded. With `redirect="false"` the URL is still called, its body is ignored, and the next element runs. A bad or unreachable action document fails the call (8012, 7012) only when `redirect` is `true`. Raise skipped trailing elements as a risk, not a broken call.
- **Put a fallback after anything that can yield nothing.** No input after `retries`, or an unanswered `Dial`, falls through to the next element. With none, the call ends in silence.
- **`<Record recordSession="true"/>` goes before what it records.** It runs in the background until the call ends and ignores `timeout`, `finishOnKey` and `playBeep`. Before a `<Dial>`, add `startOnDialAnswer="true"` to skip the ringing.
- **Treat `<Redirect>` as the end of the document.** Plivo continues the call at the new URL; the docs do not say what happens to siblings after it, so move them into the next document.
- **`<Hangup/>` ends the call; `<Hangup schedule="60"/>` only starts a timer**, and the following elements keep running.
- **Put `<PreAnswer>` first.** That is an inference; the docs list only its limits: `Speak`, `Play` and `Wait` inside, under 30 seconds, some carriers time out, the caller is not billed. Never `loop="0"` inside it when a `<Dial>` follows, or the `Dial` may never run. For ringback while dialling, `dialMusic` on `<Dial>` is simpler.

## `action` versus `callbackUrl`

- `action` expects XML, and Plivo runs it when `redirect` is `true`; returning `OK` or JSON there is then an 8012.
- `callbackUrl`, `ring_url` and the hangup URL are notifications. Return 200 with an empty body or `<Response></Response>`.
- Retries can deliver the same request twice. Make handlers idempotent on `CallUUID` (plus the element context for `action`, `RecordingID` for recordings).
- Per-URL timeouts, retries and edge region go in a URL fragment, for example `https://example.com/answer#ct=2000&rt=3000&rc=2&rp=ct,rt`. Keys, ranges and defaults are on `voice/concepts/callback-configurations`. Fragments do not apply to audio URLs in `<Play>` and `<PreAnswer>`.
- Element URL methods default to `POST`, except the MultiPartyCall `enterSoundMethod`, `exitSoundMethod`, `startRecordingAudioMethod` and `stopRecordingAudioMethod`, which default to `GET`. Test each URL with the method it is actually configured with.

## The elements most documents need

### Speak and Play

- `<Speak voice="..." language="..." loop="...">text</Speak>`. `voice`: `WOMAN` (default), `MAN`, or `Polly.<Name>`, which SSML requires. `loop` defaults to `1`; `0` repeats until the call ends.
- `language` defaults to `en-US`. Documented for `WOMAN` and `MAN`: `da-DK`, `nl-NL`, `en-AU`, `en-GB`, `en-US`, `fr-FR`, `fr-CA`, `de-DE`, `it-IT`, `pl-PL`, `pt-PT`, `pt-BR`, `ru-RU`, `es-ES`, `es-US`, `sv-SE`, with no `MAN` for `da-DK`, `fr-CA`, `ru-RU`, `sv-SE` and no `WOMAN` for `pt-PT`. A language outside that table is untested: test it on a real call before shipping it, and neither promise that it works nor claim that Plivo rejects it. For `hi-IN` use `Polly.Aditi`; for `en-IN`, `Polly.Raveena` or `Polly.Aditi`.
- SSML: a `Polly.` voice, at most 3,000 characters per `<Speak>`, and no `<amazon:effect>` or `<amazon:auto-breaths>`.
- `<Play loop="...">https://...</Play>`: HTTPS, mp3 or wav, at most 10 MB, 8 or 16 kHz mono recommended. Text inside `<Play>` is not spoken.

### GetDigits

The docs recommend `GetInput` for new work.

| Attribute | Default | Notes |
|---|---|---|
| `action` | none | receives `Digits` (without the `finishOnKey` key) and the standard parameters |
| `method` | `POST` | |
| `numDigits` | `99` | maximum digits |
| `timeout` | `5` | seconds to wait for the first digit |
| `digitTimeout` | `2` | seconds between digits |
| `finishOnKey` | `#` | a digit, `#`, `*` or `none` |
| `retries` | `1` | "Retry attempts if no input"; with no digits after `retries` attempts, the next element runs. Undocumented: whether the prompt replays, and whether `1` means one try or two |
| `redirect` | `true` | |
| `playBeep` | `false` | beep after the prompts |
| `validDigits` | `1234567890*#` | |
| `invalidDigitsSound` | none | audio URL played on an invalid digit |
| `log` | `true` | set `false` for PINs and card numbers |

### GetInput

| Attribute | Default | Notes |
|---|---|---|
| `action` | required | receives `InputType` (`dtmf` or `speech`), `Digits`, `Speech`, `SpeechConfidenceScore`, `BilledAmount` |
| `method` | `POST` | |
| `inputType` | none | `dtmf`, `speech`, or `dtmf speech` (the first one detected wins) |
| `redirect` | `true` | |
| `log` | `true` | set `false` for sensitive input |
| `executionTimeout` | `15` | total seconds, 5 to 60 |
| `digitEndTimeout`, `speechEndTimeout` | `auto` | 2 to 10 seconds, or `auto` |
| `startInputTimeout` | none | seconds for the caller to start |
| `retries` | `1` | |
| `numDigits` | `32` | 1 to 32 |
| `finishOnKey` | `#` | |
| `language` | `en-US` | the docs list "common languages include" `en-US`, `en-GB`, `en-AU`, `es-US`, `es-ES`, `fr-FR`, `de-DE`, `it-IT`, `pt-BR`, `ja-JP`, `zh-CN`; test any other code |
| `speechModel` | `default` | or `command_and_search`, `phone_call` |
| `hints` | none | comma-separated phrases; at most 500 phrases, 10,000 characters, 100 per phrase |
| `profanityFilter` | `false` | |
| `interimSpeechResultsCallback` | none | receives `StableSpeech`, `UnstableSpeech`, `Stability`, `SequenceNumber`; its method defaults to `POST` |

### Dial

| Attribute | Default | Notes |
|---|---|---|
| `action` | none | receives `DialStatus` (`completed`, `busy`, `failed`, `cancel`, `timeout`, `no-answer`), `DialRingStatus`, `DialHangupCause`, `DialALegUUID`, `DialBLegUUID` |
| `method` | `POST` | |
| `redirect` | `true` | |
| `timeout` | `120` (hangup-causes 6010; routing: none) | seconds to ring |
| `timeLimit` | `14400` | seconds once connected |
| `callerId` | the caller's | use a number you own |
| `callerName` | the caller's | at most 50 characters |
| `dialMusic` | none | a URL that returns XML, or `real` for the carrier's ringback |
| `confirmSound`, `confirmKey`, `confirmTimeout` | none | an XML URL played to the callee, the key they press to accept, seconds to wait |
| `callbackUrl`, `callbackMethod` | none | live `DialAction` events (`answer`, `connected`, `hangup`, `digits`); no XML expected |
| `hangupOnStar` | `false` | the caller presses `*` to drop the callee |
| `sipHeaders` | none | `key=value,key2=value2` |

`callType` (`voice` or `whatsapp`), `digitsMatch` and `digitsMatchBLeg` are on `voice/xml/routing`.

- `<Number>` holds a phone number. `sendDigits="wwww1234"` sends DTMF after answer (`w` is 0.5 seconds); also `sendDigitsMode="rfc2833"`, `sendOnPreanswer`, `sipHeaders`. Never emit an empty `<Number></Number>` (*observed* to fail the document).
- `<User>` holds a `sip:` URI. `sipAuthUsername` with `sipAuthPassword` (8 to 128 characters) answers a 401 or 407 challenge. Failures reach `action` as `DialHangupCause` `sip_auth_failed` (4240) or `sip_auth_timeout` (4250).
- `sipHeaders` keys starting `PH-`, `Plivo`, `FS-`, `SipAuth`, `ZT-` or `Twilio` (any case), and the name `ClientRegion`, are silently dropped.
- Several `<Number>` children in one `<Dial>` ring at once and the first to answer wins; several `<Dial>` elements ring in turn.

### Record, Redirect, Hangup

- `<Record>`: `action`, `method` `POST`, `redirect` `true`, `fileFormat` `mp3` or `wav`, `timeout` `15` (seconds of silence), `maxLength` `60` (raise it for voicemail), `finishOnKey` `#`, `playBeep` `true`, `recordSession` `false`, `startOnDialAnswer` `false`, `recordChannelType` `stereo` (one party per channel) or `mono`, `callbackUrl`. `action` receives `RecordUrl`, `RecordingID`, `RecordingDuration`, `RecordingDurationMs`, `RecordingStartMs`, `RecordingEndMs`, `Digits`. With `recordSession` or `startOnDialAnswer` the first durations are `-1`; the real values arrive at `callbackUrl`. `RecordUrl` links to the file: download it (Record page: deleted after 30 days; Recordings API: storage billed past 90).
- `<Redirect method="POST">https://...</Redirect>`: the URL receives the standard parameters and must return a document.
- `<Hangup>`: `reason` takes only `rejected` (a rejection tone) or `busy` (a busy signal); `schedule` is in seconds. Do not leave `<Hangup/>` as a placeholder while you build: the call looks like it finished normally. Return a `<Speak>` instead.

### Everything else, in a line each

- `Wait`: `length` (default 1 second), `silence`, `minSilence`, `beep` for beep detection. It is not a documented child of `GetDigits`.
- `DTMF`: `0-9`, `*`, `#`, `w` and `W` pauses; `async` defaults to `true`. To reach an extension after dialling, use `sendDigits` on `<Number>`.
- `Conference`: the room name as text; `maxMembers` 1 to 20 (default 20). Moderated: guests join with `startConferenceOnEnter="false"` and a `waitSound`, the moderator with `startConferenceOnEnter="true" endConferenceOnExit="true"`. A URL `enterSound` or `exitSound` must return XML with `Play`, `Speak` or `Wait`, not an audio file (`beep:1`, `beep:2` are built in). The page's `waitSound` examples point at `.xml` URLs: return XML there too, such as `<Play loop="0">` hold music. Two callers who join the same name are bridged.
- `MultiPartyCall`: the name as text, at most 10 participants, `role` `Customer`, `Agent`, `Supervisor` or `ai-agent`. `coachMode="true"` lets agents, not customers, hear a supervisor. Hold-music URLs return XML. Prefer it to `Conference` when you need roles, coaching, per-participant hold and mute, or API control.
- `Message`: `src` (a number you own), `dst`, `type="sms"`, `callbackUrl`, the text as the body. Several destinations are separated by `<`, which inside the attribute must be written `&lt;`.

## When the call fails

70xx means the HTTP request failed: check the status, method and host, not the XML. 80xx means the body was wrong. Read the status line before the body, and name a code only when the evidence picks it.

| Code | Meaning | Check first |
|---|---|---|
| 7011 (7012 action, 7013 transfer, 7014 redirect URL) | non-2xx or no response | a GET or POST mismatch; a 401 from Basic auth or a token on the URL (Plivo has no documented way to send credentials, so validate the signature instead); a missing route; a slow host. 7012 fails the call only when `redirect="true"` |
| 7022 to 7024, 7032 to 7034 | the action, transfer or redirect URL is not `http(s)`, or its method is not GET or POST | the attribute value |
| 8011 | the answer URL replied, but not with a Plivo document | the number is attached to a console flow application, which answers with JSON you never wrote (*observed*; check this first); a framework JSON or HTML error page; a Twilio element (`<Reject/>` *observed* as 8011; `<Say>`, `<Gather>`, `<Pause>`, `<Parameter>` are not Plivo elements); a raw `&` in an attribute; bytes before `<?xml`; an empty body; an empty `<Number>` from a template; a non-table `<Speak language>` (untested) |
| 8012 (8013 transfer, 8014 redirect) | a later document was bad; 8012 fails the call only when `redirect="true"` | the action document nobody tested: it returns `OK`, JSON, or handles only the happy branch. Run step 2 of the loop on that URL |
| 4010 | the document ran out of elements | nothing, if the call was meant to end |
| 3020 or 3010 with hangup source Answer XML | *observed* for `<Hangup reason="rejected"/>` and `reason="busy"`; the docs describe these codes as the called party rejecting or busy | deliberate screening, or a forgotten placeholder |

**8011 from a console flow application.** Confirm: `plivo numbers get <number> -o json` (the `application` URI ends in the app id), `plivo account applications get <app_id> -o json` (its `answer_url`), then the step 2 `curl` to it: JSON back is the cause. Fix: attach an XML application.

```bash
plivo account applications create --app-name <name> --answer-url https://YOUR-HOST/plivo/answer --answer-method POST --fallback-answer-url https://YOUR-HOST/plivo/fallback --dry-run
plivo numbers update <number> --app-id <new_app_id> --dry-run
```

Neither prompts, so drop `--dry-run` only after the user agrees; the old app id is the rollback. Making the flow itself answer is out of scope.

The call's debug log in the console (Voice, Logs, Calls, the call) shows Plivo's parser message with a line and column, for example `not well-formed (invalid token): line 1, column 271`. `plivo voice calls diagnose <call_uuid>` reads it for you.

Do not port Twilio XML as is. The Plivo forms are `<Speak>`, `<GetInput>`, `<Hangup reason="rejected"/>` and `<Wait>`. Build documents with an XML library or the Plivo SDK's XML classes so escaping is never done by hand.

## Three patterns

### Keypad menu

```xml
<Response>
  <GetDigits action="https://example.com/plivo/menu" method="POST" numDigits="1" timeout="10" retries="2" validDigits="12">
    <Speak>For sales press 1. For support press 2.</Speak>
  </GetDigits>
  <Speak>We did not get a choice. Goodbye.</Speak>
  <Hangup/>
</Response>
```

- The prompt sits inside `GetDigits`, so a key press interrupts it.
- The two elements after it are the no-input path. Without them the call ends in silence.
- The `action` document replaces this one, because `redirect` defaults to `true`. Handle every `Digits` value plus an unexpected one. To repeat the menu, return a `<Redirect>` to the menu URL with an exit, such as a counter in the query string.
- For speech as well, use the same shape with `<GetInput inputType="dtmf speech" hints="sales, support">` and branch on `InputType`.

### Forward, then voicemail

```xml
<Response>
  <Dial callerId="+10000000000" timeout="20" redirect="false" action="https://example.com/plivo/dial-result" method="POST">
    <Number>+10000000001</Number>
  </Dial>
  <Speak>Nobody is available. Leave a message after the beep, then press hash.</Speak>
  <Record action="https://example.com/plivo/voicemail" method="POST" redirect="false" maxLength="120" timeout="10" finishOnKey="#"/>
  <Speak>Thank you. Goodbye.</Speak>
  <Hangup/>
</Response>
```

- `redirect="false"` on `Dial` lets an unanswered call fall through to the voicemail prompt; on `Record` it keeps the thank-you below it. Both `action` URLs are still called (log `DialStatus`, store `RecordUrl`) and their bodies are ignored.
- If the prompt must never follow a completed call, keep `redirect` at its default and have the dial `action` return the voicemail document only when `DialStatus` is not `completed`.

### Record the whole call

```xml
<Response>
  <Record recordSession="true" startOnDialAnswer="true" maxLength="3600" callbackUrl="https://example.com/plivo/recording" callbackMethod="POST"/>
  <Speak>This call is recorded.</Speak>
  <Dial callerId="+10000000000" timeout="25" action="https://example.com/plivo/dial-result" method="POST">
    <Number>+10000000001</Number>
  </Dial>
</Response>
```

- `Record` comes first and runs in the background until the call ends. `startOnDialAnswer` skips the ringing, and the default `stereo` puts each party on its own channel.
- Take `RecordUrl` and the real durations from `callbackUrl`.
- Whether and how to tell the caller is the user's legal question; this skill states platform behaviour only.

## Securing the URLs

- Validate `X-Plivo-Signature-V3`, with the nonce from `X-Plivo-Signature-V3-Nonce`, using the SDK helper: `plivo.utils.validate_v3_signature` (Python), `plivo.validateV3Signature` (Node), `Plivo::Utils.valid_signatureV3?` (Ruby). A hand-built string is easy to get subtly wrong.
- Sign the URL Plivo actually requested (scheme, host, port, path and query), not the one a proxy or load balancer forwards to your app.
- `X-Plivo-Signature-V3` uses the Auth Token of the account or subaccount that owns the request entity, such as the number; `X-Plivo-Signature-Ma-V3` always uses the main account's. With several active tokens Plivo sends comma-separated signatures: accept a match on any of them.
- Prefer this to a secret in the URL. Basic auth or a bearer token on the URL makes Plivo's request fail with a 401, which is a 7011.
- The worked example string on the docs page writes `CallUuid` and adds a `Caller` parameter; it is not a test vector. Full recipe: `plivo docs show voice/concepts/signature-validation`.

## Where the docs disagree

- **`Speak voice`.** The audio-output attribute table allows only `WOMAN` and `MAN`; the same page's SSML section and the SSML page use `Polly.<Name>`. Follow the SSML pages.
- **Conference `stayAlone`.** The current row reads "End conference if only one member" with default `true`. Earlier versions of the page said `false` ends the conference when a member is alone (and once any member joins with `false`, it stays `false`), and the page's own two-caller bridge needs a lone first caller to stay. Follow that: the default `true` keeps a lone member in the room. MultiPartyCall's `stayAlone` is a different setting with a different default: `false`, described as "Stay if only participant".
- **MultiPartyCall role casing.** The roles table writes `Customer`, `Agent`, `Supervisor` and `ai-agent`; the page's first example writes `role="customer"`. Use the table's casing.
- **Timeouts and fallback.** The XML overview says Plivo waits 15 seconds for XML; the Calls API says `fallback_url` is used after 3 retries or a 60 second timeout; the callback-configurations page gives defaults of `rt=40000` ms, `rc=1`, `tt=55000` ms and `rp=ct,rt` (retry only on a connection failure or read timeout). Answer well inside 15 seconds, and set the fragment explicitly when failover timing matters.
- **Examples that break the rules above.** The routing page's sequential-dial example sets `action` with the default `redirect` and still expects the next `<Dial>` and `<Speak>` to run, and its custom-ringback example puts `<Play loop="0">` inside `<PreAnswer>` before a `<Dial>`. Set `redirect="false"` and a finite `loop` when you copy them.
