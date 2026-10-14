# D26-db-bulk-and-immutable

`Open` (D15) is for a service's live database: WAL, a writer and a pool of readers, migrations, a busy timeout for the replicator's checkpoints. Some data is not like that. A service that builds a large file once and then only reads it, as scrapes does with each run's rows, gains nothing from a journal while the file is built, since a failed build is thrown away whole, and needs no lock while it reads a file nothing will ever write again. Package `db` offers two more opens for that life: one to build a new file, and one to read a finished file. Like `Open`, they keep the SQLite driver inside `db`, so a service never imports it.

## Building a file

`OpenBulk` creates a new database file at a path and returns a `Bulk` handle on one connection. The file is written without a rollback journal and without syncing: the journal mode is off and `synchronous` is off, so every write goes straight into the file once, and nothing is written twice. There are no migrations and no `schema_migrations` table: the caller creates its own schema through `Write`, and the file holds exactly what the caller put there. Foreign keys are enforced, as on every connection `Open` makes.

The path follows `Open`'s rules: an ordinary filesystem path, relative ones resolving against the working directory, SQLite's special sources refused. Missing parent directories are created private to the service's user, as `Open` creates them. The file must be new: `OpenBulk` refuses a path where anything already exists, so a file is built once, and a leftover from an interrupted build is the caller's to delete.

