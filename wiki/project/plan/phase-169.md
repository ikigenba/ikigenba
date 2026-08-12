# Phase 169 — Pin the consumer partition on the per-item queue verbs

*Realizes design Decision 5 (`Get`/`Ack` consumer scoping).*

`internal/llm/llm.go` already sends `?consumer=` on `Get` and `Ack` and routes
`Do`'s poll and ack through `ConsumerAsk`. Nothing asserts it. The existing
`R-JXCC-GXYJ` test matches on `r.URL.Path` alone, so dropping the query string
would leave the gate green while every live `Do` poll took a `404` from the real
prompts service, which scopes both verbs to the owning consumer (root prompts
`project/design/D29.md`).

**End state.** The behavior is unchanged; it becomes proven. A test captures the
raw query on the per-item verbs at an httptest prompts and pins each to its
partition.

**Done when:**
- `go test ./...` from `wiki/` is green.
- `R-9S84-C6J2` is covered: a handoff-path `Get` and `Ack` send
  `consumer=service:wiki`; the `Get` and `Ack` that `Do` issues while polling send
  `consumer=service:wiki.ask`. A request with no `consumer` query parameter, or one
  naming the other partition, fails the test.
- The `$ikispec` coverage check emits no output.
