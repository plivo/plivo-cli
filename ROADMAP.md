# Roadmap

Pre-release. Status is captured here so users have visibility into what's coming. Dates are aspirational and may slip.

## Now — `beta` branch

The `beta` branch carries the full working CLI. Everything below is implemented and live there:

- Single static Go binary, dual TTY/JSON output, stable error envelope
- Three-segment grammar `plivo <service> <resource> <verb>`; pre-grammar short forms kept as aliases
- Credential profiles via `auth login`, with the auth token stored in the OS keychain (Keychain / libsecret / Credential Manager)
- `account` (get/update), `account subaccounts`, `account applications`
- `numbers` (list/get/search/buy/update/release), `numbers cnam`, `numbers masking sessions`, `numbers compliance` (regulatory requirements, applications, number linking)
- `voice calls` (make/list/get + hangup/transfer/play/speak/dtmf/record + stop verbs), `voice calls streams` (live audio bridge)
- `voice conferences` (+ members), `voice multiparty`, `voice recordings`, `voice endpoints`
- `message` (send/list/get), `message 10dlc` (brands/campaigns/links), `message powerpacks`, `message tollfree`
- `verify sessions`, `lookup`

## Coming soon

- **AI voice agents** (`plivo agent`) — build, publish, and run Vibe AI voice
  agents from the terminal. The public build ships a `plivo agent` placeholder
  that points here; the full surface (create / run / publish / attach / session)
  is gated behind the `internal` build tag while it still depends on
  Plivo-internal services. It graduates to the public build once those
  dependencies are externalized.

## Next

- Shell completion (`plivo completion bash|zsh|fish|powershell`)
- Update-check on startup with cached version-stamp
- Homebrew tap + Docker image distribution
- `--columns` / `--properties` for table output
- `command_not_found` suggestions

## Later

- Plugin-by-PATH mechanism (`plivo-<name>` binaries on `$PATH`)
- `plivo watch <resource>` live-tail
- Per-command `--subaccount` for impersonation
- `plivo api <resource>:<verb>` generic escape hatch via OpenAPI schema
- apt / yum / scoop distribution
- `--query` JMESPath filter for JSON output

## Cut

`main` branch promotion happens at v1.0.0 with:
- All "Now" features stable
- All "Next" items shipped
- Docs site published

Versioning follows [SemVer](https://semver.org). Until v1.0.0 the surface may change without deprecation cycles.
