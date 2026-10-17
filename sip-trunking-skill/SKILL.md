---
name: plivo-sip-trunking
description: "Takes a new or existing Plivo user from a voice agent on LiveKit, ElevenLabs, Retell, Vapi, xAI or a self-hosted SIP stack to real phone calls over Plivo SIP trunking (Zentrunk), using the Plivo CLI. It checks credit, KYC and the number, asks which platform and which call directions, checks the platform's SIP host answers, creates the origination URI, credential or IP list and trunks, routes the number, walks the user through the platform-side setup and test calls in both directions, then covers go-live, SIP REFER transfers and debugging. Use for origination URIs, inbound or outbound trunks, TCP/TLS/UDP transport, Zentrunk hangup codes (4090, 4170, 4590), India calling rules. Not for WebSocket voice agents (plivo-audio-streaming) or Plivo XML (plivo-voice-xml)."
license: Apache-2.0
---

# Plivo SIP trunking: connect a voice-agent platform to phone calls

Take a developer, or their coding agent, from "my voice agent runs on platform X" to a Plivo number that reaches it, outbound calls that connect, transfers that work and failures they can read. One flow serves new and existing users: it checks what the account already has (credit, KYC, a number, trunks) and skips what is done. This file covers Plivo's side of the trunk and what the platform must match, not the voice agent's own logic. Related skills, if installed: `plivo skill install audio-streaming` (a WebSocket voice agent on `<Stream>`), `plivo skill install voice-xml` (Plivo XML), `plivo skill install` (the CLI's own skill). Without them, use `plivo <command> --help` and the docs pointers below.

If the user only asks about a failing call or a hangup code, skip the steps and go to "When a call fails". If they already have trunks, run "Readiness check" first and continue from the first step that fails.

## Rules for every step

- Use the CLI for every Plivo step and `-o json` to read values. `plivo <command> --help` wins on which commands and flags exist; this file wins on how Plivo behaves, including tested behaviour the help text omits. Never invent a flag, API field, hangup code, hostname or IP: where the CLI has none, use `plivo api <METHOD> <path>` or the console, and say so.
- Track the steps in your agent's own task or todo tool: TaskCreate and TaskUpdate in Claude Code (TodoWrite in older versions), the plan tool in Codex CLI. Mark a step done only when its check passes. If your agent has no task tool, print the step list, and print it again with ticks after each step.
- Ask every question with your agent's own structured question tool. In Claude Code it is AskUserQuestion: one call takes 1 to 4 questions with 2 to 4 options each, and it adds a free-text "Other" option itself, so do not add one. Without such a tool, ask a numbered list with an "Other" choice and wait for the answer.
- Every write is two commands, never one shell command: first with `--dry-run` (prints the request, sends nothing), then, after you show the preview and the user agrees, the same command with `--yes`. `plivo sip` create and update, `numbers update` and `numbers compliance create|update|link` write as soon as they run without `--dry-run`, so the preview is their only check. Deletes need `--yes` ("Write safety").
- Check the credit before every step: `cash_credits` in `plivo auth whoami -o json`. At 0 or less, tell the user to add credits on the console home page, <https://cx.plivo.com/home>, and go on with the free steps. Renting a number and calls cost money; trunks, URIs, credentials and IP lists are free to create.
- Before you route a number to a trunk, record its current `application` (`plivo numbers get <number> -o json`): it is the rollback.
- Use IDs exactly as the CLI printed them, in lowercase. Copy them from the JSON; never retype or change their case. In v1.2.0 an uppercase ID slips past the delete safety check; from v1.2.1 the CLI lower-cases IDs itself and refuses a malformed one.
- Always pass `--name` on `sip uris create`, `sip trunks create`, `sip credentials create` and `sip ip-acl create`. The API rejects a URI, trunk or IP list without one, and from v1.2.1 the CLI requires it there.
- Passwords go in on stdin only (`--password-stdin`), from `.env`, never on a command line, in a SIP URI or in chat. Never write `sip:user:password@host`: from v1.2.1 the CLI refuses it, and older versions store it and print it back in plain text. Never read, print or ask for a password or API key in the chat: the user writes it into `.env` with an editor, or you generate it into `.env` without printing it (step 7).
- Before v1.2.1, `sip trunks update` on an inbound trunk dropped its fallback URI when you changed anything else: on those versions, `get` the trunk first and pass `--fallback-uri <current fallback_uri_uuid>` with the change. From v1.2.1 an update keeps the fallback, and `--fallback-uri ""` removes it. Either way, read the trunk back after an update.
- Quote every `--uri` value: the `;` in `host;transport=tcp` otherwise ends the shell command.
- Pass numbers to `numbers` commands as digits without `+` (`14155551234`); `sip connect plan --number` takes `+14155551234`.
- Before v1.2.1, `numbers update` and other writes the API answers with no body print nothing on stdout, even with `-o json`; from v1.2.1 every write prints a result. Either way, check the exit code, then read the result back with a `get` command.
- Report each step as observed, not known or failed. Observed means you saw the evidence in this session. Never say "ready" and never infer account state. A created trunk, an HTTP 2xx or a passing `sip test` is not success: a call worked only when the call record, the platform's call log and a person who heard the audio agree.
- Verdicts: **will break** when a documented rule or an observed failure says so (name the failure); **risky** when plausible or untested (say what to check, name no hangup code); **style** when nothing changes. Hangup codes and names come from Plivo's public table. Anything marked *observed* (SIP responses, message sequences) is seen on the wire, not a Plivo contract: never quote it as one.

## Talking to the user

- Say "voice agent", not "bot". Name the platform the way the user does (LiveKit, ElevenLabs).
- Keep CLI and SIP words out of questions and the report: dry-run, E.164, trunk ID, URI UUID, origination URI, IP ACL, transport, KYC accepted, main account. Say "the address Plivo sends calls to", "the login your platform uses to call out", "the list of addresses allowed to call out". The questions written out in the steps are already right as they are, and commands and their previews stay as they are.
- Name trunks, URIs and apps by their names, not their IDs, in anything the user reads. Keep the IDs inside commands.
- Mask phone numbers as `+91 AB CDXX XEFG` (country code, first four digits, X for the next three, last three) and the account ID as its first four and last three characters with `…` between, in questions and the report. Show a number in full only where the user must dial it or type it into their platform, and inside commands.
- Show prices only for a number the user rents, never for calls, secure trunking or the balance. Take them from the step 4 search. The API gives no currency, so add `₹` only when you know the account bills in rupees.
- Keep each question to one or two short sentences: what happens, and what it changes. Question headers: Account, Platform, Calls, Number, Country, Buy number, Platform host, Secure, Create, Password, Platform setup, Route, Call in, Call out, Result, Debug, Finish.

## Steps 1 to 12: from the account to calls both ways

Create these twelve tasks first, then run the steps in order. Skip the work the account already has, but run every check. Stop at the first failed check and fix it before you go on. Steps 1 to 9 place no call.

1. Check tools and login
2. Check the credit
3. Ask the setup questions
4. Get the number
5. Check the platform answers
6. Plan the connection
7. Create the trunks
8. Set up the platform side
9. Route the number
10. Make test calls
11. Check the calls
12. Finish and report

### Step 1: check tools and login

```bash
plivo --version                  # if missing: brew install plivo/tap/plivo, or the install.sh one-liner from the Plivo docs
plivo auth whoami -o json        # exit 2 with AUTH_MISSING: the user logs in (below), then run this again
```

Check: `plivo --version` is 1.2.0 or later (`sip connect plan`, `sip test` and `--all` need it), and a line marked with a later version needs that version; if it is older, ask the user to run `plivo upgrade`. Then `whoami` shows the account the user expects.

- Login opens a browser, so the user runs it. On their own computer: `plivo login` in a terminal (in Claude Code, `! plivo login`). In a container, over SSH or in WSL: the user runs `plivo login` in a terminal on that machine, approves in the browser, then pastes the full URL from the browser's address bar into that terminal (v1.1.3 or later).
- Wrong organization: `plivo auth list`, then `plivo auth use <profile>`.

### Step 2: check the credit

Check: `cash_credits` in the step 1 output is above 0. If it is not, send the user to <https://cx.plivo.com/home> to add credits (in India, at least ₹2,000 by card), and read `whoami` again after they say it is done. Only renting a number and calls cost money, so steps 3 to 9 can run while they recharge.

### Step 3: ask the setup questions

First list the account's voice numbers: `plivo numbers list --services voice -o json` (`data.objects[]` has `number`, `country` and `application`). Then ask in one question-tool call:

| Header | Question | Options |
|---|---|---|
| Account | You're logged in as <first name> <initial>. (<masked account ID>). Use this account? | Yes · Use another account: "I'll show your other logins, or you can log in again." |
| Platform | Where does your voice agent run? | LiveKit · ElevenLabs · Retell · Vapi (the free-text "Other" covers xAI, another platform, or your own SIP server) |
| Calls | Which calls should go through Plivo? | Both ways (recommended): "People call your voice agent, and it calls people." · Incoming only: "People call your voice agent." · Outgoing only: "Your voice agent calls people from a Plivo number." |
| Number | Which number should your voice agent use? Its calls will go to the agent until you switch it back. | Up to two of the account's numbers, each "<masked number>: <city>. Now used by <app or trunk name>." · Buy a new number: "About <monthly price> a month." |

- Account: "Use another account": `plivo auth list`, then `plivo auth use <profile>`, or the user logs in again (step 1). Then run steps 1 and 2 again and ask again: the numbers belong to the account.
- Platform "Other": ask in plain text for the platform's name and, if it publishes one, its SIP address and transport. xAI has a Plivo guide (matrix); anything else follows the "Other or self-hosted" row, and Plivo publishes no guide for it: say so.
- Calls: xAI takes incoming calls only; if the user picked outgoing or both, say so and continue with incoming. Retell needs the outgoing setup even for incoming only (its import asks for it), so build it either way and tell the user why.
- Number: prefer numbers on no application, and name the application or trunk each offered number is on now, because routing takes the number away from it. Fill in the monthly price only when you have it from a search.
- If the account has no voice number, or the user picks "Buy a new number", ask "Which country should the number be in?" (header Country) with the account's country (recommended) and the other of India and the United States. The account's country is the country of its numbers, or India when it has an accepted India KYC application; with neither, India.
- **Vapi and an Indian number:** Vapi cannot take or place calls on Indian numbers (they end 4590, and no Plivo setting fixes it). Ask "Vapi can't handle calls on Indian numbers. What would you like to do?" (header Country) · Use a number in another country · Pick another platform: "LiveKit and ElevenLabs can run in India." Then ask again for the number or platform.
- Any Indian number: also run "India: four gates". Gate 1 (data region) is console-only: ask the user to confirm it.

### Step 4: get the number

**A number the account has.** Check: `plivo numbers get <number> -o json` shows it on this account with `voice_enabled` true. Record its `application` (step 9 needs it for rollback). It ends with `/Application/<app_id>/` (rollback `--app-id <app_id>`) or `/Zentrunk/Trunk/<trunk_id>/` (rollback `--trunk-id <trunk_id>`). If it is empty, tell the user before step 9 that `numbers update` cannot put it back to no application. An Indian number also needs the KYC check (India gate 2).

**A new number:**

1. India: rent only after KYC is accepted (India gate 2). If no application is `accepted`, stop, tell the user Indian numbers need accepted KYC, and send them to <https://cx.plivo.com/phone-numbers?tab=compliance>. `submitted` is not `accepted`. Run the check again when they say it is done.
2. Search: `plivo numbers search --country IN --type local --limit 10 -o json` (another country: its ISO code). Keep numbers with `voice_enabled` true. India: landline numbers (a city code such as 080 or 022); if a number has a `restriction`, show its `restriction_text`. Other countries: only numbers whose `restriction` is null.
3. Ask "Pick a number:" (header Buy number) with up to three numbers, each "<masked number>: <city> · <setup price> setup + <monthly price> a month" from `setup_rate` and `monthly_rental_rate`.
4. Preview `plivo numbers buy <number> --dry-run`, then ask "Buy <masked number> for <setup price> now and <monthly price> a month? You can release it anytime." · Buy it / Not now. On yes, run `plivo numbers buy <number> --yes`. Accounts that keep one compliance application per customer add `--compliance-application-id <id>`.
5. Check: `plivo numbers get <number> -o json` shows it on this account with `voice_enabled` true.

### Step 5: check the platform answers

Skip this step for outgoing only. Work out the address Plivo will send incoming calls to, from the platform matrix:

- **LiveKit:** ask in plain text "Open your LiveKit project's settings and copy its SIP URI (it ends in `.sip.livekit.cloud`). Paste just that address here." Strip any `sip:` prefix. For India, the project must use region pinning: use the endpoint LiveKit shows for it.
- **ElevenLabs:** `sip.rtc.elevenlabs.io` (India: `sip.rtc.in.residency.elevenlabs.io`, only with an ElevenLabs India deployment: ask the user to confirm they have one).
- **Retell, Vapi, xAI:** the fixed host in the matrix.
- **Other or self-hosted:** the host, port and transport the platform documents. Never compose a hostname or guess a transport.

Then ask "Encrypt calls between Plivo and <platform>? It hides call audio on the way, and Plivo charges extra per minute for it. You can turn it on later." (header Secure) · Not for this test (recommended) · Yes, encrypt them. Offer it only for LiveKit, ElevenLabs, Retell and a self-hosted server that supports TLS; Vapi and xAI skip it. Yes means TLS on the address (`;transport=tls`, port 5061 where the platform documents it) and `--secure` on the outgoing trunk, and the user turns it on in the platform too.

Check the address answers from this machine (no login, no call):

```bash
plivo sip test --uri "<host>[:port];transport=<tcp|tls|udp>" -o json
```

- TCP or TLS: `result` must be reachable (exit 0). A DNS failure, refused connection or certificate error is **will break**: fix the address before step 7.
- UDP (Vapi): `unknown` (exit 3) is normal, because hosted platforms ignore SIP from addresses they do not know. It proves nothing either way: move on and say so.
- `sip test` probes the host's own address records and does not follow SRV records, so test the exact host the platform documents. From v1.2.1, a host given without a port that has a SIP SRV record gets a warning naming the record's target and the `--uri` that tests it (`warnings` in JSON): run that test too. Plivo's docs do not say its calls follow SRV, so a host that fails here while its SRV target answers is not known, not broken and not working.
- A pass means the host answers, not that the platform will accept the call.

### Step 6: plan the connection

For LiveKit, ElevenLabs, Retell and Vapi:

```bash
plivo sip connect plan --number +<number> --platform <livekit|elevenlabs|retell|vapi> -o json
```

It changes nothing. Read `checks[]`: any failure stops the plan (exit non-zero, the checks so far under `error.context.checks`); fix it and run it again. `requests[]` lists, in order, the origination URI, the credential or IP list, the incoming and outgoing trunks and the `numbers update`, each ending in `--dry-run`; `next_actions[]` lists the platform-side steps. Keep only what the step 3 choices need: incoming only drops the outgoing trunk and its credential (except Retell); outgoing only drops the URI, the incoming trunk and the `numbers update`.

For xAI or another platform, build the same list by hand from the matrix and "Commands by object".

Tell the user in plain words what you will create, by name, for example "An address that sends calls to ElevenLabs, a login ElevenLabs uses to call out, and two trunks, one for each direction." Then ask "Create these on your Plivo account? They're free, and nothing changes on your number yet." (header Create) · Create them / Stop.

### Step 7: create the trunks

Name everything `<platform>-in` and `<platform>-out` (add `-2`, `-3` if a name is taken: `plivo sip trunks list --all -o json`). For each command, run the `--dry-run` preview from the plan, check it, then run it again with `--yes` in place of `--dry-run`, and read the object back with its `get`. Note each ID from the output, as printed.

**Incoming:**

```bash
plivo sip uris create --name <platform>-in --platform <platform> [--uri <host>] [--transport tls] --dry-run
plivo sip uris get <uri_uuid> -o json                     # the stored uri matches step 5
plivo sip trunks create --name <platform>-in --direction inbound --platform <platform> --uri <uri_uuid> --dry-run
plivo sip trunks get <inbound_trunk_id> -o json           # trunk_direction inbound, trunk_status enabled
```

A fallback URI (`--fallback-uri <uuid>`) is the only re-route when the primary does not answer: recommend one before go-live, not for the first test.

**Outgoing, with a login (LiveKit, ElevenLabs, Retell, most others).** The username is 5 to 20 letters and digits; the password is 5 to 20 characters from letters, digits and `~!@#$%^&*()_+`, with at least one of those special characters. Ask "Your platform needs a password to call out through Plivo. Should I make one?" (header Password) · Make one and save it in .env (recommended): "I won't show it here. You'll copy it from .env into <platform>." · I'll write my own in .env. To make one, without printing it:

```bash
grep -qs '^SIP_PASSWORD=' .env || printf 'SIP_PASSWORD=%s_\n' "$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 15)" >> .env
grep -qsx '.env' .gitignore || echo "Add .env to .gitignore before you commit"   # tell the user, do not edit their .gitignore unasked
```

For "I'll write my own": "Open .env in your editor and add a line SIP_PASSWORD='your password', in single quotes (5 to 20 characters, at least one of ~!@#$%^&*()_+). Tell me when it's saved. Don't paste it here." The quotes matter: the shell that reads `.env` below changes or rejects an unquoted `$`, `&`, `(`, `)` or leading `~`. Then:

```bash
( set -a; . ./.env; set +a; printf '%s' "$SIP_PASSWORD" | plivo sip credentials create --name <platform>-out --username <username> --password-stdin --dry-run )
plivo sip trunks create --name <platform>-out --direction outbound --platform <platform> --credential <credential_uuid> [--secure] --dry-run
plivo sip trunks get <outbound_trunk_id> -o json          # trunk_domain: <trunk_id>.zt.plivo.com
```

Pick a username such as `<platform>agent<4 digits>`; it is not secret, so show it.

**Outgoing, with an address list (Vapi, or a platform with published static IPs):**

```bash
plivo sip ip-acl create --name vapi-out --platform vapi --dry-run       # fills in Vapi's published addresses
plivo sip trunks create --name vapi-out --direction outbound --platform vapi --ip-acl <ipacl_uuid> --dry-run
```

`--ip` takes one address or range per flag: repeat it. Never add `/0` or `/1`, or anything wider than the platform publishes. IPv4 only: the API refuses IPv6, and from v1.2.1 so does the CLI.

`--platform` never turns on `--secure` itself, and prints `recommended: --secure` where the guide uses it: follow the user's step 5 answer.

### Step 8: set up the platform side

Plivo cannot see the platform, so the user does this part. Do it before step 9, so the number never points at a platform that is not ready. Give the user exactly what to enter, from the matrix's "Platform side" column and the plan's `next_actions[]`:

- Incoming: the number in full (`+<country code><number>`), imported on the platform and tied to the voice agent (LiveKit: an inbound trunk listing the number **and** a dispatch rule).
- Outgoing: the `trunk_domain` (no `sip:`, no spaces), the username, and "the password: open .env and copy the value after SIP_PASSWORD=". For Vapi, nothing secret: register the number as a bring-your-own SIP trunk number.
- Encryption, if chosen in step 5: TLS on the platform side too.

Then ask "Have you finished the setup in <platform>?" (header Platform setup) · Done · I need help: "Walk me through it." On "I need help", open Plivo's guide for that platform (`plivo docs show voice-agents/sip-trunking/integration-guides/<slug>`) and walk through its platform-side section one screen at a time.

### Step 9: route the number

Skip this step for outgoing only. Routing moves all of the number's incoming calls to the voice agent at once. Read the current state, preview, then ask "Send calls on <masked number> to your <platform> voice agent? They stop going to <old app or trunk name> until you switch back. I'll do that for you at the end." (header Route) · Yes, switch it / Keep it where it is. For a number on no application, replace the second sentence with "Once routed, it can move to an app or another trunk later, but it can't go back to having none."

```bash
plivo numbers get <number> -o json                                   # application: the rollback from step 4
plivo numbers update <number> --trunk-id <inbound_trunk_id> --dry-run
```

Run it again with `--yes` in place of `--dry-run`. Check: `application` in `plivo numbers get <number> -o json` ends with `/Zentrunk/Trunk/<inbound_trunk_id>/`. On an Indian number, `numbers update` first checks the KYC application: refused when it is not `accepted` or cannot be read, and from v1.3.0 also when none is attached (older versions only warn: treat that warning as **risky**).

### Step 10: make test calls

Check the credit first. Note the time (UTC) before the first call: step 11 lists from it.

- **Incoming:** ask "Call <number in full> from your phone and talk to your voice agent for 10 seconds or more. Tell me when you've hung up." (header Call in) · Done, it answered / It didn't answer. For an Indian number, from an Indian phone.
- **Outgoing:** the platform places the call, not the CLI. Ask "Now have <platform> call your phone from <masked number>, using its test or outbound call feature. Answer and talk for 10 seconds or more." (header Call out) · Done, it called me / It didn't call. The caller ID must be a Plivo number on this account (4190 otherwise).

After each call, ask (header Result) "Did your voice agent talk with you?" · Yes, it worked · Yes, but slow or choppy · No, silence · The call didn't come.

### Step 11: check the calls

```bash
plivo sip calls list --since "<UTC time from step 10>" --limit 5 -o json        # incoming: add --to-number <digits>; outgoing: --from-number <digits>
plivo sip calls get <call_uuid> -o json
plivo sip calls insights <call_uuid> -o json                                     # rtt, jitter, packet_loss, post_dial_delay, plivo_quality_score
```

Before v1.2.1, `sip calls list --since` without `--until` covers only the 30 days after the start (from v1.2.1 it runs to now): always give a recent `--since`. Records can take a minute to appear: retry before you conclude a call is missing. `plivo voice calls` reads Voice API calls, not trunk calls.

Check, for each direction tested: the user said "Yes, it worked", `hangup_cause_code` is 3000 or 3010, and the duration is 10 seconds or more. A 3010 with 0 s ended at once without saying who ended it: read the platform's call log. If a check fails or the user reports a problem, ask "Something went wrong. Want me to find out why? Plivo's call debugger takes a few minutes." (header Debug) · Yes, debug it (recommended) / Not now. On yes, run `plivo sip calls diagnose <call_uuid>` once and tell the user its answer in plain words, then go to "When a call fails". Slow or choppy: read Insights (high `rtt` is lag, `jitter` is robotic audio, `packet_loss` is broken audio).

### Step 12: finish and report

If the number came from another application or trunk (step 4 recorded one), ask "Done testing. What should happen to <masked number>?" (header Finish) · Keep it on the voice agent (recommended when both tests passed) · Switch it back to <old app or trunk name>. To switch back, preview the step 4 rollback (`--app-id` or `--trunk-id`) and run it.

Then report, short, in these words where they fit:

- Headline: "Your <platform> voice agent works on <masked number>: it answered a real call and called you. The call records and your answers agree." Name a direction not tested, or one that failed, plainly.
- A missed check, in one line: "The incoming call lasted <N> s (the check asks for 10 s). You heard the agent, so I didn't ask for a second call."
- Setup: "Number <masked number> (<city>)", "Incoming: trunk <name>, sending calls to <host>", "Outgoing: trunk <name>, address <trunk_domain>, login <username>", and "Previously on <old app or trunk name>" for a moved number.
- Costs: only for a number rented in this run: "New number: <setup price> setup, <monthly price> a month."
- "Your SIP password is in .env. Keep it out of git, and change it with `plivo sip credentials update` if it leaks."
- A number the user kept on the agent: "To switch it back later, ask me, or run:" then the rollback command in full.
- "Before real customers: add a fallback address, decide on encryption, and check your call limits." Details: "Go-live".

## Readiness check (read-only; for an existing setup, and again before go-live)

Stop at the first blocking answer. Steps 3 to 5 are incoming only and step 6 is outgoing only. Retell needs step 6 even for incoming only.

1. `plivo auth whoami -o json`: the account you meant, `cash_credits` above zero. 4030 (no credits) carries no direction in the docs, so treat zero as a risk to incoming too.
2. `plivo numbers get <number> -o json` (the incoming number, or the outgoing caller ID): on this account, `voice_enabled` true; for incoming, record `application`. India: the gates below.
3. Read `application`. `Trunk/<id>`: `plivo sip trunks get <id> -o json` (fields under `data.object`) must show `trunk_direction` `inbound`, `trunk_status` `enabled`, `primary_uri_uuid` set (without it: 4310 `uri_not_found`). `Application/<id>`: an XML application, not a trunk; stop. Any other shape: report it, do not guess.
4. `plivo sip uris get <primary_uri_uuid> -o json`: passes "URI checks" and matches the matrix; `authentication_needed` only when the platform challenges Plivo.
5. `fallback_uri_uuid` set? Missing is a warning: the fallback is the only re-route when the primary is unreachable or errors.
6. `plivo sip trunks list --direction outbound --all -o json`: an `enabled` trunk with `credential_uuid` (`plivo sip credentials get <uuid>`) or `ipacl_uuid` (`plivo sip ip-acl get <uuid>`: no `/0`, no `/1`, nothing wider than the platform's published addresses). Its `secure` must match the platform's outgoing transport: `true` only when the platform dials over TLS.
7. Concurrency and calls per second: console only, Organization settings > Account limits.
8. Platform-side items (step 8): Plivo cannot see them, so ask.
9. `plivo sip test --uri '<uri>'` (step 5).

## Reference

### Objects

- **Incoming:** origination URI (`host[:port];transport=udp|tcp|tls`) + inbound trunk + number. **Outgoing:** credential or IP access control list + outbound trunk; the platform dials the trunk's `trunk_domain`, `<trunk_id>.zt.plivo.com` (the console labels it Termination SIP Domain). Only inbound trunks attach to numbers.
- Commands: `plivo sip uris|trunks|credentials|ip-acl|calls|test|connect` and `plivo numbers update <number> --trunk-id <id>`. `plivo sip <group> <verb> --help` is the source of truth for flags.
- `plivo docs show <path>` prints a JSON envelope outside a terminal (agent shells too): add `-o table`. Its source drops field names from attribute lists: for those, read `https://www.plivo.com/docs/<path>.md`.
- List commands return 20 rows by default (`--limit` is 1 to 20): add `--all` (it stops at 2,000 rows and sets `meta.truncated: true`) before concluding something is missing.

### Commands by object

```bash
plivo sip uris create --name <name> --uri "<uri from the matrix>" --dry-run                         # prints uri_uuid
plivo sip trunks create --name <name> --direction inbound --uri <uri_uuid> [--fallback-uri <uuid>] --dry-run
( set -a; . ./.env; set +a; printf '%s' "$SIP_PASSWORD" | plivo sip credentials create --name <name> --username <username> --password-stdin --dry-run )
plivo sip ip-acl create --name <name> --ip <ip>/32 --ip <ip>/32 --dry-run
plivo sip trunks create --name <name> --direction outbound --credential <credential_uuid> --dry-run   # or --ip-acl <uuid>; --secure for TLS and SRTP
plivo numbers update <number> --trunk-id <inbound_trunk_id> --dry-run
```

Incoming TLS is the URI's `;transport=tls`, not `--secure`. If the platform challenges Plivo on incoming calls, its username and password go on the URI, a different secret from the outgoing credential: `( set -a; . ./.env; set +a; printf '%s' "$PLATFORM_SIP_PASSWORD" | plivo sip uris create --name <name> --uri "<uri>" --authentication-needed --username <username> --password-stdin --dry-run )`.

### Write safety

- **Routing a number** reroutes a possibly live number at once. Do it after the platform side exists. Pass only the trailing id of the recorded `application`: `/v1/Account/<auth_id>/Application/<app_id>/` is `--app-id <app_id>`, `.../Zentrunk/Trunk/<trunk_id>/` is `--trunk-id <trunk_id>`. `numbers update --trunk-id` reads the trunk even under `--dry-run` and refuses an outbound trunk or one not on this account, so its preview works only once the trunk exists. A number not on the account is refused.
- **Updates that hit live traffic:** `sip trunks update --status disabled|--secure|--uri` and `sip uris update --uri` take effect at once. `get` the object and record the current values first (before v1.2.1, also pass the current `--fallback-uri`: see the rules), then preview, then apply.
- **Deletes** need `--yes` and cannot be undone. Deleting an in-use URI also deletes the trunks that point at it, so their numbers stop routing; deleting a trunk detaches every number on it; deleting a credential or IP list breaks the outbound trunks using it. The check reads every page; a delete whose check fails stops with the read's own error (exit 2 login, 4 rate limit, 3 server or network) and deletes nothing; one with dependents is refused (exit 5) unless `--force` is added to `--yes`. Add `--force` only when the user has approved breaking those dependents. Preview: (v1.2.1+) `--dry-run` alone (with `--force` for an in-use object) shows the delete and its dependents; in v1.2.0 the delete run without `--yes` is the preview. A URI, credential or IP list delete lists the dependent trunks but not their numbers: count each trunk's numbers with `plivo numbers list --all -o json | jq '.data.objects[] | select((.application // "") | contains("/Zentrunk/Trunk/<trunk_id>/")) | .number'`. Disabling a trunk is reversible but an outage for its numbers, not a safe alternative.
- **Deleting a URI safely:** find its trunks with `plivo api GET /Zentrunk/Trunk/ --query "primary_uri_uuid=<uuid>" -o json`, then `fallback_uri_uuid=<uuid>`; count their numbers; repoint each trunk with `plivo sip trunks update <id>`: `--uri <new_uuid>` where it is the primary (before v1.2.1, add `--fallback-uri <current>`), `--fallback-uri <new_uuid>` where it is the fallback; test a call; then delete.
- `sip ip-acl update` replaces the whole list; `sip credentials update` always sets a new password from stdin.

### Platform matrix

Plivo publishes guides for LiveKit, ElevenLabs, Retell, Vapi and xAI, plus a generic one: `plivo docs show voice-agents/sip-trunking/integration-guides/<slug>` (https://plivo.com/docs/voice-agents/sip-trunking/integration-guides/<slug>), where `<slug>` is `livekit`, `elevenlabs`, `retell`, `vapi`, `xai-voice-agents` or `other-platforms`.

| Platform | Incoming URI | Outgoing auth | `secure` | Platform side (Plivo cannot see it) | India |
|---|---|---|---|---|---|
| LiveKit Cloud | `<project SIP host>;transport=tcp`; `tls` for secure trunking | credential | recommended; enable LiveKit secure trunking too | inbound trunk listing `+<number>` **and** a dispatch rule; outbound trunk with `trunk_domain`, credential username and password | region pinning, then the endpoint LiveKit shows |
| ElevenLabs | `sip.rtc.elevenlabs.io:5060;transport=tcp` or `:5061;transport=tls` (only these two) | credential | recommended; TLS in ElevenLabs' outbound settings | number imported on a SIP trunk with an agent; outbound settings with `trunk_domain` and credential | India deployment from ElevenLabs, then `sip.rtc.in.residency.elevenlabs.io:5060;transport=tcp` |
| Retell | `sip.retellai.com;transport=tcp` (or `tls`) | credential, **required even incoming only**: Retell will not import a number without it | off by default; if on, Retell Outbound Transport = TLS | import the number with `trunk_domain` (no `sip:`), credential username, password, transport; bind incoming and outgoing agents. Imports cannot be edited: delete and re-import | not in Plivo's docs; confirm with Retell |
| Vapi | `sip.vapi.ai;transport=udp` | IP list with the two `/32` addresses in Plivo's Vapi guide | no | register the number (BYO SIP trunk) and assign an assistant | **not supported** |
| xAI Voice Agents | `sip.voice.x.ai;transport=tls` (the console's `sip:sip.voice.x.ai;transport=tls` is fine too) | none: **incoming only** | not used | add the number to the agent (Direct SIP); allow every Plivo signaling range the xAI guide lists, or set the same SIP digest credentials on both sides | not stated; confirm with xAI |
| Other or self-hosted | `<host>[:port];transport=<what it documents>` (TLS usually 5061); the API also accepts `sip:user@host`, never with a password in it | credential; IP list only for published static IPs | both sides must agree | route the number to an agent; allow Plivo signaling and media (below); **no digest challenge** to Plivo unless the same username and password are on the URI | must terminate SIP and media in India |

`--platform livekit|elevenlabs|retell|vapi` fills the table's values: the host, port and transport on `sip uris create` (LiveKit has no shared host: pass yours with `--uri`) and Vapi's addresses on `sip ip-acl create`; on `sip trunks create` it only checks the trunk against the guide. It never sets `--secure`, and flags you pass win. Each preset carries the date it was checked; when that is more than 90 days old the CLI warns, so compare with the platform's docs. `sips:` means TLS. From v1.2.1, `--platform` gives a `sips:` URI the guide's TLS values (ElevenLabs gets port 5061; LiveKit and Retell add nothing; Vapi lists no TLS, so the CLI warns) and refuses another `--transport` with it; before v1.2.1 it filled in the TCP values unless you passed `--transport tls` and the port. Prefer the matrix's `host;transport=tls` form.

For a platform not listed, say Plivo publishes no guide for it, take the host, port and transport from the platform's own docs, and never compose a hostname or guess a transport. The Origination URI API takes only `name`, `uri`, `authentication_needed`, `username` and `password`. OpenAI Realtime is documented over audio streaming, not SIP (`plivo skill install audio-streaming`).

Self-hosted: allow every Plivo signaling range on 5060 (UDP/TCP) and 5061 (TLS), and media on 10000-30000, from `plivo docs show sip-trunking` (https://plivo.com/docs/sip-trunking, "Signaling / Media IP addresses"). A server that digest-challenges Plivo without the same credentials on the URI never completes the call. Do not promise 4150 for it: that published row is a carrier requiring proxy auth.

### India: four gates, each blocking until proved

1. **Data region.** The organization must be in the India data region. It cannot be changed (create a new organization) and is not readable over the API: confirm in the console.
2. **KYC.** A compliance application in `accepted` status (not `submitted`) linked to the number: `plivo numbers compliance list --country IN --status accepted -o json`, then `plivo numbers compliance get <compliance_id> --expand linked_numbers -o json`. The required documents come from the live `plivo numbers compliance requirements --country IN --number-type local --user-type business -o json`, never from memory. Documents and legal details come from the user: never fill them in or invent them, and never create, update or link an application unless the user explicitly says go (it is a regulatory filing, and `create` submits at once). `numbers update --app-id|--trunk-id` on a +91 number is refused unless its attached application is `accepted` and readable; from v1.3.0 it is also refused when none is attached (older versions only warn). Never add `--force` to get past a KYC refusal: tell the user what is missing. Before v1.2.1, a network error in this check can read like missing KYC: run it again before sending the user to file. Process: `plivo docs show numbers/rent-india-numbers` (https://plivo.com/docs/numbers/rent-india-numbers).
3. **Media anchoring.** The platform must terminate SIP and media in India, or calls end **4590** `domestic_anchored_terms_not_met`. 4590 is not a KYC check. **Vapi is not supported** and no Plivo-side setting fixes its 4590: use a platform that terminates in India (LiveKit region pinning, ElevenLabs India deployment) or a self-hosted stack there. `plivo docs show voice-agents/sip-trunking/deploy/calling-in-india` (https://plivo.com/docs/voice-agents/sip-trunking/deploy/calling-in-india).
4. **Traffic rules.** Both legs in India, the right number series for the call type, consent where the rules need it (service and transactional calls need none: `plivo docs show voice/concepts/ucc-management`), no cold calls, UCC complaints handled. `plivo docs show voice/concepts/india-calling` (https://plivo.com/docs/voice/concepts/india-calling).

### URI checks

Breaks or misroutes calls (the CLI refuses the first four on `sip uris create|update`, as `BAD_FLAG`; before v1.2.1, only the first two):

- A URL (`http://`, `https://`, any path): Plivo needs the SIP endpoint, not an answer URL.
- A space, no host, a host with characters other than letters, digits, dots and hyphens, a non-numeric port, or `transport=` other than `udp`, `tcp` or `tls`.
- A password inside the URI (`sip:user:pass@host`): before v1.2.1 it is stored and printed in plain text. Use `--authentication-needed` with `--password-stdin`.
- An IPv6 host: the API refuses it. Use a hostname or IPv4 (`sip test` still takes a bracketed IPv6 host).
- A private or unroutable host (10.x, 127.x, 192.168.x, 172.16-31.x, 169.254.x, 0.x).
- A host that differs from the documented host for the named platform.
- An Indian number with a non-India endpoint (4590).

Fix before go-live:

- No `;transport=`, or one the platform does not document. Plivo's generic guide calls a transport mismatch the most common reason an incoming integration fails silently, and Plivo documents no default, so always set what the platform documents.
- A port the platform's guide does not use, or 5061 without `transport=tls`. Some guides publish a TLS URI with no port: write what the guide shows.
- A `sip:` or `sips:` scheme or a user part where the platform's guide shows the host-first form (xAI's `sip:` form is fine).
- Any parameter other than `transport=`, a parameter with no value, or a host ending in a dot.

### Outgoing details

Give the platform the `trunk_domain` (no `sip:`, no spaces), the credential **username** (not its name) and the password. If the platform sends SIP OPTIONS health checks, point them at the `trunk_domain` only, at most one every 10 to 15 s; more may get blocked. Caller ID: a Plivo number on this account; anything else risks 4190 `unknown_caller_id`, and a same-account number gets STIR/SHAKEN attestation A (`plivo docs show sip-trunking/concepts/stir-shaken`).

`--secure` means TLS signaling and SRTP media on that trunk, so the platform must dial over TLS too. TLS or SRTP against a non-secure trunk ends 4110 `secure_trunking_disabled`; a secure trunk with the platform on TCP drops calls or loses audio (Retell guide).

Digest handshake (observed): INVITE, 407, then a second INVITE with credentials is normal on a credential trunk; an IP list trunk is not challenged. A flow that **ends** at 407, or 4180 `call_rejected_unauthorized`, means a wrong username or password, or a platform source IP missing from the IP list. Never open the list to `0.0.0.0/0` (or any `/0` or `/1`) to make it work: anyone who learns the `trunk_domain` could then call on the account. The CLI warns on wide ranges but does not block them.

### Transfer to a human with SIP REFER (optional)

- The platform sends REFER with `Refer-To: <sip:+<E.164 number>@<trunk_domain>>`. Only your own `.zt.plivo.com` `trunk_domain` works: other domains, arbitrary SIP URIs and private IPs get 403, and a missing or malformed `Refer-To` gets 400. `tel:` or `sips:<IP>` targets are undocumented: do not rely on them. LiveKit sends REFER from `TransferSIPParticipant` (LiveKit's docs): give it this form.
- Plivo answers 202 and reports progress with NOTIFY (100, 180, then 200 or 4xx/5xx); answer each with 200 OK. Never send BYE before `NOTIFY 200 OK`: Plivo sends the agent leg a BYE once the target answers.
- After a `NOTIFY 4xx/5xx` the caller is still on the agent leg, in silence: make the agent speak at once, then retry or continue. A dialog that ends after `NOTIFY 100` means the agent leg dropped too early.
- If the target hangs up, the whole call ends. International targets need Geo Permissions. Expect two call records (the transfer leg's can take up to 60 s). REFER legs count toward concurrency. Outgoing transfers need two Plivo numbers.
- `plivo docs show sip-trunking/concepts/sip-refer-inbound` and `plivo docs show sip-trunking/concepts/sip-refer` (https://plivo.com/docs/sip-trunking/concepts/sip-refer).

## Go-live

- The readiness check passes for every direction in use, including `sip test`; warnings understood.
- Concurrency and calls per second sized for peak. SIP trunking **rejects** calls above either limit (5180 CPS, 5190 concurrency) and never queues; every phone-network leg counts, incoming and outgoing, shared with the Voice API. Pace the dialer. `plivo docs show sip-trunking/concepts/account-limits`. US abandoned and short-call thresholds: `plivo docs show voice-agents/sip-trunking/deploy/us-call-quality-and-cps`.
- Geo Permissions (console: Zentrunk, Geo Permissions) trimmed to the countries the agent calls: `plivo docs show sip-trunking/concepts/geo-permissions`.
- A fallback URI; an encryption decision made on both sides; passwords only in `.env` or a secret store; IP lists narrow.
- India: all four gates. US: Plivo caller ID, dialer under the CPS limit.
- Watch: `plivo sip calls list --hangup-cause-code 4090 --since <date> -o json` (and 4170, 4180, 4590, 4030, 5180, 5190). Outgoing 4410, 4340 and 4550 are outcomes, not faults, but every unanswered US attempt counts toward the abandoned-call threshold.

## When a call fails

When a call fails or the user says something did not work, first ask whether they want you to debug it (the Debug question in step 11), and ask again before another run. Run each layer once, keep the evidence, and do not repeat a billed call until the failed layer passes.

1. `plivo sip calls list -o json` (filters: `--direction`, `--hangup-cause-code`, `--hangup-source`, `--from-number`, `--to-number`, `--since`, `--until`; it strips a leading `+` from numbers), then `plivo sip calls get <call_uuid> -o json` and `plivo sip calls insights <call_uuid>`.
2. `hangup_source`: `customer` = your platform or SIP server, `carrier` = the phone network, `zentrunk` = Plivo. The `sip calls get` table ends with which side ended the call; the platform's final SIP response is in no API, only the console's SIP logs (`plivo open sip-call <call_uuid>`). Map the code with the table below.
3. Console: Zentrunk, Logs, the call. Call Stats shows trunk, transport and secure; SIP logs show the message flow, the final response and a PCAP download. The hangup code is Plivo's conclusion and the SIP flow is what the platform said; they can disagree. Reading the flow (observed): incoming with no INVITE to your URI usually means Plivo refused (4590, 4030, 4310); INVITE repeated with no reply usually ends 4170; otherwise the platform's own response is the answer (404 not imported, 401/407 it challenged Plivo, 486 no dispatch rule or agent, 503 down).
4. `plivo sip calls diagnose <call_uuid>` asks Plivo's assistant about one trunk call (`plivo voice calls diagnose` refuses trunk calls). If it cannot retrieve the call (before v1.2.1, a network error also reads as "not found"), use `plivo sip calls get`. It shares a small rate limit with `plivo ask`: do not loop it. Then Plivo support with the call UUID and the PCAP.

| Code (name) | Dir | Seen on the wire (observed) | Fix |
|---|---|---|---|
| 3000, 3010 `normal_hangup` | both | 200, then BYE | none (3010 with 0 s: read the platform logs) |
| 4090 `destination_not_found` | in | platform 404 (ElevenLabs: `Does not match any SIP Trunks`) | import `+<number>`; bind an agent or dispatch rule |
| 4170 `request_timeout_customer` | in | INVITE repeated, no reply | URI host, port and transport; firewall; platform online; `sip test` |
| 4150 `proxy_authentication_required` | in | platform 401/407 | stop challenging Plivo, or `authentication_needed` with the same credentials on the URI (the published row names a carrier) |
| 4410 `user_busy` | in | platform 486 | dispatch rule, agent availability, platform concurrency |
| 4350, 5360 | in | platform 480, 503 | platform status; fallback URI; TLS consistency |
| 4420 `carrier_cancelled` | in | caller CANCEL, 487 | none; answer faster |
| 4310 `uri_not_found` | in | no platform leg | set `primary_uri_uuid` |
| 4030 `insufficient_plivo_credits` | any | no platform leg | add credits; auto-recharge |
| 4590 `domestic_anchored_terms_not_met` | any | Plivo 403, no platform leg | India: platform must terminate SIP and media in India (not a KYC check) |
| 4180 `call_rejected_unauthorized` | out | Plivo 403 | credential username and password, or the platform IP in the IP list |
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

Use `plivo docs search <terms>` and `plivo docs show <path>` first (no credentials, no rate limit), then `plivo ask "<question>"`, which can see the account but has a small rate limit. Treat its answers as evidence and prefer the docs where they conflict. `--help` outranks this file on flags. Out of scope: PBX interconnection, carrier rates, Plivo XML (`plivo skill install voice-xml`) and WebSocket voice agents (`plivo skill install audio-streaming`).
