# wiki — the build loop (as installed)

This is the author-facing overview of the **`gather → build → verify`** build
loop installed under `project/loops/`, kept beside the prompts it describes so it
can never drift from them. `project/README.md` (the workspace map) points here;
the spec shapes it does not restate live in `project/design/` and `project/plan/`.

## Running it

The loop runs from the **service root** (`wiki/`), which is `ralph`'s working
directory, so every path the prompts touch is service-root-relative (`project/…`).
Start it with the wrapper:

```
project/loops/run
```

which is exactly:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` cycles the three prompts in **fresh, isolated contexts** —
`gather → build → verify → gather → …` — carrying no memory between turns; all
state lives in files under the service root.

## The status contract

Each turn ends by reporting a `status` and a one-sentence `message`. `ralph` reads
only the **last** message of a turn and acts on its status:

- **`NEXT`** — *terminal*: advance to the next prompt (wrapping `verify → gather`).
  **build and verify always report `NEXT`.**
- **`DONE`** — *terminal*: stop the loop. **Only `gather` ever reports it**, either
  because no `⬜` phase line remains in `STATUS.md`, or because
  `project/loops/blocked.md` exists (a phase awaiting the operator).
- **`CONTINUE`** — *non-terminal*: the status a streaming model (e.g. gpt-5.5 under
  codex, which coerces every streamed message into the schema) tags the progress
  messages it emits **before** its terminal message. It never advances the loop;
  `ralph` ignores all but the terminal message.

The harness supplies the `{status, message}` schema out of band per backend (codex
via `--output-schema`; claude via `--json-schema` surfaced as a `StructuredOutput`
tool) — the prompts describe only the contract, never a transport.

## Per-step reads / writes / commits / completions

| step | reads | writes | commits | completes a phase |
|---|---|---|---|---|
| **gather** | `project/loops/blocked.md` (existence check); `STATUS.md`; for a fresh brief, the one `phase-NN.md` + its Decision `DNN.md`(s) + `INDEX.md` + dependency interface signatures | `brief.md` **contract** region (fresh brief only); nothing on an in-flight brief or a blocked queue | no | no |
| **build** | `brief.md` only (contract + feedback) | source packages + id-tagged `*_test.go` (or the named config file, for a structural phase) | yes (the code increment) | no |
| **verify** | `brief.md` (contract + own prior feedback) + the suite + `project/design/D*.md` and `project/plan/phase-*.md` id tokens (the ratchet) | on pass: deletes the phase's `STATUS.md` line and `phase-NN.md`; on gap: `brief.md` **feedback** region; on repeated stall: `project/loops/blocked.md` | yes (the deletion, on pass) | **yes** (only verify) |

The toolchain the loop bakes in (from design's *Conventions*): build/typecheck
`go build ./...` + `go vet ./...`; test `go test ./...`; **green** = those plus
`gofmt -l .` printing nothing, all with zero failures. Tests are `*_test.go`
co-located in the package they exercise; cross-package integration tests live in
`internal/wiki/`. A **structural/config phase** (e.g. an `wiki/etc/nginx.conf`
edit) carries no `R-ids` — it is proven by a green suite plus the named fragment
check the phase states (a `project/`-excluded grep over that file).

## The brief lifecycle

`project/loops/brief.md` is the **single-phase**, **never-committed** (it should
be listed in `.gitignore`) seam between the prompts — the complete and only
input `build` and `verify` consume, so neither opens a design/plan/product doc.

- **gather authors the contract once** when a phase first becomes the active `⬜`
  phase, then **no-ops while it's in flight** — on later cycles it sees the brief
  already names the current phase and leaves it (contract *and* feedback) untouched,
  opening no big doc.
- **build consumes it** every cycle, closing verify's open gaps first, checking
  its own diff for dropped `R-` tags before committing, and commits.
- **verify passes → deletes the phase's `STATUS.md` line and `phase-NN.md`, commits
  the deletion, and deletes the brief**; **verify finds gaps → overwrites the
  feedback region** with only the currently-open gaps and leaves the brief in
  place, so it **persists across cycles** until the phase passes or a stall reset
  discards it.

## The stall and blocked ladder

`verify` measures progress cycle-to-cycle by comparing the current open-gap id set
to the prior one: **progress** is a strict subset (some gap closed); anything else
— including a fresh build commit with the same gaps unresolved — is **no
progress**, and the stall streak increments.

- **Three consecutive no-progress attempts** on the same phase → a **trajectory
  reset**: `verify` logs the stall to `~/.ralph/verify.log`, deletes the brief, and
  leaves `⬜`. The next `gather` rebuilds the contract fresh from spec — this
  assumes the accumulated brief drifted, not that the bar is wrong.
- **A second stall on the same phase** (the log already has an earlier `STALLED`
  line for it) → `verify` escalates instead of resetting again: it writes
  `project/loops/blocked.md` naming the phase, the attempts, the unsatisfied ids,
  and the exact command/output that will not go green, and logs a `BLOCKED` line.
  The next `gather` sees the file and reports `DONE`, stopping the run.
- **The operator's job with a `blocked.md`:** read the recorded command and
  output, decide the phase's done bar (in `project/design/` or `project/plan/`) is
  what needs to change, fix it there, delete `project/loops/blocked.md`, and
  restart the loop with `project/loops/run`.

## Why it converges (and is human-free)

`verify` can neither halt the loop nor advance a phase on a gap, so an incomplete
phase simply stays `⬜` and is re-attacked next cycle — now with verify's grounded,
command-tied feedback in front of `build`, and without `gather` re-reading the big
docs (it no-ops on the in-flight brief). The persisted feedback also gives `verify`
cross-cycle memory: it distinguishes *slow convergence* (the open-gap id set
shrinking) from a *true stall* (three consecutive attempts closing no gap). On a
true stall it does a **trajectory reset**; on a second stall of the same phase it
**blocks** instead, since a bar that a rebuilt contract still cannot satisfy is a
defective bar, not a stuck trajectory. The only exits are `gather → DONE`, which
requires either **zero `⬜` markers** (every phase verified green) or a blocked
phase awaiting the operator — plus the `ralph` budget rails.

## The `brief.md` schema

Two regions, one writer each — they never clobber each other:

```
# Brief — Phase NN

## Contract  (gather-owned — verify never writes here)

- Phase: NN — <one-line objective>
- Realizes: D<n>[, D<m>]
- Decision files: project/design/D0n.md[, …]

### Design prose — Decision <n> (<title>)
<the Decision's full statement, shape/signatures, and Rejected alternatives,
 copied verbatim from DNN.md — with that Decision's Verification list OMITTED>

### Ids to cover
R-XXXX-XXXX — <full requirement text, verbatim from the Decision's Verification list>
<one id per line; the id at line-start, an em-dash, then the requirement prose on
 the SAME line — only the ids this phase's Done-when lists; or `(none — structural phase)`>

### Files to touch
- <path> …

### Dependency interface signatures
<the exported signatures of depended-on packages, copied in>

### Done bar
<deterministic acceptance: green suite + every id covered by a genuinely-asserting
 co-located `// R-id` test, or the named structural check for a config phase>

## Verify feedback — attempt N  (verify-owned — gather writes this empty)
- build commit observed: <sha | none>
- stall streak: <n>
- open gaps:
  <one line per open gap: R-id + exact failing command + observed output; or (none)>
```

- The **contract region** is gather-owned, written once when the phase becomes
  active; verify never writes here.
- The **feedback region** is verify-owned; gather writes it empty on a fresh brief
  and never touches it again; build reads it but never writes it; verify
  **overwrites** it (never appends) each gap cycle.
