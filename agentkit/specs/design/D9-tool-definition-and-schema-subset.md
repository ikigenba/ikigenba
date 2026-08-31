# D9-tool-definition-and-schema-subset

A `Tool` is a name, a description, an input JSON Schema, and a call function. It is
a **sealed union** — an unexported marker keeps the set of constructions closed to
agentkit, so the orchestrator can trust every `Tool` it dispatches came through one
of the library's validated constructors. Consumers never implement `Tool`
directly; they build one from a Go type, from a raw schema, or (in a sibling like
`mcp`, D0) from a discovered schema, all of which land on the same validated
interface.

```go
package agentkit

// Tool is a callable the model may invoke. It is a sealed union — the unexported
// marker keeps construction inside agentkit's validated constructors, so the
// orchestrator can dispatch any Tool without re-checking its schema shape. Name
// is what the model calls; Schema is the canonical-subset input schema (below);
// Call runs the tool and returns its textual output or an error, both of which
// become an in-band ToolResult (D11).
type Tool interface {
	Name() string
	Description() string
	// Schema is the tool's input schema as canonical-subset JSON (see below).
	Schema() json.RawMessage
	// Call runs the tool. A returned error becomes an IsError ToolResult the
	// model may recover from (D11); it is never turn-ending.
	Call(ctx context.Context, args json.RawMessage) (string, error)
	isTool()
}
```

**Three constructors, one validated result.** The primary constructor is
generic over a Go input type and derives the schema from its `jsonschema` struct
tags. It **returns an error** rather than panicking, because a tool's schema is
often assembled from configuration data, and a config typo must not crash the host
process — the old design could only panic here, which made a data error fatal.

```go
// NewTool builds a Tool from a Go input type In, deriving the input schema from
// In's `jsonschema` struct tags. It returns an error if the derived schema falls
// outside the canonical subset (below) or the tags are malformed. fn receives the
// decoded In and returns the tool's textual output.
func NewTool[In any](name, description string, fn func(ctx context.Context, in In) (string, error)) (Tool, error)

// MustTool is the panicking sibling of NewTool, for a tool whose schema is a
// compile-time constant the author has already proven valid (e.g. a package-level
// var). Prefer NewTool wherever the schema derives from data.
func MustTool[In any](name, description string, fn func(ctx context.Context, in In) (string, error)) Tool

// NewToolFromSchema builds a Tool from a raw input schema with no backing Go
// type, for dynamically-sourced tools (a discovered MCP tool, D0). This is a
// deliberate, narrow reversal of the old design's refusal of raw-schema tools:
// a discovered tool has no Go type to derive from, yet must still be callable.
// The schema is still validated against the canonical subset — here and again at
// Send (D11) — so a runtime schema is no less safe than a derived one.
func NewToolFromSchema(name, description string, schema json.RawMessage, fn func(ctx context.Context, args json.RawMessage) (string, error)) (Tool, error)
```

**The `jsonschema` struct-tag vocabulary is a documented string contract, not a
set of exported constants.** A field tag reads like `jsonschema:"required" `or
`jsonschema:"enum=red|green|blue"`; the recognized keys (`required`, `enum`,
`description`, `minimum`, `maximum`, `minLength`, `maxLength`, `pattern`,
`format`, and the like) are documented in prose and parsed from the tag string.
They are intentionally **not** exported as Go constants: exporting them would
invite consumers to build tags by concatenation and freeze the vocabulary into the
API surface, where extending it later would be a breaking change. The contract is
the documented string grammar; the parser is free to grow.

**The canonical schema subset is the intersection every wire accepts.** Tool
schemas render onto four different wire dialects (D10), and the binding constraint
is the wire that trims hardest: it rejects schema constructs the others tolerate.
Rather than discover this per wire at request time, agentkit defines one canonical
subset that every wire can render, and validates every tool schema against it up
front. The subset is an object schema of typed properties (`object`, `string`,
`number`, `integer`, `boolean`, `array`, and nested `object`), with `required`,
`enum`, `description`, and the common numeric/string constraint keywords. It
**excludes** the constructs the hardest wire rejects: `$ref` and `$defs`
(no schema references — schemas must be inlined), `additionalProperties`,
unconstrained/tuple `items`, and the polymorphic combinators in their
tool-unfriendly forms (`anyOf`/`oneOf`/`allOf` beyond a nullable-single form).
`ValidateToolSchema` is the exported checker; it is what every constructor and the
`Send`-time gate (D11) call, and what `mcp` calls on a discovered schema.

```go
// ValidateToolSchema reports whether a raw schema lies within the canonical
// subset every wire can render. It is called by every Tool constructor, by the
// Send-time gate (D11), and by siblings validating a discovered schema (D0). A
// returned error names the offending construct (e.g. "$ref not permitted").
func ValidateToolSchema(schema json.RawMessage) error
```

Keeping the subset a single library-wide definition — rather than a per-wire
dialect hook — means a consumer's tool either works on every wire or fails
identically on all of them, which is the property that lets a tool set move
between vendors untouched. A wire that happens to accept more than the subset
(D10) still receives only canonical schemas; the extra latitude is unused on
purpose.

## REQUIREMENTS

- R-3Y5U-Z4BF: `Tool` MUST be a sealed union — an interface with an unexported marker — so every dispatchable tool originates from an agentkit constructor and no external type can satisfy it directly.
- R-3ZDR-CW24: `NewTool[In]` MUST return an error (never panic) when the schema derived from `In`'s `jsonschema` tags is malformed or outside the canonical subset, so a data-driven schema fault cannot crash the host.
- R-40LN-QNST: `MustTool[In]` MUST be the panicking sibling of `NewTool[In]`, producing an identical `Tool` for a schema proven valid at author time.
- R-41TK-4FJI: `NewToolFromSchema` MUST build a `Tool` from a raw input schema with no backing Go type, validating that schema against the canonical subset at construction.
- R-431G-I7A7: The `jsonschema` struct-tag vocabulary MUST be a documented string contract with no exported Go constants, so the vocabulary can grow without an API break.
- R-449C-VZ0W: agentkit MUST define one canonical schema subset — the intersection all four wires can render — and MUST reject `$ref`/`$defs`, `additionalProperties`, and tool-unfriendly `anyOf`/`oneOf`/`allOf` forms within it.
- R-45H9-9QRL: `ValidateToolSchema` MUST be exported and MUST be the single checker used by every constructor, by the `Send`-time gate (D11), and by siblings validating a discovered schema.
- R-46P5-NIIA: A tool schema accepted as canonical MUST be renderable by every shipped wire, so a tool set moves between vendors unchanged or fails identically on all of them.
