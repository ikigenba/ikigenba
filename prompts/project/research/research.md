# prompts — Research

Non-contractual external ground truth the design references so it never re-derives it. Rewritten in place; the build loop never reads this file.

## 1. Suite-tool context cost — the accepted price of direct attachment

At run spawn every reachable peer's tools are attached to the conversation, and their full definitions (name + description + input JSON Schema) are serialized into the provider `tools` array on **every** round-trip of the run's single `Send`. Measured against a live local suite (6 services on :3001–:3006): 79 tools, ~31 KB of serialized descriptions+schemas ≈ **8k tokens**; a full box (~11 peers, ~120 tools) lands around **15–18k tokens resident per round-trip**, plus tool-choice dilution.

This cost was previously avoided by deferring suite tools behind agentkit's `load_tools`. That route closed at agentkit `v0.12.0` (§2) and the cost is now accepted deliberately, not by oversight.

## 2. agentkit `v0.12.0` — `RawTool` removed, `Tool` sealed (why deferral closed)

`agentkit.Tool` is a sealed interface: it carries an unexported `isTool()` method, so only constructors inside the agentkit module can produce a value satisfying it. Through `v0.11.0` the escape hatch was `agentkit.RawTool(name, description, schema json.RawMessage, fn)`, which built a Tool from a **runtime-discovered** JSON schema — exactly what suite discovery needs, since peer schemas are learned over MCP at spawn and are not Go types.

`v0.12.0` removed `RawTool` (its changelog: "consumers now get one validated schema path rather than provider-dependent degradation") and left `NewTool[In any]` as the sole constructor. `NewTool` derives the schema by reflecting a Go input struct and **panics** on constructs outside agentkit's canonical subset — `json.RawMessage` fields are explicitly rejected, so there is no passthrough. A tagged test, `TestRawToolIsAbsentAndNewToolIsSoleToolConstructor`, pins the removal.

Consequence for prompts: no runtime-discovered peer schema can become an `agentkit.Tool`, so `DeferredToolGroup{Tools: []Tool}` cannot be populated from discovery. The only supported route for foreign MCP tools is `Conversation.MCPServers`.

**`Conversation.MCPServers` footprint** (`orchestration.go`, `mcp.go`), verified against the tagged `v0.16.0` source:

```go
type MCPServer struct {
    Name    string            // tool-name prefix
    URL     string            // Streamable-HTTP / JSON-RPC endpoint
    Headers map[string]string // injected on every request
}
// On Conversation:
MCPServers []MCPServer // servers attach/detach by mutating this between turns
```

- agentkit calls `tools/list` itself per server and wraps each result in its own internal tool type. Discovered tools are named `sanitizeMCPToolName(server.Name + "_" + tool.Name)`, so passing `Name: "ikigenba_crm"` against peers registering bare verbs yields `ikigenba_crm_health` — byte-identical to the `qualify()` names prompts produces today.
- MCP-sourced tools are appended to the **eager** base set (`resolveMCPTools` → `base`). There is no path from `MCPServers` into `DeferredTools`.
- Resolution is **all-or-nothing**: any server whose `tools/list` fails causes `closeMCP` plus an error return, failing the whole `Send`. There is no per-server skip. This is the regression prompts accepts (design D6); fixing it is owed by agentkit.
- The tool set is cached per server-set key after first resolve, so later turns in the same conversation do not re-list. A failing tool *call* returns an error to the model rather than ending the run.
- An MCP tool's schema is still validated against agentkit's canonical subset. That subset permits `type`, `description`, `title`, `properties`, `$defs`, `$ref`, `required`, `items`, `enum` (strings only), `const` (strings only), `anyOf`, `oneOf`; it requires an object root and rejects authored `additionalProperties`, recursive `$ref`, and non-string enum/const values.

## 3. agentkit v0.16.0 — the catalog, restructured by offering (the release this design consumes)

