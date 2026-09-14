# Stories — backup

A host is disposable; what it holds is not. Everything worth keeping goes to
one S3 prefix that belongs to this host and no other, and comes back from
there. Nothing else on the host reads or writes that prefix, and the host's
own role can reach no other space's.

Two different things are kept, so there are two pairs of commands. A
**service** is what lives under `/opt/<name>/`; the **host** is the machine's
own configuration and its certificate. A service backup never touches `/etc/`,
and a host restore never touches `/opt/`.

The top-level usage gains three lines under `Commands:`:

```
  backup    back up a service's files to S3
  host      back up and restore the host's own configuration
  restore   restore a service from its backups
```

Configuration keys:

| key | value |
|---|---|
| `aws.region` | the region the backup bucket lives in, e.g. `us-east-2` |
| `backup.s3_uri` | the prefix this host backs up to, e.g. `s3://sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/` |
| `backup.host_files_seconds` | how often the host's own configuration is copied; `0` or unset means never |
| `backup.service_files_seconds` | how often every service's files are copied; `0` or unset means never |
| `backup.service_db_seconds` | how often a declared database is snapshotted whole |
| `backup.service_wal_seconds` | how often a declared database's committed changes are shipped |

## Two mechanisms, and where the line between them falls

A service's files are copied on a timer: `opsctl backup` tars `etc/` and
`state/` and writes one object. That is a walk of the filesystem, and it is
honest about being one — it holds whatever was on disk while it ran.

A SQLite database cannot be backed up that way. Copying the file while a
writer is mid-transaction yields a torn file, and its `-wal` companion is a
separate moving target that is meaningless without the exact database it
belongs to. Worse, the application's own connection checkpoints and resets the
WAL on its own schedule, so anything that only runs on a timer finds the
history it needed already folded away.

So a declared database is replicated by `litestream`, which runs continuously
as `litestream.service`. Being resident is the whole point: it holds a read
transaction open so the WAL cannot be reset past what it has not yet shipped,
and it takes `wal_autocheckpoint` away from SQLite so it decides when frames
are folded back. A one-shot run has no mark to hold and would lose
transactions between invocations.

`litestream` is on the host the way `nginx` and `certbot` are: it comes with
the account's launch template, and `init` checks for it on PATH rather than
installing it. opsctl drives it and never fetches it.

The two mechanisms therefore do not overlap. For a service that declares a
database, `opsctl backup` **excludes** the database file, its `-wal` and
`-shm`, and litestream's own `.<name>-litestream` metadata directory. Those
objects are litestream's and reach S3 by its path, not by the tarball's.

**The two clocks are not synchronised, and are not meant to be.** A restore
takes the newest files and, separately, the newest database — so the database
is typically newer than the files around it. The database is what matters; a
missing file is a missing file. An app that needs an artifact to be
transactionally consistent with its rows must keep that artifact **in** the
database.

There is no incremental. SQLite has no incremental backup primitive, and
litestream's compaction merges files it has already shipped rather than
creating a recovery point that did not exist. What can be restored from is a
snapshot and the committed changes after it, and nothing else.

## What is backed up, and what is not

- `/etc/ikigenba/` and `/etc/letsencrypt/` — the host's own configuration and
  its certificate — by `opsctl host backup`, under `host/`.
- Every service's `etc/` and `state/`, under the service's own name, by
  `opsctl backup`.
- Every declared database, under the service's own name, by `litestream`.
- Never `cache/`: it is, by the name, reconstructible.
- Never `/etc/nginx/` and never a unit file: both are generated from the
  configuration store and what is under `/opt`, so a restored host writes
  them again rather than carrying stale copies forward.

A service is discovered, never registered: any `/opt/<name>/` holding an
`etc/` or a `state/` directory.

A service **declares a database** when its `etc/manifest.toml` holds a
`[database]` table naming the `engine` and the `path`, relative to the
service's own directory, of the database that engine owns:

```toml
[database]
engine = "sqlite"
path = "state/crm.db"
```

This mirrors the split `nginx.md` already makes: a service is *discovered* by
what is on disk, and gets more than the baseline only if its manifest asks for
it. There, a manifest naming a `port` is what earns a server block. Here, a
`[database]` table is what earns replication. A service with no manifest, or
a manifest that declares no database, has its whole `state/` in the tarball
and nothing else.

