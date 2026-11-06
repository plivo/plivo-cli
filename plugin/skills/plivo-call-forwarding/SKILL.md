---
name: plivo-call-forwarding
description: "Debugs call forwarding on Plivo with the Plivo CLI, in three cases: a carrier forwards calls into a Plivo number and ForwardedFrom, ForwardedCause or ForwardedReason is missing or odd on the answer and hangup URLs; Plivo forwards a call out with Dial XML, a PHLO Call Forward node or an agent transfer and the forwarded leg fails; a carrier forwards into a SIP trunk number and the Diversion header must reach the customer's SBC. It finds the call, reads both legs and the app, maps hangup causes and tests with voice streams simulate or a real forward. Use when forwarded calls never arrive, lack the forwarding number or fail. Not for writing XML (plivo-voice-xml) or setting up a SIP trunk (plivo-sip-trunking)."
license: Apache-2.0
---

# Plivo call forwarding: debug a forwarded call

Rules here come from the Plivo docs and from forwarded calls Plivo has traced; value lists marked *seen* are examples from live calls, not complete sets. Name a cause only when the evidence shows it, and never invent a field, flag or hangup code. Setup lives in other skills: `plivo skill install voice-xml` (Dial XML, forward then voicemail), `sip-trunking` (trunks, origination URIs), `audio-streaming` (a `<Stream>` voice agent and its handoff) or `agents` (Plivo CX agents).

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
| A. Into your number | A phone or another provider forwards its calls to your Plivo number, and your answer URL runs | `plivo numbers get <digits> -o json`: `application` does not contain `/Zentrunk/Trunk/` |
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

Advise the user's code: cut each number at the first `;`, drop a leading `+`, compare in one format (for India, `91` and the 10 digits), compare reasons without case, and work when any field is absent.

### "No ForwardedFrom" or "forwarded calls fail"

1. `plivo voice calls list --direction inbound --to <number> --since "<UTC>" -o json`. No record: see "Never arrives".
2. `plivo voice calls get <call_uuid> -o json`: `hangup_cause_code`, `hangup_cause_name`, `hangup_source`. Source `Answer XML`: the app's own XML hung up; check whether its logic rejects a missing or unexpected `ForwardedFrom`. 1010 (out of credits) or 5030 (concurrency limit): the account stopped the call. 7011 or 8011: the answer URL failed (voice-xml skill).
3. `plivo numbers get <digits> -o json`, then `plivo account applications get <app_id> -o json`: check `answer_url`, `answer_method` and `hangup_url`, and that no forwarding name sits in a query string.
4. Ask for the server's log of both requests for that `CallUUID`. In the log but not seen by the code: the query string, or code that reads only one of query and body. Missing from the log too: step 5.
5. Compare a forward from another phone or carrier to the same number, and a direct dial. `plivo voice calls diagnose <call_uuid>` for one call; then Plivo support with the call UUID, the time and the forwarding type.

### Never arrives

- No record in the window: the call never reached Plivo. Check the target number and the forwarding type with the carrier. Busy, no-answer and unreachable forwarding fire only in that condition.
- It may land on another of the account's numbers: drop `--to` and pass `--from <caller>`.
- Test from a third phone, never from the forwarding phone or the Plivo number.

### Test

- A `<Stream>` app, v1.3.3+: `plivo voice streams simulate --answer-url <url> --forwarded-from <number> --forwarded-cause 486 --forwarded-reason UserBusy --xml-only` requests the answer URL as Plivo would, with those fields, and checks the XML; it places no call. Run it again with `--forwarded-from` alone, and with none of the three. It refuses a Plivo-hosted answer URL (a console flow or an agent).
- Anything else: a real test forward. Set forwarding on a phone the user controls, call it from another phone, then read the record and the server log.

## B. Plivo forwards a call out

A forward is two calls: the A-leg (caller to Plivo) and the B-leg (Plivo to the destination), each with its own record and hangup cause. The B-leg's `parent_call_uuid` is the A-leg's UUID.

1. `plivo voice calls get <a_leg_uuid> -o json`. When nothing runs after the `<Dial>`, a failed forward ends the A-leg 4010 `End Of XML Instructions`, which looks normal: the cause is on the B-leg.
2. Find the B-leg: `DialBLegUUID` on the Dial `action` URL (empty when nobody answered), the PHLO node's `uuid` variable, or `plivo voice calls list --direction outbound --since "<UTC>" --all --query "data.objects[?parent_call_uuid=='<a_leg_uuid>']"`. Then `plivo voice calls get <b_leg_uuid> -o json`.
3. The `action` URL also gets `DialStatus` (`completed`, `busy`, `failed`, `cancel`, `timeout`, `no-answer`) and `DialHangupCause`; `callbackUrl` gets `DialBLegHangupCauseCode`, `DialBLegHangupCauseName`, `DialBLegHangupSource`. A PHLO Call Forward node ends Completed, No Answer, Busy/Rejected or Failed, with `hangup_cause` and `dial_status`.
4. `plivo voice calls diagnose <b_leg_uuid>` if the record does not explain it.

