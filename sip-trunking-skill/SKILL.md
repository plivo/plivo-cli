---
name: plivo-sip-trunking
description: "Connect an AI voice platform (LiveKit, ElevenLabs, Retell, Vapi, xAI, a self-hosted stack or any SIP-capable agent) to phone calls with Plivo SIP trunking (Zentrunk) and take it live, using the Plivo CLI. Use it for SIP trunks, Zentrunk, origination URIs, termination domains, connecting a named AI platform to Plivo, calls that do not reach the platform, inbound or outbound trunks, TCP, TLS or UDP transport, media anchoring, a Zentrunk hangup code (4090, 4170, 4590, 4000, 4550 and others) or SIP response (404, 407, 408, 480, 486, 503) on a trunk call, SIP REFER transfer to a human, India KYC for a trunk number, or go-live readiness on SIP. Not for WebSocket bots (use plivo-audio-streaming) or for Plivo XML (use plivo-voice-xml)."
license: Apache-2.0
---

# Plivo SIP trunking for AI voice agents

## What a finding licenses you to say

Three tiers, and they decide your verdict.

- **Will break.** A documented rule says so, or a call with this shape is known to have failed. Say the calls break, and name the failure.
- **Risky.** Plausible, unverified, or seen to work in some deployments. Say what could go wrong and what to check. Name no hangup code or SIP response.
- **Style.** No functional effect. Say so.

Nothing outside the first tier is a reason to tell someone their calls will break. Every hangup code stated as a fact below comes from the public table; anything in a "SIP seen" column, or marked observed, is risky and must not be quoted as a Plivo contract.

## Overview

You are guiding a developer (or their coding agent) from "I have an agent on platform X" to a phone number that reaches it, outbound calls that connect, transfers that work, and a way to read failures. Run the readiness check first, then go stage by stage with the check at each. Speak plainly.

