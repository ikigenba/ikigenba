# nginx — build loop

The installed unattended build loop for this tree: a `gather → build → verify`
cycle that `ralph` re-invokes with a **fresh context** every turn, building the
plan one pending phase at a time. This file describes the loop **as installed**,
beside the prompts it describes. The spec shapes it consumes
(`project/product/`, `project/design/`, `project/plan/`) are
documented in `project/README.md`; loop mechanics live only here.

## Running it

```
cd nginx
./project/loops/run
```

`run` is the executable operator wrapper. Its body is:

```sh
exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the **service root** (`nginx/`, its working directory), and
this tree's check commands (`mkdir -p tmp && nginx -p . -c nginx.conf -t`,
`bash -n run`) are service-root-relative to match. Every workspace path the
prompts reference is service-root-relative (`project/…`).

**Environmental precondition:** an `nginx` binary on `PATH` — no Go toolchain
needed. Per `root project/design/D23.md` a missing precondition is a **hard
failure**, never a skip: the loop reports a gap rather than passing. Note
`nginx` commonly lives in `/usr/sbin`, which is not on a non-root user's default
`PATH`; export it before starting the loop or the config check cannot run.

## The status contract

Each turn ends by reporting a `status` and a one-sentence `message`. The harness
supplies the schema out of band and reads back only the turn's **final**
message.

| status | terminal? | who reports it |
|---|---|---|
| `CONTINUE` | no | any prompt, on progress messages streamed *before* the final message. Never advances the loop. |
| `NEXT` | yes | any prompt — advance to the next prompt, wrapping `verify → gather`. |
| `DONE` | yes | **`gather` only** — the loop stops. |

`build` and `verify` **always** end on `NEXT`. Finishing a phase completely,
every check green and all gaps closed, is still `NEXT`; only `gather` ever ends
the run. `CONTINUE` exists because a streaming backend coerces every streamed
message into the schema, so a model narrating progress needs a non-terminal
value to tag those with.

The two `DONE` conditions are the only ends of the loop (plus ralph's own budget
rails): `project/loops/blocked.md` exists, or the `⬜` grep over
`project/plan/STATUS.md` finds no pending phase.

## What each step reads, writes, and commits

| step | reads | writes | commits | deletes |
|---|---|---|---|---|
| **gather** | `project/plan/STATUS.md`; the one `project/plan/phase-NN.md`; `project/design/INDEX.md`; only the named `DNN.md`; dependency facts | `project/loops/brief.md` (contract region only, and only for a fresh phase) | nothing | nothing |
| **build** | `project/loops/brief.md` only | config, `run`, static/committed files in this tree | this turn's increment | nothing |
| **verify** | `project/loops/brief.md`; the checks; the id-set grep | `project/loops/brief.md` feedback region, or `project/loops/blocked.md` | the phase-retirement deletion | the phase's `STATUS.md` line + `phase-NN.md` + the brief, on a pass |

The next unit of work is found with:

```
grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1
```

Phase lines are Markdown bullets; the `Next phase: NN` counter line is not a
bullet and never matches. There is no done marker — a completed phase's line and
body file are **deleted**.

## The gate: structural, because there is no test suite

This tree holds nginx configuration, two static committed files, and one Bash
script. **There is no Go module, no test suite, and no test-file glob**; the
repo-root `go.work` does not and must not name it, and a passing repo-wide
`go test ./...` is never evidence about it. So the loop's gate is structural.
From the service root:

- `bash -n run` exits 0.
- `mkdir -p tmp && nginx -p . -c nginx.conf -t` exits 0 and prints
  `configuration file … test is successful`. The `mkdir` is part of the command:
  the config declares its scratch paths under `tmp/` and nginx refuses to create
  that parent itself.
- Every structural check the phase names holds **with its stated expected
  output** — exact-match greps against a concrete file outside `project/`, an
  exact committed file present, an exact `diff`. For a `prints 1` check the
  observed count must equal 1 exactly: `2` fails as surely as `0`, because a
  phrase written twice is as much a defect as one written never.

Those greps are `project/`-excluded by construction, so no phrase in the spec or
in these prompts can satisfy them. A check that *could* be satisfied by text in
`project/` is a defective, self-referential bar — `verify` records it as an
open gap naming the self-reference rather than passing it, since only the
operator can fix it in `project/`.

Testing layers, per `root project/design/D23.md` (adopted by D4): **manual
only** — no hermetic, no composed, no live layer, and no `//go:build live` file.
The contract's own no-test-suite clause makes conformance here structural, so
its `[proof: per-service]` ids are deliberately **not** cited: no file in this
tree could carry an id tag. The `nginx -t` and `bash -n` checks are configuration
and syntax checks, not tests, and are not a layer.

