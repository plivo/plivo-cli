---
name: plivo-voice-xml
description: Write and fix Plivo Voice XML, the document your answer URL returns to control a phone call. Load this for "Plivo XML", "answer URL", "action URL", IVR and keypad menus, Speak and Play prompts, SSML and voices, GetDigits and GetInput, DTMF, Dial with Number or User, call forwarding and simultaneous or sequential ringing, Redirect, Wait, PreAnswer, Hangup and call screening, Record and voicemail, Conference rooms, MultiPartyCall roles, sending an SMS from a call flow, element ordering and nesting rules, callback and action URL parameters, X-Plivo-Signature-V3 validation, webhook timeouts and retries, and the XML errors 8011, 8012, 8013 and 8014. Element and behaviour reference plus ready patterns. For WebSocket voice bots use plivo-audio-streaming; for SIP platforms use plivo-sip-trunking.
license: Apache-2.0
---

# Plivo Voice XML

You are helping a developer, or their coding agent, write the XML document their answer URL returns, and understand what Plivo will do with it. Answer with a document plus the rule behind it. Speak plainly. Every rule here carries a docs page; rules marked "commonly seen" come from practice and have no docs page.

What is in this file, in order: the contract, the questions to ask, the element list, the ordering and nesting rules, the patterns, the URL contract, the failure codes, what not to do, then the deep sections (the full element reference, every pattern with a document you can copy, the URL contract in detail, and the shapes that break a call).

**Testing what you write.** The Plivo CLI places one call against your XML. Preview it first, because a call costs money and dials a real phone:

```bash
plivo voice calls make --from <number> --to <your phone> \
  --answer-url https://YOUR-HOST/plivo/answer --answer-method POST --dry-run   # prints the request, sends nothing
plivo voice calls make --from <number> --to <your phone> \
  --answer-url https://YOUR-HOST/plivo/answer --answer-method POST --yes       # only after the user agrees
plivo voice calls diagnose <call_uuid>                                          # what actually happened, including the XML errors below
```

`--answer-method` defaults to `GET`. Set `POST` explicitly, or Plivo will fetch a route your handler may not serve and you will debug the wrong thing.

**What this file assumes you have: nothing but this file and the `plivo` CLI.** Every element, attribute, ordering rule, URL contract and failure shape you need to write and fix Plivo XML is here. Other Plivo skills are separate single files you may not have. Install one only if the task moves outside XML:

- `npx skills add https://www.plivo.com/docs --skill plivo-audio-streaming` for the WebSocket voice bot journey: `<Stream>`'s own attributes, the WebSocket protocol, streaming readiness and debugging a failed agent call.
- `npx skills add https://www.plivo.com/docs --skill plivo-sip-trunking` for SIP trunks and agent platforms reached over SIP, which have no answer URL and no XML at all.
- `plivo skill install` for `plivo-cli`, the CLI's own reference. The CLI writes that file out itself.

If none is installed, do not stall: use `plivo <command> --help` and the public docs, and say which source you used.

## What a finding licenses you to say

Three tiers, and they decide your verdict.

- **Will break.** A documented rule says so, or a call with this shape is known to have failed. Say the call breaks, and name the failure.
- **Risky.** Plausible, unverified, or seen to work in some deployments. Say what could go wrong and what to check. Name no hangup code.
- **Style.** No functional effect. Say so.

Nothing outside the first tier is a reason to tell someone their call will break. A true observation about a document is not a verdict on its own: a design risk you would raise in review is still a document that connects a call. Rules below carry a docs source when they are in the first tier; a rule marked "commonly seen", "observed" or "not documented" is risky.

## The contract in six lines

1. Plivo requests your answer URL and expects one XML document back.
2. Content-Type must be `application/xml` or `text/xml`, the body must be valid XML, and it must be under 100 KB.
3. Answer well inside 15 seconds.
4. The root element is `<Response>`. Children run top to bottom, one at a time.
5. An empty `<Response>` hangs the call up. So does running out of elements.
6. A body that is not a Plivo XML document is `8011 Invalid Answer XML`. No usable HTTP response at all is `7011`, a different code with a different fix.

Sources: <https://www.plivo.com/docs/voice/xml/overview>, <https://www.plivo.com/docs/voice/xml/routing>, <https://www.plivo.com/docs/voice/troubleshooting/hangup-causes>.

## Ask before you write

1. Inbound (someone calls your Plivo number) or outbound (you placed the call through the API)? Both fetch the same kind of document, but the parameters differ.
2. What should the caller hear first: text to speech, a recorded file, or nothing?
3. Does the caller choose something? Keypad only, speech, or either?
4. Where does the call go: another number, a SIP address, a room, voicemail, or nowhere?
5. Should the call be recorded, and does the caller need to be told?
6. What ends the call, and what should the caller hear before it does?

Then copy the closest document from "Patterns and the documents to copy" below and change the URLs and text. Do not invent attributes: if it is not in the element reference below, it is not documented.

## The elements

| Element | Does |
|---|---|
| `<Speak>` | Text to speech. `voice`, `language`, `loop`. SSML with a `Polly.` voice |
| `<Play>` | Play an audio file from an HTTPS URL. `loop` |
| `<DTMF>` | Send tones on the current call. `async` |
| `<GetDigits>` | Collect keypad digits, post them to `action` |
| `<GetInput>` | Collect speech or digits, post them to `action`. Preferred for new work |
| `<Dial>` | Connect the call to `<Number>` or `<User>` |
| `<Redirect>` | Hand control to another URL of yours |
| `<Hangup>` | End the call, optionally with a `reason` or a `schedule` |
| `<Wait>` | Pause. Also beep and silence detection |
| `<PreAnswer>` | Play media before the call is answered |
| `<Record>` | Record a message, or the whole session in the background |
| `<Conference>` | Join a named room, up to 20 people |
| `<MultiPartyCall>` | Join a room with roles, coaching and per participant control |
| `<Message>` | Send an SMS from inside the call flow |
| `<Stream>` | Open a WebSocket audio stream. Its own attributes and the WebSocket protocol are in `plivo-audio-streaming`, a separate install |

Pages: <https://www.plivo.com/docs/voice/xml/audio-output> (Speak, Play, DTMF), <https://www.plivo.com/docs/voice/xml/input> (GetDigits, GetInput), <https://www.plivo.com/docs/voice/xml/routing> (Dial, Redirect, Hangup, Wait, PreAnswer), <https://www.plivo.com/docs/voice/xml/record>, <https://www.plivo.com/docs/voice/api/conferences>, <https://www.plivo.com/docs/voice/xml/multiparty-call>, <https://www.plivo.com/docs/messaging/xml/message>, <https://www.plivo.com/docs/voice/xml/audio-streaming>.

Full attribute tables, defaults and allowed values are in "Element reference" below.

## Ordering and nesting, the part people get wrong

