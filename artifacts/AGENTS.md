# artifacts

The artifacts service stores uploaded blobs and exposes their lifecycle through
the suite's MCP and HTTP surfaces. nginx is the trust boundary; the service is
loopback-only and uses the shared appkit chassis.

## Layout

- `cmd/artifacts/`: composition root and shipped-artifact guards.
- `internal/artifacts/`: upload, download, content-plane, and event domain.
- `internal/db/`: SQLite store and embedded migrations.
- `internal/mcp/`: MCP tool table.
- `internal/web/`: landing page and Carbon assets.
- `etc/`: authored manifest and nginx configuration.

## Tests

- Default gate: `go test ./...` from `artifacts/`. Green also means clean
  `go build ./...`, `go vet ./...`, and silent `gofmt -l .`.
- Layers present: **hermetic** and **composed**; there is no **live** layer.
  Hermetic tests use temp-file SQLite through the real appkit migration runner,
  the real eventplane outbox, temp-dir filesystems, deterministic injected
  clocks, `httptest`, committed files, real loopback HTTP, and headless Chrome.
  Composed tests build the real binary, run `serve`, and check `/health` over
  loopback.
- Environmental preconditions beyond the Go toolchain: a POSIX `bash` with
  `grep`, and a headless-Chrome binary. Missing tools are hard failures, never
  skips.
- GOWORK mode: **workspace** through the repo-root `go.work`; production builds
  use `GOWORK=off` through `bin/ship artifacts`.

There is no tree-local manual runbook; the assembled-stack check belongs to the
suite manual layer.

## Versioning

`VERSION` is the product version and starts at `v0.1.0`.
