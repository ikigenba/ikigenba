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

**Availability:** first published in `v0.2.0`; the API is unchanged through `v0.6.0`, the release prompts now pins (§5).

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

**Typed-credential constructors (v0.4.0).** Every provider sub-package's `New` takes a closed `Credential` type, not a string: `anthropic.New(anthropic.APIKey(key), opts...)`, likewise `openai`, `google`, `zai`, and the new `openrouter`. All five expose `WithBaseURL` and `WithHTTPClient` options. The legacy model registries and the `Provider.Pricing(model)` method are **gone** — the `agentkit.Provider` interface is now just `RoundTrip(ctx, *Request) *RoundTrip` + `Name()`. (v0.5.0's subscription-login rework touches only `openai/subscription`, which prompts does not use.)

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
