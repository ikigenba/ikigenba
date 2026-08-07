# nginx — build loop

The unattended `gather → build → verify` loop that builds
`nginx/project/plan/`'s pending phases one at a time. Start it (**from the
repo root**, not from `nginx/`) with:

```
nginx/project/loops/run
```

which wraps exactly:

```
ralph nginx/project/loops/gather.md nginx/project/loops/build.md nginx/project/loops/verify.md
```

`nginx`'s toolchain commands (`nginx -p nginx -c nginx.conf -t`, `bash -n
nginx/run`) are repo-root relative — nginx configuration and the `run` script
have no module boundary of their own, and design's Conventions state them
against the repo root — so unlike a Go subproject loop, this one's working
directory is the **repo root**, and every path the three prompts reference is
prefixed `nginx/` accordingly.

## Status contract

Every turn ends with one of three statuses. `ralph` reads only the **last**
message of the turn and advances on that.

| status | terminal? | meaning |
|---|---|---|
| `CONTINUE` | no | a progress message streamed before the turn's real end; never advances the loop |
| `NEXT` | yes | this turn's work is done; hand off to the next prompt (wraps `verify → gather`) |
| `DONE` | yes | the whole job is complete; the loop stops — **only `gather` ever reports this** |

## Per-step reads / writes / commits

| step | reads | writes | commits | deletes |
|---|---|---|---|---|
| `gather` | `nginx/project/plan/STATUS.md`, `nginx/project/loops/blocked.md` (existence); (fresh brief only) one `phase-NN.md`, `INDEX.md`, the realized `DNN.md`(s) | `nginx/project/loops/brief.md` (fresh brief case only; leaves an in-flight brief untouched) | — | — |
| `build` | `nginx/project/loops/brief.md` only | the config/static/script files the brief names | this turn's increment | — |
| `verify` | `nginx/project/loops/brief.md`; re-derives all checks itself (`bash -n`, `nginx -t`, the brief's structural checks, the id-ratchet grep) | `nginx/project/loops/brief.md` feedback region (gap case); `~/.ralph/verify.log` (stall/blocked) | the phase's `STATUS.md` line + `phase-NN.md` deletion (pass case) | pass case: `brief.md`; stall/blocked: `brief.md` |

## The brief lifecycle

`gather` authors `nginx/project/loops/brief.md` once, the first time a phase
becomes the active `⬜` phase — a self-contained contract carrying the
realized Decision's design prose (Verification list excluded), the phase's
own slice of ids (today always "none — structural phase", per
`project/design/README.md` "Requirement ids"), files to touch, dependency
interfaces, and the done bar. While that phase stays `⬜`, `gather` no-ops on
every later cycle (it never re-reads `nginx/project/design/` or the phase's
`phase-NN.md` for a phase already in flight). `build` reads the brief and does
bounded work against it, prioritizing any open gaps in the feedback region.
`verify` re-derives the truth from scratch every cycle: pass deletes the
phase's `STATUS.md` line, its `phase-NN.md`, and the brief; a gap leaves `⬜`
untouched and overwrites the feedback region with only the currently open
gaps.

## Stall and blocked ladder

`verify` tracks progress by comparing this cycle's open-gap set against the
prior cycle's, using its own persisted feedback region — not `build`'s claims.
Three consecutive attempts that close no gap trigger a **trajectory reset**:
the brief is discarded (phase stays `⬜`), so the next `gather` rebuilds the
contract fresh from spec. If the *same* phase stalls a second time after a
reset, that is not a stuck trajectory but a defective bar — `verify` writes
`nginx/project/loops/blocked.md` naming the phase, the attempts, the
still-unsatisfied checks, and the exact command/output that will not go
green, and the next `gather` reports `DONE` on sight of that file.

**Operator recovery from a `blocked.md`:** read the recorded command and
output, fix the phase's done bar in `nginx/project/plan/` or
`nginx/project/design/` (the loop treats `nginx/project/` as read-only and
cannot fix it itself), delete `nginx/project/loops/blocked.md`, and restart
`nginx/project/loops/run`.

## Why this converges

`verify` can neither halt the loop nor advance a phase on a gap — an
incomplete phase just stays `⬜` and gets re-attacked next cycle, now with
`verify`'s grounded feedback in front of `build`. The loop's only two exits
are both `gather`-only: zero `⬜` phases left (everything verified green), or
a blocked phase awaiting the operator — plus `ralph`'s own budget rails.

## `nginx/project/loops/brief.md` schema

Two regions, each owned by exactly one writer:

- **Contract region** (`gather`-owned, written once per phase): phase id +
  objective, realized Decision id(s) and file(s), the realized Decision's full
  design prose (Verification list omitted), the ids to cover (today always
  "none — structural phase"), files to touch, dependency interface signatures,
  and the done bar.
- **Verify feedback region** (`verify`-owned): a `## Verify feedback —
  attempt N` heading, the build commit `verify` last observed, the stall
  streak, and a checklist of only the currently open gaps, each grounded in
  the exact failing command/output. Empty on a fresh brief; overwritten
  (never appended) on every gap cycle; read but never written by `build`.

## This tree's specifics

`nginx/` mints no Verification ids today (every Decision's Verification list
says "ids: none" — see `project/design/README.md` "Requirement ids"), so every
phase here is **structural**: its done bar is an exact committed file, an
exact diff/grep, or a real `nginx -t`, never a tagged-test count. The two
checks every phase's done bar builds on, both run from the repo root:

- `bash -n nginx/run` exits 0.
- `mkdir -p nginx/tmp && nginx -p nginx -c nginx.conf -t` exits 0.

`verify.md` still runs a coverage-ratchet grep over `nginx/project/design/D*.md`
so the loop keeps working unmodified the moment some future Decision does
mint an id — see its step 3.

See `nginx/project/product/README.md`, `nginx/project/design/README.md`, and
`nginx/project/plan/README.md` for the spec shapes this loop consumes — this
file documents only the loop mechanics, never restates those.
