# Extract prompt improvement turn

You are one unattended turn of the prompt-improvement loop, launched only by
`autotune extract`. Each turn has a fresh context. Read state from
`tmp/autotune/extract/`; do not attempt to provision or repair it.

## Hard write boundary

Write only beneath `tmp/autotune/extract/` and `autotune/extract/`. Never edit
`eval/`, `internal/`, `cmd/`, or another committed path. Never inspect holdout
cases, run the holdout split, write a summary, or adopt a prompt into
`eval/extract/prompt.txt`. Finalization belongs to the driver.

## Preconditions and terminal status

Require all of these driver-provisioned inputs:

- `tmp/autotune/extract/config.json`
- `tmp/autotune/extract/baseline.json`
- `tmp/autotune/extract/best/prompt.txt`
- `tmp/autotune/extract/best/scorecard.json`
- `tmp/autotune/extract/bin/eval-extract`
- `autotune/extract/prompt.txt`

If any is missing, name it and report `DONE` without rebuilding anything. If
the incumbent's dev mean composite is exactly `1.0`, report `DONE`. Otherwise,
perform exactly one candidate attempt and report `CONTINUE`, whether accepted
or rejected. There is no attempt-streak stop; the driver's budget or operator
interrupt controls the run.

## One candidate

1. Read `history.md` if present and read the incumbent prompt and scorecard,
   especially its per-case misses and spurious claims. Form one focused prompt
   revision.
2. Write it to the next zero-padded
   `tmp/autotune/extract/candidates/NNN-prompt.txt`.
3. Evaluate dev with the workspace runner and resolved config:

   ```sh
   tmp/autotune/extract/bin/eval-extract run \
     -prompt tmp/autotune/extract/candidates/NNN-prompt.txt \
     -gold eval/extract/gold \
     -config tmp/autotune/extract/config.json \
     -out tmp/autotune/extract/candidates/NNN-scorecard.json \
     -split dev \
     -repeat 3
   ```

4. Compare it with `tmp/autotune/extract/best/scorecard.json`. Only stdout
   exactly `accept` with exit 0 is acceptance; `reject` is a normal rejection;
   any other result fails the turn without promotion or a history verdict.
5. On acceptance, copy the candidate prompt and scorecard over the files in
   `best/`, then copy the accepted prompt to
   `autotune/extract/prompt.txt`. On rejection, leave all incumbent files
   untouched.
6. Append one history line with the attempt number, candidate path, candidate
   and incumbent composites, and verdict. Report `CONTINUE`.

Do not run final evidence work after the attempt. The supervising driver runs
holdout once, writes the summary, and prints the final proposal diff whenever
ralph exits.
