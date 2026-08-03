# Phase 23 — Instrumented outbound client, and the poll cycle as a chain root

*Realizes design Decision 21 (instrumented outbound client) and 23 (poll cycle
is a chain root). Also re-points the drift-guard assertion behind D18's
`R-X9ED-THOL`, which D23's additive migration invalidates.*

**External dependency — build this phase after appkit and eventplane.** Both
Decisions consume seams that do not exist yet: from **appkit**,
`rt.HTTPClient(timeout)`, `rt.Recorder()`, `Recorder.StartRoot`, and
`appkit/httpclient` (`New`/`NewTransport`/`Options`) for the two
below-the-root fallbacks and the stub-transport tests; from **eventplane**,
`outbox.Append`'s new leading `context.Context` and the exported
`outbox.AddCorrelationIDSQL`. The suite's execution order builds appkit and
eventplane before any service adopts them. Until both land, this phase cannot
compile — that is the intended signal, not a defect.

## What gets built

One coherent unit — the outbound path and the poll cycle — across the
composition root and `internal/gmail`, plus one migration:

- **`cmd/gmail/main.go`** — in the `Handlers` hook, alongside the existing
  `GMAIL_*` reads: pass `rt.HTTPClient(100*time.Second)` to `gm.NewClient` in
  place of today's `nil`, and pass `rt.Recorder()` into `gm.NewEngine` through
  a new `EngineOptions.Recorder` field.
- **`internal/gmail/client.go`** — the nil-`httpClient` fallback in `NewClient`
  becomes `httpclient.New(httpclient.Options{Timeout: 100 * time.Second})`
  instead of a bare `&http.Client{…}`. The `httpClient *http.Client` parameter
  and its use by both the REST `Client` and the embedded `tokenSource` are
  unchanged, so every existing offline stub-`RoundTripper` test keeps working.
- **`internal/gmail/sync.go`** — `Engine.Poll` and `Engine.bootstrap` each start
  a root at their head with `e.rec.StartRoot(ctx, "gmail:poll-cycle", nil)`
  (one id per cycle; `StartRoot`, never `StartChain`) and thread the
  resulting context through the Gmail calls and the commit transaction;
  `Engine.commit` forwards it to each `AppendMailEvent`.
- **`internal/gmail/events.go`** — `EventSink.AppendMailEvent` and
  `outboxProducer.AppendMailEvent` take a leading `context.Context`, forwarded
  to `outbox.Append(ctx, tx, e)`. `buildPayload`, the kinds/subjects, and
  `Ring()` are untouched.
- **One new migration** created with `bin/create-migration gmail
  outbox_correlation_id`, its body exactly `outbox.AddCorrelationIDSQL`. Never
  hand-number it; never edit a committed migration.
- **`internal/db/migrations_outbox_test.go`** — the drift guard's file-level
  assertion ("the newest outbox migration contains `outbox.SchemaSQL`
  verbatim") is replaced by the column-set comparison D18's rewritten
  `R-X9ED-THOL` now specifies. The `003_outbox.sql` frozen-body check and the
  no-`type`-column check stay.

`cmd/consent` is deliberately **not** touched: a one-time operator CLI with no
runtime and no recorder, and operator tooling is outside the telemetry
contract.

Observable end state: gmail's behavior toward Google is unchanged (same
endpoints, same 100-second timeout, same retry-on-401), but every Gmail REST
call and OAuth token refresh leaves through the recorded transport, and one
poll cycle's Google calls, published events, and downstream consumer reactions
all share one correlation id.

## Done when

The suite is green — `cd gmail && go build ./...`, `go vet ./...`,
`gofmt -l .` (no output), and `go test ./...` all succeed with zero failures —
and every id below is covered by a clearly-named, genuinely-asserting test:

**D21 (outbound client)**
- `R-ZUC0-6K42` — a Gmail REST call driven through the client the composition
  root builds (against an `httptest` server, recorder draining to a live
  in-process sink) delivers an `outbound` record naming that method, host and
  path to the sink. A bare `&http.Client{…}` delivers none.
- `R-ZWRS-Y3LG` — an OAuth token refresh through that same client delivers its
  own `outbound` record, proving the `tokenSource` shares the instrumented
  client rather than holding its own.
- `R-ZXZP-BVC5` — a non-nil injected client whose transport is a test
  `RoundTripper` is used for both a REST request and a token refresh; the stub
  observes both and nothing reaches the network.
- `R-ZZ7L-PN2U` — the client the composition root builds has `Timeout` exactly
  `100 * time.Second`.

**D23 (chain root)**
- `R-PJWQ-547Q` — one `Engine.Poll` deriving three events hands all three
  `AppendMailEvent` calls a context carrying the **same** `correlation.Valid`
  id.
- `R-PL4M-IVYF` — two successive `Engine.Poll` cycles yield **different** ids.
- `R-PMCI-WNP4` — the Gmail API requests issued during a cycle carry that same
  id on their request context, observed by a stub `RoundTripper` reading
  `correlation.FromContext(req.Context())`.

**D18's re-pointed guard**
- `R-X9ED-THOL` — its existing test, updated: the migrated `outbox` table's
  column set equals the set `outbox.SchemaSQL` declares (so `correlation_id`
  is present after the new migration), `type` is absent, and `003_outbox.sql`
  is byte-identical to its frozen body.

**Additional deterministic checks**
- `grep -rn '\.Append(tx' --include='*.go' --exclude-dir=project gmail/`
  returns **no matches** (exit status 1).
- `grep -rn 'http\.DefaultClient' --include='*.go' --exclude-dir=project
  gmail/cmd/gmail gmail/internal` returns **no matches** (exit status 1) —
  `cmd/consent` is excluded by path, per D21's scope.
- `ls gmail/internal/db/migrations/ | grep -c outbox_correlation_id` prints
  **1**, and that file's body contains `ALTER TABLE outbox ADD COLUMN
  correlation_id`.
