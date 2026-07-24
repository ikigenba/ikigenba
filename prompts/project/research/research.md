# prompts — Research

Non-contractual external ground truth the design references so it never re-derives it. Rewritten in place; the build loop never reads this file.

## 1. The problem measured — eager suite-tool loading

At run spawn, `suite.Discover` lists every peer's `tools/list` and wraps **every verb of every peer** as a full `agentkit.RawTool` (name + description + input JSON Schema), all of which is serialized into the provider `tools` array on **every** round-trip of the run's single `Send`. Measured against a live local suite (6 services on :3001–:3006): 79 tools, ~31 KB of serialized descriptions+schemas ≈ **8k tokens**; a full box (~11 peers, ~120 tools) lands around **15–18k tokens resident per round-trip**, plus tool-choice dilution. All schemas are fetched at spawn regardless — the waste is context surface, not network.

## 2. agentkit `v0.2.0` — the deferred-tools API (the dependency this design consumes)

Specified in agentkit's own `project/` (Decision D23, phases 46–48); the footprint prompts consumes:

```go
// A named set of deferred tools sharing one blurb. Tools are full, callable
// Tool values — deferral changes when they are surfaced, not how they run.
type DeferredToolGroup struct {
    Name  string // catalog section heading, e.g. "crm"
    Blurb string // one-paragraph group description; may be ""
    Tools []Tool
}

// On Conversation:
DeferredTools []DeferredToolGroup // empty → no load_tools, behavior unchanged
```

Semantics (agentkit-owned; prompts relies on, does not re-prove):

- When `DeferredTools` is non-empty, agentkit synthesizes one built-in meta-tool, **`load_tools`**, whose *description* carries the generated catalog: each group's `Name`, `Blurb`, and its tools' bare names — no per-tool descriptions or schemas. No system-prompt injection.
- `load_tools` takes **exact names, batched**; its result confirms each load and carries each loaded tool's description + input schema. Unknown names are per-name errors, never terminal.
- Loading is **monotonic and conversation-scoped**; loaded tools join subsequent round-trips as ordinary native tools (frozen name-sorted base, loads appended in load order — Anthropic cache-prefix preserving).
- Calling a deferred-but-unloaded tool feeds back a corrective error naming `load_tools` and loads the tool as a side effect (the guessed input is never executed).
- One `Send` gate: name uniqueness across eager ∪ MCP ∪ deferred ∪ the reserved `load_tools` name; deferred schemas validated like `RawTool`.
- Works uniformly across all four providers; a live-Anthropic integration id (R-DFH0-A8TE, agentkit's) proves the real API accepts the mid-turn grown tools array.

**Availability:** first published in `v0.2.0`; the API is unchanged through `v0.7.0`, the release prompts now pins (§5, §6).

## 3. The blurb source — MCP `initialize.instructions`

Every suite peer publishes a one-paragraph server description in the MCP `initialize` response's `instructions` field (verified live against wiki on :3001):

```json
{"result": {"capabilities": {"tools": {}},
  "instructions": "wiki is a knowledge base built from ingested source text. …",
  "protocolVersion": "2025-03-26", "serverInfo": {"name": "wiki", "version": "dev"}}}
```

`prompts/internal/mcpclient` speaks JSON-RPC-over-HTTP already (`tools/list`, `tools/call`) but does not call `initialize` today — the method is one more `c.call(...)` against the same endpoint with the same identity headers. A peer that fails `initialize` or omits `instructions` simply has no blurb; the catalog degrades to bare names.

## 4. Prior art

Claude Code's ToolSearch is the pattern being mirrored: deferred tools appear by name + per-server blurb; fetching a tool's schema on demand makes it directly callable; loads are monotonic per session. Anthropic's provider-native tool-search beta confirms the pattern but is single-provider — hence the agentkit (cross-provider) home, decided in agentkit D23.

## 5. agentkit v0.6.0 — catalog, typed credentials, OpenRouter, cost (the release this design consumes)

Published tag `github.com/ikigenba/agentkit v0.6.0` (2026-07-18). Breaking deltas since the pinned `v0.2.1`, verified against the tagged source:

**Typed-credential constructors (v0.4.0).** Every provider sub-package's `New` takes a closed `Credential` type, not a string: `anthropic.New(anthropic.APIKey(key), opts...)`, likewise `openai`, `google`, `zai`, and the new `openrouter`. All five expose `WithBaseURL` and `WithHTTPClient` options. The legacy model registries and the `Provider.Pricing(model)` method are **gone** — the `agentkit.Provider` interface is now just `RoundTrip(ctx, *Request) *RoundTrip` + `Name()`. (v0.5.0's subscription-login rework touches only `openai/subscription`, now consumed by the subscription-auth design — facts in §8.)

