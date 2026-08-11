# artifacts — Design

**Authority: shape and its proof.** This directory owns *how* the artifacts
service is built and *how each behavior is proven* — its seams, public
interfaces, naming, struct/type definitions, data model, and the test plan.
`project/product/README.md` owns the *why* and the user-facing promises; design
states the **exact, checkable form** of those promises and never re-declares
the why. Design **uses** the product's contractual constants by value
(`/srv/artifacts/`, starting version `v0.1.0`, the 24-hour upload-link
lifetime) but does not own them. This is the **single, current** statement of
the architecture — when a decision changes its `DNN.md` is rewritten in place
(stale decisions removed, not stacked); the construction history of how it got
here lives in git, not here.

## Requirement ids

- Each Decision ends with a **Verification** list: the concrete behaviors that
  decision requires.
- Every Verification item carries a **minted id** of the form `R-XXXX-XXXX` — a
  stable, unique handle for that one behavior.
- The ids live inline in these Verification lists and nowhere else — there is
  **no separate requirements document**.
- Design's responsibility for ids ends at **minting them into this doc**. How
  coverage is measured, what counts as a covered id, and when the work is
  "done" are **not** design's concern — downstream phases own that.

## Conventions

- **Language / toolchain:** Go (the repo targets `go 1.26`); module path
  `artifacts`, built on the shared `appkit` chassis over SQLite
  (`modernc.org/sqlite`, pure-Go, no cgo). In-repo libraries are consumed via
  committed `replace` directives (`appkit => ../appkit`,
  `eventplane => ../eventplane`, `registry => ../registry`).
- **Build / typecheck command:** `cd artifacts && go build ./...` (and
  `go vet ./...`, and `gofmt -l .` printing nothing). The production binary is
  built by `bin/ship artifacts` (`CGO_ENABLED=0 GOOS=linux GOARCH=amd64
  GOWORK=off`, version/commit stamped) — not invoked during normal development.
- **Test command:** `cd artifacts && go test ./...`. "The suite is green"
  means this command exits 0 with no failures, alongside a clean
  `go build ./...`, `go vet ./...`, and `gofmt -l .`. Tests run against **real
  SQLite** (temp-file DBs via the appkit migration runner) and real temp-dir
  filesystems for the blob store — never a mocked store — with a deterministic
  injected clock.
- **Requirement-id test-file glob:** `*_test.go` — every `// R-XXXX-XXXX` tag
  lives in a Go test file matching this glob.
- **Test layers.** The suite's testing vocabulary — hermetic / composed /
  live / manual, the `live` build tag, the skip ban, per-tree declarations —
  is the contract `root project/design/D23.md`, cited and not restated here.
  artifacts' own layer facts are recorded in D10: **hermetic** and **composed**
  only, both in the default gate, no live layer, no tree-local manual runbook.
- **DB / migrations:** schema lives in `internal/db/migrations/` as ordered,
  immutable SQL applied forward-only by the appkit runner. New migrations are
  created with `bin/create-migration artifacts <name>` (timestamped); numbers
  are never hand-picked and committed migrations are never edited. The
  migration-immutability guard is the contract `root project/design/D25.md`,
  adopted in D2.
- **Loopback port:** resolved **by name** at boot — `registry.MustPort
  ("artifacts")` — never a literal in this tree's Go source (D1). The registry
  row (`{"artifacts", 3009, Core}`) lives in the shared `registry` module,
  outside this tree.
- **Time / IO:** time enters the domain through a `Clock` seam; tests inject a
  deterministic clock. The DB handle is the appkit-owned single-writer
  `*sql.DB` (`rt.DB()`); the producer outbox shares it. Blob IO goes through a
  root-dir-configured `BlobStore` seam (D2) so tests use temp dirs.
- **State layout:** the SQLite DB and the blob directory both live under the
  service's `state/` (the backed-up side of the `root project/design/D05.md`
  boundary); every on-box path is composed from `IKIGENBA_ROOT` per the env
  contract `root project/design/D11.md`, adopted in D1.
- **Configuration:** one service-owned variable, `ARTIFACTS_MAX_UPLOAD_BYTES`
  (default `209715200`, 200 MiB), authored in `etc/manifest.env` per
  `root project/design/D11.md`. The upload-link TTL is a **constant** (24h,
  the product's contractual lifetime), not a variable.
- **Human landing page:** the mount root serves the owner-facing index on the
  suite **Carbon** design system, conformed to the **cron canonical** template
  (`cron/internal/web/landing.html` + `static/tokens.css` + woff2 fonts).
  artifacts embeds its **own** copy of the template and assets under
  `internal/web/` — no shared handler, no runtime dependency on another
  service's assets. The page is served ungated in-process; the browser-session
  gate lives in the nginx fragment (D8). Page content is D9.
- **Correlation ids are a suite constant, used by value** — the header, id
  shape, strip-then-mint rule for ungated public locations, and the recorder
  are `root project/design/D14.md`, realized in `eventplane`/`appkit`.
  artifacts' share is its nginx fragment (D8); recording of requests,
  publishes, and lifecycle is chassis-owned and deliberately not re-proven
  here.
- **MCP surface shape** (structured results, closed error vocabulary, the
  three discovery tiers) is the contract `root project/design/D20.md`, cited
  by D6 and not restated. The owner-identity contract (`X-Owner-Id` keys,
  `owner_id`/`owner_email` column pair) is `root project/design/D17.md`,
  cited by D2/D6.

## Layout

The design is **split for addressability** so the build loop reads only the
one Decision a phase realizes:

- `project/design/INDEX.md` — the manifest: each Decision → its file, plus a
  sorted `R-id → Decision/file` reverse map. Regenerated whenever a Decision
  is added or its Verification ids change.
- `project/design/DNN.md` — one self-contained file per Decision (zero-padded
  `D01.md`, `D02.md`, …; referenced in prose and the plan as `D<N>`).
- `project/design/README.md` — this spine: static cross-cutting facts only,
  no per-Decision detail.

Design is **rewritten in place**, not append-only: a changed Decision is
rewritten in its `DNN.md` and `INDEX.md` is regenerated; a new Decision adds a
`DNN.md` and an INDEX entry.
