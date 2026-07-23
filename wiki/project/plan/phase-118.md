# Phase 118 — Readiness proof: one bounded run per folder, outputs discarded

*Realizes — (structural: the operator-agreed readiness gate; every behavior it exercises is owned by D71/D72 ids). Depends on Phase 115, 116, 117.*

Prove each committed folder runs end to end under the real toolchain: for each of the four steps, run the external tool with an iteration-bound budget from the `wiki/` root — `autotune --max-iterations 1 autotune/<step>` — and confirm it authenticates (`~/.autotune/auth.json`), executes every dev case, scores them (scorers reading `OPENAI_API_KEY` from the environment), writes a baseline scorecard under the folder's `runs/`, and completes exactly one improver iteration (exercising the folder's committed `improve.md` contract against the real improver model). The rail binds after that iteration: the expected termination is exit code 3 with stop reason `max iterations`; the only other acceptable termination is exit code 0 with stop reason `perfect score` (a baseline composite of 1.0 stops the run before the improver). The four runs are independent and may be launched concurrently or sequentially. **Nothing from these runs is kept**: delete each folder's `runs/` afterward and confirm the tree is clean. If the subscription login or the key is absent, stop and surface it to the operator rather than improvising credentials. Any tuning beyond the single bounded iteration is out of scope; adoption remains manual.

**Done when:**
- For each of `autotune/extract`, `autotune/analysis`, `autotune/compile`, `autotune/synthesis`: the invocation terminates with exit code 3 and stop reason `max iterations` (or exit code 0 and stop reason `perfect score`), and a `runs/*/baseline.json` exists under that folder before cleanup.
- After deleting the four `runs/` directories, `git status --porcelain` from `wiki/` shows no change attributable to the runs.
- The suite is green (design Conventions).
