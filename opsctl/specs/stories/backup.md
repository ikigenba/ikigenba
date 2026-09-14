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

Configuration keys:

| key | value |
|---|---|
| `aws.region` | the region the backup bucket lives in, e.g. `us-east-2` |
| `backup.s3_uri` | the prefix this host backs up to, e.g. `s3://sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/` |
| `backup.full_seconds` | how often the timer runs a full backup |
| `backup.incremental_seconds` | how often an incremental runs, once a service has a database |
| `backup.wal_seconds` | how often write-ahead log segments ship, once a service has a database |

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
  run   write one backup of the platform and of every service

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

A systemd timer runs this every `backup.full_seconds`. One line per thing
backed up, in name order with `platform` first, so the timer's mail — or an
operator running it by hand — says exactly what was written.

Command:

```
$ sudo opsctl backup run
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
$ sudo opsctl backup run; echo "exit $?"
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
$ sudo opsctl backup run
```

Output:

```
opsctl: backup.s3_uri not set
```

Exits 1. The line is on stderr; stdout is empty. With `aws.region` unset the
line is `opsctl: aws.region not set`.

Preconditions:

- `backup.s3_uri` is unset or empty.

Postconditions:

- Nothing has changed. Nothing was read and no object was written.

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
