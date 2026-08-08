# Phase 48 — Version-plane columns and their store accessors

*Realizes design Decision 15 (data model — the `repo_sha` / `repo_seeded` slice).*

`internal/sites` and `internal/db` gain the bookkeeping every later phase writes
through. One **additive** migration created with `bin/create-migration sites
<name>` adds `repo_sha TEXT NOT NULL DEFAULT ''` and `repo_seeded INTEGER NOT
NULL DEFAULT 0` to `sites` by `ALTER TABLE … ADD COLUMN` — no rebuild, no
`DROP TABLE`, every existing row carried forward untouched. Every previously
committed migration stays frozen.

`Site` gains `RepoSha string` and `RepoSeeded bool`, populated by the existing
`Get`/`List`/scan paths, and the store gains `SetRepoSha`, `MarkSeeded`, and
`ListUnseeded` as D15 declares them. `SetRepoSha` deliberately does **not** bump
`updated_at`.

**Done when:**

- R-OG3H-7TUS — against the real migrated SQLite: `pragma table_info(sites)`
  reports `repo_sha` (TEXT NOT NULL, default `''`) and `repo_seeded` (INTEGER
  NOT NULL, default `0`); a row inserted after the pre-existing migrations but
  before the new one survives it with every original column byte-identical and
  the new columns at their defaults; `SetRepoSha` persists a sha readable
  through `Get` while leaving `name`, `slug`, `visibility`, `owner_id`,
  `owner_email`, `created_at` and `updated_at` unchanged; `MarkSeeded` is
  idempotent; `ListUnseeded` returns exactly the `repo_seeded = 0` rows in slug
  order.
- The suite is green (design *Conventions*).
- Deterministic structural check: `grep -riE 'drop table|create table sites' sites/internal/db/migrations/<the new file>` returns **no** match (exit 1) — the new migration is additive.
