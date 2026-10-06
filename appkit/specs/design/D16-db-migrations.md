# D16-db-migrations

A service's schema is a sequence of SQL files embedded in its binary, and `db.Open` (D15) brings the database up to date with them before it returns the handle. So the schema a binary expects is the schema it runs against, a fresh database gets the whole schema at first start, and an old database put back under a newer binary upgrades itself when the service starts.

## The migration files

A service keeps its migrations in its `migrations/` directory, embeds it, and passes `fs.Sub` of it to `Open` as `Config.Migrations`, so the files sit at the root of that file system. Each is named `0001_<name>.sql`, `0002_<name>.sql` and so on; the four digits are its version, and the versions run from 1 with no gap. A file may hold several statements. Anything else at the root is a mistake in the service's build, so `Open` refuses the whole set before it touches the disk. An empty set is allowed and means no schema of the service's own.

Migrations are forward only: there are none to undo a version, and restoring a backup is the rollback. A migration that has been applied is never edited; a change is a new file, and an edit to an applied file is simply never run. Migrations carry the schema and may transform existing data, but never seed rows; seed data is a snapshot's job. A service adopting this mechanism over an existing database writes its first migration with `CREATE TABLE IF NOT EXISTS`, so it changes nothing there and is recorded as applied.

## Applying them

`Open` records each applied version, and when it was applied, in the table `schema_migrations`. It applies every version not yet recorded, in order, each in its own transaction together with its row, so a migration that fails leaves the database as the previous one left it, and `Open` fails naming the file. Migrations run on the writer before any reader exists, so no reader sees a half-migrated schema. The time is read from `Config.Now`, so a test fixes it, and written in the same layout telemetry events use.

A recorded version the binary does not know means a newer binary migrated this database and an older one is now starting on it, as after a rollback deploy. `Open` refuses to start rather than run against a schema it does not understand. Its error is `ErrUnknownVersion`, wrapped, and its message names the lowest such version as `db status` prints it, as in `unknown migration version: 0002`; naming one keeps the line short, and `db status` lists them all.

## Status

Every service has a `db status` subcommand that prints its migrations: the ones applied, with their time, the ones pending, and any the binary does not know. When there is one, it still prints every line, then returns the same error `Open` would, naming the lowest. `Status` is that subcommand's whole body; the service calls it with the same `Config` it gives `Open` and its standard output (it reads no clock), and wires it as it wires `manifest` and `--version`. The service's own command-line design owns the grammar and the exit codes; appkit owns the output and the errors. `Status` only looks: it applies nothing, and on a host where the database does not yet exist it reports everything pending and creates nothing. The manifest's `[database]` table, which tells the host about the database file, is opsctl's format, not appkit's.

## REQUIREMENTS

