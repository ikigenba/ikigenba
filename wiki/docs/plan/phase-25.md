# Phase 25 — Job lifecycle & control: `aborted`, abort, re-run, atomic integrate

*Realizes design Decision 14 (job lifecycle & control). Depends on Phase 7 (the ingest worker and integrate path) and Phase 24 (D13 `WithJobID` recording, so the worker tags each job's calls and an aborted call still records).*

Give the operator/owner direct control over the ingest queue — abort a queued or running job, re-run a terminal one — and make both correct by rebuilding `integrate` into the **atomic, idempotent** commit D4 always intended but Phase 7's code did not deliver. This is the heaviest phase: it rewrites the worker's commit path. (Append-only note: Phase 7 stays as history; this phase supersedes its integrate/commit behavior and genuinely satisfies D4's R-MB7F atomicity. The whole phase lives in `internal/wiki`, with a one-line `internal/worker` touch.)

**What gets built (the observable end state):**

- **`aborted` state** — `JobAborted = "aborted"`, terminal and distinct from `failed`. No DB `CHECK` to alter (the live `jobs.status` has none).
- **Atomic, idempotent `integrate`** — build the write-set with **no DB tx held** across the LLM calls; then one commit tx that: deletes this job's prior claims; upserts the affected subjects (`oldSubjects ∪ newSubjects`, first-writer-wins `type`); inserts this job's new claims; for each affected subject upserts its recompiled page **or deletes the page when its final claim set is empty**; and `UPDATE jobs SET status='done', finished_at=now WHERE id=? AND status='working'`. A **0-row** job-update (an abort flipped it concurrently) → rollback, nothing lands. Any build error → `UPDATE … status='failed' … WHERE status='working'`, writing no subjects/claims/pages. The per-subject claim set is computed in memory as `(persisted − this job's old) + this job's new`. The worker wraps the integrate ctx with `llm.WithJobID(ctx, job.ID)` so the job's calls are recorded.
- **Abort** — `Service.Abort(ctx, jobID)`: pending → `UPDATE … aborted WHERE status='pending'`; working → `UPDATE … aborted WHERE status='working'` then call the registered per-job `cancel()`; terminal/unknown → clean not-abortable result, status unchanged. A mutex-guarded `map[string]context.CancelFunc` on the `Service` is populated by `ProcessNext` (`jobCtx, cancel := context.WithCancel(ctx)`, registered under `job.ID`, deregistered in `defer`).
- **Re-run** — `Service.Rerun(ctx, jobID)`: `UPDATE jobs SET status='pending', started_at='', finished_at='', error='' WHERE id=? AND status IN ('done','failed','aborted')`, then nudge the worker; a 0-row result on a `pending`/`working` job → clean "can't re-run a job in progress" error, on a missing job → not-found; `source_text` is never rewritten.

**Done when:**

- R-0SCX-95OZ — aborting a `pending` job moves it to `aborted` and the worker never integrates it.
- R-0TKT-MXFO — aborting a `working` job cancels its in-flight call, the job ends `aborted`, and **zero** subjects/claims/pages are committed.
- R-0USQ-0P6D — `aborted` is terminal and distinct from `failed` (reports `aborted`, no integration `error`).
- R-0W0M-EGX2 — aborting an already-terminal or unknown job returns a clean not-abortable result, status unchanged.
- R-0X8I-S8NR — re-running a terminal job returns it to `pending` and the worker reprocesses it from the unchanged `source_text`.
- R-0YGF-60EG — after a re-run, the job's claims are **exactly** its new extraction's (prior gone, none duplicated) and affected pages are recompiled.
- R-0ZOB-JS55 — a re-run that drops a previously-touched subject recompiles its page from the reduced claim set and deletes the page when its final claim set is empty.
- R-10W7-XJVU — re-running a `pending`/`working` job returns a clean error and leaves status unchanged.
- Integration tests wire the worker + real temp SQLite + mock provider (a blockable mock for the working-abort race); the suite is green.
