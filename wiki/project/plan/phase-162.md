# Phase 162 — Serialize ingest admission behind a durable per-scope lease

*Realizes design Decision 93 (ingest lease).*

Ingest stops running documents concurrently within a scope. A new `ingest_lease`
table (created with `bin/create-migration wiki create-ingest-lease`, keyed on
`scope`) is acquired in the same transaction that admits a `pending` job and
released in the same transaction that ends the job's hold — the integrate
commit, `FailWaiting`, `FinishWorking`, `Abort`, and the boot sweep's
`working → pending` requeue. `ClaimPending` gains the "no lease held for this
scope" condition and the lease TTL; `ReleaseLease` is called from every terminal
writer.

Merge jobs take the same lease, and the in-memory `mergeMu` is removed with them.
`JobStore.Wait` is renamed `MarkPhase` and `Service.Wait` becomes `WaitForWork`,
so no seam is named for a block it never performed. Every lease release notifies
the claim loop, and the loop keeps a periodic tick so a refused admission cannot
strand a freed scope.

The migration is additive and forward-only; existing rows are untouched. Any job
already `working` at deploy is requeued by the existing boot sweep and admitted
normally under the new condition.

**Done when:** these ids are covered by clearly-named tests and the suite is
green (`go test ./...` from `wiki/`, per design Conventions):

- R-OXVD-UPJD — a second job in a scope is not admitted while that scope's lease
  is held, and admission writes status and lease in one transaction
- R-P2QZ-DSI5 — a job in a different scope is admitted concurrently
- R-OZ3A-8HA2 — every terminal transition deletes the lease in the same
  transaction and the next job in that scope is then admitted
- R-P1J3-00RG — the boot sweep's requeue releases the lease it inherited
- R-P3YV-RK8U — a refused admission does not strand the scope: the next job is
  admitted after release with no further ingest arriving
