# Changelog

All notable changes to the Plivo CLI are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- `sip * delete` ran its dependency check only when refusing. With `--yes` there
  was no pre-flight read at all, so a delete that detached other objects printed
  nothing. Deleting an in-use URI **cascade-deletes the trunks pointing at it**,
  which is exactly the case the check exists to surface. The read now always
  runs; `--yes` skips the confirmation only.
- `sip trunks update` 400'd on every flag except `--status` and `--secure`: the
  API requires `trunk_direction` on each update and the CLI never sent it. It is
  now read from the trunk, with `--direction` as an override.
- `sip calls diagnose` exited 0 when the assistant failed to analyse the call,
  so a script could not tell success from failure. It now exits non-zero when
  the turn ends in an escalation rather than an answer.
- `diagnose` told terminal users to reload the Plivo Console, and filed support
  tickets on its own initiative. Both are now ruled out in the request.
- `-o json` was ignored by every `update` and `delete`: stdout was empty, prose
  went to stderr and the exit code was 0, which a jq pipeline reads as success
  with no data.
- `sip trunks create -o json` omitted `trunk_domain`, the one value a customer
  pastes into their platform. Table mode read it back; JSON did not.
- `sip credentials update` presented `--password-stdin` as optional, but the API
  rewrites the password on every update, so an update without one blanks it.
  The flag is now required.
- `sip uris create` took `--password` on the command line, where it lands in
  shell history, `ps` output and CI logs. Passwords are stdin-only, matching
  credentials, and a URI password can now be rotated on update.
- `numbers update --trunk-id` skipped the outbound-trunk check under `--dry-run`,
  so the preview showed a request the real run refuses. Pre-flight reads now run
  under `--dry-run`; it suppresses writes, and a GET is not a write.

## [1.1.0] - 2026-09-18

### Added

- `plivo sip` — SIP Trunking was the only Plivo product with no CLI surface at
  all. Typed CRUD over trunks, origination URIs, credentials and IP access
  control lists, plus reading trunk CDRs and `sip calls diagnose`.
- `numbers update --trunk-id` routes a number to an inbound trunk. The API takes
  a trunk in `app_id`, so until now you had to know a trunk goes in a flag named
  after applications. An outbound trunk is refused: a number attached to one
  quietly stops answering.
- Credential passwords are read from stdin only. There is no `--password` flag,
  because an argument lands in shell history, `ps` output and CI logs.
- Deleting a URI, credential or IP ACL first names every trunk pointing at it,
  and deleting a trunk reports how many numbers it would detach.
- **Known limitation:** `sip calls diagnose` needs server-side support that is
  not live yet, so it currently reports that the call could not be retrieved.
  Everything else under `plivo sip` works today; use `sip calls get` for the
  hangup cause, durations and SIP details in the meantime.
- `sip calls list` filters cover exactly what the API accepts, so a flag that
  would 400 upstream does not exist. `--limit` is bounded and `--since`/`--until`
  are parsed locally, and `--until` widens a bare date to the end of that day so
  the day you name is included.

### Fixed

- The bundled CLI skill said `plivo agents` was "coming soon; no subcommands
  yet". It has a full surface — list, get, create, update, publish, pause,
  resume, delete, plus the node catalogue and run history — so an assistant
  reading the skill would decline to use commands that work. Documented.
- The bundled CLI skill claimed `plivo sms send` is equivalent to
  `plivo messaging sms send`. The alias replaces the group name only, so
  `plivo sms send` is not a command and exits 1.
- The bundled CLI skill pinned itself to "plivo-cli v0.3.0", four releases
  behind. It now points at `plivo skill list`, which reports drift directly,
  instead of naming a version that goes stale every release.
- The README and `docs/errors.md` still used the pre-v0.3.0 `.data[]` shape in
  jq examples, which is a hard jq error against the current envelope, not a
  near miss. The regression guard for this existed but only ever read the
  skill file; it now covers every doc in the repo, including the multi-line
  capture-then-filter form it previously could not see.
