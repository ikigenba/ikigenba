# Phase 111 — The driver's second step: `autotune analysis`, loop assets, and the seed gold

*Realizes design Decision 69 (analysis eval workbench — driver slice) and the rewritten Decision 68 step dispatch. Depends on Phase 110.*

`cmd/autotune`'s supported-step set becomes `{extract, analysis}` with the per-step runner build target (`go build -o tmp/autotune/<step>/bin/eval-<step> ./cmd/eval-<step>`); everything else in the driver is already step-generic and stays untouched. The existing test tagged R-EYQR-OABI is updated to D68's rewritten behavior (`analysis` proceeds; the error names `extract, analysis` as the supported set). The committed loop assets land: `eval/analysis/improve.md` (the D67 protocol with analysis paths and the three-list artifact description) and `eval/analysis/README.md` (operator guide, including the noted reasoning-effort divergence of the reference config). The seed gold corpus lands per D69: one `dev/` case and one `holdout/` case, each a question drafted from a named document of the corresponding extract gold split with its `gold.json` lists — drafts for operator review, with corpus growth continuing outside build phases.

**Done when:**
- R-BZFF-ORTW — `autotune analysis` (scripted executor) provisions `tmp/autotune/analysis/`, seeds `autotune/analysis/prompt.txt` from `eval/analysis/prompt.txt`, builds `./cmd/eval-analysis` to the workspace bin path, and runs the baseline against the analysis workspace config — while `autotune extract` still builds `./cmd/eval-extract`.
- The R-EYQR-OABI-tagged test asserts the rewritten dispatch behavior and passes.
- `eval/analysis/improve.md`, `eval/analysis/README.md`, and exactly one seeded case under each of `eval/analysis/gold/dev/` and `eval/analysis/gold/holdout/` exist, and `LoadAnalysisGold` over the committed corpus returns one dev and one holdout case in a passing test.
- Suite green (`go test ./...` from `wiki/`).
