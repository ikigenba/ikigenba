# gmail — Plan

**Authority: construction order.** This document and the `project/plan/`
directory it heads own the **build order** of **pending** work only. Completion
is deletion: the build loop removes the finished phase's `STATUS.md` line and
its `phase-NN.md` in the completion commit — the plan never holds finished work,
so it can never contradict a design that has since moved on. Construction
history lives in git, not here. To extend the work, update the product
(`project/product/README.md`) and design (`project/design/README.md` +
`project/design/`) **in place** to stay authoritative for the current state, then
**append** a new phase — a new `project/plan/phase-NN.md` body file plus a new
line in `project/plan/STATUS.md`, numbered from the `Next phase` counter, which
is bumped in the same edit. Phase numbers are never renumbered and never reused,
so a number names one phase forever even after its files are gone.

**Coverage invariant.** Every *current* design Verification id is either already
realized — its id appearing verbatim as a `// R-XXXX-XXXX` tag in a test file
that runs under the suite — or assigned to **exactly one** pending phase: no
current id unassigned, none split, none duplicated across pending phases.

**One phase = one coherent unit = one accumulating context.** Each phase is a
single coherent unit — almost always one Go package (`internal/<pkg>` or
`cmd/gmail`), or one shipped artifact (the nginx fragment, the doctrine doc) —
built in one accumulating context against the product and design. A phase reads
only the design Decisions it realizes and the *interfaces* (not the internals) of
the packages it depends on. This keeps every phase the size of a small standalone
task. The composition root (`cmd/gmail/main.go`) is the one shared file
legitimately touched by more than one phase — it is assembled incrementally as
surfaces come online; that is not a rewrite of a finished phase, only growth of a
wiring file.

**Done bar.** A phase is **done** when every Verification item — the
`R-XXXX-XXXX` ids — in the design Decisions it realizes (or the slice of those
ids assigned to it) is covered by a clearly-named, genuinely-asserting test and
the suite is green. "Green" is defined concretely in design's *Conventions*:
`cd gmail && go build ./...`, `cd gmail && go vet ./...`, `cd gmail && gofmt -l .`
(no output), and `cd gmail && go test ./...` all
succeed with zero failures. "Covered" means each listed id has a genuine test
exercising the behavior that Decision's Verification list describes — see each
`project/design/DNN.md` Verification section for what the id requires. A
**structural** phase (no ids, e.g. the docs purge) is done when its named content
check passes and the suite stays green.

## Layout

The plan is physically split so the build loop reads only what it needs:

- `project/plan/STATUS.md` — the manifest: the `Next phase` counter plus the
  **only** home of the pending markers (`⬜`), one line per **pending** phase in
  build order.
- `project/plan/phase-NN.md` — one body file per **pending** phase (zero-padded:
  `phase-01.md`, `phase-02.md`, …; sub-phases keep their suffix, e.g.
  `phase-07a.md`). A phase body carries **no** status token — status lives only
  in `STATUS.md`.
- `project/plan/README.md` — this file: the static, invariant rules above. It lists
  no phases and carries no status, so it never grows with the project.

**Completion is deletion, restated for this layout:** the build loop's only
mutation to either file is removing a finished phase's `STATUS.md` line together
with its `phase-NN.md`, in the completion commit. The `Next phase` counter is
never decremented and never touched by that mutation — it only ever moves
forward, when a new phase is appended.