Two objects do the work. An **inbound trunk** carries calls *to* the platform: origination URI, trunk, number attached. An **outbound trunk** carries calls *from* it: credential or IP list, trunk, its `trunk_domain`. Only inbound trunks attach to numbers (<https://www.plivo.com/docs/sip-trunking/api/trunks>). The URI string `host[:port];transport=...` is the whole inbound integration. The generic guide says a transport **mismatch** is the most common reason an inbound integration fails silently (<https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/other-platforms>). A missing `;transport=` is not the same thing: Plivo's API examples publish URIs without it, and no page documents the default transport, so treat an absent transport as a risk to check against the platform's guide.

Use the typed `plivo sip` commands for every trunk object (`plivo sip uris|trunks|credentials|ip-acl|calls`, added in CLI v1.1.0) and `plivo numbers update --trunk-id` to route a number. Use `plivo api` only where no typed command exists: call Insights (`/Zentrunk/Call/<uuid>/Insights/`). `plivo <command> --help` is the source of truth for flags; never invent one.

How the commands behave, which changes how you preview:

- `create` and `update` have **no `--yes` gate**: they write as soon as they run without `--dry-run`. Preview with `--dry-run`, show the output, ask, then run the same command without `--dry-run`. Never put the preview and the write in one shell command.
- `delete` needs `--yes`. To preview a delete, run it **without** `--yes`: it names the trunks that use the object (a trunk delete counts the numbers on it), then refuses (exit 5). `--dry-run` on a delete also suppresses that read, so it shows nothing useful.
- Passwords go in on stdin only (`--password-stdin`). Never put one on a command line, in a file you commit, or in chat.
- Quote every `--uri` value: the `;` in `host;transport=tcp` ends the shell command otherwise.

**What this file assumes you have: nothing but this file and the `plivo` CLI.** Everything the SIP trunking journey needs is here, including the India prerequisites. Other Plivo skills ship inside the CLI: `plivo skill install voice-xml` for Plivo XML and answer URLs, `plivo skill install audio-streaming` for a WebSocket voice bot, and `plivo skill install` for the CLI's own reference. If none is installed, use `plivo <command> --help` and `plivo docs search <keywords>`, and say which source you used.

What is in this file, in order: the readiness checklist, prerequisites by country, six stages, go-live, the debugging order with a short code table, what not to do, then the deep sections (India, security and limits, the platform table, commands per platform, SIP REFER, the `plivo sip` reference, the full hangup-code table).

Three questions before anything else: inbound, outbound or both (Retell needs the outbound trunk even for inbound-only; xAI is inbound only)? Which platform, hosted or self-hosted? Which country are the numbers and callees in (India changes the rules below)?

## Readiness check (run first, and again before go-live)

Read-only. Nothing here changes anything. Run in order and stop at the first blocking answer.

1. `plivo auth whoami -o json`. Look for: the account you meant, and `cash_credits` above zero. The published 4030 row carries no direction and no page says inbound trunk calls are billed, so treat a zero balance as a risk to inbound too; check the balance rather than predicting a code.
2. `plivo numbers get <number> -o json`. Look for: the number is on this account and `voice_enabled` is true. Note the `application` field: it is the current binding and your rollback value. For Indian numbers confirm compliance with `plivo numbers compliance list --country IN --status accepted -o json`, and that the application is linked to this number with `plivo numbers compliance get <compliance_id> --expand linked_numbers -o json`.
3. Read the binding. A value containing `Trunk/<id>` is a trunk: run `plivo sip trunks get <id> -o json` and read `data.object`, which must show `trunk_direction` `inbound`. A value containing `Application/<id>` is an XML application, not a trunk: stop, the number is not on SIP trunking. Report any other shape instead of guessing. Look for: `trunk_direction` is `inbound`, `trunk_status` is `enabled`, and `primary_uri_uuid` is set. A trunk with no primary URI has nowhere to send the call; the published 4310 row is `uri_not_found`, "Origination URI not found".
4. `plivo sip uris get <primary_uri_uuid> -o json`. Look for: a URI that passes the URI checklist in stage 2 and matches the platform table. `authentication_needed` is set only when the platform challenges Plivo.
5. `plivo sip uris get <fallback_uri_uuid> -o json`. A missing fallback URI is a warning, not a blocker: it is the only way Plivo re-routes when the primary is unreachable or returns an error.
6. Outbound only: `plivo sip trunks list --direction outbound -o json`, then `plivo sip ip-acl get <ipacl_uuid> -o json` or `plivo sip credentials list -o json`. Look for: an enabled outbound trunk with a credential or a narrow IP list; no `0.0.0.0/0`, no `/0`, no `/1`. Vapi needs its two `/32` addresses. Retell will not import a number without its termination URI, even inbound only.
7. Limits (console only): Organization settings > Account limits shows the account's concurrency limit. Every PSTN leg counts toward concurrency, inbound and outbound, across SIP Trunking and Voice API, and SIP Trunking **rejects** calls above either limit (5180 CPS, 5190 concurrency); it never queues (<https://www.plivo.com/docs/sip-trunking/concepts/account-limits>).
8. Ask the platform-side questions in stage 3. Plivo cannot see any of them. A missing step there usually shows up as the platform's own 404 (stage 3) or 486 (Debugging, step 3).
9. Optional reachability probe: one SIP OPTIONS to the URI host over its transport. A timeout proves nothing, because hosted platforms may ignore OPTIONS from unknown sources; a DNS or TLS error does prove something.

There is no single CLI command that runs this checklist. Run the steps by hand, in order.

| Check | Verified how | Failure it prevents |
|---|---|---|
| CLI has credentials; account has credits | step 1 | 4030 no credits to start a call |
| Number rented, voice-enabled | step 2 | nothing routes |
| India: an accepted compliance application attached to the number; data region confirmed by you | step 2 and the console | the number cannot be rented or used until the application is `accepted`. 4590 is the media-anchoring code, a different gate |
| Number attached to an **inbound** trunk that is enabled | step 3 | nothing routes; an outbound trunk or XML application cannot carry inbound |
| Trunk has a primary URI | step 3 | 4310 |
| URI host, port, `;transport=` match the platform table; no URL, no private IP | step 4 | 4170 no answer; silent inbound failure |
| `authentication_needed` only when the platform challenges Plivo | step 4 | a challenge Plivo cannot answer |
| Outbound: the trunk's `secure` flag matches the platform's outbound transport | step 6 | one-sided TLS; 4110 |
| Fallback URI present (warning) | step 5 | platform 503 with no second route |
| Outbound trunk enabled with a credential or a narrow IP list | step 6 | 407 never answered or 4180; toll fraud on open lists. The wide-range block is this file's rule; the CLI warns on it too |
| Concurrency and CPS known | step 7 | 5190 and 5180 rejections |
| Platform reachable | step 9 | DNS and TLS errors (a timeout proves nothing) |
| Platform-side items Plivo cannot see | step 8 | 4090 platform 404, 4410 486 |

## Prerequisites by country

**India.** Read the India section below. Account in the India data region (cannot be changed; not readable over the API), KYC `accepted` and linked to the number (`plivo numbers compliance requirements|create|get|link`, with documents the user supplies), caller ID a Plivo India number, both legs in India, right number series, consent, no cold calls. **4590** `domestic_anchored_terms_not_met` is the documented India media-anchoring code. KYC is a separate gate, so check it with `plivo numbers compliance get`; do not read 4590 as proof that KYC is missing. **Vapi cannot do India**; ElevenLabs needs its India deployment and `sip.rtc.in.residency.elevenlabs.io:5060;transport=tcp`; LiveKit needs region pinning; Retell: confirm with Retell first (<https://www.plivo.com/docs/voice-agents/sip-trunking/deploy/calling-in-india>).

**US.** No KYC. Caller ID: the technical-specifications page requires a Plivo number for all outbound calls; the generic guide also allows a verified caller ID. Use a Plivo number on the same account, which also gets STIR/SHAKEN attestation A; anything else risks 4190. Geo permissions: turn off every country you do not call (console only). New accounts start at 1 CPS and move to 2 CPS as spend grows (US accounts created before 1 October 2026 have custom limits), each outbound trunk defaults to 1 CPS, and calls above the limit are **rejected** with 5180, not queued; pace the dialer (<https://www.plivo.com/docs/voice-agents/sip-trunking/deploy/us-call-quality-and-cps>). Detail: "Security and limits" below.

## Stage 1: Account and number

```bash
plivo auth whoami -o json && plivo account get -o json          # right account? cash_credits > 0?
plivo numbers list --services voice -o json                       # or: plivo numbers search --country US --type local --limit 5, then plivo numbers buy <number> (preview first)
plivo numbers compliance list --country IN --status accepted -o json   # India only
```

**Check:** the number is on this account, voice-enabled, and (India) compliance is `accepted`.

## Stage 2: Inbound, Plivo side

Pick the URI from the table (detail and India variants in "Platforms" below; commands per platform in "Commands per platform"):

| Platform | URI | Outbound auth |
|---|---|---|
| LiveKit Cloud | `<livekit_sip_host>;transport=tcp` (`tls` for secure trunking) | credentials |
| ElevenLabs | `sip.rtc.elevenlabs.io:5060;transport=tcp` or `:5061;transport=tls` | credentials |
| Retell | `sip.retellai.com;transport=tcp` (or `tls`) | credentials, required by Retell |
| Vapi | `sip.vapi.ai;transport=udp` | IP list `44.229.228.186/32`, `44.238.177.138/32` |
| xAI Voice Agents | `sip.voice.x.ai;transport=tls` | none: xAI agents are inbound only |
| Other or self-hosted | `<host>[:port];transport=<what it documents>`; the API also accepts `sip:user@host` | credentials, or its static IPs |

Plivo publishes an integration guide for LiveKit, ElevenLabs, Retell, Vapi and xAI, and a generic guide for everything else. If the platform is not one of the five, say that Plivo does not document it, then use the "Other or self-hosted" row with the host, port and transport that platform publishes. Do not compose a hostname or guess a transport.

Sources: <https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/livekit>, <https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/elevenlabs>, <https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/retell>, <https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/vapi>, <https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/xai-voice-agents>, <https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/other-platforms>, <https://www.plivo.com/docs/sip-trunking/api/origination-uris>.

```bash
plivo sip uris create --name <name> --uri "<uri from the table>" --dry-run       # preview; then the same command without --dry-run (data: uri_uuid)
plivo sip uris get <uri_uuid> -o json                                            # read back what was stored
plivo sip trunks create --name <name> --direction inbound --uri <uri_uuid> --dry-run   # add --fallback-uri <uuid> as needed; inbound TLS is the URI's ;transport=tls; then run it without --dry-run
plivo numbers get <number> -o json                                               # note the current binding: your rollback value
plivo numbers update <number> --trunk-id <trunk_id> --dry-run                    # refuses an outbound trunk; then run it without --dry-run after the user approves
plivo numbers get <number> -o json                                               # confirm it now points at the trunk
```

Pass the number to `numbers` commands as digits without `+` (for example `14155551234`), as the CLI docs do; the CLI puts the argument into the URL path unchanged. `numbers update --trunk-id` reads the trunk first, also under `--dry-run`, and refuses an outbound trunk or one that is not on the account. `--app-id` skips that check, so use `--trunk-id`. The trunk create prints `trunk_domain`.

### Check the URI string before you create it

Would break or mis-route calls:

- It is a URL (`http://`, `https://`, or anything with a path). Plivo needs the SIP endpoint, not an answer URL.
- It contains a space, or the host has characters other than letters, digits, dots and hyphens, or there is no host at all.
- The port is not a number.
- `;transport=` is set to something other than `udp`, `tcp` or `tls`.
- The host is a private or unroutable address (10.x, 127.x, 192.168.x, 172.16 to 172.31, 169.254.x, 0.x). Plivo cannot reach it.
- A host that does not match the documented host for the platform you named.
- An Indian number with a non-India endpoint: Vapi is not supported at all, ElevenLabs must be the `in.residency` host, Retell is unconfirmed. For LiveKit, region pinning must be on; the docs accept a dedicated India URI or the project's standard endpoint with the India region enabled. Otherwise calls end 4590.

Fix before go-live:

- No `;transport=` at all, or a transport the platform does not document. LiveKit, ElevenLabs and Retell: `tcp`, or `tls` for secure trunking. Vapi: `udp`. xAI: `tls`. Plivo does not document the default, so always set it.
- A `sip:` or `sips:` scheme, or a user part, on a platform whose guide shows the host-first form. No page says it breaks, and the Plivo console fills `sip:sip.voice.x.ai;transport=tls` when you pick xAI, so accept that form for xAI; otherwise write what the platform's own guide shows.
- A port the platform's guide does not use. ElevenLabs publishes exactly two forms, `:5060;transport=tcp` and `:5061;transport=tls`; treat anything else as untested.
- Port 5061 without `transport=tls`. Some platform guides publish a TLS URI with no port; write what that guide shows.
- A parameter other than `transport=`, or a parameter with no value. Only `transport=` is documented.
- A host ending in a dot.
- An unknown platform with no expected host and transport to compare against: get both from the platform's own docs before creating the URI.

**Check:** `plivo numbers get <number> -o json` shows the trunk and the readiness checklist has no blocking answer. Add a fallback URI when the platform offers a second region.

## Stage 3: Inbound, platform side (their dashboard)

Plivo cannot see this, so ask and confirm (<https://www.plivo.com/docs/voice-agents/sip-trunking/getting-started/your-first-agent-call>): LiveKit inbound trunk listing `+<number>` **and** a dispatch rule (India: region pinning on); ElevenLabs number imported on a SIP trunk with an agent; Retell number imported (needs Stage 4 first) and an inbound agent bound; Vapi number registered with an assistant; xAI number added to the agent (Phone numbers, Add number, Direct SIP) with all 14 Plivo signaling ranges under Allowed addresses, or SIP digest credentials set on both sides; self-hosted: Plivo signalling allowed on 5060/5061 and media UDP 10000 to 30000, and **no digest challenge** to Plivo unless the same username and password are on the Plivo URI (`authentication_needed`), or the handshake cannot complete. Do not promise 4150 for it: the published row for that code is a carrier requiring proxy auth. When this step is missing the platform answers **404** (ElevenLabs `Does not match any SIP Trunks`; other platforms use their own phrase). The published 4090 row is `destination_not_found`, "No route to destination", with a carrier framing and no direction, so read an inbound 4090 alongside the SIP flow rather than as proof of a missing import. Nothing in the docs ranks inbound failure causes, so treat "most common" as this file's experience, not a published fact.

## Stage 4: Outbound

The password is the one thing that must never reach the chat, a file you keep, or the command line. Ask the user to set it in the shell that runs you without it landing in shell history (`read -rs SIP_PASSWORD && export SIP_PASSWORD`), or to run the credential command themselves. Rules: username 5 to 20 alphanumeric; password 5 to 20 characters, alphanumeric plus `~!@#$%^&*()_+`, at least one special character.

```bash
printf '%s' "$SIP_PASSWORD" | plivo sip credentials create --name <name> --username <username> --password-stdin --dry-run   # preview redacts the password
printf '%s' "$SIP_PASSWORD" | plivo sip credentials create --name <name> --username <username> --password-stdin             # after approval (data: credential_uuid)
#   or for Vapi:  plivo sip ip-acl create --name vapi --ip 44.229.228.186/32 --ip 44.238.177.138/32 --dry-run, then without --dry-run
plivo sip trunks create --name <name> --direction outbound --credential <credential_uuid> --dry-run   # or --ip-acl <uuid>; add --secure for TLS/SRTP
plivo sip trunks create --name <name> --direction outbound --credential <credential_uuid>             # after approval; prints trunk_domain = <trunk_id>.zt.plivo.com
```

Put `trunk_domain` (no `sip:`, no spaces), the credential **username** (not its name) and password into the platform's outbound trunk or number import. Caller ID = a Plivo number on this account. `--secure` means TLS signaling and SRTP media, so the platform must use TLS too (Retell "Outbound Transport = TLS", LiveKit secure trunking, TLS transport in ElevenLabs' outbound settings). xAI needs no outbound trunk.

**Check:** re-run the readiness checklist, including the outbound steps, with no blocking answer, and the first call's SIP flow ends in 100/183. On a trunk that authenticates by credentials you will normally see INVITE, 407, then a second INVITE carrying them: that is the digest handshake (<https://www.plivo.com/docs/sip-trunking/interconnection-guides/asterisk>). A trunk that authenticates by IP ACL has no challenge and no 407. Wrong credentials, or a platform source IP missing from the IP list, end as 4180 `call_rejected_unauthorized`. Do not whitelist `0.0.0.0/0` to "make it work".

## Stage 5: First call, both directions

Dial the number from a phone; then place one outbound call from the platform to your own phone (skip for xAI, which is inbound only). Then read what Plivo saw:

```bash
plivo sip calls list --limit 5 -o json                                   # newest records: hangup_cause_code, hangup_cause_name, hangup_source, transport_protocol, srtp
plivo sip calls get <call_uuid> -o json                                  # one record
plivo api GET /Zentrunk/Call/<call_uuid>/Insights/ -o json               # rtt, jitter, packet_loss, plivo_quality_score (no typed command)
```

`sip calls list` strips a leading `+` from `--from-number` and `--to-number`; a raw filter with `+` matches nothing. `plivo voice calls list|get` read the Voice API, not SIP trunking call records.

**Check:** `hangup_cause_code` 3000 or 3010 with a non-zero duration in both directions. The published table gives 3010 as `normal_hangup`, a normal hangup from the user, and nothing more, so a 3010 with 0 s duration is a call that ended at once without saying who ended it: read the platform logs and the SIP flow before concluding anything. Anything else: **Debugging** below.

## Stage 6: Transfer to a human with SIP REFER (only if you need it)

The platform sends `REFER` with `Refer-To: <sip:+14155551234@<trunk_id>.zt.plivo.com>`; Plivo answers `202`, dials the target, reports progress with `NOTIFY 100/180/200` (answer each with 200 OK), and sends your leg a `BYE` when the target answers (<https://www.plivo.com/docs/sip-trunking/concepts/sip-refer-inbound>). Rules: `Refer-To` **must** use your `.zt.plivo.com` trunk domain (other domains and arbitrary SIP URIs are blocked); no private IPs; E.164 target, else 400; UDP, TCP and TLS all fine; chained transfers allowed; **if the transfer target hangs up the whole call ends**; caller ID on the transfer leg is the caller's number on inbound calls and your Plivo number on outbound; do not send BYE before `NOTIFY 200 OK`; the destination country must be allowed in Geo Permissions; two call records. Some endpoints send `tel:` or `sips:<IP>` targets and Plivo may accept them; only the documented form is supported, so use it. Detail: "Transfer to a human with SIP REFER" below.

**Check:** one test transfer; `NOTIFY 200 OK` seen, then Plivo's BYE. After a `NOTIFY 4xx/5xx` the caller hears silence until the agent speaks, and callers commonly hang up within seconds: make the agent speak as soon as the sipfrag is a failure.

## Go-live

- Re-run the readiness checklist for both directions, including the reachability probe; no blocking answers, warnings understood.
- Concurrency and CPS sized for the traffic (step 7). Raise them with **Request Enterprise** under Organization settings > Account limits, or ask Buddy in the console; US limits also rise automatically with spend.
- Geo permissions trimmed to the countries you call (console only). Credentials never pasted anywhere; IP lists narrow.
- India: KYC accepted on every number, platform pinned to India, consent captured. US: Plivo caller ID, dialer under the CPS limit.
- A fallback URI, and a `secure` decision made on both sides.
- Watch daily: `plivo sip calls list --hangup-cause-code 4090 --limit 20 -o json` (and 4170, 4180, 4590, 4030, 5180, 5190). Outbound agents see 4410, 4340 and 4550 every day; those are outcomes, not faults, but every unanswered US attempt counts toward the abandoned-call threshold.

## Debugging with the CLI, in this order

1. `plivo sip calls list --limit 20 -o json` (filters: `--direction`, `--hangup-cause-code`, `--hangup-source customer|carrier|zentrunk`, `--from-number`, `--to-number`, `--since`, `--until`); then `plivo sip calls get <uuid> -o json` and the Insights call above.
2. Map the code with the table below, or with the full table in "Zentrunk hangup codes" at the end. `hangup_source`: `customer` = your platform, `carrier` = network side, `zentrunk` = Plivo.
3. Console Zentrunk, Logs, the call: **Call Stats** (trunk, transport, secure) and **SIP logs** (message flow, final response, PCAP). Trust the hangup code for Plivo's conclusion and the SIP flow for what the platform said; they can disagree. Read it in three lines (observed readings, not a published mapping): inbound with no INVITE towards your URI usually means Plivo refused (4590, 4030 or 4310); INVITE repeated with no reply usually ends as 4170; the platform's own 4xx is the answer (404 usually means not imported, 401/407 means your server challenged Plivo, 486 usually means no dispatch rule or agent, 503 means the platform is down). Outbound on a credential-authenticated trunk: one 407 then a second INVITE is normal; a flow that **ends** at 407, or a 4180, means the credentials or the IP list are wrong.
4. `plivo sip calls diagnose <uuid>` is the trunk-call diagnose command (`plivo voice calls diagnose` refuses trunk UUIDs). The CLI's own release notes say its server side is not live yet, so it may report that it cannot retrieve the call; use `plivo sip calls get` in the meantime. It shares a rate limit with `plivo ask`; do not loop it. Then `plivo ask "..."` or Plivo support with the UUID and the PCAP.

The public hangup-code table publishes a code, a name, a cause and a fix. It does not publish the SIP response that goes with each code. The **SIP seen** column below is therefore observational, gathered from SIP flows, not a Plivo contract: use it to recognise a flow you are already looking at, never as the reason for a conclusion. Where a row names a code as documented it is from the public table.

| Code / cause | SIP seen (observed, not published) | Owner | Fix |
|---|---|---|---|
| in 4090 destination_not_found | platform 404. The phrase is the platform's own; Plivo's docs record only ElevenLabs `Does not match any SIP Trunks` (<https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/ai-coding-agent>) | platform config | import `+<number>`, dispatch rule or agent on the platform |
| in 4170 request_timeout_customer | INVITE repeated, no reply (or Plivo 408) | platform config | URI host, port, `;transport=`; reachability probe; firewall; platform online |
| in 4150 proxy_authentication_required | platform 401/407 | the published row is a **carrier** requiring proxy auth; reading it as your platform challenging Plivo is observational | stop challenging Plivo, or URI `authentication_needed` plus the same credentials |
| in 4410 user_busy | platform 486 | the published table says only `user_busy`, destination is busy | inbound: check the dispatch rule, agent availability and concurrency on the platform, because it is the destination. Outbound this is a normal outcome. The code alone does not assign ownership; read the SIP flow |
| in 5360 / 4350 | platform 503 / 480 | platform config | platform status; fallback URI; TLS consistency |
| in 4420 carrier_cancelled | caller CANCEL, 487 (a racing platform 404 gets the 4090 fix) | caller | nothing; answer faster if ringing is long |
| any 4590 domestic_anchored_terms_not_met | 403 from Plivo, no platform leg on inbound (observed; 403 is documented for 4650, not for this code) | Plivo policy (India) | the SIP trunking page's cause is that the orchestration platform does not terminate SIP and media in India; the Voice API pages phrase the same rule as both legs staying in India. Move the platform endpoint and both legs into India. KYC is a separate gate, so check it too, but this code does not report it |
| any 4030 insufficient credits | from Plivo | customer Plivo config | credits, auto-recharge |
| in 4310 uri_not_found | 404 from Plivo | customer Plivo config | set `primary_uri_uuid` |
| out, flow ends at 407 (any code) | 407 with no second INVITE | platform config | credential username (not name) and password, or the platform IPs in the IP list |
| out 4180 call_rejected_unauthorized | 403 from Plivo | platform config | wrong credential username or password, or a platform source IP missing from the IP list |
| out 4000 bad_request | 400 | platform config per the docs | inspect the packet, do not retry without fixing. Commonly seen, not documented: the 400 arrives after 183 ringing; check the flow before assuming a syntax error, and collect the UUID for Plivo support |
| out 4190 unknown_caller_id | 403 from Plivo | platform config | caller ID = Plivo number on this account |
| out 4560 barred_country (docs also list 4650) | 403 Barred | customer Plivo config (geo permissions) | console Zentrunk, Geo Permissions |
| out 4570 barred_number | 403 Barred | Plivo policy | drop the number, or Plivo support if it is legitimate |
| out 4100 prefix_not_supported | 404 Prefix Not Supported | platform config (dial list) | E.164 with country code |
| out 4410 / 4340 / 4550 | 486 / 480 / platform CANCEL | carrier or platform timeout | normal outcomes; very short 4550 cancels count as abandoned calls (US) |
| out 4090 / 4160 / 5000 / 5300 / 5350 / 6000 / 6040 | 404 / 503 / 502 | carrier/destination | retry; Plivo support if concentrated on one destination |
| out 4630 / 4370 | 488 / 482 | carrier/destination | 4630: offer PCMU/PCMA and fix the secure flag; 4370: remove the routing loop. See the full table |
| out 5180 cps_limit_reached | 503 from Plivo | platform config (pacing) | pace the dialer; raise CPS with Request Enterprise or Buddy in the console |
| any 5190 concurrent_call_limit_exceeded | rejected at once | Plivo policy | reduce concurrency or raise the limit (Organization settings > Account limits) |
| any 5220 service_interrupted_by_customer | 200 then platform 4xx (or no answer to a mid-call request) | platform config | platform logs at the drop time; re-INVITE and UPDATE handling |
| any 3040 abnormal_hangup_due_to_reinvite | re-INVITE rejected | platform config | what changed mid-call. The published row says only "Re-invite rejected, check for changes in session parameters during call"; the transferred-leg association is observational, so collect Call-ID and PCAP for Plivo support rather than assuming a transfer caused it |
| transfer: `NOTIFY 4xx/5xx` (docs examples 486, 408; 480 and 503 also seen. No hangup code; the agent leg stays up) | REFER 202, then the sipfrag | transfer target, or geo permissions | agent speaks at once, retries or continues |
| transfer: `NOTIFY 100 Trying` then nothing | agent leg ended early | platform config | keep the leg up until `NOTIFY 200` or a failure |

## What not to do

- Don't leave `;transport=` off the URI because LiveKit happens to answer over UDP. Write what the platform documents.
- Don't skip the platform-side import because the Plivo side is green. The platform's 404 is the inbound failure this file sees most, though nothing in the docs ranks causes.
- Don't paste an answer URL into the origination URI field. For the documented platforms, write the URI form their own guide shows.
- Don't whitelist `0.0.0.0/0` (or any `/0`) in an IP list to get outbound working; use credentials. The docs do not warn about this explicitly, so it is this file's rule, not a Plivo one.
- Don't turn on secure trunking on one side only. Secure trunking (`--secure`) is documented for outbound trunks and means TLS signaling and SRTP media (<https://www.plivo.com/docs/sip-trunking>, "Secure trunking on outbound trunks"); the platform's outbound calls must use TLS too. Inbound TLS is the URI's `;transport=tls`.
- Don't run an Indian number without a linked, accepted KYC application, and don't dial outside India from it.
- Don't put numbers without a country code in the dial list (4100).
- Don't read a 407 as an auth failure. On a credential-authenticated trunk, one 407 followed by a second INVITE is the digest handshake. An IP ACL trunk is not challenged and shows no 407, so do not treat a missing 407 as a fault either.
- Don't treat outbound 4410, 4340 and 4550 as configuration problems; they are normal outbound outcomes, though each unanswered US attempt counts toward the abandoned-call threshold. Inbound 4410 points at the platform because the platform is the destination, but the published table says only `user_busy`, so confirm with the SIP flow before assigning blame. Do treat a 4550 within a second or two as a dialer timeout bug.
- Don't retry a 4000 without reading the SIP flow first.

- Don't preview and write in one shell command, and don't pass `--dry-run` to a delete expecting a preview: run the delete without `--yes` to see what it would detach.

## India readiness for SIP trunking agents

Treat every item as blocking until proved. The items below are what a SIP trunk needs; each is enough to work from on its own.

1. The business is registered in India and the Plivo organisation uses the **India data region**. The data region cannot be changed; create a new organisation if needed. Not readable over the API: confirm in the console. (<https://www.plivo.com/docs/voice/concepts/india-calling>, <https://www.plivo.com/docs/voice-agents/sip-trunking/deploy/calling-in-india>)
2. A compliance (KYC) application in `accepted` status is attached to the number. Draft, submitted, rejected, suspended, expired or missing is not enough; renting and calling both need `accepted`. (<https://www.plivo.com/docs/numbers/rent-india-numbers>, <https://www.plivo.com/docs/numbers/compliance>)

   The whole flow runs from the CLI, and one rule governs it: `plivo numbers compliance requirements --country IN --number-type local --user-type business -o json` returns the document types required right now, and you supply exactly those. The public pages disagree on how many documents are needed, so never hard-code one or two: the live requirements response is the source of truth. The certificate files and the exact legal details (legal business name as printed on the certificate, CIN or Udyam registration number, GSTIN, registered address, contact email) come from the user and you must never fill them in or invent them. The first application must be sealed and signed by an authorised signatory. Then, previewing each writing step first:

   ```bash
   plivo numbers compliance requirements --country IN --number-type local --user-type business -o json   # read-only
   plivo numbers compliance create --data @app.json --file 'documents[0].file=@first_document.pdf' --dry-run
   plivo numbers compliance create --data @app.json --file 'documents[0].file=@first_document.pdf' --yes -o json
   plivo numbers compliance get <compliance_id> --expand documents -o json    # read-only; poll until it leaves "submitted"
   plivo numbers compliance link --link +9180XXXXXXXX=<compliance_id> --dry-run
   plivo numbers compliance link --link +9180XXXXXXXX=<compliance_id> --yes
   plivo numbers compliance list --country IN --status accepted -o json       # read-only; confirms the state
   ```

   Review for 022 and 080 landline numbers is automated and usually takes a few minutes. `submitted` is not `accepted`. If a create is rejected, read `rejection_reason`, fix the document or the field and run `compliance update`, which replaces every document, so re-attach them all. A compliance application is a regulatory filing: submit it only when the user has explicitly said go.
3. The caller ID is a Plivo-rented India number. Using one on the same account is this file's recommendation, not a rule the India page states.
4. Plivo and the AI platform terminate SIP and media in India; violation fails with **4590**. The Voice API name for the same rule is `violates_media_anchoring`; SIP trunking uses 4590. (<https://www.plivo.com/docs/voice-agents/sip-trunking/deploy/calling-in-india>)

Items 5, 6 and 8 come from the Voice API India pages, not from any SIP trunking page: the SIP trunking India checklist covers account region, a KYC'd number, platform region, the trunk URI and a test call. They are number-level regulatory rules, so they still apply to a number on a trunk, but say where they come from.

5. Number series: landline (022, 080) for service and transactional calls only; 140 for promotional calls only; 160 for BFSI service and transactional calls only. Using the wrong series is itself a violation, and complaints from such calls count as UCC even with consent. 140 and 160 numbers are not provisioned through the compliance API: they go through Tata DLT registration, a signed declaration, a NOC per number and voice header and template approval, which takes several business days and runs through Plivo support. There is no CLI for that path.
6. Explicit digital consent for every commercial call; cold calling is prohibited; complaints are treated as UCC. Complaints arrive on the console UCC dashboard and through the UCC API (`plivo api GET /Ucc/` and `plivo api GET /Ucc/<reference_id>/` to read them). Submitting opt-in proof takes a `multipart/form-data` upload with a `file` part (<https://www.plivo.com/docs/numbers/ucc>). No CLI command builds that multipart upload, so upload proof on the console UCC dashboard. Opt-in proof is due within a few business days of a complaint, unresolved complaints block the compliance application, and repeated complaints suspend it. Remove a complainant from the list at once; calling them again is itself a violation. Read <https://www.plivo.com/docs/voice/concepts/ucc-management> for the current timers rather than quoting one from memory.
7. Platform support: LiveKit via region pinning; ElevenLabs via an India deployment and `sip.rtc.in.residency.elevenlabs.io:5060;transport=tcp`; **Vapi not supported**; Retell: confirm with Retell, Plivo's docs do not verify it.
8. Inbound to India: India to India. Outbound: Indian number to an Indian destination.

Commonly seen, not documented, and safe to check:

- The documented causes of 4590 are a call leg outside India and a conference that mixes PSTN and WebRTC legs. Check KYC as its own gate rather than inferring it from this code.
- LiveKit India: turn on region pinning, then copy the endpoint LiveKit shows. The docs accept a dedicated India URI or the project's standard endpoint with the India region enabled. Do not compose a hostname from parts.

The readiness checklist proves the number's state from the Number API, and `plivo numbers compliance list --country IN --status accepted` proves the application state. It cannot infer the organisation's data region or where the platform terminates media; those are manual confirmations. `compliance_status` on the number record is returned by the live API but is not in the published phone number schema, so treat its absence as "not known".

## Security and limits: geo permissions, caller ID, secure trunking, IP lists, CPS, concurrency

Docs: <https://www.plivo.com/docs/sip-trunking/concepts/geo-permissions>, <https://www.plivo.com/docs/sip-trunking/concepts/stir-shaken>, <https://www.plivo.com/docs/sip-trunking/concepts/technical-specifications>, <https://www.plivo.com/docs/sip-trunking/concepts/account-limits>, <https://www.plivo.com/docs/sip-trunking/api/trunks>, <https://www.plivo.com/docs/sip-trunking>, <https://www.plivo.com/docs/voice-agents/sip-trunking/deploy/us-call-quality-and-cps>.

### Geo permissions (outbound)

- Console only: Zentrunk, Geo Permissions. All countries allowed by default; deselect the ones you never call; changes apply immediately. No API endpoint is documented.
- High Risk Permissions toggle (on by default) blocks premium and high-risk prefix groups.
- A blocked destination fails with SIP `403 Barred Country`; the geo-permissions page says code **4650**, the hangup table lists **4560 `barred_country`** and 4650. 4570 `barred_number` is a Plivo block on one number (Plivo policy; contact support), not a geo-permissions setting.
- For an agent that calls one country, turn every other country off before go-live. A leaked credential then costs one country's rates.

### Caller ID and STIR/SHAKEN (US and Canada)

- The technical-specifications page requires a Plivo number as caller ID for all outbound calls; the generic guide also allows a verified caller ID. Use a Plivo number rented on the same account: it is always accepted and is signed A / Verified. Anything else risks **4190 `unknown_caller_id`**.
- STIR/SHAKEN is automatic. A call with a Plivo DID from the same account is signed A / Verified; anything else is B or C / Not Verified; non-US destinations show Not Applicable.
- Call record fields: `stir_verification`, `attestation_indicator` on `plivo sip calls get <uuid> -o json`; filter with `plivo sip calls list --stir-verification "Not Verified"`.

### CPS and concurrency

- US data region: new accounts start at 1 CPS and move to 2 CPS as spend grows. India Professional accounts start at 2 CPS. Each outbound trunk defaults to 1 CPS, allocated from the account pool; inbound trunks have no CPS limit. Both levels are enforced.
- Concurrency has tiers by plan (US: Free Tier 2, Professional 5 to 25, Enterprise 50; India Professional starts at 50). US accounts created before 1 October 2026 have custom limits instead. Every PSTN leg counts, inbound and outbound, across SIP Trunking and Voice API; REFER legs count too.
- SIP Trunking **rejects** every call above a limit, with 5180 (CPS) or 5190 (concurrency). It never queues. Pace the dialer.
- See your concurrency limit under Organization settings > Account limits. Raise limits with Request Enterprise there, or ask Buddy in the console.
- US quality thresholds (abandoned and short calls) are on the US call quality page; every unanswered attempt counts as abandoned.

### Secure trunking (TLS plus SRTP)

- Secure Trunking is documented for outbound trunks: `--secure` means TLS signaling and SRTP media for that trunk. Mirror it on the platform's outbound side: LiveKit secure trunking, Retell Outbound Transport = TLS, TLS transport in ElevenLabs' outbound settings.
- Inbound: `;transport=tls` in the URI, with the port the platform's guide writes. Outbound: the platform dials `<trunk_id>.zt.plivo.com` over TLS.
- Mismatch: **4110 `secure_trunking_disabled`** is documented for TLS or SRTP used against a trunk without Secure Trunking. The reverse mismatch, a secure trunk with a platform on TCP, is documented in the Retell guide: "Calls drop or have no audio".

### IP lists and credentials

- An IP ACL is a source-IP allow list for the outbound trunk. `0.0.0.0/0` or `128.0.0.0/1` means anyone who learns the trunk domain can place calls on your account. `plivo sip ip-acl create` warns on such ranges but does not block them; block them yourself.
- `plivo sip ip-acl update` replaces the whole list: pass every address you want kept.
- Credential passwords: stdin only (`--password-stdin`), never returned, and `plivo sip credentials update` requires a new one on every update. Use the credential **username** on the platform, not the credential name.
- Plivo signaling and media: ports 5060 (UDP/TCP), 5061 (TLS), media 10000 to 30000. Allow all 14 published signaling ranges, because calls can route through another region during an incident (<https://www.plivo.com/docs/sip-trunking>, "Signaling / Media IP addresses"): North California 13.52.9.0/25, 216.120.187.128/26; Virginia 18.214.109.128/25, 18.215.142.0/26, 204.89.148.128/26; Frankfurt 3.120.121.128/26; São Paulo 18.228.70.64/26, 54.233.191.0/27; Sydney 13.238.202.192/26; Singapore 18.136.1.128/26, 204.89.149.128/27; India 15.207.90.192/31, 204.89.151.128/27, 204.89.151.160/27.

### Not available over the API (console or support only)

Geo permissions, account limits, premium-number unblocking, the SIP flow and PCAP (console Zentrunk, Logs), the account data region. UCC proof upload is in the API as a multipart upload, but no CLI command builds it, so use the console dashboard.

## Platforms: what goes in the Plivo URI, how the platform authenticates, what to do on its side

Anything not in Plivo's docs is marked **not in docs**.

| Platform | Inbound URI (create with `plivo sip uris create`) | India | Outbound auth | `secure` | Platform-side step, inbound | Platform-side step, outbound |
|---|---|---|---|---|---|---|
| **LiveKit Cloud** | `<livekit_sip_host>;transport=tcp` (your project's SIP endpoint). `;transport=tls` for secure trunking | Enable region pinning on the project, then copy the endpoint LiveKit shows (a dedicated India URI or the standard endpoint with the India region enabled) | Credentials. The docs' API example sets `"secure": true`; then enable secure trunking in LiveKit too | recommended | LiveKit **inbound trunk** listing the Plivo number and a **dispatch rule** | LiveKit outbound trunk with address `<trunk_id>.zt.plivo.com`, the credential username and password |
| **LiveKit self-hosted** (not in docs) | `<your-sip-host>[:5060];transport=tcp` or `:5061;transport=tls` | Your servers must be in India | Credentials or an IP list of your egress IPs | both sides must agree | Allow Plivo signalling IPs; if your server challenges Plivo, set the same username and password on the Plivo URI (`authentication_needed`) | same as Cloud |
| **ElevenLabs** | `sip.rtc.elevenlabs.io:5060;transport=tcp` or `sip.rtc.elevenlabs.io:5061;transport=tls` | `sip.rtc.in.residency.elevenlabs.io:5060;transport=tcp` after ElevenLabs sets up an India deployment | Credentials; secure trunking recommended | recommended | Import the Plivo number on a SIP trunk and link an agent. A `404 Does not match any SIP Trunks` means this step is missing | Termination domain `<trunk_id>.zt.plivo.com` plus the credentials in the number's outbound settings |
| **Retell** | `sip.retellai.com;transport=tcp` (or `;transport=tls`, TLS 1.2+) | Plivo's docs do not verify an India endpoint; confirm with Retell first | Outbound trunk **required**: Retell will not import a number without its termination URI, even inbound only. Create the credential and enter its username and password in Retell's import | off by default; if on, set Outbound Transport = TLS in Retell | Import the number (Connect via SIP trunking) with termination URI `<trunk_id>.zt.plivo.com` (no `sip:`), username, password, transport; bind an inbound agent | Bind an outbound agent. Retell cannot edit an imported number: delete and re-import |
| **Vapi** | `sip.vapi.ai;transport=udp` | **Not supported** | IP list `44.229.228.186/32`, `44.238.177.138/32` | no | Register the number in Vapi (BYO SIP trunk) and assign an assistant | Vapi dials `<trunk_id>.zt.plivo.com` from the two IPs |
| **xAI Voice Agents** | `sip.voice.x.ai;transport=tls` | Not stated in the guide; confirm with xAI | None: xAI agents are inbound only, so no outbound trunk | not used (inbound only; TLS is in the URI) | Add the Plivo number to the agent, and allow all 14 Plivo signaling ranges (or set the same SIP digest credentials on the Plivo URI and in xAI) | none |
| **Other or self-hosted** | The platform's SIP host and the transport **it** documents: `host[:port];transport=...`. The generic integration guide's TLS example uses port 5061 and the technical specifications list 5061 for TLS, so prefer 5061 when the platform does not say otherwise. Some platform guides publish a TLS URI with no port at all, so do not treat 5061 as mandatory: write what that platform's own Plivo integration guide shows. The API also accepts `sip:user@host` | Must terminate SIP and media in India | Credentials if it supports digest auth or has dynamic IPs; IP list only for published static IPs | both sides must agree | Register the Plivo number and route it to an agent | Termination domain plus credentials; caller ID = a Plivo number on the account; OPTIONS pings at most one per 10 to 15 s, to the outbound trunk only |

Sources: <https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/livekit>, <https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/elevenlabs>, <https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/retell>, <https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/vapi>, <https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/xai-voice-agents>, <https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/other-platforms>, <https://www.plivo.com/docs/voice-agents/sip-trunking/deploy/calling-in-india>, <https://www.plivo.com/docs/sip-trunking/api/origination-uris>.

### Platforms Plivo does not document

Plivo publishes SIP trunking integration guides for LiveKit, ElevenLabs, Retell, Vapi and xAI, plus one generic guide for everything else. Some platforms customers ask about, such as OpenAI Realtime, have no Plivo SIP trunking guide; Plivo documents OpenAI Realtime over audio streaming (`plivo skill install audio-streaming`).

For those, say so plainly, then follow the "Other or self-hosted" row. Get the SIP host, the port and the transport from that platform's own documentation, and get the corresponding Plivo-side requirements from Plivo. Do not invent a hostname, a transport, or a field the Origination URI API does not document: it documents `name`, `uri`, `authentication_needed`, `username` and `password`, which map to the `plivo sip uris create` flags. If a platform needs something outside that set, that is a question for Plivo before it is a request body.

### Transport parameter

- "The transport parameter must match what your platform expects. A mismatch is the most common reason an inbound integration fails silently." (<https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/other-platforms>)
- LiveKit, ElevenLabs, Retell: TCP by default, TLS for secure trunking. Vapi: UDP. Plivo does not document the default when `;transport=` is missing, so always set it.
- LiveKit commonly answers over UDP when the parameter is missing, so treat that as a warning rather than a blocker; write what the docs say for new setups. A missing transport parameter is a common cause of inbound calls that get no reply.
- The SIP trunking API page shows `sip.livekit.cloud:5060` and `sip.vapi.ai:5060` without a transport and Vapi with `authentication_needed: true`; the integration guides disagree and are what this skill follows.

### Outbound authentication and the digest handshake

Prefer credentials for LiveKit Cloud, ElevenLabs and Retell; use Vapi's two published IPs for Vapi. An IP list containing `0.0.0.0/0` lets anyone who learns your trunk domain place calls on your account. The IP ACL page publishes no prohibition, so this is this file's rule, not a Plivo one.

The docs describe the handshake in words: the trunk challenges the INVITE and the platform's configured auth section answers it (<https://www.plivo.com/docs/sip-trunking/interconnection-guides/asterisk>). On the wire that is Plivo answering the INVITE with 407 and the platform re-sending it with credentials. The 407 code and the two-INVITE sequence are commonly seen, not written down in Plivo's docs. A flow that **ends** at 407 means the platform never authenticated; Plivo records wrong credentials, and a source IP missing from the IP list, as 4180 `call_rejected_unauthorized`. Fix: the credential username (not its name) and password on the platform, or add the platform's egress IPs to the IP list.

### Questions to ask because Plivo cannot see them

| Platform | Ask |
|---|---|
| LiveKit | Does the LiveKit inbound trunk list `+<number>` exactly, with a dispatch rule? Region pinning on for Indian numbers? |
| ElevenLabs | Is `+<number>` imported on a SIP trunk with an agent? For India, was the trunk created against the `in.residency` host? |
| Retell | Is `+<number>` imported with termination URI `<trunk_id>.zt.plivo.com` and the credential username, and is an inbound agent bound? |
| Vapi | Is `+<number>` registered in Vapi with an assistant? (Indian numbers will not work.) |
| xAI | Is `+<number>` added to the xAI agent, and are all 14 Plivo signaling ranges allowed (or digest credentials set on both sides)? |
| A platform Plivo does not document | What SIP host, port and transport does it publish, and what does it need on the Plivo side? Confirm both with the platform and with Plivo before creating anything. |
| Self-hosted | Is Plivo allowed through your firewall on 5060/5061 and UDP 10000 to 30000, and does your server accept INVITEs from Plivo without a digest challenge, or are the same credentials on the Plivo URI? |

## Commands per platform

Order: URI, inbound trunk, attach the number; credential or IP list, outbound trunk. Preview each with `--dry-run`, show it, ask, then run it without `--dry-run`. Replace every `<...>` placeholder. The password comes from `$SIP_PASSWORD`, set by the user (Stage 4).

**LiveKit Cloud** (India: the endpoint your region-pinned project shows)

```bash
plivo sip uris create --name livekit-primary --uri "<livekit_sip_host>;transport=tcp"          # ;transport=tls for secure trunking
plivo sip trunks create --name livekit-inbound --direction inbound --uri <uri_uuid>
printf '%s' "$SIP_PASSWORD" | plivo sip credentials create --name livekit-out --username <username> --password-stdin
plivo sip trunks create --name livekit-outbound --direction outbound --credential <credential_uuid> --secure
```

**ElevenLabs** (India: `sip.rtc.in.residency.elevenlabs.io:5060;transport=tcp`; set `--secure` only when ElevenLabs' outbound settings use TLS: Plivo's guide recommends Secure Trunking but names no ElevenLabs-side step, so check ElevenLabs' own Plivo guide)

```bash
plivo sip uris create --name elevenlabs-primary --uri "sip.rtc.elevenlabs.io:5060;transport=tcp"   # or "sip.rtc.elevenlabs.io:5061;transport=tls"
plivo sip trunks create --name elevenlabs-inbound --direction inbound --uri <uri_uuid>
printf '%s' "$SIP_PASSWORD" | plivo sip credentials create --name elevenlabs-out --username <username> --password-stdin
plivo sip trunks create --name elevenlabs-outbound --direction outbound --credential <credential_uuid>   # add --secure with TLS on the ElevenLabs side
```

**Retell** (needs the outbound trunk even for inbound only; set `--secure` only with Outbound Transport = TLS in Retell)

```bash
plivo sip uris create --name retell-primary --uri "sip.retellai.com;transport=tcp"
plivo sip trunks create --name retell-inbound --direction inbound --uri <uri_uuid>
printf '%s' "$SIP_PASSWORD" | plivo sip credentials create --name retell-out --username <username> --password-stdin
plivo sip trunks create --name retell-outbound --direction outbound --credential <credential_uuid>
```

**Vapi** (source IPs, no credential; not for Indian numbers)

```bash
plivo sip uris create --name vapi-primary --uri "sip.vapi.ai;transport=udp"
plivo sip trunks create --name vapi-inbound --direction inbound --uri <uri_uuid>
plivo sip ip-acl create --name vapi-source-ips --ip 44.229.228.186/32 --ip 44.238.177.138/32
plivo sip trunks create --name vapi-outbound --direction outbound --ip-acl <ipacl_uuid>
```

**xAI Voice Agents** (inbound only; no outbound trunk)

```bash
plivo sip uris create --name xai-primary --uri "sip.voice.x.ai;transport=tls"
plivo sip trunks create --name xai-inbound --direction inbound --uri <uri_uuid>
```

**Other or self-hosted** (host and transport from the platform's own docs; a fallback URI when it has a second region; an IP list only for published static egress IPs)

```bash
plivo sip uris create --name platform-primary --uri "<documented sip host>[:port];transport=<udp|tcp|tls>"
plivo sip uris create --name platform-fallback --uri "<second host or region>;transport=<same>"
plivo sip trunks create --name platform-inbound --direction inbound --uri <uri_uuid> --fallback-uri <fallback_uri_uuid>
printf '%s' "$SIP_PASSWORD" | plivo sip credentials create --name platform-out --username <username> --password-stdin
plivo sip trunks create --name platform-outbound --direction outbound --credential <credential_uuid>
```

If the platform challenges Plivo on inbound, put the platform's inbound username and password on the URI. That is a different secret from the outbound credential, so keep it in its own variable: `printf '%s' "$PLATFORM_SIP_PASSWORD" | plivo sip uris create --name <name> --uri "<uri>" --authentication-needed --username <username> --password-stdin`.

Attach the number, for every platform:

```bash
plivo numbers get <number> -o json                                   # note the current application: rollback
plivo numbers update <number> --trunk-id <inbound trunk_id> --dry-run
plivo numbers update <number> --trunk-id <inbound trunk_id>          # after approval
plivo numbers get <number> -o json                                   # confirm it now points at the trunk
```

For a platform Plivo does not document, ask it for the exact SIP host, port and transport for inbound and the source IPs or credentials it uses outbound; run the URI checklist in stage 2; create the URI, read it back with `plivo sip uris get`, and only then attach a trunk. Tell the customer plainly that Plivo publishes no guide for their platform.

## Transfer to a human with SIP REFER

Docs: <https://www.plivo.com/docs/sip-trunking/concepts/sip-refer>, <https://www.plivo.com/docs/sip-trunking/concepts/sip-refer-inbound>, <https://www.plivo.com/docs/sip-trunking/concepts/sip-refer-outbound>, <https://www.plivo.com/docs/sip-trunking/concepts/technical-specifications>.

### How it works (same for inbound and outbound calls)

1. The call is up between the caller and your platform over the Plivo trunk.
2. Your platform sends `REFER` with `Refer-To: <sip:+14155551234@<trunk_id>.zt.plivo.com>`.
3. Plivo answers `202 Accepted`, then `NOTIFY 100 Trying`, `180 Ringing` while the target rings. Answer every NOTIFY with `200 OK`.
4. Target answers: `NOTIFY 200 OK`; Plivo bridges caller and target and sends your leg a `BYE`.
5. Target busy or no answer: `NOTIFY 4xx/5xx`. The documented examples are `486 Busy Here` and `408 Request Timeout`; `480` and `503 Transfer Failed` are also commonly seen but are not in Plivo's docs. The caller is still on your leg; retry another number or keep talking.

**Do not send BYE before `NOTIFY 200 OK`**; it drops the caller mid-transfer.

### Rules (the readiness check for a transfer)

- `Refer-To` must use **your** trunk domain, ending in `.zt.plivo.com`. Other domains and arbitrary SIP URIs are blocked.
- No private IP targets.
- The target is an E.164 number reachable through your trunk; a missing or malformed `Refer-To` gives `400 Bad Request`.
- Works on UDP, TCP and TLS trunks.
- Plivo handles codec re-negotiation between the legs.
- Caller ID on the transfer leg: inbound calls show the original caller's number; outbound calls show your Plivo number.
- Chained transfers allowed; each REFER replaces the previous transfer leg.
- **If the transfer target hangs up, the whole call ends.** The caller is not reconnected to your endpoint.
- International targets must be enabled in Geo Permissions.
- Two call records: the original leg and the transfer leg; allow up to 60 s for the transfer record.
- Outbound prerequisite: at least two Plivo numbers (customer leg caller ID and transfer destination).
- REFER is sent by your endpoint. `NOTIFY` is listed as not supported on the technical-specifications page, which means Plivo does not accept NOTIFY from you; Plivo does send NOTIFY to report progress.

The documented outbound target form is `sip:+E164@<trunk_id>.zt.plivo.com`. Write that. Some endpoints emit `tel:+E164` or `sips:+E164@<IP>` instead; Plivo publishes nothing about those, so do not assume either works.

### Design rules for the agent

- After a `NOTIFY 4xx/5xx` the caller hears silence until the agent speaks. Callers commonly hang up within seconds. Make the agent say something ("I could not reach a colleague, let me help") as soon as the sipfrag is a failure, then retry or continue.
- A dialog that ends after `NOTIFY 100 Trying` means the agent leg was dropped before a result. Keep the leg up until `NOTIFY 200` or a failure.
- Hold music while the target rings: play it from your endpoint before sending REFER; the caller otherwise hears the target's ring-back.
- If DTMF is not recognised after a transfer to an IVR, custom `X-` headers do not reach the target, or a re-INVITE on the transferred leg is rejected (3040, 5220): collect the Call-ID and the PCAP from the console SIP logs and contact Plivo support. Plivo's documented custom-header contract is the Voice API `X-PH-` prefix (<https://www.plivo.com/docs/voice/use-cases/pass-custom-headers>), not a SIP trunking page.

### Platform notes (their docs, not Plivo's; not verified here)

- LiveKit: `TransferSIPParticipant` sends the REFER; give it the trunk-domain form.
- ElevenLabs: the transfer-to-number tool over SIP trunking sends REFER through the same trunk.
- If your platform can only transfer by dialling a new call, the alternative is a warm transfer inside the platform (a second outbound call through the same trunk, bridged by the agent). That costs a second billed leg and keeps the AI in the media path.

### Troubleshooting

| Symptom | Check |
|---|---|
| `403 Forbidden` to REFER | `Refer-To` domain is not your `.zt.plivo.com` trunk domain, or a private IP, or the country is barred in Geo Permissions |
| `400 Bad Request` | `Refer-To` missing or not `sip:+E164@<trunk>.zt.plivo.com` |
| `202` then the caller is dropped | your endpoint sent BYE before `NOTIFY 200 OK` |
| `202` but no NOTIFY arrives | your endpoint is not listening on the address it advertised in `Contact` |
| `NOTIFY 4xx/5xx` (documented examples 486, 408; 480 and 503 also commonly seen) | target busy, unreachable or not answering; the caller hears silence until the agent speaks |
| `NOTIFY 100 Trying` then nothing | the agent leg ended early; keep it up until a final NOTIFY |
| No audio after transfer | codec mismatch between legs; Plivo support with the Call-ID |

## The `plivo sip` reference

The CLI wraps every Zentrunk object (`plivo sip`, alias `sip-trunking`; `plivo sip <group> <verb> --help` is the source of truth). Account-scoped JSON output is `{"data": ...}`; list commands return `data.meta` and `data.objects`.

| Object | Commands | Notes |
|---|---|---|
| Origination URI | `plivo sip uris create --name --uri [--authentication-needed --username --password-stdin]`, `list`, `get <uri_uuid>`, `update <uri_uuid> [--uri --name --authentication-needed=<bool> --username --password-stdin]`, `delete <uri_uuid>` | Quote `--uri`. A password rotation also restates `authentication_needed` and the username; to preview it with `--dry-run`, pass `--username` too. **Deleting a URI deletes every trunk that uses it** as primary or fallback; run the delete without `--yes` first to see them |
| Trunk | `plivo sip trunks create --name --direction inbound\|outbound [--uri --fallback-uri \| --credential --ip-acl] [--secure]`, `list [--direction]`, `get <trunk_id>`, `update <trunk_id> [--status enabled\|disabled --secure=<bool> --uri --fallback-uri --credential --ip-acl --name]`, `delete <trunk_id>` | Create checks the conditional fields locally and prints `trunk_domain`. Update reads `trunk_direction` from the trunk (the API needs it on every update). Delete reports how many numbers it detaches |
| Credential | `plivo sip credentials create --name --username --password-stdin`, `list`, `get`, `update <uuid> --password-stdin [--username --name]`, `delete` | The password is stdin only and never printed; `--dry-run` redacts it. To preview an update with `--dry-run`, pass `--username` too |
| IP access control list | `plivo sip ip-acl create --name --ip <ip/cidr> [--ip ...]`, `list`, `get`, `update <uuid> --ip ...`, `delete` | Update replaces the whole list. Wide ranges get a warning, not a block |
| Calls | `plivo sip calls list [--direction --from-number --to-number --hangup-cause-code --hangup-source --since --until --stir-verification --limit 1-20 --offset]`, `get <call_uuid>`, `diagnose <call_uuid>` | `diagnose` is not live on the server yet (CLI 1.1.0 release notes); use `get` |
| Number routing | `plivo numbers update <number> --trunk-id <trunk_id>` | Refuses an outbound trunk; `--app-id` and `--trunk-id` together are refused |

Commands with no typed form: call Insights (`plivo api GET /Zentrunk/Call/<uuid>/Insights/ -o json`). Through `plivo api`, every HTTP error comes back as `UPSTREAM_ERROR` (exit 3) with the real status in `status_code`; switch on `status_code`, not on `code`.

Docs: <https://www.plivo.com/docs/sip-trunking/api/trunks>, <https://www.plivo.com/docs/sip-trunking/api/origination-uris>, <https://www.plivo.com/docs/sip-trunking/api/credentials>, <https://www.plivo.com/docs/sip-trunking/api/ip-access-control-lists>, <https://www.plivo.com/docs/sip-trunking/api/calls>.

## Zentrunk hangup codes for AI agent calls: owner, fix, what the SIP flow shows

Every row comes from the public table <https://www.plivo.com/docs/sip-trunking/troubleshooting/zentrunk-hangup-codes> unless another page is named.

Where to read a code: `plivo sip calls get <uuid> -o json` gives `hangup_cause_code`, `hangup_cause_name`, `hangup_source` (`customer` = your platform, `carrier` = the network side, `zentrunk` = Plivo). The final SIP response on the platform leg and the message flow are in the console: Zentrunk, Logs, the call, SIP logs (<https://www.plivo.com/docs/sip-trunking/troubleshooting/zentrunk-debug-logs>). Trust the hangup code for Plivo's conclusion and the SIP flow for what the platform said; the two can disagree.

Owner values: **platform config** = the AI platform or your SIP server side; **customer Plivo config** = your trunk, URI, credential, number or account setup on Plivo; **Plivo policy** = Plivo refused on account or regulatory grounds; **Plivo** = Plivo-side handling; **carrier/destination** = the network or callee; **caller**; **expected outcome**.

### Read the SIP message sequence in three lines

- Inbound: Plivo's INVITE to your URI, then the platform's answer. No platform leg at all usually means Plivo refused the call itself (4590, 4030 or 4310). INVITE repeated with nothing back usually means 4170. Both readings come from SIP flows, not from a published mapping.
- Outbound on a credential-authenticated trunk: INVITE, 407, then a second INVITE with credentials is the digest handshake. The docs state it in words (the trunk challenges the INVITE, the platform's auth block answers it); the 407 code and the two-INVITE sequence are observed on the wire, not published values. A sequence that **ends** at 407 means the platform never authenticated; the hangup code recorded for that varies, so read the flow. A trunk authenticated by IP ACL is not challenged, so it shows no 407.
- Anything after 200 OK is a connected call; 3000 and 3010 are normal ends.

### Codes common on AI agent trunks

| Code | Direction | Wire (final on the platform leg) | Owner | Fix |
|---|---|---|---|---|
| 3000 | both | 200 then BYE | expected outcome | nothing (normal hangup by the carrier side) |
| 3010 | both | 200 then BYE | expected outcome | nothing. The published table says only `normal_hangup`, a normal hangup from the user; a 0 s duration means the call ended at once but does not name who ended it, so read the platform logs and the SIP flow |
| 4090 | inbound | platform **404**. Only ElevenLabs's phrase `Does not match any SIP Trunks` appears in Plivo's docs (<https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/ai-coding-agent>); LiveKit `No trunk found` / `Does not match Trunks or Dispatch Rules` and Retell `Invalid destination` are the platforms' own phrases, commonly seen, not in Plivo's docs | platform config | import `+<number>` on the platform, bind an agent or dispatch rule; the readiness checklist confirms the Plivo attachment |
| 4090 | outbound | 404 (sometimes 503) | carrier/destination | verify the E.164 destination; dial-list quality |
| 4170 | inbound | no response at all (INVITE repeated), or 100 then Plivo 408 | platform config | URI host, port, `;transport=`; platform online; reachability probe; firewall |
| 4180 | outbound | Plivo 403 | platform config | wrong credential username or password, or a platform source IP missing from the IP list |
| 4150 | inbound | platform 401/407 | published row is **carrier** proxy auth; the inbound platform-challenge reading is observational | stop challenging Plivo, or `authentication_needed` with the same credentials on the URI |
| 4410 | inbound | platform 486 (`Rejected`) | platform config | dispatch rule, agent availability, concurrency |
| 4410 | outbound | 486 | carrier/destination | normal outcome; retry |
| 4420 | inbound | CANCEL from the caller side; a racing platform 404 gets the 4090 fix | caller | nothing, or answer faster |
| 4440 | inbound | 487 | Plivo | read the flow; Plivo support with the UUID |
| 5220 | both | 200 then a platform 4xx to a re-INVITE, or no answer (408) to a mid-call request | platform config | platform logs at the drop time |
| 5360 | inbound | platform 503 | platform config | platform status; add a fallback URI |
| 5000 | outbound | 503 | carrier/destination | retry; Plivo support if per-country |
| 4000 | outbound | **400**, or a sequence ending at 407 | platform config | inspect the packet; do not retry without fixing. Commonly seen, not documented: the 400 arrives after the far end returned 183 ringing; check the flow before assuming a syntax error, and if the INVITE is well formed collect the UUID for Plivo support |
| 4010 | outbound | 503 | carrier/destination | retry; Plivo support if repeated |
| 4040 | outbound | 503 | carrier/destination | check the number; Plivo support if systematic |
| 4100 | outbound | 404 `Prefix Not Supported` | platform config (dial list) | dial E.164 with the country code |
| 4160 | outbound | 503 | carrier/destination | retry |
| 4340 | outbound | 480 | carrier/destination | normal outcome |
| 4370 | outbound | 482 | carrier/destination | not dialling your own trunk number? else Plivo support |
| 4550 | outbound | platform CANCEL | platform config | normal if intended; very short cancels count as abandoned calls (US) |
| 4560 | outbound | Plivo 403 `Barred Country` | customer Plivo config (geo permissions) | console Zentrunk, Geo Permissions. The geo-permissions page documents **4650** for the same block |
| 4570 | outbound | Plivo 403 `Barred Number` | Plivo policy | drop the number, or Plivo support if legitimate |
| 4590 | both | Plivo 403 (inbound: refused before any platform leg), observed | Plivo policy (India) | the SIP trunking page's cause is that the orchestration platform does not terminate SIP and media in India; the Voice API pages phrase the same rule as both legs staying in India. Move the platform endpoint and both legs into India; do not mix PSTN and WebRTC legs in a conference. Nothing published maps this code to compliance, so check KYC as its own gate |
| 4630 | outbound | 488 | carrier/destination | offer PCMU/PCMA, RFC 2833 DTMF; fix the secure flag if you offer SRTP only |
| 5300 | outbound | 502 (`bad_gateway`) | carrier/destination | retry; check carrier status |
| 5310 | outbound | mid-call carrier 4xx | carrier/destination | carrier issue; Plivo support if frequent |
| 5330 | outbound | 504 | carrier/destination | retry |
| 5350 | outbound | 503 | carrier/destination | retry later |
| 6000 | outbound | 503 or 486 | carrier/destination | retry later |
| 6040 | outbound | 503 | carrier/destination | retry; Plivo support if frequent |

### Other codes in the public table (docs meaning)

| Code | Cause | Fix | Owner |
|---|---|---|---|
| 3020 | RTP timeout | network; allow RTP ports (the table says 10000-20000, the technical-specifications page says 10000-30000; use the wider range) | platform config |
| 3030 | Credits exhausted mid-call | add credits, auto-recharge | customer Plivo config |
| 3040 | Re-INVITE rejected | inspect changed session parameters; on a transferred leg collect the Call-ID and PCAP for Plivo support | platform config |
| 4020 | Authentication required | attach correct credentials | customer Plivo config |
| 4030 | Insufficient credits (`insufficient_plivo_credits`; call records may show `insufficient_credits`) | add credits. The published row carries no direction and no page says inbound trunk calls are billed, so treat a zero balance as a risk to inbound too rather than a documented cause | customer Plivo config |
| 4050 | Number blacklisted for verification | Plivo support | Plivo policy |
| 4060 | Endpoint authentication failed | correct endpoint credentials | platform config |
| 4070 | Trunk not found | use the exact `trunk_domain` | platform config |
| 4080 | No matching inbound route | correct incoming route settings | customer Plivo config |
| 4110 | Secure trunking disabled | `secure=true`, or non-secure transport on both sides | customer Plivo config |
| 4120 | Invalid destination format | E.164 | platform config |
| 4130 | Caller ID non-numeric | numeric E.164 Plivo number | platform config |
| 4140 | Caller ID too short | full E.164 Plivo number | platform config |
| 4180 | Invalid credential or unauthorised source | correct credentials or IP list | platform config |
| 4190 | Unknown caller ID | caller ID = a Plivo number on this account | platform config |
| 4200 | Do Not Originate caller ID | another outbound-capable number | platform config |
| 4220 | Unsupported media type | PCMU, PCMA, telephone-event | platform config |
| 4230 | Unsupported URI scheme | correct the SIP URI format | platform config |
| 4270 | Session interval too small | increase the session timer | platform config |
| 4310 | Origination URI missing | create a URI, set `primary_uri_uuid` | customer Plivo config |
| 4320 | Origination URI cannot resolve | fix DNS or the host | customer Plivo config |
| 4330 | Zentrunk temporarily unavailable | retry; check CPS | Plivo |
| 4350 | Callee unavailable (customer side, 480) | platform online; TLS consistency | platform config |
| 4360 | Call-ID mismatch | check duplicate Call-IDs | platform config |
| 4380 | Too many SIP hops | reduce proxy hops | platform config |
| 4430 | SDP rejected | correct SDP compatibility | platform config |
| 4500 | Request already pending | wait for the current request | platform config |
| 4520 | Security agreement required | implement the required security mechanism | platform config |
| 4540 | Zentrunk authentication failure | Plivo support | Plivo |
| 4580 | Invalid SIP packet | headers and request format | platform config |
| 4610 | Request URI rejected | correct URI format | platform config |
| 4620 | Codec rejected | offer PCMU or PCMA | platform config |
| 4640 | Endpoint rejected SDP | correct SDP for the platform | platform config |
| 4650 | Geo Permissions block (SIP 403 Barred Country) | enable the destination; Plivo support if geo permissions were disabled by Plivo | customer Plivo config |
| 5010 | Port capacity reached | request more capacity | Plivo |
| 5020 | Function not implemented | use a supported SIP method | platform config |
| 5030 | Carrier does not implement request | different request method | carrier/destination |
| 5040 | Endpoint does not implement request | update endpoint behaviour | platform config |
| 5180 | CPS limit reached (rejected, not queued) | pace within CPS; the docs route a CPS increase through the console assistant rather than a support ticket | platform config (pacing) |
| 5190 | Concurrent call limit exceeded | reduce concurrency; ask for a higher limit | Plivo policy |
| 5200 | Server timer expired | retry | Plivo |
| 5230 | Endpoint 5xx or 6xx mid-call | platform logs | platform config |
| 5240 | Mid-call media error | network and NAT | platform config |
| 5250 | Plivo-side service error | retry; Plivo support if persistent | Plivo |
| 5260 | Plivo-side routing error | retry; Plivo support if persistent | Plivo |
| 5270 | Plivo-side media error | retry; Plivo support if persistent | Plivo |
| 5290 | Trunk URI fetch error | retry; verify the trunk has a primary URI | Plivo |
| 5320 | Carrier 5xx or 6xx mid-call | carrier status | carrier/destination |
| 5340 | Endpoint timeout | endpoint responsiveness | platform config |
| 6020 | Destination does not exist | correct the destination | carrier/destination |
| 6030 | Answer timeout | handle no-answer | carrier/destination |
| 6070 | Session description rejected | correct SDP | carrier/destination |

### Transfer outcomes (no hangup code; read the NOTIFY sipfrag)

A failed REFER leaves the agent leg up, so it never shows as a hangup code.

| NOTIFY sipfrag | Owner | Fix |
|---|---|---|
| `100 Trying` then `200 OK` | expected outcome | nothing; Plivo sends BYE to the agent leg |
| `100 Trying` then `4xx` or `5xx` (documented examples `486 Busy Here`, `408 Request Timeout`; `480` and `503 Transfer Failed` also commonly seen, not documented) | transfer target or route | the agent speaks at once, then retries another number or continues; check Geo Permissions and balance. Callers often hang up within seconds of silence |
| `100 Trying` only | platform config | keep the agent leg up until `NOTIFY 200` or a failure; do not send BYE early |

### Name differences between the docs table and call records (quote the one you mean)

The call-record names are observed on call records, not published.

| Code | Docs | Call record |
|---|---|---|
| 4030 | `insufficient_plivo_credits` | `insufficient_credits` |
| 4550 | `user_cancelled` | `customer_cancelled` |
| 5220 / 5310 | `service_interrupted_by_customer` / `..._by_carrier_4xx` | `service_interrupted_middialog_by_customer` / `..._middialog_by_carrier_4xx` |
| 4010 | `unauthorized_by_carrier` | `call_rejected_unauthorized_by_carrier` |
| geo block | 4650 (geo-permissions page) and 4560 (hangup table) | 4560 |

A code that is not in the public table still appears in `hangup_cause_name`. Do not invent a meaning for it: use `plivo ask` or Plivo support with the call UUID.

## When this skill does not have the answer

Do not guess, and do not fill the gap from general knowledge of other platforms. In order:

1. **Read the current documentation.** Every page on <https://www.plivo.com/docs> is available as Markdown by adding `.md` to its URL, and <https://www.plivo.com/docs/llms.txt> indexes many pages but not all (it has no CLI pages, for one). Start at <https://www.plivo.com/docs/sip-trunking/api/overview>, <https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/other-platforms> and <https://www.plivo.com/docs/sip-trunking/troubleshooting/zentrunk-hangup-codes>. From a terminal `plivo docs search <keywords>` searches the full text of every page, `plivo docs list` prints the index and `plivo docs show <path-or-title>` prints one page; those three need no credentials and are not rate limited, so reach for them before the assistant.
2. **Ask Plivo's assistant from the terminal**: `plivo ask "<your question>"`. It reads the documentation and can see the account, so it answers things this file cannot: what a specific call did, whether a compliance application is accepted, what a destination costs. It has a small per-account rate limit: on `RATE_LIMITED`, wait the time the error message gives. Save it for the question you cannot answer another way. `plivo sip calls diagnose <call_uuid>` is the same assistant pointed at one trunk call (`plivo voice calls diagnose` refuses trunk calls). Its server side is not live yet, so read the record with `plivo sip calls get`, and do not loop either command: they share the rate limit with `plivo ask`.
3. **If you have no CLI access**, tell the person you are working with to ask the same question to the assistant in the Plivo console.

Treat the answer as evidence, not as final. If it contradicts the documentation, say that it does and prefer the documentation for published behaviour. If it gives a number the documentation does not publish, repeat it as something the assistant said, not as a documented fact.

For account state read it yourself with `plivo sip <group> get <id> -o json` and `plivo numbers get`, and for one specific call read the console SIP logs. For which CLI commands and flags exist, `plivo <command> --help` outranks this file: if the two disagree, the CLI is right and this file needs updating, and you should say so. Where this file says a command behaves differently from its help text, it describes tested behavior: follow this file. Rules marked here as observed rather than documented are safe checks, not Plivo commitments. Never invent flags, API fields, hangup codes or platform IPs. Where Plivo's pages disagree with each other (4560 vs 4650, `user_cancelled` vs `customer_cancelled`, RTP port range, caller ID rules, the CIDR list (14 ranges on the SIP trunking page, 8 on the technical-specifications page), API examples without `;transport=`), this file names both and which one it follows.

## CANNOT

- **Cannot write Plivo XML.** A SIP trunk hands the call to your platform. There is no answer URL and no XML on this path. For XML, `plivo skill install voice-xml`, or read <https://www.plivo.com/docs/voice/xml/overview>. For a WebSocket bot, `plivo skill install audio-streaming`.
- **Cannot invent CLI flags.** Use the `plivo sip` flags in the reference above, checked with `--help`. Geo permissions, account limits, and the SIP flow and PCAP are console or support only; upload UCC proof on the console dashboard. Say so.
- **Cannot infer account state.** Look the number, trunk, URI, credentials and IP lists up with the readiness checklist; never assume a number is attached, a trunk enabled, a URI's transport right, or KYC accepted.
- **Cannot treat an accepted API request as a working call.** A created trunk means the object exists. A call works when the record shows 3000/3010 with a non-zero duration and the SIP flow shows the platform answering 200. Test both directions.
- **Cannot spend or reconfigure without a preview.** Before `numbers update --trunk-id`, `numbers buy`, or any `plivo sip` create, update or delete, show the preview (`--dry-run`, or for a delete a run without `--yes`) and the rollback (`plivo numbers update <number> --app-id <previous>` or `--trunk-id <previous>`; for trunks the previous field values). A deleted URI takes its trunks with it; a deleted credential breaks every trunk using it.
- **Cannot fill in or submit KYC.** Business details and documents come from the user; a compliance application is a regulatory filing and is submitted only when the user says so.
- **Cannot see the platform side.** Dispatch rules, number imports, agent bindings, region pinning, platform egress IPs: ask, then verify with a call.
- **Cannot present an undocumented platform as supported.** Plivo publishes guides for LiveKit, ElevenLabs, Retell, Vapi, xAI and a generic path. For anything else, say Plivo does not document it and get the values from that platform and from Plivo.
- **Cannot handle credentials.** Passwords go in on stdin from a variable the user set, never on the command line, in chat, or in this skill's output; Plivo never returns them.

This skill does not cover: PBX interconnection (3CX, Asterisk, FreeSWITCH, FreePBX, FusionPBX, Twilio BYOC), carrier routing and rates, WebSocket Audio Streaming (`plivo skill install audio-streaming`), Voice API `<Dial>` and transfer flows and Plivo XML (`plivo skill install voice-xml`), SMS, number porting, or how any agent platform works inside. It cannot see your platform's logs or your Plivo account's data except through the CLI commands it names.
