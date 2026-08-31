# D0-scope-and-siblings

Scope preamble for agentkit. This document carries no requirements and mints no
ids — it fixes the boundary the numbered design docs (D1–D17) are authored
inside. Its job is to keep the in/out line and the pending sibling projects from
being lost. Anything an in-scope seam exists *for the sake of* a sibling is noted
here so the obligation survives even before that sibling is built.

## In agentkit

Message + block model; the four wires and five endpoints; credentials;
generation settings + reasoning representability; tool definition + the canonical
schema subset + per-wire schema rendering + runtime argument validation; the
turn/orchestration loop; deferred tools + the `load_tools` meta-tool; the
message-granular event stream; usage; cost + the shipped chat-only rate table;
errors; the JSONL event log; `agentkit/retry` (public leaf); the SSE frame reader
(public leaf); the sibling-facing export contract.

## Out to sibling sub-projects (pending — NOT designed in agentkit's specs)

These are separate sub-projects in the ikigenba monorepo. They are not designed
here, but agentkit's export surface must leave room for them. Do not fold their
functionality back into agentkit.

- **`toolkit`** — bash / read / write / edit / glob / grep + WebSearch / WebFetch.
- **`ocr`** — document text extraction.
- **`catalog`** — advisory extras: context windows, reasoning vocabularies, model
  discovery.
- **`mcp`** — remote MCP tool discovery. Consumer does
  `tools, err := mcp.Discover(ctx, server)` → `[]agentkit.Tool`, appended to
  `conv.Tools`. (D-B.) This is why agentkit MUST export `NewToolFromSchema`
  (runtime-schema tools) and the SSE frame reader as public leaves.
- **`embed`** — embeddings surface (`Embedder`, `Embed`, `EmbeddingProvider`).
  (D-H.) Owns its own trivial API-key-only auth; ships no rate table.

## Day-one endpoints

OpenAI, Anthropic, Google Gemini, xAI, OpenRouter. Z.ai is dropped; GLM is
reachable via OpenRouter (`z-ai/glm-*` slugs).

## What agentkit owes the siblings (export obligations to preserve)

- `NewToolFromSchema(name, desc, json.RawMessage, fn)` — runtime-schema tools with
  no Go type, for `mcp` and any dynamically-sourced tool source. Root still
  validates the schema against the canonical subset at `Send`.
- The SSE frame reader as a public leaf — `mcp` reuses it for `text/event-stream`
  responses.
- The sealed `Tool` interface, `Pricing` / `Cost` / `Usage` / `Identity`, the
  error taxonomy + `Retryable(err) bool`, and `agentkit/retry` — the shared
  vocabulary every sibling builds against.
- The rate table is CHAT-ONLY and lives in agentkit as the one deliberate
  exception to "values live in siblings." `embed` ships no rate table.

Full export contract is designed in D17 (the sibling-facing export contract).
