# Phase 105 — The wrapped loop and finalizer, and the loop assets rewritten for the driver

*Realizes design Decision 68 (autotune driver), slice R-F3MD-7DAA, R-F4U9-L50Z, R-F625-YWRO, R-F7A2-COID, and Decision 67 (loop protocol — structural). Depends on Phase 104.*

`cmd/autotune` runs the loop and always lands the same way: the verbatim `--` passthrough execs `ralph <args> eval/<step>/improve.md` (streamed through, signals forwarded), the watcher prints a unified `eval/<step>/prompt.txt` → `autotune/<step>/prompt.txt` diff on every accept, and the finalizer runs on any child exit — holdout once + `summary.md` (with the overfit call-out) + final diff when `best/` differs from the start prompt and holds no holdout scorecard yet; a plain "no improvement" line, no holdout, exit 0 otherwise. The committed assets catch up with D67: `eval/extract/improve.md` rewritten to the one-turn protocol (workspace config, accept-copies-to-`autotune/`, CONTINUE-always except perfect-1.0 or unprovisioned-workspace DONE, no bootstrap/holdout/reject-streak), `eval/extract/README.md` rewritten as the driver-era operator guide, and `wiki/.gitignore` gains `/autotune/`.

**Done when:**
- R-F3MD-7DAA — exact ralph argv with and without passthrough — covered by a tagged test.
- R-F4U9-L50Z — diff printed on mid-run workspace-prompt change; none when unchanged — covered by a tagged test.
- R-F625-YWRO — finalizer with a winner across success/non-zero/signaled exits: one holdout run, `summary.md` contents, final diff, no re-holdout on a stamped resume — covered by a tagged test.
- R-F7A2-COID — finalizer without a winner: no holdout, "no improvement" with attempt count, exit 0 — covered by a tagged test.
- `wiki/.gitignore` contains a `/autotune/` line; `grep -c "CONTINUE" eval/extract/improve.md` ≥ 1 and `grep -c "consecutive" eval/extract/improve.md` = 0 (the reject-streak rule is gone from the asset).
- The suite is green per design Conventions.
