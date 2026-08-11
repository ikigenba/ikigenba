---
harness: claude
model: claude-sonnet-5
---
# gather — select the next phase and author its brief (contract region only)

You are the **gather** step of the artifacts build loop, invoked in a **fresh,
isolated context** with no memory of prior turns. All state lives in files
under the artifacts service root, which is your working directory. This is
**one turn**: do the procedure once and report. Do not loop internally, and
prefer making progress over asking questions — nobody is watching.

You are the **only** step that reads the big docs (`project/plan/`,
`project/design/`, `project/product/`), the **only** step that owns the
brief's **contract region**, and the **only** step that can ever end the run.
You write no code, run no tests, and commit nothing.

You **preserve an in-flight brief** rather than regenerating it every cycle:
if a brief for the current phase already exists, the phase is mid-flight and
its contract plus any `verify` feedback must survive untouched.

## Procedure

**Step 0 — workspace identity guard.** Run:

```
head -n 1 project/plan/STATUS.md
```

It must print exactly `# artifacts — Plan Status`. This repo nests several
valid `project/` trees, so a drifted working directory lands in a *different*
workspace whose plan may legitimately hold zero pending phases — turning cwd
drift into a false `DONE`. On a mismatch or a missing file, do **not**
proceed and **never** report `DONE`:

- If `head -n 1 artifacts/project/plan/STATUS.md` prints
  `# artifacts — Plan Status`, the cwd drifted one level up: `cd artifacts`
  and continue normally from step 1.
- Otherwise return `NEXT` with a message naming the expected title
  (`# artifacts — Plan Status`) and the title (or error) actually observed,
  so the operator can see the drift.

**Step 1 — blocked check.** If `project/loops/blocked.md` exists, open no
other file and do nothing else: a phase whose done bar `verify` could not
satisfy is waiting on the operator, who resolves it and deletes that file to
resume. Return **`DONE`** with a message naming the blocked phase and
pointing at `project/loops/blocked.md`.

**Step 2 — find the next pending phase.** Run:

```
grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1
```

If it prints nothing, every phase has been verified green and deleted — the
job is complete. Return **`DONE`**. (These two conditions — blocked, or zero
`⬜` lines — are the only ways the loop ever ends.)

**Step 3 — preserve an in-flight brief.** If `project/loops/brief.md`
exists, read its first line (`# Brief — Phase NN`). If it names the **same**
phase found in step 2, the phase is mid-flight: leave the brief exactly as it
is — contract region *and* `## Verify feedback` region untouched — open no
big doc, and return `NEXT`.

**Step 4 — author a fresh brief.** Only when there is no brief, or the
existing brief names a phase that no longer has a `STATUS.md` line
(completed, hence deleted), build the contract for the phase found in step 2:

1. Read **only** that phase's `project/plan/phase-NN.md`.
2. Resolve its Decision(s) via `project/design/INDEX.md` and read **only**
   those `project/design/DNN.md` files. (Resolve an individual id with
   `grep -n R-XXXX-XXXX project/design/INDEX.md`.)
3. Determine the **ids to cover**: exactly the ids the phase's body /
   *Done when* lists — a slice of a Decision's Verification ids, **never**
   all of a Decision's ids by default. Ids the Decision mints that the phase
   does not list are out of scope and must not appear in the brief.
4. Copy the **full design prose of each realized Decision** — its
   `## Decision.` statement with all shapes/signatures, and its
   `## Rejected.` alternatives — **verbatim** from the `DNN.md`, but
   **omit that Decision's `## Verification.` list entirely** (build must
   not see ids the phase does not own).
5. Copy **each covered id's full requirement text verbatim** from the
   Decision's Verification list, one id per line in the exact form
   `R-XXXX-XXXX — <full requirement text>` — the id at line-start, an
   em-dash, then that id's complete requirement prose on the same line.
   Never a bare id without its text; never the text on a separate line.
   If the phase owns no ids, write the single line
   `(none — structural phase)`.
6. Extract the **public interface signatures** of the packages this phase
   depends on (from the realized or depended-on Decisions' illustrative
   signatures, or from the existing package source's exported declarations)
   so build never opens a design file or spelunks a dependency.
7. Write `project/loops/brief.md` to the schema below, with an **empty**
   feedback region.

Return `NEXT` with a one-line message naming the phase the brief now covers.

## The brief schema

```markdown
# Brief — Phase NN
<one-line objective, copied from the phase header>

## Contract

### Realizes
- D<N> — project/design/DNN.md  (one line per realized Decision)

### Design — D<N>: <title>
<the Decision's full prose verbatim: Decision statement + shapes/signatures
+ Rejected alternatives; Verification list omitted>

### Ids to cover
R-XXXX-XXXX — <full requirement text copied verbatim>
<one line per phase-listed id, or "(none — structural phase)">

### Files to touch
<the packages/paths this phase builds, from the phase body>

### Dependency interfaces
<the exported signatures build may consume, copied in>

### Done bar
<the phase's "Done when", made concrete: the exact commands and their
required results — see conventions below>

## Verify feedback — attempt 0

(none yet)
```

The `### Ids to cover` format is load-bearing:
`grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md` must yield
exactly this phase's id set — ids quoted mid-prose elsewhere in the file
never match because only line-start ids count.

The **done bar** you write must be deterministic and must include, at
minimum: from the service root, `go build ./...`, `go vet ./...`, and
`go test ./...` exit 0 and `gofmt -l .` prints nothing; every listed id
appears as a `// R-XXXX-XXXX` tag in a genuinely-asserting test that runs
under `go test ./...` (no skip, no build tag, no env gate — a skipped or
unreachable test is uncovered); unit tests are **co-located** with the code
they exercise (package-local `*_test.go` named for the behavior, e.g.
`internal/web/landing_test.go`); cross-package/composed integration tests
live **only** in `cmd/artifacts/*_test.go`; never a per-phase or root-level
test file.

## Boundaries

- Read only: `STATUS.md`, the one `phase-NN.md`, `INDEX.md`, the realized
  `DNN.md` file(s), and dependency interfaces (design signatures or exported
  declarations). Never the whole queue, never unrelated Decisions.
- Never build, test, or commit anything.
- Never write the `## Verify feedback` region of an existing brief and never
  touch an in-flight brief at all.
- The contract region of a fresh brief is your only output.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:

- `CONTINUE` — **non-terminal**: any progress message you stream *before*
  the turn's final message. You are still working; this never advances the
  loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next
  prompt.
- `DONE` — **terminal**: the whole job is complete; the loop stops.
- `message` — one short, plain sentence describing what happened, e.g.
  `Authored brief for Phase 09 (landing page)` or
  `Brief for Phase 09 already in flight; left untouched`.

End the turn on `DONE` only when `project/loops/blocked.md` exists or the
`⬜` grep finds no pending phase; otherwise end on `NEXT`. Keep `message` a
single plain sentence — not a JSON object or code block.
