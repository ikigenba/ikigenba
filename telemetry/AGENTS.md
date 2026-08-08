# telemetry

telemetry is the suite's forensic record store under `/srv/telemetry/`. It is an
appkit binary backed by SQLite, with module path `telemetry`. It is neither an
event-plane producer nor an event-plane consumer, and it has no web surface or
token logic.

Changes are made through the specifications in `project/` and proceed only in
the direction allowed by the current project gate. Keep implementation and
documentation changes aligned with the accepted project decisions and
requirements.

## Layout

- `cmd/telemetry` is the composition root and service binary.
- `internal/record` defines forensic records.
- `internal/db` owns SQLite persistence and migrations.
- `internal/ingest` accepts records for storage.
- `internal/retention` prunes expired records.
- `internal/mcp` provides the read-only MCP surface.
- `internal/e2e` contains composed end-to-end tests.
- `etc/` contains deployment, manifest, and proxy configuration.
- `project/` contains the service specification and build-loop state.

## Tests

The default gate is `go test ./...`, run from `telemetry/`.

The test layers present are **hermetic** and **composed**. Composed tests are
`internal/e2e/` and the boot smoke in `cmd/telemetry/main_test.go`; everything
else is hermetic. There is no live layer and no tree-local manual layer.

There are no environmental preconditions beyond the Go toolchain.

Local development uses telemetry's own `go.work`. The production build uses
`GOWORK=off` through `bin/ship telemetry`.

## Versioning

The version is committed in `VERSION`. Advance it with `bin/bump telemetry` and
produce the standalone release with `bin/ship telemetry`.
