# Stories — restore

A fresh space starts from seed. When a developer wants a space to hold what
another space held, the only channel is that other space's backups in the
account's backup bucket. Restore copies one app's newest backup set from the
source space's prefix into a staging prefix under the target space's own, and
has `opsctl` on the target host restore from that staging prefix. Nothing is
ever read from a running host, and no host can read another space's prefix;
the developer's identity does the copy.

A **backup set** is everything `opsctl` and `litestream` wrote for that app
under `<from domain>/<app>/`: the newest file tarball, and the database
objects needed to rebuild the app's database at its own newest point. The
exact layout is opsctl's and litestream's; devctl copies the set whole and
never looks inside an object.

The copy lands in `<domain>/restore/<app>/`, never in `<domain>/<app>/`. That
second prefix is the target host's **own** backup history. Another space's
objects written into it would outlive the restore, and a later
`opsctl restore <app>` on that host — the ordinary one, with no prefix given —
could pick one up and quietly hand the host a third space's data. Staging
keeps the two apart, and `opsctl restore --prefix` is how the host is told to
read the staged set this once.

The two halves of the set are restored to their own newest points, which are
not the same instant: the tarball is written on a timer and the database is
replicated continuously, so the database is the newer of the two. That is
opsctl's rule and devctl does not try to improve on it.

## A developer asks what `restore` can do

The top-level usage gains the line `  restore   give a space's app another
space's data` under `Commands:`.

Command:

```
$ devctl restore --help
```

Output:

```
Usage: devctl --account <name> restore <domain> <app> --from <from domain> [--from-account <name>]

Copy <app>'s newest backup set from <from domain>'s prefix to <domain>'s
restore/<app>/ staging prefix in the backup bucket, then have opsctl on
<domain> restore <app> from it. <domain>'s own backups are neither read nor
written.

Options:
  --from <from domain>     the space whose backups are copied; required
  --from-account <name>    the profile that account is read with; defaults to --account

A restore across accounts reads the source bucket with --from-account and writes
the target bucket with --account.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer gives a sandbox space the apex space's data

A developer debugging against production-shaped data wants the apex space's
`crm` data on their sandbox space. `--from` names the source space;
`--from-account` names the profile its bucket is read with, because the
source is in another account.

The source need not exist as an instance any more; only its backups under
`<from domain>/<app>/` in its account's backup bucket need exist.

Command:

```
$ devctl --account 602773793009 restore foo.sbx.ikigenba.dev crm --from ikigenba.dev --from-account 295229566359
```

Output:

```
source: ok (ikigenba-dev-295229566359/ikigenba.dev/crm/, newest 2026-09-12T03:00:04Z)
copy: ok (9 objects -> sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/restore/crm/)
restore: ok (opsctl restore crm)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- Live SSO sessions for both profiles.
- The target space exists, its instance is `running`, and `opsctl` is
  installed on it.
- Backups for `crm` exist under `ikigenba.dev/crm/` in the durable account's
  backup bucket.
- The developer's ssh configuration can reach the target instance as
  `ec2-user`.

Postconditions:

- `foo.sbx.ikigenba.dev/restore/crm/` in the ephemeral account's bucket was
  emptied and now holds the newest backup set from `ikigenba.dev/crm/`,
  object for object. The source prefix is untouched.
- `sudo opsctl restore crm --prefix s3://sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/restore/crm/`
  has been run on the target host over ssh and has exited 0, so `crm`'s
  `etc/`, `state/`, and database on the target are the source's data.
  `opsctl` alone decides what a restore does on the host; devctl runs it and
  reports its exit.
- `foo.sbx.ikigenba.dev/crm/`, the target's own backup history, was neither
  read nor written. The next `opsctl backup` on that host adds to it as
  before.
- The staged objects are left where they are. The bucket's lifecycle policy
  reaps them; the next restore of this app empties the prefix first.

## A developer gives a space another space's data in the same account

With `--from-account` omitted, the source is in the account named by
`--account`.

Command:

```
$ devctl --account 295229566359 restore staging.ikigenba.dev crm --from ikigenba.dev
```

Output:

