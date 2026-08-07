# gmail/project — workspace layout

Everything the gmail service needs to be **designed, planned, and built**
lives under `project/`. This file is the only loose file here; everything else
is in one of the folders below. Paths are written relative to the **service
root** (`gmail/`), which is also the directory the `ralph` build loop runs
from.

## The folders

| folder | what's in it | owned by |
|---|---|---|
| `product/` | `README.md` — the *why*, for whom, scope, user-facing promises | `/product-mode` (rewritten in place) |
| `research/` | `research.md` — collected external ground truth design references; plus free-form `*-research.md` working notes | `research.md`: `$seal-spec` (rewritten in place). Other notes: free-form. |
| `design/` | `README.md` (spine) + `INDEX.md` (manifest + sorted `R-id → Decision` map) + `DNN.md` (one per Decision) | `/design-mode` (rewritten in place) |
| `plan/` | `README.md` (spine) + `STATUS.md` (the manifest — the `Next phase` counter + the only home of each pending phase's `⬜` marker) + `phase-NN.md` (one per **pending** phase) | `/plan-mode` (a work queue: completion is deletion, not a flip) |
| `bugs/` | free-form bug diagnoses / write-ups | free-form (not mode-owned) |
| `requests/` | free-form feature requests | free-form (not mode-owned) |
| `loops/` | the `ralph` build-loop prompts: `gather.md`, `build.md`, `verify.md` (+ the ephemeral `brief.md`) | build-loop infrastructure |

The four **spine documents** (`product/README.md`, `research/research.md`,
`design/README.md`, `plan/README.md`) are each singular and owned by the
spec-authoring workflow (`$seal-spec`) — that workflow is the sanctioned way to
change them. The `bugs/`,
`requests/` and extra `research/*-research.md` notes are informal
scratch and are *not* owned by any mode command. Don't add ad-hoc documents to
the spine folders; fold corrections and follow-ons into the existing spine docs
via the mode commands (and append a plan phase) instead.

## The build loop

`project/loops/run` is the autonomous executor's entry point — run it from this
service directory. It wraps `ralph`, handed the three prompt files
(`gather.md`, `build.md`, `verify.md`) under `project/loops/`; see
`project/loops/README.md` for the full mechanics. In short: the prompts cycle
in fresh contexts — `gather → build → verify → …` — on a three-status contract
(`CONTINUE` non-terminal for mid-turn progress; `NEXT`/`DONE` terminal, with
only `gather` ever reporting `DONE`, on an empty `⬜` queue or a
`project/loops/blocked.md` file). `gather` authors the ephemeral,
gitignored `project/loops/brief.md` seam for the active phase; `build` reads it
and commits an increment; `verify` re-derives coverage independently and either
retires the phase (deleting its `STATUS.md` line and `phase-NN.md`) or writes
grounded feedback back into the brief for the next `build` turn. A phase that
stalls three attempts running gets its brief rebuilt from spec; stalling twice
escalates to `blocked.md`, which stops the loop for the operator. See
`project/loops/README.md` for the full contract, the brief schema, and the
stall/blocked ladder.
