# wiki — Design

**Authority: shape and its proof.** This document and the `project/design/` directory it heads own *how* the wiki is built and *how each behavior is proven*. The product (`project/product/product.md`) owns the *why*, *for whom*, and the user-facing promises; design states the **exact, checkable form** of those promises and never re-declares the why. Design *uses* the product's contractual constants by value (page cap 12,000 chars; subject types `entity|event|concept`; `ask` strictly read-only) but does **not** own them. This is the single, current statement of the architecture — it is rewritten in place to stay true (stale decisions are removed, not stacked); the history of how it got here lives in the plan.

## Requirement ids

- Each Decision ends with a **Verification** list: the concrete behaviors that decision requires.
- Every Verification item carries a **minted id** of the form `R-XXXX-XXXX` — a stable, unique handle for that one behavior.
- The ids live inline in these Verification lists and nowhere else — there is **no separate requirements document**.
- Design's responsibility for ids ends at minting them into this doc. How coverage is measured, what counts as a covered id, and when the work is "done" are **not** design's concern — downstream phases own that.

## Conventions

Shared facts every Decision leans on:

- **Language / toolchain:** Go **1.26**, single module `module wiki` rooted at `wiki/`. Pure-Go SQLite driver `modernc.org/sqlite` (no cgo).
- **Build / typecheck command:** `cd wiki && go build -trimpath -ldflags "-X main.version=$(cat VERSION)" -o build/wiki.bin ./cmd/wiki`. A bare typecheck is `go build ./...` and `go vet ./...`. The production build adds `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off -buildvcs=false` (driven by `bin/ship`).
- **Test command:** `cd wiki && go test ./...`. **"The suite is green"** means: `go build ./...`, `go vet ./...`, `gofmt -l .` (no output), `go test ./...`, and `bin/check-migrations wiki` all succeed with zero failures.
- **Formatting:** `gofmt`-clean; `gofmt -l .` must print nothing.
- **Module wiring:** `appkit` and `eventplane` are committed in-repo replace-siblings (`replace appkit => ../appkit`, `replace eventplane => ../eventplane`); `github.com/ikigenba/agentkit` is a **published, proxy-fetched** dependency with **no committed replace** (see D1).
- **Migrations:** ordered SQL under `wiki/internal/db/migrations/`, embedded via `//go:embed` as `db.FS`, applied forward-only by the appkit runner. Never hand-author a version; always `bin/new-migration wiki <name>`. `001_schema_migrations.sql` is frozen/verbatim. Never edit a committed migration; `bin/check-migrations` enforces this in CI.
- **Time / IO:** the service takes its clock and any external effect (LLM provider, DB) as injected dependencies at the composition root (`cmd/wiki/main.go` via appkit's `Handlers`/`Workers` hooks), so domain code is testable without wall-clock or network.

## Testing strategy

Testing is part of the architecture, not an afterthought; the seams above exist so behavior can be exercised in isolation. The cross-cutting approach every Decision's Verification list assumes:

- **The LLM is always mocked in tests.** The `llm.Client` / `agentkit.Provider` is reached only through the D5 seam, so extract (D6), compile (D7), and ask (D9) are unit-tested against a **capturing/scripted mock provider** — it returns canned (optionally fenced/over-cap/invalid) responses and records the Conversation it was handed. **No test makes a live LLM call;** the suite is green offline with no `ANTHROPIC_API_KEY`.
- **The DB is a real temp SQLite.** Schema, constraints, normalization, the job lifecycle, and subject/page resolution (D3/D4/D9) are tested against a real `modernc.org/sqlite` database opened on a temp path and migrated by the appkit runner — DB-enforced invariants (UNIQUE, CHECK) and the `DROP TABLE pages_fts` migration (D8) are only meaningful against the real engine. These tests carry no network dependency. **Concurrency** is likewise proven against the real engine: the split write/read handles (D17) are tested with concurrent goroutines on one temp WAL database — a reader not blocking on an open writer, two writers serializing, read-your-writes across the handles — since these are properties of the engine + pool config, not of mocks.
- **Determinism via injection.** Clock and any IO are injected (above), so time-dependent behavior (received-at anchoring, job timestamps) is exercised with a fixed clock.
- **Seam-level unit tests + thin integration.** Each Decision is proven primarily at its own seam (mocked neighbors); a small number of integration tests wire the worker + real DB + mock provider end-to-end to prove the ingest→page→ask spine (the D4/D9 compounding and honest-empty ids). Pure functions (`Normalize` (D3, the single normalizer), `Path` (D11), `Mentions`/`RenderFooter` (D12), `ExtractJSON`, truncate-at-boundary) are table-tested directly; path resolution (`GetByPath`, and the alias-aware `Resolver.ResolveByPath` that the `page`/`claims` read entry adopts — D29) and the read-time link projection (`PageWithLinks`, now alias-aware — D12) run against a real temp SQLite.
- **MCP surface** (D10) is tested by driving the JSON-RPC handler with `tools/list` and `tools/call` requests over an in-process server with a stubbed identity, asserting the tool list, result/not-found shapes, `type/slug` path resolution and the rendered page link footer (D11/D12), and identity gating.
- **The human web surface** (D39, D42–D46) is tested with `net/http/httptest` against the `internal/web` handler as the composition root mounts it. The page-shape behaviors — routing/dispatch and `<base href>` (D42), home search box + orphan list (D43), ask answer + mention footer (D44), subject prose + outbound/inbound footer + styled 404 (D45) — are driven with **stub seams** (a recording `Asker`/`Mentioner`/`PageFinder`/`OrphanLister`), no DB/LLM/identity, since they are pure routing+templating. The seams' real wiring is **not** left to stubs alone: D44 (R-AXQR-2TF9) and D45 (R-PODT-EU1H) each drive the **real** `asker`/`ResolveByPath`/`PageWithLinks`/`MentionsIn` over a **real temp SQLite** (LLM still mocked per above) through the web handler, and D46 proves the orphan computation against the real engine — so "the web surface renders real ask answers and real link projections" is exercised end to end, not only against fakes. The nginx session-gate itself is config, not Go — proven by named fragment-check phases (the D39 Phase 64 pattern, extended for D47's `/subject/` and `/static/` locations), not an `R-id` test.