Published tag `github.com/ikigenba/agentkit v0.16.0`. Deltas since the pinned `v0.7.0`, verified against the tagged source.

**Typed-credential constructors (unchanged since v0.4.0).** Every provider sub-package's `New` takes a closed `Credential` type: `anthropic.New(anthropic.APIKey(key), opts...)`, likewise `openai`, `google`, `zai`, `openrouter`. All five expose `WithBaseURL` and `WithHTTPClient`.

**Credentials became lazy (v0.15.0).** Anything needing a credential can now be *constructed* without one; the operation that needs it returns an error naming what is missing. `agentkit.ErrMissingCredential` marks an absent credential; a credential that is present but unusable for an operation reports `agentkit.ErrInvalidConfig` (notably a ChatGPT subscription credential handed to `openai.NewEmbedder`, which previously panicked at construction). So a missing API key no longer fails at factory time — it fails at send time.

**Providers report an `Identity`, not a name (v0.10.0).** `Provider.Name() string` and `EmbeddingProvider.Name() string` became `Identity() agentkit.Identity`:

```go
type ProviderID string  // ProviderAnthropic, ProviderOpenAI, ProviderGoogle, ProviderZAI, ProviderOpenRouter
type AuthMode  string   // AuthAPIKey, AuthSubscription
type Identity struct { Provider ProviderID; Auth AuthMode }
func (i Identity) String() string // "openai.subscription" — the old combined form
```

`agentkit.Error` likewise split its `Provider` field into typed `Provider` and `Auth`; JSONL `turn_start` records gained an `auth` field. The `zai` provider id is spelled **`z-ai`** in errors and logs (the Go package is still `zai`).

**The catalog is organized by offering (v0.10.0, completed v0.11.0).** `Entry` no longer carries `Provider`, `Routes`, `Pricing`, `Reasoning`, or `Context`. Those moved onto a per-provider `Offering`, ordered with the default route at index zero:

```go
type Entry struct {
    Model     string
    Vendor    VendorID          // OpenRouter namespace spelling: anthropic, openai, google, z-ai, x-ai, deepseek, moonshotai
    Offerings []Offering        // authored preference order; [0] is the default route
    Embedding *EmbeddingInfo
}
type Offering struct {
    Provider  agentkit.ProviderID
    WireName  string            // explicit override; empty means derive
    Pricing   *agentkit.Pricing
    Reasoning *ReasoningSpec
    Context   int64
    Options   json.RawMessage
}
type Resolution struct {
    Vendor VendorID; Provider agentkit.ProviderID; WireModel string
    Offering Offering; Coverage Coverage   // Curated | Passthru | Unrouted
}

func Lookup(model string) (Entry, bool)
func Offerings(model string) []Offering
func Offer(model string, provider agentkit.ProviderID) (Offering, bool)
func (e Entry) WireModel(provider agentkit.ProviderID) string
func Resolve(provider agentkit.ProviderID, model string) Resolution
func ListCurated(provider agentkit.ProviderID) []Entry
func Check(model string, provider agentkit.ProviderID, v agentkit.ReasoningValue) (accepted bool, spec *ReasoningSpec, ok bool)
```

Load-bearing consequences:

