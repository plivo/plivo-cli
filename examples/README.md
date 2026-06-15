# Examples

Runnable scripts for common Plivo CLI tasks. Each uses the
`plivo <service> <resource> <verb>` grammar.

Authenticate first — either:

```bash
export PLIVO_AUTH_ID=MAxxxxxxxxxxxxxxxxxxxx
export PLIVO_AUTH_TOKEN=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
# or: plivo login
```

Then run a script (override the defaults with env vars):

```bash
./send-sms.sh                       # uses the placeholder src/dst
DST=+14155551234 ./send-sms.sh      # override a value
./make-call.sh
./account-overview.sh
```

| Script | What it does | Cost |
|---|---|---|
| `send-sms.sh` | Send an SMS (runs in `--dry-run`) | free as written |
| `make-call.sh` | Place a call to an answer URL (runs in `--dry-run`) | free as written |
| `account-overview.sh` | Print account + numbers as JSON | free (read-only) |

The spend scripts default to `--dry-run`, so they print the request without
sending. Remove `--dry-run` to execute for real.
