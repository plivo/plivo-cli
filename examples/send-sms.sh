#!/usr/bin/env bash
#
# Send an SMS with the Plivo CLI.
#
# Auth: export PLIVO_AUTH_ID / PLIVO_AUTH_TOKEN, or run `plivo auth login` first.
# This script runs in --dry-run mode so it prints the request without sending.
# Remove --dry-run (and keep --yes) to actually send the message — it costs money.
set -euo pipefail

SRC="${SRC:-+14155550100}"
DST="${DST:-+14155550199}"
TEXT="${TEXT:-Hello from the Plivo CLI}"

plivo sms messages send \
  --src "$SRC" \
  --dst "$DST" \
  --text "$TEXT" \
  --yes \
  --dry-run