- **`Resolve` no longer reports failure.** It returns one value and no `ok`; every coverage state yields a runnable `WireModel`. `Curated` means the model+provider pair is in the catalog; `Passthru` means the provider may still serve it but the catalog has no terms; `Unrouted` means neither the model nor the provider is known. Nothing gates execution — a caller that wants to reject an unknown pair must test `Coverage` or call `Offer` itself.
- **Wire model ids are derived, not stored.** `Entry.WireModel(provider)` honors an offering's explicit `WireName`, else namespaces with the vendor for OpenRouter (`x-ai/grok-4.5`) and otherwise returns the bare model name. Only the three Anthropic slugs containing dots carry overrides.
- **Reasoning vocabulary is per offering, not per model.** The same model reached through its vendor and through OpenRouter can accept different values, so any acceptance check now needs a provider. `Check` gained that argument for exactly this reason.
- **`ReasoningSpec.Default` changed type** from `agentkit.ReasoningValue` to `catalog.ReasoningDefault{Mode, Value}` with `Mode` ∈ {`DefaultUnaudited`, `DefaultOff`, `DefaultFixed`, `DefaultDynamic`} — providers that decide per request no longer need an invented default.
- **`ReasoningSpec.CanEnable`** reports permission to turn reasoning *on* separately from `CanDisable`. `agentkit.EnableReasoning()` is the matching explicit on-form; Google and OpenAI reject it with `ErrInvalidConfig` because their wires have no bare on-form.
- **`ListByProvider` was renamed `ListCurated`**, reflecting that it lists catalog coverage rather than everything a provider can serve.
- **Every shipped chat entry now carries an OpenRouter offering** with audited rates and a confirmed reasoning vocabulary, so alternate routing is broadly available where it previously was not. `glm-5.2` via OpenRouter no longer misreports Z.ai's figures.

**Consumer-owned cost (unchanged).** A provider-reported cost wins; otherwise `Conversation.Pricing` prices the turn. The source is now `Resolution.Offering.Pricing` rather than `Entry.Pricing`.

**Assistant text normalization (v0.13.0).** Every provider now returns an uninterrupted answer as a single `TextBlock` rather than one block per SSE frame; text separated by reasoning, tool use, or tool results stays separate and ordered. A Google reasoning-replay bug (a `thoughtSignature` and its `functionCall` arriving in different frames) was fixed in the same release.

**Box wiring.** `OPENROUTER_API_KEY` is already exported by `prompts/.envrc` from `~/.secrets/OPENROUTER_API_KEY`, following the same pattern as the other four provider keys.

## 4. agentkit v0.16.0 — the `toolkit` subpackage (the release this design consumes)

`github.com/ikigenba/agentkit/toolkit` supplies six standard coding tools as ready-made `agentkit.Tool` values, via per-tool constructors and `All(root)`:

```go
func All(root string) []agentkit.Tool          // Bash, Read, Write, Edit, Glob, Grep, in that order
func Bash(root string, _ ...Option) agentkit.Tool  // + Read, Write, Edit, Glob, Grep
```

Behavior, verified against the tagged source and empirically where noted:

- **Confinement**: file tools resolve paths against `root` with symlink-aware checks (`EvalSymlinks` on root and the longest existing ancestor, `filepath.Rel` containment). Toolkit's own doc states this protects against *accidental* filesystem access and is not a security sandbox; `Bash` runs with `root` as its working directory but is not confined.
- **Bash**: `command` + optional `timeout` (ms, default 120000, no ceiling). Runs `bash -c` in its own process group; on timeout or context cancellation it SIGKILLs the whole group and returns output with a `[command timed out after Nms]` / `[command cancelled]` marker. A nonzero exit is **not** a tool error — output gets an appended `[exit status N]` marker.
- **Output caps**: every tool result is capped at 30,000 characters (rune-safe), with a `[output truncated: showing first N of M characters]` marker.
- **Read**: whole file or `offset`/`limit` line window (1-based offset), negative values rejected. **Changed in v0.8.0**: it now refuses non-text files with an error naming the **detected content type**, rather than decoding binary data as text. Detection is by content, not extension, so an extensionless UTF-8 file still reads and a `.txt` file holding binary does not. PDFs and images are refused by this path.
- **Write**: creates parent directories; returns `wrote <path>`.
- **Edit**: exact-string replace; empty `old_string` rejected; a non-unique match without `replace_all` is refused with the occurrence count.
- **Glob**: plain patterns via `filepath.Glob`; `**` patterns via a walk that skips `.git`. Returns a sorted JSON array of root-relative slash paths. **Known gap (empirically verified)**: a non-`**` pattern is joined to the base *without* confinement, so `pattern: "../*"` lists entry names outside `root` (list-only — Read/Write/Edit still refuse those paths). Accepted for prompts (Bash is unconfined anyway, so this widens nothing).
- **Schemas (v0.12.0)**: all six tools now carry generated schemas with a description on every input property and required fields aligned to each tool's contract, replacing the earlier bare property names.
- **Options (v0.16.0)**: every constructor accepts `...Option`; `WithBaseURL` and `WithHTTPClient` configure the network tools only. The six local tools ignore them.

