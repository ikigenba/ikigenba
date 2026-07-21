# Extract prompt autotuning

The `autotune` development command owns the complete run from baseline through
final evidence. Run it from the `wiki/` root; do not launch the ralph prompt
directly.

## Launch

Build and launch with the production evaluation pins:

```sh
go build -o bin/autotune ./cmd/autotune
bin/autotune extract -- --max-time 2h
```

The step model evaluates candidate extraction prompts. Override only its eval
block with agentrepl-style flags before `--`:

```sh
bin/autotune extract -c provider=anthropic -c model=<step-model> -- \
  --harness agentkit -c model=<improver-model> --max-time 2h
```

Arguments after `--` pass verbatim to ralph and choose the harness, improver
model, and budgets. The two models have separate jobs. Resume a compatible
workspace with `bin/autotune extract --resume -- <ralph-args>`; the driver
rejects a step-model config mismatch. Use `--from <file>` on a fresh run to
start from another prompt.

## Workspaces and diffs

`tmp/autotune/extract/config.json` is the resolved config stamp. The same file
governs baseline, every candidate, and holdout. `baseline.json` stores the
initial repeated dev score; `best/` is the current incumbent; `candidates/`
and `history.md` preserve attempts. Finalization adds
`holdout-scorecard.json` and `summary.md` once when there is a winner.

The visible incumbent is `autotune/extract/prompt.txt`. The driver prints a
unified diff against `eval/extract/prompt.txt` whenever an acceptance changes
it and prints the proposal again at exit. Both workspaces are ignored, so a
run leaves committed paths clean. If nothing improves, the driver reports the
attempt count and evidence path and does not run holdout.

## Reproduce, review, adopt

First verify the checkout is clean. Then reproduce the claimed winner using
the workspace config and three dev repeats:

```sh
git status --short
tmp/autotune/extract/bin/eval-extract run \
  -prompt tmp/autotune/extract/best/prompt.txt \
  -gold eval/extract/gold \
  -config tmp/autotune/extract/config.json \
  -out tmp/autotune/extract/reproduced-scorecard.json \
  -split dev \
  -repeat 3
```

Reject the run if its dev score does not reproduce within the baseline
epsilon. Otherwise review the prompt, dev evidence, holdout evidence, overfit
verdict, and printed diff. Adoption is manual: copy the reviewed winner over
`eval/extract/prompt.txt` through the normal specification and commit workflow.
The driver and loop never change that committed file.

## Grow the gold corpus

Add cases only after operator review. Capture real misses or spurious claims,
remove sensitive material, specify the expected extraction, deliberately
choose dev or holdout, and commit the reviewed case under
`eval/extract/gold/`. Never let the improver generate or edit gold cases. Run a
fresh baseline after corpus changes.