| Code | Means | Fix |
|---|---|---|
| 3000 `No Answer`, 6010 `Ring Timeout Reached` | nobody answered in time | raise Dial `timeout` or the node's ring timeout (30 s by default); a fallback after the Dial or in the `action` document (voice-xml skill) |
| 3010 `Busy Line`, 3020 `Rejected` | busy, or declined | a fallback or voicemail |
| 3030 `Unknown Caller ID` | a non-Plivo number was the caller ID; `<Dial>` passes on the caller's number unless `callerId` is set | `callerId` (PHLO: From) = a number the account rents |
| 2000, 3050 | invalid or unallocated destination | E.164 with the country code |
| 2030, 2040, 2050 | country, premium number or prefix barred | console: Voice, Geo Permissions |
| 2060 `Loop Detected` | the B-leg would dial the A-leg's number | forward elsewhere; test from another phone |
| 2070 `Violates Media Anchoring` | India: both legs must stay in India | an India destination |
| 5030 `Concurrency Limit Breached` | every PSTN leg counts: a forward to a phone takes two | fewer calls, or a higher limit |
| 4240, 4250 | SIP credentials rejected, or no reply to the challenge | `sipAuthUsername` and `sipAuthPassword` on `<User>`; endpoint up |
| 7013, 8013 | a transfer URL failed, or returned invalid XML | that URL (voice-xml skill) |

India: the caller ID must be a Plivo-rented Indian number (`plivo docs show voice/concepts/india-calling`). Every other code: `plivo docs show voice/troubleshooting/hangup-causes`.

A SIP address (`<User>`): forward to a phone number first. If that connects and SIP does not, the fault is on the SIP path: no INVITE arrives (allow Plivo's signaling on port 5060), no audio (the endpoint must advertise its public IP), rejected before ringing (offer PCMU or PCMA), one-way audio (allow the RTP ranges both ways), the line stays up after a hangup (do not pin source port 5060). `plivo docs show voice/use-cases/transfer-to-human-agent/troubleshooting`.

Agent transfers:

- A voice agent on `<Stream keepCallAlive="true">`: the transfer XML runs only after the WebSocket closes. Close it when transferring (keep `keepCallAlive`), add a `<Wait>` after the `<Stream>`, or use a Multiparty Call (audio-streaming skill).
- A Plivo CX agent: wire the `call_forward` node's `no_answer`, `busy_rejected` and `failed` handles; read the run with `plivo agents runs list <agent_id>` and `plivo agents runs get <agent_id> <run_id>` (agents skill).
- PHLO: check the Call Forward node hangs off the Start node's Incoming Call trigger. The CLI has no PHLO command: read the B-leg record.

## C. A carrier forwards into a SIP trunk number

A number routed to an inbound trunk runs no answer URL, so there is no `ForwardedFrom`. The carrier's forwarding information, when it sends any, reaches the user's SBC as the SIP `Diversion` header on the INVITE Plivo sends to the origination URI.

1. `plivo numbers get <digits> -o json`: `application` contains `/Zentrunk/Trunk/<trunk_id>/`.
2. `plivo sip calls list --direction inbound --to-number <digits> --since "<UTC>" -o json`, then `plivo sip calls get <call_uuid> -o json`. A trunk call record has no documented forwarding field: read `Diversion` in the SBC's SIP trace of that INVITE, found by caller, number and time.
3. No `Diversion`: a direct dial, or the carrier sent none. Control: forward the same way to a number on an application and look for `ForwardedFrom`. Present there but missing on the trunk call: Plivo support with the trunk call UUID, the time and the SIP trace.
4. At the SBC but not in the application behind it: the SBC, PBX or platform drops or rewrites it.

Trunk failures and their codes: the sip-trunking skill, or `plivo sip calls diagnose <call_uuid>`.

## Not documented

Say so instead of guessing, and route to Plivo support: whether a PHLO or a CX agent exposes the forwarding fields to its nodes, and the raw SIP header a carrier sent on a call to an application.

## Report

Give the kind (A, B or C), the call UUIDs, what Plivo received and sent with its evidence, the cause, the fix, and what stays unverified.
