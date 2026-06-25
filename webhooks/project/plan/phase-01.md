# Phase 1 — Module scaffold & data model

*Realizes design Decision 2 (Data model & migrations). Depends on no earlier phase.*

Stand up the Go module so the suite can build and test, and land the persistence
layer the whole service rests on. This phase establishes the module plumbing —
`webhooks/go.mod` (module path `webhooks`, `go 1.26`) with committed `replace`
directives for `appkit => ../appkit` and `eventplane => ../eventplane`, and the
bare-SemVer `webhooks/VERSION` at `0.1.0` — but **not** the composition root
(`cmd/webhooks/main.go` is Phase 6); no `main.go` exists yet, so `go build ./...`
covers only the inner packages.

The data model itself lands as design D2 specifies:

- `internal/db/db.go` — `//go:embed migrations/*.sql` exporting the migration
  `FS`, plus the test `Open` helper that opens a real temp-file SQLite (`db.Open`,
  the suite convention) and applies migrations forward-only via the appkit runner.
- `internal/db/migrations/` — the bootstrap trio: `001_schema_migrations.sql`
  (appkit's tracking table, verbatim), `002_webhooks.sql` (the `webhooks` table —
  `name TEXT PRIMARY KEY`, `owner_email`, `secret_hash`, `created_at`,
  `last_triggered_at` nullable, plus `idx_webhooks_owner`), and `003_outbox.sql`
  **byte-identical** to `eventplane/outbox.SchemaSQL`.
- The concrete `Store` over `*sql.DB` (`Insert`, `GetByName`, `ListByOwner`,
  `Delete`, `UpdateSecret`, `TouchLastTriggered`), with `GetByName` returning the
  `secret_hash` *separately* from the secret-free `Webhook` value.

End state: `cd webhooks && go build ./... && go vet ./... && go test ./...` is
green, with the store tests exercising real temp-file SQLite.

**Done when:** design D2's Verification ids are each covered by a genuine
real-SQLite test and the suite is green —
- R-SZ8I-R4EY — duplicate-`name` `Insert` fails on the PK/UNIQUE constraint and
  leaves the original row untouched;
- R-T0GF-4W5N — `ListByOwner(A)` returns exactly A's webhooks, never B's;
- R-T1OB-INWC — `Delete` is owner-scoped (B's name is not deleted by A; B can then
  delete it);
- R-T2W7-WFN1 — committed `003_outbox.sql` is byte-identical to
  `eventplane/outbox.SchemaSQL`.
