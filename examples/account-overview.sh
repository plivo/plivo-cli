#!/usr/bin/env bash
#
# Print the active account and the first page of rented numbers, as JSON —
# handy for piping into jq or feeding an automation/AI agent.
#
# Auth: export PLIVO_AUTH_ID / PLIVO_AUTH_TOKEN, or run `plivo auth login` first.
# These are read-only operations (no cost, no --yes needed).
set -euo pipefail

echo "# account"
plivo account get -o json

echo "# numbers (first 5)"
plivo numbers list --limit 5 -o json