- Dead documentation links: the cosign install page, and the `<Conference>`
  XML page referenced twice by the Voice XML skill.
- The CLI skill's top-level command map still listed `agent` as "coming soon"
  and omitted `config`, `docs`, and `skill` — including `plivo skill`, which
  is how the skill is installed. The map is now checked against the real
  command tree, and the repo-wide doc check reads git's file list rather than
  walking the filesystem, which reported a stale nested checkout as a defect.



- Cancelling sign-in in the browser now ends `plivo login` immediately. The
  Cancel button only closed the browser tab, so the CLI kept listening and
  failed five minutes later with a timeout telling the user to go and approve
  the thing they had just refused. An `error` in the loopback callback is now
  handled; previously it fell through to "missing code in callback URL".

### Security

- `voice streams forward` now validates Plivo's V3 signature on `/answer` and
  on the `/ws` upgrade (SA-01). Both were reachable by anyone who learned the
  tunnel URL, and after any successful upgrade the CLI dialled `--to` and
  forwarded frames both ways, so an unauthenticated caller could drive the
  local handler and read its replies. Origin checking does not help: a
  non-browser caller simply omits `Origin`.
- Verification uses the public tunnel URL rather than the request's `Host`,
  which is the local listener once the request has come through the tunnel.
- `--insecure-skip-signature` restores the old behaviour deliberately.
- The localhost.run SSH fallback now verifies the tunnel host (SA-02). It ran
  with `StrictHostKeyChecking=no` and `UserKnownHostsFile=/dev/null`, so it
  accepted any server without authenticating it and discarded the user's
  stored trust. The justification in the code reasoned about confidentiality
  ("nothing secret in the tunnel") and missed integrity: the server's output
  supplies the URL the CLI writes into the application's `answer_url`, so an
  impersonator redirects live call handling.
- Uses `accept-new` against a dedicated `~/.plivo/known_hosts_tunnel`: an
  unknown host is recorded once, a changed key is refused. An attacker now has
  to be present at the first connection rather than at any connection.
- This narrows SA-02 rather than closing it. localhost.run publishes no
  fingerprint to pin, and re-reading the key on each run would just re-learn it
  from the party being authenticated. First use prints that the provider's
  identity cannot be checked and points at ngrok, whose client authenticates
  its own service.
- Credentials no longer appear in `--dry-run` or `--log-level debug` output
  (SA-05). Both printed the request body verbatim, so
  `voice endpoints create --password ...` put the SIP password in the
  terminal, and from there into terminal recordings, CI logs, support
  attachments and agent transcripts. A shared recursive redactor now covers
  every path that prints a body, at any nesting depth, for JSON and
  urlencoded forms.
- Feedback redaction no longer depends on where a token's digits fall
  (SA-06). The pattern required 30-80 characters *after* a prefix proving both
  character classes were present, so a 40-character token whose only digit sat
  near the end needed 60+ characters to match and reached the collector
  intact. Length and character classes are now checked independently.
- ngrok tunnel discovery is now bound to the tunnel we actually started
  (SA-04). It returned the first HTTPS tunnel advertised on
  127.0.0.1:4040, but that port belongs to whichever ngrok started first, so
  an unrelated instance could hand us its URL, which is then written into the
  Plivo application's `answer_url` and routes the account's calls to a tunnel
  we do not own.
- The tunnel must now forward to the port we requested, and polling aborts if
  our own ngrok exits rather than waiting out the timeout against somebody
  else's.
- Terminal control sequences in API-provided text are now neutralised before
  they reach human output (SA-07). A backend storing hostile text in an agent
  name, alias or caller ID could repaint the terminal, hide or fake output, or
  drive sequences some terminals act on. Applies to tables, key-value output
  and the plain error renderer. Printable text, including every non-ASCII
  script, is untouched; only C0 controls and DEL are escaped, and tab and
  newline are kept because the renderers use them for layout.
- Saving credentials now tightens permissions that already exist (SA-09).
  `MkdirAll` and `OpenFile` only apply their mode when they create, so a
  `~/.plivo` left at 0755 or a `config.toml` left at 0644 kept those modes and
  the auth token was written into a file other local users could read.
