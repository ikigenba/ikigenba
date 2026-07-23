# Phase 118 — Readiness proof: one small baseline run per folder, outputs discarded

*Realizes — (structural: the operator-agreed readiness gate; every behavior it exercises is owned by D71/D72 ids). Depends on Phase 115, 116, 117.*

Prove each committed folder runs end to end under the real toolchain: for each of the four steps, run the external tool with a small spend cap from the `wiki/` root — `autotune --max-spend 2 autotune/<step>` — and confirm it authenticates (`~/.autotune/auth.json`), executes every dev case, scores them (scorers reading `OPENAI_API_KEY` from the environment), and writes a baseline scorecard under the folder's `runs/`. **Nothing from these runs is kept**: delete each folder's `runs/` afterward and confirm the tree is clean. If the subscription login or the key is absent, stop and surface it to the operator rather than improvising credentials. Any tuning beyond the baseline is out of scope; adoption remains manual.

**Done when:**
- For each of `autotune/extract`, `autotune/analysis`, `autotune/compile`, `autotune/synthesis`: the capped `autotune` invocation exits 0 and a baseline scorecard file exists under that folder's `runs/` before cleanup.
- After deleting the four `runs/` directories, `git status --porcelain` from `wiki/` shows no change attributable to the runs.
- The suite is green (design Conventions).
