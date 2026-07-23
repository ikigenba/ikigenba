# Phase 117 — Tune folder: synthesis, completing the four-folder workspace

*Realizes design Decision 71 (R-A5T0-FD39, R-A88T-6WKN, R-A9GP-KOBC — the full four-folder contract) and 72 (R-AECB-3RA4, R-AGS3-VARI). Depends on Phase 116 (the structure ids assert all four folders).*

Create `autotune/synthesis/` per D71: `prompt.txt` copied from `internal/ask/synthesis-prompt.txt`; `improve.md`; the pinned `config.json`; `judge-prompt.txt`; a python3 hybrid `score` (deterministic gates — `{found, text, citations}` contract, citations resolve to supplied pages, `found:true` requires ≥1 citation, expected-empty cases must answer `found:false` — then the sol judge for groundedness and completeness, honoring `SCORE_SKIP_JUDGE=1`); `fixtures/`; `README.md`; `.gitignore`. Seed the corpus (~14 dev / ~7 holdout, at least one expected-empty case per split) from the shared universe: each `input.txt` is the production-rendered synthesis user message (question + page bodies drafted from the universe's claims), `gold.json` the expected key points, `found` value, and allowed citation set; split assignment follows the universe documents' placement, and questions align with the analysis corpus's splits. Seed cases count as gold only after operator review. Finish `autotune/folders_test.go`: the D71 structure, config-pin, and case-shape assertions now run over all four folders.

**Done when:**
- R-A5T0-FD39 — all four folders carry the required contract files (executable `score`, `prompt.txt`, `improve.md`, `config.json`, `.gitignore` covering `runs/`).
- R-A88T-6WKN — all four `config.json`s pin exactly runner openai/gpt-5.6-luna/sub/low and improver openai/gpt-5.6-sol/sub/high, with no temperature or max_tokens keys.
- R-A9GP-KOBC — in every folder both splits are non-empty, every case has non-empty `input.txt` + parseable `gold.json`, and no case name repeats across a folder's splits.
- R-AECB-3RA4 — the synthesis scorer (judge skipped) floors unsupplied-page citations, uncited `found:true`, and `found:true` on an expected-empty case at the fixtures' expected values; clean fixtures pass; identical across two runs.
- R-AGS3-VARI — with `WIKI_TUNE_LIVE=1` and `OPENAI_API_KEY` set, the judge path completes a real sol call returning a composed score in [0,1] (skips without the env).
- The suite is green offline (design Conventions).
