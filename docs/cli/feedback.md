# `plivo feedback`

Share feedback about the Plivo CLI — a 1-5 rating and an optional comment.
Useful for bug reports, feature asks, or just letting us know something
feels off.

## Usage

```bash
plivo feedback                              # interactive
plivo feedback --rating 4                   # one-shot rating only
plivo feedback --message "..."              # one-shot comment only
plivo feedback --rating 2 --message "..."   # one-shot both
plivo feedback --rating 5 --yes             # skip pre-submit confirmation
```

## Flags

| Flag | Description |
|---|---|
| `--rating <1-5>` | One-shot rating. Skips the interactive rating prompt. |
| `--message <text>` | One-shot comment (max 500 chars). Skips the interactive comment prompt. |
| `--no-context` | Don't auto-attach CLI version / OS / arch metadata. CLI version still attached (needed for any aggregate). |
| `--yes` | Skip the pre-submit confirmation step. |

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

## Privacy & opt-out

`plivo config telemetry off` (or `PLIVO_CLI_TELEMETRY=0`) strips the
auth ID / email / region / AOM UUID headers from every CLI request,
feedback included — your rating, comment, and CLI version still send.
`PLIVO_FEEDBACK_TELEMETRY=0` goes further and disables feedback
submission entirely; `PLIVO_FEEDBACK_PROMPT=0` only silences the
auto-prompt. The explicit `plivo feedback` command still works either
way, unless you've set `PLIVO_FEEDBACK_TELEMETRY=0`.

## How it's sent

A single HTTPS POST to the collector configured via
`PLIVO_FEEDBACK_ENDPOINT`. 5-second timeout. If the collector is
unreachable, you'll see a clear "couldn't reach the feedback endpoint"
message; nothing is silently dropped.

While the backend collector is still being wired up (early access /
beta period), the command will instead print:

```
⚠ Feedback collector endpoint not configured yet (PLIVO_FEEDBACK_ENDPOINT unset).
  Your feedback was prepared but not sent. The CLI team is wiring this up;
  for now, please open an issue at https://github.com/plivo/plivo-cli/issues.
```

— and exit cleanly with status 0.

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
| 0 | Submitted, or "nothing to submit", or "collector not configured yet" |
| 1 | Invalid flag value (e.g. `--rating 9`, comment over 500 chars) |
| 3 | Network error reaching the collector |
| 130 | User cancelled (Ctrl-C, or answered N to the pre-submit confirm) |
