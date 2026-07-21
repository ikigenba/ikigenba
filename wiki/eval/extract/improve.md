# Extract prompt improvement loop

You are one unattended turn of a ralph prompt-improvement loop. Run from the
`wiki/` repository root. Each turn starts with no conversational memory; the
only loop state is under `tmp/autotune/extract/`.

## Non-negotiable write boundary

Write **only** beneath `tmp/autotune/extract/`. Never create, edit, delete, or
move anything under `eval/`, `internal/`, or `cmd/`, and do not change any
other repository path. In particular, never adopt a winner into
`eval/extract/prompt.txt` and never edit this prompt. Reading committed files
and running the evaluator are allowed. Build the evaluator with:

```sh
mkdir -p tmp/autotune/extract/bin
go build -o tmp/autotune/extract/bin/eval-extract ./cmd/eval-extract
```

All generated binaries, prompts, scorecards, histories, and summaries must
remain inside the workspace so `git status --short` stays empty.

## Workspace

Maintain this layout:

```text
tmp/autotune/extract/
  bin/eval-extract
  baseline.json
  best/prompt.txt
  best/scorecard.json
  candidates/NNN-prompt.txt
  candidates/NNN-scorecard.json
  history.md
  holdout-scorecard.json     # final turn only
  summary.md                 # final turn only
```

`NNN` is a zero-padded, monotonically increasing attempt number. Each
non-final turn creates exactly one new candidate prompt and its scorecard.
`history.md` contains exactly one concise line per attempted candidate,
including its path, composite score, and `accept` or `reject` verdict.

## Bootstrap an empty workspace

If no incumbent exists, create the workspace, build the evaluator as above,
and establish the committed prompt's dev baseline:

```sh
tmp/autotune/extract/bin/eval-extract run \
  -prompt eval/extract/prompt.txt \
  -gold eval/extract/gold \
  -config eval/extract/config.json \
  -out tmp/autotune/extract/baseline.json \
  -split dev \
  -repeat 3
mkdir -p tmp/autotune/extract/best tmp/autotune/extract/candidates
cp eval/extract/prompt.txt tmp/autotune/extract/best/prompt.txt
cp tmp/autotune/extract/baseline.json tmp/autotune/extract/best/scorecard.json
: > tmp/autotune/extract/history.md
```

The repeated baseline records `RunComposites` and the acceptance `Epsilon`.
After bootstrapping, continue with the one-turn procedure below.

## Start-of-turn stop check

Read `history.md` before proposing anything. If its last five attempt lines are
all rejects, do not create another candidate. Instead, evaluate only the final
incumbent on holdout, exactly once:

```sh
tmp/autotune/extract/bin/eval-extract run \
  -prompt tmp/autotune/extract/best/prompt.txt \
  -gold eval/extract/gold \
  -config eval/extract/config.json \
  -out tmp/autotune/extract/holdout-scorecard.json \
  -split holdout
```

Write `summary.md` with the winning dev score, baseline dev score, epsilon,
holdout score, attempt count, and whether the apparent dev gain generalized.
The holdout result is reporting evidence only: do not replace the committed
prompt or otherwise adopt the winner. If a claimed winner beats baseline on
dev but not on holdout, explicitly report it as overfit. Then report `DONE`.

If `holdout-scorecard.json` and `summary.md` already exist, do not run holdout
again; read them and report `DONE` immediately.

## One improvement turn

When the stop rule has not fired:

1. Read `history.md`, `best/prompt.txt`, and `best/scorecard.json`. Inspect the
   incumbent's per-case misses and spurious claims, then form one focused
   revision based on that evidence.
2. Write exactly one new `candidates/NNN-prompt.txt`. Keep the extraction task
   intact; change only the instruction text suggested by the scorecard.
3. Run that candidate on the dev split. Use three repeats so every promoted
   incumbent scorecard continues to carry an empirically measured epsilon:

   ```sh
   tmp/autotune/extract/bin/eval-extract run \
     -prompt tmp/autotune/extract/candidates/NNN-prompt.txt \
     -gold eval/extract/gold \
     -config eval/extract/config.json \
     -out tmp/autotune/extract/candidates/NNN-scorecard.json \
     -split dev \
     -repeat 3
   ```

4. Compare it with the incumbent. Capture stdout because exit status 1 can
   mean either a normal rejection or an error:

   ```sh
   tmp/autotune/extract/bin/eval-extract compare \
     -candidate tmp/autotune/extract/candidates/NNN-scorecard.json \
     -baseline tmp/autotune/extract/best/scorecard.json
   ```

   Only stdout exactly equal to `accept` with exit status 0 is acceptance.
   Stdout `reject` is a normal rejection; any other output or evaluator error
   fails this turn without changing the incumbent or appending a verdict.
5. On acceptance, copy the candidate prompt and scorecard over the two files
   under `best/`. On rejection, leave both incumbent files untouched.
6. Append one line to `history.md` containing `NNN`, candidate path, candidate
   composite, incumbent composite used for comparison, and the verdict. End
   the turn without running holdout, even if this line is the fifth consecutive
   rejection; the next fresh turn performs the final stop procedure.

Never inspect holdout cases or run the holdout split while proposing or judging
candidates. The operator's ralph time, token, or spend budget may stop the loop
before the five-reject rule; do not work around that budget.

