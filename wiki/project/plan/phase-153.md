# Phase 153 — Orphan-free page embeddings: guarded writes, joined hydration, sweep reaping

*Realizes design Decision 85 (orphan-free page embeddings).*

Hardens the durable embedding lifecycle in `internal/wiki` so no `page_embeddings` row can outlive its subject. End state: `EmbeddingStore.Upsert` lands only when the subject row exists at write time (the existence check inside the write statement; a vanished subject is a silent zero-row no-op, never an error); startup hydration (`LoadVectorCacheEntries`) resolves scopes via a join against `subjects` so an orphan row is skipped instead of failing the load (the per-embedding scope lookup that errors on `sql.ErrNoRows` is deleted); each catch-up sweep cycle first deletes any embedding rows whose subject is gone, on the write handle.

**Done when:** the suite is green (design Conventions) and each of these ids is covered by a clearly-named test:

- R-R7IF-IID7 — the guarded `Upsert` no-ops for a deleted subject and persists for a live one.
- R-R8QB-WA3W — hydration succeeds over a seeded orphan row, returning every live entry with its correct scope and omitting the orphan.
- R-R9Y8-A1UL — a sweep cycle reaps orphan rows, leaves live rows untouched, and the next hydration is clean.
- R-RB64-NTLA — the select→delete→store race end to end through the real worker path leaves no orphan and errors nothing.
