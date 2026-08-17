# bin — Design Conventions

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
  The tooling itself shells out to `go`, `git`, `tar`, `scp`/`ssh`, `jq`,
  `aws`, and — for the lint gate only — **golangci-lint at exactly the pinned
  version the suite lint contract names** (`root project/design/D30.md`,
  currently v2.12.2). The pin is the contract's; this tree re-declares no
  version of its own, and `bin/lint` refuses any other installed version.
- **Build / typecheck command.** `go build ./bintest/...` from the service root
  (`bin/`, the loop's working directory), in workspace mode. `bin/bintest` is a
  test-only package, so its compile check is `go test ./bintest/...` succeeding.
- **Test command — the green gate.** `go test ./bintest/...` from the service
  root, in workspace mode (equivalently `go test ./bin/bintest/...` from the
  repo root, the AGENTS.md default gate). **"This tree is green" means that
  command exits 0.**
  Because `bin/bintest` is a `go.work` member, the same tests also run under the
  repo-wide `go test ./...`, so the tree's green is a subset of the suite's and
  needs no additional runner.
- **Test-file glob.** Requirement-id tags live in **`bintest/*_test.go`**,
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
- **Testing language (suite contract).** This tree uses the suite's testing
  vocabulary and rules from `root project/design/D23.md`, cited and never
  restated; D7 records the mapping and the declaration. In this tree's terms:
  every `bin/bintest` test is **hermetic**, the deliberately-untested bash tier
  is the **manual** layer, and there is no composed and no live layer — the tree
  commits **no** `//go:build live` file and defines no `-tags live` invocation.
  `t.Skip` and its variants appear nowhere in it.
- **Environmental preconditions.** The Go toolchain, plus — for the lint-gate
  tests only (D9) — golangci-lint at the contract's pinned version on `PATH`.
  A missing or wrong-version binary makes those tests **fail loudly naming the
  pin**, never skip (the skip ban holds): the gate's whole point is that the
  pinned tool is present wherever the suite is verified, and `doctor`-style
  setup is the operator's, out of gate. Everything else a test execs is
  committed in this repo, and the module facts D6 reads come from
  `go mod`/`go work` subcommands of the Go toolchain.
- **GOWORK mode.** Workspace. `bin/bintest` is a `go.work` member and resolves
  its sibling modules through it; `GOWORK=off` would break D5 and D6 by
  construction. The suite deliberately does not unify this across trees.
- **Uniform, name-parameterized.** Every command takes the service name as its
  only per-service input and derives everything else — the port from the
  registry, the version from `<svc>/VERSION`, the environment from
  `<svc>/etc/manifest.env` and `<svc>/etc/env.list`. **No port literal, version
  literal, or per-service branch appears in any script.**
