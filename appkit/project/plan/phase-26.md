# Phase 26 — Gate the MCP transport on HTTP method before decoding the body

*Realizes design Decision 8 (the `appkit/mcp` JSON-RPC transport), the single
id R-MSET-O79A. Depends on no pending phase.*

`mcp/` is the only package this phase touches. `Handler.ServeHTTP` inspects the
request's HTTP method before it reads the body: anything other than `POST` is
answered with status `405` and an `Allow: POST` header, and returns without
decoding a body or writing a JSON-RPC envelope. `POST` proceeds into the
existing decode-and-dispatch path unchanged.

Nothing else about the transport moves. The JSON-RPC error taxonomy keeps its
current behavior exactly, including `-32700` for a malformed body on a `POST`
and the HTTP `200` that carries every JSON-RPC error — D8 rejects widening the
fix to those codes, so a change there is out of this phase's scope. No tool
table, result helper, construction guard, or route registration changes, and
nothing outside `appkit/` is edited.

**Done when:**

- `R-MSET-O79A` is covered by an id-tagged test in `mcp/` asserting, through
  the real `ServeHTTP` seam with a test tool table: a body-less `GET` returns
  status **exactly 405** and **not 200**, carries an `Allow` header naming
  `POST`, and returns a body containing **no** `jsonrpc` key and no `-32700`
  code; a `DELETE` behaves identically; a `GET` carrying a **well-formed**
  JSON-RPC body still returns 405 (proving the gate runs before the decode,
  not because the body was unreadable); and a `POST` `tools/list` over the same
  handler still returns its normal result.
- The test fails against the pre-phase handler: with the method gate removed,
  the body-less `GET` assertion goes red on a `200` parse error.
- `R-MIN1-KS98` still passes unmodified — the malformed-body `-32700` behavior
  is untouched by this phase, and its test is not edited to accommodate the
  change.
- The suite is green as design's *Conventions* define it: `go build ./...`,
  `go vet ./...`, and `go test ./...` each exit 0 with no failures, plus the
  isolated `GOWORK=off go build ./...` check.
