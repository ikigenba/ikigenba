# Stories — backup

A host is disposable; what it holds is not. Everything worth keeping goes to
one S3 prefix that belongs to this host and no other, and comes back from
there. Nothing else on the host reads or writes that prefix, and the host's
own role can reach no other space's.

The top-level usage gains two lines under `Commands:`:

```
  backup    back up the platform and its services to S3
  restore   restore a service's data from its backups
```

`init`'s sequence gains the step `timers`, after `nginx.conf`: the three
service and timer pairs that run the three backup subcommands, at the periods
the store names.

Configuration keys:

| key | value |
|---|---|
| `aws.region` | the region the backup bucket lives in, e.g. `us-east-2` |
| `backup.s3_uri` | the prefix this host backs up to, e.g. `s3://sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/` |
| `backup.full_seconds` | how often the timer runs a full backup; `0` or unset means never |
| `backup.incremental_seconds` | how often an incremental runs; `0` or unset means never |
| `backup.wal_seconds` | how often write-ahead log segments ship; `0` or unset means never |

What is backed up, and what is not:

- `/etc/ikigenba/` and `/etc/letsencrypt/` — the host's configuration and its
  certificate — under the name `platform`.
- Every service's `etc/` and `state/`, under the service's own name.
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
`[database]` table is what earns incrementals and WAL segments. A service with
no manifest, or a manifest that declares no database, is backed up by fulls
alone — which covers `platform`, and a service restored from backup but never
installed.

**Not settled here.** What an incremental holds, what a WAL segment is, how the
three kinds of object are told apart under `<backup.s3_uri><service>/`, and how
`opsctl restore` chooses among them are open questions. This group fixes the
three verbs, what declares a database, and who writes the timers; it does not
fix the layout.

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
Usage: opsctl backup <subcommand>

Back the host up to the prefix in backup.s3_uri: /etc/ikigenba/ and
/etc/letsencrypt/ as 'platform', and every service's etc/ and state/ under its
own name. cache/ is never backed up, and neither is anything opsctl generates.

Subcommands:
  full         write one whole backup of the platform and of every service
  incremental  write an incremental for every service that declares a database
  wal          ship write-ahead log segments for every service that declares one

A service declares its database with a [database] table in etc/manifest.toml
naming its engine and its path. A service without one is covered by fulls
alone. 'opsctl init' writes the timers that run these at the configured
periods.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## The host backs itself up

`ikigenba-backup-full.timer` runs this every `backup.full_seconds`. One line per thing
backed up, in name order with `platform` first, so the timer's mail — or an
operator running it by hand — says exactly what was written.

Command:

```
$ sudo opsctl backup full
```

Output:

