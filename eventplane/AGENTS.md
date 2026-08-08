# eventplane

The suite's shared **event-plane** library: the producer and consumer plumbing
behind the internal SSE event plane, wired into consumers via a committed
`replace eventplane => ../eventplane`. It is a **library, not a service** (no
port, no nginx fragment, no `bin/run`, not deployable). The producer half
(`package outbox`) provides the atomic outbox on the caller's `*sql.Tx`, the
canonical schema DDL, the `GET /feed` SSE handler, the generation/epoch sidecar
token, and background retention. The consumer half (`package consumer`) provides
the reconnect/backoff engine that streams a producer's feed past a durable
per-upstream cursor and gates cursor advance on handler return. The normative
wire contract is `project/design/D18.md` at the repo root; on any conflict that doc wins.

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

- `outbox/`: producer half: outbox append, `SchemaSQL` DDL, `FeedHandler()`, epoch token, retention.
- `consumer/`: consumer half: feed engine, reconnect/backoff, `feed_offset` cursor, `SchemaSQL` DDL.
- `routing/`: event routing helpers (`routing.go`).
- `project/`: the spec (product/design/plan) the build loop works from.

## Tests

The default gate is `go test ./...`, run in workspace mode through the
repository-root `go.work`; do not set `GOWORK=off`.

The suite uses the testing-layer vocabulary defined by the repository-root
`project/design/D23.md`. All eventplane tests are hermetic: they use pure
table tests, temp-directory SQLite databases with the real schema, loopback
`httptest` servers with real HTTP clients, and a local `go list -deps`
subprocess. Eventplane has no composed layer, no live layer, and no manual
layer.

Beyond the Go toolchain, the `observe` import-discipline test requires the `go`
binary on `PATH` at test time and a module cache that already resolves this
module's dependencies. Missing either prerequisite is a hard test failure, not
a skip; the test must not fetch dependencies from the network.

## Versioning

Not versioned. eventplane is a shared library consumed within the mono-repo,
with no `VERSION` file and no git tag.
