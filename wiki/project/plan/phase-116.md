# Phase 116 — Tune folder: compile, with its hybrid scorer and seed corpus

*Realizes design Decision 71 (compile slice) and 72 (R-AD4E-PZJF, R-AFK7-HJ0T). Depends on Phase 114 (the extract cases the corpus derives from; the shared test package).*

Create `autotune/compile/` per D71: `prompt.txt` copied from `internal/compile/prompt.txt`; `improve.md`; the pinned `config.json`; `judge-prompt.txt` (the fixed rubric); a python3 hybrid `score` (deterministic gates — `{title, body}` contract, 12,000-char cap, citations resolve to claim ids present in `input.txt` — then a `gpt-5.6-sol` judge over the rubric, combined by top-of-script weights, honoring `SCORE_SKIP_JUDGE=1`); `fixtures/` with hand-computed expected gate outcomes; `README.md`; `.gitignore`. Seed the corpus (~14 dev / ~7 holdout) derived from the shared gold universe: each case's `input.txt` is the production-rendered compile user message built from an extract-gold subject's identity + claims, `gold.json` the key facts + judge notes; **split assignment follows the universe document's existing dev/holdout placement** (no leakage). Seed cases count as gold only after operator review (noted in the folder README). Extend `autotune/folders_test.go` to cover the folder and shell the scorer's deterministic paths, plus the env-gated live judge smoke.

**Done when:**
- R-AD4E-PZJF — the compile scorer (judge skipped) floors an over-cap body, an unresolvable citation, and a malformed output at the fixtures' expected values, passes a clean fixture, and scores identically across two runs.
- R-AFK7-HJ0T — with `WIKI_TUNE_LIVE=1` and `OPENAI_API_KEY` set, the judge path completes a real sol call returning a composed score in [0,1] (test skips without the env).
- `autotune/compile` passes the folder-contract assertions in the test package (all required files, executable scorer, both splits non-empty, valid `gold.json` in every case).
- The suite is green offline (design Conventions) — the live smoke skips.