```
platform: ok (2026-09-12T03:00:04Z.tar.zst, 48.2 KiB)
crm: ok (2026-09-12T03:00:04Z.tar.zst, 12.4 MiB)
dashboard: ok (2026-09-12T03:00:04Z.tar.zst, 1.1 MiB)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `aws.region` and `backup.s3_uri` are set, and the host's role can write
  under that prefix.
- Two services are installed.

Postconditions:

- `<backup.s3_uri>platform/2026-09-12T03:00:04Z.tar.zst` holds
  `/etc/ikigenba/` and `/etc/letsencrypt/`, and
  `<backup.s3_uri><service>/2026-09-12T03:00:04Z.tar.zst` holds that
  service's `etc/` and `state/`, for each service.
- Every object of one run carries the same timestamp, so a run is one set.
- Nothing on the host has changed, and no earlier backup was deleted or
  overwritten. What is kept for how long is the bucket's lifecycle policy,
  which infra owns and opsctl never touches.
- Running it again writes a new set under a new timestamp.

## The host backs up a service it cannot read

One service's failure is a fact about that service, so the run reports it and
still backs up everything else — one run tells the operator every problem —
and the exit code says the report holds a failure.

Command:

```
$ sudo opsctl backup full; echo "exit $?"
```

Output:

```
platform: ok (2026-09-12T03:00:04Z.tar.zst, 48.2 KiB)
crm: failed: /opt/crm/state/crm.db: permission denied
dashboard: ok (2026-09-12T03:00:04Z.tar.zst, 1.1 MiB)
exit 1
```

Exits 1. The lines are on stdout; stderr is empty.

Preconditions:

- `/opt/crm/state/crm.db` cannot be read.

Postconditions:

- The platform's and `dashboard`'s objects were written. No object was
  written for `crm`, and its earlier backups are untouched.

## An agent backs up a host with nowhere to put it

Command:

```
$ sudo opsctl backup full
```

Output:

```
opsctl: backup.s3_uri not set
```

Exits 1. The line is on stderr; stdout is empty. With `aws.region` unset the
line is `opsctl: aws.region not set`. `incremental` and `wal` say the same.

Preconditions:

- `backup.s3_uri` is unset or empty.

Postconditions:

- Nothing has changed. Nothing was read and no object was written.

## The host backs up the services that declare a database

`ikigenba-backup-incremental.timer` and `ikigenba-backup-wal.timer` run these
every `backup.incremental_seconds` and every `backup.wal_seconds`. Only the
services that declare a database appear: there is nothing an incremental or a
segment could hold for the others, and a line claiming otherwise would be a lie
about what is in the bucket.

The object names below are the full's shape standing in until the layout is
settled — see the note at the top of this group. What this story fixes is which
services are reported and which are silent.

Command:

```
$ sudo opsctl backup incremental
$ sudo opsctl backup wal
```

Output:

```
crm: ok (2026-09-12T09:00:04Z.tar.zst, 3.7 MiB)
```

```
crm: ok (2026-09-12T09:05:04Z.tar.zst, 96.0 KiB)
```

Each exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `aws.region` and `backup.s3_uri` are set, and the host's role can write
  under that prefix.
- `/opt/crm/etc/manifest.toml` declares a `[database]` at `state/crm.db`.
- `/opt/dashboard/etc/manifest.toml` declares none.

Postconditions:

- One object was written for `crm`. `dashboard` and `platform` have nothing
  written for them and are not reported.
- `/opt/crm/state/crm.db` is exactly as it was: both subcommands read it
  through its engine and neither stops a writer.
- No earlier object was deleted or overwritten.

## A service with no database gets fulls only

A host where nothing declares a database still runs both timers; they simply
find nothing to ship. Writing no object and saying so with the exit code is the
honest answer, and it keeps the timer from filling the bucket with empty
archives.

Command:

```
$ sudo opsctl backup incremental; echo "exit $?"
```

Output:

```
exit 0
```

Exits 0. Nothing is on stdout or stderr. `opsctl backup wal` behaves the same.

Preconditions:

- `aws.region` and `backup.s3_uri` are set.
- No installed service's manifest holds a `[database]` table, or no service is
  installed at all.

Postconditions:

- Nothing was read and no object was written. What those services hold comes
  back from their fulls and from nothing else.

## An operator asks for a period of zero

A period of `0` — or a key that was never set — is how an account says it wants
no backups of that kind. The unit pair is still written, so an operator can run
one by hand with `systemctl start`, but the timer is left disabled and stopped
and fires nothing.

Command:

```
$ sudo opsctl config set backup.incremental_seconds=0
$ sudo opsctl config set backup.wal_seconds=0
$ sudo opsctl init
$ systemctl is-enabled ikigenba-backup-wal.timer; echo "exit $?"
```

Output:

```
disabled
exit 1
```

Preconditions:

- `opsctl init` reports the host ready.

Postconditions:

- `ikigenba-backup-full.timer` is enabled and active at its period;
  `ikigenba-backup-incremental.timer` and `ikigenba-backup-wal.timer` are
  written, disabled, and not running.
- A space whose three periods are all `0` writes no backups at all, so its
  prefix stays empty and it can be a restore target but never a source.
- `ikigenba-backup-<kind>.service` runs `opsctl backup <kind>` as root, and
  changing a period is `opsctl config set` followed by `opsctl init`: a timer
  is generated from the store, so an edit to one would be overwritten by the
  next `init`.

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
Usage: opsctl restore SERVICE

Replace /opt/SERVICE/state/ with the newest backup under
<backup.s3_uri>SERVICE/. Data only: nothing under bin/, etc/, or share/ is
touched, and no service is started or stopped.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An agent gives a service another space's data

`devctl restore` has already copied the source space's newest backup set into
*this* host's prefix; from here it is an ordinary restore of the newest set
this host can see. opsctl alone decides what a restore does on the host.

Command:

```
$ sudo opsctl restore crm
```

Output:

```
source: ok (crm/2026-09-12T03:00:04Z.tar.zst, 12.4 MiB)
unit: ok (ikigenba-crm.service inactive)
state: ok (/opt/crm/state, 12 files)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `aws.region` and `backup.s3_uri` are set, and the host's role can read
  under that prefix.
- `<backup.s3_uri>crm/` holds at least one backup.
- `ikigenba-crm.service` is not active.

Postconditions:

- `/opt/crm/state/` is exactly what the newest backup holds. Anything that
  was there and is not in the backup is gone.
- `/opt/crm/bin/`, `/opt/crm/etc/`, and `/opt/crm/share/` are untouched: the
  code on the host is the deploy's business, not the restore's.
- No unit was started, stopped, enabled, or disabled, and nginx was not
  reloaded.
- No object under `<backup.s3_uri>` was written or deleted.

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
source: ok (crm/2026-09-12T03:00:04Z.tar.zst, 12.4 MiB)
unit: warning (ikigenba-crm.service is active)
state: ok (/opt/crm/state, 12 files)
exit 0
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `ikigenba-crm.service` is active.

Postconditions:

- Everything the previous story's postconditions say, and: the unit is still
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

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `<backup.s3_uri>gmail/` holds no object.

Postconditions:

- Nothing has changed. `/opt/gmail/` was neither created nor touched.

## An operator restores into a host that has never run the service

A restore is how a fresh host is given another host's data, so the service
need not be installed first. What comes back is data and only data: the host
has a `state/` for a service with no binary until a deploy brings one.

Command:

```
$ sudo opsctl restore crm
```

Output:

```
source: ok (crm/2026-09-12T03:00:04Z.tar.zst, 12.4 MiB)
unit: ok (no ikigenba-crm.service)
state: ok (/opt/crm/state, 12 files)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `/opt/crm/` does not exist and no `ikigenba-crm.service` is installed.

Postconditions:

- `/opt/crm/state/` holds the backup's data. `/opt/crm/` was created with
  mode `0755` and holds nothing else.
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

## An operator runs `backup` with no subcommand, or one that does not exist

Command:

```
$ sudo opsctl backup
```

Output:

```
opsctl: no backup subcommand given

see 'opsctl backup --help' for usage
```

Command:

```
$ sudo opsctl backup restore crm
```

Output:

```
opsctl: unknown backup subcommand 'restore'

see 'opsctl backup --help' for usage
```

Both exit 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.
