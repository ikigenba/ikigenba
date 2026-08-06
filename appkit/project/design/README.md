# appkit — Design

**Authority: shape and its proof.** This document and the `project/design/`
directory it heads own *how* appkit's ralph-governed surfaces are shaped and
*how each behavior is proven*. The product (`project/product/README.md`) owns
the *why* and the user-facing promises; design states the **exact, checkable
form** of those promises and never re-declares the why. This design is the
**single current** statement, rewritten in place (stale decisions removed, not
stacked); construction history lives in git, not here.

> **Scope.** This design covers three threads:
>
> 1. **The manifest read-path** (D1–D4, built): every manifest reader resolves
>    *through* the per-app `etc/current` deploy symlink; the local dev layout
>    mirrors the box; the stable sibling path is retired.
> 2. **The uniform service chassis surfaces** (D5–D9, built): the on-disk
>    web-asset root (config resolution D5, the `appkit/web` package D6, the
>    chassis integration D7) and the chassis MCP surface (the JSON-RPC
>    transport D8, the standard `health`/`reflection` tools D9).
> 3. **Chassis-owned consumer loops** (D10, active): the declared
>    `Spec.Consumers` table from which the chassis derives the manifest
>    `CONSUMES=`, the reflection subscriptions, and the running
>    `eventplane/consumer` loops — feed-URL/`From` env resolution included.
> 4. **Event-routing conformance** (D11 + the D9 rewrite, active): appkit
>    compiles and plumbs the suite's routing revision
>    (`docs/event-routing-design.md`, specified in
>    `eventplane/project/design/` D1–D4) — `Spec.Events` carries the
>    family-based `outbox.Registry`, the chassis `reflection` tool speaks
>    kinds (D9, rewritten in place), and every other eventplane coupling
>    (feed pass-through, test fixtures) cuts over. **Externally ordered:**
>    eventplane's plan phases 01–04 build first; appkit's conformance phase
>    then precedes every service's own conformance phase (appkit is the hinge
>    between eventplane and the services).
> 5. **Structured MCP results + the loopback route class** (D8/D9 revised +
>    D12, active): appkit implements the suite's structured-results contract
>    (`docs/structured-mcp-design.md`) — protocol `2025-06-18`,
>    `Tool.OutputSchema`, `StructuredResult` (replacing `JSONResult`, deleted),
>    the typed `ErrorCode` vocabulary in a re-signed `ErrorResult`, the
>    `-32603` handler-fault mapping, structured `health`/`reflection` — and
>    hoists the loopback-only guard into a chassis route class
>    (`LoopbackOnly` / `Router.HandleLoopback`, `/feed` mount wrapped), with
>    the predicate narrowed to `X-Forwarded-Proto` per the caller-asserted
>    identity contract. **Externally ordered:** appkit builds first; every
>    service's adoption phase (result-helper conversion, output schemas,
>    `HandleLoopback`) and eventplane's inline-guard deletion follow; the
>    suite deploys together.
> 6. **Suite telemetry** (D14–D20, active): the chassis becomes the suite's
>    universal recording point. appkit consumes the shared leaf package
>    `eventplane/correlation` and turns its request middleware into a
>    read-or-mint correlation point (D14, which also fixes `logging.NewULID`
>    to Crockford base32); adds the `appkit/telemetry` recorder — a bounded
>    ring buffer batching fire-and-forget POSTs to the telemetry service's
>    ingest endpoint (D15) — and the allowlist record shape with its
>    capture-under-threshold param encoder (D16); instruments both inbound
>    seams, `dispatchTool` and the middleware chain (D17); emits lifecycle
>    records and wires the `eventplane/observe` hooks in `serve` (D18); and
>    provides `appkit/httpclient`, the shared instrumented outbound client
>    that propagates the correlation id to loopback peers only (D19); and the
>    chain-root helpers plus the `Router.Recorder()` / `Router.HTTPClient()`
>    accessors through which service code reaches all of it (D20).
>    **Externally ordered:** eventplane's `correlation` and `observe` leaf
>    packages must exist before appkit's phases build; the telemetry
>    *service* (the ingest sink) is built after appkit, and its absence is a
>    no-op by design — a service on this chassis runs identically with nothing
>    listening on the ingest endpoint.
>
> appkit's other pre-existing surfaces — the verb dispatcher, migrations, the
> loopback server's PRM/health/feed routes, the producer/worker seams — are
> settled prior art this design extends and does **not** reopen. Every D5–D10
> change is **additive**: a service that sets none of the new Spec fields and
> imports none of the new packages compiles and behaves exactly as before.

