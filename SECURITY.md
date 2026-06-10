# Security policy

## Reporting a vulnerability

If you discover a security vulnerability in the plivo CLI, please report it privately so we can investigate and remediate before the issue is public.

**Email:** security@plivo.com

Include:
- A description of the vulnerability
- Steps to reproduce
- Affected version (`plivo --version`)
- Your environment (OS, architecture)
- Any proof-of-concept or evidence

We aim to acknowledge reports within two business days and ship a fix or workaround within 30 days for high-severity issues. We do not currently run a bug-bounty program.

## What is in scope

- The `plivo` binary in this repository
- Build / release artifacts published from this repository
- Code that handles credentials (`internal/config`, `internal/clierr`, `cmd/auth*`)

## What is out of scope

- Vulnerabilities in the Plivo REST API itself — report those via Plivo's product security channel
- Issues in third-party dependencies that are already publicly disclosed (please report to the upstream maintainer)
- Issues that require physical access to the user's machine or already-compromised credentials

## Disclosure

Please do not file public GitHub issues for security vulnerabilities. We will publicly disclose the issue after a fix is shipped and users have had reasonable time to upgrade.
