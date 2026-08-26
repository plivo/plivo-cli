#!/usr/bin/env bash
# Exercise a built plivo binary. No network: every check is a local render or
# a --dry-run print. Runs under bash on macOS, Linux and Git Bash on Windows.
#
# Usage: scripts/smoke.sh <path-to-plivo-binary>
set -euo pipefail

BIN="${1:-}"
if [ -z "$BIN" ]; then
  echo "usage: $0 <path-to-plivo-binary>" >&2
  exit 2
fi
if [ ! -x "$BIN" ]; then
  echo "✗ $BIN is not an executable file" >&2
  exit 2
fi

# Not a real-shaped auth_id, so secret scanners don't flag this file. Any
# non-empty value is enough for dry-run prints.
export PLIVO_AUTH_ID="${PLIVO_AUTH_ID:-CIFAKEPLACEHOLDER001}"
export PLIVO_AUTH_TOKEN="${PLIVO_AUTH_TOKEN:-ci-only-not-a-real-token}"

# Keep the run hermetic.
export PLIVO_NO_UPDATE_CHECK=1
export PLIVO_FEEDBACK_PROMPT=0

echo "== smoke against $BIN ($(uname -s)/$(uname -m)) =="

# ─── version + help ──────────────────────────────────────────────────────────
echo "--> version + help"
"$BIN" --version
"$BIN" --help > /dev/null

# ─── shell completion renders ────────────────────────────────────────────────
# Generated from the live command tree, so it catches broken registrations.
echo "--> completion"
for sh in bash zsh fish powershell; do
  "$BIN" completion "$sh" > /dev/null
  echo "✓ completion $sh"
done

# ─── per-group --help (no HTTP) ──────────────────────────────────────────────
# Every command group must render --help. `agent` is a coming-soon stub.
echo "--> per-group --help"
groups=(
  "account" "agent" "ask" "auth" "lookup" "messaging" "numbers" "support" "upgrade" "verify" "voice"
  "account subaccounts" "account applications"
  "numbers masking" "numbers masking sessions"
  "numbers compliance" "numbers compliance requirements"
  "voice calls" "voice calls streams" "voice conferences"
  "voice multiparty" "voice recordings" "voice endpoints"
  "messaging" "messaging sms" "messaging sms 10dlc" "messaging sms 10dlc brands"
  "messaging sms powerpacks" "messaging sms tollfree" "messaging whatsapp" "messaging mms"
  "verify sessions"
)
for cmd in "${groups[@]}"; do
  read -ra parts <<< "$cmd"
  "$BIN" "${parts[@]}" --help > /dev/null
  echo "✓ plivo $cmd --help"
done

# ─── dry-run URL construction ────────────────────────────────────────────────
# --dry-run prints the request without sending it, so this catches
# URL-construction regressions without needing a real account.
echo "--> dry-run URL construction"

# Request base, in one place so the assertions below read as path grammar.
BASE='https://hodor\.plivo\.com/v1/cli'

assert_url() {
  local label="$1" expected_pattern="$2"
  shift 2
  local out
  # `|| true`: report the pattern mismatch, don't abort under `set -e`.
  out=$("$@" 2>&1) || true
  if ! printf '%s\n' "$out" | grep -Eq "$expected_pattern"; then
    echo "✗ $label — expected pattern '$expected_pattern' in output:"
    echo "$out"
    exit 1
  fi
  echo "✓ $label"
}

assert_url "account get" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/" \
  "$BIN" account get --dry-run

assert_url "account subaccounts list" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/Subaccount/" \
  "$BIN" account subaccounts list --dry-run --limit 5

assert_url "numbers list" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/Number/" \
  "$BIN" numbers list --dry-run --limit 5

assert_url "account applications list" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/Application/" \
  "$BIN" account applications list --dry-run

assert_url "message send" \
  "POST ${BASE}/api/v1/Account/[A-Z0-9]+/Message/" \
  "$BIN" messaging sms send --src +14155551234 --dst +14155556789 --text hi --dry-run

assert_url "voice calls list" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/Call/" \
  "$BIN" voice calls list --dry-run

assert_url "voice recordings list" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/Recording/" \
  "$BIN" voice recordings list --dry-run --limit 3

assert_url "voice conferences list" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/Conference/" \
  "$BIN" voice conferences list --dry-run

assert_url "voice multiparty list" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/MultiPartyCall/" \
  "$BIN" voice multiparty list --dry-run --limit 3

assert_url "verify sessions list" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/Verify/Session/" \
  "$BIN" verify sessions list --dry-run --limit 3

assert_url "lookup (separate base host)" \
  "GET ${BASE}/api/v1/Lookup/Number/\+14155552671\?type=carrier" \
  "$BIN" lookup +14155552671 --dry-run

assert_url "message 10dlc brands list (10DLC path)" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/10dlc/Brand/" \
  "$BIN" messaging sms 10dlc brands list --dry-run --limit 3

assert_url "numbers masking sessions list (Masking/Session segments)" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/Masking/Session/" \
  "$BIN" numbers masking sessions list --dry-run --limit 3

assert_url "message powerpacks list" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/Powerpack/" \
  "$BIN" messaging sms powerpacks list --dry-run --limit 3

assert_url "voice endpoints list" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/Endpoint/" \
  "$BIN" voice endpoints list --dry-run --limit 3

assert_url "numbers compliance requirements" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/PhoneNumber/Compliance/Requirements/" \
  "$BIN" numbers compliance requirements --country US --number-type local --user-type business --dry-run

assert_url "numbers compliance list" \
  "GET ${BASE}/api/v1/Account/[A-Z0-9]+/PhoneNumber/Compliance/" \
  "$BIN" numbers compliance list --dry-run

assert_url "numbers compliance create (multipart upload)" \
  "POST ${BASE}/api/v1/Account/[A-Z0-9]+/PhoneNumber/Compliance/" \
  "$BIN" numbers compliance create --data '{"country_iso":"US"}' --dry-run

assert_url "ask (SSE)" \
  "POST ${BASE}/cx/v1/aiassist/buddy-ext/chat" \
  "$BIN" ask "ping" --dry-run

assert_url "support (past escalations)" \
  "GET ${BASE}/cx/v1/aiassist/buddy-ext/escalations" \
  "$BIN" support --dry-run

# ─── destructive verbs refuse without --yes ──────────────────────────────────
# Destructive verbs must exit 5 with DESTRUCTIVE_REFUSED without --yes.
echo "--> destructive refusal"

assert_refused() {
  local label="$1"
  shift
  local out code
  # Capture, don't pipe: the expected exit 5 would trip `pipefail`.
  out=$("$@" --dry-run 2>&1) && code=0 || code=$?
  if [ "$code" -ne 5 ]; then
    echo "✗ $label — expected exit 5 (ExitRefused), got $code:"
    echo "$out"
    exit 1
  fi
  if ! printf '%s\n' "$out" | grep -q "DESTRUCTIVE_REFUSED"; then
    echo "✗ $label exited 5 but without a DESTRUCTIVE_REFUSED code:"
    echo "$out"
    exit 1
  fi
  echo "✓ $label refused without --yes (exit 5)"
}

assert_refused "numbers release"         "$BIN" numbers release +14155551234
assert_refused "voice calls hangup"      "$BIN" voice calls hangup CALL-FAKE
assert_refused "voice recordings delete" "$BIN" voice recordings delete REC-FAKE

echo
echo "✓ smoke passed on $(uname -s)/$(uname -m)"
