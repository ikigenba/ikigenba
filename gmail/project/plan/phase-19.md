# Phase 19 — Structured MCP results, output schemas, and typed error codes in `internal/mcp`

*Realizes design Decision 20 (structured MCP adoption — results/schemas/codes
slice: R-8K29-UF3R, R-8LA6-86UG, R-8MI2-LYL5, R-8NPY-ZQBU, R-8OXV-DI2J,
R-8RDO-51JX). Depends on Phase 11 (D10 — the `internal/mcp` tool table over the
appkit chassis).*

This phase makes gmail green again against the new appkit contract and adopts
the structured-MCP result shape across gmail's ten mailbox tools, all within
`internal/mcp` (`tools.go` + `tools_test.go`). No result field is added,
removed, renamed, or reshaped — the change is result *plumbing*, output schemas,
and error codes over the JSON gmail already emits.

Observable end state:

- Every domain success returns `appkitmcp.StructuredResult(v)` (was
  `JSONResult(v)`), so each result carries `structuredContent` (the machine
  rendering) plus the mirrored text block; the marshal error propagates on the
  handler's `error` return, never swallowed. No `JSONResult` token remains in
  gmail's non-test Go source.
- Each of the ten `appkitmcp.Tool` values declares an `OutputSchema` literal
  (built with the existing `obj` helper plus a shared `messageSchema()` for the
  `read`/`thread` message object), mirroring the emitted JSON per D20 §2. gmail
  has no prose-exception domain tool, so all ten declare a schema.
- Every tool error carries a typed code: the pre-call argument guards emit
  `appkitmcp.ErrValidation`; a new private `errorResultFor(err)` maps
  `Client` errors — `gm.ErrValidation`→`validation`, `gm.ErrNotFound`→
  `not_found`, default (incl. `gm.ErrInvalidGrant`)→`source_unavailable` — per
  D20 §3.

**Done when:**

- `cd gmail && go build ./... && go vet ./... && gofmt -l . && go test ./...`
  all succeed with zero failures and no `gofmt` output (design Conventions:
  "the suite is green").
- The realized ids are each covered by a clearly-named test in
  `internal/mcp/tools_test.go`, green:
  - R-8K29-UF3R — `tools/list` surfaces a non-nil `outputSchema` for each of the
    ten domain tools (table-driven; fails if any domain tool omits its schema).
  - R-8LA6-86UG — every domain tool's success result carries `structuredContent`
    deep-equal to the JSON parsed from its own mirrored text block.
  - R-8MI2-LYL5 — an argument-validation miss (`read` no `id`, `label` no
    `label_id`, `send` no `to`) returns `isError` with
    `structuredContent.code == "validation"` and makes zero `Client` calls.
  - R-8NPY-ZQBU — a `Client` call returning `gm.ErrNotFound` yields
    `structuredContent.code == "not_found"`.
  - R-8OXV-DI2J — a `Client` call returning `gm.ErrInvalidGrant` and a generic
    transport error each yield `structuredContent.code == "source_unavailable"`,
    while `gm.ErrNotFound` on the same tool still yields `not_found`.
  - R-8RDO-51JX — structural:
    `cd gmail && grep -rn 'JSONResult' internal cmd --include='*.go' | grep -v _test.go`
    returns empty output.
