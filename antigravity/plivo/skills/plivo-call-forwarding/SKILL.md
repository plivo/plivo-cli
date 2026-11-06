---
name: plivo-call-forwarding
description: "Debugs call forwarding on Plivo with the Plivo CLI: a carrier forwards calls into a Plivo number and ForwardedFrom, ForwardedCause or ForwardedReason is missing or odd; Plivo forwards a call out (Dial XML, a PHLO Call Forward node, an agent transfer) and the forwarded leg fails; or a carrier forwards into a SIP trunk number and the Diversion header must reach the SBC. Use when forwarded calls never arrive, lack the forwarding number or fail. Not for writing XML (plivo-voice-xml) or SIP trunk setup (plivo-sip-trunking)."
license: Apache-2.0
---

# Plivo call forwarding: debug a forwarded call

Rules here come from the Plivo docs and from Plivo's own checks of forwarded calls; lists marked *seen* are examples, not complete sets. Name a cause only when the evidence shows it, and never invent a field, flag or hangup code. Setup lives in other skills: `plivo skill install voice-xml` (Dial XML), `sip-trunking`, `audio-streaming` (a `<Stream>` voice agent) or `agents` (Plivo CX agents).

## Rules for every step

- Use the CLI for every Plivo step and `-o json` to read values. `plivo <command> --help` wins on which commands and flags exist.
- Debugging only reads. Preview any write with `--dry-run` and run it after the user agrees. A test call costs money: check `cash_credits` in `plivo auth whoami -o json` first.
- `--since` and `--until` are UTC (`YYYY-MM-DD HH:MM`) and filter on when a call ended. Pass numbers to `numbers` commands as digits, no `+`.
- Ask with your agent's question tool (AskUserQuestion in Claude Code). Mask phone numbers in chat; show them in full only in commands.
- Report what you saw this session, not what you assume. Run `diagnose` once per call: it shares a small rate limit with `plivo ask`.
- When this file runs out: `plivo docs search "<terms>"`, `plivo docs show <path>`, then `plivo ask "<question>"`, then Plivo support (https://support.plivo.com) with the call UUID and the time.

## Which kind of forwarding?

If it is unclear, ask (header Forwarding) which kind goes wrong:

| Kind | What happens | Tell it by |
|---|---|---|
| A. Into your number | A phone or provider forwards calls to your Plivo number; your answer URL runs | `plivo numbers get <digits> -o json`: `application` ends `/Application/<app_id>/` |
| B. Out of Plivo | `<Dial>`, a PHLO Call Forward node or an agent transfer rings another phone or SIP address | the forward is a second call, the B-leg |
| C. Into a SIP trunk number | As A, but the number routes to an inbound SIP trunk: no answer URL | `application` contains `/Zentrunk/Trunk/` |

A call can be A and B at once: forwarded in, then dialled out.

## A. A carrier forwards calls into your Plivo number

On a forwarded call, Plivo's answer URL and hangup URL requests carry `ForwardedFrom` (the number that forwarded the call, the one the caller dialled), `ForwardedCause` and `ForwardedReason`, in India and in other regions.

- Values pass through as the carrier sent them, not normalized: `+91XXXXXXXXXX`, `91XXXXXXXXXX`, a 10-digit national number, or one with a suffix such as `;rn=...;npdi`.
- `ForwardedCause` is a SIP code. *Seen*: 302, 404, 408, 486, 503.
- `ForwardedReason` casing varies by carrier. *Seen*: `NoReply`, `UserBusy`, `no-answer`, `Unconditional`, `NotLoggedIn`, `SubscriberNotReachable`, `unknown`, in pairs such as 408 `NoReply`, 486 `UserBusy`, 302 `no-answer`, 302 `UserBusy`, 302 `Unconditional`.
- Each field is optional on its own: some forwarded calls carry `ForwardedFrom` with no cause or reason.
- None of the three: a direct dial, or the carrier sent no forwarding information. Only the carrier can say why.
- A value in the answer URL's query string shadows the one in the POST body: keep these names out of it.
- The call record has no documented forwarding field: the server's log of the answer and hangup requests is the record.
- The `<Stream>` `start` event carries no forwarding fields: read them on the answer URL request and pass what the voice agent needs in `extraHeaders`, letters and digits only (drop `+` and anything from `;`).

Advise the user's code: cut numbers at the first `;`, drop a leading `+`, compare one format (India: `91` plus the 10 digits), compare reasons without case, and work with any field absent.

### "No ForwardedFrom" or "forwarded calls fail"

1. `plivo voice calls list --direction inbound --to <number> --since "<UTC>" -o json`. No record: see "Never arrives".
2. `plivo voice calls get <call_uuid> -o json`: `hangup_cause_code`, `hangup_cause_name`, `hangup_source`. Source `Answer XML`: the app's own XML hung up; check whether its logic rejects a missing or unexpected `ForwardedFrom`. 1010 (out of credits) or 5030 (concurrency limit): the account stopped the call. 7011 or 8011: the answer URL failed (voice-xml skill).
3. `plivo numbers get <digits> -o json`, then `plivo account applications get <app_id> -o json`: check `answer_url`, `answer_method` and `hangup_url` (none set: the hangup request goes to `answer_url`), and that no forwarding name sits in a query string.
4. Ask for the server's log of both requests for that `CallUUID`. Logged but unseen by the code: the query string, or code reading only query or only body. Not logged: step 5.
5. Compare a forward from another phone or carrier to the same number, and a direct dial. `plivo voice calls diagnose <call_uuid>` for one call; then Plivo support with the call UUID, the time and the forwarding type.

### Never arrives

- No record in the window: a record appears once the call ends, and a number may be stored with or without `+`, so try both. Still none: the call likely never reached Plivo. Check the target number and the forwarding type with the carrier: busy, no-answer and unreachable forwarding fire only in that condition.
- It may land on another of the account's numbers: drop `--to` and pass `--from <caller>`.
- Test from a third phone, never from the forwarding phone or the Plivo number.

### Test

- A `<Stream>` app, v1.3.3+: `plivo voice streams simulate --answer-url <url> --forwarded-from <number> --forwarded-cause 486 --forwarded-reason UserBusy --xml-only` requests the answer URL as Plivo would, with those fields, and checks the XML; it places no call. Run it again with `--forwarded-from` alone, and with none of the three. It refuses a Plivo-hosted answer URL (a console flow or an agent).
- Anything else: a real test forward. Set forwarding on a phone the user controls, call it from another phone, then read the record and the server log.

## B. Plivo forwards a call out

A forward is two calls: the A-leg (caller to Plivo) and the B-leg (Plivo to the destination). A B-leg record has `parent_call_uuid` set to the A-leg's UUID; when nobody answered there may be no B-leg at all.

1. `plivo voice calls get <a_leg_uuid> -o json`. Without an `action` that redirects, an unanswered `<Dial>` falls through to the next element; with none, the A-leg ends 4010 `End Of XML Instructions`, which looks normal.
2. Read the forward's outcome: `DialStatus`, `DialHangupCause` and `DialBLegUUID` (empty when nobody answered) on the Dial `action` URL; `DialBLegHangupCauseCode`, `DialBLegHangupCauseName` and `DialBLegHangupSource` on `callbackUrl`; a PHLO Call Forward node's state (Completed, No Answer, Busy/Rejected, Failed) with its `uuid`, `hangup_cause` and `dial_status`.
3. A B-leg record: `plivo voice calls get <b_leg_uuid> -o json`, or search the minutes around the call (`--all` stops after 100 pages): `plivo voice calls list --direction outbound --since "<UTC>" --until "<UTC>" --all --query "data.objects[?parent_call_uuid=='<a_leg_uuid>']"`.
4. `plivo voice calls diagnose <call_uuid>` if that does not explain it.

| Code | Means | Fix |
|---|---|---|
| 6010 `Ring Timeout Reached` | not answered in time | raise Dial `timeout` (120 s default) or the PHLO ring timeout (30 s, then Failed); add a fallback (voice-xml skill) |
| 3000 `No Answer` | destination unavailable or unreachable | check it is online; retry |
| 3010 `Busy Line`, 3020 `Rejected` | busy, or declined | a fallback or voicemail |
| 9000 `Lost Race` | another number in the same Dial answered first | none |
| 3030 `Unknown Caller ID` | a non-Plivo caller ID: `<Dial>` passes on the caller's number unless `callerId` is set | `callerId` (PHLO: From) = a number the account rents |
| 2000, 3050 | invalid or unallocated destination | E.164 with the country code |
| 2030, 2040, 2050 | country, premium number or prefix barred | console: Voice, Geo Permissions |
| 2060 `Loop Detected` | the B-leg would dial the A-leg's number | forward elsewhere; test from another phone |
| 2070 `Violates Media Anchoring` | India: both legs must stay in India | an India destination and server |
| 5030 `Concurrency Limit Breached` | every PSTN leg counts: a forward to a phone takes two | fewer calls, or a higher limit |
| 4240, 4250 | SIP credentials rejected, or no reply to the challenge | `sipAuthUsername` and `sipAuthPassword` on `<User>` (or IP auth); check the endpoint is up |
| 7012, 8012, 7013, 8013 | the `action` URL (with `redirect` true) or a transfer URL failed, or sent invalid XML | that URL (voice-xml skill) |

India: the caller ID must be a Plivo-rented Indian number (`plivo docs show voice/concepts/india-calling`). Every other code: `plivo docs show voice/troubleshooting/hangup-causes`.

A SIP address (`<User>`): forward to a phone number first. If that connects and SIP does not, the SIP path is at fault: Plivo's signaling blocked on port 5060, a private IP advertised, no PCMU or PCMA offered, RTP allowed one way only, or a firewall pinned to source port 5060. `plivo docs show voice/use-cases/transfer-to-human-agent/troubleshooting`.

Agent transfers:

- A voice agent on `<Stream keepCallAlive="true">`: the transfer XML runs only after the WebSocket closes, so close it when transferring (keep `keepCallAlive`). A `<Wait>` after the `<Stream>` holds the call if the socket closes before a Transfer API call arrives; through the API, transfer before stopping the stream. Still failing: a Multiparty Call (audio-streaming skill).
- A Plivo CX agent: wire the `call_forward` node's `no_answer`, `busy_rejected` and `failed` handles; read the run with `plivo agents runs list <agent_id>` and `plivo agents runs get <agent_id> <run_id>` (agents skill).
- PHLO (no CLI command): check the Call Forward node hangs off the Start node's Incoming Call trigger.

## C. A carrier forwards into a SIP trunk number

A number routed to an inbound trunk runs no answer URL, so there is no `ForwardedFrom`. The carrier's forwarding information, when it sends any, reaches the user's SBC as the SIP `Diversion` header on the INVITE Plivo sends to the origination URI.

1. `plivo numbers get <digits> -o json`: `application` contains `/Zentrunk/Trunk/<trunk_id>/`.
2. `plivo sip calls list --direction inbound --to-number <digits> --since "<UTC>" -o json`, then `plivo sip calls get <call_uuid> -o json`. A trunk call record has no documented forwarding field: read `Diversion` in the SBC's SIP trace of that INVITE, found by caller, number and time.
3. No `Diversion`: a direct dial, or the carrier sent none. Control: forward the same way to a number on an application and look for `ForwardedFrom`. Present there but missing on the trunk call: Plivo support with the trunk call UUID, the time and the SIP trace.
4. At the SBC but not in the application behind it: the SBC, PBX or platform drops or rewrites it.

Trunk failures and their codes: the sip-trunking skill, or `plivo sip calls diagnose <call_uuid>`.

## Finish

Report the kind (A, B or C), the call UUIDs, the evidence, the cause and the fix, and mark what stays unverified. Not documented, so route to Plivo support rather than guess: whether a PHLO or a CX agent exposes the forwarding fields to its nodes, and the raw SIP header a carrier sent on a call to an application.