`Write` runs a function in one transaction, as `DB.Write` does, and commits when it returns nil. What it cannot do is undo: with no journal there is nothing to roll back to, and SQLite leaves a rollback in that mode undefined. So a `Write` that fails, whatever the cause (the function's error, a panic, the context ending, the commit), spoils the handle: every later `Write` fails without calling the function, and the file's contents are undefined. The caller closes the handle, which succeeds, and deletes the file. A build either completes or is discarded.

`Close` closes the connection and leaves only the file. It does not sync: without `synchronous` the bytes may still sit in the kernel's cache, and a caller that needs the file durable, before it renames it into place for example, syncs the file and its directory itself. A file `Bulk` built is an ordinary SQLite database in the rollback journal mode, which `OpenImmutable` reads.

## Reading a finished file

`OpenImmutable` opens an existing file read-only and marks it immutable to SQLite: it takes no locks, waits on no busy timeout, and creates no journal, WAL or shared-memory file beside it, so it reads a file in a directory it cannot write. The promise the caller makes in return is that nothing changes the file while a handle is open; a file `Bulk` built and closed is such a file. The path follows the same rules, and a missing file is reported as one, so a caller can tell a file that is gone from one that is broken. A file that is not a SQLite database is refused at open, not at the first read.

`Read` runs a function in one read transaction, as `DB.Read` does; a statement that would change the file fails. A service opens a handle for one task, a tool call or a stream, and closes it when the task ends; concurrent `Read` calls on one handle are safe, and how many run at once is not part of the contract.

Neither handle has a failure seam: a test breaks a build by failing its function, and a read by removing or corrupting the file.

## REQUIREMENTS

- R-FSE3-XJJ2: Package `db` MUST export type `Bulk` and `func OpenBulk(ctx context.Context, path string) (*Bulk, error)`, where `context` is the standard library's `context`.
- R-FTM0-BB9R: Package `db` MUST export the methods `func (b *Bulk) Write(ctx context.Context, fn func(*sql.Tx) error) error` and `func (b *Bulk) Close() error`, where `sql` is the standard library's `database/sql`.
- R-FUTW-P30G: Package `db` MUST export type `Immutable` and `func OpenImmutable(ctx context.Context, path string) (*Immutable, error)`.
- R-FW1T-2UR5: Package `db` MUST export the methods `func (i *Immutable) Read(ctx context.Context, fn func(*sql.Tx) error) error` and `func (i *Immutable) Close() error`.
- R-FYHL-UE8J: When `ctx` is not done before `db.OpenBulk` returns, `path` is not empty, is not `:memory:`, does not begin with `file:`, and contains no `?`, nothing exists at `path`, no file other than a directory exists where a directory of `path`'s parent would be, and the process can write and search the nearest existing directory among `path`'s ancestors and search every existing ancestor, `db.OpenBulk` MUST return a non-nil `*Bulk` and a nil error.
- R-FZPI-85Z8: When `db.OpenBulk` returns a non-nil `*Bulk`, a regular file MUST exist at `path` and every directory of `path`'s parent MUST exist, a relative `path` resolving against the process working directory at the time of the call.
- R-G0XE-LXPX: Each directory `db.OpenBulk` creates MUST have the owner's read, write, and search permission bits set and no group or other permission bits set.
- R-G25A-ZPGM: `db.OpenBulk` MUST NOT change the mode of any directory that existed before the call.
- R-G3D7-DH7B: `db.OpenBulk` MUST return a nil `*Bulk` and a non-nil error, and create no file or directory, when `path` is empty, is `:memory:`, begins with `file:`, or contains `?`.
- R-G4L3-R8Y0: When `ctx` is done before `db.OpenBulk` is called, `db.OpenBulk` MUST return a nil `*Bulk` and a non-nil error for which `errors.Is(err, ctx.Err())` is true, and create no file or directory.
- R-G5T0-50OP: When a file or directory exists at `path`, `db.OpenBulk` MUST return a nil `*Bulk` and a non-nil error for which `errors.Is(err, fs.ErrExist)` is true, where `fs` is the standard library's `io/fs`, and that file's bytes and mode, or that directory's mode and entries, MUST be unchanged.
- R-G70W-ISFE: `db.OpenBulk` MUST return a nil `*Bulk` and a non-nil error when a missing directory of `path`'s parent cannot be created because a regular file exists where it would be.
- R-G88S-WK63: When nothing exists at `path` and the process has no permission to write `path`'s existing parent directory, `db.OpenBulk` MUST return a nil `*Bulk` and a non-nil error and create no file.
- R-G9GP-ABWS: Within the `fn` of a `Bulk.Write` call, `PRAGMA journal_mode` executed on the `*sql.Tx` MUST yield the single value `off`.
- R-GAOL-O3NH: Within the `fn` of a `Bulk.Write` call, `PRAGMA synchronous` executed on the `*sql.Tx` MUST yield the single value `0`.
- R-GBWI-1VE6: Within the `fn` of a `Bulk.Write` call, `PRAGMA foreign_keys` executed on the `*sql.Tx` MUST yield the single value `1`.
- R-GD4E-FN4V: Within the `fn` of the first `Bulk.Write` call on a handle, `SELECT count(*) FROM sqlite_schema` executed on the `*sql.Tx` MUST yield `0`.
- R-GECA-TEVK: `Bulk.Write` MUST call `fn` at most once per call, and exactly once with a non-nil `*sql.Tx` in every call that returns nil.
- R-GFK7-76M9: When `fn` returns a non-nil error `fnErr`, `Bulk.Write` MUST return a non-nil error for which `errors.Is(err, fnErr)` is true.
- R-GGS3-KYCY: When `fn` panics, `Bulk.Write` MUST panic with the same value.
- R-GJ7W-CHUC: After a `Bulk.Write` call on a handle that returned a non-nil error or panicked, every later `Bulk.Write` call on that handle MUST return a non-nil error without calling `fn`.
- R-GKFS-Q9L1: When `ctx` becomes done before `fn` returns, `Bulk.Write` MUST return a non-nil error.
- R-GLNP-41BQ: When a `Bulk.Write` call is waiting for another `Bulk.Write` on the handle to return and its `ctx` becomes done, it MUST return a non-nil error for which `errors.Is(err, ctx.Err())` is true without calling `fn`.
- R-GMVL-HT2F: `Bulk.Write` calls on one handle MUST NOT run `fn` concurrently, so N concurrent calls whose `fn` each reads a counter and stores it plus one all return nil and leave the counter N greater.
- R-GO3H-VKT4: `Bulk.Write` MUST be safe to call concurrently from multiple goroutines on the same handle.
- R-GPBE-9CJT: When every `Bulk.Write` call on a handle returned nil and the first `Bulk.Close` call on it, made with no `Bulk.Write` in progress, returned, a `db.OpenImmutable` of the same `path` MUST return a non-nil `*Immutable`, and an `Immutable.Read` on it MUST see exactly the tables, indexes and rows those calls' `fn` left, and no table `schema_migrations` unless one of them created it.
- R-GQJA-N4AI: After the first `Bulk.Close` call on a handle returns, a file in `path`'s directory other than `path` that did not exist there before `db.OpenBulk` was called MUST NOT exist.
- R-GRR7-0W17: The first `Bulk.Close` call on a handle on which no `Bulk.Write` is in progress and every `Bulk.Write` returned nil MUST return nil.
- R-GSZ3-ENRW: The first `Bulk.Close` call on a handle on which no `Bulk.Write` is in progress and a `Bulk.Write` returned a non-nil error or panicked MUST return nil.
- R-GU6Z-SFIL: Every `Bulk.Close` call after the first on a handle MUST return nil.
- R-GVEW-679A: After `Bulk.Close` returns, `Bulk.Write` on that handle MUST return a non-nil error without calling `fn`.
- R-GWMS-JYZZ: When `ctx` is not done before `db.OpenImmutable` returns, `path` is not empty, is not `:memory:`, does not begin with `file:`, and contains no `?`, `path` names a file that a `Bulk` built and whose handle was closed, and the process can read that file and search every directory among its ancestors, `db.OpenImmutable` MUST return a non-nil `*Immutable` and a nil error, whether or not the process can write that file or its directory.
- R-GXUO-XQQO: `db.OpenImmutable` MUST return a nil `*Immutable` and a non-nil error, and create no file or directory, when `path` is empty, is `:memory:`, begins with `file:`, or contains `?`.
- R-GZ2L-BIHD: When `ctx` is done before `db.OpenImmutable` is called, `db.OpenImmutable` MUST return a nil `*Immutable` and a non-nil error for which `errors.Is(err, ctx.Err())` is true, and create no file or directory.
- R-H1IE-31YR: When nothing exists at `path`, `db.OpenImmutable` MUST return a nil `*Immutable` and a non-nil error for which `errors.Is(err, fs.ErrNotExist)` is true, and create no file or directory.
- R-H2QA-GTPG: `db.OpenImmutable` MUST return a nil `*Immutable` and a non-nil error for which `errors.Is(err, fs.ErrNotExist)` is false when `path` names a directory.
- R-H3Y6-ULG5: When `path` names a non-empty regular file whose first 16 bytes are not the SQLite header string `SQLite format 3` followed by a NUL byte, `db.OpenImmutable` MUST return a nil `*Immutable` and a non-nil error for which `errors.Is(err, fs.ErrNotExist)` is false, and the file's bytes and mode MUST be unchanged.
- R-H563-8D6U: From before `db.OpenImmutable` is called until after the first `Immutable.Close` call on the handle it returned returns, with any `Immutable.Read` calls between, the bytes and mode of the file at `path` MUST be unchanged.
- R-H6DZ-M4XJ: After the first `Immutable.Close` call on a handle returns, a file in `path`'s directory that did not exist there before `db.OpenImmutable` was called MUST NOT exist.
- R-H7LV-ZWO8: `Immutable.Read` MUST call `fn` exactly once with a non-nil `*sql.Tx` and, when `fn` returns nil, return nil.
- R-H8TS-DOEX: When `fn` returns a non-nil error `fnErr`, `Immutable.Read` MUST return a non-nil error for which `errors.Is(err, fnErr)` is true.
- R-HA1O-RG5M: When `fn` panics, `Immutable.Read` MUST panic with the same value, and later `Immutable.Read` calls on the handle MUST behave as on a handle on which no `fn` panicked.
- R-HB9L-57WB: While another connection in the process holds an exclusive lock on the file at `path` and changes nothing, `Immutable.Read` MUST call `fn` and, when `fn` returns nil, return nil.
- R-HCHH-IZN0: A statement inside the `fn` of an `Immutable.Read` call that would change the database, such as an `INSERT` into an existing table, MUST return a non-nil error.
- R-HDPD-WRDP: `Immutable.Read` MUST be safe to call concurrently from multiple goroutines on the same handle, each call returning as it would alone.
- R-HEXA-AJ4E: After `Immutable.Close` returns, `Immutable.Read` on that handle MUST return a non-nil error without calling `fn`.
- R-HG56-OAV3: The first `Immutable.Close` call on a handle on which no `Immutable.Read` is in progress MUST return nil.
- R-HHD3-22LS: Every `Immutable.Close` call after the first on a handle MUST return nil.
