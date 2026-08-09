# Phase 39 — Caller-requested, capped TTL on run-token minting

*Realizes design Decision 20 (run tokens) — the `ttl` request field only.*

`POST /run-token` accepts an optional `ttl` field: a Go duration string naming
the lifetime the calling service wants for the token. The handler parses it,
caps it at `REPOS_RUN_TOKEN_TTL`, and mints with the effective value; the
response's `expires_at` reflects what was actually granted. An absent field
preserves today's env-default behavior byte-for-byte; a malformed, zero, or
negative duration is a 400 that mints nothing. No schema change: the stored row
already carries `expires_at`.

**Done when:**

- R-II0Q-VTSI — a requested `35m` yields `expires_at` = now + 35m, and a
  request above `REPOS_RUN_TOKEN_TTL` yields `expires_at` = now + the env cap —
  covered by a tagged test.
- R-IJ8N-9LJ7 — `"ttl":"banana"` and `"ttl":"-5m"` each 400 with no
  `run_tokens` row inserted; the field omitted still mints with the env-default
  expiry — covered by a tagged test.
- The suite is green per design Conventions (`go build ./...`, `go vet ./...`,
  `gofmt -l .` silent, `go test ./...` from `repos/`).
