# Phase 51 — Widen the `llm_calls` stage CHECK to the current closed set

*Realizes design Decision 13 (the LLM-call footprint: `llm_calls` table + recorder
seam). Depends on Phase 24 (the original `llm_calls` migration + `LLMCallStore`),
Phase 35 (D19 — the `ask`→`ask-subject`/`ask-synthesis` split and per-call-site
config), and Phase 37/38 (D21 — the eval-only `judge` call site).*

The Phase-24 `llm_calls` migration shipped a stage CHECK pinned to the **pre-D19**
set, `CHECK (stage IN ('extract','compile','ask'))`. D19 (Phase 35) split the single
`ask` site into `ask-subject` + `ask-synthesis`, and D21 added the eval-only `judge`
site — but neither updated the CHECK. So every `ask-subject`, `ask-synthesis`, and
`judge` round-trip fails the constraint at the recorder's INSERT
(`CHECK constraint failed: stage IN ('extract', 'compile', 'ask')`), which has broken
**`ask`** and eval **`judge`** since Phase 35: they die trying to record their LLM
call rather than answering. A committed migration is immutable and SQLite cannot
`ALTER` a CHECK, so the fix is a new forward migration that rebuilds the table with
the corrected closed set, plus the regression guard the original migration lacked.

**End state.**

- `internal/db/migrations/<ts>_widen_llm_calls_stage.sql` — a new forward migration
  (generated with `bin/new-migration wiki widen_llm_calls_stage`; **never** hand-name
  the version) that rebuilds `llm_calls` to carry
  `CHECK (stage IN ('extract','compile','ask-subject','ask-synthesis','judge'))` —
  the exact set every `CallSite` emits, with the retired `ask` label **deliberately
  excluded**. SQLite cannot alter a CHECK, so the body is the standard rebuild:
  create the new table with the corrected CHECK → `INSERT INTO … SELECT *` the
  existing rows → `DROP TABLE` the old → `ALTER TABLE … RENAME` the new into place →
  re-create both indexes (`llm_calls_job` on `(job_id, started_at, id)` and
  `llm_calls_time` on `(started_at, id)`). All other columns, defaults, and the
  TEXT-timestamp convention are preserved verbatim from Phase 24. The copy carries
  every existing row; on the reset dev DB it copies zero rows, and no surviving row
  can hold the now-removed `ask` label.
- No Go changes are required for the fix itself — `LLMCallStore.Record`
  (`internal/wiki/llm_calls.go`) and the `internal/llm` recorder seam already write
  `stage` as-is; the closed set lives in the schema. The composition root and the
  embedded `db.FS` migration set pick the new file up automatically.

**Done when:** the suite is green (per design *Conventions* — including
`bin/check-migrations wiki`, which must accept the new file and still reject any edit
to a committed one) and these design Verification ids are covered by clearly-named
tests, exercised against a **real** temp SQLite migrated by the appkit runner (a mock
recorder cannot falsify a DB CHECK — the real engine is the only substrate that can):

- **R-EMWV-6RK5** (D13, real SQLite) — `LLMCallStore.Record` accepts a row for
  **every** canonical stage — `extract`, `compile`, `ask-subject`, `ask-synthesis`,
  and `judge` — against the migrated temp DB, and each row is persisted and reads
  back. A schema still pinned to the pre-D19 set fails this on `ask-subject`.
- **R-EO4R-KJAU** (D13, real SQLite) — `LLMCallStore.Record` for a non-canonical
  stage (the retired `ask`, and an arbitrary `bogus`) is refused by the DB CHECK and
  lands no row, proving the closed set is engine-enforced, not convention.

The existing D13 ids (R-VNS0-1Z85 … R-VV3E-CLOB) stay green: the rebuild changes only
the CHECK membership, leaving every column, index, default, and the
append-only/detached-write behavior untouched.