## Web surface (the human read UI)

wiki is no longer MCP-only: it serves a small **read-only human HTML surface** — **beside** the unchanged MCP/`/health`/PRM JSON surfaces — that is a styled window onto already-built capabilities (`ask`, pages, the D12 link graph), adding no domain logic and mutating nothing. Three states, all Carbon-styled: a **home** page (`GET /{$}`) with a search box, an **orphan index** of subjects nothing links to, and a name+version footer; an **ask result** (`GET /{$}?q=…`) rendering D9's cited answer with an outbound-mention footer; and **subject pages** (`GET /subject/{type}/{slug}`) rendering a compiled page's prose with outbound+inbound link footers, alias-aware (D29). The decomposition: **D39** owns the cross-cutting gating/asset/exact-root **spine**; **D42** the package, route table, `<base href>`/mount strategy, and injected seams; **D43/D44/D45/D46** the home/ask/subject/orphan states; **D47** the nginx gating expansion.

Two audiences, two gates: **agents** reach `/mcp` (and `/health`/`/feed`) with an opaque bearer (`auth_request /_authn`, unchanged); **humans** reach the web surface (root + `/subject/` + `/static/`) with the dashboard login-session cookie (`auth_request /_session-authn`, the same coarse gate `sites` uses for its private tier — D47). Every web route is mounted **ungated in-process** — nginx remains the sole trust boundary — so no web handler reads a token or trusts a client identity header. wiki embeds its **own** copy of the Carbon assets (`tokens.css` + woff2 fonts) and templates under the `internal/web` package via `//go:embed`, mirroring the dashboard's `ui/` precedent, so the binary stays one static file and wiki's surface diverges from other apps' without a shared-library change.

## Layout

The design is split for addressability so a build phase reads only the one Decision it realizes:

- `project/design/design.md` — this spine: static cross-cutting facts only, no per-Decision detail.
- `project/design/DNN.md` — one self-contained file per Decision (zero-padded: `D01.md`, `D02.md`, …; referenced in prose and the plan as `D<N>`).
- `project/design/INDEX.md` — the manifest: each Decision → its file, plus a sorted `R-id → Decision/file` reverse map. It is the grep target for resolving an id.

Design is **rewritten in place**, not append-only (history lives in the plan): a changed Decision is rewritten in its `DNN.md` and `INDEX.md` is regenerated; a new Decision adds a `DNN.md` and an INDEX entry. Existing `R-XXXX-XXXX` ids are stable handles — never renumbered; a newly added behavior gets a freshly minted id, and a removed behavior's id is deleted with it.
