# telemetry — Plan

**Authority: construction order.** This document — and the `project/plan/`
directory it heads — owns the build order of the telemetry service's **pending**
work only. It does **not** restate *why* (that is `project/product/README.md`)
or *how/proof* (that is `project/design/`); it orders design's Decisions into
dependency-respecting phases. **Completion is deletion**: when a phase finishes,
the build loop removes its `STATUS.md` line and its `phase-NN.md` in the
completion commit — the plan never holds finished work, so it can never
contradict a design that has since moved on. Construction history lives in git,
not here. To extend the plan, first update product and design *in place*, then
**append** a new phase — a new `project/plan/phase-NN.md` body plus a new line
in `project/plan/STATUS.md`, numbered from the `Next phase` counter — never
renumber, never reuse a number.

## Coverage invariant

Every *current* design Verification id is either already **realized** — its id
appearing verbatim as a tag in a test file that runs under the suite — or
assigned to **exactly one** pending phase: no current id unassigned, none split,
none duplicated across pending phases.

## One phase = one package = one build-turn context

Each phase is a single coherent unit of work — almost always one Go package —
scoped to that unit's design Decisions and the *interfaces* (not the internals)
of the packages it depends on, and sized so the build loop can carry it in one
fresh build-turn context and ideally finish it in a turn or two. The loop does
*not* build a phase in one long accumulating context — size to a single build
turn, not an imagined single sitting. Sizing a phase as large as cleanly fits
one turn is good: fewer cycles, less context churn. Where a phase must
establish a structural seam that design assigns to a different Decision (the
`Clock` interface, say), the seam is born in the phase that first needs it; the
*verification ids* of the owning Decision are realized by the phase named in
`STATUS.md`.

## Cross-workspace dependency

telemetry is a greenfield service inside the suite mono-repo, and its
`project/` governs only the `telemetry/` tree. It builds on seams that land in
**other** workspaces first, through those workspaces' own specs and build loops,
in the suite's settled execution order: the repo-root protocol doc and the
`registry` row (which gives `registry.MustPort("telemetry")` a value), then
`appkit` (the correlation middleware, the telemetry recorder, MCP-dispatch and
lifecycle instrumentation, and the per-route recording opt-out `/ingest` uses)
and `eventplane`, and only then this service. No phase here edits, tests, or
builds anything outside `telemetry/`; where a phase depends on one of those
seams it consumes it as an existing library API (see `project/research/`
§2–§3). If a needed seam is absent when a phase runs, that is a blocked phase to
report, never a license to reach into a sibling workspace.

## Done bar

A phase is **done** when every Verification item — each `R-XXXX-XXXX` id — in
the design Decision(s) it realizes (or the slice of those ids assigned to it) is
covered by a clearly-named, id-tagged test, and the suite is green. "Green" is
defined in design's *Conventions* (`project/design/README.md`): `cd telemetry`
and then `go build ./...`, `go vet ./...`, and `go test ./...` all exit 0 with
no failures, with storage tests run against real temp-file SQLite and transport
tests over a real loopback listener — never a mocked store — and with a
deterministic injected clock. "Covered" means what each Decision's Verification
list says it means: a genuine test exercising the named behavior against the
substrate that Decision specifies. A skipped test is a **gap, not a pass**: a
phase whose ids are only reachable when some condition holds must make that
condition hold, not skip. Every phase's acceptance bar is a deterministic exit
condition, never a subjective judgment, never a self-referential or
unsatisfiable check.

## Layout

The plan is **split for addressability** so the build loop reads only the one
phase it is working on, never the whole queue:

- `project/plan/STATUS.md` — the manifest: the `Next phase` counter plus the
  **only** home of the pending marker (`⬜`) for each pending phase.
- `project/plan/phase-NN.md` — one body file per **pending** phase, zero-padded
  (`phase-01.md`, `phase-02.md`, …; a sub-phase keeps its suffix, e.g.
  `phase-03a.md`). The body carries **no** status token.
- `project/plan/README.md` — this file: the static, invariant rules. It lists no
  phases and carries no status, so it never grows with the project.

Completion-is-deletion, restated for this layout: the build loop's only
mutations are removing a finished phase's `STATUS.md` line together with its
`phase-NN.md` in the completion commit. The counter is never decremented and
never touched except to bump it when a new phase is appended.
