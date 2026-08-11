# Phase 152 — Scope-labeled vector cache: scope-carrying updates and scope-wide eviction

*Realizes design Decision 84 (scope-labeled vector cache).*

Builds the in-process half of the scope-delete cascade across `internal/retrieve`, `internal/wiki`, and the composition root (`cmd/wiki/main.go`, the one legitimately shared file). End state: the vector-cache update hook signature carries the subject's scope end to end (`WithVectorCacheUpdater(func(scope, subjectID, title string, vec []float32))`), fed by `embedAndStore` on the ingest/merge after-commit path (the job's scope) and by the catch-up sweep (its candidate query joining `subjects` for each page's scope); `VectorCache.Upsert`/`Replace` panic on an empty scope and the `"" → "default"` coercion is deleted; `VectorCache` exposes `RemoveScope(scope)`; `ScopeStore` gains a `VectorInvalidate` hook driven immediately after `Delete`'s transaction commits (beside the existing `AskInvalidate`), wired in the composition root to `RemoveScope`. Query-side filtering is untouched.

**Done when:** the suite is green (design Conventions) and each of these ids is covered by a clearly-named test:

- R-R1EX-LNNQ — a runtime (re-)embed labels its live cache entry with the subject's actual scope: found by its own scope's meaning lane, absent from `default`'s (through the `buildSpec`-shaped service).
- R-R2MT-ZFEF — `Upsert`/`Replace` panic on an empty scope; no coercion path remains.
- R-R3UQ-D754 — `ScopeStore.Delete` empties the deleted scope's meaning lane immediately, no restart; other scopes' entries survive.
- R-R52M-QYVT — commit-then-evict: a failed delete transaction fires no eviction; a successful one evicts only after the rows are durably gone.
- R-R6AJ-4QMI — wiring guard: the composed service labels after-commit embeds with the job's scope and routes `scope_delete` through `VectorInvalidate`; dropping either wire fails the tests.
