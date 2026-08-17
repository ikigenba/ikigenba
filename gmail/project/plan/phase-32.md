# Phase 32 — `GMAIL_REFRESH_TOKEN` moves to channel 5 (`state/`): client, consent, and declaration

*Realizes design Decision 28 (rotating-credential adoption), realizing the
umbrella's R-ENXH-KJ01, R-EP5D-YAQQ (`root project/design/D32.md`,
`[proof: per-service]`) and gmail's own R-S3ZK-NBWM.*

Move gmail's refresh token off the app-config parameter and into `state/`
(D28), leaving the two static OAuth credentials unchanged:

- **Declaration.** `gmail/etc/env.list`: `secret GMAIL_REFRESH_TOKEN` becomes
  `rotating GMAIL_REFRESH_TOKEN`; `GMAIL_CLIENT_ID` / `GMAIL_CLIENT_SECRET` stay
  `secret`.
- **Client read.** At the `cmd/gmail/main.go` composition root the refresh token
  is read from `${IKIGENBA_ROOT}/gmail/state/GMAIL_REFRESH_TOKEN` (composed like
  the SQLite path), failing loudly and naming the credential when the file is
  absent; client-id/secret still come from the environment.
- **Rotation.** The OAuth token source writes a provider-rotated refresh token
  back to that same file atomically (`0600`), so the next boot reads it.
- **Consent.** `cmd/consent` writes the consented token to the composed local
  `state/` path (`0600`), and its `~/.secrets/GMAIL_REFRESH_TOKEN` writer is
  removed.
- **Prose.** Purge the retired `.envrc` / `~/.secrets` references from gmail's Go
  source and `AGENTS.md` (`internal/gmail/client.go` comment, `cmd/consent`,
  `AGENTS.md`).

**Done when** (from `gmail/`, the loop's working directory):
- `go test ./...` exits 0.
- **R-ENXH-KJ01** covered by a tagged hermetic test: over a temp-dir
  `IKIGENBA_ROOT`, a present `state/GMAIL_REFRESH_TOKEN` resolves to its bytes,
  and an absent file yields a loud error naming the credential (no silent start).
- **R-EP5D-YAQQ** covered by a tagged hermetic test: a token source that returns
  a rotated refresh token causes the `state/` file to be rewritten, and a re-read
  returns the rotated value.
- **R-S3ZK-NBWM** covered by a tagged hermetic test: `cmd/consent` writes the
  consented token to `${IKIGENBA_ROOT}/gmail/state/GMAIL_REFRESH_TOKEN` as a
  `0600` file, not under `~/.secrets`.
- `grep -rniE '\.envrc|\.secrets' cmd internal AGENTS.md` prints nothing — the
  retired-mechanism references are gone (a `project/`-excluded grep).