**The `catalog` package (v0.4.0, extended v0.6.0).** Advisory metadata keyed by globally unique model name — advisory upstream ("coverage never controls whether a model can be sent"), but the authoritative table of supported provider/model/effort combinations. Footprint:

```go
type Entry struct {
    Model     string            // catalog name, globally unique, e.g. "grok-4.5"
    Provider  string            // default provider
    Routes    map[string]string // provider → wire model slug, e.g. {"openrouter": "deepseek/deepseek-v4-flash"}
    Pricing   *agentkit.Pricing // nil on embedding-only entries
    Reasoning *ReasoningSpec    // nil → model has no reasoning control
    Context   int64
    Embedding *EmbeddingInfo
    Options   json.RawMessage
}
func Lookup(model string) (Entry, bool)
func Resolve(provider, model string) (routeProvider, wireModel string, entry Entry, ok bool)
func ListByProvider(provider string) []Entry
```

`Resolve("", model)` fills the entry's default provider; an explicit provider succeeds only when it is the default or appears in `Routes` (returning the route's wire slug). `ReasoningSpec` (aliased from the root package) has `Kind` ∈ {`ReasoningEnum` (+`Levels`), `ReasoningRange` (+`Min`/`Max`/`Sentinels`), `ReasoningToggle`}, `Default`, `CanDisable`, and `Accepts(v ReasoningValue) bool` — the one-call create-time check for effort/level/budget/disable values.

**Chat inventory at v0.6.0** (entries with non-nil `Pricing`): anthropic `claude-opus-4-8`, `claude-sonnet-4-6`, `claude-haiku-4-5`, `claude-fable-5`, `claude-sonnet-5`; google `gemini-2.5-flash`, `gemini-2.5-pro`, `gemini-3.5-flash`, `gemini-3.1-flash-lite`, `gemini-3.1-pro-preview`; openai `gpt-5.5-pro`, `gpt-5.5`, `gpt-5.4`, `gpt-5.4-mini`, `gpt-5.4-nano`, `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-5.6-luna`; openrouter (new in v0.6.0) `grok-4.5`, `grok-4.3`, `grok-4.20`, `grok-4.20-multi-agent`, `deepseek-v4-flash`, `deepseek-v4-pro`, `kimi-k3`, `kimi-k2.7-code`, `kimi-k2.6`; zai `glm-5.2`, `glm-5.1` (thinking toggle only — the v0.6.0 spec fix), `glm-4.7`, `glm-4.6`. Plus embedding-only entries (`text-embedding-3-*`, `gemini-embedding-001`). **The anthropic/openai/google/zai entries currently carry no `Routes` map** — only their native provider serves them; the OpenRouter-routed vendors carry vendor-namespaced slugs (e.g. `x-ai/...`, `deepseek/...`, `moonshotai/...`). Widening routing (e.g. Claude via OpenRouter) is an agentkit catalog change prompts picks up with a version bump.

**Consumer-owned cost (v0.4.0).** A provider-reported cost takes precedence; otherwise `Conversation.Pricing` (a `*agentkit.Pricing` field) prices the turn's usage; with neither, cost is zero and the stream emits a `WarnCostUnknown` warning. `catalog.Entry.Pricing` is the intended source for the field.

**Box wiring.** `OPENROUTER_API_KEY` is already exported by `prompts/.envrc` from `~/.secrets/OPENROUTER_API_KEY`, following the same pattern as the other four provider keys.

## 6. agentkit v0.7.0 — the `toolkit` subpackage (the release this design consumes)

Published tag `github.com/ikigenba/agentkit v0.7.0` (2026-07-19). The only delta over v0.6.0 is the new `github.com/ikigenba/agentkit/toolkit` subpackage: six standard coding tools as ready-made `agentkit.Tool` values, via per-tool constructors and `All(root)`:

