---
name: plivo-sip-trunking
description: "Connects an AI voice platform (LiveKit, ElevenLabs, Retell, Vapi, xAI, self-hosted) to phone numbers over Plivo SIP trunking (Zentrunk). Use for origination URIs, inbound or outbound trunks, TCP/TLS/UDP transport, Zentrunk hangup codes (4090, 4170, 4590), SIP REFER transfer, India calling rules, go-live checks. Not for WebSocket bots (plivo-audio-streaming) or Plivo XML (plivo-voice-xml)."
license: Apache-2.0
---

# Plivo SIP trunking for AI voice agents

Take a developer from "my agent runs on platform X" to a number that reaches it, outbound calls that connect, transfers that work, and failures they can read. Ask first: inbound, outbound or both? Which platform, hosted or self-hosted? Which country (India changes the rules)? Then run the readiness check and go stage by stage.

Evidence rule: say calls **will break** only when a documented rule or a known failure says so; otherwise call it a risk and say what to check. Hangup codes and names here come from Plivo's public table. Anything marked *observed* (SIP responses, message sequences) is seen on the wire, not a Plivo contract: never quote it as one.

## Objects and CLI rules

- **Inbound trunk**: origination URI (`host[:port];transport=udp|tcp|tls`) + trunk + number. **Outbound trunk**: credential or IP access control list (IP ACL) + trunk; the platform dials the trunk's `trunk_domain`, `<trunk_id>.zt.plivo.com` (the console labels it Termination SIP Domain). Only inbound trunks attach to numbers.
- Commands: `plivo sip uris|trunks|credentials|ip-acl|calls` and `plivo numbers update <number> --trunk-id <id>`. `plivo sip <group> <verb> --help` is the source of truth for flags; never invent one. Call Insights has no typed command: `plivo api GET /Zentrunk/Call/<call_uuid>/Insights/ -o json`.
- `plivo docs show <path>` prints a JSON envelope outside a terminal (agent shells too): add `-o table`. Its source drops field names from attribute lists and can cut a page short: for either, read `https://www.plivo.com/docs/<path>.md`.
- Pass numbers to `numbers` commands as digits without `+` (`14155551234`).
- List commands return 20 rows by default: page with `--offset` before concluding something is missing.
- Passwords go in on stdin only (`--password-stdin`), never on a command line, in a file or in chat. Ask the user to run `read -rs SIP_PASSWORD && export SIP_PASSWORD` in the shell that runs you, or to run the command themselves.
- Quote every `--uri` value: the `;` in `host;transport=tcp` otherwise ends the shell command.

### Write safety

