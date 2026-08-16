# ledger

A deployable path-routed service of the ikigenba suite, double-entry bookkeeping
for personal and small-business use, routed at `/srv/ledger/` (module `ledger`).
It is an immutable journal of balanced transactions modeled on ledger-cli: every
report is a query over postings, the chart of accounts is emergent and typed, and
money is integer cents. The domain surface is a fixed set of seven verbs (record,
reverse, reconcile, balance, register, get, describe) over one write entity, the
transaction, exposed as MCP; the chassis adds standard `health`/`reflection`
tools. It is an event-plane producer, emitting `transaction.recorded` to an outbox
at `GET /feed`. Loopback-only over SQLite; nginx is the sole trust boundary, so
the service runs no token logic.

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

- `internal/ledger/`: the domain package (store, service, per-verb files, events).
- `internal/mcp/`: the seven-tool declaration, sole dispatcher and arg validation.
- `internal/db/`: embedded migrations plus load and outbox byte-equality guards.
- `internal/ids/`: ULID generation.
- `cmd/ledger/`: `main.go`, the `appkit.Main(appkit.Spec{…})` entrypoint.
- `share/www/`: the human web landing surface.
- `project/`: the spec (product/design/plan) the build loop works from.

## Tests

The default-gate test command is `cd ledger && go test ./...`. It runs inside
the green bar of `go build ./...`, `go vet ./...`, a silent `gofmt -l .`, and
`go test ./...`, followed by the isolated build check and the semantic lint
gate. Run the latter from the ledger root as `llm-lint "$PWD"`; the absolute
path keeps the lint scoped to this tree.

The suite has two layers: hermetic and composed. There is no live layer and no
manual layer.

- Hermetic tests cover the `internal/ledger` domain and events against a real,
  fully migrated SQLite database; the `internal/db` migration and outbox guards;
  the `internal/mcp` HTTP seam; the `cmd/ledger` web and real eventplane feed
  handlers over loopback `httptest`; and shipped-file guards for
  `etc/nginx.conf`, `etc/manifest.env`, and the loopback source scan. These tests
  are in-process and loopback-only.
- Composed testing is the boot smoke in `cmd/ledger/main_test.go`. It builds the
  ledger binary, creates an `/opt/ledger/`-shaped tree under `t.TempDir()`, runs
  the binary on a free loopback port, and checks `/health`; it is offline by
  construction.
- There is no live layer because ledger has no external-service dependency. The
  nginx trust boundary is not run by the gate and is checked through committed
  file assertions.
- There is no manual layer or ledger-specific runbook; whole-stack composition
  belongs to the umbrella suite.

Environmental preconditions beyond the Go toolchain are the `llm-lint` binary
on `PATH` and the provider API key required by its configured default model.
Their absence fails the semantic lint gate. The composed smoke uses only
`go build` and the binary it builds; tests require no `git`, `python3`, browser,
credential, external service, or running suite.

The gate uses workspace GOWORK mode, resolving `appkit`, `eventplane`, and
`registry` through the repository `go.work` and committed sibling replacements.
The separate isolated build check is `cd ledger && GOWORK=off go build ./...`.
It mirrors the production build, compiles without running tests, and catches
dependencies that resolve only through the workspace.

## Versioning

The committed `ledger/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.10.1`). Advance it with `bin/bump ledger <major|minor|patch>`;
ship with `bin/ship ledger`. Git tags are not the version mechanism.
