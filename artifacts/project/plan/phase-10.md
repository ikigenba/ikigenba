# Phase 10 — Mount the MCP surface in the composition root

*Realizes design Decision 6 (MCP tool surface) — slice: R-P52Q-H8MG only.*

The assembled service serves its MCP surface. `cmd/artifacts/main.go`'s
`Handlers` hook builds the handler from the existing tool table
(`internal/mcp.New(svc, rt.Version())`) and mounts it on the served router as
`rt.Handle("POST /mcp", rt.RequireIdentity(handler))`, per D6's mount
declaration and the suite chassis pattern. No tool, schema, or domain change:
the table and its behavior ids are already realized; this phase wires the
surface and proves reachability through the running binary.

The proof extends the existing composed boot smoke pattern in
`cmd/artifacts/main_test.go` (R-39K3-W9HR's substrate): build the real binary,
run `serve` against a temp `state/`, then drive `POST /mcp` over loopback.

**Done when:**

- R-P52Q-H8MG — the composed smoke's `POST /mcp` with `X-Owner-Id` completes
  MCP `initialize` and a `tools/list` naming every domain tool (`upload`,
  `import`, `list`, `get`, `update`, `set_visibility`, `delete`, `guide`);
  without identity headers the same request is refused `401` and runs no
  tool — asserted against the running binary, never a test-constructed
  handler; tagged `// R-P52Q-H8MG` in a `*_test.go` file under `cmd/artifacts/`.
- The suite is green: `cd artifacts && go build ./... && go vet ./... &&
  go test ./...` all exit 0 and `gofmt -l .` prints nothing.
