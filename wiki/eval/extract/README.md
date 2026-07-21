# Extract prompt evaluation and improvement

Run these commands from the `wiki/` repository root. The evaluation runner uses
the model and provider pinned in `eval/extract/config.json`; the separate
improver model is selected when launching ralph.

## Build the runner

```sh
go build -o bin/eval-extract ./cmd/eval-extract
```

## Produce a baseline

The committed production prompt is evaluated three times on the dev split so
the scorecard includes the run composites and an empirical epsilon:

```sh
mkdir -p tmp/autotune/extract
bin/eval-extract run \
  -prompt eval/extract/prompt.txt \
  -gold eval/extract/gold \
  -config eval/extract/config.json \
  -out tmp/autotune/extract/baseline.json \
  -split dev \
  -repeat 3
```

The loop also performs this bootstrap itself when its disposable workspace is
empty, so the manual baseline command is useful for inspection but is not a
prerequisite.

## Launch the unattended loop

Choose the agent harness and improver model at launch; they are intentionally
not pinned in this repository. For example:

```sh
ralph --harness <harness> -c model=<improver-model> eval/extract/improve.md
```

The essential invocation is `ralph eval/extract/improve.md`. Each ralph turn is
fresh and uses scorecard evidence left in `tmp/autotune/extract/`. The loop
stops after five consecutive rejected candidates, or when the ralph budget
ends. Only at the five-reject stop does it score the final winner once on the
holdout split and write its final summary.

## Read the workspace

- `baseline.json` is the repeated scorecard for the committed prompt.
- `best/prompt.txt` and `best/scorecard.json` are the current dev incumbent.
- `candidates/NNN-*` preserve every attempted prompt and its dev scorecard.
- `history.md` has one candidate, composite, and verdict per line.
- `holdout-scorecard.json` and `summary.md` exist only after normal completion;
  the summary calls out a dev winner that failed to generalize as overfit.

Everything in `tmp/autotune/extract/` is disposable and gitignored. The loop is
forbidden from writing anywhere else, including `eval/`, `internal/`, and
`cmd/`, so a completed run must leave the committed tree clean.

## Reproduce, then adopt manually

Do not copy a loop winner into production merely because the summary looks
good. From a clean checkout, first verify that the repository is unchanged:

```sh
git status --short
```

Then reproduce the claimed winner on dev with the committed config and three
runs:

```sh
bin/eval-extract run \
  -prompt tmp/autotune/extract/best/prompt.txt \
  -gold eval/extract/gold \
  -config eval/extract/config.json \
  -out tmp/autotune/extract/reproduced-scorecard.json \
  -split dev \
  -repeat 3
```

Reject the tuning run if the clean-checkout score does not reproduce within
the baseline epsilon. If it does reproduce, review the prompt and both dev and
holdout evidence. Adoption is still a deliberate operator action through the
normal specification and commit workflow: only then copy
`best/prompt.txt` over `eval/extract/prompt.txt`, review the diff, and commit it.
The improvement loop itself never performs that copy.

## Grow the gold corpus

Add cases only after operator review. Capture real extraction misses or
spurious claims, remove sensitive material, write the expected result, choose
the dev or holdout split deliberately, and review the case before committing
it under `eval/extract/gold/`. Never let the improvement loop generate or edit
gold cases: an unreviewed or prompt-aware corpus would make its score
untrustworthy. Re-run the repeated baseline after the reviewed corpus changes.
