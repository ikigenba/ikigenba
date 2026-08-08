# Phase 35 — The version-plane client (`internal/repos`) behind the domain seam

*Realizes design Decision 36 (version-plane client). Depends on Phase 34.*

A new package `internal/repos` holds `Client` — the loopback client for repos'
verb surface (create, commit, head, read-file, rename, delete, run-token) — and
`internal/script/version.go` declares the `VersionPlane` interface it satisfies
plus the new `ErrConflict` sentinel (mapped to the closed-vocabulary `conflict`
code in `internal/mcp`'s `structuredError`). `Service.Plane` is field-injected
at the composition root from `registry.BaseURL("repos")`; no route literal and
no `127.0.0.1:30xx` literal appears in `internal/repos`. No verb calls the plane
yet — this phase delivers the seam and its wire behavior.

**Done when:** the suite is green and each of these ids is covered by a genuine
test:

- R-25E7-7ZFH — `Commit` issues exactly one request carrying the key, the
  byte-identical `main.py` entry, the message, and `X-Client-Id:
  scripts:<script id>`, and returns the server's sha verbatim.
- R-27TZ-ZIWV — `Head`, `ReadFile` (binary-safe) and `RunToken` return the
  server's values verbatim, one request each.
- R-291W-DANK — 404/409/400/500 and a closed port map to `ErrNotFound`,
  `ErrConflict`, `ErrValidation`, a non-sentinel error, and
  `ErrSourceUnavailable` respectively.
- R-2A9S-R2E9 — the composition root builds the client from
  `registry.BaseURL("repos")`, `Service.Plane` is non-nil, and no
  `127.0.0.1:30` literal exists under `internal/repos`.
