# dropbox — Plan (landing page)

**Authority: construction order.** This document and the `project/plan/`
directory it heads own the **build order** of **pending** dropbox work only.
Completion is deletion: the build loop removes the finished phase's line from
`project/plan/STATUS.md` and its `project/plan/phase-NN.md` body file in the
completion commit — the plan never holds finished work, so it can never
contradict a design that has since moved on. History lives in git, not here. To
extend the work, update the product (`project/product/README.md`) and design
(`project/design/README.md` + `project/design/`) **in place** to stay
authoritative for the current state, then **append** a new phase: a new
`project/plan/phase-NN.md` body file plus a new line in `project/plan/STATUS.md`,
numbered from the `Next phase` counter — never renumber, never reuse a number.

**Coverage invariant.** Every *current* design Verification id is either already
**realized** — its id appearing verbatim as a tag in a test file that runs under
the suite — or assigned to **exactly one** pending phase: no current id
unassigned, none split, none duplicated across pending phases.

**One phase = one coherent unit = one build-turn context.** Each phase is a
single coherent unit — almost always one Go package (`internal/<pkg>` or
`cmd/dropbox`), or one shipped artifact (the nginx fragment, the doctrine doc) —
sized so the build loop can carry it in one fresh build-turn context and ideally
finish it in a turn or two. A phase reads only the design Decisions it realizes
and the *interfaces* (not the internals) of the packages it depends on. This
keeps every phase the size of a small standalone task. The composition root
(`cmd/dropbox/main.go`) is the one shared file legitimately touched by more than
one phase — it is assembled incrementally as surfaces come online; that is not a
rewrite of a finished phase, only growth of a wiring file.

**Done bar.** A phase is **done** when every Verification item — the
`R-XXXX-XXXX` ids — in the design Decisions it realizes (or the slice of those
ids assigned to it) is covered by a clearly-named, genuinely-asserting test and
the suite is green. "Green" is defined concretely in design's *Conventions*:
`cd dropbox && go build ./...`, `cd dropbox && go vet ./...`,
`cd dropbox && gofmt -l .` (no output), and `cd dropbox && go test ./...` all
succeed with zero failures. "Covered" means
each listed id has a genuine test exercising the behavior that Decision's
Verification list describes — see each `project/design/DNN.md` Verification
section for what the id requires. A **structural** phase (no ids, e.g. the docs
purge) is done when its named content check passes and the suite stays green.
Every phase's acceptance bar is deterministic exit conditions, never a subjective
judgment, never a self-referential/unsatisfiable check.

## Layout

The plan is physically split so the build loop reads only what it needs:

- `project/plan/STATUS.md` — the manifest: the `Next phase` counter plus one line
  per **pending** phase in build order, and the **only** home of the `⬜`
  marker.
- `project/plan/phase-NN.md` — one body file per **pending** phase (zero-padded:
  `phase-01.md`, `phase-02.md`, …). A phase body carries **no** status token —
  status lives only in `STATUS.md`.
- `project/plan/README.md` — this file: the static, invariant rules above. It
  lists no phases and carries no status, so it never grows with the project.

**Completion is deletion, restated for this layout:** the build loop's only
mutations to this directory are removing a finished phase's `STATUS.md` line
together with its `phase-NN.md` body file. The `Next phase` counter is never
decremented and never touched by the build loop — only `$seal-spec` bumps it, on
appending a new phase.
