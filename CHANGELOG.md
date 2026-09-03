# Changelog

All notable changes to the Plivo CLI are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-09-03

First stable release. The command grammar, JSON envelope, exit codes and
config layout are now considered stable; breaking changes to them will come
with a major version bump.

### Added

- `plivo agents` — manage AI agent flows against the public Agents API:
  `create`, `get`, `list`, `update`, `delete`, the `publish`/`pause`/`resume`
  lifecycle verbs, plus `agents runs` for executions and `agents nodes` for
  the node catalogue. `--all` auto-paginates `agents list` and
  `agents runs list`, and is registered only on the commands that implement
  it rather than globally.
- Multi-organization login. `plivo login` now names the saved profile after
  the organization instead of always `default`, so authorizing a second
  organization no longer overwrites the first. `-n/--name` still overrides,
  re-authorizing the same organization updates it in place, and a different
  organization gets its own profile rather than clobbering one. `plivo auth
  list` shows the organization per profile.
- `plivo ask` sends a client-minted conversation id, so the turns of one
  conversation group together instead of appearing unrelated.

### Fixed

- `plivo ask` no longer treats a handful of event names as stream
  terminators that the server does not send; they are retained as
  forward-compatible no-ops and documented as such.

## [0.4.1] - 2026-09-02

### Fixed

- The agent skill file taught the retired JSON shape. Three of its own
  examples still used `.data[]` for list commands, which v0.3.0 moved to
  `.data.objects[]` — the same file that documents the change. An agent
  installing the skill and copying an example got
  `Cannot index string with string "number"`. A test now runs the file's
  examples, so this cannot ship again.
- `--explain` was a global flag that only 7 commands implemented, so on the
  other 165 it was silently ignored — the same defect that got `--all`
  removed in v0.3.0. It is now registered only on the commands that support
  it (`api`, `applications create`, `auth whoami`, `calls make`,
  `messaging send`, `numbers buy`, `numbers release`); elsewhere it returns
  `unknown flag: --explain` instead of pretending.

### Added

- The skill file now points at the three product skills
  (`plivo-audio-streaming`, `plivo-sip-trunking`, `plivo-voice-xml`), which
  an agent installing the CLI skill previously had no way to discover.

## [0.4.0] - 2026-08-31

### Added

- `voice streams forward` no longer needs ngrok. It defaults to
  **localhost.run** over ssh — no install, no account, nothing to sign up for —
  and uses ngrok instead when it is already on PATH. `--tunnel auto | ngrok |
  localhost.run` forces a choice.
- Release provenance. `SHA256SUMS` is now signed with cosign keyless, and
  `install.sh`, `install.ps1` and `plivo upgrade` all verify that signature when
  `cosign` is available — pinning the signer identity and OIDC issuer, without
  which any Sigstore identity would produce a passing check. Unsigned releases
  and machines without cosign still install; a signature that is present and
  fails is fatal.

- `plivo docs` — read the documentation from the terminal. `docs search
  <keywords>` full-text searches every page (a page must contain all the
  keywords, ranked by frequency), `docs list` shows the index, and
  `docs show <path-or-title>` prints one page. Backed by the docs site's own
  `llms.txt` / `llms-full.txt` exports, so it needs **no credentials** and works
  in a bare container. The full text is cached under `~/.plivo/cache` for a day;
  `--refresh` re-fetches, and a stale cache is served if the network is down.

### Fixed

- **`voice streams` emitted the wrong audio contract.** The `<Stream>` XML
  carried `contentType` and `sampleRate` as two attributes; the rate belongs
  inside `contentType` (`audio/x-mulaw;rate=8000`) and there is no `sampleRate`
  attribute. The l16 MIME type was also wrong — `audio/x-l16`, not `audio/l16`.
  Separately, `streams test --codec l16` announced 16-bit PCM but generated
  mu-law bytes at half the expected frame size, so the pre-flight passed while
  the endpoint received noise. Both spellings and the audio generator now come
  from one place, and an unsupported codec/rate pair is rejected up front —
  there is no mu-law 16kHz stream.
