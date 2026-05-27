# Changelog

All notable changes to the Plivo CLI are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Three-segment command grammar: `plivo <service> <resource> <verb>` (e.g.
  `plivo voice calls list`, `plivo sms messages send`, `plivo numbers search`).
- Cross-platform installers for macOS, Linux, and Windows (`install.sh`,
  `install.ps1`), with architecture auto-detection.
- `plivo agent` ships as a coming-soon stub in the public build.

### Changed
- Commands are now grouped under service namespaces (`voice`, `sms`, `numbers`,
  `verify`, `account`). The pre-grammar short forms (`plivo call list`,
  `plivo msg send`, …) continue to work as aliases.

[Unreleased]: https://github.com/plivo/plivo-cli/commits/main
