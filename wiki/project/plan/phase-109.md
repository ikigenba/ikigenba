# Phase 109 — The analysis scorer: `internal/eval` config, gold, and list alignment

*Realizes design Decision 69 (analysis eval workbench — scorer slice). Depends on Phase 108.*

`internal/eval` gains the analysis side of the workbench per D69: `AnalysisConfig`/`LoadAnalysisConfig` (parses `eval/analysis/config.json`, fail-loudly discipline matching D64's), `AnalysisGoldCase`/`LoadAnalysisGold` (walks `dev/` + `holdout/`, one case = `question.txt` + `gold.json` with the three gold lists), and `ScoreAnalysisCase` scoring each of the three lists independently by exactly D65's claim-alignment rules (greedy best-first embedding pairing behind the existing `EmbedFunc` seam, threshold, margin, digit-token agreement, one match per string), rolling up per-list F1 into `composite = 0.50·F1_subq + 0.30·F1_kw + 0.20·F1_alias` with D65's honest-empty carve-out applied per list. Deterministic marshal, `Epsilon`, `Accept`, and the disk cache are the existing shared functions — reused, not reimplemented.

**Done when:**
- R-BKSN-3IXK — `LoadAnalysisConfig` parses the committed config with every pinned value intact and weights summing to 1; a missing field or bad weight sum fails naming the field.
- R-BM0J-HAO9 — `LoadAnalysisGold` returns every case under `dev/` and `holdout/` of a fixture corpus; a case missing `question.txt` or with malformed `gold.json` fails naming the case.
- R-BN8F-V2EY — list alignment accepts a pairing clearing threshold, margin, and digit agreement (fake `EmbedFunc`, per list).
- R-BOGC-8U5N — list alignment rejects each way independently: below threshold; within margin of an alternative; disjoint non-empty digit-token sets.
- R-BPO8-MLWC — one-to-one matching and the exact weighted composite on a hand-computed fixture.
- R-BQW5-0DN1 — honest empty per list: empty-gold/empty-output list scores 1.0; empty-gold with output scores 0; an all-empty case earns composite 1.0.
- Suite green (`go test ./...` from `wiki/`).
