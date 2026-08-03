# cron — Research

Collected ground truth that cron's design references instead of re-deriving.
Everything here is **external to `cron/`** — owned by the suite, by `appkit`,
or by `eventplane` — and cron consumes it as a fixed contract it cannot
change. This document is non-contractual and rewritten in place; it is the
current statement of the facts cron's Decisions lean on, not a log.

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

## 2. The durable-root rule — and why cron is the exception it names

`docs/correlation-ids.md` says the id is **minted once, at the outermost cause
of a causal chain** — "the user's MCP call, web request, **or a trigger firing
on the user's behalf**" — and adds the **durable-root reuse** rule: when a
chain is rooted at a durable entity that already carries a suite ULID (an
ingest job, a prompts run), that entity's own id *is* the correlation id and no
second one is minted; mint fresh **only when no durable root exists**.

A cron tick has no durable entity. A crontab row is not the root — it is the
*schedule*, reused every matching minute, so its identity cannot distinguish
Tuesday's firing from Wednesday's. The `(schedule, slot)` pair identifies a
firing but is not a ULID and is not opaque. So a tick falls squarely in the
"mint fresh" case, and cron is the outermost cause: nothing upstream of the
minute boundary exists to inherit an id from.

The suite telemetry capability states the same rule from the other direction:
**self-originated work mints its own root id and records a `root` record** —
cron tick, prompts run spawn, scripts run spawn, and a consumer processing an
event that carries no correlation id.

## 3. `eventplane/correlation` — the shared leaf package

A leaf package below both `appkit` and `eventplane` (eventplane must never
import appkit, and both libraries must read the id off the **same** context
key). cron consumes it; cron designs none of it.

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
reachable only through these accessors. `New` is the only sanctioned minter for
a root id — nothing copies the ULID construction or reuses `logging.NewULID`
for it. In practice cron reaches it indirectly, through the chassis's
`StartRoot` helper (§5).

## 4. `eventplane/outbox` — what changes for a producer

- **`Append` takes a leading context:**

  ```go
  func (o *Outbox) Append(ctx context.Context, tx *sql.Tx, ev Event) error
  ```

  It reads `correlation.FromContext(ctx)` and stores the result on the row, and
  threads the ctx into the insert (`tx.ExecContext`). `outbox.Event` gains
  **no** correlation field — the context is the propagation channel. An absent
  id stores `""`; **`Append` never mints**, because minting belongs at the
  outermost cause of a chain, which the library cannot see. This is a
  **compile-caught** change at cron's call site in `internal/tick`.
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
  `sqlite_sequence` high-water mark untouched — so no consumer's cursor moves
  and the `AUTOINCREMENT` guarantee is undisturbed. **eventplane applies
  nothing itself: each service owns its migration runner** and applies this as
  one new timestamped migration.
- The SSE envelope gains `correlation_id` (always present; `""` when the event
  carried no chain).
- The `publish` telemetry hop is recorded by an injectable observation hook
  that `appkit` supplies; **a producer wires nothing** for it.
- `outbox.Event{Kind, Subject, Payload}` and the `outbox.Family`/`Registry`
  family model are **unchanged** by this work (cron D14 already conforms).

Consequence for cron, stated plainly: a `context.Background()` at the `Append`
call site compiles and appends a row with an empty `correlation_id`. Every
scheduled chain in the suite would then be rootless and unqueryable, and
nothing downstream would notice. That is the failure cron's own Decision has to
make impossible.

## 5. `appkit` — the telemetry recorder and what arrives for free

cron writes no inbound instrumentation. Rebuilding against the revised chassis
brings correlation middleware on the loopback server (read-or-mint,
`correlation.With` on every handler ctx, echoed on the response), `request`
records for MCP tool calls and plain HTTP, `lifecycle` `start`/`stop` records,
and `publish` records at outbox append.

What cron *does* write is one **`root`** record per tick — and the chassis owns
that idiom too, so cron consumes it rather than hand-rolling mint + install +
record:

