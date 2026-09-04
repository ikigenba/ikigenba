# D17-export-contract

agentkit is the root of a family of sibling sub-projects — `toolkit`, `ocr`,
`catalog`, `mcp`, `embed` (D0) — that build *on top of* it. What agentkit exports
is therefore a deliberate contract, not an accident of which identifiers happen to
be capitalized. This document fixes what the root package promises to siblings and,
just as load-bearing, what it **refuses** to export so a sibling cannot couple to
an internal. The rule is: export the *seam shapes* siblings compose against;
withhold the *mechanisms* the root owns.

The centerpiece is **tool authorship**. A sibling like `toolkit` defines tools
against a small, stable surface (full detail in D9); agentkit exports it and
nothing more:

```go
package agentkit

// Tool is the sealed interface every tool satisfies (D9). Sealed via an
// unexported marker: siblings construct tools through the constructors below,
// never by declaring their own Tool implementation.
type Tool interface {
	Name() string
	Call(ctx context.Context, input json.RawMessage) (string, error)
	// ... schema accessor + unexported marker (D9)
}

// NewTool is the primary constructor: it derives the JSON schema from the In type
// parameter's `jsonschema` struct tags and RETURNS AN ERROR on a malformed tag or
// unsupported type. This replaces the old library's panic-only constructor, whose
// failure mode meant config-driven tool data could crash the host process.
func NewTool[In any](name, desc string, fn func(context.Context, In) (string, error)) (Tool, error)

// MustTool is the panicking sibling of NewTool, for static tool definitions where
// a bad schema is a programming error the author wants surfaced at init.
func MustTool[In any](name, desc string, fn func(context.Context, In) (string, error)) Tool

// NewToolFromSchema builds a tool from a runtime-sourced schema for tools that
// have no Go input type — the mcp sibling's remotely-discovered tools, above all.
// This is a narrow, deliberate reversal of the old design's refusal of raw-schema
// tools; the root still validates the schema against the canonical subset at Send.
func NewToolFromSchema(name, desc string, schema json.RawMessage, fn func(context.Context, json.RawMessage) (string, error)) (Tool, error)
```

Around those constructors agentkit exports the supporting vocabulary a tool author
needs and nothing they do not: the **`jsonschema` struct-tag vocabulary** as a
*documented string contract* (the tag names and their meanings are the API; there
are no exported tag-name constants to bind to); **`ValidateToolSchema`** so a
sibling can check a schema against the canonical subset before it ever constructs
a tool; the **error taxonomy** — the `Error` struct, its `Category` enum, the
`ErrInvalidConfig` / `ErrClosed` sentinels, and `Retryable(err) bool` (D4) — so a
sibling classifies failures the same way the root does; the **value types**
`Pricing`, `Cost`, `Usage`, and `Identity` (D1, D3) that flow across the seam; the
**`agentkit/retry`** leaf (stdlib-only `Clock` / `Policy` / `Do`, D14) as a shared
retry primitive; and the **SSE frame reader** `SSEFrames` (D5), the one piece of
transport plumbing a sibling legitimately reuses.

```go
package agentkit

// ValidateToolSchema checks a JSON schema against the canonical subset (D9) that
// every wire can render. Siblings call it to fail early, before constructing a
// tool or advertising a discovered one.
func ValidateToolSchema(schema json.RawMessage) error

// SSEFrames splits a server-sent-events stream into raw frames. Exported as a
// leaf so the mcp sibling can decode an SSE-carried JSON-RPC response with the
// same reader the wire codecs use, rather than reimplementing frame boundaries.
func SSEFrames(r io.Reader) iter.Seq2[[]byte, error]
```

Equally deliberate is what agentkit **refuses to export**. It does not export
`validateToolArguments`: argument validation against a tool's schema happens once,
in the root, immediately before dispatch (D11); a sibling that re-validated would
duplicate the gate and risk drifting from it. It does not export any **`Warning`
channel** for tool authors — a tool's `Call` returns `(string, error)`, and an
error becomes an in-band `IsError` result (D12, D16); the `Warning` type was cut
from the whole design (D4), and re-introducing it at the tool seam would resurrect
it. It does not export the **30k output-cap constant**: truncating oversized tool
output is `toolkit`'s own convention, not a root invariant, so the number lives in
`toolkit`. And it deliberately exports **no shared `WithBaseURL` / `WithHTTPClient`
symbols**: each module — root, each sibling — defines its own `Option` type, so
a base-URL option on one is not assignable to another; the root itself has no
option type, since the base URL is a positional parameter of `NewEndpoint`.

The sibling that pushes hardest on this contract is **`mcp`** (D-B), and it
validates the export list precisely. `mcp.Discover(ctx, server)` returns
`[]agentkit.Tool` built with **`NewToolFromSchema`** (remote tools have no Go
type) and decodes `text/event-stream` responses with **`SSEFrames`** — the two
exports that exist largely for its sake. Its failure surfaces split cleanly along
the seam the taxonomy already draws: a **discovery** failure is an ordinary Go
error returned from `Discover`, surfaced in consumer code *before* `Send`; a
tool-call failure against a **dead MCP server mid-turn** comes back as an in-band
`IsError` `ToolResult`, which the model can react to, never as `Stream.Err()`.
Because the root validates every tool's schema at `Send`, a discovered tool whose
schema escapes the canonical subset fails the turn with `ErrInvalidConfig`, not at
some later opaque point — the same gate that governs hand-written tools.

## REQUIREMENTS

- R-5ZBT-XCT3: The root package MUST export the sealed `Tool` interface together with `NewTool[In]`, `MustTool[In]`, and `NewToolFromSchema` as the sole tool-construction surface, and `Tool` MUST NOT be implementable outside agentkit.
- R-60JQ-B4JS: `NewTool[In]` and `NewToolFromSchema` MUST return an error on a malformed schema rather than panic, and `MustTool[In]` MUST be the only panicking constructor.
- R-61RM-OWAH: The `jsonschema` struct-tag vocabulary MUST be specified as a documented string contract with no exported tag-name constants.
- R-647F-GFRV: The root MUST export `ValidateToolSchema`, the error taxonomy (`Error`, `Category`, `ErrInvalidConfig`, `ErrClosed`, `Retryable`), the value types `Pricing`/`Cost`/`Usage`/`Identity`, the `agentkit/retry` leaf, and the `SSEFrames` reader as the shared sibling-facing vocabulary.
- R-65FB-U7IK: The root MUST NOT export its argument-validation routine, any tool-author `Warning` channel or type, the output-cap constant, or a shared base-URL/HTTP-client option symbol; each module MUST define its own `Option` type.
- R-66N8-7Z99: A tool built by a sibling via `NewToolFromSchema` MUST be validated against the canonical subset at `Send` and fail the turn with `ErrInvalidConfig` on a non-conforming schema, identically to a root-authored tool.
- R-67V4-LQZY: The export set MUST be sufficient for the `mcp` sibling to build `[]Tool` from remote schemas and decode SSE-framed responses, with discovery failures surfaced as a returned error before `Send` and mid-turn call failures surfaced as an in-band `IsError` result rather than `Stream.Err()`.
