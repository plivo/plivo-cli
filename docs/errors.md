# Error reference

Every CLI error — from missing creds to a Plivo API 500 — produces the same envelope. AI consumers parse the JSON; humans read the pretty form.

## Envelope schema

JSON (stderr when piped or `-o json`):

```json
{
  "error": {
    "code":        "AUTH_INVALID",
    "message":     "Credentials were rejected.",
    "hint":        "Run `plivo auth whoami` to verify, or `plivo login` to re-enter.",
    "retryable":   false,
    "status_code": 401,
    "request_id":  "abc123",
    "docs_url":    "...",
    "context":     { "endpoint": "/v1/Account/..." }
  }
}
```

Plain (TTY):

```
✗ Credentials were rejected.

  code:        AUTH_INVALID
  http:        401
  hint:        Run `plivo auth whoami` to verify, or `plivo login` to re-enter.
  request_id:  abc123
```

## Error codes

Stable strings — switch on these, not on the message text.

### Auth / session

| Code | Meaning | Hint shape |
|---|---|---|
| `AUTH_MISSING` | No Plivo creds configured | Run `plivo login` or set `PLIVO_AUTH_ID/TOKEN` |
| `AUTH_INVALID` | Upstream returned 401 | Re-check creds, run `plivo auth whoami` |
| `AUTH_FORBIDDEN` | Upstream returned 403 (role/scope) | Account lacks permission |
| `AUTH_EXPIRED` | Cached session token TTL elapsed | Re-login |
| `AUTH_2FA_REQUIRED` | Account has 2FA enabled | Disable for CLI account or use non-2FA path |
| `AUTH_RECAPTCHA_REQUIRED` | Endpoint requires browser reCAPTCHA | Use `plivo login --browser` (CLI-friendly path) |

### Input / state

| Code | Meaning | Hint shape |
|---|---|---|
| `VALIDATION_ERROR` | Server rejected the payload (400/422) | Inspect body; re-run with `--log-level debug` |
| `RESOURCE_NOT_FOUND` | 404 | Use the matching `... list` command first |
| `RESOURCE_CONFLICT` | 409 | Choose a unique name/id |
| `DESTRUCTIVE_REFUSED` | Cost or delete op without `--yes` | Pass `--yes` or `--dry-run` |
| `BAD_FLAG` | Flag value out of range / wrong shape | Run `--help` |
| `BAD_INPUT` | Generic CLI argument problem | Run `--help` |

### Plivo policy gates

| Code | Meaning | Hint shape |
|---|---|---|
| `GEO_PERMISSION_DENIED` | Destination country not enabled | Console → Messaging/Voice → Geo Permissions |
| `OUTBOUND_DISABLED` | Account-level outbound off | Contact Plivo support to enable |
| `INSUFFICIENT_FUNDS` | Cash balance too low | Top up the account |

### Transport / upstream

| Code | Retryable? | Hint shape |
|---|---|---|
| `RATE_LIMITED` (429) | ✓ | Back off; Plivo allows 300 req / 5 s |
| `UPSTREAM_TIMEOUT` (408/504) | ✓ | Pass `--timeout 60`; retry |
| `UPSTREAM_UNAVAILABLE` (502/503) | ✓ | Retry in a few seconds |
| `UPSTREAM_ERROR` (5xx generic) | ✓ | Retry; contact support with `request_id` |
| `NETWORK_ERROR` | ✓ | Check connectivity / VPN |

### Catch-alls

| Code | Meaning |
|---|---|
| `INTERNAL_ERROR` | Bug in the CLI itself — please file an issue |
| `USER_ERROR` | Anything we couldn't categorise |

## Exit codes

| Exit | Category |
|---|---|
| 0 | OK |
| 1 | User error (BAD_FLAG, BAD_INPUT, VALIDATION_ERROR, RESOURCE_NOT_FOUND, USER_ERROR, policy gates) |
| 2 | Auth (AUTH_MISSING / INVALID / FORBIDDEN / EXPIRED / 2FA / CONTACTO_NOT_LOGGED_IN) |
| 3 | Upstream / transport (NETWORK_ERROR, UPSTREAM_*, INTERNAL_ERROR) |
| 4 | RATE_LIMITED |
| 5 | DESTRUCTIVE_REFUSED |

## AI driver loop sketch

```bash
out=$(plivo agent list -o json 2>&1)
exit_code=$?
case "$exit_code" in
  0) echo "$out" | jq '.data[] | .name' ;;
  2) plivo login --browser && retry ;;
  4) sleep 6 && retry ;;
  *)
     code=$(echo "$out" | jq -r '.error.code')
     hint=$(echo "$out" | jq -r '.error.hint')
     echo "❌ $code — $hint"
     ;;
esac
```