```
source: ok (ikigenba-dev-295229566359/ikigenba.dev/crm/, newest 2026-09-12T03:00:04Z)
copy: ok (9 objects -> ikigenba-dev-295229566359/staging.ikigenba.dev/restore/crm/)
restore: ok (opsctl restore crm)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The target space `staging.ikigenba.dev` exists, its instance is `running`,
  and `opsctl` is installed on it.
- Backups for `crm` exist under `ikigenba.dev/crm/` in the account's backup
  bucket.
- The developer's ssh configuration can reach the target instance as
  `ec2-user`.

Postconditions:

- The newest backup set is copied to `staging.ikigenba.dev/restore/crm/` in
  the same bucket; the source prefix is untouched, and so is
  `staging.ikigenba.dev/crm/`.
- `sudo opsctl restore crm --prefix` naming that staging prefix has been run
  on the target host and exited 0.

## A developer restores an app the source space kept no database for

An app with no `[database]` in its manifest has one kind of object in its
prefix and no other. The set is smaller and the copy says so; nothing else
about the command changes, because what the set holds is opsctl's business.

Command:

```
$ devctl --account 602773793009 restore foo.sbx.ikigenba.dev dashboard --from ikigenba.dev --from-account 295229566359
```

Output:

```
source: ok (ikigenba-dev-295229566359/ikigenba.dev/dashboard/, newest 2026-09-12T03:00:04Z)
copy: ok (1 object -> sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/restore/dashboard/)
restore: ok (opsctl restore dashboard)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `ikigenba.dev/dashboard/` holds file tarballs and no database objects.

Postconditions:

- Everything the first story's postconditions say, for `dashboard`.
- `opsctl restore dashboard` reported no `db` line, because the app declares
  no database; devctl reports the exit and not the lines.

## A developer runs `restore` without a source

Command:

```
$ devctl --account 602773793009 restore foo.sbx.ikigenba.dev crm
```

Output:

```
devctl: restore needs --from <from domain>

see 'devctl restore --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer names the target as its own source

Command:

```
$ devctl --account 602773793009 restore foo.sbx.ikigenba.dev crm --from foo.sbx.ikigenba.dev
```

Output:

```
devctl: --from names the space being restored
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer restores to a space that does not exist

Command:

```
$ devctl --account 602773793009 restore gone.sbx.ikigenba.dev crm --from ikigenba.dev --from-account 295229566359
```

Output:

```
devctl: no space at 'gone.sbx.ikigenba.dev'
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- Live SSO sessions for both profiles.
- No instance in the ephemeral account is tagged
  `Space=gone.sbx.ikigenba.dev`.

Postconditions:

- Nothing has changed. Nothing was copied.

## A developer restores from a space that has no backups

An account whose periods are all `0`, and whose apps declare no database,
writes no backups, so a space there can be a target but never a source; its
prefix is simply empty.

Command:

```
$ devctl --account 602773793009 restore foo.sbx.ikigenba.dev crm --from bar.sbx.ikigenba.dev
```

Output:

```
devctl: no backups under sbx-ikigenba-dev-602773793009/bar.sbx.ikigenba.dev/crm/
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- A live SSO session for the profile named by `--account`.
- The target space exists.
- No object under `bar.sbx.ikigenba.dev/crm/` in the account's backup bucket.

Postconditions:

- Nothing has changed. The target's staging prefix was not emptied and not
  written: nothing is cleared until there is something to put in its place.

## A developer's restore fails on the host

`opsctl`'s own report follows the error line. It is a report of what the host
found, so opsctl wrote it to its stdout; devctl relays it as the detail of a
failure rather than as its own product.

Command:

```
$ devctl --account 602773793009 restore foo.sbx.ikigenba.dev crm --from ikigenba.dev --from-account 295229566359
```

Output:

```
source: ok (ikigenba-dev-295229566359/ikigenba.dev/crm/, newest 2026-09-12T03:00:04Z)
copy: ok (9 objects -> sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/restore/crm/)
devctl: restore: ssh ec2-user@3.19.79.227 sudo opsctl restore crm --prefix s3://sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/restore/crm/: exit status 1

source: ok (restore/crm/2026-09-12T03:00:04Z.tar.zst, 12.4 MiB)
stop: ok (ikigenba-crm.service, litestream.service)
files: ok (/opt/crm/etc, /opt/crm/state, 12 files)
db: failed: litestream restore: no snapshot under the prefix
start: warning (ikigenba-crm.service, litestream.service left stopped)
```

Exits 1. devctl's own `source:` and `copy:` lines are on stdout; the error
line and the relayed report are on stderr.

Preconditions:

- Live SSO sessions for both profiles.
- The target space exists, its instance is `running`, and `opsctl` is
  installed on it.
- Backups for `crm` exist under `ikigenba.dev/crm/` in the durable account's
  backup bucket.
- The developer's ssh configuration can reach the target instance as
  `ec2-user`.
- `opsctl restore crm --prefix ...` on the target host exits non-zero.

Postconditions:

- The copied set is under the target's staging prefix in the bucket.
- The target's `etc/`, `state/`, and database for `crm` are whatever `opsctl`
  left. The target's own backup prefix is untouched either way.
