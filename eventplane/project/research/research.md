# eventplane — Research

Non-contractual evidence base. Three kinds of ground truth: the governing
suite-level contracts, the as-built facts of this library verified directly in
the code on 2026-08-03, and options deliberately evaluated and not chosen.

## The governing external contracts

**`project/design/D18.md` at the repo root** (suite-level) is the addressing
model this library realizes: the envelope, the canonical key
`source + ":" + kind + subject`, the doublestar glob dialect,
families-not-enumerations reflection, filter-vs-family validation, and the
hard-cutover migration stance. Design uses its values verbatim and does not
re-derive them. Its producer-key examples are direction, not a binding rename
list — each service picks its kinds/subjects in its own spec.

**`project/design/D14.md` at the repo root** (suite-level) is the correlation
standard this library realizes on the event plane. The facts design consumes:

- A correlation id is a **bare suite ULID**: 26 characters, **Crockford
  base32**, encoding 48 bits of millisecond timestamp followed by 80 bits of
  cryptographic randomness (e.g. `AGPXX34WA3IGS4MQVE5LMRXK7U`). No prefix, no
  separators, no internal structure any consumer may parse.
- **Minted once, at the outermost cause** of a causal chain, and **propagated
  verbatim** thereafter. Re-minting mid-chain severs the trail and is always
  wrong.
- **Durable-root reuse**: when a chain is rooted at a durable entity that
  already has a suite ULID, that id *is* the correlation id. Mint fresh only
  when no root exists. Two situations qualify, and only one of them is this
  library's: a service that originates its own work mints a root before it
  publishes (a poll or sync cycle mints one per cycle, a timer tick, a spawned
  run) — the service's own spec owns that; and a consumer receiving an event
  that carries no id at all mints one so the reaction is still chained, which
  is this library's (D8).

Two facts about that document's current text matter to this design:

- Its adoption notes describe carrying the id in an **event payload field**.
  That convention is **superseded** by the first-class envelope field this
  library adds. Amending the document is the **root workspace's** work, not
  this library's — this spec conforms to the superseding contract and does not
  edit the doc.
- The Crockford alphabet is the standard's, and it is the one this library
  implements. (The appkit chassis carried an RFC 4648 base32 minter, corrected
  to Crockford in appkit's own spec; the two now agree by both calling this
  library's minter.)

The header name carrying the id between processes suite-wide is
**`X-Correlation-Id`**, and the envelope field name on the plane is
**`correlation_id`** — both settled at suite level.

**Who consumes the observation hook.** The appkit chassis' telemetry recorder
is the intended (and, today, only) consumer of the publish/consume observation
seam: it turns each observation into a telemetry record whose operation name is
the canonical routing key. This matters to the seam's shape for one structural
reason: **appkit depends on eventplane** — `appkit/go.mod` carries
`require eventplane v0.0.0` with `replace eventplane => ../eventplane` — so
eventplane importing appkit would be an import cycle. The seam must therefore
be a plain callback owned by this library, with the recorder wired in from the
composition root. The same direction is why the correlation-id primitives (the
header constant, the minter, the context accessor) live here rather than in the
chassis: everything that needs them sits downstream of this library.

Note: the normative wire contract is `project/design/D18.md` at the repo root.
It replaced a long-cited event-protocol document under `docs/` that never
existed in the tree; this library's older code comments still carry that name's
`§` section references, which resolve to nothing and are a known residue.

## As-built facts (verified in code, 2026-08-03)

The routing revision is built; these are the current shapes a new decision
extends, not the pre-revision ones.

- **Envelope** (`outbox/feed.go`, `envelope` struct): JSON
  `{id, source, time, kind, subject, payload}`, marshaled compact onto the
  single `data:` line.
- **SSE event frame** (`outbox/feed.go`, `eventFrame`): the `id:` line carries
  the opaque cursor `<generation>.<seq>`; the `event:` line carries
  `routing.Key(source, kind, subject)`; the `data:` line is the envelope.
  Reserved control frame names: `resync`, `caught-up`, `status` (plus
  `: keepalive` comment frames) — none contains a `:`, while every canonical
  key does.
- **`outbox.SchemaSQL`** (`outbox/schema.go`): `outbox(seq INTEGER PRIMARY KEY
  AUTOINCREMENT, event_id TEXT NOT NULL, kind TEXT NOT NULL, subject TEXT NOT
  NULL DEFAULT '', payload TEXT NOT NULL, created_at TEXT NOT NULL)` plus
  `idx_outbox_created_at`. The `AUTOINCREMENT` is load-bearing (a persistent
  high-water mark, so retention emptying the table cannot restart rowids and
  strand a cursored consumer); SQLite backs it with a `sqlite_sequence` row.
- **`outbox.Append(tx *sql.Tx, ev Event) error`** (`outbox/outbox.go`): no
  `context` parameter today; validates kind then subject via `routing`, then
  the registry gate; mints the event id (`newULID()`) and `created_at` once at
  append; `tx.Exec` inserts `(event_id, kind, subject, payload, created_at)`.
  `fetch` selects `seq, event_id, kind, subject, payload, created_at` into
  `eventRow`. Callers invoke `Ring()` after their own `Commit` — the library
  never sees the commit.
- **In-suite `Append` call sites** (non-test, 2026-08-03): eight —
  `crm/internal/crm/service.go`, `cron/internal/tick/tick.go`,
  `dropbox/internal/dropbox/events.go`, `gmail/internal/gmail/events.go`,
  `ledger/internal/ledger/events.go`, `prompts/internal/prompt/store.go`,
  `repos/internal/repos/events.go`, `scripts/internal/script/store.go`,
  `webhooks/internal/webhooks/events.go`. Every one already sits in a
  request- or job-scoped function, so threading a `context.Context` is
  mechanical and compile-caught. (Each service adopts it in its own spec.)
- **`newULID`** (`outbox/cursor.go`): 48-bit millisecond time + 80 bits of
  `crypto/rand`, encoded with `base32.StdEncoding.WithPadding(NoPadding)` —
  i.e. **RFC 4648**, not Crockford, despite the comment. It mints event ids and
  generation tokens, which are *not* correlation ids and are not governed by
  `project/design/D14.md` at the repo root.
- **`consumer.Event`** (`consumer/consumer.go`): `{ID, Source, Time, Kind,
  Subject, Payload}` with `Key()` delegating to `routing.Key`. `Handler` is
  `func(ctx context.Context, ev Event) error` — the engine already passes a
  context, which is `resp.Body`'s request context, i.e. the engine's run
  context.
- **Consumer dispatch** (`handleFrame`): a literal switch on the `event:` line
  for the three control names; otherwise an event frame — a frame with no `id:`
  is ignored without advancing; `parseEvent` decodes the envelope and rejects
  an **empty `kind`**; an unparseable envelope logs, runs no handler, and still
  commits the cursor; the handler's return gates the cursor (nil advances,
  `ErrSkip` logs loud and advances, anything else stalls the connection and
  replays from the committed cursor).
