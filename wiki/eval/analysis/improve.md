# Analysis prompt improvement turn

You are one unattended turn launched only by `autotune analysis`. Read state
from `tmp/autotune/analysis/`; do not provision or repair it.

## Hard write boundary

Write only beneath `tmp/autotune/analysis/` and `autotune/analysis/`. Never edit
committed paths, inspect holdout cases, run holdout, write a summary, or adopt a
prompt into `eval/analysis/prompt.txt`; finalization belongs to the driver.

## Preconditions and status

Require the resolved config, baseline, incumbent prompt and scorecard, runner,
and visible prompt provisioned by the driver. If any is missing, name it and
report `DONE`. If the incumbent dev mean composite is exactly 1.0, report
`DONE`. Otherwise perform exactly one candidate attempt and report `CONTINUE`.

## One candidate

1. Read `history.md` if present and inspect the incumbent prompt and per-case
   sub-query, keyword, and alias misses and spurious strings.
2. Write one focused revision to the next zero-padded
   `tmp/autotune/analysis/candidates/NNN-prompt.txt`.
3. Run the workspace `eval-analysis` binary over the dev split with the
   workspace config and three repeats, writing the matching scorecard path.
4. Compare the candidate with `best/scorecard.json`; only stdout exactly
   `accept` with exit 0 is acceptance, while `reject` is a normal rejection.
5. On acceptance, replace the files in `best/` and copy the prompt to
   `autotune/analysis/prompt.txt`; on rejection leave the incumbent untouched.
6. Append one history line with attempt number, paths, composites, and verdict,
   then report `CONTINUE`.

Do not run final evidence after the attempt. The driver owns holdout, summary,
and the final proposal diff.
