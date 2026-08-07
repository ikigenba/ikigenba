# sites/project/loops — the installed build loop

These are the generated build-loop prompts, not spec artifacts. The spec lives in
`product/`, `research/`, `design/`, and `plan/`; these prompts are generated *from*
the finished spec by the `create-gather-build-verify-prompts` workflow and describe
the loop topology installed here. `project/README.md` (the workspace map) only
points here; the loop mechanics live only in this file.

## Running it

```
./run
```

is the operator wrapper; it wraps exactly:

```
ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the **service root** (`sites/`), re-invoking each prompt in a
**fresh, isolated context** every turn. It cycles `gather → build → verify →
gather → …`, building sites one phase at a time, until the queue is empty (or a
ralph budget rail trips).

## The status contract

Each turn's **final** message carries a terminal `status`; `ralph` reads only that
last message and acts on it:

- **`NEXT`** — terminal: advance to the next prompt (wrapping `verify → gather`).
  **build and verify always report `NEXT`.**
- **`DONE`** — terminal: stop the loop. **Only `gather` ever reports `DONE`**, and
  only when `STATUS.md` shows no `⬜` phase line left (the queue is empty) or a
  `project/loops/blocked.md` file is present (a phase awaiting the operator).
- **`CONTINUE`** — **non-terminal**: the status a streaming model tags its
  mid-turn progress messages with (codex/gpt coerces *every* streamed message into
  the schema). It never terminates a turn and never drives the loop.

## Per-step reads / writes / commits

| step | reads | writes | commits | deletes the phase? |
|---|---|---|---|---|
| **gather** | `blocked.md` (existence), `STATUS.md`, one `phase-NN.md`, `INDEX.md`, the realized `DNN.md`, (opt.) `product/README.md` | authors `brief.md` **contract region** (or no-ops on an in-flight brief) | no | no |
| **build** | **only** `brief.md` (contract + feedback) | source + co-located `// R-id` tests | yes (the code increment) | no |
| **verify** | `brief.md` + runs the suite; re-derives truth independently | pass: deletes `brief.md`; gap: overwrites `brief.md` **feedback region**; blocked: writes `blocked.md` | yes (only the `STATUS.md` line + `phase-NN.md` deletion, on pass) | **yes — the only step that does** |

## The brief lifecycle

`project/loops/brief.md` is the ephemeral, single-phase seam — **never committed**
(gitignored), **phase-scoped, not per-cycle**:

- **gather** authors the contract region **once**, when a phase first becomes the
  active `⬜` phase. While that phase stays `⬜`, gather **no-ops** on the in-flight
  brief (it checks the `# Brief — Phase NN` header) and does not re-read the big
  docs — so the docs are read once per phase, not once per cycle.
- **build** consumes the brief (contract + any feedback) and never opens a big doc.
- **verify** either **passes** the phase (delete its `STATUS.md` line and
  `phase-NN.md`, delete the brief), records **gaps** (overwrite the feedback
  region, keep the brief), or — after a second stall on the same phase — declares
  it **blocked** (write `project/loops/blocked.md`, delete the brief). The brief
  therefore **persists across cycles** until the phase passes, is stall-reset, or
  is blocked.

## The stall and blocked ladder

- **Progress** means the current open-gap id set is a strict subset of the prior
  attempt's — some gap closed. A new build commit alone is never progress.
- **No progress** for **3** consecutive attempts on the same gaps → a **stall
  reset**: verify discards the brief, logs
  `<date> Phase NN STALLED after N attempts: <gap ids>` to `~/.ralph/verify.log`,
  and leaves the phase `⬜`. The next `gather` rebuilds the brief fresh from spec —
  a new contract, a clean slate.
- A **second** stall on the **same** phase (verify finds an earlier `STALLED` line
  for it in `~/.ralph/verify.log`) means a rebuilt contract already failed to
  converge — the phase's done bar itself is the suspect, not the trajectory.
  Verify writes `project/loops/blocked.md` naming the phase, the attempt count,
  the unsatisfied ids, and the exact failing command/output, then logs
  `<date> Phase NN BLOCKED after N attempts: <gap ids>` and leaves the phase `⬜`.
  The next `gather` sees `blocked.md` and reports `DONE`, stopping the run.
- **Operator recovery:** read the diagnosis in `project/loops/blocked.md`, fix the
  phase's done bar (or its Decision) in `project/` via `$open-spec`/`$seal-spec`,
  delete `blocked.md`, and restart the loop with `./run`.

## Why it converges

`verify` can neither halt the loop nor advance a phase on a gap — an incomplete
phase just stays `⬜` and is re-attacked next cycle, now with verify's grounded,
command-tied feedback in front of `build`. The persisted feedback also gives
verify cross-cycle memory: it distinguishes *slow progress* (the open-gap id set
shrinking) from a *true stall* (the same gap ids unsatisfied for 3 consecutive
no-progress attempts). The stall/blocked ladder above turns a defective bar into a
handful of attempts and a written diagnosis instead of an infinite spin. The
**only** exits are `gather → DONE`: zero `⬜` phase lines left (every phase
verified green and deleted), or a blocked phase awaiting the operator — plus a
`ralph` budget rail.

## The `project/loops/brief.md` schema

Two regions, one writer each (they never clobber each other):

```
# Brief — Phase NN

## Contract
<!-- gather-owned: written once when the phase becomes active; verify never writes here -->

**Phase:** NN — <one-line objective>
**Realizes:** D<n>[, D<m>]
**Decision files:** project/design/DNN.md[, …]

### Design prose
<Decision statement + shape/signatures + rejected alternatives, verbatim per
realized Decision — that Decision's Verification list OMITTED>

### Ids to cover
R-XXXX-XXXX — <full requirement text, verbatim from the Verification list>
…                     # the ONLY lines beginning with `R-` at column 0
                      # structural phase → the single line: (none — structural phase)

### Files to touch
- <path> — <what changes>

### Dependency interface signatures
```go
// public signatures of the packages this phase consumes, copied in
```

### Done bar
<deterministic exit conditions: green suite + each id covered by a co-located,
genuinely-asserting `// R-id` test that runs (no SKIP); structural phase names its
grep/smoke>

## Verify feedback
<!-- verify-owned: gather writes it empty; verify overwrites (never appends) with only the currently-open gaps -->
## Verify feedback — attempt <N>
build-commit: <sha>
stall-streak: <count>

- R-XXXX-XXXX — <exact failing command> → <observed output> (file:line)
```

## The green bar

"The suite is green" (from design's *Conventions*), run from `sites/`:

```
cd sites && go build ./...
cd sites && go vet ./...
cd sites && gofmt -l .      # prints nothing
cd sites && go test ./...
```

succeed with zero failures. Green **includes** the D23 headless-Chrome
browser-wiring test and therefore hard-requires a `google-chrome` binary on
`PATH`; no Chrome makes the suite **red**, never skipped (one browser-*launch*
retry is allowed; scenario assertions are never retried). Coverage of a
requirement id means a genuinely-asserting `// R-XXXX-XXXX`-tagged test that
actually runs under `go test ./...` (no SKIP, no unreachable gate), **co-located**
with the code it exercises in a package-local `*_test.go`, or — for the
cross-package landing-render / goja-logic / browser-wiring / nginx-fragment
tests — in `cmd/sites/*_test.go`. Never a per-phase or root-level test file.

Next-phase lookup: `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`.
