# registry — Design

**Authority: shape and its proof.** This directory owns *how* `registry` is built
and *how each behavior is proven*. The product (`project/product/README.md`) owns
the *why* and the caller-facing promises; design states the **exact, checkable
form** of those promises and never re-declares the why. Design *uses* the
product's contractual constants (`dashboard = 3000`, loopback host `127.0.0.1`) by
value but does not own them. This is the **single, current** statement of the
architecture: when a decision changes its file is rewritten in place (never
stacked); construction history lives in git, not here.

## Requirement ids

- Each Decision ends with a **Verification** list: the concrete behaviors that
  decision requires.
- Every Verification item carries a **minted id** of the form `R-XXXX-XXXX` — a
  stable, unique handle for that one behavior.
- The ids live inline in these Verification lists and **nowhere else** — there is
  no separate requirements document.
- Design's responsibility for ids ends at **minting them into this doc**. How
  coverage is measured, what counts as covered, and when work is "done" are not
  design's concern and are owned by downstream phases.

## Conventions

Shared facts every Decision leans on.

- **Language / module.** Go (`go 1.26`); module path `registry`, a new standalone
  module at the repo root. It is a shared library and is **not** release-versioned.
- **Package.** A single flat package `registry` at the module root (files
  `registry.go`, `registry_test.go`) — no `internal/`, no `cmd/`. It is small
  enough that a nested layout adds only ceremony.
- **Zero third-party dependencies.** The module imports **only** the Go standard
  library. This is load-bearing: it is what lets `opsctl` (and anyone else) adopt
  `registry` without inheriting the chassis's dependency graph. The mechanical
  check is that `go list -deps` reports no non-standard import paths (see D1).
- **Build / typecheck command.** `GOWORK=off go build ./...` from the module root
  (`registry/`). Forcing `GOWORK=off` matches the deterministic production build
  and proves the module resolves standalone, without the workspace.
- **Test command.** `GOWORK=off go test ./...` from the module root.
- **"The suite is green"** means: `GOWORK=off go build ./...` succeeds **and**
  `GOWORK=off go test ./...` passes with no failures and no `SKIP`, from
  `registry/`.
- **Test placement.** Package-local `registry/*_test.go`, in package `registry`,
  co-located with the code they exercise and named for the behavior. There is no
  separate integration-test home and there are **no** per-phase or root-level test
  files.
- **Purity.** The whole package is pure: compile-time data and total functions
  over it. No I/O, no environment reads, no clock, no randomness — so every claim
  below is falsifiable by a plain in-process test against the real code (there is
  no external substrate to stub, and none is needed).

## Layout

The design is **split for addressability** so a build phase reads only the one
Decision it realizes:

- `project/design/INDEX.md` — the manifest: each Decision → its file, plus a
  sorted `R-id → Decision/file` reverse map. Regenerated whenever a Decision is
  added or its Verification ids change.
- `project/design/DNN.md` — one self-contained file per Decision (zero-padded
  filename `D01.md`, `D02.md`, …; referenced in prose and the plan as `D<N>`).
- `project/design/README.md` — this spine: static cross-cutting facts only, no
  per-Decision detail.

Design is **rewritten in place**, not append-only (history lives in the plan): a
changed Decision is rewritten in its `DNN.md` and `INDEX.md` is regenerated; a new
Decision adds a `DNN.md` and an INDEX entry.
