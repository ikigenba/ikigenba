# webhooks — build loop (as installed)

This directory holds the **three-prompt build loop** an unattended harness
(`ralph`) re-invokes with a **fresh context** every turn to build the webhooks
service one phase at a time. This README describes the loop **as it is installed
on disk** — it lives beside the prompts it documents so it can never drift from
them. The workspace map (`project/README.md`) only points here; the spec shapes
live in `project/design/`, `project/plan/`, and `project/product/`.

## Running it

```
project/loops/run
```

`run` is the executable operator wrapper; its entire body is:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the **service root** (`webhooks/`, its working directory), so
every workspace path the prompts reference is service-root-relative (`project/…`).
It cycles the three prompts — `gather → build → verify → gather → …` — each in its
own fresh, isolated context.

## Status contract

Each turn ends by reporting a `status` and a one-sentence `message`. The harness
supplies the `{status, message}` schema out of band (codex via `--output-schema`,
claude via `--json-schema`) and reads back the **final** message of the turn — a
prompt never emits literal JSON:

- **`NEXT`** — terminal: this turn is done; advance to the next prompt (wrapping
  `verify → gather`).
- **`DONE`** — terminal: the whole job is complete; the loop stops. **Only
  `gather` ever reports `DONE`**, on finding no `⬜` phase left or a blocked phase
  awaiting the operator. `build` and `verify` always report `NEXT`.
- **`CONTINUE`** — the **non-terminal** status a streaming model tags the progress
  messages it emits *before* its terminal message. `ralph` reads only the last
  message, so `CONTINUE` never advances the loop.

## Per-step reads / writes / commits / queue mutations

| step | reads | writes | commits | mutates STATUS.md |
|---|---|---|---|---|
| **gather** | `blocked.md` (existence), `STATUS.md`, the one `phase-NN.md`, its realized `DNN.md` (via `INDEX.md`), dependency source | `brief.md` **contract region** (fresh phase only) | no | no |
| **build**  | `brief.md` only | service code + co-located id-tagged tests | yes (the code increment) | no |
| **verify** | `brief.md` + runs the suite + traces coverage + the global ratchet | `brief.md` **feedback region** (on gap), or deletes `brief.md` (on pass/stall), or writes `blocked.md` (on repeated stall) | yes (deletes the phase's line + `phase-NN.md`, on pass) | yes (deletes the phase's line, on pass only) |

Next-phase lookup (gather): `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
(phase lines are Markdown bullets). "The suite is green" (build &
verify): `go build ./...`, `go vet ./...`, and `go test ./...` all exit 0 with no
failures, tests run against real temp-file SQLite with a deterministic injected
clock — never a mocked store or outbox.

## Brief lifecycle

`project/loops/brief.md` is the ephemeral seam between the three prompts — the
complete and only input `build` and `verify` consume, so neither opens the big
docs. It is **never committed** (gitignored) and describes exactly **one** phase
at a time:

- **gather** authors the brief's **contract region** once, when a phase first
  becomes the active `⬜` phase. While that phase stays `⬜`, gather **no-ops** on
  it — it leaves the in-flight brief (contract *and* feedback) untouched and opens
  no big doc.
- **build** consumes the whole brief (contract + feedback), does as much of the
  phase as cleanly fits one turn, commits, and never writes the brief.
- **verify** re-derives truth from scratch. On **pass** it deletes the phase's
  `STATUS.md` line and its `phase-NN.md`, commits that deletion, and **deletes**
  the brief. On a **gap** it **overwrites** the feedback region with only the
  currently-open gaps (each tied to an `R-id` and grounded in the exact failing
  command/output) and leaves the brief in place, so the next `build` sees the
  feedback. The brief thus **persists across cycles** until the phase passes or
  a stall reset discards it.

## Why it converges (human-free)

`verify` can neither halt the loop nor advance a phase on a gap, so an incomplete
phase just stays `⬜` and is re-attacked next cycle — now with verify's grounded
feedback in front of `build`, and without `gather` re-reading the big docs (it
no-ops on the in-flight brief). The persisted feedback also gives `verify`
cross-cycle memory: it distinguishes *slow convergence* (the open-gap id set
shrinking) from a *true stall* (no gap closed across **3** consecutive attempts —
a new build commit alone is never counted as progress).

## The stall and blocked ladder

- **Attempt loop:** every gap cycle, `verify` overwrites the brief's feedback
  region with the currently-open gaps and hands back to `build`.
- **First stall (3 consecutive no-progress attempts):** a **trajectory reset** —
  `verify` logs `Phase NN STALLED …` to `~/.ralph/verify.log`, discards the brief,
  and leaves the phase `⬜`. The next `gather` rebuilds the contract fresh from
  spec, on the theory that the accumulated brief (not the bar) was the problem.
- **Second stall on the same phase:** a rebuilt contract was already tried and
  did not help, so `verify` escalates instead of resetting again — it writes
  `project/loops/blocked.md` naming the phase, the attempts, the unsatisfied ids,
  and the exact command/output that will not go green, logs
  `Phase NN BLOCKED …` to `~/.ralph/verify.log`, discards the brief, and leaves
  the phase `⬜`.
- **Operator response to a `blocked.md`:** read the recorded command and output,
  fix the phase's done bar (or the design/plan it derives from) in `project/`,
  delete `project/loops/blocked.md`, and restart the loop with `project/loops/run`.
  Until the file is deleted, every `gather` turn reports `DONE` immediately.

Both rungs of the ladder stay inside the "verify never halts / never advances on
a gap" invariant — a stall resets the trajectory, a repeat stall escalates to the
operator, and the loop's only exits remain `gather → DONE`: zero `⬜` phases left,
or a blocked phase awaiting the operator, plus the ralph budget rails.

## `project/loops/brief.md` schema

**Contract region** (gather-owned; written once per phase):

```
# Brief — Phase NN: <one-line objective>

phase: NN
realizes: <D14 | D7, D8>
decision_files: <project/design/D0k.md[, …]>
status_line: <the exact STATUS.md phase line, verbatim>

## realized design (verbatim from the DNN.md — Verification list omitted)
<each realized Decision's full design prose — header, Decision statement with
 shape/signatures, and Rejected alternatives — copied verbatim, but stopping
 before that Decision's Verification list>

## ids to cover
<one id per line: `R-XXXX-XXXX — <full requirement text copied verbatim>`;
 only the ids this phase owns; or `(none — structural phase)`>

## files to touch
<one path per line; ../ paths for repo-root harness edits>

## dependency interfaces (copied — build must NOT open design/plan)
<go signatures build will call, labelled by source phase/package; or
 "(none — no earlier phase)">

## done bar
<per-id behavior + substrate; suite-green definition; the test-placement rule;
 any "requires the suite up" note>
```

**Feedback region** (verify-owned; overwritten each gap cycle, empty on a fresh
brief):

```
## Verify feedback — attempt N
<attempt counter, the build commit verify observed, the stall-streak counter, and
 a checklist of ONLY the open gaps — each line an R-id + the exact failing command
 + observed output (+ file:line when known)>
```

`project/loops/brief.md` and `project/loops/blocked.md` are both gitignored —
neither is a spec artifact, and neither is ever committed.