- `plivo sip` create and update, `numbers update` and `numbers compliance create|update|link` have **no `--yes` gate**: they write as soon as they run without `--dry-run`. Preview with `--dry-run`, show it, get approval, then run the same command without it. Never put preview and write in one shell command.
- **Routing a number** (`numbers update <number> --trunk-id <id>`) reroutes a possibly live number immediately. Do it last, after the platform side (import, dispatch rule, agent) exists. First run `plivo numbers get <number> -o json` and record `application`: it is the rollback. Pass only its trailing id: `/v1/Account/<auth_id>/Application/<app_id>/` is `--app-id <app_id>`, `.../Zentrunk/Trunk/<trunk_id>/` is `--trunk-id <trunk_id>`. If `application` is empty, tell the user before asking for approval that `numbers update` cannot restore that state (an empty `--app-id` is rejected). The command reads the trunk even under `--dry-run` and refuses an outbound trunk or one not on this account, so its preview works only once the trunk exists; `--app-id` skips that check, so use `--trunk-id`.
- **Updates that hit live traffic**: `sip trunks update --status disabled|--secure|--uri` and `sip uris update --uri` take effect immediately. `get` the object and record the current values first, then preview, then apply.
- **Deletes** need `--yes` and cannot be undone. Deleting an in-use URI also deletes the trunks that point at it, so their numbers stop routing; deleting a trunk detaches every number on it; deleting a credential or IP ACL breaks the outbound trunks using it. The preview is the delete run **without** `--yes` (`--dry-run` shows no dependents: it skips the read): a URI, credential or IP ACL delete lists the dependent trunks but not their numbers, a trunk delete counts its numbers, and either prints nothing if its read fails. Count each affected trunk's numbers yourself: `plivo numbers list -o json | jq '.data.objects[] | select((.application // "") | contains("/Zentrunk/Trunk/<trunk_id>/")) | .number'`, paging with `--offset`. Disabling a trunk is reversible but an outage for its numbers, not a safe alternative.
- **Deleting a URI safely**: find its trunks with `plivo api GET /Zentrunk/Trunk/ --query "primary_uri_uuid=<uuid>" -o json`, then `fallback_uri_uuid=<uuid>`; count their numbers (above); repoint each trunk (`plivo sip trunks update <id> --uri <new_uuid>`, or `--fallback-uri`), test a call, then delete (its preview now lists no trunks).
- `sip ip-acl update --ip` replaces the whole list; `sip credentials update` always sets a new password from stdin.

## Readiness check (read-only; run first and again before go-live)

Stop at the first blocking answer. Steps 3-5 are inbound only and step 6 is outbound only: an outbound-only setup (for example a Vapi agent that only places calls from a Plivo caller ID) skips 3-5 and goes green without an inbound trunk. Retell needs step 6 even for inbound only.

1. `plivo auth whoami -o json`: the account you meant, `cash_credits` above zero. 4030 (no credits) carries no direction in the docs, so treat zero as a risk to inbound too.
2. `plivo numbers get <number> -o json` (the inbound number, or the outbound caller ID): on this account, `voice_enabled` true; for inbound, record `application`. India: the gates below.
3. Read `application`. `Trunk/<id>`: run `plivo sip trunks get <id> -o json` (fields under `data.object`) and require `trunk_direction` `inbound`, `trunk_status` `enabled`, `primary_uri_uuid` set (without it: 4310 `uri_not_found`). `Application/<id>`: an XML application, not a trunk; stop. Any other shape: report it, do not guess.
4. `plivo sip uris get <primary_uri_uuid> -o json`: passes the URI checks (Stage 2) and matches the platform matrix; `authentication_needed` only when the platform challenges Plivo.
5. `fallback_uri_uuid` set? Missing is a warning: the fallback is the only re-route when the primary is unreachable or errors.
6. `plivo sip trunks list --direction outbound -o json`: an `enabled` trunk with `credential_uuid` (`plivo sip credentials get <uuid>`) or `ipacl_uuid` (`plivo sip ip-acl get <uuid>`: no `/0`, no `/1`, nothing wider than the platform's published IPs). Its `secure` must match the platform's outbound transport: `true` only when the platform dials over TLS, `false` for TCP or UDP.
7. Concurrency and CPS: console only, Organization settings > Account limits.
8. Platform-side items (Stage 3): Plivo cannot see them, so ask.
9. Optional: one SIP OPTIONS to the URI host over its transport. A DNS or TLS error proves something; a timeout proves nothing (hosted platforms may ignore unknown sources).

## Platform matrix

Plivo publishes guides for LiveKit, ElevenLabs, Retell, Vapi and xAI, plus a generic one: `plivo docs show voice-agents/sip-trunking/integration-guides/<slug>` (https://plivo.com/docs/voice-agents/sip-trunking/integration-guides/<slug>), where `<slug>` is `livekit`, `elevenlabs`, `retell`, `vapi`, `xai-voice-agents` or `other-platforms`.

| Platform | Inbound URI | Outbound auth | `secure` | Platform side (Plivo cannot see it) | India |
|---|---|---|---|---|---|
| LiveKit Cloud | `<project SIP host>;transport=tcp`; `tls` for secure trunking | credential | recommended; enable LiveKit secure trunking too | inbound trunk listing `+<number>` **and** a dispatch rule; outbound trunk with `trunk_domain`, credential username and password | region pinning, then the endpoint LiveKit shows |
| ElevenLabs | `sip.rtc.elevenlabs.io:5060;transport=tcp` or `:5061;transport=tls` (only these two) | credential | recommended; TLS in ElevenLabs' outbound settings | number imported on a SIP trunk with an agent; outbound settings with `trunk_domain` and credential | India deployment from ElevenLabs, then `sip.rtc.in.residency.elevenlabs.io:5060;transport=tcp` |
| Retell | `sip.retellai.com;transport=tcp` (or `tls`) | credential, **required even inbound only**: Retell will not import a number without it | off by default; if on, Retell Outbound Transport = TLS | import the number with `trunk_domain` (no `sip:`), credential username, password, transport; bind inbound and outbound agents. Imports cannot be edited: delete and re-import | not in Plivo's docs; confirm with Retell |
| Vapi | `sip.vapi.ai;transport=udp` | IP ACL with the two `/32` addresses in Plivo's Vapi guide | no | register the number (BYO SIP trunk) and assign an assistant | **not supported** |
| xAI Voice Agents | `sip.voice.x.ai;transport=tls` (the console's `sip:sip.voice.x.ai;transport=tls` is fine too) | none: **inbound only**, no outbound trunk | not used | add the number to the agent (Direct SIP); allow every Plivo signaling range the xAI guide lists, or set the same SIP digest credentials on both sides | not stated; confirm with xAI |
| Other or self-hosted | `<host>[:port];transport=<what it documents>` (TLS usually 5061); the API also accepts `sip:user@host` | credential; IP ACL only for published static IPs | both sides must agree | route the number to an agent; allow Plivo signaling and media (Stage 3); **no digest challenge** to Plivo unless the same username and password are on the URI | must terminate SIP and media in India |

For a platform not listed, say Plivo publishes no guide for it, take the host, port and transport from the platform's own docs, and never compose a hostname or guess a transport. The Origination URI API takes only `name`, `uri`, `authentication_needed`, `username` and `password`; anything more is a question for Plivo. OpenAI Realtime is documented over audio streaming, not SIP (`plivo skill install audio-streaming`).

## India: four gates, each blocking until proved

1. **Data region.** The organization must be in the India data region. It cannot be changed (create a new organization) and is not readable over the API: confirm in the console.
2. **KYC.** A compliance application in `accepted` status (not `submitted`) linked to the number: `plivo numbers compliance list --country IN --status accepted -o json`, then `plivo numbers compliance get <compliance_id> --expand linked_numbers -o json`. The required documents come from the live `plivo numbers compliance requirements --country IN --number-type local --user-type business -o json`, never from memory. Documents and legal details come from the user: never fill them in or invent them, and never create, update or link an application unless the user explicitly says go (it is a regulatory filing, and `create` submits at once). CLI flow: `plivo numbers compliance <verb> --help`. Process: `plivo docs show numbers/rent-india-numbers` (https://plivo.com/docs/numbers/rent-india-numbers).
3. **Media anchoring.** The platform must terminate SIP and media in India, or calls end **4590** `domestic_anchored_terms_not_met`. 4590 is not a KYC check: check KYC as its own gate. Platform support is in the matrix. **Vapi is not supported** and no Plivo-side setting fixes its 4590: send India traffic to a platform that terminates in India (LiveKit region pinning, ElevenLabs India deployment) or a self-hosted stack there; keep Vapi for other countries. `plivo docs show voice-agents/sip-trunking/deploy/calling-in-india` (https://plivo.com/docs/voice-agents/sip-trunking/deploy/calling-in-india).
4. **Traffic rules.** Both legs in India, the right number series for the call type, explicit consent, no cold calls, UCC complaints handled. These come from a Voice API page but are number-level, so they apply to trunk numbers: `plivo docs show voice/concepts/india-calling` (https://plivo.com/docs/voice/concepts/india-calling).

## Stage 1: account and number

```bash
plivo auth whoami -o json                       # right account, cash_credits > 0
plivo numbers list --services voice -o json     # or numbers search, then numbers buy (needs --yes; spends money)
```

## Stage 2: inbound, Plivo side

```bash
plivo sip uris create --name <name> --uri "<uri from the matrix>" --dry-run              # then without --dry-run; prints uri_uuid
plivo sip uris get <uri_uuid> -o json                                                   # read back what was stored
plivo sip trunks create --name <name> --direction inbound --uri <uri_uuid> --dry-run      # add --fallback-uri <uuid>; then without --dry-run
```

Inbound TLS is the URI's `;transport=tls`, not `--secure`. If the platform challenges Plivo on inbound, its username and password go on the URI, a different secret from the outbound credential: `printf '%s' "$PLATFORM_SIP_PASSWORD" | plivo sip uris create --name <name> --uri "<uri>" --authentication-needed --username <username> --password-stdin`.

Route the number only after Stage 3, following the routing rule in Write safety:

```bash
plivo numbers get <number> -o json                                       # record application: the rollback
plivo numbers update <number> --trunk-id <inbound trunk_id> --dry-run    # then, after approval, without --dry-run
plivo numbers get <number> -o json                                       # confirm Trunk/<trunk_id>
```

### URI checks

Breaks or misroutes calls:

- A URL (`http://`, `https://`, any path): Plivo needs the SIP endpoint, not an answer URL.
- A space, no host, a host with characters other than letters, digits, dots and hyphens, a non-numeric port, or `transport=` other than `udp`, `tcp` or `tls`.
- A private or unroutable host (10.x, 127.x, 192.168.x, 172.16-31.x, 169.254.x, 0.x).
- A host that differs from the documented host for the named platform.
- An Indian number with a non-India endpoint (4590).

Fix before go-live:

- No `;transport=`, or one the platform does not document. Plivo's generic guide calls a transport mismatch the most common reason an inbound integration fails silently, and Plivo documents no default, so always set what the platform documents; LiveKit may answer over UDP without it, but do not rely on that.
- A port the platform's guide does not use, or 5061 without `transport=tls`. Some guides publish a TLS URI with no port: write what the guide shows.
- A `sip:` or `sips:` scheme or a user part where the platform's guide shows the host-first form (xAI's `sip:` form is fine).
- Any parameter other than `transport=`, a parameter with no value, or a host ending in a dot.
- A platform with no documented host and transport to compare against: get both before creating the URI.

## Stage 3: inbound, platform side

Ask about and confirm the matrix's platform-side column. When it is missing the platform answers **404** (ElevenLabs: `Does not match any SIP Trunks`) and the record usually shows 4090 `destination_not_found`, whose published row has a carrier framing, so read it with the SIP flow. A missing LiveKit dispatch rule or no available agent usually shows as a platform 486, recorded as 4410 (observed).

Self-hosted: allow every Plivo signaling range on 5060 (UDP/TCP) and 5061 (TLS), and media on 10000-30000, from `plivo docs show sip-trunking` (https://plivo.com/docs/sip-trunking, "Signaling / Media IP addresses"). A server that digest-challenges Plivo without the same credentials on the URI never completes the call. Do not promise 4150 for it: that published row is a carrier requiring proxy auth.

## Stage 4: outbound

Credential rules: username 5 to 20 alphanumeric characters; password 5 to 20 characters from alphanumerics and `~!@#$%^&*()_+`, with at least one special character.

```bash
printf '%s' "$SIP_PASSWORD" | plivo sip credentials create --name <name> --username <username> --password-stdin --dry-run   # preview redacts it; then without --dry-run
plivo sip ip-acl create --name <name> --ip <ip>/32 --ip <ip>/32 --dry-run                          # instead of a credential, for published static IPs (Vapi)
plivo sip trunks create --name <name> --direction outbound --credential <credential_uuid> --dry-run   # or --ip-acl <uuid>; --secure for TLS and SRTP; prints trunk_domain
```

Give the platform the `trunk_domain` (no `sip:`, no spaces), the credential **username** (not its name) and the password. If the platform sends SIP OPTIONS health checks, point them at the `trunk_domain` only, at most one every 10 to 15 s; more may get blocked. Caller ID: a Plivo number on this account; anything else risks 4190 `unknown_caller_id`, and a same-account number gets STIR/SHAKEN attestation A (`plivo docs show sip-trunking/concepts/stir-shaken`, https://plivo.com/docs/sip-trunking/concepts/stir-shaken).

`--secure` means TLS signaling and SRTP media on that trunk, so the platform must dial over TLS too. TLS or SRTP against a non-secure trunk ends 4110 `secure_trunking_disabled`; a secure trunk with the platform on TCP drops calls or loses audio (Retell guide).

Digest handshake (observed): INVITE, 407, then a second INVITE with credentials is normal on a credential trunk; an IP ACL trunk is not challenged and shows no 407. A flow that **ends** at 407, or 4180 `call_rejected_unauthorized`, means a wrong username or password, or a platform source IP missing from the IP ACL. Never open the list to `0.0.0.0/0` (or any `/0` or `/1`) to make it work: anyone who learns the `trunk_domain` could then call on the account. The CLI warns on wide ranges but does not block them.

## Stage 5: first call, both directions

Dial the number from a phone, then place one outbound call from the platform to your own phone (no outbound for xAI).

```bash
plivo sip calls list --limit 5 -o json                         # hangup_cause_code, hangup_cause_name, hangup_source, transport_protocol, srtp
plivo sip calls get <call_uuid> -o json
plivo api GET /Zentrunk/Call/<call_uuid>/Insights/ -o json     # rtt, jitter, packet_loss, plivo_quality_score
```

`sip calls list` strips a leading `+` from `--from-number` and `--to-number`. `plivo voice calls` reads Voice API calls, not trunk calls.

**Pass:** 3000 or 3010 with a non-zero duration in both directions. A 3010 with 0 s ended at once without saying who ended it: read the platform logs and the SIP flow.

## Stage 6: transfer to a human with SIP REFER (optional)

- The platform sends REFER with `Refer-To: <sip:+<E.164 number>@<trunk_domain>>`. Only your own `.zt.plivo.com` `trunk_domain` works: other domains, arbitrary SIP URIs and private IPs get 403, and a missing or malformed `Refer-To` gets 400. `tel:` or `sips:<IP>` targets are undocumented: do not rely on them. LiveKit sends REFER from `TransferSIPParticipant` (LiveKit's docs): give it this form.
- Plivo answers 202 and reports progress with NOTIFY (100, 180, then 200 or 4xx/5xx); answer each with 200 OK. Never send BYE before `NOTIFY 200 OK`: Plivo sends the agent leg a BYE once the target answers.
- After a `NOTIFY 4xx/5xx` the caller is still on the agent leg, in silence: make the agent speak at once, then retry or continue. A dialog that ends after `NOTIFY 100` means the agent leg dropped too early.
- If the target hangs up, the whole call ends. International targets need Geo Permissions. Expect two call records (the transfer leg's can take up to 60 s). REFER legs count toward concurrency. Outbound transfers need two Plivo numbers.
- Flow, restrictions and troubleshooting: `plivo docs show sip-trunking/concepts/sip-refer-inbound` (https://plivo.com/docs/sip-trunking/concepts/sip-refer-inbound) and `plivo docs show sip-trunking/concepts/sip-refer` (https://plivo.com/docs/sip-trunking/concepts/sip-refer).

## Go-live

- The readiness check passes for every direction in use, including the probe; warnings understood.
- Concurrency and CPS sized for peak. SIP trunking **rejects** calls above either limit (5180 CPS, 5190 concurrency) and never queues; every PSTN leg counts, inbound and outbound, shared with Voice API. Pace the dialer. Current limits and how to raise them: `plivo docs show sip-trunking/concepts/account-limits` (https://plivo.com/docs/sip-trunking/concepts/account-limits). US abandoned and short-call thresholds: `plivo docs show voice-agents/sip-trunking/deploy/us-call-quality-and-cps` (https://plivo.com/docs/voice-agents/sip-trunking/deploy/us-call-quality-and-cps).
- Geo Permissions (console: Zentrunk, Geo Permissions) trimmed to the countries the agent calls: `plivo docs show sip-trunking/concepts/geo-permissions` (https://plivo.com/docs/sip-trunking/concepts/geo-permissions).
- A fallback URI; a `secure` decision made on both sides; credentials never pasted anywhere; IP lists narrow.
- India: all four gates. US: Plivo caller ID, dialer under the CPS limit.
- Watch: `plivo sip calls list --hangup-cause-code 4090 -o json` (and 4170, 4180, 4590, 4030, 5180, 5190). Outbound 4410, 4340 and 4550 are outcomes, not faults, but every unanswered US attempt counts toward the abandoned-call threshold.

## Debugging, in this order

1. `plivo sip calls list -o json` (filters: `--direction`, `--hangup-cause-code`, `--hangup-source`, `--from-number`, `--to-number`, `--since`, `--until`), then `plivo sip calls get <call_uuid> -o json` and Insights.
2. `hangup_source`: `customer` = your platform, `carrier` = the network side, `zentrunk` = Plivo. Map the code with the table below.
3. Console: Zentrunk, Logs, the call. Call Stats shows trunk, transport and secure; SIP logs show the message flow, the final response and a PCAP download. The hangup code is Plivo's conclusion and the SIP flow is what the platform said; they can disagree. Reading the flow (observed): inbound with no INVITE to your URI usually means Plivo refused (4590, 4030, 4310); INVITE repeated with no reply usually ends 4170; otherwise the platform's own response is the answer (404 not imported, 401/407 it challenged Plivo, 486 no dispatch rule or agent, 503 down).
4. `plivo sip calls diagnose <call_uuid>` asks Plivo's assistant about one trunk call (`plivo voice calls diagnose` refuses trunk calls). If it cannot retrieve the call, use `plivo sip calls get`. It shares a small rate limit with `plivo ask`: do not loop it. Then Plivo support with the call UUID and the PCAP.

| Code (name) | Dir | Seen on the wire (observed) | Fix |
|---|---|---|---|
| 3000, 3010 `normal_hangup` | both | 200, then BYE | none (3010 with 0 s: read the platform logs) |
| 4090 `destination_not_found` | in | platform 404 | import `+<number>`; bind an agent or dispatch rule |
| 4170 `request_timeout_customer` | in | INVITE repeated, no reply | URI host, port and transport; firewall; platform online; probe |
| 4150 `proxy_authentication_required` | in | platform 401/407 | stop challenging Plivo, or `authentication_needed` with the same credentials on the URI (the published row names a carrier) |
| 4410 `user_busy` | in | platform 486 | dispatch rule, agent availability, platform concurrency |
| 4350, 5360 | in | platform 480, 503 | platform status; fallback URI; TLS consistency |
| 4420 `carrier_cancelled` | in | caller CANCEL, 487 | none; answer faster |
| 4310 `uri_not_found` | in | no platform leg | set `primary_uri_uuid` |
| 4030 `insufficient_plivo_credits` | any | no platform leg | add credits; auto-recharge |
| 4590 `domestic_anchored_terms_not_met` | any | Plivo 403, no platform leg | India: platform must terminate SIP and media in India (not a KYC check) |
| 4180 `call_rejected_unauthorized` | out | Plivo 403 | credential username and password, or the platform IP in the IP ACL |
| 4190 `unknown_caller_id` | out | Plivo 403 | caller ID = a Plivo number on this account |
| 4110 `secure_trunking_disabled` | out | | `secure` on the trunk, or non-TLS on both sides |
| 4000 `bad_request` | out | 400, often after 183 | read the packet before any retry; support with the UUID if the INVITE looks valid |
| 4100 `prefix_not_supported` | out | 404 | E.164 with the country code |
| 4560 `barred_country`, 4650 | out | Plivo 403 Barred Country | enable the country in Geo Permissions |
| 4570 `barred_number` | out | Plivo 403 | drop the number, or support if legitimate |
| 4630 | out | 488 | offer PCMU or PCMA; fix the secure flag |
| 4410, 4340, 4550 | out | 486, 480, platform CANCEL | normal outcomes; a 4550 within a second or two is a dialer timeout bug |
| 4090, 4160, 5000, 5300, 5350, 6000, 6040 | out | 404, 502, 503 | carrier or destination: check E.164, retry; support if one destination keeps failing |
| 5180 `cps_limit_reached`, 5190 `concurrent_call_limit_exceeded` | any | rejected at once | pace the dialer; raise the limit |
| 5220, 3040 | any | 200, then a platform 4xx to a re-INVITE | platform logs at the drop time; support with Call-ID and PCAP |
| REFER `NOTIFY 4xx/5xx` | | no hangup code; agent leg stays up | the agent speaks at once |

Every other code, and the current wording of these: `plivo docs show sip-trunking/troubleshooting/zentrunk-hangup-codes` (https://plivo.com/docs/sip-trunking/troubleshooting/zentrunk-hangup-codes). Call records sometimes carry other names (`insufficient_credits`, `customer_cancelled`), so filter by code. For a code missing from that page, do not invent a meaning: `plivo ask` or Plivo support with the UUID.

## Where Plivo's docs disagree

- Geo block: the hangup table gives 4560 `barred_country` and also lists 4650; the geo-permissions page gives 4650. Expect either.
- RTP ports: the 3020 row says 10000-20000; the `sip-trunking` page and technical specifications say 10000-30000. Allow the wider range.
- Signaling ranges: 14 on the `sip-trunking` page, 8 on technical specifications. Allow all 14.
- India anchoring: `voice/concepts/india-calling` names it `violates_media_anchoring` (Voice code 2070); trunk calls end 4590 `domestic_anchored_terms_not_met`. Filter trunk calls by 4590.
- `;transport=`: Origination URI API examples omit it; the integration guides set it. Follow the guides.
- Caller ID: technical specifications require a Plivo number; the generic guide also allows a verified caller ID. Use a Plivo number on this account.
- NOTIFY: technical specifications list it as not supported. That means Plivo does not accept NOTIFY from you; it still sends NOTIFY to report REFER progress.

## When this file is not enough

Use `plivo docs search <terms>` and `plivo docs show <path>` first (no credentials, no rate limit), then `plivo ask "<question>"`, which can see the account but has a small rate limit. Treat its answers as evidence and prefer the docs where they conflict. `--help` outranks this file on flags. Never invent flags, API fields, hangup codes, hostnames or IPs. Out of scope: PBX interconnection, carrier rates, Plivo XML (`plivo skill install voice-xml`) and WebSocket bots (`plivo skill install audio-streaming`).
