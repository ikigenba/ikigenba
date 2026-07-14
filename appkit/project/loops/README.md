# appkit — the installed build loop

The gather → build → verify loop `ralph` drives to build appkit one plan phase
at a time, unattended. The operator starts it with:

```
project/loops/run
```

which is exactly:

```
exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the service root (`appkit/`), re-invoking each prompt in a
**fresh context** and cycling `gather → build → verify → gather → …`.

## The status contract

Each turn ends with a terminal status `ralph` reads from the final message:

- `NEXT` — advance to the next prompt (wrapping `verify → gather`).
- `DONE` — stop the loop. **Only `gather` ever reports it**, and only when
  `STATUS.md` has no `⬜` phase left; `build` and `verify` always report `NEXT`.
- `CONTINUE` — the non-terminal status a streaming model tags its mid-turn
  progress messages with; it never drives the loop (`ralph` reads only the
  terminal message).

## Who reads and writes what

| step | reads | writes | commits | flips marker |
|---|---|---|---|---|
| **gather** | `STATUS.md`, one `phase-NN.md`, the realized `DNN.md` via `INDEX.md`, dependency signatures | `brief.md` contract region (fresh phase only; no-ops on an in-flight brief) | never | never |
| **build** | `brief.md` only (both regions) | source + tests; never the brief | every turn (its increment) | never |
| **verify** | `brief.md` (both regions), the repo, the suite | `brief.md` feedback region, or deletes the brief | the `⬜→✅` flip on pass | the only flipper |

`gather` is the only prompt that opens the big docs; `build` and `verify` work
entirely from the brief.

## The brief lifecycle

`project/loops/brief.md` is the ephemeral, **gitignored** seam (listed in the
repo-root `.gitignore`). It is **phase-scoped, not per-cycle**:

1. When a phase first becomes the active `⬜` phase, `gather` authors the brief's
   contract region and an empty feedback stub. While that phase stays `⬜`,
   `gather` finds the matching header on later cycles and **no-ops** — no big
   doc is re-read, and `verify`'s feedback survives.
2. `build` consumes the whole brief, closes any listed gaps first, and commits
   its increment.
3. `verify` re-derives coverage from scratch. **Pass** → flip the phase's marker
   in `STATUS.md`, commit, delete the brief. **Gap** → overwrite the feedback
   region with only the currently-open gaps (attempt counter, observed build
   commit, stall streak, each gap tied to an `R-id` and a failing command);
   the brief persists into the next cycle.

## Why it converges

`verify` can neither halt nor advance a phase on a gap, so an incomplete phase
just stays `⬜` and is re-attacked next cycle — with `verify`'s grounded feedback
in front of `build`. The persisted feedback gives `verify` cross-cycle memory:
it distinguishes slow convergence (the open-gap set shrinking/changing) from a
true stall (the same gap ids with no new build commit for 3 consecutive
attempts). On a true stall it logs to `~/.ralph/verify.log`, deletes the brief,
and leaves the marker `⬜`, so the next `gather` rebuilds the contract fresh from
spec. The only exit is `gather` finding zero `⬜` phases (`DONE`) — or a `ralph`
budget rail.

## The `project/loops/brief.md` schema

Two single-writer regions split by the literal marker line
`<!-- VERIFY FEEDBACK BELOW … -->` — `gather` owns everything above and
including it, `verify` everything below it.

**Contract region** (gather-owned, written once per phase):

- `# Brief — Phase NN` — the header gather keys its no-op check on.
- `## Objective` — the phase's one-line objective.
- `## Realized Decision(s)` — `D<k>` and its file path.
- `## Design prose …` — each realized Decision's statement, shape/signatures,
  and Rejected alternatives, verbatim from its `DNN.md`, **minus** its
  Verification list.
- `## Ids to cover` — one id per line, `R-XXXX-XXXX — <full requirement text
  verbatim>` (id at line-start; extractable with
  `grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md`), or the single
  line `(none — structural phase)`.
- `## Files to touch` — the paths.
- `## Dependency interface signatures` — copied verbatim, or `(none)`.
- `## Done bar` — the phase's deterministic pass predicates as exact commands.

**Feedback region** (verify-owned, overwritten each gap cycle):

- `## Verify feedback — attempt N` with `build-commit-observed`, `stall-streak`,
  and an `open gaps` checklist — each line one `R-id`, the exact failing
  command, and its observed output. Gather's fresh stub is
  `## Verify feedback` + `(none yet — first build attempt)`.
