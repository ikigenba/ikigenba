# Analysis prompt autotuning

The `autotune` development command owns the run from baseline through final
evidence. Run it from the `wiki/` root; do not launch the ralph prompt directly.

## Launch

```sh
go build -o bin/autotune ./cmd/autotune
bin/autotune analysis -- --max-time 2h
```

Override only the analysis eval block with `-c key=value` arguments before
`--`; arguments after `--` configure ralph. Resume a compatible workspace with
`bin/autotune analysis --resume -- <ralph-args>`, or use `--from <file>` on a
fresh run.

The reference config mirrors production's model and token limit. Production's
`low` reasoning-effort setting is intentionally absent because the workbench
config supports the independent `thinking` knob, not effort levels. Tuning
uses the resolved workspace config and does not inherit omitted reference keys.

## Evidence and adoption

State lives in `tmp/autotune/analysis/`; the visible incumbent is
`autotune/analysis/prompt.txt`. The driver prints diffs against the committed
`eval/analysis/prompt.txt`, evaluates dev candidates, and runs holdout once for
a winning prompt. Reproduce a winner with:

```sh
tmp/autotune/analysis/bin/eval-analysis run \
  -prompt tmp/autotune/analysis/best/prompt.txt \
  -gold eval/analysis/gold \
  -config tmp/autotune/analysis/config.json \
  -out tmp/autotune/analysis/reproduced-scorecard.json \
  -split dev -repeat 3
```

Review the three list scores (`sub_queries`, `keywords`, and `aliases`), dev and
holdout evidence, overfit verdict, and prompt diff. Adoption is manual through
the normal specification and commit workflow; the driver never edits the
committed prompt.

Add gold cases only after operator review. Questions must be grounded in the
matching split of `eval/extract/gold/`; never move a holdout case to dev or let
the improver inspect or edit gold.
