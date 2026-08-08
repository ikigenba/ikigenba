# Phase 40 — Retire the `body` column behind a seeding guard

*Realizes design Decision 40 (the retiring migration — its slice: R-2YNS-EH85).
Depends on Phase 39.*

A second timestamped migration (`bin/create-migration scripts retire_body`)
aborts through a `CHECK`-guarded temp table when any `scripts` row still has a
NULL `repo_seeded_at`, and otherwise drops the `body` column. The same phase
deletes `Service.SeedRepos` and its wiring (its input is gone), stops
`create`/`update`/`import` writing `body`, and drops `Script.Body` from the
stored-row path — `get` already reads the plane (Phase 36). **Deploy ordering:**
this must reach the box only after Phase 39's sweep has stamped every row there;
the guard makes a premature deploy fail loudly instead of losing content.

**Done when:** the suite is green and this id is covered by a genuine test:

- R-2YNS-EH85 — on a fully-stamped database the migration succeeds, `PRAGMA
  table_info(scripts)` shows no `body` column, and every row survives with its
  identity columns intact; on a database with one unstamped row the migration
  fails and `body` and its content remain; every previously committed migration
  stays byte-identical to its frozen body.
