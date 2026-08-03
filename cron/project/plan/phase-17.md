# Phase 17 — Mint a root correlation id at every tick and record its `root`

*Realizes design Decision 16 (correlation adoption and the tick root), code
slice — its ids R-2V3L-RE05, R-2WBI-55QU, R-2XJE-IXHJ, R-2YRA-WP88. Does
**not** depend on Phase 16 (that phase touches only `etc/nginx.conf`).*

> **Cross-workspace dependency — operator-sequenced, not built here.** This
> phase cannot compile until **`appkit` and `eventplane` have been built**
> against the suite telemetry capability. It consumes, as fixed external
> contracts: `eventplane/correlation` (`New`/`Valid`/`WithContext`/
> `FromContext`), the revised `outbox.Append(ctx, tx, ev)`,
> `outbox.SchemaSQL`'s new `correlation_id` column, `outbox.AddCorrelationIDSQL`,
> and appkit's `*telemetry.Recorder` with `StartRoot(ctx, op, detail)` plus the
> `rt.Recorder()` accessor on the chassis runtime.
> `project/research/research.md` records their exact footprint. The suite build
> order is: root + registry, then appkit and eventplane, then the telemetry
> service, then dashboard, then the remaining services including cron.

cron becomes the **root of every scheduled chain**. Three things get built.

**1. The migration.** One new timestamped migration under
`internal/db/migrations/`, minted with `bin/create-migration cron
outbox_correlation` (never a hand-picked number), whose body is
`outbox.AddCorrelationIDSQL` verbatim —
`ALTER TABLE outbox ADD COLUMN correlation_id TEXT NOT NULL DEFAULT '';`. The
frozen `003_outbox.sql` and the D14 `outbox_routing` migration are **not**
touched, and the existing DDL drift guard in
`internal/db/migrations_outbox_test.go` stays pointed at the `outbox_routing`
migration (the newest one carrying a full `outbox.SchemaSQL` body) — the new
migration carries an `ALTER`, not the create text.

**2. The recorder reaches the tick worker** (`cmd/cron/main.go` +
`internal/tick`). `tick.Worker` gains a `rec *telemetry.Recorder` field, set by
a widened `tick.New`. The composition root captures `rt.Recorder()` in
`Spec.Handlers` into a closure variable — exactly as it already captures
`store` — and passes it to `tick.New` in `Spec.Producer` alongside the outbox.
Every `Recorder` method is nil-safe, so `internal/tick` carries no nil checks
and no error handling for it.

**3. The mint and the root, in `fireOne`** (`internal/tick`). `fireOne` calls
`w.rec.StartRoot(ctx, "cron:"+event.Kind+event.Subject(name), nil)` **before**
`BeginTx`, and uses the returned context for the transaction and for
`w.ob.Append(ctx, tx, ev)`. The `op` is composed from the existing `event.Kind`
and `event.Subject` — no second format string — so the root record and the
emitted event cannot drift apart.

`StartRoot`, not `StartChain`: a tick has no cause outside itself, and adopting
an ambient id off the long-lived serve context would fuse every tick for the
process's lifetime into one chain. One call per **firing**, not per scan: two
schedules matching the same minute are two independent chains.

Everything else in the tick seam is unchanged — the same per-schedule
transaction Appends the event and advances `last_slot` atomically, the guarded
`UPDATE` and the in-scan `last_slot` check still give at-most-once per
`(schedule, slot)`, `Ring()` still fires once after the scan, and the payload
stays `{name, scheduled_for, fired_at}` with **no** `correlation_id` field (the
id rides the envelope). `etc/manifest.env` gains no key. No MCP tool declares
`SensitiveParams`.

**Done when** — the suite is green per design's *Conventions*
(`cd cron && go build ./...`, `go vet ./...`, `gofmt -l .` empty, and
`go test ./...` all pass with zero failures), the additional deterministic
checks below hold, and each id is covered by a clearly-named test tagged with
that id:

- **cron mints only through the chassis helper.** From `cron/`,
  `grep -rn 'correlation.New()' internal/ cmd/ --include='*.go' | grep -v '_test.go'`
  prints nothing — the mint is `StartRoot`'s, not a hand-rolled copy.
- **No context is manufactured on the emit path.**
  `grep -rn 'context.Background()\|context.TODO()' internal/tick/ --include='*.go' | grep -v '_test.go'`
  prints nothing.
- **Exactly one new migration.** `internal/db/migrations/` gains exactly one
  file; `003_outbox.sql` and the `outbox_routing` migration are byte-identical
  to their committed bodies (`git diff --exit-code` over those two paths
  succeeds).
- R-2V3L-RE05 — a test applies cron's full embedded migration set (`db.FS`
  through the appkit runner) to a fresh real SQLite database and asserts by
  `PRAGMA table_info(outbox)` that a `correlation_id` column exists, is `TEXT`,
  is `NOT NULL`, and defaults to `''` — and that the resulting column list is
  identical, in order, to the one `outbox.SchemaSQL` alone produces on a
  separate fresh database.
- R-2WBI-55QU — a test runs the real `Worker.Fire` over two schedules
  (`nightly`, `bill-sweep`) whose exprs match the slot, on a migrated real
  SQLite database, and asserts both outbox rows read back by SQL carry a
  `correlation_id` that is non-empty and passes `correlation.Valid` (26
  characters, Crockford alphabet — an id containing `I`/`L`/`O`/`U`, or of any
  other length, fails).
- R-2XJE-IXHJ — the same test asserts the two rows carry **different**
  `correlation_id` values, and that a subsequent `Fire` for a **later** slot
  appends a row whose id differs from both. A build that mints once per scan,
  once per worker, or once per process passes R-2WBI-55QU and fails this.
- R-2YRA-WP88 — a test wires the worker to an inspectable recorder, drives
  `Fire` for `bill-sweep`, and asserts exactly **one** record with `kind`
  `root` and `op` exactly `cron:tick/bill-sweep`, whose `correlation_id`
  **equals** the `correlation_id` on that schedule's outbox row read back by
  SQL; then asserts a second `Fire` for the **same** slot appends zero further
  outbox rows and emits zero further root records. A build that starts the root
  in `Fire`, outside the in-scan `last_slot` guard, fails the second half.
- **No design Verification id is unrealized:** the ikispec coverage `comm -23`
  check (design ids vs `*_test.go` tags plus any remaining pending phase files)
  prints nothing but the literal `R-XXXX-XXXX` placeholder.
