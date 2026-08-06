# Phase 09 — Bring the MCP input schemas into agentkit-subset conformance

*Realizes design Decision 8 (agentkit tool-schema subset conformance).*

`internal/mcp` advertises input schemas that agentkit's `Send` gate rejects,
which disables an agent's entire tool surface the moment telemetry is attached.
This phase makes the four advertised input schemas conform and proves it against
the real gate.

The end state, in `telemetry/internal/mcp`:

- The single `objectSchema` helper is gone, replaced by `inputObjectSchema`
  (authors no `additionalProperties`) and `outputObjectSchema` (keeps
  `"additionalProperties": false`). Every call site builds through the helper
  matching its role: `searchInputSchema` and the inline `chain`, `get`, and
  `guide` schemas through the input helper; `searchOutputSchema`,
  `chainOutputSchema`, and `recordSchema` through the output helper.
- `search`'s `limit` property declares `type`, `default: 50`, and a
  `description` giving the 1-to-500 range. `minimum` and `maximum` are gone. The
  handler's own range check and its `validation` error are unchanged.
- `recordSchema`'s nested `params` and `detail` still declare
  `{"type": "object", "additionalProperties": true}` — they live only inside
  output schemas and are untouched.
- A new `mcp_test.go` conformance test drives the real agentkit gate: a stub
  `agentkit.Provider` that records whether `RoundTrip` ran, a raw-tool type that
  embeds the sealed `agentkit.Tool` interface and overrides its four exported
  methods, one tool per entry of the production `Tools(...)` table carrying that
  entry's real `InputSchema`, and a single `Conversation.Send`.
- `telemetry/go.mod` requires `github.com/ikigenba/agentkit` at a released tag.
  It is imported from `_test.go` files only; nothing under `cmd/` or any
  non-test file references it.

No tool is added, renamed, or removed, no handler behavior changes, and no
output schema loses a key. D5's surface and D5's promises are untouched.

**Done when:**

- `R-D2X0-57VI` — a `Conversation` over the stub provider, carrying every entry
  of the production `Tools(...)` table with its real `InputSchema`, completes a
  `Send` with `Err() == nil` and the stub's `RoundTrip` recorded as called; a
  variant test that injects an excluded construct into one tool's input schema
  gets `errors.Is(err, agentkit.ErrInvalidConfig)` and no `RoundTrip`.
- `R-D44W-IZM7` — over the real MCP handler, `tools/list` shows `search`'s
  `limit` with no `minimum` and no `maximum` key, with `"default": 50`, and with
  a non-empty `description` containing both `1` and `500`; no input schema of
  any of the four tools carries an `additionalProperties` key at any depth; and
  `limit: 0` and `limit: 501` still return a `validation` error naming `limit`
  with no records.
- `R-D5CS-WRCW` — in the same `tools/list`, `search`, `chain`, and `get` each
  declare an `outputSchema` whose root carries `"additionalProperties": false`,
  and the record schema's `params` and `detail` carry
  `"additionalProperties": true`; `mcp.New` constructs the handler without
  error.
- Each id above appears verbatim as a `// R-XXXX-XXXX` tag in a `*_test.go`
  file.
- `grep -rn '"minimum"\|"maximum"\|"additionalProperties"' internal/mcp/mcp.go`
  returns only lines belonging to output-schema construction — no hit inside
  `searchInputSchema`, `inputObjectSchema`, or any tool's `InputSchema`
  expression.
- `cd telemetry` and `go build ./...`, `go vet ./...`, `go test ./...` each exit
  0 with no failures and no skipped tests.
