# Phase 165 — The inbox drain contract: no item may stop the drain

*Realizes design Decision 94 (inbox drain contract). Depends on Phase 164.*

`applyCompletion` returns an `Outcome{Class, Reason, Err}` instead of an error,
and `ProcessInbox` returns a `Drain` count struct rather than `error`. The drain
classifies and handles every item it is handed; only a canceled context ends a
tick early. The worker's inbox loop consumes the `Drain` and can no longer
discard the result.

Ack policy follows the class: `Applied`, `Discarded`, and `JobFailed` all ack;
only `Deferred` skips the ack. Deferral is reserved for environment conditions
(prompts unreachable, transport error, database busy) and is bounded — past a
small number of consecutive deferrals the item is reclassified `JobFailed`, its
job fails with the last error, and the item is acked, so the head of the queue
always clears. Every unrecognized error is permanent by default.

Ack remains the last action after every consequence is durable, and only
terminal items are ever acked. A failed job's recorded error names what failed
and what to do, since the job list is the only place an unattended ingest
surfaces.

The two worker loops stay separate: merges still compile inline, so folding them
together would let one scope's merge stall completion-apply for every scope.

**Done when:** these ids are covered by clearly-named tests and the suite is
green:

- R-P56S-5BZJ — a drain whose first item fails still applies every later
  applicable item in the same tick
- R-P6EO-J3Q8 — a permanently unapplyable item fails its job with a named cause,
  clears staging, releases the lease, and is acked
- R-P7MK-WVGX — a transient failure is not acked and is retried, then
  reclassified permanent after the bound, failing the job and acking the item
- R-P8UH-AN7M — a failure injected between the integrate commit and the ack
  leaves the job `done`, and the redelivery is discarded without altering state
