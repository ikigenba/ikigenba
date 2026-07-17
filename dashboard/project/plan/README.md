# dashboard — Plan (web pages restructure)

**Authority: construction order.** This document and the `project/plan/`
directory it heads own the **build order** of the dashboard's **pending** work
only. Completion is deletion: the build loop removes the finished phase's line
from `project/plan/STATUS.md` and its `project/plan/phase-NN.md` body file in
the completion commit — construction history lives in git, never in the spec.
To extend the work, update the product (`project/product/README.md`) and design
(`project/design/README.md` + `project/design/`) **in place** to stay
authoritative for the current state, then **append** a new phase here — a new
`project/plan/phase-NN.md` body file plus a new line in `project/plan/STATUS.md`,
numbered from the `Next phase` counter in `STATUS.md`. Phase numbers are never
renumbered and never reused, so a number names one phase forever even after its
files are gone.

**Coverage invariant.** Every *current* design Verification id is either already
**realized** — its id appearing verbatim as a tag in a test file that runs
under the suite — or assigned to **exactly one** pending phase: no current id
unassigned, none split, none duplicated across pending phases.

**One phase = one coherent increment = one accumulating context.** Each phase is a
single coherent unit sized for one subagent, built in one accumulating context
against the product and design. A phase reads only the design Decision(s) it
realizes (resolved through `STATUS.md` → `phase-NN.md` → the brief). For this
change every phase touches the `internal/server` package and the `ui/` templates —
the composition is a few files (`routes.go`, a new `profile.go`, the templates,
the view builders), assembled incrementally as the three pages come online; that
is growth of a shared wiring surface, not a rewrite of a finished phase. The
phases are **sequential**: the profile page must exist before token/grant
management can move onto it, and the landing must be cleared of that management
before the AGENTS.md truth is rewritten.

**Done bar.** A phase is **done** when every Verification item — the
`R-XXXX-XXXX` ids — in the design Decision(s) it realizes is covered by a
clearly-named, genuinely-asserting test and the suite is green. "Green" is defined
concretely in design's *Conventions*: `cd dashboard && go build ./...`,
`go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed
with zero failures. "Covered" means
each listed id has a genuine test exercising the behavior that Decision's
Verification list describes — see each `project/design/DNN.md` Verification section
for what the id requires. The doc-truth phase (D6) is verified by a text check on
`AGENTS.md`, not a Go test. Every phase's acceptance bar is a deterministic exit
condition, never a subjective judgment, never a self-referential/unsatisfiable
check.

## Layout

The plan is physically split so the build loop reads only what it needs:

- `project/plan/STATUS.md` — the manifest: the `Next phase` counter plus one
  line per **pending** phase in build order, and the **only** home of the `⬜`
  marker.
- `project/plan/phase-NN.md` — one body file per **pending** phase (zero-padded:
  `phase-01.md`, …; sub-phases keep their suffix, e.g. `phase-07a.md`). A phase
  body carries **no** status token — status lives only in `STATUS.md`.
- `project/plan/README.md` — this file: the static, invariant rules above. It lists
  no phases and carries no status, so it never grows with the project.

**Completion is deletion, restated for this layout:** the build loop's only
mutation to either file is removing a finished phase's `STATUS.md` line
together with its `phase-NN.md` body file, in the completion commit. The
`Next phase` counter is never decremented and never touched by the loop — only
`$seal-spec` bumps it, when it appends a new phase.
