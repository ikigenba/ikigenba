# Phase 177 — Autotune: the `match` folder, its scorer, and the extract corrections cases

*Realizes design Decisions 71 and 72 (corrections slice: R-7YU7-T9RG, R-8024-71I5, R-81A0-KT8U) and Decision 6's tune-case id R-7E3X-B65N. Depends on Phase 173 (the match prompt and render shape it copies).*

Builds the committed `autotune/match/` folder (prompt copy, `improve.md`, pinned `config.json`, deterministic `score`, fixtures, README, `.gitignore`, dev/holdout cases drawn from the shared universe with aligned splits), extends the extract scorer with the corrections alignment component and the extract case set with the correction-ruling dev case, and updates `autotune/folders_test.go` to walk all five folders.

**Done when:** the suite is green (`go test ./...` from `wiki/`, scorer fixtures running under `SCORE_SKIP_JUDGE=1` where applicable), `autotune/match/prompt.txt`, `autotune/match/score`, and `autotune/extract/cases/dev/correction-ruling/input.txt` exist, `grep -c 'match' autotune/folders_test.go` returns a non-zero count, and each id is covered by a genuine tagged test:

- R-7E3X-B65N — the committed correction-ruling extract dev case.
- R-7YU7-T9RG — the `autotune/match` folder conforms in full.
- R-8024-71I5 — the match scorer's deterministic edge-set scoring and floors.
- R-81A0-KT8U — the extract scorer's corrections component.
