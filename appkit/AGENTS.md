# appkit

The shared Go chassis library for the ikigenba suite. A service's `main.go`
collapses to one `appkit.Main(appkit.Spec{…})` call; appkit supplies the fixed
verb dispatcher (`serve`/`version`/`manifest`/`migrate`/`schema`), config-from-env,
the migration runner and downgrade guard, the loopback server (PRM, identity gate,
`/feed`, consumer loops), the MCP transport, and manifest emit/parse. Module path:
`appkit`, consumed by every service via a committed `replace`. It is a library, not
a deployable service, and knows nothing about LLMs (that is `agentkit`).

## How changes are made

Changes go through the spec under `project/`, not direct edits: settle the spec,
then let the build loop realize it. Edit code directly only on explicit operator
instruction. See the `$ikispec` skill for the `project/` spec contracts and
`$ralph` for the unattended build workflow.

## Layout

- Root `.`: the `appkit` package and verb dispatcher (`appkit.go`, `verbs.go`).
- `config`, `db`, `server`, `mcp`, `feed`, `manifest`, `inventory`, `web`,
  `logging`: the chassis subsystems.
- `internal/testmigrations`: fixtures for the chassis's own tests.
- `project/`: the spec the build loop works from.

## Tests

- Unit: `go test ./...`
- Isolated build check (mirrors the prod build): `GOWORK=off go build ./...`

## Versioning

Not versioned. appkit is a shared library consumed via a committed `replace`, with
no `VERSION` file and no git tag. Each service binary's `version`/`commit` are
stamped in at build time via `-ldflags`, not carried by appkit.
