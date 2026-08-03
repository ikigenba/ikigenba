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
- **Published agentkit**: `github.com/ikigenba/agentkit v0.16.0` — the external dependency (D1 pins it; the release carries the offering-structured model catalog, typed-credential constructors with lazy credential resolution, typed provider `Identity`, the OpenRouter provider, consumer-owned cost resolution, `Conversation.MCPServers`, and the `toolkit` subpackage of standard coding tools — research §2, §3, §4). The tag is published; both local dev and the production build (`GOWORK=off`) resolve it from the module cache. Three capabilities the release carries — `toolkit.WebSearch`, `toolkit.WebFetch`, and the `ocr` subpackage — are deliberately unconsumed (D1).
- **Local chassis modules**: `appkit` and `eventplane` remain as committed `replace` directives in `prompts/go.mod`, consumed as fixed external contracts (never edited from here). The **revised eventplane routing API** (kind/subject envelope, `routing.Key`/`Match`, `outbox.Family`/`Registry.CouldMatch`, `consumer.Event{Kind, Subject}` + `Key()` — `eventplane/project/design/` D1–D4) and an appkit that compiles against it are **external preconditions** for the conformance Decisions D24/D25 (operator-sequenced; see the ⛔ banners there).
- **Migrations**: schema changes land only as new timestamped migrations minted with `bin/create-migration prompts <name>`; committed migrations are immutable (the suite rule).
- **Share filesystem API**: the file-share tools (D26) consume dropbox's loopback filesystem API (`dropbox/docs/filesystem-api.md`) as a fixed external contract, addressed through the registry-defaulted `DROPBOX_BASE_URL`. Its refined mutation error contract (dropbox design D16, error-contract slice; dropbox plan phase 25) is an **external precondition**, operator-sequenced before D26's phases (see the ⛔ banner in D26).
- **Suite telemetry contracts (external, consumed by value)**: the correlation header is `X-Correlation-Id` carrying a bare 26-char Crockford-base32 ULID (`docs/correlation-ids.md`, `docs/telemetry-protocol.md`); appkit owns the read-or-mint middleware and its context accessors (`correlation.FromContext` / `correlation.WithContext`), `correlation.StartChain` (adopts the context's id, minting only when absent, and records the `root`) that every self-originated spawn goes through, the telemetry recorder and its record kinds (`edge`/`request`/`outbound`/`publish`/`consume`/`root`/`lifecycle`), and the instrumented outbound HTTP client the **Router** hands out (`rt.HTTPClient(...)`); eventplane owns the outbox `correlation_id` column, the wire envelope field, the ctx-populated `Append`, and the id it surfaces into consumer handler contexts. prompts consumes all of these as fixed external contracts and re-implements none of them: it mints no chain id, builds no telemetry record, and names the chassis accessors only at the composition root — the domain and transport packages take narrow injected seams instead. Both revisions are **external preconditions** for D44–D47 (see the ⛔ banners there).
- **Shared `registry` module**: adopted by D14 as a third committed `replace registry => ../registry` (plus `require registry v0.0.0`) in `prompts/go.mod`, wired exactly like `eventplane`. It is a zero-dependency leaf that turns a service **name** into its loopback port / base URL from one authoritative table. The `registry` module itself and the repo-root `go.work use ./registry` entry are **external preconditions** owned outside `prompts/` and assumed satisfied; no phase here creates or edits them.

## Run durability and the two prompt loaders

A run is a **durable, self-contained record**. Its artifacts live in one directory per run under the service's durable state tree — `<stateDir>/runs/<run_id>/` holding `input/`, `output.jsonl`, and `sandbox/` — which survives restarts and is never reclaimed automatically (D39). Its `input/` copy is written at spawn from the live prompt and is what the runner executes, so a run reports what it ran even after the prompt is edited or deleted.

That split is expressed as two named loaders returning one shape, `Executed`: `LoadFromPrompt` reads the live `prompts` row and is called only at spawn; `LoadFromRun` reads the frozen snapshot and backs every read of a past run (D40). The rule they encode is that **the run path never reads the `prompts` table** — run-side ownership is scoped off `runs.owner_id`, which is why `RunList` no longer preconditions on the prompt existing.

Deleting a prompt **does not cascade**: it removes the prompt row and its triggers, and its runs survive intact, listable and readable. Run history is removed only by `run_delete`, which takes one run and removes its `calls` rows, its `runs` row, and its directory (D41).

## Web surface (the browse UI)

prompts is no longer MCP-only: it serves a **human browse UI** — server-rendered pages under the session-gated `/ui/` namespace (Prompts and Runs tabs, detail pages, the per-run calls log; D34/D35), with the bare mount root `GET /{$}` redirecting into it — **beside** the unchanged MCP/`/health`/PRM/`/feed` surfaces. The two surfaces have two audiences gated two ways (D10): **agents** reach `/mcp` with an opaque bearer (`auth_request /_authn`, unchanged); **humans** reach the UI with the dashboard login-session cookie (`auth_request /_session-authn`, the same coarse gate `sites` uses for its private tier — any logged-in user, no owner scoping). All human routes are mounted **ungated in-process** (in `registerRoutes`, beside the existing `POST /mcp`) — nginx remains the sole trust boundary — so the page handlers read no token and no identity header. prompts ships its **own** copy of the Carbon assets (`tokens.css` + woff2 fonts) and the UI templates on disk in the release `share/www` tree, served through the chassis `Spec.WWW` (`rt.WWW().Render`; the chassis auto-mounts `GET /static/`), as diffable release artifacts (D16). The pages are proven with `net/http/httptest` over a seeded SQLite DB and the repo-real `share/www` tree loaded via `appkit/web` from the composition-root package — no LLM, no runner, no identity header. The nginx session-gates themselves are config, not Go — proven by string assertions over `etc/nginx.conf`. Details: D10 (gates + root), D34 (`ui/` namespace), D35 (pages), D13 (assets/fonts), D12 (Home link).

## Inference surface (the loopback plumbing endpoints)

prompts is the suite's sole inference service: beside agent sessions it executes one-shot **completions** (`POST /complete`, D29) and **embeddings** (`POST /embed`, D30) on behalf of sibling daemons, records every inference unit in the **`calls`** table (D28, one durable row per session run / completion / embedding), bounds concurrency with semaphores (D31), and reports through the `calls`/`usage` MCP tools (D32). The two endpoints are **loopback-only plumbing**, mounted through the chassis loopback guard beside `/feed` and `/run-content` — never routed by nginx, no identity headers, trusted because one box is one trust domain. The doctrine line they sit on: the event plane carries *facts* between daemons; loopback plumbing endpoints carry *capabilities* one daemon consumes from another (the nginx→dashboard `/internal/authn` precedent) — and the bar for adding a new capability endpoint stays high. The term "ledger" is never used for this surface (`ledger` names a sibling service); the table, package, and tools say `calls`. OpenAI-backed work — sessions and completions alike — can opt into **ChatGPT subscription authentication** per config (`auth: "sub"`) instead of the metered API key; the credential file, store lifecycle, and factory wiring are D38 (embeddings stay key-authenticated).

## Correlation and telemetry (the run as a chain root)

prompts is the suite's **content store for agent chains**: telemetry records the
skeleton of what happened everywhere, and when a chain touches a run, the run's
own archive (`output.jsonl`) is the single copy of the conversation. That works
because a run's causal chain id is stored durably on the run row and is
queryable (D44): a run started by an MCP caller or an event *continues* the
inbound chain, and a run with no inbound cause *is* its own root (durable-root
reuse — the run id is the chain id) and records one `root` record at spawn
(D47), established by seeding the run id onto the context and letting the
chassis `correlation.StartChain` adopt it — prompts mints no chain id. Every suite peer MCP call an in-run agent makes carries that id
(`X-Correlation-Id` in the `MCPServer` headers agentkit injects, D45); the
hop is recorded once, by the receiving peer, since agentkit's client offers
no instrumentation seam (D45's recorded boundary). At the edge, the fragment captures the introspection-minted id on every
gated location and strips it on the ungated PRM bootstrap (D46). Everything
else — inbound `request` records, `lifecycle`, `publish`/`consume` — arrives by
rebuilding against the new appkit/eventplane (D47), which also states the
boundary: the run's *inside* (provider traffic, builtin sandbox tool use) is
never recorded. `calls.correlation_id` (the causal chain) and `calls.group_id`
(the caller's reporting label) are deliberately distinct — D44 records why.

## Layout

`project/design/INDEX.md` is the manifest: each Decision maps to its `DNN.md` file, and every `R-XXXX-XXXX` id maps back to its Decision and file.

`project/design/DNN.md` — one self-contained file per Decision (zero-padded), referenced in prose and the plan as `D<N>`.

This spine holds only the cross-cutting facts above. Rewritten in place when decisions change; construction history lives in git.
