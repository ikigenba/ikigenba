# Phase 21 — Lifecycle records and event-plane hop recording in `serve`

*Realizes design Decision 18 (lifecycle + hop wiring). Depends on Phase 19,
Phase 20, and Phase 23 (the consumer root record calls D20's `StartChain`).
It is deliberately last in the queue despite its number — `STATUS.md` order is
build order.*

**Cross-workspace dependency, stated in prose:** the publish/consume ids here
need the `eventplane/observe` package (`observe.Hook`, `observe.Event`,
`observe.Hop`) and the `Observe` field on `outbox.Options` and
`consumer.Config`, plus eventplane's first-class `correlation_id` envelope
field, to exist in the sibling `eventplane` module. eventplane's own plan builds
them; appkit only supplies the hook closure.

**End state.** `runServe` constructs the recorder from resolved config, starts
its flush loop on the serve context, emits a `lifecycle`/`start` record carrying
`detail.version` = `versionString()` before listening, emits `lifecycle`/`stop`
after `runServerAndWorkers` returns and **before** `recorder.Close`, and wires
the recorder into `server.Options`, `mcp.Options`, and — as a single
`observe.Hook` closure switching on `ev.Hop` — into `feed.Options` and each
`consumer.Config`. The hook records kind `publish`/`consume` with `ev.Key()` as
op, the hop's correlation id, and an outcome carrying the duration and an error
**class** (never a raw message). It only enqueues on the recorder, so it never
blocks and can never fail a publish or a delivery. Separately, each declared
consumer's handler is wrapped so a delivery whose `consumer.Event.CorrelationID`
is empty — the wire fact, an event that carried no chain — emits a `root` record
naming the routing key via `StartChain`, adopting the id eventplane already
minted onto the handler context.

**Done when:**
- These Verification ids are covered by clearly-named tests tagged with the id
  verbatim, driven against a **live in-process HTTP sink** for the records and a
  real `t.TempDir()` SQLite database with a real `feed.Start` SSE feed for the
  hops (the D10/D11 substrate):
  - R-1WSC-AQ6Q — a `serve` run delivers `lifecycle`/`start` with empty
    correlation id and `detail.version` equal to the binary's version string.
  - R-1Y08-OHXF — cancelling the serve context delivers `lifecycle`/`stop`
    before `serve` returns, with both start and stop present at the sink.
  - R-1Z85-29O4 — a real outbox append records `publish` with the routing key
    as op and the append context's correlation id.
  - R-20G1-G1ET — consuming that event over the real feed records `consume`
    with the same routing key and the publisher's correlation id.
  - R-21NX-TT5I — an envelope with no correlation id yields a `root` record
    sharing the `consume` record's non-empty valid id; two such events yield
    two different ids; a correlated envelope yields no `root` record.
- The suite is green per design's *Conventions* (`go build ./...`, `go vet
  ./...`, `gofmt -l .` empty, `go test ./...`, all from `appkit/`).
