# cron — build loop (gather → build → verify)

This directory holds the **installed autonomous build loop** for the cron
service: three prompts an unattended harness (`ralph`) re-invokes with a **fresh
context** every turn to build the project one phase at a time, plus the operator
wrapper that launches them. This README describes the loop **as installed**, so it
can never drift from the prompts on disk. It is not a spec artifact — the spec
shapes live under `project/product`, `project/design`, and `project/plan`, and
`project/README.md` (the workspace map) only points here.

## Running it

From the service root (`cron/`):

```
project/loops/run
```

`run` is a one-line executable wrapper whose entire contents are:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the **service root** (its working directory), cycles the three
prompt paths in fresh contexts (`gather → build → verify → gather → …`), and reads
only the **final message** of each turn.

## Status contract

Each turn ends on one of two **terminal** statuses, with a third **non-terminal**
status for progress:

- **`NEXT`** — terminal: this turn is done; advance to the next prompt (wrapping
  `verify → gather`).
- **`DONE`** — terminal: the whole job is complete; the loop stops. **Only
  `gather` ever reports it**, and only when no `⬜` phase remains. `build` and
  `verify` never report `DONE` — a fully-finished phase is still `NEXT`.
- **`CONTINUE`** — **non-terminal**: the status a streaming model tags each
  progress message it emits *before* its terminal message. `ralph` reads only the
  terminal (last) message, so `CONTINUE` never advances or ends the loop; it
  exists because codex coerces every streamed message into the `{status, message}`
  schema.

The harness supplies the `{status, message}` schema out of band and reads it back
itself (codex via `--output-schema`, claude via `--json-schema` surfaced as a
`StructuredOutput` tool) — the prompts describe only *which* status to report and
*what* the message says, never a transport.

## Per-step reads / writes / commits / completions

| step | reads | writes | commits | completes phase |
|---|---|---|---|---|
| **gather** | `STATUS.md`, one `phase-NN.md`, `design/INDEX.md`, realized `DNN.md`, (optionally) `product/README.md` | `brief.md` **contract region** (only when authoring a fresh brief) | no | no |
| **build** | `brief.md` only (contract + feedback) | service code + co-located id-tagged tests | yes (the code increment) | no |
| **verify** | `brief.md` (contract + own prior feedback) + runs the suite | on pass: deletes the phase's `STATUS.md` line + `phase-NN.md`, then `brief.md`; on gap: overwrites `brief.md` **feedback region** | yes (only the phase-deletion commit, on pass) | yes (on pass only) |

The next unit of work is found with:

```
grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1
```

(cron's `STATUS.md` phase lines are Markdown bullets: `- Phase NN ⬜ …`.)

## Brief lifecycle

`project/loops/brief.md` is the ephemeral, **git-ignored** seam
(`*/project/loops/brief.md` in the repo-root `.gitignore`) that keeps `build`'s
context scoped to one phase — the complete and only input `build` and `verify`
consume, so neither ever opens design or plan. It is **single-phase** and
**phase-scoped, not per-cycle**:

- **gather** authors the brief's **contract region once**, when a phase first
  becomes the active `⬜` phase; while that phase stays `⬜`, gather **no-ops on the
  in-flight brief** (leaves it untouched, opens no big doc).
- **build** consumes the whole brief (contract + feedback), prioritizing any open
  gaps in the feedback region, and never writes the brief.
- **verify** either **passes** the phase (delete its `STATUS.md` line and
  `phase-NN.md`, commit the deletion, delete the brief) or records a **gap**
  (leave the `⬜` line and body file in place, overwrite the feedback region with
  only the currently-open gaps, each tied to an `R-id` and grounded in the exact
  failing command/output — never delete the brief). The brief thus persists
  across cycles until the phase passes or a stall reset.

## Why it converges (and is human-free)

`verify` can neither halt the loop nor advance a phase on a gap, so an incomplete
phase just stays `⬜` and is re-attacked next cycle — now with verify's grounded
feedback in front of `build`, and without `gather` re-reading the big docs (it
no-ops on the in-flight brief). The persisted feedback also gives `verify`
cross-cycle memory: it distinguishes *slow convergence* (the open-gap id set
shrinking/changing) from a *true stall* (the **same** gap ids unsatisfied for **3**
consecutive no-progress attempts with no new build commit). On a true stall it does
a **trajectory reset** — discards the brief and logs the stall to
`~/.ralph/verify.log` — so the next `gather` rebuilds the contract fresh from spec,
still without halting or advancing the phase. The only exit is `gather → DONE`,
which requires an empty queue (no `⬜` line left in `STATUS.md`), so the run ends
only when every phase has been verified green and deleted (or a ralph budget
rail trips).

## The `project/loops/brief.md` schema

`gather` writes the **contract region**; `verify` writes the **feedback region**;
the two writers never clobber each other.

**Contract region** (gather-owned, written once per phase):

- `# Brief — Phase NN: <one-line objective>` header, plus `phase:`, `realizes:`,
  and `decision_files:` lines.
- `## Design` — the **full design prose** of each realized Decision (its Decision
  statement, shape/signatures, and Rejected alternatives) copied verbatim from the
  `DNN.md`, **with that Decision's Verification list omitted**.
- `## Ids to cover` — **only** the phase-listed ids, **one per line** in the exact
  form `R-XXXX-XXXX — <full requirement text copied verbatim>` (id at line-start,
  em-dash, full requirement prose on the same line), or the single line
  `(none — structural phase; see Done bar's named check)`. This keeps the
  denominator grep-able: `grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md`
  yields exactly this phase's id set.
- `## Files to touch`, `## Dependency interfaces / required shapes` (public
  signatures + required config/doc snippets copied from design), and `## Done bar`
  (the deterministic acceptance conditions, including the co-located test-placement
  rule and the green-suite commands).

**Feedback region** (`verify`-owned): a single `## Verify feedback — attempt N`
heading carrying the per-attempt counter, the build commit verify observed, the
stall-streak counter, and a checklist of **only** the currently-open gaps — each
line tied to one `R-id` and grounded in the exact failing command/output. `gather`
writes this **empty** (`(none yet)`) on a fresh brief; `verify` **overwrites** it
(never appends) each gap cycle; `build` reads but never writes it.
