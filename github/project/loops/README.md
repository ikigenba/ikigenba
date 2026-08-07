# github — build loop (gather → build → verify)

This directory holds the **three-prompt build loop** an unattended harness
(`ralph`) re-invokes with a **fresh context** every turn to build the github
service one phase at a time, plus the operator wrapper that launches it. This
README describes the loop **as installed here** — it lives beside the prompts so
it can never drift from them. It carries only *loop mechanics*; the spec shapes
(product / design / plan) live under `../design`, `../plan`, `../product` and are
not restated here.

## Running it

From the **service root** (`github/`, ralph's working directory):

```
./project/loops/run
```

which is exactly:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` cycles the three prompts in order and wraps `verify → gather`. Each prompt
is read in a **fresh, isolated context**; all cross-turn state lives in the
workspace (`project/plan/STATUS.md` markers, the ephemeral `brief.md`, and — only
on an unresolved defective bar — `blocked.md`). Toolchain commands run directly
from the service root (`GOWORK=off go build ./...`, not `cd github && …`).

## The status contract

Each turn ends with a `{status, message}` the harness supplies out of band
(`ralph` injects the schema per backend — codex `--output-schema`, claude
`--json-schema`) and reads back itself; the prompts never hard-code a transport.
`ralph` reads only the **terminal** (last) message of a turn and advances on it:

| status | meaning |
|---|---|
| `CONTINUE` | **non-terminal** — a progress message streamed *before* the turn's final message (a streaming backend like codex tags every mid-turn message). Never advances the loop. |
| `NEXT` | **terminal** — this turn is done; advance to the next prompt (wrapping `verify → gather`). |
| `DONE` | **terminal** — the whole job is complete; the loop stops. **Only `gather` ever reports this**, and only when no `⬜` phase remains, or a `project/loops/blocked.md` is found. |

`build` and `verify` **always** report `NEXT` — never `DONE`. Finishing a phase
completely (green suite, all gaps closed) is still `NEXT`; ending the run belongs
to `gather` alone.

## Per-step reads / writes / commits / deletes

| step | reads | writes | commits | deletes phase |
|---|---|---|---|---|
| **gather** | `blocked.md` (checked first), `STATUS.md`, one `phase-NN.md`, `INDEX.md`, the realized `DNN.md` | `brief.md` **contract region** — only for a phase with no in-flight brief | no | no |
| **build** | `brief.md` (contract **+** feedback) only | production code + co-located id-tagged tests | yes (the code increment) | no |
| **verify** | `brief.md` (contract + its own feedback), the suite, the design/plan id-set greps (ratchet only) | deletes the phase's `STATUS.md` line + `phase-NN.md`, or writes `brief.md` feedback region, or writes `blocked.md` | the phase deletion (pass only) | **yes** (pass only) |

Only `gather` reads the big docs; only `verify` deletes a phase's `STATUS.md`
line and body file, deletes the brief, or writes `blocked.md`.

## The brief lifecycle

`project/loops/brief.md` is the ephemeral, **git-ignored**, single-phase seam that
keeps `build`/`verify` scoped to one phase without opening design or plan. It is
**phase-scoped, not per-cycle**, and **region-owned** (each region has one
writer):

- **gather** authors the **contract region** once, when a phase first becomes the
  active `⬜` phase. On later cycles, if a brief for that *same* phase already
  exists, gather **no-ops** — it leaves both regions untouched and opens no big
  doc.
- **build** consumes the whole brief. If the `## Verify feedback` region lists
  open gaps, it closes those first, then does as much of the remaining work as
  cleanly fits (ideally the whole phase). It never writes the brief.
- **verify** re-derives truth independently. **Pass** → delete the phase's
  `STATUS.md` line and `phase-NN.md`, commit, and **delete** the brief. **Gap**
  → leave `⬜`, change no source, and **overwrite**
  the feedback region with only the currently-open gaps (each tied to an `R-id`
  and grounded in the exact failing command/output) — the brief **persists** so
  the next `build` sees the feedback.

## The stall-and-blocked ladder

`verify` tracks progress cycle to cycle (a shrinking open-gap id set; a new
build commit alone is never progress):

1. **Three consecutive no-progress attempts on a phase** → **stall reset**:
   `verify` logs `<date> Phase NN STALLED after N attempts: <gap ids>` to
   `~/.ralph/verify.log`, deletes `brief.md`, leaves `⬜`, and returns `NEXT`.
   The next `gather` rebuilds the contract fresh from spec — this is a reset of
   a stuck *trajectory*, not a halt.
2. **A second stall on the same phase** (an earlier `STALLED` line for it
   already in `~/.ralph/verify.log`) → **blocked escalation**: a rebuilt
   contract was already tried and did not help, so the phase's done bar is the
   likely fault. `verify` writes `project/loops/blocked.md` naming the phase,
   the total attempts, the still-unsatisfied ids, and the exact command +
   observed output that will not go green, logs `<date> Phase NN BLOCKED after
   N attempts: <gap ids>`, deletes `brief.md`, leaves `⬜`, and returns `NEXT`.
3. **The next `gather`** sees `blocked.md` on its first check, reports `DONE`
   without reading anything else, and the run stops.

**Operator recovery from a `blocked.md`:** read the recorded failing command and
output, fix the phase's done bar (or design/plan) in `project/` — the loop never
edits `project/` itself — delete `project/loops/blocked.md`, and restart
`./project/loops/run`.

## Why it converges (and terminates)

`verify` can neither halt the loop nor advance a phase on a gap, so an incomplete
phase just stays `⬜` and is re-attacked next cycle — now with grounded feedback
in front of `build`, and without `gather` re-reading the big docs (it no-ops on
the in-flight brief). The persisted feedback gives `verify` cross-cycle memory to
tell *slow convergence* (shrinking gap set) from a *true stall* (identical gaps,
no new commit) and reset the latter; a second stall on the same phase converts
into a `blocked.md` instead of spinning forever on a defective bar. The **only**
exits are `gather → DONE` on zero `⬜` markers, or `gather → DONE` on finding
`blocked.md` — so the run ends only when every phase is verified green, or a
defective phase is surfaced to the operator (or a ralph budget rail trips).

## `project/loops/brief.md` schema

Two regions, one writer each:

```
# Brief — Phase NN: <one-line objective>

phase: NN
realizes: D<n>[, D<m>]
decision_files:
  - project/design/D0n.md

## Ids to cover
R-XXXX-XXXX — <full requirement text copied verbatim from the Decision's Verification list>
R-YYYY-YYYY — <full requirement text copied verbatim>
# ...one id per line in that exact `R-... — text` form, OR:
# (none — structural phase; see the Done bar's named content check)

## Design prose (copied verbatim from the DNN.md — Verification lists omitted)
### Decision <n> — <title>
<Decision statement + shape/signatures + Rejected alternatives, verbatim,
 WITHOUT that Decision's Verification list>

## Files to touch
- <path>

## Dependency interfaces (copied from design — do not open design files)
```go
// package <dep>  (from D0k)
<copied exported type / func / const signatures>
```

## Done bar
- Every "Ids to cover" id covered by a genuinely-asserting, reachable
  `// R-XXXX-XXXX`-tagged test (structural: the named content check instead).
- Test placement: co-located with the code exercised, named for the behavior,
  never a per-phase or root-level test file.
- The suite is green: GOWORK=off go build ./... · GOWORK=off go vet ./... ·
  gofmt -l . (empty) · GOWORK=off go test ./...
- <any phase-specific content check, copied verbatim>

## Verify feedback
<!-- gather leaves empty; verify overwrites with attempt N, build commit,
     stall streak, and the current open-gap checklist -->
```

- **gather-owned contract region** — everything from the header through the Done
  bar. Written once per phase; `verify` never writes here.
- **verify-owned feedback region** — the `## Verify feedback — attempt N`
  heading with its per-attempt counter, the observed build commit, the stall
  streak, and a checklist of **only** the open gaps. `gather` writes it empty;
  `verify` overwrites it each gap cycle; `build` reads but never writes it.

`project/loops/brief.md` and `project/loops/blocked.md` are both **git-ignored**
via the repo-root `.gitignore` (`*/project/loops/brief.md`,
`*/project/loops/blocked.md`) — neither is a spec artifact, and both are
ephemeral loop state.

## The global coverage ratchet (`verify`, step 4)

Beyond this phase's own denominator, `verify` also runs a set check over the
**whole** design each cycle, to catch a rewrite silently dropping a
previously-covered id from an *earlier, already-completed* phase:

```
comm -23 \
  <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md \
      | grep -v '^R-DMUT-QF4A$' | sort -u) \
  <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
        <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
```

Empty output is the pass condition; any id it prints is a coverage regression
and an open gap. **`R-DMUT-QF4A` (D2) is excluded from this check by design**:
it is the one live-substrate id, proven only against the real GitHub App and
verified **out of loop** per `project/github-verification.md` and
`project/plan/README.md` — no phase's brief ever assigns it, and it is never
tagged in an offline test, so its absence from both sets is expected and must
never be reported as a gap.

## This service's toolchain (baked into the prompts)

Taken from design's *Conventions* (`project/design/README.md`); the prompts
inline these so no step guesses:

- **Build / typecheck:** `GOWORK=off go build ./...` from `github/`. Forcing
  `GOWORK=off` matches the deterministic production build and proves the
  module resolves standalone via its `replace` directives.
- **Test:** `GOWORK=off go test ./...` from `github/`.
- **"Green"** = build succeeds, `go test` passes with **no failures and no
  `SKIP`**, `gofmt -l .` is empty, and `GOWORK=off go vet ./...` is clean — all
  from `github/`.
- **Package layout:** `cmd/github/main.go` is the composition root;
  `internal/githubapp` (the appkit Spec), `internal/gh` (auth + REST client),
  `internal/mcp` (the domain tool registrations), `internal/db` (embedded
  migrations), `internal/web` (landing page + embedded assets).
- **Test placement:** package-local `*_test.go`, co-located with the code they
  exercise and named for the behavior (e.g. `internal/gh/*_test.go`,
  `internal/mcp/*_test.go`, `internal/web/*_test.go`); the single home for
  composition-root / cross-package integration and suite-contract smoke tests
  is `cmd/github/main_test.go`. No per-phase or root-level test file.
- **Offline tests only.** The GitHub client is exercised against an injected
  `http.RoundTripper` stub. The one live-substrate id, `R-DMUT-QF4A` (the real
  GitHub App `health` proof), is verified **out of loop** per
  `project/github-verification.md` and never appears in a brief — see the
  ratchet carve-out above.
- **Zero new third-party dependencies.** Only the Go standard library and the
  chassis already wired via `replace` (`appkit`, and `eventplane` only for a
  shared type).
- **Next-phase lookup:** `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
  (github's STATUS lines are Markdown bullets prefixed with `- `).
