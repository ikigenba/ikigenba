# Phase 166 — Liveness, the five-status surface, and wedge visibility

*Realizes design Decision 95 (job liveness and the honest job surface). Depends on Phase 165.*

A job is always queued, running, or terminal. `jobs.deadline_at` is stamped at
handoff and extended whenever a unit lands, and `ReapExpired` runs on the drain's
tick, failing every job past its deadline — recording that its completion never
arrived — and clearing staging and lease in the same transaction. An absolute
lifetime ceiling measured from admission applies regardless of progress, so no
document can hold its scope's lease indefinitely; the lease's own expiry is
shorter than the job deadline, so a leaked lease is reclaimed too.

The surface reports only the five product statuses. The handoff phase never
surfaces: a handed-off document reads `working`, which removes the state that
was observable but not filterable rather than growing the filter's valid set.
`status` returns the job's compiled subjects once `done` and reports units
complete and units total while running.

Every apply decision emits one structured record carrying outcome class, reason,
job, stage, and unit; each tick emits a summary; consecutive ticks that see
items but apply, discard, and fail none are reported as a no-progress condition
and degrade health. Health gains running-job count, oldest running job age,
deferred count, and the no-progress streak.

**Done when:** these ids are covered by clearly-named tests and the suite is
green:

- R-PA2D-OEYB — a job whose completion never arrives is reaped to `failed` with
  its staging and lease cleared, and the next job in that scope is admitted
- R-PBAA-26P0 — a progressing job is not reaped, and is still failed once past
  its absolute lifetime ceiling
- R-PCI6-FYFP — every status reported across a job's whole life is one of the
  five valid values, and the internal phase never appears
- R-PEXZ-7HX3 — a filter naming all five statuses returns every existing job
  including one awaiting a completion, and the count matches the listing
- R-PDQ2-TQ6E — a `done` ingest reports its compiled subjects; a running one
  reports units complete and total
- R-PG5V-L9NS — every apply decision emits its outcome and reason, and a drain
  seeing items while applying none reports a no-progress condition
- R-PHDR-Z1EH — health reports the running count, oldest running age, and
  no-progress streak, and degrades while the drain makes no progress
