#!/usr/bin/env bash
#
# Place an outbound call that fetches PlivoXML from --answer-url when answered.
#
# Auth: export PLIVO_AUTH_ID / PLIVO_AUTH_TOKEN, or run `plivo login` first.
# Runs in --dry-run mode (prints the request, dials nothing). Remove --dry-run
# (and keep --yes) to actually place the call — it costs money.
set -euo pipefail

FROM="${FROM:-+14155550100}"
TO="${TO:-+14155550199}"
ANSWER_URL="${ANSWER_URL:-https://example.com/answer.xml}"

plivo voice calls make \
  --from "$FROM" \
  --to "$TO" \
  --answer-url "$ANSWER_URL" \
  --yes \
  --dry-run
