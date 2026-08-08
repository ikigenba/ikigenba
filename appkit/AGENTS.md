# appkit

The shared Go chassis library for the ikigenba suite. A service's `main.go`
collapses to one `appkit.Main(appkit.Spec{…})` call; appkit supplies the fixed
verb dispatcher (`serve`/`version`/`manifest`/`migrate`/`schema`), config-from-env,
the migration runner and downgrade guard, the loopback server (PRM, identity gate,
`/feed`, consumer loops), the MCP transport, and manifest emit/parse. Module path:
`appkit`, consumed by every service via a committed `replace`. It is a library, not
a deployable service, and knows nothing about LLMs (that is `agentkit`).

## How changes are made

Changes go through the spec under `project/`, not direct edits — settle the
spec, then let the build loop realize it. The spec itself is direction-gated:
`project/**` is written only inside an operator-invoked move (the `$open-spec`
→ `$grill-me` → `$seal-spec` arc, or the build loop's completion mutations).
In any other session `project/` is read-only reference — a stale or wrong spec
is a finding to report, not a license to edit, and a settled discussion is not
direction: say what should change and wait. Edit code directly only on
explicit operator instruction. See the `$ikispec` skill for the `project/`
spec contracts and `$ralph` for the unattended build workflow.

## Layout

- Root `.`: the `appkit` package and verb dispatcher (`appkit.go`, `verbs.go`).
- `config`, `db`, `server`, `mcp`, `feed`, `manifest`, `inventory`, `web`,
  `logging`: the chassis subsystems.
- `internal/testmigrations`: fixtures for the chassis's own tests.
- `project/`: the spec the build loop works from.

## Tests

The default test gate is `go test ./...`. The appkit suite has these layers:

- **Hermetic:** package tests use injected environment maps, temporary filesystem
  trees and SQLite databases, `net/http/httptest`, loopback listeners, and local
  subprocesses.
- **Composed:** the boot smoke in `appkit_test.go` builds and runs a real minimal
  chassis-based service, then checks its health endpoint over loopback HTTP.
- **Manual:** `project/appkit-verification.md` is the operator-run live-box
  runbook.

appkit has no live test layer: tests do not contact external services or read
credentials, and this tree has no `//go:build live` test files.

Tests and vet run in workspace mode through the repository-root `go.work`. The
suite also runs `GOWORK=off go build ./...` as an isolated build check that
mirrors the deterministic production build and verifies the module resolves
standalone through its committed `replace` directives.

Beyond the Go toolchain itself, the composed boot smoke requires the `go` binary
on `PATH` at test time and a module cache that already resolves appkit's
`replace` siblings. It performs no network fetch. A missing precondition is a
hard test failure, never a skip.

## Versioning

Not versioned. appkit is a shared library consumed via a committed `replace`, with
no `VERSION` file and no git tag. Each service binary's `version`/`commit` are
stamped in at build time via `-ldflags`, not carried by appkit.