The claims that actually matter — a request refused at the boundary, a real CA
issuing for names that really resolve, a real nginx selecting a real
`default_server` — are verified by hand against the running stack (`bin/start`,
then the local front door) or against the live box via the runbook in the
repo-root `deploy.md`. They are never asserted by a stub, because a stub would
accept anything and prove nothing.

## The coverage ratchet (currently a no-op, deliberately kept)

`verify` runs, every cycle:

```
grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u
```

**Empty output is the pass condition.** The `grep -v 'R-XXXX-XXXX'` filter is
required: the design docs write `R-XXXX-XXXX` as the *shape* of an id in prose,
and without the filter that placeholder would surface as a phantom uncovered id
the check could never clear.

This tree mints no ids today, so both sides are empty. The check is kept live so
that the moment a Decision does mint one, `verify` treats every such id as an
**open gap** rather than passing silently — there is no test-file glob and no
`R-`-tag convention here, so an id with no defined proof mechanism is a defect in
the loop and the spec, not in the code. No amount of building closes it, so it
escalates straight to `blocked.md` for the operator.

## The brief lifecycle

`project/loops/brief.md` is the seam that keeps `build`'s context scoped to
one phase. It is **never committed** (git-ignored via `.gitignore`),
**single-phase**, and **phase-scoped rather than per-cycle**:

1. `gather` authors it when a phase first becomes the active `⬜` phase, then
   **no-ops while that phase stays in flight** — it re-reads no big doc and
   leaves both regions untouched.
2. `build` consumes the whole brief, prioritizing any open gaps in the feedback
   region, and never writes it.
3. `verify` on a **pass** deletes the phase's line + body file and the brief; on
   a **gap** it overwrites (never appends) the feedback region with only the
   currently-open gaps, and the brief persists into the next cycle.

Region ownership keeps the two writers from clobbering each other: the contract
region is gather's, the feedback region is verify's.

### Brief schema

```markdown
# Brief — Phase NN

## Objective
<one line, from the phase file>

## Realizes
<Decision id(s), noting "structural" where the phase realizes no ids>

## Decision file(s)
<path(s) to the DNN.md read>

## Design prose
<each realized Decision's full Decision statement, shape/signatures, and
rejected alternatives, verbatim, Verification section omitted>

## Ids to cover
(none — structural phase)

## Files to touch
<paths, service-root-relative>

## Dependency facts
<the fragment shape / run flags / committed parked paths the phase depends on>

## Done bar
- `bash -n run` exits 0.
- `mkdir -p tmp && nginx -p . -c nginx.conf -t` exits 0.
- <each of the phase's structural checks, verbatim, as an exact command with
  its stated expected output>

## Verify feedback — attempt N
- build commit observed: <sha>
- stall streak: <k>
- open gaps:
  - <named check> — <exact failing command> → <observed output> [file:line]
```

If a phase ever does carry ids, each `## Ids to cover` line starts at column 0
with the bare id, an em-dash, then the full requirement text on the same line,
so the phase's id set is extractable with
`grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md`.

## Why it converges, and the stall/blocked ladder

`verify` can neither halt the run nor advance a phase on a gap, so an incomplete
phase simply stays `⬜` and the loop re-attacks it next cycle — now with
`verify`'s grounded, command-level feedback in front of `build`, and without
`gather` re-reading the big docs. The persisted feedback also gives `verify`
cross-cycle memory, letting it distinguish slow convergence (the open-gap set
shrinking) from a true stall.

- **Progress** = the current open-gap set is a strict subset of the previous
  one. **A new build commit is never progress** and never resets the streak — a
  builder that cannot satisfy a bar keeps committing plausible rewordings, and a
  detector keyed on commit motion would read that churn as convergence.
- **Stall reset (streak = 3):** three consecutive attempts closing no gap means
  the accumulated brief may not be converging. `verify` logs
  `Phase NN STALLED …` to `~/.ralph/verify.log`, deletes the brief, leaves `⬜`,
  and returns `NEXT`; the next `gather` rebuilds the contract fresh from spec.
- **Blocked escalation (second stall on the same phase):** a rebuilt contract
  has already been tried, so the **done bar** is the fault and no amount of
  rebuilding fixes it. `verify` writes `project/loops/blocked.md` with the
  phase, the attempt count, the unsatisfied checks, and the exact command and
  observed output that will not go green; logs `Phase NN BLOCKED …`; deletes the
  brief; leaves `⬜`; and returns `NEXT`. The next `gather` sees the file and
  reports `DONE`.

Both branches stay inside the invariant that `verify` never halts and never
advances on a gap.

**What the operator does with a `blocked.md`:** read the recorded command and
its observed output, fix the phase's done bar in `project/` (the loop
cannot — `project/` is read-only to it), delete
`project/loops/blocked.md`, and restart the loop.
