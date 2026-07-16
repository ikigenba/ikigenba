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

Changes go through the spec under `project/`, not direct edits: settle the spec,
then let the build loop realize it. Edit code directly only on explicit operator
instruction. See the `$ikispec` skill for the `project/` spec contracts and
`$ralph` for the unattended build workflow.

## Layout

- `internal/ledger/`: the domain package (store, service, per-verb files, events).
- `internal/mcp/`: the seven-tool declaration, sole dispatcher and arg validation.
- `internal/db/`: embedded migrations plus load and outbox byte-equality guards.
- `internal/ids/`: ULID generation.
- `cmd/ledger/`: `main.go`, the `appkit.Main(appkit.Spec{…})` entrypoint.
- `share/www/`: the human web landing surface.
- `project/`: the spec (product/design/plan) the build loop works from.

## Tests

- Unit: `go test ./...`
- Isolated build check (mirrors the prod build): `GOWORK=off go build ./...`

## Versioning

The committed `ledger/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.10.1`). Advance it with `bin/bump ledger <major|minor|patch>`;
ship with `bin/ship ledger`. Git tags are not the version mechanism.
