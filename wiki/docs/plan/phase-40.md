# Phase 40 — Itemized (diff-style) scorecard output + the hard gold case

*Realizes design Decision 23 (the itemized human scorecard) and Decision 22 (the shipped hard gold case). Depends on Phase 37 (the `internal/eval` `CaseResult`/`SubjectScore`/`ClaimText` types and `Score`) and Phase 38 (the `Scorecard` + `WriteHuman`/`WriteJSON` and the committed dataset).*

`Scorecard.WriteHuman` stops collapsing the per-case partitions to bare counts and instead renders them as a diff the operator can read, and the dataset gains a demanding hard case so the diff has something non-trivial to show. The observable end state:

- `WriteHuman` emits, per case, each found/missed/hallucinated **subject** by `type/slug` under a distinct partition marker (`=`/`-`/`+`), and the **claim text** for every partition: for a matched subject each covered claim shows both its gold claim and the predicted claim that covered it, each missed claim shows the gold text, each extra claim shows the predicted text; for a missed subject every gold claim renders as missed text and for a hallucinated subject every predicted claim renders as extra text. This is a pure rendering change over data already on `CaseResult` (D21) — no `internal/eval` type, scoring, or judge change.
- The existing per-case precision/recall and `found/extra/missed`, `covered/extra/missed` count lines and the aggregate + per-difficulty rollups are **retained** (the diff is additive), and `WriteJSON` is unchanged and still round-trips into an equal `Scorecard`.
- A second blessed case ships under `testdata/eval/extract/meridian-freshcrate-acquisition/` (`document.txt` + `gold.json`, D20 shape) — a hard, multi-entity acquisition whose gold holds more than one subject, each with multiple cross-referencing claims. (The case files are present for human blessing; this phase guards them with a load test.)

**Done when:**
- R-8KX2-4ASY, R-8M4Y-I2JN, R-8NCU-VUAC, R-8OKR-9M11 (D23) — each covered by a clearly-named test asserting the rendered output of `WriteHuman` over a constructed `CaseResult`: subject identities appear under their markers; covered claims show both gold and predicted text; missed/extra claims show their text (matched and unmatched subjects alike); the count/metric lines and aggregate survive and `WriteJSON` still round-trips.
- R-8PSN-NDRQ (D22) — a clearly-named test asserts the committed `meridian-freshcrate-acquisition` case loads through `eval.LoadCase` with no error, `difficulty == "hard"`, more than one gold subject, each with ≥1 claim.
- The suite is green per design *Conventions*.
