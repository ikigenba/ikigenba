# telemetry — Design

**Authority: shape and its proof.** This directory owns *how* the telemetry
service is built and *how each behavior is proven* — its seams, public
interfaces, naming, type definitions, data model, and the test plan.
`project/product/README.md` owns the *why* and the user-facing promises; design
states the **exact, checkable form** of those promises and never re-declares the
why. Design **uses** the product's contractual constants by value (service name
`telemetry`, mount `/srv/telemetry/`, starting version `v0.1.0`, the 90-day
default retention window) but does not own them; likewise it consumes the suite
telemetry protocol's record model, thresholds, and chain-id format as external
facts recorded in `project/research/research.md`. This is the **single, current**
statement of the architecture — when a Decision changes, its `DNN.md` is
rewritten in place (stale decisions removed, not stacked); the construction
history of how it got here lives in git, not here.

## Requirement ids

- Each Decision ends with a **Verification** list: the concrete behaviors that
  Decision requires.
- Every Verification item carries a **minted id** of the form `R-XXXX-XXXX` — a
  stable, unique handle for that one behavior.
- The ids live inline in these Verification lists and nowhere else — there is
  **no separate requirements document**.
- Design's responsibility for ids ends at **minting them into this doc**. How
  coverage is measured, what counts as a covered id, and when the work is "done"
  are **not** design's concern — downstream phases own that.

## Conventions

- **Language / toolchain:** Go (the repo targets `go 1.26`); module path
  `telemetry`, built on the shared `appkit` chassis over SQLite
  (`modernc.org/sqlite`, pure Go, no cgo). In-repo libraries are consumed via
  committed `replace` directives (`appkit => ../appkit`, `registry =>
  ../registry`). telemetry does **not** depend on `eventplane`: it is neither a
  producer nor a consumer.
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

## Layout

The design is **split for addressability** so the build loop reads only the one
Decision a phase realizes:

- `project/design/INDEX.md` — the manifest: each Decision → its file, plus a
  sorted `R-id → Decision/file` reverse map. Regenerated whenever a Decision is
  added or its Verification ids change.
- `project/design/DNN.md` — one self-contained file per Decision (zero-padded
  `D01.md`, `D02.md`, …; referenced in prose and in the plan as `D<N>`).
- `project/design/README.md` — this spine: static cross-cutting facts only, no
  per-Decision detail.

Design is **rewritten in place**: a changed Decision is rewritten in its
`DNN.md` and `INDEX.md` is regenerated; a new Decision adds a `DNN.md` and an
INDEX entry. Stale decisions are removed, never stacked beside their
replacements.