`init`'s sequence gains two steps after `nginx.conf`: `litestream`, which
writes `/etc/litestream.yml` from the declared databases and the two database
periods and enables `litestream.service`, and `timers`, which writes the two
service and timer pairs that run the two file backups at their periods.

## An operator asks what `backup` can do

Command:

```
$ opsctl backup --help
```

```
$ opsctl backup -h
```

Output:

```
Usage: opsctl backup [SERVICE]

Copy every service's etc/ and state/ to the prefix in backup.s3_uri, under the
service's own name, or just SERVICE when one is named.

Never copied: cache/, anything opsctl generates, and -- for a service that
declares a [database] -- the database file, its -wal and -shm, and its
litestream metadata directory. Those are replicated continuously by
litestream.service. The host's own /etc/ is 'opsctl host backup'.

A service declares its database with a [database] table in etc/manifest.toml
naming its engine and its path. 'opsctl init' writes the timer that runs this
at backup.service_files_seconds.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## The host backs up its services

`ikigenba-backup-services.timer` runs this every
`backup.service_files_seconds`. One line per service, in name order, so the
timer's mail — or an operator running it by hand — says exactly what was
written.

Command:

```
$ sudo opsctl backup
```

Output:

```
crm: ok (2026-09-12T03:00:04Z.tar.zst, 1.2 MiB)
dashboard: ok (2026-09-12T03:00:04Z.tar.zst, 1.1 MiB)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `aws.region` and `backup.s3_uri` are set, and the host's role can write
  under that prefix.
- Two services are installed. `/opt/crm/etc/manifest.toml` declares a
  `[database]` at `state/crm.db`; `dashboard`'s declares none.

Postconditions:

- `<backup.s3_uri><service>/2026-09-12T03:00:04Z.tar.zst` holds that
  service's `etc/` and `state/`, for each service.
- `crm`'s object holds no `state/crm.db`, no `state/crm.db-wal`, no
  `state/crm.db-shm`, and nothing under `state/.crm.db-litestream/`.
  `dashboard`'s object holds all of its `state/`.
- Every object of one run carries the same timestamp, so a run is one set.
- Nothing on the host has changed, and no earlier backup was deleted or
  overwritten. What is kept for how long is the bucket's lifecycle policy,
  which infra owns and opsctl never touches.
- Running it again writes a new set under a new timestamp.

## An operator backs up one service

An operator about to do something risky to one service wants that service's
files in the bucket now, and has no reason to wait for the timer or to touch
the others.

Command:

```
$ sudo opsctl backup crm
```

Output:

```
crm: ok (2026-09-12T14:22:51Z.tar.zst, 1.2 MiB)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `/opt/crm/` holds an `etc/` or a `state/`.

Postconditions:

- One object was written, under `crm/` and no other prefix. No other
  service was read.

## The host backs up a service it cannot read

One service's failure is a fact about that service, so the run reports it and
still backs up everything else — one run tells the operator every problem —
and the exit code says the report holds a failure.

Command:

```
$ sudo opsctl backup; echo "exit $?"
```

Output:

```
crm: failed: /opt/crm/state/outbox: permission denied
dashboard: ok (2026-09-12T03:00:04Z.tar.zst, 1.1 MiB)
exit 1
```

Exits 1. The lines are on stdout; stderr is empty.

Preconditions:

- `/opt/crm/state/outbox` cannot be read.

Postconditions:

- `dashboard`'s object was written. No object was written for `crm`, and its
  earlier backups are untouched.
- `crm`'s database is unaffected: litestream is still replicating it, and a
  file the tarball could not read is not a file litestream reads.

## An operator backs up a service that is not there

Command:

```
$ sudo opsctl backup gmail
```

Output:

```
opsctl: no service 'gmail'
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `/opt/gmail/` does not exist, or holds neither an `etc/` nor a `state/`.

Postconditions:

- Nothing has changed. No object was written.

## An agent backs up a host with nowhere to put it

Command:

```
$ sudo opsctl backup
```

Output:

