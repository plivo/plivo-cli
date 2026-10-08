# `plivo feedback`

Share feedback about the Plivo CLI — a 1-5 rating and an optional comment.
Useful for feature asks, or just letting us know something feels off. For a
bug, `--bug` attaches the last command that failed (see
[Bug reports](#bug-reports---bug)).

## Usage

```bash
plivo feedback                              # interactive
plivo feedback --rating 4                   # one-shot rating only
plivo feedback --message "..."              # one-shot comment only
plivo feedback --rating 2 --message "..."   # one-shot both
plivo feedback --rating 5 --yes             # skip pre-submit confirmation
plivo feedback --bug --dry-run              # show a bug report, send nothing
plivo feedback --bug --message "..." --yes  # report the last failure
```

## Flags

| Flag | Description |
|---|---|
| `--rating <1-5>` | One-shot rating. Skips the interactive rating prompt. |
| `--message <text>` | One-shot comment (max 500 chars). Skips the interactive comment prompt. |
| `--no-context` | Don't auto-attach CLI version / OS / arch metadata. CLI version still attached (needed for any aggregate). |
| `--yes` | Skip the pre-submit confirmation step. A bug report sent without a terminal needs it. |
| `--bug` | Send a bug report: your comment plus the last failed command, printed in full before sending. |

## What gets sent

When you submit feedback, the following is attached automatically:

- **Your rating** (1-5)
- **Your comment** — after client-side PII scrubbing (phone numbers,
  auth tokens, emails, and Plivo auth IDs are replaced with
  `[REDACTED-*]` placeholders)
- **CLI version**, OS, architecture
- **Anonymous machine ID** (one UUID per machine, persisted at
  `~/.plivo/machine-id`)
- **Session ID** (one UUID per CLI invocation)
- If you're logged in and haven't opted out (see below): your **auth
  ID**, **email**, **region**, and **AOM UUID**, sent as request headers
  — raw, not hashed — so feedback joins the same per-account view as
  everything else the CLI reports.

We do NOT collect:

- Phone numbers (any format)
- Auth tokens or scoped tokens
- File paths or attachment paths
- Free-text from `plivo ask` / `plivo support` message bodies
- Argument values you passed to the CLI

The client-side redaction is defence-in-depth — the collector re-runs
the same scrub server-side.

## Bug reports (`--bug`)

Every command that fails records the failure in `~/.plivo/last-error.json`
(mode 0600, replaced on each failure): the command path (for example
`plivo voice calls get`), exit code, error code, request ID, CLI version,
OS/arch and time. Never your arguments or flag values, and never the error
message, since either can echo what you typed. A failed `plivo feedback`
does not replace it.

`plivo feedback --bug` builds a report from that record and your comment
(`--message`, or a prompt in a terminal). The collector keeps only the
comment field of an event, so the recorded failure is added to the comment,
after your text has been scrubbed. Before anything is sent, the command
prints the exact request: the endpoint, the `X-Plivo-CLI-*` and `Client-*`
headers (including the identity headers while telemetry is on, see below),
and the JSON body.

- In a terminal it then asks `Send this report? [Y/n]`.
- Without a terminal it refuses (exit 5) unless you pass `--yes`.
- `--dry-run` prints the report and sends nothing.
- If sending fails, it prints a prefilled GitHub issue link for
  `plivo/plivo-cli` with your scrubbed comment and the recorded failure. The
  link carries no account details and is capped in length; nothing is filed
  until you submit the form.

## Privacy & opt-out

`plivo config telemetry off` (or `PLIVO_CLI_TELEMETRY=0`) strips the
auth ID / email / region / AOM UUID headers from every CLI request,
feedback included — your rating, comment, and CLI version still send.
`PLIVO_FEEDBACK_TELEMETRY=0` goes further and disables feedback
submission entirely; `PLIVO_FEEDBACK_PROMPT=0` only silences the
auto-prompt. The explicit `plivo feedback` command still works either
way, unless you've set `PLIVO_FEEDBACK_TELEMETRY=0`.

## How it's sent

A single HTTPS POST to the Plivo CLI feedback collector;
`PLIVO_FEEDBACK_ENDPOINT` points it at another one. 5-second timeout. If
the collector can't be reached or rejects the event, the command exits 3
with a network error; nothing is silently dropped. A bug report also prints
a prefilled GitHub issue link so you can file it there instead.

## Examples

### Interactive (most common)

```
$ plivo feedback

 How's plivo CLI? (1-5, Enter to skip rating)
   1 = bad    2 = not great    3 = ok    4 = good    5 = love it
 > 4

 Anything to add? Optional. (multi-line; press Enter twice or Ctrl-D to finish)
 > the SSE for `plivo ask` sometimes hangs on first connect

 About to submit:
   Rating:   4/5
   Comment:  the SSE for `plivo ask` sometimes hangs on first connect
   Metadata: CLI v0.1.0-beta.3, darwin/arm64
 Submit? [Y/n] y

✓ Submitted. Thanks!
  Use `plivo feedback` anytime to share more.
```

### One-shot in a CI script

```bash
plivo feedback --rating 1 --message "compliance create failed in CI run #1234" --yes
```

### Comment with PII (auto-redacted)

```
$ plivo feedback --rating 2 --message "tried with MAABCDEFGHIJKLMNOPQR and got HTTP 500" --yes

 About to submit:
   Rating:   2/5
   Comment:  tried with [REDACTED-AUTH-ID] and got HTTP 500
             (PII redacted: 1 match(es))
   Metadata: CLI v0.1.0-beta.3, darwin/arm64
 Submit? [Y/n] y

✓ Submitted. Thanks!
```

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Submitted, nothing to submit, answered N at the confirmation, or `--dry-run` |
| 1 | Invalid flag value (e.g. `--rating 9`, comment over 500 chars) |
| 3 | Network error reaching the collector |
| 5 | `--bug` without a terminal and without `--yes` |
| 130 | Ctrl-C |
