# Changelog

All notable changes to the Plivo CLI are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `plivo login` + `plivo logout` — top-level unified login. Phase 1 ships
  the `--auth-id` flow with hidden interactive token prompt or
  `--auth-token-stdin` for CI/scripts. Validates credentials with
  `GET /Account/` before saving. `--email` and `--browser` will follow
  in subsequent releases (Phase 2 / Phase 3).
- `plivo ask "<query>"` + `plivo support` — talks to Plivo's customer-facing
  AI assistant via `/v1/aiassist/buddy-ext` (SSE streaming, Plivo Basic auth).
  `--call-uuid` adds voice-debug context. Ctrl-C cancels cleanly; `-o json`
  emits JSONL events for scripts and AI agents. `plivo support` lists past
  escalations.
- Three-segment command grammar: `plivo <service> <resource> <verb>` (e.g.
  `plivo voice calls list`, `plivo message send`, `plivo numbers search`).
- Cross-platform installers for macOS, Linux, and Windows (`install.sh`,
  `install.ps1`), with architecture auto-detection.
- `plivo agent` ships as a coming-soon stub in the public build.
- `numbers compliance` — the unified number-compliance API: requirements,
  application create/get/list/update/delete (with document uploads), and bulk
  number linking.

### Changed
- Commands are now grouped under service namespaces (`voice`, `message`,
  `numbers`, `verify`, `account`). The pre-grammar short forms (`plivo call
  list`, `plivo msg send`, …) continue to work as aliases.
- Messaging uses a two-segment form: `plivo message send` (was `plivo sms
  messages send`); message protocol is the `--type sms|mms|whatsapp` flag.
  `sms` and `msg` remain aliases of `message`.

### Removed
- `plivo auth login` and the internal-build `plivo contacto login` — both
  replaced by the unified `plivo login` (no aliases; hard cut). Profile
  management subcommands (`plivo auth list / use / remove / whoami`) stay.
- Legacy `account compliance` (the older `/ComplianceDocument/` endpoint),
  superseded by `numbers compliance`.

[Unreleased]: https://github.com/plivo/plivo-cli/commits/main