- **`consumer.Config`** has no `Filter` field and no observation field:
  the engine invokes the handler for every event and filtering is the
  service's job. `consumer.Subscription` is `{Source, Filter, Description,
  Handler}` with `Filter` a canonical-key glob and **no `Match` method**.
- **`consumer.SchemaSQL`** (`consumer/schema.go`): `feed_offset(source PRIMARY
  KEY, cursor, subscribed, updated_at)`. It carries **no event-shape column**
  — the cursor is opaque TEXT — so an envelope change requires no change to it.
  `store.commit` is a single upsert of the cursor.
- **`routing`** (`routing/routing.go`): `Key`, `Match`, `ValidKind`,
  `ValidSubject`, `CouldMatchSubject`, and the hand-rolled glob compiler.
  Imports `fmt` only — a stdlib-only leaf package, and the precedent for
  adding another.
- **Test style to model** (`consumer/consumer_test.go`, `outbox/feed_test.go`):
  the highest-value tests wire the **real** `outbox.FeedHandler()` into an
  `httptest.Server` and run `consumer.Run` against it over a real SQLite
  database — that substrate is what a correlation round-trip claim must also
  exercise.
- **Toolchain** (`eventplane/go.mod`, `eventplane/Makefile`): module
  `eventplane`, Go 1.26, sole direct dependency `modernc.org/sqlite`. Makefile
  targets: `test` (`go test ./...`), `vet`, `fmt`. Local dev runs in workspace
  mode via the repo-root `go.work`.

## SQLite facts the additive column depends on

- `ALTER TABLE … ADD COLUMN <name> TEXT NOT NULL DEFAULT ''` is accepted:
  SQLite permits a `NOT NULL` added column when its default is a constant,
  and back-fills existing rows with that default.
- `ADD COLUMN` rewrites no rows and touches neither `rowid`/`seq` values nor
  the `sqlite_sequence` high-water mark, so an in-place upgrade cannot disturb
  a consumer's cursor.
- Column *order* from `ADD COLUMN` is append-at-the-end, so a fresh
  `CREATE TABLE` must declare the new column last if the two are to be
  byte-comparable by `PRAGMA table_info`.

## Options evaluated and not chosen

- **Matcher** — `path.Match` (stdlib) cannot express `**`. `bmatcuk/doublestar`
  implements the dialect but is a third-party dependency for ~one screen of
  matching logic; the operator decision is a **hand-rolled matcher** in this
  library, with the dialect pinned by an exhaustive table-driven test instead
  of by an upstream's semantics.
- **Where the correlation context key lives** — three candidates. In **appkit**
  (natural home for request scope, but the import direction forbids it: this
  library would have to import the chassis). **Duplicated** in each of appkit
  and eventplane (compiles, and is the classic silent failure: two distinct
  context keys means a handler's id is invisible to the producer it calls, with
  no error anywhere). In a **stdlib-only leaf package here**, which everything
  downstream can import — the chosen option, and the shape `routing` already
  established.
- **A third-party ULID library** (`oklog/ulid`) for the Crockford minter —
  rejected on the same ground as the matcher: the construction is a dozen lines
  and this module's dependency footprint is deliberately one entry.
