# prompts — Design

**Authority: shape and its proof.** This document owns *how* the migration is built and *how each behavior is verified*. The product doc owns the *why* and the user-facing promises; this doc uses the product's contractual constants (provider names, config keys) by value but does not restate the intent behind them. Design states the exact, checkable form of those promises — mechanism, interfaces, types, naming, test strategy. This is the single current statement of the architecture: when a decision changes, its `DNN.md` is rewritten in place; construction history lives in git.

## Requirement ids

Each Decision ends with a **Verification** list. Every item in that list carries a minted id of the form `R-XXXX-XXXX` — a stable, unique handle for that one behavior. The ids live inline in the Verification lists and nowhere else; there is no separate requirements document.

Design's responsibility ends at minting. How coverage is measured, what counts as covered, and when the work is done are downstream concerns — not specified here.

## Conventions

- **Language / toolchain**: Go 1.26, module path `prompts`.
- **Build**: `go build ./...` run from the `prompts/` directory. Passes when all packages compile without error.
- **Test**: `go test ./...` run from the `prompts/` directory. "The suite is green" means every test passes and no race detector violations appear (`-race` is implicit in CI).
- **Formatting**: `gofmt -l .` emits no output.
- **Requirement-id tag glob**: `*_test.go` — the test-file glob under which `R-XXXX-XXXX` tags must appear for an id to count as realized.
- **Published agentkit**: `github.com/ikigenba/agentkit v0.7.0` — the external dependency (D1 pins it; the release carries the model catalog, typed-credential constructors, the OpenRouter provider, consumer-owned cost resolution, and the `toolkit` subpackage of standard coding tools — research §2, §5, §6). The `v0.7.0` tag is published; both local dev and the production build (`GOWORK=off`) resolve it from the module cache.
- **Local chassis modules**: `appkit` and `eventplane` remain as committed `replace` directives in `prompts/go.mod`, consumed as fixed external contracts (never edited from here). The **revised eventplane routing API** (kind/subject envelope, `routing.Key`/`Match`, `outbox.Family`/`Registry.CouldMatch`, `consumer.Event{Kind, Subject}` + `Key()` — `eventplane/project/design/` D1–D4) and an appkit that compiles against it are **external preconditions** for the conformance Decisions D24/D25 (operator-sequenced; see the ⛔ banners there).
- **Migrations**: schema changes land only as new timestamped migrations minted with `bin/create-migration prompts <name>`; committed migrations are immutable (the suite rule).
- **Share filesystem API**: the file-share tools (D26) consume dropbox's loopback filesystem API (`dropbox/docs/filesystem-api.md`) as a fixed external contract, addressed through the registry-defaulted `DROPBOX_BASE_URL`. Its refined mutation error contract (dropbox design D16, error-contract slice; dropbox plan phase 25) is an **external precondition**, operator-sequenced before D26's phases (see the ⛔ banner in D26).
- **Shared `registry` module**: adopted by D14 as a third committed `replace registry => ../registry` (plus `require registry v0.0.0`) in `prompts/go.mod`, wired exactly like `eventplane`. It is a zero-dependency leaf that turns a service **name** into its loopback port / base URL from one authoritative table. The `registry` module itself and the repo-root `go.work use ./registry` entry are **external preconditions** owned outside `prompts/` and assumed satisfied; no phase here creates or edits them.

## Web surface (the browse UI)

prompts is no longer MCP-only: it serves a **human browse UI** — server-rendered pages under the session-gated `/ui/` namespace (Prompts and Runs tabs, detail pages, the per-run calls log; D34/D35), with the bare mount root `GET /{$}` redirecting into it — **beside** the unchanged MCP/`/health`/PRM/`/feed` surfaces. The two surfaces have two audiences gated two ways (D10): **agents** reach `/mcp` with an opaque bearer (`auth_request /_authn`, unchanged); **humans** reach the UI with the dashboard login-session cookie (`auth_request /_session-authn`, the same coarse gate `sites` uses for its private tier — any logged-in user, no owner scoping). All human routes are mounted **ungated in-process** (in `registerRoutes`, beside the existing `POST /mcp`) — nginx remains the sole trust boundary — so the page handlers read no token and no identity header. prompts ships its **own** copy of the Carbon assets (`tokens.css` + woff2 fonts) and the UI templates on disk in the release `share/www` tree, served through the chassis `Spec.WWW` (`rt.WWW().Render`; the chassis auto-mounts `GET /static/`), as diffable release artifacts (D16). The pages are proven with `net/http/httptest` over a seeded SQLite DB and the repo-real `share/www` tree loaded via `appkit/web` from the composition-root package — no LLM, no runner, no identity header. The nginx session-gates themselves are config, not Go — proven by string assertions over `etc/nginx.conf`. Details: D10 (gates + root), D34 (`ui/` namespace), D35 (pages), D13 (assets/fonts), D12 (Home link).

## Inference surface (the loopback plumbing endpoints)

prompts is the suite's sole inference service: beside agent sessions it executes one-shot **completions** (`POST /complete`, D29) and **embeddings** (`POST /embed`, D30) on behalf of sibling daemons, records every inference unit in the **`calls`** table (D28, one durable row per session run / completion / embedding), bounds concurrency with semaphores (D31), and reports through the `calls`/`usage` MCP tools (D32). The two endpoints are **loopback-only plumbing**, mounted through the chassis loopback guard beside `/feed` and `/run-content` — never routed by nginx, no identity headers, trusted because one box is one trust domain. The doctrine line they sit on: the event plane carries *facts* between daemons; loopback plumbing endpoints carry *capabilities* one daemon consumes from another (the nginx→dashboard `/internal/authn` precedent) — and the bar for adding a new capability endpoint stays high. The term "ledger" is never used for this surface (`ledger` names a sibling service); the table, package, and tools say `calls`.

## Layout

`project/design/INDEX.md` is the manifest: each Decision maps to its `DNN.md` file, and every `R-XXXX-XXXX` id maps back to its Decision and file.

`project/design/DNN.md` — one self-contained file per Decision (zero-padded), referenced in prose and the plan as `D<N>`.

This spine holds only the cross-cutting facts above. Rewritten in place when decisions change; construction history lives in git.