```go
// Package toolkit provides standard tools for local coding agents.
func All(root string) []agentkit.Tool // Bash, Read, Write, Edit, Glob, Grep, in that order
func Bash(root string) agentkit.Tool  // + Read, Write, Edit, Glob, Grep per-tool constructors
```

Behavior, verified against the tagged source and empirically where noted:

- **Confinement**: file tools resolve paths against `root` with symlink-aware checks (`EvalSymlinks` on root and the longest existing ancestor, `filepath.Rel` containment). Toolkit's own doc states this protects against *accidental* filesystem access and is not a security sandbox; `Bash` runs with `root` as its working directory but is not confined.
- **Bash**: `command` + optional `timeout` (ms, default 120000, no ceiling). Runs `bash -c` in its own process group; on timeout or context cancellation it SIGKILLs the whole group and returns output with a `[command timed out after Nms]` / `[command cancelled]` marker. A nonzero exit is **not** a tool error — output gets an appended `[exit status N]` marker.
- **Output caps**: every tool result is capped at 30,000 characters (rune-safe), with a `[output truncated: showing first N of M characters]` marker.
- **Read**: whole file or `offset`/`limit` line window (1-based offset), negative values rejected.
- **Write**: creates parent directories; returns `wrote <path>`.
- **Edit**: exact-string replace; empty `old_string` rejected; a non-unique match without `replace_all` is refused with the occurrence count.
- **Glob**: plain patterns via `filepath.Glob`; `**` patterns via a walk that skips `.git`. Returns a sorted JSON array of root-relative slash paths. **Known gap (empirically verified)**: a non-`**` pattern is joined to the base *without* confinement, so `pattern: "../*"` lists entry names outside `root` (list-only — Read/Write/Edit still refuse those paths). Accepted for prompts (Bash is unconfined anyway, so this widens nothing); flagged as a future toolkit improvement.
- **Grep**: Go regexp over files under `path` (optional base-name `glob` filter), skipping `.git` and binary files (NUL in the first 8 KB). Returns a sorted JSON array of `rel/path:line:text` matches. Reads whole files, so long lines are safe (no 64 KB scanner limit).
- **Schemas**: input structs carry `json` tags only — no `jsonschema` description tags — so the generated schemas have bare property names, and tool descriptions are terse ("Run a shell command."). Field-level guidance must come from the framing prompt if wanted.
## 7. agentkit v0.6.0 — the one-shot and embedding footprint (the unified-inference dependency)

Facts verified against the tagged `v0.6.0` source that the completion/embedding/accounting design consumes:

**Stateless multi-turn one-shots.** `agentkit.Conversation` carries a public `History []Message` field; a stateless caller sets `History` to prior turns and calls `Send(ctx, lastUserText)` once. `Message` roles are `RoleUser` / `RoleAssistant` (`block.go`). A `Conversation` with empty `Tools`/`DeferredTools` performs exactly one provider round-trip per `Send` — the tool loop never engages — making it the natively supported one-shot completion primitive.

