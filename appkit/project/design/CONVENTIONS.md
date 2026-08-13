# appkit — Design Conventions

Shared facts every Decision leans on:

- **Language / toolchain:** Go **1.26**, single module `module appkit` rooted at
  `appkit/`. Consumed by every service via a committed
  `replace appkit => ../appkit`; never tagged.
- **Build / typecheck command:** `cd appkit && go build ./...` and `go vet ./...`.
  The isolated-module check (mirroring the production build) adds `GOWORK=off`.
- **Test command:** `cd appkit && go test ./...`. **"The suite is green"** means
  `go build ./...`, `go vet ./...`, `gofmt -l .` (no output), and `go test ./...`
  all succeed with zero failures, from `appkit/`.
- **Test layers.** The suite's testing vocabulary — the hermetic / composed /
  live / manual layers, what each may touch, the single `//go:build live`
  mechanism, and the ban on `t.Skip` outside live-tagged files — is the contract
  `root project/design/D23.md`, cited and not restated here. appkit's own layer
  facts (hermetic + composed in the default gate, a manual live-box runbook, no
  live layer) are recorded in D21.
- **Formatting:** `gofmt`-clean; `gofmt -l .` must print nothing.
- **Requirement-id tag location:** `R-XXXX-XXXX` ids live inline in Go test
  source, tagged verbatim in files matching `*_test.go`.
- **Cross-library correlation home.** The correlation id's header name, its
  Crockford-ULID minter/validator, and its **context key** live in the leaf
  package `eventplane/correlation`, owned by the eventplane workspace. appkit
  consumes it; it must not re-declare any of those, because eventplane can
  never import appkit and both libraries must read the id off the same context
  key. Its consumed surface is recorded in `project/research/research.md`.
- **Dependencies:** the D5–D9 packages use only the standard library plus the
  existing in-repo `eventplane` sibling (already a committed require/replace in
  `appkit/go.mod`). D10 adds one more **in-repo** replace-sibling, `registry`
  (`require registry v0.0.0` + `replace registry => ../registry`) — the static
  leaf address table. No new third-party dependency. Because a dependency's
  `replace` directives are not transitive, every module requiring appkit must
  mirror the registry replace (exactly as `eventplane` already forced); the
  sweep over the services that don't yet carry it is an operator step in
  `project/plan/README.md`, not appkit-phase work.
- **Testing substrate:** all D5–D9 behavior is provable in-process —
  `net/http/httptest` for HTTP, `t.TempDir()` for on-disk asset roots, injected
  `getenv` maps for config. A `t.TempDir()` tree is a real filesystem, so the
  web-root load/serve/missing-root claims are exercised against the substrate
  that can falsify them — no mocks stand in for the disk.
- **The on-box layout is a fixed external contract.** `bin/ship` bundles a
  service's `<svc>/share/` directory into `share/<version>/`, and `opsctl`
  swaps the `share/current` symlink atomically on deploy/rollback;
  `IKIGENBA_ROOT` roots the `/opt/<app>/` tree. appkit *consumes* these facts
  (D5 composes paths from them); it does not define or change them.
- **Cross-module collaborators (outside `appkit/`).** The repo-root shell
  scripts `bin/registry` and `bin/start` are not Go and not under `appkit/`.
  They are governed by **`bin/project/`** and tested by the **`bin/bintest`**
  Go module, which execs the real scripts under `bin/`'s own green gate; the
  repo-root aggregate gate is `suite_test.go` in the repo-root module, which
  fans `go test ./...` out across every `go.work` module (documented in the
  root `project/design/README.md` *Conventions*). No appkit Decision owns
  those scripts' behavior or mints ids for it.
  Where one appears in prose here (D4's retirement end state, D7's dev
  wiring), it is a boundary-crossing collaborator: context this chassis work
  relies on, proven in the suite workspace or by a live `bin/start` smoke,
  **not** by the appkit Go suite.
- **Additivity guard (D5–D10):** none of the new Spec fields, Router accessors,
  or packages may change the behavior of a Spec that doesn't use them. The
  pre-existing appkit test suite passing unchanged is the standing proof.
  **D11 is the deliberate exception:** the routing revision is a suite-wide
  hard cutover (no compatibility period), so D11 revises fixtures and the
  reflection wire surface rather than preserving them — additivity does not
  apply to it. **The structured-results revision (D8/D9 revised, D12) is the
  same kind of exception:** `JSONResult` is deleted and `ErrorResult`'s
  signature changes, so services do not compile until their adoption phases
  convert them — a deliberate singular move under the coordinated-deploy
  rule, not drift. appkit's own suite stays the green bar.
  **Telemetry (D14–D20) is additive to services:** a service that sets no new
  Spec field and imports neither new package compiles and behaves the same, and
  recording is on by default but never load-bearing — with nothing listening on
  the ingest endpoint every service behaves exactly as before. The one in-appkit
  break is `logging.RequestIDMiddleware` / `logging.WithRequestID`, replaced by
  the read-or-mint correlation middleware; every mount of them is inside appkit,
  so no service source changes.
- **Suite-contract proofs carried here.** Some `*_test.go` files in this module
  are tagged with requirement ids minted by the **umbrella** project (the repo
  root's `project/design/`) rather than by a Decision in this directory: the
  umbrella marks those ids `[proof: appkit]`, naming appkit as the one tree that
  carries the tagged test for a suite-wide contract it owns — appkit is the
  chassis every service runs on, so the behavior is proven once, here. Those
  tags are correct and expected; this design neither owns nor restates the
  contracts behind them, so a tree-local sweep that reads only
  `appkit/project/design/` will not find their home, and that is not a defect.
  The converse also holds: an umbrella id marked `[proof: per-service]` does
  **not** belong on a test here. appkit is a library, not a service, so it is
  never itself the adopter of a per-service contract; each service carries that
  proof in its own tree.
- **This design touches no schema and no `opsctl` code.** appkit is a library:
  it owns no service database and no outbox table, so the routing revision's
  outbox DDL change (D11) reaches services through their own migrations, never
  through appkit.
- **Build ordering across workspaces (external).** Several threads land in a
  fixed cross-tree order that appkit's own phases assume:
  - *Event-routing conformance (D11):* eventplane's plan phases 01–04 build
    first; appkit's conformance phase then precedes every service's own
    conformance phase (appkit is the hinge between eventplane and the services).
  - *Structured MCP results + loopback route class (D8/D9 revised, D12):* appkit
    builds first; every service's adoption phase (result-helper conversion,
    output schemas, `HandleLoopback`) and eventplane's inline-guard deletion
    follow; the suite deploys together.
  - *Suite telemetry (D14–D20):* eventplane's `correlation` and `observe` leaf
    packages must exist before appkit's phases build; the telemetry *service*
    (the ingest sink) is built after appkit, and its absence is a no-op by
    design — a service on this chassis runs identically with nothing listening
    on the ingest endpoint.
