# Phase 18 — Thread the request context to `outbox.Append` and add the `correlation_id` column

*Realizes design Decision 17 (correlation adoption), code slice — its ids
R-Y3Z7-H9BG, R-VOH6-HG8R, R-VPP2-V7ZG. Does **not** depend on Phase 17 (that
phase touches only `etc/nginx.conf`).*

> **Cross-workspace dependency — operator-sequenced, not built here.** This
> phase cannot compile until **`appkit` and `eventplane` have been built**
> against the suite telemetry capability: it consumes
> `eventplane/correlation` (`WithContext`/`FromContext`/`New`/`Valid`), the
> revised `outbox.Append(ctx, tx, ev)`, `outbox.SchemaSQL`'s new
> `correlation_id` column, and `outbox.AddCorrelationIDSQL`. Those surfaces
> are owned by the `eventplane` workspace (its correlation and producer-path
> Decisions) and the `appkit` workspace, and are consumed here as fixed
> contracts — `project/research/research.md` records their exact footprint.
> The suite build order is: root + registry, then appkit and eventplane, then
> the telemetry service, then dashboard, then the remaining services including
> ledger.

ledger becomes a **pure propagator** of the chain id: it mints nothing,
records nothing, and adds no instrumentation. Two things get built.

**1. The migration.** One new timestamped migration under
`internal/db/migrations/`, minted with `bin/create-migration ledger
outbox_correlation` (never a hand-picked number), whose body is
`outbox.AddCorrelationIDSQL` verbatim —
`ALTER TABLE outbox ADD COLUMN correlation_id TEXT NOT NULL DEFAULT '';`. The
frozen `003_outbox.sql` and `20260712184833_outbox_routing.sql` are **not**
touched, and the existing DDL drift guard in
`internal/db/migrations_outbox_test.go` stays pointed at
`20260712184833_outbox_routing.sql` (the newest migration carrying a full
`outbox.SchemaSQL` body) — the new migration carries an `ALTER`, not the
create text.

**ledger holds live customer data.** This migration adds one column to the
`outbox` table, rewrites no rows, and drops nothing. No domain table, no
journal row, and no posting is in its path.

**2. The context threading** (`internal/ledger` only). Three signatures widen
so the request context stops being dropped between the MCP boundary and the
outbox:

- `EventSink.AppendRecorded(ctx context.Context, tx *sql.Tx, t Transaction) error`
  (`service.go`)
- `(*Service).persist(ctx context.Context, tx *sql.Tx, t Transaction) error`
  (`service.go`)
- `(*outboxProducer).AppendRecorded(ctx context.Context, tx *sql.Tx, t Transaction) error`
  (`events.go`), which calls `o.ob.Append(ctx, tx, ev)`

`Record` (`transaction.go`) and `Reverse` (`reverse.go`) each already receive
`ctx` and use it for `BeginTx`; each passes that same `ctx` to `persist`. **No
ledger call site on any path reaching `Append` constructs a context** — no
`context.Background()`, no `context.TODO()`.

Everything else is unchanged: `transactionRecordedEvent`, the payload shape
(no `correlation_id` field is added to it — the id rides the envelope), the
`Ring()`-after-commit rule, the one-event-per-committed-transaction rule with
reversal mirrors included, and the atomicity of the append with the journal
write. `etc/manifest.env` gains no key. No MCP tool declares
`SensitiveParams`.

**Done when** — the suite is green per design's *Conventions*
(`cd ledger && go build ./...`, `go vet ./...`, `gofmt -l .` empty, and
`go test ./...` all pass with zero failures), the additional deterministic
checks below hold, and each id is covered by a clearly-named test tagged with
that id:

- **No context is manufactured on the emit path.** From `ledger/`,
  `grep -rn 'context.Background()\|context.TODO()' internal/ledger/ --include='*.go' | grep -v '_test.go'`
  prints nothing.
- **Exactly one new migration.** `internal/db/migrations/` gains exactly one
  file; `003_outbox.sql` and `20260712184833_outbox_routing.sql` are
  byte-identical to their committed bodies (`git diff --exit-code` over those
  two paths succeeds).
- R-Y3Z7-H9BG — a test applies ledger's full embedded migration set (`db.FS`
  through the appkit runner) to a fresh real SQLite database and asserts by
  `PRAGMA table_info(outbox)` that a `correlation_id` column exists, is `TEXT`,
  is `NOT NULL`, and defaults to `''` — and that the resulting column list is
  identical, in order, to the one `outbox.SchemaSQL` alone produces on a
  separate fresh database.
- R-VOH6-HG8R — a test drives `Record` on a migrated real SQLite database
  under a context prepared with `correlation.WithContext(ctx, X)` for a known
  valid id `X`, reads the appended outbox row back by SQL, and asserts its
  `correlation_id` is **exactly** `X` (not empty, and not some other
  well-formed 26-character id); it then drives `Reverse` under a context
  carrying a second, different known id `Y` and asserts the mirror's outbox
  row carries **exactly** `Y`. A build that passes `context.Background()` to
  `Append`, or mints in the producer, fails this.
- R-VPP2-V7ZG — a test drives `Record` on the same substrate under a context
  carrying **no** correlation id and asserts: the call returns no error, the
  transaction and its postings are present in the journal by SQL, and the
  appended outbox row's `correlation_id` is the **empty string** — no
  synthesized id. A build that rejects or errors on an uncorrelated append
  fails this.
- **No design Verification id is unrealized:** the ikispec coverage `comm -23`
  check (design ids vs `*_test.go` tags plus any remaining pending phase
  files) prints nothing but the literal `R-XXXX-XXXX` placeholder.
