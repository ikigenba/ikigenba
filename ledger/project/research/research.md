# ledger — Research

Collected ground truth that ledger's design references instead of re-deriving.
Everything here is **external to `ledger/`** — it is owned by the suite, by
`appkit`, or by `eventplane`, and ledger consumes it as a fixed contract it
cannot change. This document is non-contractual and rewritten in place; it is
the current statement of the facts ledger's Decisions lean on, not a log.

## 1. The suite correlation standard

From `docs/correlation-ids.md` and the suite telemetry capability:

- A **correlation id** is a bare **26-character Crockford base32 ULID**
  (alphabet `0123456789ABCDEFGHJKMNPQRSTVWXYZ` — no `I`, `L`, `O`, `U`):
  48 bits of millisecond timestamp then 80 bits of cryptographic randomness.
  It is opaque; nothing parses it.
- **One id per causal chain, propagated verbatim on every hop.** Re-minting
  mid-chain severs the trail and is always wrong.
- Between processes the id travels in the header **`X-Correlation-Id`**.
- On the event plane it travels as the **envelope field `correlation_id`**,
  populated by the library from the caller's context. The older
  payload-field convention in `docs/correlation-ids.md` is **superseded** — a
  producer never packs the id into its own payload.
- **The edge strips and mints.** The dashboard's introspection endpoints mint
  the id for gated routes and return it on the auth-subrequest response as
  `X-Correlation-Id`; each service's nginx fragment captures it with
  `auth_request_set` and re-sets it with `proxy_set_header`, which **replaces**
  anything the client sent. Ungated public locations set the header to the
  empty string so the service mints for itself. A public caller can therefore
  never inject an id.

## 2. `eventplane/correlation` — the shared leaf package

A leaf package below both `appkit` and `eventplane` (eventplane must never
import appkit, and both libraries must read the id off the **same** context
key). ledger consumes it; ledger designs none of it.

A **stdlib-only leaf** — it imports nothing from `outbox`/`consumer`/`routing`
and adds no `require`, so anything downstream can import it without dragging
the event plane along. It is the single suite-wide home of these primitives; a
second context key anywhere would silently split the chain.

```go
package correlation // eventplane/correlation

const Header = "X-Correlation-Id"   // the wire header
func New() string                   // mints a 26-char Crockford ULID
func Valid(s string) bool           // exactly 26 Crockford chars
func WithContext(ctx context.Context, id string) context.Context // ignores an invalid id
func FromContext(ctx context.Context) string                     // "" when absent
func Ensure(ctx context.Context) (context.Context, string)       // read-or-mint
```

The context key is an unexported zero-size struct type, so the value is
reachable only through these accessors.

## 3. `eventplane/outbox` — what changes for a producer

- **`Append` takes a leading context:**

  ```go
  func (o *Outbox) Append(ctx context.Context, tx *sql.Tx, ev Event) error
  ```

  It reads `correlation.FromContext(ctx)` and stores the result on the row, and
  threads the ctx into the insert (`tx.ExecContext`). `outbox.Event` gains
  **no** correlation field — the context is the propagation channel. An absent
  id stores `""`; **`Append` never mints**, because minting belongs at the
  outermost cause of a chain, which the library cannot see. This is a
  **compile-caught** change at ledger's call site.
- **`SchemaSQL` gains one column, declared last** so a fresh table and an
  upgraded table are column-for-column identical:
  `correlation_id TEXT NOT NULL DEFAULT ''`.
- **The library owns the upgrade text**, so fourteen services cannot spell it
  fourteen ways:

  ```go
  const AddCorrelationIDSQL = `ALTER TABLE outbox ADD COLUMN correlation_id TEXT NOT NULL DEFAULT '';`
  ```

  It is additive by construction: `ADD COLUMN` rewrites no rows, back-fills
  existing rows with `''`, and leaves `seq` values and SQLite's
  `sqlite_sequence` high-water mark untouched — so no consumer's cursor moves.
  **eventplane applies nothing itself: each service owns its migration
  runner** and applies this as one new timestamped migration.
- The SSE envelope gains `correlation_id` (always present; `""` when the event
  carried no chain).
- The `publish` telemetry hop is recorded by an injectable observation hook
  that `appkit` supplies; **a producer wires nothing** for it.
- `outbox.Event{Kind, Subject, Payload}` and the `outbox.Family`/`Registry`
  family model are **unchanged** by this work (ledger D15 already conforms).

Consequence for ledger, stated plainly: the id reaches the outbox row **only**
if the context that carried it reaches the `Append` call. A call site that
passes `context.Background()` compiles, appends a row, and silently severs
every chain ledger participates in. That is the failure this work has to make
impossible, and it is what ledger's own Decision is about.

## 4. `appkit` — what arrives for free on a rebuild

ledger writes no instrumentation code. Rebuilding against the revised chassis
brings:

- **Correlation middleware** on the loopback server: read `X-Correlation-Id`
  and use it verbatim when `correlation.Valid` accepts it, otherwise mint;
  stash it with `correlation.With` so every handler's `ctx` carries it; echo
  it on the response.
- **`request` records** for every MCP tool call (`op` = `mcp:<tool>`, actor
  from the forwarded identity headers, params under the capture policy,
  outcome status/duration/size+digest) and for plain HTTP requests
  (`op` = `<METHOD> <path>`).
- **`lifecycle` records** — `start` (with version) at boot, `stop` on graceful
  shutdown.
- **`publish` records** at outbox append.
- The recorder is **best-effort by contract**: a bounded in-memory ring that
  drops oldest, batched fire-and-forget POSTs to
  `http://127.0.0.1:3008/ingest`, and `Record` is nil-safe and never blocks or
  errors. Telemetry being unreachable never blocks or fails a ledger write.

## 5. Telemetry configuration — why ledger's deploy artifacts do not change

The recorder is configured from **unprefixed, suite-wide** environment
variables resolved by `appkit/config` with working defaults:

- `TELEMETRY_INGEST_URL` — defaults to `registry.BaseURL("telemetry") +
  "/ingest"`, i.e. `http://127.0.0.1:3008/ingest`.
- `TELEMETRY_ENABLED` — defaults to **true**; `0`/`false`/`no` disables
  recording entirely.

ledger's `etc/manifest.env` carries only `APP`/`MOUNT`/`DEFAULT`/`PORT`/`MCP`/
`FEED` and the two `OUTBOX_RETENTION_*` values. Both telemetry defaults are
correct for ledger, so the manifest needs **no** new key — adding one would
give a single suite-wide address fifteen homes.

## 6. ledger's live-data constraint

ledger is the one suite service holding **live customer data**. The suite's
migration warning protects its `state/` absolutely. The telemetry work touches
ledger's nginx fragment and one in-process call seam; the **only** schema
movement is `outbox.AddCorrelationIDSQL`, an `ALTER TABLE … ADD COLUMN` against
the `outbox` table alone, applied by ledger's ordinary forward-only migration
runner. It rewrites no rows and drops nothing. No ledger domain table, no
journal row, and no posting is in its path.