- The config is now written to a fresh 0600 temp file and renamed into place.
  A new file cannot inherit a permissive mode, and the replace is atomic, so
  an interrupted save can no longer leave a half-written config holding a
  partial token.
- Release signature verification no longer fails open (SA-03). Every failure
  path returned a nil error, so a signature that could not be downloaded, or
  assets that were simply absent, meant "install anyway". A checksum proves the
  binary matches its manifest, not who published either, so an attacker able to
  serve both only had to break the signature fetch to remove the signer check.
- Releases from v0.3.0 onward must now carry a verifiable signature. That
  boundary was described in comments but never enforced, so a brand-new release
  with its signature assets removed verified as "skipped" and installed.
  Genuinely older releases still install on the checksum alone.
- Missing assets, download failures and staging errors are fatal on a release
  that must be signed, in `plivo upgrade`, `install.sh` and `install.ps1`.
  `PLIVO_ALLOW_UNSIGNED=1` overrides deliberately.
- cosign not being installed stays a warning rather than an error. An attacker
  cannot uninstall the user's cosign, so it is not a path they control, and
  blocking upgrades over a tool the user never installed would cost more than
  it buys.
- Go toolchain baseline moved from 1.26.3 to 1.26.8 (SA-08). Every workflow
  pins its toolchain with `go-version-file: go.mod`, so the stale `go`
  directive was the build baseline, and the audit found symbol-level paths to
  eight standard-library advisories from it.
- CI now runs `govulncheck` over both the public and internal builds, so the
  baseline cannot drift unnoticed again. Nothing was watching it before.

## [1.0.1] - 2026-09-08

### Added

- Three product skills are now bundled in the binary, alongside the CLI
  skill: `plivo skill install audio-streaming | sip-trunking | voice-xml`.
  They cover connecting a WebSocket voice bot with `<Stream>`, connecting an
  AI voice platform over SIP trunking, and writing Voice XML. Embedded, so
  they install with no network.
- `plivo skill list` shows every bundled skill, where it installs, and
  whether the copy on disk still matches this binary. A skill written by an
  older binary reports `installed (differs from bundled)`; re-running
  `skill install` refreshes it.
- `install.sh` accepts `--version` and `--dir`, which work through a pipe:
  `curl -fsSL … | bash -s -- --version v1.0.1`. `PLIVO_CLI_VERSION` could
  not be used as documented — in `PLIVO_CLI_VERSION=x curl … | bash` the
  variable is set on curl, not on the bash running the script, so it was
  silently ignored and you got the latest release instead.

### Changed

- The CX agents skill is no longer offered. Its files stay in the repo but
  nothing imports them, so the content is not in the binary and
  `skill install all` does not write it.

### Fixed

- A 401 that is not about your credentials no longer tells you to log in
  again. When the server cannot resolve an account's region, re-running
  `plivo login` cannot help, and the old hint sent people through repeated
  logouts and reinstalls while their credential was valid the whole time.
- `plivo --help` listed credential precedence with the active profile above
  the environment variables. Environment variables have won since v0.3.0;
  the text had been wrong for three releases.
- The `plivo api` examples included `GET /Account/`, which expands to
  `/v1/Account/<auth_id>/Account/` and 404s. Published documentation had
  copied that example from this help text.
- `plivo auth token` (internal builds) pointed at `plivo contacto login`, a
  command that exists in no build, for a session nothing can create.
- The permission-denied path in `install.sh` suggested the same
  `VAR=… curl | bash` form that does not work.

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

[Unreleased]: https://github.com/plivo/plivo-cli/compare/v1.0.1...HEAD
[1.0.1]: https://github.com/plivo/plivo-cli/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/plivo/plivo-cli/compare/v0.4.1...v1.0.0
[0.4.1]: https://github.com/plivo/plivo-cli/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/plivo/plivo-cli/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/plivo/plivo-cli/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/plivo/plivo-cli/releases/tag/v0.2.0
