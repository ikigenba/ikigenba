# bin — Design

**Authority: shape and its proof.** This directory owns *how* the repo-root
`bin/` operator tooling is built — the scripts, their flags and seams, the
artifacts they emit, and the split between the deliberately-untested
orchestrating tier and the automatically-proven layout-reading tier — and *how
each behavior is proven*. The product (`bin/project/product/README.md`) owns the
*why* and the user-facing promises. Design uses the suite's contractual
constants — the `v`-prefixed SemVer 2.0 version identity, the release bundle's
tier layout, the on-box install tree, the env/manifest contract, the per-app
secrets parameter shape — **by value** and does not own them: each is an
umbrella Decision at the repo root, **cited by path and never restated here**
(see *Suite contracts this tree conforms to* below). This is the **single,
current** statement of the architecture: when a Decision changes, its `DNN.md`
is rewritten in place — Decisions are never stacked. Construction history lives
in git.

## Requirement ids

- Each Decision ends with a **Verification** list: the concrete behaviors that
  Decision requires.
- Every Verification item carries a **minted id** of the form `R-XXXX-XXXX` — a
  stable, unique handle for that one behavior, minted with `idgen -n <count> -p R`
  and never hand-written, invented, or renumbered.
- The ids live inline in those Verification lists and **nowhere else** — there
  is no separate requirements document.
- Design's responsibility for ids ends at **minting** them here. How coverage is
  measured, what counts as covered, and when the work is "done" are downstream
  phases' concern and are deliberately not specified in this directory.
- Most of this tree mints **no ids by decision** — see *Conventions* — so a
  Decision here stating "no ids" is the normal case, not an omission.
- Because ids live only in Decision files, the coverage denominator for this
  tree is `bin/project/design/D*.md`. The `R-XXXX-XXXX` shown above is the
  *shape* of an id, not one; this spine mints nothing.

## Conventions

Shared facts every Decision leans on:

- **Two tiers, one rule.** `bin/` is **bash orchestration** — it builds, copies,
  launches, and calls remote APIs, none of which a hermetic test can stand in
  for faithfully. That tier is **deliberately untested** (the former `bin/test` +
  `bin/*.test.sh` tier was removed in commit `019a99ee` because nothing
  automated ran it, and a test outside the gate rots) and is verified **once,
  manually, outside the build loop**, when created or changed. The **exception**
  is the *layout readers* — the tooling that reads the suite's on-disk manifest
  layout rather than orchestrating something — which is covered automatically
  (D04). A Decision in the untested tier mints no ids and its phases carry
  deterministic **structural** exit conditions instead.
- **Language / toolchain.** Bash (`#!/usr/bin/env bash`, `set -euo pipefail`)
  for every script; Go 1.26 for the one test module, `bin/bintest` (module path
  `bintest`), wired into the repo-root `go.work` so it needs no separate runner.
  The tooling itself shells out to `go`, `git`, `tar`, `scp`/`ssh`, `jq`, and
  `aws`.
- **Build / typecheck command.** `go build ./bin/bintest/...` from the repo root
  (workspace mode). `bin/bintest` is a test-only package, so its compile check
  is `go test ./bin/bintest/...` succeeding.
- **Test command — the green gate.** `go test ./bin/bintest/...` from the repo
  root, in workspace mode. **"This tree is green" means that command exits 0.**
  Because `bin/bintest` is a `go.work` member, the same tests also run under the
  repo-wide `go test ./...`, so the tree's green is a subset of the suite's and
  needs no additional runner.
- **Test-file glob.** Requirement-id tags live in **`bin/bintest/*_test.go`**,
  each as a comment line immediately above the test that realizes it. That glob
  is the coverage denominator's numerator for this tree.
- **Tests exec the real scripts.** A `bin/bintest` test always invokes the
  actual script under `bin/`, resolved from the package directory's repo root —
  never a Go reimplementation of the script's logic. The script is the only
  substrate that can falsify a claim about the script.
- **Hermetic, unprivileged, network-free.** Tests run with no box, no ports, no
  secrets, and no network, against fixtures in `t.TempDir()`. Any seam a script
  needs to be testable is an **env override or an inert flag** that is a no-op
  when unused, so the operator's ordinary invocation is unchanged. A claim that
  needs a real box, a real remote copy, or a live cloud API is verified by an
  **out-of-gate manual check**, never as a `t.Skip`-gated test — a skipped
  requirement test counts as uncovered, so it is never the in-gate proof.
- **Uniform, name-parameterized.** Every command takes the service name as its
  only per-service input and derives everything else — the port from the
  registry, the version from `<svc>/VERSION`, the environment from
  `<svc>/etc/manifest.env` and `<svc>/.envrc`. **No port literal, version
  literal, or per-service branch appears in any script.**

## Suite contracts this tree conforms to

Cited by path, **never restated** — a local restatement is drift by
construction. Each lives in the umbrella project at the repo root:

- **`project/design/D01.md`** — the `/opt/<svc>/` install tree.
- **`project/design/D02.md`** — the versioned release bundle: `libexec/` binary,
  `etc/<v>/`, `share/<v>/`, and the symlink swap.
- **`project/design/D03.md`** — the `v`-prefixed SemVer 2.0 version identity and
  its ordering.
- **`project/design/D10.md`** — the on-box `stage` / `deploy` / `rollback` /
  `prune` orchestration this tree's artifacts are consumed by.
- **`project/design/D11.md`** — the env contract: the portable authored
  `manifest.env` and `IKIGENBA_ROOT` path composition.
- **`project/design/D12.md`** — the per-app secrets parameter: its path, its
  value shape, and the box-side launcher that consumes it.

Conformance is the default. Where this tree deviates from an umbrella contract,
the local Decision that does so names the umbrella Decision it departs from and
the reason; silence means conformance.

## Layout

The design is **split for addressability** so a build phase reads only the one
Decision it realizes:

- `bin/project/design/INDEX.md` — the manifest: each Decision → its file, plus a
  sorted `R-id → Decision/file` reverse map. Regenerated whenever a Decision is
  added or its Verification ids change.
- `bin/project/design/DNN.md` — one self-contained file per Decision
  (zero-padded `D01.md`, `D02.md`, …; referenced in prose and the plan as
  `D<N>`).
- `bin/project/design/README.md` — this spine: cross-cutting facts only, no
  per-Decision detail.

Design is **rewritten in place**: a changed Decision is rewritten in its
`DNN.md` and `INDEX.md` is regenerated; a new Decision adds a `DNN.md` and an
INDEX entry.