## Requirement ids

- Each Decision ends with a **Verification** list: the concrete behaviors that
  decision requires.
- Every Verification item carries a **minted id** of the form `R-XXXX-XXXX` — a
  stable, unique handle for that one behavior.
- The ids live inline in these Verification lists and nowhere else — there is **no
  separate requirements document**.
- Design's responsibility for ids ends at minting them into this doc. How coverage
  is measured and when the work is "done" are **not** design's concern —
  downstream phases own that.

## Conventions

Shared facts every Decision leans on:

- **Language / toolchain:** Go **1.26**, single module `module appkit` rooted at
  `appkit/`. Consumed by every service via a committed
  `replace appkit => ../appkit`; never tagged.
- **Build / typecheck command:** `cd appkit && go build ./...` and `go vet ./...`.
  The isolated-module check (mirroring the production build) adds `GOWORK=off`.
- **Test command:** `cd appkit && go test ./...`. **"The suite is green"** means
  `go build ./...`, `go vet ./...`, `gofmt -l .` (no output), and `go test ./...`
  all succeed with zero failures, from `appkit/`.
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
  They are governed and tested by the **suite-level workspace** (the repo-root
  `project/`, whose `bintest` module execs the real scripts under the root
  green gate) — no appkit Decision owns their behavior or mints ids for it.
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
- **This design touches no schema and no `opsctl` code.** appkit is a library:
  it owns no service database and no outbox table, so the routing revision's
  outbox DDL change (D11) reaches services through their own migrations, never
  through appkit.

## Testing strategy (D5–D20)

- **`appkit/config`** is pure over its injected `getenv`; www-root resolution is
  table-tested exactly like the existing DB-path composition.
- **`appkit/web`** is tested against real temporary directories: tests write
  template and asset files into `t.TempDir()`, load them, and drive the returned
  handlers with `httptest`. Failure paths (missing root, missing template) are
  real filesystem states.
- **Server integration (D7)** is tested at the `server.New` seam the existing
  server tests use: build `server.Options` with and without a loaded site, and
  assert route presence/absence and served bytes through the real mux.
- **`appkit/mcp`** is tested through the real `ServeHTTP` JSON-RPC seam — the
  same harness style every service's `tools_test.go` uses — with a test tool
  table whose handlers record their inputs. The standard tools are driven
  through the same seam with real `outbox.Registry` / `consumer.Subscription`
  values.
- **Consumer loops (D10)** split the same way `appkit/config` and the server
  do: env resolution is pure and table-tested over injected `getenv` maps; the
  loop itself is proven end-to-end against a **real** SSE feed served by
  `httptest` over a **real** `t.TempDir()` SQLite database, because cursor
  independence and delivery ordering are exactly the claims a stub cannot
  falsify. The manifest/reflection derivations reuse the existing manifest-emit
  and mcp-seam harnesses.
- **Routing conformance (D11 + revised D9)** is proven on the real eventplane:
  the gating and wire claims run `feed.Start` over a real `t.TempDir()` SQLite
  database (`modernc.org/sqlite`) and drive the returned `Producer.Handler`
  through `httptest`; the reflection claims go through the D8 `ServeHTTP` seam
  with real family-shaped `outbox.Registry` values. Converted consumer-test
  fixtures frame `kind`/`subject` envelopes with canonical-key `event:` lines.
- **Structured results (revised D8/D9)** stay on the D8 `ServeHTTP` seam:
  result-shape claims parse the JSON-RPC response and compare
  `structuredContent` against the parsed text block (the no-drift property
  asserted between the two renderings, never against a string fixture);
  descriptor claims assert `outputSchema` presence/absence keys in
  `tools/list`.
