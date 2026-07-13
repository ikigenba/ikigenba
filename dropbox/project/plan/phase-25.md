# Phase 25 — Loopback mutation error contract: 404 for not-found, detail bodies

*Realizes design Decision 16 (slice: the error contract — R-BZLK-E9VW,
R-C0TG-S1ML; the phase-18 ids stay owned by phase 18 and must remain green).
Depends on phase 18 (the write Service methods + loopback routes).*

Observable end state:

- The loopback mutation routes (`PUT`/`DELETE /content`, `POST /mkdir`,
  `POST /move`) map the domain error onto the HTTP status:
  `ErrValidation`/`ErrPathEscape` → **400**, `ErrNotFound` → **404**, anything
  else → **500**. The fixed `"validation error"` body is gone: every 4xx body
  is a single plain-text line carrying the underlying domain error text.
- `Service.Move` reports a missing `from` as `ErrNotFound` (the mirror's
  rename ENOENT is mapped to the domain sentinel before it reaches the route),
  so a stale move source is a 404, not a 400 or 500.
- `DELETE /content` of an absent path is still success (idempotent contract
  unchanged); the read routes (`GET /content`, `GET /stat`) are unchanged.
- The `dropbox/docs/` filesystem-API reference states the refined error
  contract (statuses + detail bodies) where it documents the mutation routes.

**Done when:** the suite is green (design Conventions commands, from
`dropbox/`) and:

- R-BZLK-E9VW is covered by a test driving `POST /move` through the shipped
  handler wiring with a `from` absent from the mirror: the response is **404**
  with a body naming the missing path, and afterward the index is unchanged,
  the recording event sink captured nothing, and `upload_queue` holds no row —
  while `DELETE /content` of an absent path still returns success in the same
  suite.
- R-C0TG-S1ML is covered by a test driving a mutation with an escaping path
  and one with an empty path: each response is **400** and its body contains
  the specific domain detail (the escape message / `path is required`), not
  the bare string `validation error`.
