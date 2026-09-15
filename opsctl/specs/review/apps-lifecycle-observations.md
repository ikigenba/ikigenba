# App lifecycle external evidence

On 2026-09-14, read-only `ssh -o BatchMode=yes -o ConnectTimeout=10 dev`
probe `command -v sqlite3` printed no executable. `systemctl --help`
advertised `show` and `is-active`. This does not establish full unit or
journal protocol behavior; that remains part of external observations.

## SQLite persistent journal mode

[SQLite file format §1.3.3](https://www.sqlite.org/fileformat.html#file_format_version_numbers)
defines header offsets 18 and 19 as both 1 for rollback modes and both 2
for WAL. [SQLite PRAGMA journal_mode](https://www.sqlite.org/pragma.html#pragma_journal_mode)
describes journal modes, including the persistence of WAL across connections.
The header cannot expose another connection's transient choice among rollback
modes. Status adopts what a fresh read-only connection observes.

On the designated host, `python3` with its standard-library sqlite3 module
created six synthetic files inside a `TemporaryDirectory` named with prefix
`opsctl-journal-probe-`. For each, it created table `t(x)`, set one mode,
inserted one row, committed, read header bytes 18–19, and opened a separate
`file:<path>?mode=ro` URI connection to query `pragma journal_mode` while the
original connection remained open. Both connections were closed and the
entire temporary directory was automatically removed. No deployment files
were accessed. The module was used only by this evidence probe; it is not a
runtime dependency of opsctl.

| Original connection mode | Header bytes | Fresh read-only connection |
|---|---|---|
| delete | 0101 | delete |
| truncate | 0101 | delete |
| persist | 0101 | delete |
| memory | 0101 | delete |
| off | 0101 | delete |
| wal | 0202 | wal |

This establishes an implementation route using ordinary read-only file IO,
without a SQLite Go module or sqlite3 host tool. It establishes the two
persistent mode values on synthetic valid files. It does not prove handling
of corrupted files, races with concurrent journal-mode changes, or the absence
of writes from a SQLite library read itself; the target contract forbids such
writes and requires '-' for unsupported/unreadable input.

## Remaining external limits

Litestream is absent on dev per environment-observations.md. Stopping the app
before the shared replication process is an explicit ordering contract, but
actual Litestream shutdown sync and final remote database contents remain
unobserved under issues/external-observations.md and
issues/retire-final-database-guarantee.md. D10's final replica guarantee is
conditional on successful shutdown synchronization, not evidence of it.

The source's literal no-disk-write wording for restart is interpreted as
opsctl-owned effects; restarting an app necessarily allows its new process
to write its own state. D10 makes that distinction explicit without changing
the installed binary, unit or environment file.
