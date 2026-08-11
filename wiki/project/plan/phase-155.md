# Phase 155 — The dead-job discard: a missing job row integrates nothing, cleanly

*Realizes design Decision 88 (dead-job discard).*

Extends the existing in-transaction job-row guard in `internal/wiki` (`integrate` and `mergeSubjects`) to the missing-row case: `sql.ErrNoRows` from the guard read means the job's generation was deleted with its scope — return `nil` and write nothing, exactly like the existing not-`working` discard, instead of propagating the raw error. No cancellation machinery, no rerun; zombies run to completion and self-discard.

**Done when:** the suite is green (design Conventions) and each of these ids is covered by a clearly-named test:

- R-RJPF-C7S5 — an ingest job whose row is deleted mid-flight (scope deleted, namesake recreated) integrates as a clean no-op: `nil` returned, nothing from the dead generation in the recreated scope, no job row resurrected.
- R-RKXB-PZIU — the merge path discards the same way: a merge job whose row is gone returns `nil` and writes no alias, deletion, or page rewrite.
