# Phase 39 — Seed every existing script into the version plane

*Realizes design Decision 40 (the seeding sweep — its slice: R-2W7Z-MXQR,
R-2XFW-0PHG). Depends on Phase 36.*

`Service.SeedRepos(ctx)` runs from `registerRoutes` after the crash-recovery
sweep: for each row with `repo_seeded_at IS NULL` it fills `name_key` if
missing, creates the repository (treating `ErrConflict` as "already exists"),
commits the stored `body` as `main.py`, and stamps the row. It is idempotent
(a stamped row is never revisited) and non-fatal (a failed row is logged, left
unstamped for the next boot, and does not stop boot). No column is dropped in
this phase — the retirement is Phase 40, and it must not be deployed until this
sweep has run in production and stamped every row.

**Done when:** the suite is green and each of these ids is covered by a genuine
test:

- R-2W7Z-MXQR — the sweep creates and commits each unseeded row's stored body
  byte-identically as `main.py`, stamps `name_key` and `repo_seeded_at`, alters
  nothing else, and makes zero plane calls on a second run.
- R-2XFW-0PHG — an `ErrConflict` on `Create` still commits and stamps; a failing
  `Commit` leaves that one row unstamped while the others are stamped and the
  sweep returns without error; a later healthy sweep stamps the straggler.
