# Phase 10 — Method-restrict the MCP route at the composition root

*Realizes design Decision 1 (service skeleton, chassis Spec & composition root),
the single id R-NTSI-XWI1. Depends on no pending phase.*

`cmd/telemetry` is the only package this phase touches. The composition root
registers the MCP route with the method-qualified pattern D1 specifies,
`rt.Handle("POST /mcp", rt.RequireIdentity(mcpHandler))`, so the chassis router
answers any other verb itself with `405` and an `Allow: POST` header instead of
passing it to a handler that decodes the body before looking at the method. The
bare-path registration currently in `main.go` is the drift being corrected; no
other route, Spec field, or handler changes, and nothing outside
`cmd/telemetry/` is edited.

The proof lives beside D1's existing route and manifest tests in
`cmd/telemetry/main_test.go`, and it goes over the wire: the real composed
`appkit.Spec` on a real ephemeral `127.0.0.1` listener, driven by a real
`http.Client`. A test that called the handler's `ServeHTTP` directly would pass
even with the route unregistered, which is exactly the defect at issue, so that
shortcut is not available here.

**Done when:**

- `R-NTSI-XWI1` is covered by an id-tagged test in
  `cmd/telemetry/main_test.go` asserting, against the real served Spec over a
  real loopback listener: `GET /mcp` returns status **exactly 405** and
  **not 200**, with an `Allow` header naming `POST`; `DELETE /mcp` likewise
  returns 405; and `POST /mcp` still reaches the handler, returning a decodable
  JSON-RPC response to a real `tools/list` call.
- The test fails against the pre-phase registration. Reverting `main.go` to
  `rt.Handle("/mcp", ...)` makes it red on the `GET` assertion (200, parse
  error); restoring `POST /mcp` makes it green.
- The suite is green as design's *Conventions* define it: `cd telemetry` and
  `go build ./...`, `go vet ./...`, `go test ./...` each exit 0 with no
  failures and no skipped tests among the ids this phase realizes.
- `grep -c 'rt.Handle("/mcp"' cmd/telemetry/main.go` returns 0.
