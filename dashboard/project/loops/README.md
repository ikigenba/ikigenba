# dashboard/project/loops — the installed build loop

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

`ralph` runs from the **service root** (`dashboard/`), re-invoking each prompt in a
**fresh, isolated context** every turn. It cycles `gather → build → verify →
gather → …`, building the dashboard one phase at a time, until the queue is
empty, a phase gets blocked awaiting the operator, or a ralph budget rail trips.

## The status contract

Each turn's **final** message carries a terminal `status`; `ralph` reads only that
last message and acts on it:

- **`NEXT`** — terminal: advance to the next prompt (wrapping `verify → gather`).
  **build and verify always report `NEXT`.**
- **`DONE`** — terminal: stop the loop. **Only `gather` ever reports `DONE`**, and
  only when `project/loops/blocked.md` exists or `STATUS.md` shows no `⬜` phase
  line left (the queue is empty).
- **`CONTINUE`** — **non-terminal**: the status a streaming model tags its
  mid-turn progress messages with (codex/gpt coerces *every* streamed message into
  the schema). It never terminates a turn and never drives the loop.

## Per-step reads / writes / commits

| step | reads | writes | commits | deletes the phase? |
|---|---|---|---|---|
| **gather** | `blocked.md` (existence check), `STATUS.md`, one `phase-NN.md`, `INDEX.md`, the realized `DNN.md`, (opt.) `product/README.md` | authors `brief.md` **contract region** (or no-ops on an in-flight brief) | no | no |
| **build** | **only** `brief.md` (contract + feedback) | source + co-located `// R-id` tests | yes (the code increment) | no |
| **verify** | `brief.md` + runs the suite + the global ratchet; re-derives truth independently | pass: deletes `brief.md`; gap: overwrites `brief.md` **feedback region**; stall/second-stall: deletes `brief.md`, may write `blocked.md` | yes (only the `STATUS.md` line + `phase-NN.md` deletion, on pass) | **yes — the only step that does** |

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
  region, keep the brief), or performs a **stall reset / blocked escalation**
  (delete the brief, leave `⬜`). The brief therefore **persists across cycles**
  until the phase passes or is stall-reset.

## The stall and blocked ladder

`verify` tracks, per phase, whether each cycle's open-gap id set is a **strict
subset** of the prior cycle's (progress) or not (no progress; a new build commit
alone is never progress). Three consecutive no-progress attempts on the same
phase trigger a **trajectory reset**: `verify` discards the brief, logs
`Phase NN STALLED after N attempts: <gap ids>` to `~/.ralph/verify.log`, and
leaves the phase `⬜` — the next `gather` rebuilds the contract fresh from spec,
in case the accumulated brief itself had drifted.

If the **same phase stalls a second time**, a rebuilt contract has already been
tried and did not help — the fault is the phase's done bar, not the trajectory,
and no amount of rebuilding fixes that. On this second stall `verify` instead
**escalates**: it writes `project/loops/blocked.md` naming the phase, the total
attempts, the still-unsatisfied ids, and the exact command + observed output
that will not go green, then logs
`Phase NN BLOCKED after N attempts: <gap ids>` and leaves the phase `⬜`. The
next `gather` sees `blocked.md` and reports `DONE`, stopping the run.

**Operator response to a `blocked.md`:** read the recorded diagnosis (the
command and output that would not go green), fix that phase's done bar or
design in `project/` (the loop cannot — `project/` is read-only to it), delete
`project/loops/blocked.md`, and restart `./run`.

## Why it converges

`verify` can neither halt the loop nor advance a phase on a gap — an incomplete
phase just stays `⬜` and is re-attacked next cycle, now with verify's grounded,
command-tied feedback in front of `build`. The persisted feedback also gives
verify cross-cycle memory: it distinguishes *progress* (the open-gap id set
shrinking) from a *stall* (no gap closed for 3 consecutive attempts), and a
second stall on the same phase from a merely slow one. The **only** exits are
`gather → DONE`: either zero `⬜` phase lines (every phase verified green) or a
blocked phase awaiting the operator — plus the ralph budget rails.

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

The green suite (from design *Conventions*), run from `dashboard/`:
`go build ./...`, `go vet ./...`, `gofmt -l .` (empty), `go test ./...`.
Next-phase lookup: `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`.
The global coverage ratchet verify also runs every cycle:

```
comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | sort -u) \
         <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
               <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
```