```
opsctl: backup.s3_uri not set
```

Exits 1. The line is on stderr; stdout is empty. With `aws.region` unset the
line is `opsctl: aws.region not set`. `opsctl host backup` and
`opsctl restore` say the same.

Preconditions:

- `backup.s3_uri` is unset or empty.

Postconditions:

- Nothing has changed. Nothing was read and no object was written.

## An operator asks what `host` can do

The host's own configuration is not a service and is not shaped like one:
there is one of it, it lives under `/etc/`, and restoring it replaces what the
machine is rather than what an app holds. It gets its own noun for that
reason.

Command:

```
$ opsctl host --help
```

```
$ opsctl host -h
```

Output:

```
Usage: opsctl host <subcommand>

Back up and restore the host's own configuration: /etc/ikigenba/ and
/etc/letsencrypt/, under 'host/' in backup.s3_uri. Nothing under /opt is
touched either way -- that is 'opsctl backup' and 'opsctl restore'.

Subcommands:
  backup    write /etc/ikigenba/ and /etc/letsencrypt/ to S3
  restore   replace them with the newest backup

'opsctl init' writes the timer that runs the backup at
backup.host_files_seconds.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## The host backs up its own configuration

`ikigenba-backup-host.timer` runs this every `backup.host_files_seconds`.
The configuration store is reconstructible — `devctl space create` writes
every key — but the certificate is not: the CA rate-limits how often it will
issue the same names, so a host rebuilt often can find itself unable to get
one back.

Command:

```
$ sudo opsctl host backup
```

Output:

```
host: ok (2026-09-12T03:00:04Z.tar.zst, 48.2 KiB)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `aws.region` and `backup.s3_uri` are set, and the host's role can write
  under that prefix.

Postconditions:

- `<backup.s3_uri>host/2026-09-12T03:00:04Z.tar.zst` holds `/etc/ikigenba/`
  and `/etc/letsencrypt/`, with their modes.
- Nothing under `/opt/` was read. No earlier object was deleted or
  overwritten.

## An operator gives a rebuilt host its certificate back

`/etc/letsencrypt/` comes back rather than being re-issued, which is the
point. The configuration store comes back with it, so a host restored this way
is configured as the old one was — including any key set by hand that
`space create` would not know to write.

Command:

```
$ sudo opsctl host restore
```

Output:

```
source: ok (host/2026-09-12T03:00:04Z.tar.zst, 48.2 KiB)
files: ok (/etc/ikigenba, /etc/letsencrypt, 31 files)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `aws.region` and `backup.s3_uri` are set, and the host's role can read
  under that prefix.
- `<backup.s3_uri>host/` holds at least one backup.

Postconditions:

- `/etc/ikigenba/` and `/etc/letsencrypt/` are exactly what the newest backup
  holds, with their modes. Anything that was there and is not in the backup
  is gone.
- Nothing under `/opt/` was touched, no unit was started or stopped, and
  nginx was not reloaded. `opsctl init` is what makes the host act on what
  was just restored.
- No object under `<backup.s3_uri>` was written or deleted.

## An operator asks for a period of zero

A period of `0` — or a key that was never set — is how an account says it
wants no backups of that kind. The unit pair is still written, so an operator
can run one by hand with `systemctl start`, but the timer is left disabled and
stopped and fires nothing.

Command:

```
$ sudo opsctl config set backup.service_files_seconds=0
$ sudo opsctl init
$ systemctl is-enabled ikigenba-backup-services.timer; echo "exit $?"
```

Output:

```
disabled
exit 1
```

Preconditions:

- `opsctl init` reports the host ready.

Postconditions:

- `ikigenba-backup-host.timer` is enabled and active at its period;
  `ikigenba-backup-services.timer` is written, disabled, and not running.
- `ikigenba-backup-host.service` runs `opsctl host backup` as root and
  `ikigenba-backup-services.service` runs `opsctl backup` as root; changing a
  period is `opsctl config set` followed by `opsctl init`, because a timer is
  generated from the store and an edit to one would be overwritten by the
  next `init`.
- A space whose periods are all `0` and which has no service declaring a
  database writes nothing at all, so its prefix stays empty and it can be a
  restore target but never a source.

## An operator asks what `restore` can do

Command:

```
$ opsctl restore --help
```

```
$ opsctl restore -h
```

Output:

```
Usage: opsctl restore SERVICE [--prefix <s3 uri>]

