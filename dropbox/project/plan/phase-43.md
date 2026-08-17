# Phase 43 — `DROPBOX_REFRESH_TOKEN` moves to channel 5 (`state/`): client and declaration

*Realizes design Decision 33 (rotating-credential adoption), realizing the
umbrella's R-ENXH-KJ01, R-EP5D-YAQQ (`root project/design/D32.md`,
`[proof: per-service]`).*

Move dropbox's refresh token off the app-config parameter and into `state/`
(D33), leaving the two static OAuth credentials unchanged:

- **Declaration.** `dropbox/etc/env.list`: `secret DROPBOX_REFRESH_TOKEN`
  becomes `rotating DROPBOX_REFRESH_TOKEN`; `DROPBOX_APP_KEY` /
  `DROPBOX_APP_SECRET` stay `secret`.
- **Client read.** At the `cmd/dropbox/main.go` composition root the refresh
  token is read from `${IKIGENBA_ROOT}/dropbox/state/DROPBOX_REFRESH_TOKEN`
  (composed like the SQLite path), failing loudly and naming the credential when
  the file is absent; app-key/app-secret still come from the environment.
- **Rotation.** The OAuth token source writes a provider-rotated refresh token
  back to that same file atomically (`0600`), so the next boot reads it.
- **Live test.** `internal/dropbox/client_live_test.go` reads the refresh token
  from the `state/` file (seeded by `opsctl seed-state`) rather than the
  environment; app-key/app-secret still from the environment.
- **Prose.** Purge the retired `.envrc` reference from
  `internal/dropbox/client.go` and `AGENTS.md`.

**Done when** (from `dropbox/`, the loop's working directory):
- `go test ./...` exits 0.
- **R-ENXH-KJ01** covered by a tagged hermetic test: over a temp-dir
  `IKIGENBA_ROOT`, a present `state/DROPBOX_REFRESH_TOKEN` resolves to its bytes,
  and an absent file yields a loud error naming the credential (no silent start).
- **R-EP5D-YAQQ** covered by a tagged hermetic test: a token source that returns
  a rotated refresh token causes the `state/` file to be rewritten, and a re-read
  returns the rotated value.
- `grep -rniE '\.envrc' cmd internal AGENTS.md` prints nothing — the retired
  `.envrc` references are gone (a `project/`-excluded grep).
