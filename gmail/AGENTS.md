# gmail

gmail is a deployable, path-routed service in the ikigenba suite, mounted at
`/srv/gmail/` (Go module `gmail`, over SQLite on the shared appkit chassis). It
is a loopback-only Gmail connector: a bearer-gated MCP surface for agents plus a
Gmail History API poll daemon, and an event-plane producer that publishes
`mail.*` facts to its outbox `/feed`. nginx routes `/srv/gmail/` and stays the
sole trust boundary; the service accepts the trusted identity headers as input,
runs no token logic, and reads its Gmail OAuth secrets only from the environment.

## How changes are made

Changes go through the spec under `project/`, not direct edits: settle the spec,
then let the build loop realize it. Edit code directly only on explicit operator
instruction. See the `$ikispec` skill for the `project/` spec contracts and
`$ralph` for the unattended build workflow.

## Layout

- `cmd/gmail/`: binary entrypoint, builds the `appkit.Spec` and verb dispatch.
- `cmd/consent/`: one-time OAuth consent CLI.
- `internal/mcp/`: Gmail MCP tools, handler, and the published-event registry.
- `internal/gmail/`: Gmail client and the producer engine (poll loop, outbox).
- `internal/db/`: migration `FS` and load guards; SQLite/migrations are appkit's.
- `share/`: the human web landing assets served via `Spec.WWW`.
- `project/`: the spec (product/design/plan) the build loop works from.

## Tests

- Package checks from this directory: `go build ./...`, `go vet ./...`,
  `gofmt -l .`, `go test ./...`.

## Versioning

The committed `gmail/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.9.1`). Advance it with `bin/bump gmail <major|minor|patch>`;
ship with `bin/ship gmail`. Git tags are not the version mechanism.