Replace /opt/SERVICE/etc/ and /opt/SERVICE/state/ with the newest backup, and,
when SERVICE declares a [database], replace that database with the newest
litestream has. Nothing under bin/ or share/ is touched, and no service is
started or stopped.

Options:
  --prefix <s3 uri>   where to read from; defaults to <backup.s3_uri>SERVICE/

The files and the database are restored to their own newest points, which are
not the same instant. The database is the newer of the two.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An operator puts a service back as it was

The `db` line is the database's own newest point, which is later than the
tarball's: the tarball is written on a timer and the database is replicated
continuously.

Command:

```
$ sudo opsctl restore crm
```

Output:

```
source: ok (crm/2026-09-12T03:00:04Z.tar.zst, 1.2 MiB)
unit: ok (ikigenba-crm.service inactive)
files: ok (/opt/crm/etc, /opt/crm/state, 12 files)
db: ok (/opt/crm/state/crm.db, newest 2026-09-12T09:07:11Z)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `aws.region` and `backup.s3_uri` are set, and the host's role can read
  under that prefix.
- `<backup.s3_uri>crm/` holds at least one tarball and litestream has
  replicated `crm`'s database there.
- `ikigenba-crm.service` is not active.

Postconditions:

- `/opt/crm/etc/` and `/opt/crm/state/` are what the newest tarball holds,
  and then `/opt/crm/state/crm.db` is what litestream restored. Anything that
  was there and is in neither is gone.
- `/opt/crm/bin/` and `/opt/crm/share/` are untouched: the code on the host is
  the deploy's business, not the restore's.
- No unit was started, stopped, enabled, or disabled, and nginx was not
  reloaded.
- No object under `<backup.s3_uri>` was written or deleted.

## An operator puts back a service that keeps no database

With no `[database]` in the manifest there is nothing for litestream to have
replicated, and the whole of `state/` was in the tarball. The `db` line is
absent rather than reported as skipped: a line claiming a database was handled
would be a lie about what is on disk.

Command:

```
$ sudo opsctl restore dashboard
```

Output:

```
source: ok (dashboard/2026-09-12T03:00:04Z.tar.zst, 1.1 MiB)
unit: ok (ikigenba-dashboard.service inactive)
files: ok (/opt/dashboard/etc, /opt/dashboard/state, 40 files)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `/opt/dashboard/etc/manifest.toml` declares no `[database]`.

Postconditions:

- `/opt/dashboard/state/` is exactly what the tarball holds, and is the whole
  of it. No litestream call was made.

## An agent gives a service another space's data

`devctl restore` has copied the source space's newest set — the tarball and
litestream's objects — into a staging prefix under *this* host's own prefix,
and names that prefix here. It is staged rather than written into `crm/`
because `crm/` is this host's own backup history: another space's objects
sitting in it would outlive the restore, and a later `opsctl restore crm` with
no `--prefix` could pick one up.

Command:

```
$ sudo opsctl restore crm --prefix s3://sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/restore/crm/
```

Output:

```
source: ok (restore/crm/2026-09-12T03:00:04Z.tar.zst, 12.4 MiB)
unit: ok (ikigenba-crm.service inactive)
files: ok (/opt/crm/etc, /opt/crm/state, 12 files)
db: ok (/opt/crm/state/crm.db, newest 2026-09-12T03:04:55Z)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The named prefix holds a tarball and litestream's objects for `crm`.
- `ikigenba-crm.service` is not active.

Postconditions:

- Everything the ordinary restore's postconditions say, read from the named
  prefix instead of `<backup.s3_uri>crm/`.
- `<backup.s3_uri>crm/` was not read and not written: this host's own backup
  history neither contributed to the restore nor gained an object from it.
- Nothing under the staging prefix was deleted. Clearing it is devctl's, and
  the bucket's lifecycle policy reaps what is left.

## An operator restores a service that is running

opsctl never starts or stops anything, so it does not stop the service for the
operator — and it does not pretend the data underneath a running process was
replaced quietly either. The state of the unit is a fact about the host, so it
is part of the report rather than a diagnostic, and the restore still happens.

Command:

```
$ sudo opsctl restore crm; echo "exit $?"
```

Output:

```
source: ok (crm/2026-09-12T03:00:04Z.tar.zst, 1.2 MiB)
unit: warning (ikigenba-crm.service is active)
files: ok (/opt/crm/etc, /opt/crm/state, 12 files)
db: ok (/opt/crm/state/crm.db, newest 2026-09-12T09:07:11Z)
exit 0
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `ikigenba-crm.service` is active.

