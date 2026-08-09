# repos

repos is a core-block service at `/srv/repos/` on loopback port `3007`. It owns
first-party repository custody, exposes its public page and MCP transport, and
publishes domain events on `/feed`. The v2 custody routes and tools are built in
incremental phases over the shared appkit chassis and SQLite store.

The service is a producer only. It has no feed consumers, peer clients, or
autonomous execution engine. Configuration is read from `REPOS_*` environment
variables at the composition root and passed into domain components.

## How changes are made

Changes go through the spec under `project/`, not direct edits. The spec is
written only by an operator-invoked authoring move or by the unattended build
loop's authorized completion mutations. In other sessions it is read-only
reference.

## Layout

- `cmd/repos/`: binary composition and appkit verb dispatch.
- `internal/repos/`: repository custody domain and persistence.
- `internal/mcp/`: owner-facing MCP transport and tools.
- `internal/db/`: SQLite migration embedding and DDL guards.
- `etc/`, `share/`: deployment configuration and public web content.
- `project/`: the specification and unattended loop state.

## Tests

Run the default gate from `repos/` with `go test ./...`. A green change also
requires `go build ./...` and `go vet ./...` to succeed and `gofmt -l .` to
print nothing.

The tree has exactly two test layers, both in the default gate:

- **hermetic** tests use temporary filesystems, real temp-file SQLite, local
  subprocesses, and loopback HTTP peers.
- **composed** testing is the install-layout boot smoke in
  `cmd/repos/main_test.go`, which builds and runs the real service.

There is no live layer and no tree-local manual runbook. Tests do not contact
non-loopback services or depend on an assembled suite.

The real `git` binary must be on `PATH`; its `git http-backend` and repository
plumbing are exercised directly against temporary local fixtures and absence is
a hard failure. The `go` binary must be on `PATH` in the test process's
environment for the composed smoke, with the module cache resolving repos'
`replace` siblings. Tests, builds, and vet run in workspace mode through the
repo-root `go.work`. The real `google-chrome` binary must also be on `PATH` for
the hermetic browser wiring proof; its absence or failure to launch is a hard
failure, never a skip. The production build forces `GOWORK=off`; that mode is
not part of the default gate.

## Versioning

The committed `VERSION` file is the v-prefixed version source of truth. Advance
it with `bin/bump repos <major|minor|patch>` and ship with `bin/ship repos`.
