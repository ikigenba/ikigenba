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

**Availability precondition:** the published tag `github.com/ikigenba/agentkit v0.2.0` (agentkit phase 48). Local dev builds resolve agentkit through the repo-root `go.work` replace to `~/projects/agentkit`; the production build (`GOWORK=off`) needs the tag.

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
