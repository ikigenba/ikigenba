# sites — the installed build loop

This folder holds the **gather → build → verify** loop that builds sites one
phase at a time, unattended. It describes the loop **as installed here** — the
prompts beside this file are the loop; nothing else documents its mechanics.

## Running it

```
cd sites && ./project/loops/run
```

`project/loops/run` is an executable wrapper whose whole body is:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the **service root** (`sites/`) and re-invokes each prompt in a
**fresh, isolated context**, cycling `gather → build → verify → gather → …`. Every
workspace path in the prompts is therefore service-root-relative (`project/…`).

## The status contract

Each turn ends with a `status` and a one-sentence `message`. The harness supplies
the `{status, message}` schema out of band and reads back only the turn's **final**
message.

| status | terminal? | meaning |
|---|---|---|
| `CONTINUE` | no | a progress message streamed *before* the final message; never advances the loop |
| `NEXT` | yes | this turn is done — advance to the next prompt (verify wraps to gather) |
| `DONE` | yes | the whole job is complete — the loop stops |

**Only `gather` ever reports `DONE`**, on exactly two conditions: `project/loops/blocked.md`
exists, or the `⬜` grep finds no pending phase. `build` and `verify` always end on
`NEXT` — finishing a phase completely, green suite and all, is still `NEXT`.

## What each step reads, writes, and commits

| step | reads | writes | commits / deletes |
|---|---|---|---|
| **gather** | `blocked.md` (existence), `plan/STATUS.md`, one `plan/phase-NN.md`, `design/INDEX.md`, the realized `design/DNN.md`, optionally `product/README.md` | the brief's **contract region** (only when authoring a fresh brief) | nothing |
| **build** | `project/loops/brief.md` only | source + co-located `*_test.go` files | commits its increment; never touches `STATUS.md`, the brief, or a phase file |
| **verify** | the brief (both regions), the tree's real state via commands | the brief's **feedback region**, or `blocked.md` | on pass: deletes the phase's `STATUS.md` line + `git rm`s its `phase-NN.md`, commits, deletes the brief |

## The brief lifecycle

`project/loops/brief.md` is the seam that keeps `build`'s context scoped to one
phase. It is **never committed** (gitignored at the repo root) and describes
exactly one phase at a time.

1. `gather` authors it once, when a phase first becomes the active `⬜` phase —
   copying in the realized Decision's full design prose (Verification list
   omitted), the phase's own ids with their full requirement text, files to
   touch, dependency interface signatures, and the done bar; the feedback region
   is left empty.
2. While that phase stays `⬜`, `gather` **no-ops** on it: it reads the brief's
   `# Brief — Phase NN` header, sees the same phase, opens no big doc, and hands
   off. The contract and any accumulated feedback survive untouched.
3. `build` reads the whole brief — contract **and** feedback — and closes the
   listed gaps first.
4. `verify` either **passes** the phase (retire + delete the brief) or
   **overwrites** the feedback region with only the currently-open gaps, each
   tied to an `R-id` and grounded in the exact failing command and output.

Regions are single-writer: `gather` owns the contract region, `verify` owns the
feedback region, `build` writes neither.

## The brief schema

```
# Brief — Phase NN

## Contract
<!-- gather-owned: written once when this phase became active. verify never writes here. -->

**Phase:** NN — <one-line objective>
**Realizes:** D<n>[, D<m>]
**Decision files:** project/design/DNN.md[, project/design/DMM.md]

### Design prose
<Decision statement + shape/signatures + rejected alternatives, verbatim per
realized Decision — the Verification list OMITTED.>

### Ids to cover
R-XXXX-XXXX — <full requirement text, verbatim from the Decision's Verification list>
R-YYYY-YYYY — <full requirement text …>
<!-- the ONLY lines in this file beginning with `R-` at column 0 -->
<!-- structural phase → the single line:  (none — structural phase) -->

### Files to touch
- <path> — <what changes>

### Dependency interface signatures
```go
// public signatures of the packages this phase consumes
```

### Done bar
<deterministic exit conditions: the green suite AND each id covered by a
co-located, genuinely-asserting `// R-id` test that runs with no SKIP.>

## Verify feedback
_(empty — no verify attempt yet)_
```

`grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md` yields exactly this
phase's id set; feedback gap lines are bulleted and never miscounted.

## The gate sites' loop enforces

From design's *Conventions*, "the suite is green" means all four of these succeed
with zero failures, from `sites/`:

```
go build ./...
go vet ./...
gofmt -l .          # must print nothing
go test ./...
```

Green **hard-requires a `google-chrome` binary on `PATH`** — the browser-wiring
test runs for real; no Chrome means the suite is **red, never skipped**.

Per `root project/design/D23.md` sites has a **hermetic** and a **composed** layer
only — no live layer, no tree-local manual runbook. `verify` therefore also runs a
**skip ban**: `grep -rnE 't\.Skip(f|Now)?\(' --include='*_test.go' --exclude-dir=project .`
must print nothing, and any hit is an open gap.

The **global coverage ratchet** catches a rewrite silently dropping a
previously-covered id: the design id set minus (test tags ∪ pending-phase ids)
must be empty. The design id extraction pipes through `grep -v 'R-XXXX-XXXX'` so
the docs' literal format *placeholder* never surfaces as a phantom uncovered id.

## The stall and blocked ladder

`verify` keeps cross-cycle memory in the feedback region: an attempt counter, the
observed build commit (diagnostic only — a new commit is **never** progress), and
a stall streak. Progress means this cycle's open-gap id set is a **strict subset**
of the previous one.

- **3 consecutive no-progress attempts** → **trajectory reset**: log
  `Phase NN STALLED` to `~/.ralph/verify.log`, delete the brief, leave `⬜`. The
  next `gather` rebuilds the contract fresh from spec.
- **A second stall on the same phase** → **blocked**: a rebuilt contract already
  failed, so the phase's *done bar* is the suspect. `verify` writes
  `project/loops/blocked.md` with the phase, attempt count, unsatisfied ids, and
  the exact command and observed output that will not go green; logs
  `Phase NN BLOCKED`; deletes the brief; leaves `⬜`. The next `gather` sees the
  file and reports `DONE`.

**Operator response to a `blocked.md`:** read the recorded command and output, fix
the phase's done bar in `project/` (the loop cannot — `project/` is read-only to
it), delete `project/loops/blocked.md`, and restart the loop.

## Why it converges

`verify` can neither halt the run nor advance a phase on a gap, so an incomplete
phase simply stays `⬜` and is re-attacked next cycle — now with grounded,
command-level feedback in front of `build`, and without `gather` re-reading the
big docs (it no-ops on the in-flight brief). Every gate is a deterministic,
mechanically-evaluable predicate whose passing state is reachable, so a phase
either converges or trips the stall ladder into a written diagnosis. The only
ends are `gather → DONE` (no `⬜` left, or a blocked phase) plus ralph's own
budget rails.