**Available but not adopted.** Three capabilities exist in this release and are deliberately left unconsumed by the minimal bump, each a candidate for a later spec extension:

- `toolkit.WebSearch(BraveAPIKey, opts...)` (v0.14.0, key type v0.15.0) and `toolkit.WebFetch(opts...)` (v0.14.0) — neither joins `All`, which still returns exactly the six local tools. `WebFetch` overlaps prompts' own `Fetch` sandbox tool (D21), which is content-plane confined to loopback and is not the same thing.
- The `ocr` subpackage (v0.8.0, cache layout reworked v0.9.0) — `ocr.New(ocr.APIKey(key), opts...)` plus `ocr.Tool(root, cacheDir, backend)`, extracting text from scanned PDFs and raster images through OpenRouter's `file-parser` plugin, writing a `transcript.md` under `<root>/ocr/` and returning a bounded preview plus its path. This bears directly on D23, which currently boxes PDF handling into shell `pdftotext` and names model-native PDF a non-goal.

## 5. agentkit v0.16.0 — the one-shot and embedding footprint (the unified-inference dependency)

Facts verified against the tagged source that the completion/embedding/accounting design consumes:

**Stateless multi-turn one-shots.** `agentkit.Conversation` carries a public `History []Message` field; a stateless caller sets `History` to prior turns and calls `Send(ctx, lastUserText)` once. `Message` roles are `RoleUser` / `RoleAssistant` (`block.go`). A `Conversation` with empty `Tools`/`DeferredTools` performs exactly one provider round-trip per `Send` — the tool loop never engages — making it the natively supported one-shot completion primitive.

