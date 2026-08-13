# registry — Design Conventions

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
- **Test layers.** The suite's testing vocabulary — the hermetic / composed /
  live / manual layers and what each may touch, the single `//go:build live`
  mechanism, and the ban on `t.Skip` outside live-tagged files — is the contract
  `root project/design/D23.md`, cited and not restated here. Given the purity
  above, every test in this tree is **hermetic**; registry has no composed, live,
  or manual layer. D4 records registry's full declaration, including its
  `GOWORK=off` mode and its lack of any precondition beyond the Go toolchain.