- R-MVSS-S98A: Package `db` MUST export `var ErrUnknownVersion error`, non-nil.
- R-QB4H-2NM9: Package `db` MUST export `func Status(ctx context.Context, cfg Config, w io.Writer) error`, where `io` is the standard library's `io`.
- R-QCCD-GFCY: The migrations MUST be valid exactly when `cfg.Migrations` is not nil, every entry at its root is a regular file whose name matches the regular expression `^[0-9]{4}_[a-z0-9_]+\.sql$`, and the versions of those files, each the integer its first four digits spell, are exactly the integers 1 through N, each once, for some N of 0 or more.
- R-QDK9-U73N: When the migrations are not valid, `db.Open` MUST return a nil `*DB` and a non-nil error, create no file or directory, and leave an existing database at `cfg.Path` byte for byte unchanged.
- R-QES6-7YUC: When the migrations are not valid, the error `db.Open` or `db.Status` returns MUST NOT satisfy `errors.Is(err, db.ErrUnknownVersion)`, whatever the database at `cfg.Path` holds.
- R-N0OE-BC72: After every `db.Open` call that returns a non-nil `*DB`, the database MUST hold a table `schema_migrations` whose columns, as `PRAGMA table_info(schema_migrations)` reports them, are exactly `version`, declared type `INTEGER` and the primary key, then `applied_at`, declared type `TEXT`.
- R-N1WA-P3XR: Before it returns a non-nil `*DB`, `db.Open` MUST execute every SQL statement of each migration whose version `schema_migrations` does not hold, in ascending order of version, and add for each a row to `schema_migrations` whose `version` is that version.
- R-N347-2VOG: `db.Open` MUST NOT execute a migration whose version `schema_migrations` already holds, whatever that file now contains.
- R-QG02-LQL1: The `applied_at` of each row `db.Open` adds MUST be exactly `cfg.Now()` read when that migration is applied, converted to UTC, truncated with `Time.Truncate(time.Microsecond)`, and formatted with the layout `2006-01-02T15:04:05.000000Z`.
- R-QH7Y-ZIBQ: When `cfg.Now` is nil, `db.Open` MUST take each `applied_at` from the standard library's `time.Now`.
- R-N5JZ-UF5U: When a statement of a migration fails, `db.Open` MUST return a nil `*DB` and a non-nil error whose message contains that migration's file name.
- R-4YUM-515F: When a statement of a migration fails, none of that migration's changes and no `schema_migrations` row for its version MUST be kept.
- R-502I-ISW4: When a statement of a migration fails, every migration of lower version that the same `db.Open` call applied MUST remain applied with its `schema_migrations` row.
- R-JZVB-L0R9: When the migrations are valid and `schema_migrations` holds a version that is not the version of a migration, `db.Open` MUST return a nil `*DB` and a non-nil error for which `errors.Is(err, db.ErrUnknownVersion)` is true.
- R-TLWM-VN5T: When the migrations are valid and `schema_migrations` holds a version that is not the version of a migration, the message of the error `db.Open` returns MUST be exactly `unknown migration version: ` followed by the lowest such version in decimal, zero-padded to four digits.
- R-N97O-ZQDX: When `schema_migrations` holds a version that is not the version of a migration, `db.Open` MUST execute no migration and add no row to `schema_migrations`.
- R-P68C-RQSP: When `ctx` is not done, `cfg.Path` is not empty, is not `:memory:`, does not begin with `file:`, and contains no `?`, and the migrations are valid, and every `w.Write` call returns a nil error, and `cfg.Path` names a SQLite database the process can open and read that holds `schema_migrations`, `db.Status` MUST write to `w`, for each version that is the version of a migration or that `schema_migrations` holds, once each and in ascending order, exactly one line: the version in decimal zero-padded to four digits, then ` applied ` and the row's `applied_at` text when it is both, ` pending` when it is only a migration's, or ` unknown ` and the row's `applied_at` text when it is only held, then LF; and MUST write nothing else.
- R-QJNR-R1T4: `db.Status` MUST NOT execute any migration and MUST NOT change any table, row, or schema of the database at `cfg.Path`.
- R-QM3K-ILAI: `db.Status` MUST NOT change the journal mode of the database at `cfg.Path`, so a database in the rollback journal mode stays in it.
- R-QNBG-WD17: `db.Status` MUST NOT call `cfg.Now`.
- R-P7G9-5IJE: When `ctx` is not done, `cfg.Path` is not empty, is not `:memory:`, does not begin with `file:`, and contains no `?`, and the migrations are valid, and every `w.Write` call returns a nil error, and nothing exists at `cfg.Path`, `db.Status` MUST write, for each migration in ascending order of version, its version zero-padded to four digits then ` pending` then LF, MUST write nothing else, and MUST create no file or directory.
- R-P8O5-JAA3: When `ctx` is not done, `cfg.Path` is not empty, is not `:memory:`, does not begin with `file:`, and contains no `?`, and the migrations are valid, and every `w.Write` call returns a nil error, and `cfg.Path` names a SQLite database the process can open and read that holds no table `schema_migrations`, `db.Status` MUST write, for each migration in ascending order of version, its version zero-padded to four digits then ` pending` then LF, and MUST write nothing else.
- R-BBYT-8YTC: When `db.Status` writes an ` unknown ` line, it MUST return a non-nil error for which `errors.Is(err, db.ErrUnknownVersion)` is true.
- R-TN4J-9EWI: When `db.Status` writes an ` unknown ` line and every `w.Write` call returns a nil error, the message of the error it returns MUST be exactly `unknown migration version: ` followed by the lowest version for which it writes an ` unknown ` line, in decimal, zero-padded to four digits.
- R-QQZ6-1O9A: When `ctx` is not done, `cfg.Path` is not one R-QS72-FFZZ rejects, the migrations are valid, `cfg.Path` names nothing or a SQLite database the process can open and read, `schema_migrations` holds no version that is not the version of a migration, and every `w.Write` call returns a nil error, `db.Status` MUST return nil.
- R-QS72-FFZZ: `db.Status` MUST return a non-nil error, write nothing to `w`, and create no file or directory when `cfg.Path` is empty, is `:memory:`, begins with `file:`, or contains `?`.
- R-NIYW-1WBH: `db.Status` MUST return a non-nil error and write nothing to `w` when the migrations are not valid.
- R-5660-FNLL: `db.Status` MUST return a non-nil error and write nothing to `w` when `cfg.Path` names a non-empty regular file whose first 16 bytes are not the SQLite header string `SQLite format 3` followed by a NUL byte.
- R-QUMV-6ZHD: When `cfg.Path` names an existing file that `db.Status` cannot open or read as a SQLite database, as when the process has no permission to read it, `db.Status` MUST return a non-nil error and write nothing to `w`.
- R-BGUE-S1S4: When `ctx` is done before `db.Status` is called, `db.Status` MUST return a non-nil error for which `errors.Is(err, ctx.Err())` is true and write nothing to `w`.
- R-QVUR-KR82: `db.Status` MUST return a non-nil error and write nothing to `w` when `cfg.Path` names a directory.
- R-NLEO-TFSV: When a `w.Write` call returns a non-nil error `werr`, `db.Status` MUST return a non-nil error for which `errors.Is(err, werr)` is true.