Postconditions:

- Everything the ordinary restore's postconditions say, and: the unit is still
  active and was never signalled. Whether the app noticed its data change
  underneath it is the app's business; restarting it is the operator's.

## An operator restores a service with no backups

Command:

```
$ sudo opsctl restore gmail
```

Output:

```
opsctl: no backups for gmail under s3://sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/
```

Exits 1. The line is on stderr; stdout is empty. With `--prefix` given, the
line names the prefix that was read.

Preconditions:

- `<backup.s3_uri>gmail/` holds no object.

Postconditions:

- Nothing has changed. `/opt/gmail/` was neither created nor touched.

## A restore finds files but no database

The tarball and the database reach S3 by two different paths, so one can be
there without the other — a prefix staged by hand, a service whose litestream
replication never started, a `[database]` added to a manifest after the last
backup. The files are restored before the database is looked for, so the
report says how far it got, and the exit code says it did not finish.

Command:

```
$ sudo opsctl restore crm; echo "exit $?"
```

Output:

```
source: ok (crm/2026-09-12T03:00:04Z.tar.zst, 1.2 MiB)
unit: ok (ikigenba-crm.service inactive)
files: ok (/opt/crm/etc, /opt/crm/state, 12 files)
db: failed: litestream restore: no snapshot under the prefix
exit 1
```

Exits 1. The lines are on stdout; stderr is empty.

Preconditions:

- `<backup.s3_uri>crm/` holds a tarball, and litestream has replicated
  nothing for `crm`.
- `/opt/crm/etc/manifest.toml` in that tarball declares a `[database]`.

Postconditions:

- `/opt/crm/etc/` and `/opt/crm/state/` are the tarball's, and there is no
  database at `/opt/crm/state/crm.db`: the tarball never held one.
- Nothing was rolled back. The host is left where an operator can look at it,
  and re-running the restore once the database objects are in place finishes
  the job.

## An operator restores into a host that has never run the service

A restore is how a fresh host is given another host's data, so the service
need not be installed first. What comes back is data and only data: the host
has an `etc/` and a `state/` for a service with no binary until a deploy brings
one.

Command:

```
$ sudo opsctl restore crm
```

Output:

```
source: ok (crm/2026-09-12T03:00:04Z.tar.zst, 1.2 MiB)
unit: ok (no ikigenba-crm.service)
files: ok (/opt/crm/etc, /opt/crm/state, 12 files)
db: ok (/opt/crm/state/crm.db, newest 2026-09-12T09:07:11Z)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `/opt/crm/` does not exist and no `ikigenba-crm.service` is installed.

Postconditions:

- `/opt/crm/etc/` and `/opt/crm/state/` hold the backup's data. `/opt/crm/`
  was created with mode `0755` and holds nothing else.
- The manifest the restore just wrote is what said the service declares a
  database, so the `db` line is there even though nothing was installed.
- `opsctl status` shows `crm - -` until an app is deployed over it.

## An operator runs restore with no service, or more than one

Command:

```
$ sudo opsctl restore
```

Output:

```
opsctl: restore needs SERVICE

see 'opsctl restore --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. More than one operand gives
`opsctl: restore takes one SERVICE`.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.

## An operator runs `host` with no subcommand, or one that does not exist

Command:

```
$ sudo opsctl host
```

Output:

```
opsctl: no host subcommand given

see 'opsctl host --help' for usage
```

Command:

```
$ sudo opsctl host status
```

Output:

```
opsctl: unknown host subcommand 'status'

see 'opsctl host --help' for usage
```

Both exit 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.
