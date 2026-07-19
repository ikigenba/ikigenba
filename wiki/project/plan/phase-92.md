# Phase 92 — Embeddings through `/embed`

*Realizes design Decision 34 (the embedding call site over prompts). Depends on Phase 91.*

Add `Client.Embed` to `internal/llm` (batch `/embed`, vectors in input order) and re-wire both embedding paths through it at the composition root: page side as `wiki.embed-page`/role `document`, query side as `wiki.embed-query`/role `query`, replacing the agentkit embedder and the recording wrapper on those paths. `EmbedSite` sheds its provider field; the `EMBED_MODEL`/`EMBED_DIMS` knobs and their fail-loud parsing are unchanged. D30 storage and D32/D33 retrieval are untouched consumers of the returned vectors.

**Done when:** the suite is green and these ids are covered by tagged tests:

- R-Z932-H2RA — embed knobs resolve with defaults and fail-loud parsing.
- R-1385-QVMS — `/embed` request carries model/dims/role/origin/group_id/name/inputs; vectors return in input order.
- R-14G2-4NDH — page wiring is `wiki.embed-page` + `document`; query wiring is `wiki.embed-query` + `query`, through the same client method.
- R-15NY-IF46 — the operator-run live smoke against a running prompts `/embed` is written and documented in the test file (runs only when the operator opts in, e.g. an env-gated test; excluded from the offline green gate).
