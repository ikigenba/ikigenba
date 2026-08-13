# telemetry — Design Conventions

- **Language / toolchain:** Go (the repo targets `go 1.26`); module path
  `telemetry`, built on the shared `appkit` chassis over SQLite
  (`modernc.org/sqlite`, pure Go, no cgo). In-repo libraries are consumed via
  committed `replace` directives per `root project/design/D22.md` — including
  `eventplane`, which telemetry's **own source never imports** (it is neither a
  producer nor a consumer) but whose `require`+`replace` pair `go.mod` must
  carry as the necessary transitive consequence of the appkit chassis; the
  module path is written as a **plain literal**, like every module path in the
  suite. The checkable form of the no-participation claim is source-level:
  `grep -rn 'eventplane' --include='*.go' --exclude-dir=project .` from the
  telemetry tree root returns empty — never a condition on `go.mod`, which a
  conforming file cannot satisfy. One external module,
  `github.com/ikigenba/agentkit`, is consumed as a **tagged, test-only**
  dependency — imported from `_test.go` files alone, so no production binary
  links it — and exists solely so D8's conformance test can drive agentkit's
  real tool-schema gate. Its pinned version lives in `go.mod` and is not
  restated in the spec.
- **Build / typecheck command:** `cd telemetry && go build ./...` (and
  `go vet ./...`). The production binary is built by `bin/ship telemetry`
  (`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off`, version/commit stamped) —
  not invoked during normal development.
- **Test command:** `cd telemetry && go test ./...`. **"The suite is green"**
  means this command exits 0 with no failures, alongside a clean
  `go build ./...` and `go vet ./...`. Tests run against **real SQLite**
  (temp-file databases opened the same way `serve` opens the real one) — never a
  mocked store — with a deterministic injected clock. HTTP-level behavior is
  exercised over a **real loopback listener**, not `httptest.NewServer`'s
  in-memory shortcuts where the loopback property is what is under test.
- **Testing vocabulary:** the layer names and the rule fixing each layer by what
  a test may touch are the suite contract's, adopted by D10 and cited at
  `root project/design/D23.md`; this design restates none of them and uses no
  other testing vocabulary. telemetry has two layers — **composed**:
  `internal/e2e/`, which stands up the real composed service over a loopback port
  (including the restart-survival check), and the boot smoke in
  `cmd/telemetry/main_test.go`, which builds and runs the real binary against a
  temporary install tree; **hermetic**: everything else, including the real
  loopback listeners the transport tests bind. There is **no live layer** and no
  tree-local manual layer. Environmental preconditions beyond the Go toolchain:
  **none**. GOWORK mode: telemetry's own `telemetry/go.work` for development,
  `GOWORK=off` for the production build. These facts are declared in
  `telemetry/AGENTS.md` and re-checked by the gate (D10).
- **Requirement-id test-file glob:** `*_test.go` — every `// R-XXXX-XXXX` tag
  lives in a Go test file matching this glob.
- **Package layout:** `cmd/telemetry` (composition root: the `appkit.Spec` and
  the route wiring), `internal/record` (the record type, its JSON codec, and
  validation), `internal/db` (the `Store` plus embedded migrations),
  `internal/ingest` (the loopback ingest handler), `internal/retention` (the
  pruner), `internal/mcp` (the tool table and the embedded guide),
  `internal/e2e` (the end-to-end layer). No package imports `cmd/`.
- **DB / migrations:** schema lives in `internal/db/migrations/` as ordered,
  immutable SQL applied forward-only by the appkit runner. The greenfield set
  ships the suite bootstrap `001_schema_migrations.sql` verbatim plus the domain
  schema; every *later* change is a new file minted with
  `bin/create-migration telemetry <name>` (timestamped). Numbers are never
  hand-picked and committed migrations are never edited or deleted.
- **Loopback port:** read at composition time with
  `registry.MustPort("telemetry")`. The number appears in **no** Go source. The
  one place a literal port is written is `etc/nginx.conf`, which the app-layout
  contract requires be shipped verbatim and directly loadable; D6's guard test
  pins that literal to the registry so the two cannot drift.
- **Time / IO:** time enters the domain through a `Clock` interface
  (`Now() time.Time`); tests inject a deterministic clock. Wall-clock time is
  used for two things only — stamping `received_at` and computing the retention
  cutoff. A record's own `time` always comes from the reporter and is never
  substituted.
- **Timestamp normalization:** the protocol's `time` is RFC3339Nano, whose
  fractional part is variable-width, so it is **not** lexicographically ordered
  as text. Every timestamp is normalized on the way in to the fixed-width UTC
  form `2006-01-02T15:04:05.000000000Z` (Go layout
  `"2006-01-02T15:04:05.000000000Z07:00"` in UTC) before it is stored, compared,
  indexed, or put in a cursor. The normalized form is what the store, the
  indexes, the cursors, and the query results all speak; the tool surface renders
  it back out as-is.
- **Error vocabulary:** tool errors use `appkit/mcp`'s closed codes
  (`validation`, `not_found`, `conflict`, `too_large`, `source_unavailable`,
  `internal`); the ingest surface, which is not MCP, answers with HTTP status
  codes only.
- **Append-only by construction:** the `Store` exposes exactly one write path
  for records (an idempotent insert) and exactly one delete path (the retention
  prune, taking a cutoff). There is no update method, no delete-by-id, and
  nothing on the MCP surface reaches either write path. This is a structural
  property of the store's interface, not a runtime check.
- **Cross-workspace dependency and build order.** telemetry is a greenfield
  service inside the suite mono-repo, and its `project/` governs only the
  `telemetry/` tree. It builds on seams that land in **other** workspaces first,
  through those workspaces' own specs and build loops, in the suite's settled
  execution order: the repo-root protocol doc and the `registry` row (which gives
  `registry.MustPort("telemetry")` a value), then `appkit` (the correlation
  middleware, the telemetry recorder, MCP-dispatch and lifecycle instrumentation,
  and the per-route recording opt-out `/ingest` uses) and `eventplane`, and only
  then this service. No phase here edits, tests, or builds anything outside
  `telemetry/`; where a phase depends on one of those seams it consumes it as an
  existing library API (see `project/research/` §2–§3). If a needed seam is absent
  when a phase runs, that is a blocked phase to report, never a license to reach
  into a sibling workspace.
