# Phase 99 — The deterministic scorer (`internal/eval`)

*Realizes design Decision 65 (all ids) and Decision 64 (slice: R-KJKV-RYMZ, R-KKSS-5QDO — the config and gold loaders). Depends on Phase 98.*

The `internal/eval` package: config and gold loaders, the subject/field/claim
scoring pipeline, the deterministic scorecard, the disk-backed embedding cache
behind the `EmbedFunc` seam, and the acceptance arithmetic (`Epsilon`/`Accept`)
— exactly the D65 shapes. Pure library: no provider wiring, no main; every test
runs against constructed fixtures and fake `EmbedFunc`s. No agentkit import in
this phase (the runner wires providers in Phase 100).

**Done when:**
- R-KJKV-RYMZ — config loads with all pins intact; missing field or bad weights fail loudly naming the field — covered by a tagged test.
- R-KKSS-5QDO — gold loader walks dev/holdout, loads the seeded cases, fails loudly naming a malformed case — covered by a tagged test.
- R-KM0O-JI4D — subject pairing by normalized name/alias + type; misses and spurious counted — covered by a tagged test.
- R-KN8K-X9V2 — field checks and field-accuracy arithmetic — covered by a tagged test.
- R-KOGH-B1LR — claim pairing accepted under threshold + margin + digit-token agreement — covered by a tagged test.
- R-KPOD-OTCG — each claim-rejection path (below threshold; within margin; disjoint digit tokens) rejects independently — covered by a tagged test.
- R-KQWA-2L35 — one-to-one greedy assignment; no claim in two pairs — covered by a tagged test.
- R-KTC2-U4KJ — precision/recall/F1, field accuracy, and weighted composite match hand-computed fixtures; empty extraction yields 0, not NaN — covered by a tagged test.
- R-KUJZ-7WB8 — byte-identical `MarshalDeterministic` output across repeated scoring — covered by a tagged test.
- R-KVRV-LO1X — disk cache: zero wrapped calls on a warm second run; model or text change misses — covered by a tagged test.
- R-KWZR-ZFSM — `Epsilon` is max−min; `Accept` strictly above best+epsilon only — covered by a tagged test.
- The suite is green per design Conventions.