**Usage grain (design-constraining).** Per-provider-round-trip usage is **not** consumer-visible: `stream.Usage()` / `stream.Cost()` are aggregates for the whole `Send`, and the JSONL `Log` emits **one** `usage` record per `Send` (after the tool loop; `orchestration.go:234`, confirmed by `log_test.go`'s expected sequence carrying one `usage` across a two-round-trip turn). Any accounting grain finer than one-row-per-`Send` would require an agentkit change or a provider-seam fork.

**Embeddings.** `agentkit.Embedder{Provider, Model, Pricing *EmbeddingPricing, Dimensions, Retry}` with `Embed(ctx, inputs []string, role InputType) (*EmbedResult, error)` — batch in, vectors out, dimension-checked against the request. Roles: `InputDocument` / `InputQuery`. Embedder constructors exist on exactly two provider sub-packages: `openai.NewEmbedder(cred, opts...)` and `google.NewEmbedder(cred, opts...)` (both return `agentkit.EmbeddingProvider`, an interface — fakeable). anthropic/zai/openrouter have no embedder.

**Embedding catalog.** `catalog.Entry.Embedding *EmbeddingInfo` carries `Pricing agentkit.EmbeddingPricing`, `NativeDimension`, `MinDimension`, `MaxDimension`, `MaxInputTokens`. Entries at v0.6.0: `text-embedding-3-small` (openai, native 1536, min 1), `text-embedding-3-large` (openai, native 3072, min 1), `gemini-embedding-001` (google, native 3072, min 128). So embedding requests are catalog-validatable (model + dimension range) exactly like chat configs. `Entry.Embedding` survived the offering restructure unchanged — it hangs off the entry, not off an offering, because an embedding model is served by exactly one provider.

**Suite context (non-contractual).** wiki today embeds agentkit directly for five structured-generation call sites plus openai embeddings, logging every attempt to its own `llm_calls` SQLite table with stage/job_id/attempt filters — the consumer shape the `/complete`+`/embed`+`calls` surface is designed to absorb. wiki's conversion is wiki's own spec; the suite-level decision (prompts owns all inference, spend, and reporting) was settled in the unified-inference discussion of 2026-07-19.

## 6. OpenAI subscription auth — `agentkit/openai/subscription` + the `oauth` CLI (the subscription-auth dependency)

Facts verified against the tagged agentkit source and the `oauth` CLI repo (`github.com/ikigenba/oauth`):

**The credential file format.** `openai/subscription` (reworked in v0.5.0) consumes the **raw OAuth token-endpoint JSON response** — `{access_token, refresh_token, id_token}` — not a wrapper format. `subscription.Load(path) (*Store, error)` parses it, requires a non-empty `access_token`, and derives the ChatGPT account id from the `https://api.openai.com/auth` JWT claim (id_token first, access_token fallback); a missing claim fails the load. Login itself was deliberately removed from agentkit in v0.5.0 — producing the file is an external tool's job.

**Refresh semantics.** `Store.Token(ctx) (bearer, accountID, error)` refreshes when the access token expires within a 5-minute skew, POSTing `grant_type=refresh_token` to `https://auth.openai.com/oauth/token` with the pinned client id `app_EMoamEEZ73f0CkXaXp7hrann`, then **atomically rewrites the file** with the new response (preserving `refresh_token`/`id_token` when the response omits them). The refresh-token lineage **rotates**: a stale copy of the file is dead after the live copy refreshes, so exactly one process may own a file, and copies must never be shared between machines. Refreshes are serialized **within one Store instance only** — nothing protects two Stores opened on the same path, which forces the one-store-per-process-per-file rule.

**Provider integration.** `openai.Subscription(ts TokenSource) Credential` (TokenSource = `Token(ctx) (bearer, accountID, error)`, satisfied by `*subscription.Store`) flips the provider onto the `https://chatgpt.com` base URL and gives it `Identity{Provider: ProviderOpenAI, Auth: AuthSubscription}`, whose `String()` renders the familiar `openai.subscription` (vs `openai` for key auth). Costs agentkit resolves for subscription-authenticated turns are **notional API-rate equivalents**, not subscription spend (documented in `openai/openai.go`). The subscription credential is Responses-surface only — the embedder constructors take API-key credentials.

**The `oauth` CLI.** Provider-agnostic authorization-code + PKCE login: serves its own loopback callback, opens the browser, exchanges the code, and writes the token endpoint's JSON response **verbatim to stdout** (human output on stderr; failed login writes nothing, exits non-zero) — exactly the file `subscription.Load` consumes. Its own `--help` carries the OpenAI worked example: `--auth-url https://auth.openai.com/oauth/authorize --token-url https://auth.openai.com/oauth/token --client-id app_EMoamEEZ73f0CkXaXp7hrann --scope "openid profile email offline_access" --port 1455 --callback-path /auth/callback` (matching OpenAI's registered `http://localhost:1455/auth/callback`). opsctl init-box installs it to `/usr/local/bin` on every box (its D11); on a headless box the printed authorize URL plus an `ssh -L 1455:localhost:1455` forward completes the flow.

**On-box home.** `/opt/prompts/state/` is the durable, service-owned tree: deploy chowns it to `prompts:prompts` and never touches its contents — the correct home for a file the service must rewrite and that must survive deploys. `/opt/prompts/etc/` is deploy-owned versioned config (root-written, 0644) and is wrong for a mutable credential. The SSM/env secret path is also wrong: it delivers static values from the workstation, and a pushed copy of a rotating lineage goes stale after the first on-box refresh.