```go
package telemetry // appkit/telemetry

// StartRoot begins a NEW chain: it mints a fresh correlation id (IGNORING any
// id ctx already carries), installs it on the returned context, and records a
// `root` record with the caller's op. Use it at a true origin — a timer tick,
// a poll/watch/sync cycle — where inheriting an ambient chain would wrongly
// attribute the work to whatever ran before it. One call per CYCLE, not per
// item. detail may be nil.
func (r *Recorder) StartRoot(ctx context.Context, op string, detail map[string]any) (context.Context, string)

// StartChain adopts ctx's id when it has one and mints only when it does not,
// recording a `root` record either way. For work that has a name worth
// recording as an origin but MAY have been caused by a request.
func (r *Recorder) StartChain(ctx context.Context, op string, detail map[string]any) (context.Context, string)
```

The chassis states the choice rule once, so nine services do not each decide
it: **`StartRoot` when the work has no cause outside itself; `StartChain` when
it might.** The `op` is the caller's vocabulary — appkit's own example for cron
is `cron:tick/<name>`, which is exactly the canonical routing key of the event
the tick publishes.

Both helpers, and every other `Recorder` method, are **nil-safe on a nil
`*Recorder`**: they still mint and install the id, they simply record nothing.
So cron's correlation behavior does not depend on whether telemetry is enabled,
and cron needs no nil checks and no error handling.

The recorder reaches service code through one accessor on the runtime a
service's `Spec.Handlers` hook already receives:

```go
// Recorder returns the telemetry recorder appkit wired at serve, so a service
// hands it to the domain objects it builds (an outbound client, a background
// worker). It is nil when telemetry is disabled.
func (rt *Router) Recorder() *telemetry.Recorder
```

Underneath, the recorder is best-effort by contract: a bounded in-memory ring
that drops oldest, batched fire-and-forget POSTs to
`http://127.0.0.1:3008/ingest`. **Telemetry being unreachable never blocks or
fails a tick.**

## 6. Telemetry configuration — why cron's deploy artifacts do not change

The recorder is configured from **unprefixed, suite-wide** environment
variables resolved by `appkit/config` with working defaults:

- `TELEMETRY_INGEST_URL` — defaults to `registry.BaseURL("telemetry") +
  "/ingest"`, i.e. `http://127.0.0.1:3008/ingest`.
- `TELEMETRY_ENABLED` — defaults to **true**; `0`/`false`/`no` disables
  recording entirely.

cron's `etc/manifest.env` carries only `APP`/`MOUNT`/`DEFAULT`/`PORT`/`MCP`/
`FEED` and the two `OUTBOX_RETENTION_*` values. Both telemetry defaults are
correct for cron, so the manifest needs **no** new key — adding one would give
a single suite-wide address fifteen homes.

## 7. cron's own emit path (ground truth for the mint seam)

The tick worker (`internal/tick`) is a `Spec.Workers` background loop, not an
inbound HTTP handler, so **no chassis middleware ever runs for it** and no
context reaches it carrying a correlation id. Its shape today:

- `Run(ctx)` sleeps to the next wall-clock minute boundary, computes
  `slot = Slot(time.Now())` and calls `Fire(ctx, slot, time.Now())`.
- `Fire` lists the crontab, and for each row whose expr matches the slot and
  whose `last_slot` is not already the slot, calls `fireOne`.
- `fireOne(ctx, name, slot, firedAt, slotStr)` builds the event with
  `event.Build`, opens **one per-schedule transaction**, `UPDATE`s
  `last_slot`, `Append`s the event, and commits — so "emitted" and "recorded
  as emitted" commit atomically. A 0-row `UPDATE` means another writer already
  took the slot and the tx rolls back **without** appending.
- `Fire` calls `w.ob.Ring()` once after the scan if anything fired.

Two properties of this shape decide where a mint can legally go: `fireOne` is
the **only** place a tick event is appended, and it is called **once per
(schedule, slot) that actually fires** — a skipped row, an unparseable expr,
and a lost race all return before it or inside it without appending.

The crontab `name` column carries a DB CHECK
(`name <> '' AND name NOT GLOB '*[^a-z0-9-]*'`), so every emitted name is
non-empty `[a-z0-9-]+` and `cron:tick/<name>` is always a well-formed
canonical key.