- **The loopback route class (D12)** is proven through `httptest` against the
  real handler and the real `server.New` mux (recording inner handlers for
  the not-invoked claims); the `/feed`-mount claim drives the real route
  table with `Options.Feed` set.
- **Telemetry (D14–D20) is deliberately not a mock-only capability.** The
  claims that hinge on a real external contract — records actually arriving
  over a real loopback HTTP ingest, a dead sink not blocking the caller, a
  correlation header actually reaching (or not reaching) a peer over the wire,
  a correlation id surviving a real SSE hop — are proven against **real
  substrates**: `httptest.NewServer` as a live in-process ingest sink and as a
  live outbound peer, a genuinely **closed** TCP port for the refused-connection
  and drop-not-block claims, and a real `t.TempDir()` SQLite database with a
  real `feed.Start` feed for the publish/consume hops (the D10/D11 substrate).
  A sink that accepts whatever it is handed proves nothing about delivery, so
  no telemetry id is satisfied by one. The pure parts — the record's JSON
  shape, the param encoder's thresholds and elision order, the digest format,
  env resolution — stay table-tested over real `encoding/json`,
  `crypto/sha256`, and injected `getenv` maps.

## Layout

The design is **split for addressability** so the build loop reads only the one
Decision a phase realizes:

- `project/design/INDEX.md` — the manifest: each Decision → its `DNN.md`, plus a
  sorted `R-id → Decision/file` reverse map (the grep target for id lookup).
- `project/design/DNN.md` — one self-contained file per Decision (zero-padded
  `D01.md`, …; referenced in prose and the plan as `D<N>`).
- `project/design/README.md` — this spine: static cross-cutting facts only.

**New packages (D5–D9).** This work adds two packages to the module:
`appkit/web` (template loading + rendering + static serving over an on-disk
root) and `appkit/mcp` (the JSON-RPC MCP transport + standard tools). It extends
two existing seams: `appkit/config` (www-root resolution) and the root
`appkit`/`appkit/server` pair (the `Spec.WWW` field, site loading at serve, the
auto-mounted static route, the `Router.WWW()` accessor).

**Consumer seam (D10).** No new package: the `Consumer` type and `Consumers`
Spec field live in the root `appkit` package beside `Workers`; the
feed-URL/`From` env resolution extends `appkit/config`; the manifest and
reflection derivations extend the existing emit/tool paths.

**Structured results + loopback class (revised D8/D9, D12).** No new package:
the result contract lives in `appkit/mcp` (protocol constant, `OutputSchema`
field, `StructuredResult`/`ErrorCode`/`ErrorResult`, dispatch error mapping,
standard-tool schemas); the route class extends `appkit/server`
(`LoopbackOnly`, `Router.HandleLoopback`, the wrapped `/feed` mount).

**Telemetry (D14–D20).** This work adds two packages to the module:
`appkit/telemetry` (the record shape, the param encoder, the digest helper, and
the buffering/batching recorder with its ingest client) and
`appkit/httpclient` (the instrumented outbound `http.Client`/`RoundTripper`).
It extends four existing seams: `appkit/logging` (the Crockford ULID fix and
the read-or-mint correlation middleware that replaces `RequestIDMiddleware`),
`appkit/server` (`Options.Recorder`, `Options.RecordExclude`, the recording
middleware, the response-digesting `statusRecorder`), `appkit/mcp`
(`Options.Recorder`, `Tool.SensitiveParams`, `dispatchTool` recording), and the
root `appkit`/`appkit/config` pair (`Spec.TelemetryExclude`, the resolved
`TelemetryIngestURL` / `TelemetryEnabled`, and the recorder, lifecycle, and
`eventplane/observe`-hook wiring in `runServe`). D20 adds the service-facing
surface every other workspace consumes: `Recorder.StartRoot`/`StartChain` in
`appkit/telemetry`, and `Router.Recorder()` / `Router.HTTPClient(timeout)` on
`appkit/server`.

Design is rewritten in place, not append-only (construction history lives in git): a
changed Decision is rewritten in its `DNN.md` and `INDEX.md` is regenerated; a new
Decision adds a `DNN.md` and an INDEX entry.