- **An unknown subcommand exited 0.** `plivo voice streams bogustypo` printed
  help and reported success; the same hole existed on 35 command groups. Cobra
  only rejects an unrecognized subcommand for the true root, so every parent
  command that hosts only subcommands silently short-circuited to help. A bare
  group invocation still prints help and exits 0.
- `-o json` is now honoured by `voice streams test`, `voice streams forward`
  and `upgrade`, which previously always printed prose. Each emits a single
  machine-readable summary of the run, and progress output is suppressed so
  stdout stays parseable.
- `make sign-release` failed outright against cosign 3.x, which defaults to a
  bundle format requiring `--bundle`. The signing path had never been executed
  end to end.

## [0.3.0] - 2026-08-28

### Changed

- **BREAKING: `-o json` now returns the API response as-is.** Previously the
  typed structs were re-marshalled, which silently dropped every field the CLI
  did not have a tag for — a `/Number/` row has 32 fields and only 16 survived.
  For list commands `data` therefore changes from an array to the full response
  object:

  ```
  before   {"data": [ {...} ], "meta": {...}}
  after    {"data": {"api_id": "...", "meta": {...}, "objects": [ {...} ]}}
  ```

  Scripts reading `data[0]` need `data.objects[0]`. Single-resource commands
  keep the same shape and simply gain the missing fields. Table output is
  unchanged.
- **BREAKING: `--all` removed.** It was accepted on every command and did
  nothing, while being documented as "auto-paginate through all pages". Real
  pagination will come back as its own change rather than as a flag that lies.
- **Credentials: `PLIVO_AUTH_ID` / `PLIVO_AUTH_TOKEN` now beat a stored
  profile.** Previously the active profile won, so exported credentials were
  silently ignored whenever any profile existed — including one holding a stale
  token. An explicit `--profile` still wins over the environment. This matches
  the aws, stripe and twilio CLIs.
- `diagnose` now fails fast with `RESOURCE_NOT_FOUND` when the call or message
  id is not on the account, instead of handing an unknown id to the assistant
  (which could not tell "does not exist" from "lookup failed", and raised a
  support ticket either way).

### Added

- `plivo config telemetry on|off|status` (plus generic `plivo config
  get/set`) — turn off the identity headers (email, auth ID, region, AOM
  UUID) sent on CLI requests, persisted in `~/.plivo/config.toml`.
  `PLIVO_CLI_TELEMETRY=0` does the same for a single shell session or CI
  job, and wins over the config file. Version/OS/arch metadata is
  unaffected — the server needs it for the upgrade nudge.
- `--media-url` on `messaging mms send`, repeatable, for attaching media.
- `plivo ask` shows a working spinner so long runs do not look frozen.

### Fixed

- `voice recordings list` crashed on every call: `recording_duration_ms` is a
  decimal string upstream but was typed as an integer, so the response never
  decoded. The command had never worked against the live API.
- `messaging sms tollfree list/get/create` hit `TollFreeVerification`, but the
  API serves `TollfreeVerification`. Every call 404'd.
- `voice streams forward` now discloses its blast radius before you confirm: it
  rewrites the application's answer URL, so every number on that app forwards,
  not just `--number`. Its `--dry-run` also printed a blank preview.
- `plivo support` refuses with a clear message when the credentials carry no
  user identity, instead of sending an unscoped request.
- `ask -i` no longer refuses when stdout is piped.
- `--dst` help text rendered as `--dst <` because of a stray backtick.
- A rejected-credentials error blamed `PLIVO_AUTH_ID` / `PLIVO_AUTH_TOKEN` even
  when a stored profile supplied them. It now names the source actually used.

### Internal

- CI builds and runs the binary on Ubuntu, macOS and Windows, and exercises
  `install.sh` / `install.ps1` on each. Previously only the Linux build was
  ever executed, so the Windows and macOS artefacts shipped unrun.

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

[Unreleased]: https://github.com/plivo/plivo-cli/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/plivo/plivo-cli/compare/v0.4.1...v1.0.0
[0.4.1]: https://github.com/plivo/plivo-cli/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/plivo/plivo-cli/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/plivo/plivo-cli/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/plivo/plivo-cli/releases/tag/v0.2.0
