# Phase 40 — `POST /embed`

*Realizes design Decision 30 (the synchronous embedding endpoint). Depends on Phase 39.*

Extend `internal/inference` with the embedding executor and handler, mounted as `POST /embed` through the same loopback guard. Validation per D30 (embedding catalog entry, dimension range, embedder-capable provider, role, inputs, size cap); execution via `provider.BuildEmbedder` + `admit.AcquireCall` + `agentkit.Embedder`; recording as class `embedding` (request body stored, response body NULL); response with vectors in input order, usage, cost.

**Done when:** the suite is green and these ids are covered by tagged tests driving the real handler with an injected fake `agentkit.EmbeddingProvider`:

- R-604H-L3QC — 200 happy path: ordered vectors, priced usage, one `embedding` row (request body set, response body NULL)
- R-61CD-YVH1 — non-embedding catalog model → 400, no row
- R-62KA-CN7Q — dimensions outside the catalog range → 400 naming the range, no row
- R-63S6-QEYF — embedder-less provider → 400, no row
- R-6503-46P4 — empty/blank inputs → 400, no row
- R-667Z-HYFT — embedding provider failure → 502, row records the error
