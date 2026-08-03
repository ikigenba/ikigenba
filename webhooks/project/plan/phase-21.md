# Phase 21 — The delivery's correlation id rides the published event

*Realizes design Decision 19 (one inbound delivery, one chain). Depends on
Phase 20 for the fragment that guarantees the id is service-minted rather than
caller-supplied.*

> **External precondition (operator-sequenced, not built here).** The revised
> **appkit** (correlation package + read-or-mint request middleware + context
> accessor + recorder) and the revised **eventplane** (`correlation_id` outbox
> column and wire envelope field, populated at `Append` from context) must be
> **built first**. Until both land this phase cannot compile: `Append` gains a
> context parameter, a compile-caught change at webhooks' single emission site.

What gets built, in `internal/webhooks` (the emission seam) and `internal/e2e`
(the proof):

- `Service.Record` passes its existing `ctx` into the outbox append, inside the
  transaction it already opens — so eventplane writes the request's correlation
  id into the `correlation_id` column together with the appended row and the
  `last_triggered_at` touch. No parameter is added to `Record`, no field to
  `webhookReceivedPayload`, and the D4 ingress handler is untouched.
- `go.mod`/build wiring is refreshed against the revised in-repo `appkit` and
  `eventplane` siblings; the `correlation_id` column itself arrives with
  eventplane's additive outbox migration re-applied through webhooks' own
  runner, so the existing DDL drift guard on the newest outbox migration keeps
  asserting the migration matches `outbox.SchemaSQL` verbatim.
- webhooks adds **no** recorder calls of its own: the inbound `POST /in/<name>`
  is recorded as kind `request` by the chassis middleware and the append is
  recorded as a `publish` hop through eventplane's observation hook, both
  chassis-owned and deliberately not re-proven here.

**Done when:** the three ids below are each covered by a clearly-named test in
`internal/e2e` (real temp-file SQLite built by webhooks' full embedded migration
set, the real domain service over the real `eventplane/outbox`, the real ingress
handler, and the real eventplane `FeedHandler` over `httptest`), and the suite
is green per design's *Conventions* (`go build ./...`, `go vet ./...`,
`go test ./...` all clean):

- **R-L1A1-XMRN** — an accepted delivery arriving with **no** `X-Correlation-Id`
  produces exactly one outbox row whose `correlation_id`, read back by SQL, is a
  non-empty 26-character Crockford-base32 ULID equal to the id the chassis put
  on the request context; a second delivery to the same hook yields a different
  id.
- **R-L2HY-BEIC** — a delivery arriving **with** a known valid inbound
  `X-Correlation-Id` (the trusted loopback-peer case) produces an outbox row
  carrying that exact id verbatim — propagated, not re-minted.
- **R-L3PU-P691** — the event appended by a real accepted delivery, served by
  the real `FeedHandler` over an `httptest` SSE connection, frames
  `event: webhooks:received/<hook name>` with a `data:` envelope whose
  `correlation_id` equals the delivering request's id, still carrying `kind` and
  `subject` and no `type` key.