**Usage grain (design-constraining).** Per-provider-round-trip usage is **not** consumer-visible: `stream.Usage()` / `stream.Cost()` are aggregates for the whole `Send`, and the JSONL `Log` emits **one** `usage` record per `Send` (after the tool loop; `orchestration.go:234`, confirmed by `log_test.go`'s expected sequence carrying one `usage` across a two-round-trip turn). Any accounting grain finer than one-row-per-`Send` would require an agentkit change or a provider-seam fork.

**Embeddings.** `agentkit.Embedder{Provider, Model, Pricing *EmbeddingPricing, Dimensions, Retry}` with `Embed(ctx, inputs []string, role InputType) (*EmbedResult, error)` — batch in, vectors out, dimension-checked against the request. Roles: `InputDocument` / `InputQuery`. Embedder constructors exist on exactly two provider sub-packages: `openai.NewEmbedder(cred, opts...)` and `google.NewEmbedder(cred, opts...)` (both return `agentkit.EmbeddingProvider`, an interface — fakeable). anthropic/zai/openrouter have no embedder.

**Embedding catalog.** `catalog.Entry.Embedding *EmbeddingInfo` carries `Pricing agentkit.EmbeddingPricing`, `NativeDimension`, `MinDimension`, `MaxDimension`, `MaxInputTokens`. Entries at v0.6.0: `text-embedding-3-small` (openai, native 1536, min 1), `text-embedding-3-large` (openai, native 3072, min 1), `gemini-embedding-001` (google, native 3072, min 128). So embedding requests are catalog-validatable (model + dimension range) exactly like chat configs — **no version bump needed**; v0.6.0 already carries everything (v0.7.0 adds only the `toolkit` coding-tools subpackage, irrelevant here).

**Suite context (non-contractual).** wiki today embeds agentkit directly for five structured-generation call sites plus openai embeddings, logging every attempt to its own `llm_calls` SQLite table with stage/job_id/attempt filters — the consumer shape the `/complete`+`/embed`+`calls` surface is designed to absorb. wiki's conversion is wiki's own spec; the suite-level decision (prompts owns all inference, spend, and reporting) was settled in the unified-inference discussion of 2026-07-19.

## 8. OpenAI subscription auth — `agentkit/openai/subscription` + the `oauth` CLI (the subscription-auth dependency)

Facts verified against the tagged `v0.7.0` agentkit source and the `oauth` CLI repo (`github.com/ikigenba/oauth`):

**The credential file format.** `openai/subscription` (reworked in v0.5.0) consumes the **raw OAuth token-endpoint JSON response** — `{access_token, refresh_token, id_token}` — not a wrapper format. `subscription.Load(path) (*Store, error)` parses it, requires a non-empty `access_token`, and derives the ChatGPT account id from the `https://api.openai.com/auth` JWT claim (id_token first, access_token fallback); a missing claim fails the load. Login itself was deliberately removed from agentkit in v0.5.0 — producing the file is an external tool's job.

**Refresh semantics.** `Store.Token(ctx) (bearer, accountID, error)` refreshes when the access token expires within a 5-minute skew, POSTing `grant_type=refresh_token` to `https://auth.openai.com/oauth/token` with the pinned client id `app_EMoamEEZ73f0CkXaXp7hrann`, then **atomically rewrites the file** with the new response (preserving `refresh_token`/`id_token` when the response omits them). The refresh-token lineage **rotates**: a stale copy of the file is dead after the live copy refreshes, so exactly one process may own a file, and copies must never be shared between machines. Refreshes are serialized **within one Store instance only** — nothing protects two Stores opened on the same path, which forces the one-store-per-process-per-file rule.

**Provider integration.** `openai.Subscription(ts TokenSource) Credential` (TokenSource = `Token(ctx) (bearer, accountID, error)`, satisfied by `*subscription.Store`) flips the provider onto the `https://chatgpt.com` base URL and names it `openai.subscription` (vs `openai` for key auth). Costs agentkit resolves for subscription-authenticated turns are **notional API-rate equivalents**, not subscription spend (documented in `openai/openai.go`). The subscription credential is Responses-surface only — the embedder constructors take API-key credentials.

**The `oauth` CLI.** Provider-agnostic authorization-code + PKCE login: serves its own loopback callback, opens the browser, exchanges the code, and writes the token endpoint's JSON response **verbatim to stdout** (human output on stderr; failed login writes nothing, exits non-zero) — exactly the file `subscription.Load` consumes. Its own `--help` carries the OpenAI worked example: `--auth-url https://auth.openai.com/oauth/authorize --token-url https://auth.openai.com/oauth/token --client-id app_EMoamEEZ73f0CkXaXp7hrann --scope "openid profile email offline_access" --port 1455 --callback-path /auth/callback` (matching OpenAI's registered `http://localhost:1455/auth/callback`). opsctl init-box installs it to `/usr/local/bin` on every box (its D11); on a headless box the printed authorize URL plus an `ssh -L 1455:localhost:1455` forward completes the flow.

**On-box home.** `/opt/prompts/state/` is the durable, service-owned tree: deploy chowns it to `prompts:prompts` and never touches its contents — the correct home for a file the service must rewrite and that must survive deploys. `/opt/prompts/etc/` is deploy-owned versioned config (root-written, 0644) and is wrong for a mutable credential. The SSM/env secret path is also wrong: it delivers static values from the workstation, and a pushed copy of a rotating lineage goes stale after the first on-box refresh.