These are the parent and child pairs the docs list. The nesting section is headed "Some elements can be nested inside others" and never says the list is exhaustive or that other nesting is rejected, so treat undocumented nesting as untested and move the element out rather than claiming Plivo rejects the document (<https://www.plivo.com/docs/voice/xml/overview>, <https://www.plivo.com/docs/voice/xml/routing>):

| Parent | Allowed children |
|---|---|
| `Response` | any element |
| `GetDigits` | `Speak`, `Play` |
| `GetInput` | `Speak`, `Play` |
| `Dial` | `Number`, `User` |
| `PreAnswer` | `Speak`, `Play`, `Wait` |

Six ordering rules:

- **`redirect` decides who owns the rest of the call, and this is a risk to raise rather than a verdict.** On `GetDigits`, `GetInput`, `Record`, `Dial` and `Conference`, `redirect` defaults to `true`, so when the `action` URL answers, Plivo runs the XML it returns. Those are default rows, not a documented statement that the elements below are discarded, and the docs' own sequential dialling example leaves `redirect` at its default on two `<Dial>` elements and still expects the trailing `<Speak>` to run. Say the content below may be skipped for a caller who responds; do not say the call breaks. With `redirect="false"` the `action` URL is still called, its answer is ignored, and the next element in your document runs. This is also why a bad action document only fails the call when `redirect` is `true`.
- **Put a fallback after every element that can produce nothing.** After `GetDigits` or `GetInput` with no input, and after a `Dial` that nobody answered, execution falls through to the next element. If there is no next element the call ends silently.
- **`<Record recordSession="true"/>` goes before the thing you want recorded.** It starts immediately, runs in the background until the call ends, and ignores `timeout`, `finishOnKey` and `playBeep`. To capture both parties on a transfer, use `startOnDialAnswer="true"` and place it before `<Dial>`.
- **`<Redirect>` is the end of the document.** Nothing written after it runs, because control has moved to the new URL.
- **`<Hangup/>` is the end of the call, unless it is scheduled.** `<Hangup schedule="60"/>` sets a timer and lets the following elements keep running.
- **Put `<PreAnswer>` first.** It plays before the call is answered, so anything that answers the call should not precede it. The page gives three limitations and no ordering rule: only `Speak`, `Play` and `Wait` go inside; the call is not answered during PreAnswer, so some carriers may time out; keep it under 30 seconds. The ordering itself is this file's inference, so treat a `<PreAnswer>` that is not first as a risk to raise, not a rejected document.

## Patterns

Every one of these has a document you can copy in "Patterns and the documents to copy" below, with the rule behind the choice.

| You want | Shape |
|---|---|
| Keypad menu | `GetDigits` wrapping the prompt, then a fallback `Speak` and `Hangup` |
| Speech or keypad menu | `GetInput inputType="dtmf speech"` with `hints` |
| Forward a call | `Dial` with one `Number`, `callerId` set |
| Ring several people at once | one `Dial`, several `Number` children |
| Try people in turn | several `Dial` elements, each with a `timeout` |
| Dial a SIP address | `Dial` with one `User`, `sipAuthUsername` and `sipAuthPassword` |
| Forward, then voicemail | `Dial` then `Redirect`, or `Dial` then `Record` |
| Take a voicemail | `Speak`, then `Record` with `maxLength` and `finishOnKey` |
| Record a whole conversation | `Record recordSession="true"` before `Dial` |
| Conference room | `startConferenceOnEnter` false for guests, true for the host |
| Contact centre room with roles | `MultiPartyCall role="Agent"` |
| Reject a caller on purpose | `Hangup reason="rejected"` |
| Say you are closed, then stop | `Speak` then `Hangup` |
| Acknowledge a callback | `<Response></Response>` returned to a `callbackUrl` or hangup URL |
| Custom ringback | `PreAnswer` with `Play`, then `Dial` |
| SSML prompt | `Speak voice="Polly.Joanna"` with `prosody` and `break` |
| Text the caller a link | `Speak` then `Message` |

## URLs and callbacks

Two kinds of URL come out of an XML document, and mixing them up is a common bug (<https://www.plivo.com/docs/voice/concepts/callbacks>):

- **`action`** expects XML back. Plivo runs it. If it is unreachable you get `7012`, and if it is not XML you get `8012`.
- **`callbackUrl`** expects nothing back. It is a notification. Return `200` with an empty body or an empty `<Response></Response>`.

Set a **Fallback Answer URL** on the application, or `fallback_url` on the API call, so a failing primary URL does not kill every call. Plivo retries webhooks and can deliver the same callback twice, so make handlers idempotent on `CallUUID`. Validate `X-Plivo-Signature-V3` rather than putting a token in the URL (<https://www.plivo.com/docs/voice/concepts/signature-validation>). Timeouts, retry counts and retry policy are set with URL fragments such as `#ct=2000&rc=3&rp=ct,rt` (<https://www.plivo.com/docs/voice/concepts/callback-configurations>). Full parameter lists per element are in "The URL contract" below.


## Two cautions before you name a code

**Do not name a hangup code the evidence does not pick.** 7011 and 8011 are different failures: 7011 means Plivo never got a usable HTTP response, and 8011 means it got one and the body was not a Plivo XML document. A non-XML body you can see in a `curl` is an 8011 shape, but if the same request also returned a non-2xx status it is a 7011 and the body is beside the point. Read the status line first, then the body, and name a code only when the evidence picks one.

**Do not promise what an undocumented input will do.** The docs describe valid documents; they do not say what Plivo does with an invalid one beyond reporting 8011. A raw `&` inside an attribute value is invalid XML and you should fix it, but do not tell a customer it is definitely why their call failed, and do not tell them it is safe either. Report it as a defect to fix, then find the evidence for the actual failure.

## When the call fails

| Code | Name | What it means | First move |
|---|---|---|---|
| 8011 | Invalid Answer XML | The answer URL replied, but not with a Plivo XML document | Print the exact bytes your server sent, then read "What breaks, and how to tell which" |
| 8012 | Invalid Action XML | An `action` document was bad. Only fails the call when `redirect="true"` | Check the second document, not the first |
| 8013 / 8014 | Invalid Transfer / Redirect XML | Same problem on a transfer or redirect URL | Same |
| 7011 | Error Reaching Answer URL | A non 2xx response from the answer URL, or no response at all: 404, 401, 405, timeout, dead host | Check status code and method, not the XML. A 200 with an empty body is 8011, not this |
| 7012 / 7013 / 7014 | Error Reaching Action / Transfer / Redirect URL | Same, on the later URL | Same |
| 4010 | End Of XML Instructions | The document ran out. Normal | Nothing, if the call was meant to end |
| 3020 / 3010 with hangup source Answer XML | Rejected / Busy Line | Observed pairing, not a documented one. The routing page documents only the audible effect (a rejection tone, a busy signal) and the hangup table describes 3020 and 3010 as coming from the called party. Call records show these two codes with source Answer XML after a `<Hangup reason=.../>` | Intended screening, or a placeholder someone forgot |

Codes: <https://www.plivo.com/docs/voice/troubleshooting/hangup-causes>.

Three different empties get confused, so keep them apart. **No usable HTTP answer** (a non 2xx, a timeout, a dead host) is 7011: the hangup table defines it as a non 2xx response from the answer URL, so there was nothing to parse. **A 200 whose body is not Plivo XML** is 8011: JSON, HTML, plain text, or a zero length body, which the XML overview lists as "Empty response" among the causes of invalid XML. **An empty `<Response></Response>`** is neither: it is valid XML that runs and immediately ends the call.

The nine shapes that produce 8011 and 8012, each with the fix, are in "What breaks, and how to tell which" below. The short list: a non XML body, a JSON error page from your framework or from object storage answering a POST, plain text, leading bytes before the XML declaration, an unescaped `&` in a URL attribute, a Twilio element such as `<Reject/>` or `<Say>`, a `<Number>` or attribute left empty by a template, an unsupported `<Speak language>`, and a second document that was never tested because only the first one was. Free text in `Hangup reason` and a truncated document are commonly seen and are worth fixing, but no page names an outcome for either.

## What not to do

- Do not copy Twilio XML. `<Say>`, `<Gather>`, `<Reject>`, `<Pause>` and `<Parameter>` are not Plivo elements. The nearest Plivo forms are `<Speak>`, `<GetInput>`, `<Hangup reason="rejected"/>` and `<Wait>` (<https://www.plivo.com/docs/voice/xml/overview>).
- Do not put a raw `&` inside an attribute value. Write `&amp;`. A query string with two parameters is the usual way this breaks (docs, plus commonly seen).
- Do not return `<Hangup/>` as a placeholder while you build. It ends the call gracefully the moment it runs, so the call answers and stops and the record looks like a document that finished normally, which you cannot tell apart from a real fault later. Return `<Speak>` instead. `<Hangup reason="rejected"/>` and `<Hangup reason="busy"/>` are the forms that give the caller a rejection or busy signal, and they show as 3020 and 3010.
- Do not point a number at a console flow application and then serve your own XML from somewhere else. The number uses whatever application it is attached to (commonly seen).
- Do not use a `language` or `voice` that is not in the tables. `voice` is `WOMAN` or `MAN`, or a `Polly.` name; SSML only works with `Polly.` voices (<https://www.plivo.com/docs/voice/xml/audio-output>, <https://www.plivo.com/docs/voice/concepts/ssml>).
- Do not leave `log="true"` on a `GetDigits` or `GetInput` that collects a PIN or a card number.
- Do not build a `Redirect` loop with no exit. Every branch must reach a document that does not redirect.
- Do not assume `action` and `callbackUrl` behave the same. Only `action` expects XML.
- Do not host prompt audio on a slow or plain HTTP host. `<Play>` needs HTTPS, mp3 or wav, under 10 MB.

## When this skill does not have the answer

Do not guess, and do not fill the gap from general knowledge of other platforms. In order:

1. **Read the current documentation.** Every page on <https://www.plivo.com/docs> is available as Markdown by adding `.md` to its URL, and <https://www.plivo.com/docs/llms.txt> lists every page. Start at <https://www.plivo.com/docs/voice/xml/overview> and the element page for whatever you are writing. From a terminal `plivo docs search <keywords>` searches the full text of every page, `plivo docs list` prints the index and `plivo docs show <path-or-title>` prints one page; those three need no credentials and are not rate limited, so reach for them before the assistant.
2. **Ask Plivo's assistant from the terminal**: `plivo ask "<your question>"`. It reads the documentation and can see the account, so it answers things this file cannot: what a specific call did, whether a compliance application is accepted, what a destination costs. It is limited to five requests per ten minutes per account, so save it for the question you cannot answer another way. `plivo voice calls diagnose <call_uuid>` is the same assistant pointed at one call, and it shares that limit, so do not loop either.
3. **If you have no CLI access**, tell the person you are working with to ask the same question to the assistant in the Plivo console.

Treat the answer as evidence, not as final. If it contradicts the documentation, say that it does and prefer the documentation for published behaviour. If it gives a number the documentation does not publish, repeat it as something the assistant said, not as a documented fact.

Never invent an element, an attribute, an allowed value or a hangup code. Where Plivo's own pages disagree, the element reference below names both readings and which one this file follows.

## CANNOT

- **Cannot see your server.** A document that looks right here can still fail with 7011 because of a status code, an auth check or a dead host. Ask for the exact bytes and the exact HTTP status.
- **Cannot confirm a call worked.** Well formed XML is not a working call. Only the call record and someone who heard the audio can say that.
- **Cannot decide legal questions.** Recording notices, consent, retention and calling hours need the user's own legal review. This skill states the platform behaviour only.
- **Cannot cover the voice agent journey.** Readiness, WebSocket protocol, streaming debugging and go live belong to `plivo-audio-streaming`, and SIP trunks and agent platforms to `plivo-sip-trunking`. Both are separate installs (`npx skills add https://www.plivo.com/docs --skill <name>`); if the user does not have one, say so and point at the public docs section rather than improvising the answer.
- **Cannot cover the rest of the platform.** Number provisioning, compliance and KYC, the Voice API beyond the URLs named here, the Browser SDK, messaging beyond `<Message>`, and the console flow application builder are all out of scope.

## Element reference: every documented attribute

Every attribute below is documented on a Plivo docs page, named under each element. Nothing here is inferred. Defaults are Plivo's documented defaults, not recommendations. `<Stream>` is deliberately not covered here; install `plivo-audio-streaming` for it.

Attribute names are case sensitive and camelCase. Element names are case sensitive too: `<speak>` is not `<Speak>`.

Two warnings about completeness. First, some deployments carry attributes on `Dial`, `Conference`, `MultiPartyCall`, `User`, `GetInput` and `Wait` that are not in any of the tables below. Some of them may work, but none of them is documented, so this file does not list them and you should not recommend one. If a deployment already uses one, say that it is undocumented and ask Plivo support before relying on it. Second, what Plivo does with an attribute or element it does not recognise is not documented, so "the call still worked" is not evidence that an attribute is real, and neither is a failure proof that the attribute caused it. Remove anything undocumented rather than reasoning about how it is handled.

### Response

Root element. Children run in order, one at a time.

An empty `<Response></Response>` ends the call when it runs. Returned to a `callbackUrl` or a hangup URL it is simply an acknowledgement, which is fine and common.

Nesting rules for the whole language are in "Ordering and nesting" above. No element other than `Response`, `GetDigits`, `GetInput`, `Dial` and `PreAnswer` takes children. `MultiPartyCall`, `Conference`, `Redirect`, `Play`, `DTMF` and `Message` carry their value as element text.

### Speak

Text to speech. Text goes in the element body. Docs: <https://www.plivo.com/docs/voice/xml/audio-output>.

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `voice` | string | `WOMAN` | `WOMAN` or `MAN` |
| `language` | string | `en-US` | see the language table below |
| `loop` | integer | `1` | number of repeats. `0` repeats until the call ends |

**Languages for the `WOMAN` and `MAN` voices**: `da-DK`, `nl-NL`, `en-AU`, `en-GB`, `en-US`, `fr-FR`, `fr-CA`, `de-DE`, `it-IT`, `pl-PL`, `pt-PT`, `pt-BR`, `ru-RU`, `es-ES`, `es-US`, `sv-SE`. Not every language has both a woman and a man; the docs table says which. This is the list the documentation prints. Treat a language outside it as unverified: it may work, but test it on a real call before you rely on it, and tell the user it is untested rather than either promising it or claiming Plivo rejects it. For `en-IN` and `hi-IN` the documented route is a `Polly.` voice that covers them.

**Polly voices.** Set `voice="Polly.<Name>"` to use an Amazon Polly voice. Polly covers 27 languages and more than 40 voices, including `hi-IN` (`Polly.Aditi`) and `en-IN` (`Polly.Raveena`, `Polly.Aditi`) which the table above does not list. Full voice list: <https://www.plivo.com/docs/voice/concepts/ssml>. Polly voices must carry the `Polly.` prefix.

**Docs disagreement.** The audio output page says the allowed values are `WOMAN` and `MAN`. The same page's SSML section and the SSML concept page both use `Polly.` names in the same attribute. This file follows the SSML pages: `Polly.` names are valid in `voice`, and they are required for SSML.

**SSML.** Only works with a Polly voice. Maximum 3,000 characters per `<Speak>`. Supported tags include `<break>`, `<say-as>`, `<prosody>`, `<emphasis>`, `<p>`, `<s>`. Plivo does not support `<amazon:effect>` or `<amazon:auto-breaths>`.

**Nesting.** `<Speak>` may sit inside `<GetDigits>`, `<GetInput>` and `<PreAnswer>`.

### Play

Plays an audio file. The URL goes in the element body. Docs: <https://www.plivo.com/docs/voice/xml/audio-output>.

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `loop` | integer | `1` | `0` loops until the call ends |

File rules: mp3 or wav, served over HTTPS, maximum 10 MB, 8 kHz or 16 kHz mono recommended.

The body must be a URL. Putting a sentence inside `<Play>` does not speak it; use `<Speak>` (commonly seen mistake).

**Nesting.** `<Play>` may sit inside `<GetDigits>`, `<GetInput>` and `<PreAnswer>`.

### DTMF

Sends DTMF tones on the current call. The digits go in the element body. Docs: <https://www.plivo.com/docs/voice/xml/audio-output>.

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `async` | boolean | `true` | `true` starts the next element while tones are still being sent; `false` waits |

Allowed characters: `0-9`, `*`, `#`, `w` (wait 0.5 s), `W` (wait 1 s).

When you are dialling out and want to enter an extension, the docs recommend `sendDigits` on `<Number>` instead of `<DTMF>`.

### GetDigits

Collects keypad digits and posts them to `action`. The docs recommend `<GetInput>` for new applications. Docs: <https://www.plivo.com/docs/voice/xml/input>.

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `action` | URL | none | where the digits are posted |
| `method` | string | `POST` | `GET` or `POST` |
| `numDigits` | integer | `99` | maximum digits to collect |
| `timeout` | integer | `5` | seconds to wait for the first digit |
| `digitTimeout` | integer | `2` | seconds between digits |
| `finishOnKey` | string | `#` | a digit, `#`, `*`, or `none` |
| `retries` | integer | `1` | attempts when no input arrives |
| `redirect` | boolean | `true` | `true` means the action document takes over the call |
| `playBeep` | boolean | `false` | beep after the prompts, before collecting |
| `validDigits` | string | `1234567890*#` | digits the caller may press |
| `invalidDigitsSound` | URL | none | audio played on an invalid digit |
| `log` | boolean | `true` | set `false` for PINs and card numbers |

**Children:** `<Speak>` and `<Play>` only. The prompt plays while Plivo waits, and collection starts as soon as the first digit is pressed.

**Flow:** prompts play, optional beep, digits are collected until `numDigits`, `finishOnKey` or a timeout, digits are posted to `action`, and the action document runs. After `retries` attempts with no input, execution falls through to the next element in your document.

**Action parameters:** `Digits`, which excludes the `finishOnKey` character, plus all the standard request parameters.

### GetInput

Collects speech, digits, or either, and posts the result to `action`. Docs: <https://www.plivo.com/docs/voice/xml/input>.

Core:

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `action` | URL | required | where the result is posted |
| `method` | string | `POST` | `GET` or `POST` |
| `inputType` | string | none | `dtmf`, `speech`, or `dtmf speech` |
| `redirect` | boolean | `true` | `true` means the action document takes over the call |
| `log` | boolean | `true` | set `false` for sensitive input |

Timing:

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `executionTimeout` | integer | `15` | total seconds, 5 to 60 |
| `digitEndTimeout` | string | `auto` | seconds between digits, 2 to 10, or `auto` |
| `speechEndTimeout` | string | `auto` | seconds of silence that end speech, 2 to 10, or `auto` |
| `startInputTimeout` | integer | none | seconds to wait for the caller to start |
| `retries` | integer | `1` | attempts when no valid input arrives |

DTMF: `numDigits` default `32`, range 1 to 32; `finishOnKey` default `#`.

Speech: `language` default `en-US`; `speechModel` default `default`, also `command_and_search` and `phone_call`; `hints`, a comma separated list of phrases; `profanityFilter` default `false`.

Callbacks: `interimSpeechResultsCallback` and `interimSpeechResultsCallbackMethod` (default `POST`).

**Children:** `<Speak>` and `<Play>` only.

**Hints limits:** 500 phrases per request, 10,000 characters in total, 100 characters per phrase.

**Documented speech languages**, listed in the docs as "common languages include": `en-US`, `en-GB`, `en-AU`, `es-US`, `es-ES`, `fr-FR`, `de-DE`, `it-IT`, `pt-BR`, `ja-JP`, `zh-CN`. The list is explicitly not exhaustive, so a code that is not on it may or may not work. Test it before relying on it. What Plivo does with a code the recogniser does not accept is not documented, so treat an unlisted code as untested rather than as known to be rejected.

**Action parameters:** `InputType` (`dtmf` or `speech`), `Digits`, `Speech`, `SpeechConfidenceScore`, `BilledAmount`, plus the standard request parameters.

**Interim callback parameters:** `StableSpeech`, `UnstableSpeech`, `Stability`, `SequenceNumber`.

Speech recognition is billed per 15 second increment.

### Dial

Connects the current call to another party. Must contain at least one `<Number>` or `<User>`. Docs: <https://www.plivo.com/docs/voice/xml/routing>.

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `action` | URL | none | receives the dial result |
| `method` | string | `POST` | `GET` or `POST` |
| `timeout` | integer | none | seconds to wait for an answer |
| `timeLimit` | integer | `14400` | maximum seconds once connected |
| `callerId` | string | the caller's own id | number shown to the person you dial |
| `callerName` | string | the caller's name | maximum 50 characters |
| `hangupOnStar` | boolean | `false` | caller presses `*` to drop the other leg |
| `redirect` | boolean | `true` | `true` means the action document takes over |
| `callType` | string | `voice` | `voice` or `whatsapp`. WhatsApp cannot reach the phone network and does not support machine detection |
| `callbackUrl` | URL | none | live dial events, no XML expected back |
| `callbackMethod` | string | none | `GET` or `POST` |
| `confirmSound` | URL | none | returns XML played to the person you dialled |
| `confirmKey` | string | none | key they must press to accept |
| `confirmTimeout` | integer | none | seconds to wait for that key |
| `dialMusic` | URL or `real` | none | ringback. `real` plays the carrier's own ringing |
| `digitsMatch` | string | none | DTMF patterns to report from the caller side |
| `digitsMatchBLeg` | string | none | DTMF patterns to report from the dialled side |
| `sipHeaders` | string | none | `key=value,key2=value2` |

#### Number

Dials a phone number. The number goes in the element body.

| Attribute | Default | Notes |
|---|---|---|
| `sendDigits` | none | DTMF sent after answer. `w` is a 0.5 s pause |
| `sendDigitsMode` | none | `rfc2833` sends telephone events instead of inband tones |
| `sendOnPreanswer` | `false` | send the digits during early media |
| `sipHeaders` | none | headers for this number only |

An empty `<Number></Number>` is not a valid target and fails the document (commonly seen). Leave the element out rather than emitting it empty.

#### User

Dials a SIP address. The `sip:` URI goes in the element body.

| Attribute | Notes |
|---|---|
| `sipHeaders` | headers for this user only |
| `sipAuthUsername` | SIP digest username, for endpoints that challenge with 401 or 407 |
| `sipAuthPassword` | 8 to 128 characters, required when the username is set |

Attribute names are camelCase: `sipAuthUsername`, not `sip_auth_username`.

**Reserved `sipHeaders` prefixes**, silently dropped, case insensitive: `PH-`, `Plivo`, `FS-`, `SipAuth`, `ZT-`, `Twilio`, and the exact name `ClientRegion`.

**SIP auth failures** appear on the `action` URL as `DialHangupCause` `sip_auth_failed` (code 4240, `DialStatus` `failed`) or `sip_auth_timeout` (code 4250, `DialStatus` `timeout`).

#### Dialling several people

- **At once:** several `<Number>` children inside one `<Dial>`. The first to answer is connected.
- **In turn:** several `<Dial>` elements one after another, each with its own `timeout`.

#### Dial action and callback parameters

Action URL, sent when the dial finishes: `DialStatus` (`completed`, `busy`, `failed`, `cancel`, `timeout`, `no-answer`), `DialRingStatus`, `DialHangupCause`, `DialALegUUID`, `DialBLegUUID`.

Callback URL, live events with no XML expected back: `DialAction` (`answer`, `connected`, `hangup`, `digits`), `DialBLegStatus`, `DialALegUUID`, `DialBLegUUID`, `DialBLegDuration`, `DialBLegBillDuration`, `DialBLegFrom`, `DialBLegTo`, `DialDigitsMatch`, `DialDigitsPressedBy` (`ALeg` or `BLeg`), `DialBLegHangupCauseName`, `DialBLegHangupCauseCode`, `DialBLegHangupSource`, `STIRVerification`.

### Redirect

Hands control to another URL of yours. The URL goes in the element body. Docs: <https://www.plivo.com/docs/voice/xml/routing>.

| Attribute | Type | Default |
|---|---|---|
| `method` | string | `POST` |

The redirect URL receives the standard request parameters. The page says `<Redirect>` transfers call execution to a different URL and Plivo continues the call there; it does not separately say that siblings below it are skipped, so treat anything after a `<Redirect>` as dead code to move rather than a documented failure. Every branch must eventually reach a document that does not redirect.

### Hangup

Ends the call. Docs: <https://www.plivo.com/docs/voice/xml/routing>.

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `reason` | string | none | `rejected` gives the caller a rejection tone, `busy` gives a busy signal |
| `schedule` | integer | none | seconds to wait. Following elements keep running meanwhile |

`reason` takes only those two documented values. Free text there is not a documented use and should not be relied on (commonly seen, undocumented).

If your document does not end with `<Hangup>`, the call ends anyway once every element has run.

### Wait

Pauses execution. Docs: <https://www.plivo.com/docs/voice/xml/routing>.

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `length` | integer | `1` | seconds to wait |
| `silence` | boolean | `false` | `true` plays silence instead of hold music |
| `minSilence` | integer | none | milliseconds of silence to detect |
| `beep` | string | none | `true`, or beep parameters |

Beep parameters are a comma separated string, for example `beep="duration=300,inter_silence=50,intra_silence=500,threshold=256"`, with those values as defaults.

`<Wait>` is not a documented child of `<GetDigits>`. Only `<Speak>` and `<Play>` are. Not documented is not the same as rejected. Say "this nesting is not documented, so it may be ignored rather than honoured; move it outside the element to be safe", and do not claim Plivo rejects the document unless a documented error says so. The same wording applies to every other nesting claim below.

### PreAnswer

Plays media before the call is answered, so the caller is not billed for it. Docs: <https://www.plivo.com/docs/voice/xml/routing>.

Children: `<Speak>`, `<Play>`, `<Wait>` only.

Limits: only those three elements are allowed; the call is not answered during this phase, so some carriers time out; keep it under 30 seconds.

### Record

Records audio and reports where the file is. Docs: <https://www.plivo.com/docs/voice/xml/record>.

Basic:

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `action` | URL | none | receives the recording details |
| `method` | string | `POST` | `GET` or `POST` |
| `fileFormat` | string | `mp3` | `mp3` or `wav` |
| `redirect` | boolean | `true` | `true` means the action document takes over |

Timing:

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `timeout` | integer | `15` | seconds of silence that stop the recording |
| `maxLength` | integer | `60` | maximum seconds |
| `finishOnKey` | string | `#` | a digit, `#`, `*`, or `none` |
| `playBeep` | boolean | `true` | beep before recording starts |

Session recording:

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `recordSession` | boolean | `false` | record the whole call in the background |
| `startOnDialAnswer` | boolean | `false` | start when the dialled party answers |
| `recordChannelType` | string | `stereo` | `mono` or `stereo`. Stereo puts each party on its own channel |

Transcription: `transcriptionType` (`auto`, `hybrid`, `manual`), `transcriptionUrl`, `transcriptionMethod` (default `POST`), `transcriptionReportType` (`full` or `compact`, default `compact`). Transcription is English only, 500 ms to 4 hours, under 2 GB.

Callbacks: `callbackUrl`, `callbackMethod` (default `POST`).

**Behaviour notes.** With `recordSession="true"` the recording starts at once and runs until the call ends, and `timeout`, `finishOnKey` and `playBeep` are ignored. With `recordSession` or `startOnDialAnswer` set, the durations in the initial `action` request are `-1`; the real values arrive at `callbackUrl`.

**Action parameters:** `RecordUrl`, `RecordingID`, `RecordingDuration`, `RecordingDurationMs`, `RecordingStartMs`, `RecordingEndMs`, `Digits`.

**Callback parameters:** the same list without `Digits`.

**Transcription parameters:** `transcription`, `transcription_charge`, `transcription_rate`, `duration`, `call_uuid`, `recording_id`, `error`.

Recordings are deleted after 30 days, so download what you need.

### Conference

Joins a named room. The room name goes in the element body. Maximum 20 participants. Docs: <https://www.plivo.com/docs/voice/api/conferences>.

Basic:

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `muted` | boolean | `false` | join muted, still hears others |
| `enterSound` | string | empty | `beep:1`, `beep:2`, or a URL |
| `exitSound` | string | empty | `beep:1`, `beep:2`, or a URL |
| `maxMembers` | integer | `20` | 1 to 20 |
| `timeLimit` | integer | `86400` | maximum seconds |
| `hangupOnStar` | boolean | `false` | member presses `*` to leave |
| `stayAlone` | boolean | `true` | keep the room open with one member left |

Moderation: `startConferenceOnEnter` default `true`, `endConferenceOnExit` default `false`, `waitSound` a URL played while waiting for the room to start.

Recording: `record` default `false`, `recordFileFormat` default `mp3`, plus `transcriptionType`, `transcriptionUrl` and `transcriptionMethod`.

Callbacks: `action`, `method` (default `POST`), `callbackUrl`, `callbackMethod` (default `POST`), `redirect` default `true`.

DTMF: `digitsMatch`, `floorEvent` default `false`, `relayDTMF` default `true`.

An `enterSound` URL must return XML containing `Play`, `Speak` or `Wait`, and the same is documented for the MultiPartyCall hold-music URLs. The conference page states it for `enterSound` only, so read it as very likely true for `waitSound` and `exitSound` rather than as a published rule for those two. It is a URL that returns a document, not an audio file.

**Action parameters:** `ConferenceName`, `ConferenceUUID`, `ConferenceMemberID`, `RecordUrl`, `RecordingID`.

**Callback parameters:** `ConferenceAction` (`enter`, `exit`, `digits`, `floor`, `record`), `ConferenceName`, `ConferenceUUID`, `ConferenceMemberID`, `CallUUID`, `ConferenceDigitsMatch`, `RecordUrl`, `RecordingID`, `RecordingDuration`, `RecordingDurationMs`, `RecordingStartMs`, `RecordingEndMs`.

### MultiPartyCall

Creates or joins a multi party call with roles. The name goes in the element body. Docs: <https://www.plivo.com/docs/voice/xml/multiparty-call>.

Roles: `Customer`, `Agent`, `Supervisor`, and `ai-agent` for an AI agent connected over WebSocket streaming.

MPC level:

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `maxDuration` | integer | `14400` | 300 to 28800 seconds |
| `maxParticipants` | integer | `10` | 2 to 10 |
| `record` | boolean | `false` | record the MPC |
| `recordFileFormat` | string | `mp3` | `mp3` or `wav` |
| `recordMinMemberCount` | integer | `1` | 1 or 2 members before recording starts |
| `waitForAgent` | boolean | `false` | customers hear wait music until an agent joins |
| `recordCoachVoice` | boolean | none | include the supervisor's voice in the recording |
| `startRecordingAudio` | URL | none | XML for audio played when recording starts |
| `startRecordingAudioMethod` | string | `GET` | `GET` or `POST` |
| `stopRecordingAudio` | URL | none | XML for audio played when recording stops |
| `stopRecordingAudioMethod` | string | `GET` | `GET` or `POST` |

Hold music: `waitMusicUrl` and `waitMusicMethod`, `agentHoldMusicUrl` and `agentHoldMusicMethod`, `customerHoldMusicUrl` and `customerHoldMusicMethod`. These URLs must return XML with `Play`, `Speak` or `Wait`.

Callbacks: `statusCallbackUrl`, `statusCallbackMethod`, `statusCallbackEvents`, `recordingCallbackUrl`, `recordingCallbackMethod`.

Participant level:

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `role` | string | required | `Agent`, `Supervisor`, or `Customer` |
| `mute` | boolean | `false` | join muted |
| `hold` | boolean | `false` | join on hold |
| `coachMode` | boolean | `true` | supervisors only. Agents hear the supervisor, customers do not |
| `stayAlone` | boolean | `false` | stay when alone |
| `startMpcOnEnter` | boolean | `true` | start the MPC on joining |
| `endMpcOnExit` | boolean | `false` | end the MPC on leaving |

Entry and exit sounds: `enterSound` default `beep:1`, `exitSound` default `beep:2`, each accepting `none`, `beep:1`, `beep:2` or a URL, with `enterSoundMethod` and `exitSoundMethod` defaulting to `GET`.

Actions: `onExitActionUrl`, `onExitActionMethod`, `relayDTMFInputs`.

AI agent stream attributes, used only with `role="ai-agent"`: `aiAgentStreamServiceUrl`, `aiAgentStreamContentType` (default `audio/x-l16;rate=8000`), `aiAgentStreamStatusCallbackUrl`, `aiAgentStreamStatusCallbackMethod` (default `POST`), `aiAgentStreamSamplingRate`, `aiAgentStreamExtraHeaders`. The attributes are documented; whether this form behaves exactly like the streaming API is a question for the `plivo-audio-streaming` skill.

**Status callback event groups:** `mpc-state-changes`, `participant-state-changes`, `participant-speak-events`, `participant-digit-input-events`, `add-participant-api-events`, `participant-audio-events`. Pass them as a comma separated list.

**Status callback parameters:** `EventName`, `EventTimestamp`, `MPCUUID`, `MPCName`, `MemberID`, `ParticipantRole`, `ParticipantCallUUID`, `ParticipantCoachMode`, `MPCDuration`, `MPCBilledDuration`, `MPCBilledAmount`.

**On exit parameters:** `MPCUUID`, `MPCFriendlyName`, `MemberID`, `ParticipantCallUUID`, `ParticipantJoinTime`, `ParticipantEndTime`, `ParticipantRole`.

**Conference or MultiPartyCall.** Conference takes 20 participants and has no roles; MultiPartyCall takes 10, has roles, coach mode, per participant hold and mute, and fuller API control. Use Conference for a simple bridge and MultiPartyCall for a contact centre.

**Docs disagreement.** The roles table writes the roles capitalised (`Customer`, `Agent`, `Supervisor`) while the AI role is lower case (`ai-agent`). Both cases are commonly seen for the human roles. This file follows the docs and writes `Agent`, `Supervisor`, `Customer`, `ai-agent`.

### Message

Sends an SMS from inside a call flow. The message text goes in the element body. Docs: <https://www.plivo.com/docs/messaging/xml/message>.

| Attribute | Type | Notes |
|---|---|---|
| `src` | string | sending number, must be one you own |
| `dst` | string | destination. Several numbers are separated by `<` |
| `type` | string | `sms` |
| `callbackUrl` | string | receives delivery reports |
| `callbackMethod` | string | `GET` or `POST`, default `POST` |

`<Message>` is documented in the messaging XML reference, not in the voice XML reference, and it does not appear in the voice element table on the voice XML overview page. It works inside a call flow per the messaging page.

### Standard request parameters

Sent with every request to an answer, action, redirect or fallback URL: `CallUUID`, `From`, `To`, `CallStatus`, `Direction`.

- Inbound: `From` is the caller, `To` is your Plivo number, `Direction` is `inbound`.
- Outbound: `From` is the caller id you set, `To` is the destination, `Direction` is `outbound`.
- Outbound calls also carry `ALegUUID` and `ALegRequestUUID`.
- Forwarded calls may carry `ForwardedFrom`, subject to the carrier.
- Completed calls carry `HangupCause`, `Duration`, `BillDuration`, `TotalCost`.
- `CallStatus` values: `ringing`, `in-progress`, `completed`, `busy`, `failed`, `timeout`, `no-answer`.
- SIP calls carry custom headers with an `X-PH-` prefix. Sending `sipHeaders="CustomId=123"` produces `X-PH-CustomId=123`.

Docs: <https://www.plivo.com/docs/voice/xml/overview>.

### Hangup causes seen in call records

`NORMAL_CLEARING`, `USER_BUSY`, `NO_ANSWER`, `CALL_REJECTED`, `UNALLOCATED_NUMBER`, `NETWORK_OUT_OF_ORDER`. The numeric code list is at <https://www.plivo.com/docs/voice/troubleshooting/hangup-causes>; the ones caused by XML are in "What breaks, and how to tell which" below.

## Patterns and the documents to copy

Each document below is small and well formed. Replace every `example.com` URL, every `+1000000000x` number and every prompt with your own. Every attribute used is documented in the element reference above.

### The four things a document can do

Every Plivo XML document does one or more of these, in this order:

1. **Say something.** `Speak`, `Play`.
2. **Ask something.** `GetDigits`, `GetInput`.
3. **Connect something.** `Dial`, `Conference`, `MultiPartyCall`, `Stream`.
4. **End or hand off.** `Hangup`, `Redirect`, or simply running out of elements.

`Record` sits alongside all of them: it either records a message on its own, or records the session in the background.

If the document does not reach step 3 or step 4 the call still ends, because a document that runs out of elements hangs up.

### Keypad menu

```xml
<Response>
  <GetDigits action="https://example.com/plivo/menu" method="POST" numDigits="1" timeout="10" retries="2" validDigits="123">
    <Speak voice="WOMAN" language="en-US">For sales press 1. For support press 2. To hear this again press 3.</Speak>
  </GetDigits>
  <Speak voice="WOMAN" language="en-US">We did not get a choice. Goodbye.</Speak>
  <Hangup/>
</Response>
```

Rules:

- The prompt goes **inside** `GetDigits`, so the caller can interrupt it by pressing a key.
- Only `Speak` and `Play` may go inside.
- The two elements **after** `GetDigits` are the no input path. Without them the call ends in silence after `retries` attempts.
- `validDigits` stops the caller entering something you have no branch for.
- Set `log="false"` when the digits are a PIN or a card number.
- The `action` document replaces this one, because `redirect` defaults to `true`.

What that menu's `action` URL returns for one branch:

```xml
<Response>
  <Speak voice="WOMAN" language="en-US">Connecting you to sales.</Speak>
  <Dial callerId="+10000000000" timeout="25" action="https://example.com/plivo/dial-result" method="POST">
    <Number>+10000000001</Number>
  </Dial>
</Response>
```

To loop back to the menu, have the action document return a `<Redirect>` to the menu URL. Give the loop an exit: a counter in the query string, or a `Hangup` after N passes.

### Speech or keypad menu

Same shape with `GetInput`, which the docs recommend for new work.

```xml
<Response>
  <GetInput action="https://example.com/plivo/menu" method="POST" inputType="dtmf speech" language="en-US" numDigits="1" executionTimeout="15" speechEndTimeout="auto" hints="sales, support, billing, agent">
    <Speak voice="WOMAN" language="en-US">Tell me what you need, or press 1 for sales and 2 for support.</Speak>
  </GetInput>
  <Speak voice="WOMAN" language="en-US">Sorry, I did not catch that. Goodbye.</Speak>
  <Hangup/>
</Response>
```

The action URL receives `InputType` so you can tell which one the caller used, plus `Digits` or `Speech`. `hints` improves recognition of the words you actually expect.

### Forwarding a call

```xml
<Response>
  <Dial callerId="+10000000000" timeout="25" action="https://example.com/plivo/dial-result" method="POST">
    <Number>+10000000001</Number>
  </Dial>
</Response>
```

- `callerId` is what the person you dial sees. Use a number you own.
- `timeout` is how long you ring before giving up.
- The `action` URL receives `DialStatus`, one of `completed`, `busy`, `failed`, `cancel`, `timeout`, `no-answer`. Handle all six, not just `completed`.
- With `redirect` left at its default, the action document takes over. With `redirect="false"` the next element in this document runs instead.

**Ring several people at once:** several `<Number>` children in one `<Dial>`. First to answer wins.

```xml
<Response>
  <Dial callerId="+10000000000" timeout="25" action="https://example.com/plivo/dial-result" method="POST">
    <Number>+10000000001</Number>
    <Number>+10000000002</Number>
    <Number>+10000000003</Number>
  </Dial>
</Response>
```

**Try people in turn:** several `<Dial>` elements, each with its own `timeout`.

```xml
<Response>
  <Dial callerId="+10000000000" timeout="15" redirect="false">
    <Number>+10000000001</Number>
  </Dial>
  <Dial callerId="+10000000000" timeout="15" redirect="false">
    <Number>+10000000002</Number>
  </Dial>
  <Speak voice="WOMAN" language="en-US">Sorry, nobody is available. Please try again later.</Speak>
  <Hangup/>
</Response>
```

**Dial a SIP address:** `sipAuthUsername` and `sipAuthPassword` when the far end challenges. Failures come back as `sip_auth_failed` (4240) or `sip_auth_timeout` (4250) on the action URL.

```xml
<Response>
  <Dial callerId="+10000000000" timeout="30" dialMusic="real" action="https://example.com/plivo/dial-result" method="POST">
    <User sipAuthUsername="sipuser" sipAuthPassword="REPLACE_WITH_SIP_PASSWORD" sipHeaders="TicketId=12345">sip:queue@example.com</User>
  </Dial>
</Response>
```

**Make sure a human, not a voicemail, takes it:** `confirmSound` returns a short document, and `confirmKey` is the key they must press to accept.

**Silence while ringing:** set `dialMusic`, either a URL that returns a document or the literal `real` to pass the carrier's own ringing through.

**Dial an extension:** `<Number sendDigits="wwww1234">`, where each `w` is half a second.

### Voicemail

`Record` has the same `redirect` default as `GetDigits` and `Dial`: `true`. With an `action` URL set and `redirect` left alone, Plivo runs the document that URL returns when the recording completes, so a thank-you written under the `Record` may never play. That is the default row, not a documented statement that the rest of your document is discarded, so treat it as a risk to design around rather than a broken call. There are two correct shapes, and the difference is only where the thank-you lives.

**Shape 1, everything in one document.** `redirect="false"` keeps control here, so the recording details are still posted to `action` and Plivo then carries on to the next element.

```xml
<Response>
  <Speak voice="WOMAN" language="en-US">Leave a message after the beep, then press hash.</Speak>
  <Record action="https://example.com/plivo/voicemail" method="POST" redirect="false" maxLength="120" timeout="10" finishOnKey="#" playBeep="true" fileFormat="mp3"/>
  <Speak voice="WOMAN" language="en-US">Thank you. Goodbye.</Speak>
  <Hangup/>
</Response>
```

**Shape 2, let the action URL finish the call.** Leave `redirect` at its default and put the thank-you in the document `/plivo/voicemail` returns. Use this when the thank-you depends on the recording, for example a different message when the caller hung up without speaking.

```xml
<Response>
  <Speak voice="WOMAN" language="en-US">Leave a message after the beep, then press hash.</Speak>
  <Record action="https://example.com/plivo/voicemail" method="POST" maxLength="120" timeout="10" finishOnKey="#" playBeep="true" fileFormat="mp3"/>
</Response>
```

and `/plivo/voicemail` returns:

```xml
<Response>
  <Speak voice="WOMAN" language="en-US">Thank you. Goodbye.</Speak>
  <Hangup/>
</Response>
```

The `action` URL receives `RecordUrl`, `RecordingID` and the durations either way. `maxLength` defaults to only 60 seconds, so set it.

**Forward, then voicemail.** Put the `Record` after the `Dial` in the same document. Both need `redirect="false"`: on the `Dial` so execution continues here when nobody answers, and on the `Record` so the thank-you below it still runs.

```xml
<Response>
  <Dial callerId="+10000000000" timeout="20" redirect="false" action="https://example.com/plivo/dial-result" method="POST">
    <Number>+10000000001</Number>
  </Dial>
  <Speak voice="WOMAN" language="en-US">Nobody is available. Leave a message after the beep, then press hash.</Speak>
  <Record action="https://example.com/plivo/voicemail" method="POST" redirect="false" maxLength="120" timeout="10" finishOnKey="#" playBeep="true" fileFormat="mp3"/>
  <Speak voice="WOMAN" language="en-US">Thank you. Goodbye.</Speak>
  <Hangup/>
</Response>
```

### Recording a conversation

```xml
<Response>
  <Record recordSession="true" startOnDialAnswer="true" maxLength="3600" fileFormat="mp3" recordChannelType="stereo" callbackUrl="https://example.com/plivo/recording" callbackMethod="POST"/>
  <Speak voice="WOMAN" language="en-US">This call is recorded for quality.</Speak>
  <Dial callerId="+10000000000" timeout="25" action="https://example.com/plivo/dial-result" method="POST">
    <Number>+10000000001</Number>
  </Dial>
</Response>
```

- `<Record>` goes **before** the thing you want recorded. It starts in the background and runs until the call ends.
- `startOnDialAnswer="true"` waits for the other party to answer, so you do not record the ringing.
- With `recordSession` set, `timeout`, `finishOnKey` and `playBeep` do nothing.
- The durations in the first `action` request are `-1`. Use `callbackUrl` for the real values.
- `stereo` puts each party on their own channel, which is what analytics tools want. `mono` is smaller.
- Tell the caller. Recording notice rules are legal, not technical.
- Recordings are deleted after 30 days.

### Conference rooms

Guests join with `startConferenceOnEnter="false"` and a `waitSound`; the host joins with `startConferenceOnEnter="true"` and usually `endConferenceOnExit="true"`.

```xml
<Response>
  <Conference startConferenceOnEnter="false" waitSound="https://example.com/plivo/hold-xml" maxMembers="20" enterSound="beep:1" exitSound="beep:2" action="https://example.com/plivo/conference-exit" method="POST">weekly-standup</Conference>
</Response>
```

```xml
<Response>
  <Conference startConferenceOnEnter="true" endConferenceOnExit="true" record="true" recordFileFormat="mp3" callbackUrl="https://example.com/plivo/conference-events" callbackMethod="POST">weekly-standup</Conference>
</Response>
```

`waitSound`, `enterSound` and `exitSound` take a **URL that returns a document** containing `Play`, `Speak` or `Wait`, not an audio file directly. `beep:1` and `beep:2` are the built in shortcuts.

A conference is also the simplest way to bridge two separate inbound calls: give both the same room name. Twenty participants maximum.

### Rooms with roles

Use `MultiPartyCall` when you need roles, coaching, or per participant hold and mute.

```xml
<Response>
  <MultiPartyCall role="Agent" maxDuration="14400" maxParticipants="10" record="true" recordFileFormat="mp3" startMpcOnEnter="true" endMpcOnExit="false" statusCallbackUrl="https://example.com/plivo/mpc" statusCallbackMethod="POST" statusCallbackEvents="mpc-state-changes,participant-state-changes">support-call-1</MultiPartyCall>
</Response>
```

A supervisor joins the same room name with `role="Supervisor"` and `coachMode="true"`: agents hear them, customers do not. `onExitActionUrl` gives you a document to run when a participant leaves, which is where a post call survey goes.

### Screening and out of hours

```xml
<Response>
  <Hangup reason="rejected"/>
</Response>
```

`rejected` gives a rejection tone, `busy` gives a busy signal. This is a legitimate pattern at scale: allow lists, blocked callers, closed hours. It shows up in call records as 3020 or 3010 with hangup source Answer XML, which is an observation and not a mapping the docs publish.

A friendlier version says something first:

```xml
<Response>
  <Speak voice="WOMAN" language="en-US">Our office is closed. We are open Monday to Friday, nine to five.</Speak>
  <Hangup/>
</Response>
```

Decide which one you want. They are not hard to tell apart later: a deliberate `<Hangup reason="rejected"/>` is seen in call records as 3020 with your answer document behind it, while a broken deployment records 7011 or 8011. Choose the rejection deliberately rather than by accident.

### Custom ringback before answering

**Avoid `loop="0"` in a `PreAnswer` that has a `Dial` after it.** `loop="0"` is documented as an infinite loop and children run one at a time top to bottom, so the `PreAnswer` may never finish and the `Dial` below it may never be reached. Nothing documents that outcome, and the public routing page's custom-ringback example uses `loop="0"` in exactly this shape, so raise it as a risk and use a finite loop sized to your ring time rather than calling the document broken.

```xml
<Response>
  <PreAnswer>
    <Play loop="3">https://example.com/audio/ringback.mp3</Play>
  </PreAnswer>
  <Dial callerId="+10000000000" timeout="25">
    <Number>+10000000001</Number>
  </Dial>
</Response>
```

`loop="0"` is only correct when nothing needs to run afterwards, for instance a hold document returned to `dialMusic` or `waitSound`, where the element is meant to play until the call moves on for another reason.

If what you actually want is ringback while you dial, `dialMusic` on `<Dial>` is usually the better tool: it plays to the caller while the other leg rings, and the literal value `real` passes the carrier's own ringing through.

`PreAnswer` takes only `Speak`, `Play` and `Wait`, and should stay under 30 seconds because the call is not answered yet and some carriers give up: those are the three limitations the page lists. Putting it first is this file's inference from what it does, not a documented rule.

### SSML prompts

SSML needs a `Polly.` voice.

```xml
<Response>
  <Speak voice="Polly.Joanna" language="en-US"><prosody rate="medium">Your reference is <say-as interpret-as="spell-out">AB12</say-as><break time="500ms"/>Please keep it safe.</prosody></Speak>
</Response>
```

Maximum 3,000 characters per `<Speak>`. `<amazon:effect>` and `<amazon:auto-breaths>` are not supported.

### Sending an SMS from a call

`<Message>` sends an SMS mid flow. `src` must be a number you own.

```xml
<Response>
  <Speak voice="WOMAN" language="en-US">Thanks. I am texting you the link now.</Speak>
  <Message src="+10000000000" dst="+10000000001" type="sms" callbackUrl="https://example.com/plivo/sms-status" callbackMethod="POST">Here is the link you asked for: https://example.com/booking</Message>
  <Hangup/>
</Response>
```

### Acknowledging a callback

```xml
<Response></Response>
```

Return that, or an empty 200, to any `callbackUrl`, `ring_url` or hangup URL. Never return it from an answer URL unless you want the call to end.

### XML around a voice bot

If the call is going to a WebSocket voice bot, the bot workflow, readiness and debugging belong to **`plivo-audio-streaming`**, a separate install. What belongs here is the XML that surrounds `<Stream>`, and its ordering rules:

- **A greeting before the bot.** `<Speak>` or `<Play>` before `<Stream>` runs first, so it delays the bot by however long it takes. Keep it short.
- **A keypad menu in front of the bot.** `<GetDigits>` before `<Stream>` works like any other menu. Remember that `redirect` defaults to `true`, so a caller who presses a key gets the action document instead of the `<Stream>` you wrote below it. A caller who presses nothing still falls through to it after `retries` attempts. Either set `redirect="false"`, or repeat the `<Stream>` in the action document, and raise it as a risk rather than a broken document.
- **Recording.** `<Record recordSession="true"/>` goes **before** `<Stream>`, for the same reason it goes before `<Dial>`: it must be running while the audio flows.
- **Handing off to a human.** `<Dial>` with `<Number>` or `<User>`, and an `action` URL that reads `DialStatus`. Handle `busy`, `no-answer`, `timeout` and `failed`, not only `completed`.
- **Continuing after the bot.** `<Redirect>` after `<Stream>` sends the call to a URL of yours when the stream ends, instead of the call ending. `<Hangup/>` after `<Stream>` ends it deliberately.
- **Putting the bot in a room.** `MultiPartyCall` with `role="ai-agent"` and the `aiAgentStream*` attributes.

The `<Stream>` element's own attributes, the WebSocket protocol, and every question about whether the bot is ready for production are out of scope here. Install `plivo-audio-streaming` (`npx skills add https://www.plivo.com/docs --skill plivo-audio-streaming`) or read <https://www.plivo.com/docs/voice-agents/audio-streaming/xml/stream>.

### A checklist before you ship a document

1. Does it parse? Run it through any XML parser.
2. Is every `&` inside an attribute written `&amp;`?
3. Does every element that can produce nothing have something after it?
4. Have you tested every URL the document names, each with the method it is actually configured with, and looked at the body? Most documented XML element URL methods default to `POST`, but not all: `startRecordingAudioMethod`, `stopRecordingAudioMethod`, `enterSoundMethod` and `exitSoundMethod` on `<MultiPartyCall>` default to `GET`. The `method`, `callbackMethod` or `transcriptionMethod` you set on the element wins, as does the method set on the application or on the API request. Test each URL with the method it is actually configured with rather than assuming.
5. Does the account have a Fallback Answer URL set?
6. Is `log="false"` on anything that collects a secret?
7. Does every `Redirect` loop have an exit?
8. Does the caller get told the call is recorded, when it is?

## The URL contract: callbacks, signatures, timeouts

Which URL Plivo calls, when, what it sends, what it expects back, and how to secure and tune it.

### The four call level URLs

Set on the voice application for inbound calls, or in the API request for outbound calls. Docs: <https://www.plivo.com/docs/voice/concepts/callbacks>.

| URL | When | Expects |
|---|---|---|
| Primary Answer URL, or `answer_url` | as soon as the call is answered | one Plivo XML document |
| Fallback Answer URL, or `fallback_url` | when the primary answer URL is not reachable | one Plivo XML document |
| `ring_url` | when the call starts ringing | nothing. Return 200 |
| Hangup URL, or `hangup_url` | when the call is disconnected | nothing. Return 200 |

`answer_url` is mandatory for an outbound API call, and a Primary Answer URL is mandatory on a voice application; the rest are optional. For an outbound API call, `fallback_url` is invoked if `answer_url` fails after 3 retries or a 60 second timeout (<https://www.plivo.com/docs/voice/api/calls>). Default method for each is `POST`.

Set the fallback. Without it, a primary answer URL that flaps under load turns every call into a failed call.

### The two element level URLs

These come out of the XML itself, and mixing them up is a frequent bug.

**`action`** expects XML back. Plivo runs whatever it returns. It is invoked at the end of an element's execution, for example when the caller has finished entering digits.

**`callbackUrl`** expects nothing back. It is a notification about something that happened during an element's execution, for example a conference participant being muted. Return HTTP 200. An empty body or `<Response></Response>` are both fine.

Returning the word `OK` or a JSON status object to an `action` URL matters only when `redirect` is left at its default `true`, because only then does Plivo fetch that document to run it: the parse fails and the call ends with 8012. With `redirect="false"` the URL is still called but its answer is ignored, so the same body is harmless. Returning it to a `callbackUrl` is always harmless.

`redirect` controls whether the `action` document takes over the call. It defaults to `true` on `GetDigits`, `GetInput`, `Record`, `Dial` and `Conference`. With `redirect="false"` the URL is still called, the answer is ignored, and the next element in your document runs. A bad or unreachable action document only fails the call when `redirect` is `true`.

### What every request carries

Standard parameters on the answer, fallback, action and redirect URLs: `CallUUID`, `From`, `To`, `CallStatus`, `Direction`.

Direction changes the meaning of `From` and `To`:

- inbound: `From` is the caller, `To` is your Plivo number.
- outbound: `From` is the caller id you set, `To` is the destination.

Extra parameters:

- outbound calls: `ALegUUID`, `ALegRequestUUID`.
- forwarded calls: `ForwardedFrom`, when the carrier supplies it.
- completed calls: `HangupCause`, `Duration`, `BillDuration`, `TotalCost`.
- SIP calls: custom headers with an `X-PH-` prefix. `sipHeaders="CustomId=123"` arrives as `X-PH-CustomId=123`.

`CallStatus` values: `ringing`, `in-progress`, `completed`, `busy`, `failed`, `timeout`, `no-answer`.

### Per element parameters

The full lists are in the element reference above. In short:

| Element | Action URL receives | Callback URL receives |
|---|---|---|
| `GetDigits` | `Digits` | none |
| `GetInput` | `InputType`, `Digits`, `Speech`, `SpeechConfidenceScore`, `BilledAmount` | interim speech: `StableSpeech`, `UnstableSpeech`, `Stability`, `SequenceNumber` |
| `Dial` | `DialStatus`, `DialRingStatus`, `DialHangupCause`, `DialALegUUID`, `DialBLegUUID` | `DialAction`, B leg status, duration, numbers, DTMF matches, hangup cause and source, `STIRVerification` |
| `Record` | `RecordUrl`, `RecordingID`, durations, `Digits` | the same without `Digits` |
| `Conference` | `ConferenceName`, `ConferenceUUID`, `ConferenceMemberID`, `RecordUrl`, `RecordingID` | `ConferenceAction` plus the same identifiers and recording fields |
| `MultiPartyCall` | `onExitActionUrl`: MPC and participant identifiers and times | `statusCallbackUrl`: `EventName` and the event's fields |

### Securing the URLs

Every request from Plivo carries `X-Plivo-Signature-V3`, `X-Plivo-Signature-Ma-V3` and `X-Plivo-Signature-V3-Nonce`. Validate the signature instead of protecting the URL with Basic auth, a bearer token or a secret in the query string. Plivo has no way to send your credentials, so an answer URL behind Basic auth or a bearer token is expected to reject Plivo's request and produce 7011. That is an inference, not a documented rule: no page states what Plivo does with a 401.

Use your SDK's helper. Every Plivo server SDK has one, and the manual form is easy to get subtly wrong.

How it is built (<https://www.plivo.com/docs/voice/concepts/signature-validation>): Plivo takes the full request URL including scheme, port and query string; appends a `.`; appends the POST parameters sorted alphabetically by name with Unix style case sensitive sorting, as name then value with no separator between them; appends a second `.`; appends the nonce from `X-Plivo-Signature-V3-Nonce`; and signs the result with HMAC SHA256 using your Auth Token, Base64 encoded. On a GET the parameters are already in the query string, so the middle part is empty but both dots still stand.

**The `.` separators are the part manual implementations miss.** The documented worked example, for URL `https://example.com/abcd?foo=bar` with POST parameters `CallUUID`, `Digits`, `From` and `To` and nonce `kjsdhfsd87sd7yisud2`, assembles to:

```text
https://example.com/abcd?foo=bar.CallUuid4vbcpem8-0u46-x1ha-9af1-438vc92bf374Digits1234From+15551111111To+15555555555.kjsdhfsd87sd7yisud2
```

A `.` between the URL and the sorted parameters, and a `.` before the nonce. Omit them and you compute a different string and reject every genuine request. Read the current page before shipping a hand written validator.

If your account has more than one auth token, Plivo sends comma separated signatures and you must accept a match against any of them.

V2 signatures are deprecated.

If your server sits behind a proxy or load balancer, sign against the URL the client actually requested, not the internal one, or the signature will never match.

### Timeouts, retries and edge region

Plivo retries a webhook when it does not get a 200. You tune this with URL fragments appended to the callback URL, in the form `#key=value&key2=value2` (<https://www.plivo.com/docs/voice/concepts/callback-configurations>).

| Key | Meaning | Allowed | Default |
|---|---|---|---|
| `ct` | connection timeout, milliseconds | 100 to 10000 | 2000 |
| `rt` | read timeout, milliseconds | 100 to 40000 | 40000 |
| `tt` | total timeout across retries, milliseconds | 100 to 55000 | 55000 |
| `rc` | retry count | 0 to 5 | 1 |
| `rp` | retry policy | `4xx`, `5xx`, `ct`, `rt`, `all`, comma separated | `ct,rt` |
| `er` | edge region | `nearest`, `local`, `n_california`, `n_virginia`, `frankfurt`, `singapore`, `mumbai` | `nearest` |

Example: `https://example.com/answer?x=1#ct=2000&rt=3000&rc=3&rp=ct,rt`.

The docs' "Applicable URLs" list is longer than most people expect. The fragments apply to:

- **Voice application on the console:** Primary Answer URL, Fallback Answer URL, Hangup URL.
- **Make a call:** `answer_url`, `ring_url`, `hangup_url`, `fallback_url`, `machine_detection_url`.
- **Transfer a call:** `aleg_url`, `bleg_url`.
- **Recording and transcription:** `transcription_url` and `callback_url` on call and conference recording.
- **XML element URLs:** `action` and `callbackUrl` on the elements that take them, plus `confirmSound`, `dialMusic` and `waitSound`, and the interim speech results callback on `GetInput`.

They do **not** apply to audio URLs used by `<Play>` or `<PreAnswer>`, which use fixed values: 2 second connection timeout, 120 second read timeout, retry count 1, retry policy `all`. A partial response is not retried. Read <https://www.plivo.com/docs/voice/concepts/callback-configurations> for the current list rather than assuming a URL is covered.

### Idempotency

Plivo may deliver the same callback more than once because of retries and network problems. Make every handler idempotent.

Suggested keys:

| Callback | Key | Note |
|---|---|---|
| `hangup_url` | `CallUUID` | the call ended, safe to process once |
| `ring_url` | `CallUUID` | can fire more than once when dialling several numbers |
| `action` URL | `CallUUID` plus the element context | the same action can be reached again after a retry |
| `recordingCallbackUrl` | `RecordingID` | recording ready |

### Answer quickly, and answer with the right headers

- Content-Type `application/xml` or `text/xml`.
- Under 100 KB.
- Under 15 seconds.
- HTTPS for every callback URL as a security rule (Plivo accepts `http://` too) and every audio URL.

A note on the documented sample URL. `https://s3.amazonaws.com/static.plivo.com/answer.xml` is Plivo's own test answer URL. It is a static object, so it answers GET but rejects POST. Used as an answer URL with the default POST method, or reused as a hangup URL, it returns an HTTP 405 and an XML error document rather than Plivo XML. Point real applications at your own endpoint (commonly seen).

## What breaks, and how to tell which

The codes Plivo reports when a document is wrong or unreachable, and the shapes that cause them. Codes come from <https://www.plivo.com/docs/voice/troubleshooting/hangup-causes>; the shapes are commonly seen in practice.

### The codes

**XML errors, 8011 to 8014.** Your URL replied, but not with a document Plivo could use.

| Code | Name | Which document |
|---|---|---|
| 8011 | Invalid Answer XML | the answer URL's document, the first one of the call |
| 8012 | Invalid Action XML | an `action` URL's document. Only fails the call when `redirect="true"` |
| 8013 | Invalid Transfer XML | a transfer URL's document |
| 8014 | Invalid Redirect XML | a `<Redirect>` URL's document |

**URL errors, 7011 to 7034.** Plivo never got a usable HTTP response, so there is no document to inspect.

| Code | Name | Meaning |
|---|---|---|
| 7011 | Error Reaching Answer URL | non 2xx from the answer URL |
| 7012 | Error Reaching Action URL | non 2xx from an action URL. Only fails when `redirect=true` |
| 7013 | Error Reaching Transfer URL | non 2xx from a transfer URL |
| 7014 | Error Reaching Redirect URL | non 2xx from a redirect URL |
| 7022, 7023, 7024 | Invalid Action, Transfer, Redirect URL | the URL is not a valid `http://` or `https://` URL |
| 7032, 7033, 7034 | Invalid Method | a method other than GET or POST |

**Normal ends that people mistake for faults.**

| Code | Name | Meaning |
|---|---|---|
| 4010 | End Of XML Instructions | the document ran out of elements. Normal for an XML controlled call |
| 3020 | Rejected | the hangup-causes table describes this as the called party rejecting. Observation, not documented: call records for a `<Hangup reason="rejected"/>` answer document carry 3020 with hangup source Answer XML, and the routing page documents only the rejection tone |
| 3010 | Busy Line | the hangup-causes table describes this as the destination being busy. Same observation for `<Hangup reason="busy"/>`; the routing page documents only the busy signal |

The difference between a 70xx and an 80xx is the difference between "your server did not answer" and "your server answered with the wrong thing". Do not debug the XML for a 7011. Read the HTTP status code, the method and the host.

### The shapes that produce 8011 and 8012

All of these are commonly seen. Roughly in order of how often they turn up.

**1. A JSON body where XML should be.** The single most common shape. Two sources.

A number is attached to a **console flow application** rather than to your XML application. The flow answers the call instead of your answer URL, and if the flow itself cannot run you get a JSON body you did not write. Plivo publishes no schema for that body, so match on "this is JSON and I did not write it" rather than on any particular field.

Or your own framework returns its JSON error envelope:

```json
{"code":404,"message":"This webhook is not registered for POST requests. Did you mean to make a GET request?"}
```

Fix. Check what the number is actually attached to before you debug your code. Then make the endpoint answer POST, and make its error path return a valid document such as `<Response><Speak>We are sorry, we cannot take your call right now.</Speak><Hangup/></Response>` rather than a JSON error.

**2. An HTML page.** Your web framework returned a page instead of a document. The parser has nothing to work with, because it read to the end without finding a Plivo root element. Plivo does not publish the parser messages, so match on the body you sent rather than on any particular error string. The body starts with something like `<!DOCTYPE html><html lang="en">`. This is a 404 page, a login page, or a single page application catch all route swallowing the path. Large pages also break the 100 KB limit.

Fix. Request the URL yourself with POST and look at the first 40 bytes of the body. If it starts with `<!DOCTYPE`, the route is wrong.

**3. Plain text returned to an action URL.** `OK`, `ok`, `DialEnd OK`. Plivo's parser reports `syntax error` at line 1. Someone treated an `action` URL as a notification.

Fix. Either return a real document, or set `redirect="false"` on the element so the URL is treated as a notification and the next element in your document runs. See "The URL contract" above.

**4. An unescaped ampersand.** A query string with two parameters written straight into an attribute:

```xml
<Response>
  <Record action="https://example.com/rec?type=inbound&amp;to=+10000000001"/>
</Response>
```

Written with a bare `&`, Plivo's parser reports `not well-formed (invalid token)` at the column of the `&`. Write `&amp;`, as above. Use your language's XML builder, or the Plivo server SDK, and this cannot happen.

**5. A Twilio element.**

```xml
<Response>
  <Hangup reason="rejected"/>
</Response>
```

`<Reject>`, `<Say>`, `<Gather>`, `<Pause>` and `<Parameter>` are Twilio elements and none of them is in Plivo's element list, so a document built from them has no element Plivo can run. Answer documents with a top-level `<Reject/>` have been observed to fail with 8011; a top-level `<Say>` and a nested `<Parameter>` have been seen to be ignored instead, and the docs do not say what Plivo does with an unrecognised element. Fix all of them, and name a code only for `<Reject/>`. Plivo's equivalents are `<Hangup reason="rejected"/>` as shown, plus `<Speak>`, `<GetInput>` and `<Wait>`. Ported Twilio code also tends to keep Twilio's URL paths, which is a useful clue.

**6. An empty or half filled template.**

```xml
<Response>
  <Speak>Connecting you now.</Speak>
  <Dial callerId="+10000000000" timeout="30">
    <Number></Number>
  </Dial>
</Response>
```

An empty `<Number>`, an empty `sendDigits=""`, or a whole `<Response></Response>` because the template rendered no branch. An empty `<Response>` is not a parse error, but it hangs the call up the moment it runs. Returned to a `callbackUrl` it is a perfectly good acknowledgement; returned to an answer URL it is a dropped call.

Fix. Omit the element rather than emitting it empty, and give the template a default branch.

**7. An unsupported language or voice.**

```xml
<Response>
  <GetInput action="https://example.com/plivo/input" method="POST" inputType="speech" language="hi-IN">
    <Speak voice="Polly.Aditi" language="hi-IN">Please say your order number.</Speak>
  </GetInput>
</Response>
```

The documented speech language list is printed as partial, so a code that is not on it is untested rather than known to be rejected: the docs do not say what happens to one. The same applies to `<Speak language>` outside the documented table unless you are using a `Polly.` voice that covers it.

Fix. Use a code from the tables in the element reference above, or a Polly voice that covers the language. Test any code that is not listed before you rely on it.

**8. A second document nobody ever tested.** 8012 usually arrives well into a call that was working. The answer document was correct; the `action` document was never exercised. Common instances: the `GetDigits` action URL that only handles the happy path, the `Dial` action URL that returns `OK`, the `Record` action URL that returns the recording as JSON.

Fix. Test every URL your document names, not just the first one. For each, send a POST with the parameters that element sends and check the body is a valid `<Response>`.

**9. Leading bytes before the root.** The docs make no Plivo specific claim here, so this is ordinary XML behaviour, checked against `xmllint` and Python's ElementTree. An XML declaration must be the first thing in the document: put a newline or any other whitespace in front of `<?xml` and both parsers refuse it with "XML declaration allowed only at the start of the document". Without a declaration, leading whitespace is legal and both accept it, and both also accept a UTF-8 byte order mark, which they strip as an encoding signature. Any other stray non whitespace byte before the root is a parse error. So: if you emit a declaration, emit nothing before it; if a body that looks correct still fails, print its first bytes as hex.

### Shapes that are legitimate, not bugs

- **`<Response><Hangup reason="rejected"/></Response>` as the whole answer document.** This is deliberate screening: an allow list, a blocked caller, or out of hours. Call records for this shape show 3020 with hangup source Answer XML and zero duration, which is an observation rather than a documented mapping. It is only a bug when someone left it in as a placeholder.
- **`<Response></Response>` returned to a `callbackUrl` or a hangup URL.** An acknowledgement. Correct.
- **A document with no `<Dial>` and no `<Stream>` at all.** Speak or play something, then hang up. That is a whole class of production traffic: out of hours messages, deflection announcements, notifications.
- **4010 End Of XML Instructions.** The document finished. Nothing is wrong.

### How to debug one call

1. Read the code first. 70xx means HTTP, 80xx means the body, 4010 means it worked and ended.
2. For 80xx, get the exact bytes. Console, Voice, Logs, Calls, the call, then the debug log. Plivo records its parser's own message there, for example `XML Parsing Error: Invalid XML Syntax: not well-formed (invalid token): line 1, column 271`. The line and column point straight at the problem. `plivo voice calls diagnose <call_uuid>` reads the same call for you.
3. Reproduce it yourself with the same method and the same parameters:

```bash
curl -s -i -X POST https://example.com/plivo/answer \
  -d 'CallUUID=test&From=%2B10000000000&To=%2B10000000001&Direction=inbound&CallStatus=ringing'
```

Read the status line, the `Content-Type` and the first 40 bytes of the body. Most 8011s are visible in those three things.

4. For 8012, run the same request against the `action` URL with that element's parameters, not against the answer URL.
5. If the body is right and the call still fails, check the size (100 KB), the content type (`application/xml` or `text/xml`) and the time to first byte (under 15 seconds).

Sources: the Voice XML, callbacks and troubleshooting sections of the Plivo docs (pages cited above). Where a rule says "commonly seen", it is an observation from practice with no docs page behind it: treat it as a thing to check, not as a Plivo commitment, and re-verify it if the platform changes.
