# Changelog

All notable changes to the Plivo CLI are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2026-06-17

### Added

- `plivo ask -i` / `--interactive` — an interactive chat REPL on top of
  `plivo ask`. Each turn replays recent conversation as history so
  follow-ups keep context (a one-shot `ask` still sends none). Supports
  `/reset` to start fresh, `/help`, and `/exit` (or Ctrl-D); Ctrl-C
  cancels just the in-flight turn, not the REPL. History is capped to the
  most recent turns, and an optional message seeds the first turn.
  Single-shot behaviour is unchanged.
- `plivo voice streams test` and `plivo voice streams forward` — local-dev
  workflow for WebSocket-based call audio. `test` pre-flights a customer
  WS endpoint with synthetic audio frames (no Plivo backend involved);
  `forward` saves an app's `answer_url`, spins up an ngrok tunnel + local
  HTTP/WS server, points the app at the tunnel, bridges incoming call
  audio to the user's local WebSocket handler, and restores the original
  `answer_url` on Ctrl+C (`--keep` to skip restore).
- `plivo feedback` ships events to PostHog via the new hodor route
  `POST /v1/accounts/cli/feedback`. Captures rating + sanitised comment +
  identity (account_id / email / region / aom_uuid) so feedback joins the
  same Persons as `cli.request` events in PostHog. Custom collector still
  supported via `PLIVO_FEEDBACK_ENDPOINT`. Opt out of all submission with
  `PLIVO_FEEDBACK_TELEMETRY=0`.
- Post-success **feedback auto-prompt** — once per 24h on an interactive
  TTY, the CLI asks `Got 30s to rate the CLI? [y/N]` after a successful
  command. State persists in `~/.plivo/feedback-state.json`. Opt out with
  `PLIVO_FEEDBACK_PROMPT=0` (separate knob from `PLIVO_FEEDBACK_TELEMETRY`
  so users can keep manual `plivo feedback` working while silencing the
  prompt).
- `cli-skill/` — Claude Code skill files at the repo root (`SKILL.md`
  with the full command reference + safety knobs + install + upgrade
  guidance). Auto-triggers when users mention Plivo / Contacto / Vibe /
  PHLO. Install locally with
  `ln -s "$(pwd)/cli-skill" ~/.claude/skills/plivo-cli`.
- **Per-user analytics attribution** — every request ships
  `X-Plivo-CLI-Email`, `X-Plivo-CLI-Auth-ID`, `X-Plivo-CLI-Region`, and
  `X-Plivo-CLI-AOM-UUID` (when the active profile has them populated).
  Hodor lifts these into PostHog `cli.request` + `cli.feedback` events so
  dashboards can filter per-human within an org — `auth_id` alone is
  org-level (multiple humans share it).
- Profile now persists `email`, `name`, `aom_uuid`, and `region`
  alongside `auth_id`. Captured at login time from hodor's PKCE response
  or the email/password response. Sent on every request so analytics
  stays per-human without an extra round-trip per command.
- `plivo login` interactive picker → `plivo login` directly opens the
  browser PKCE flow by default. `--manual` triggers the auth_id + token
  prompt explicitly; `--email` opens the email/password flow (internal
  builds only).
- `plivo ask "<query>"` + `plivo support` — talks to Plivo's
  customer-facing AI assistant (SSE streaming, Plivo Basic auth).
  `--call-uuid` adds voice-debug context. Ctrl-C cancels cleanly; `-o
  json` emits JSONL events for scripts and AI agents. `plivo support`
  lists past escalations.
- Three-segment command grammar: `plivo <service> <resource> <verb>`
  (e.g. `plivo voice calls list`, `plivo messaging sms send`,
  `plivo numbers search`).
- Cross-platform installers for macOS, Linux, and Windows (`install.sh`,
  `install.ps1`), with architecture auto-detection.
- `plivo agent` ships as a coming-soon stub in the public build.
- `numbers compliance` — the unified number-compliance API:
  requirements, application create/get/list/update/delete (with document
  uploads), and bulk number linking.

### Changed

- `plivo login` defaults to the browser PKCE flow (no flag needed). The
  earlier "auth_id + token paste" interactive default now requires
  explicit `--manual`. `--auth-id MA…` and `--auth-token-stdin` continue
  to work for CI scripts.
- `plivo --profile X feedback` now uses profile X's identity for the
  event, not the active profile (regression fix —
  `resolveAuthIDForFeedback` was ignoring `--profile`).
- Commands are grouped under service namespaces (`voice`, `messaging`,
  `numbers`, `verify`, `account`). The pre-grammar short forms (`plivo
  call list`, `plivo msg send`, …) continue to work as aliases.
- Messaging uses a per-channel form: `plivo messaging sms send` /
  `messaging whatsapp send` / `messaging mms send`. `sms` and `msg`
  remain aliases of `messaging`. Universal `plivo messaging get <uuid>`
  works across channels.

### Removed

- **Breaking:** `plivo login --email`, `--auth-id`, `--manual`,
  `--auth-token-stdin`, `--env`, and `--browser` flags. Browser PKCE is
  the only `plivo login` method on main. For headless / CI usage, set
  `PLIVO_AUTH_ID` + `PLIVO_AUTH_TOKEN` environment variables instead and
  skip `plivo login` entirely — every command picks credentials up from
  the environment.

  Migrating CI scripts:

      # before
      echo "$TOKEN" | plivo login --auth-id MA... --auth-token-stdin
      plivo voice calls list

      # after
      export PLIVO_AUTH_ID=MA...
      export PLIVO_AUTH_TOKEN=$TOKEN
      plivo voice calls list

- The two-option login picker introduced earlier this cycle — replaced
  by browser-default with the env-var fallback above.
- `auth_id_hash` field from the feedback event body — identity now
  travels via the `X-Plivo-CLI-Auth-ID` header so the server has the raw
  value for use as PostHog `distinct_id` (Persons stitch with
  `cli.request` events instead of splitting on hashed-vs-raw).
- `plivo auth login` — replaced by the unified `plivo login` (no
  aliases; hard cut). Profile management subcommands (`plivo auth
  list / use / remove / whoami`) stay.
- Legacy `account compliance` (the older `/ComplianceDocument/`
  endpoint), superseded by `numbers compliance`.

### Security

- `plivo upgrade` now verifies the downloaded binary against the
  release's `SHA256SUMS` before replacing the running executable. The
  previous TLS + size check was not an integrity check; the upgrade now
  fetches `SHA256SUMS`, hashes the temp file, and aborts on mismatch
  before the atomic replace (adds reusable `release.VerifyChecksum` +
  `AssetByName`).
- `install.ps1` (Windows) now verifies the downloaded `.exe` against
  `SHA256SUMS` before moving it into place, mirroring `install.sh`. It
  downloads the binary + checksums to a temp dir, compares via
  `Get-FileHash`, and only installs on a match.

[Unreleased]: https://github.com/plivo/plivo-cli/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/plivo/plivo-cli/releases/tag/v0.2.0
