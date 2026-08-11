# Phase 77 — The completion queue core: table, store, executor, envelope, retention

*Realizes design Decision 29 (the completion queue) — the storage and execution slice.*

Builds the new `internal/completion` package and its migration: the `completions` table (created with `bin/create-migration prompts create-completions`), the store (ensure-idempotent insert on `(origin, key)`, claim, terminal writes, inbox/get reads, ack delete, TTL sweep), and the executor pool — claim oldest `queued`, run under the configurable runtime bound (production 4 h) through the injected provider factory with D31 `AcquireCall` around every round trip, enforce the `{status, result, message}` JSON envelope with up to 3 internal corrective round trips, unwrap to `done`/`failed`, write one D28 `calls` row per executed round trip, aggregate usage/cost onto the item. Boot requeues `running` → `queued`. The hourly retention sweep deletes only terminal items past the 7-day TTL.

End state: the queue lifecycle is fully exercisable in-process against a temp DB and a scripted fake provider; no HTTP surface yet (phase 78).

**Done when:**

- These Verification ids are covered by clearly-named tests tagged verbatim:
  - R-JBE5-L2M1 — queued item → `done` with unwrapped result, usage/cost, one `calls` row
  - R-JCM1-YUCQ — envelope violation → internal corrective round trip; per-round-trip `calls` rows
  - R-JDTY-CM3F — envelope never satisfied → `failed` after exactly 3 corrective round trips
  - R-JF1U-QDU4 — envelope `status:"error"` → `failed` with the envelope's `message`
  - R-JG9R-45KT — provider failure → `failed` with the provider error, recorded on the `calls` row
  - R-JHHN-HXBI — runtime bound elapsed → `failed` naming the bound
  - R-JIPJ-VP27 — boot requeue: `running` → `queued` → re-executed to terminal
  - R-JOT1-SJRO — TTL sweep deletes only terminal items older than 7 days (injected clock)
- `go test ./...` from `prompts/` is green.
