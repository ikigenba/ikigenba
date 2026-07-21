# Phase 101 — The improvement-loop assets (`improve.md`, operator README)

*Realizes design Decision 67 (structural — no ids). Depends on Phase 100.*

The committed loop assets, exactly per D67:

- `eval/extract/improve.md` — the single-prompt ralph loop encoding the D67
  protocol: the `tmp/autotune/extract/` workspace layout, the bootstrap
  (baseline via `-repeat 3`), the one-candidate-per-turn cycle
  (run → compare → promote-or-keep → history line), the stop rule
  (5 consecutive rejects → holdout run on the winner → summary → `DONE`), and
  the hard write boundary (only `tmp/autotune/extract/`; never `eval/`,
  `internal/`, `cmd/`).
- `eval/extract/README.md` — the operator guide: building `bin/eval-extract`,
  producing a baseline, launching `ralph eval/extract/improve.md` from `wiki/`
  (harness/model at the operator's choice), reading the workspace, the
  reproduce-then-adopt step, and how the gold corpus grows by operator review.

**Done when:**
- `eval/extract/improve.md` and `eval/extract/README.md` exist.
- From `wiki/`, `grep -l 'tmp/autotune/extract' eval/extract/improve.md eval/extract/README.md` lists both files, and `grep -c 'DONE' eval/extract/improve.md` prints ≥ 1 — the loop names its workspace and carries its exit token.
- `grep -c '^/tmp/$' .gitignore` from `wiki/` prints `1` (the workspace stays disposable).
- The suite is green per design Conventions.
