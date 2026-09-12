# Stories — restore

A fresh space starts from seed. When a developer wants a space to hold what
another space held, the only channel is that other space's backups in the
account's backup bucket. Restore copies one app's newest backup set from the
source space's prefix to the target space's prefix and has `opsctl` on the
target host restore from it. Nothing is ever read from a running host, and no
host can read another space's prefix; the developer's identity does the copy.

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

Copy <app>'s newest backup set from <from domain>'s prefix to <domain>'s prefix
in the backup bucket, then have opsctl on <domain> restore <app> from it.

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
`<from domain>/<app>/` in its account's backup bucket need exist. A backup set
is what `opsctl` writes there: the newest full backup and every incremental
and WAL segment after it. The exact layout is opsctl's; devctl copies the set
whole.

Command:

```
$ devctl --account 602773793009 restore foo.sbx.ikigenba.dev crm --from ikigenba.dev --from-account 295229566359
```

Output:

```
source: ok (ikigenba-dev-295229566359/ikigenba.dev/crm/, newest 2026-09-12T03:00:04Z)
copy: ok (9 objects -> sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/crm/)
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

- The newest backup set for `crm` from `ikigenba.dev/crm/` is copied, object
  for object, to `foo.sbx.ikigenba.dev/crm/` in the ephemeral account's
  bucket. The source prefix is untouched.
- `sudo opsctl restore crm` has been run on the target host over ssh and has
  exited 0, so `crm`'s `state/` on the target is the source's data. `opsctl`
  alone decides what a restore does on the host; devctl runs it and reports
  its exit.

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
copy: ok (9 objects -> ikigenba-dev-295229566359/staging.ikigenba.dev/crm/)
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

- The newest backup set is copied to `staging.ikigenba.dev/crm/` in the same
  bucket; the source prefix is untouched.
- `sudo opsctl restore crm` has been run on the target host and exited 0.

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

An account whose three backup periods are all `0` writes no backups, so a
space there can be a target but never a source; its prefix is simply empty.

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

- Nothing has changed.

## A developer's restore fails on the host

`opsctl`'s output follows the error line.

Command:

```
$ devctl --account 602773793009 restore foo.sbx.ikigenba.dev crm --from ikigenba.dev --from-account 295229566359
```

Output:

```
source: ok (ikigenba-dev-295229566359/ikigenba.dev/crm/, newest 2026-09-12T03:00:04Z)
copy: ok (9 objects -> sbx-ikigenba-dev-602773793009/foo.sbx.ikigenba.dev/crm/)
devctl: restore: ssh ec2-user@3.19.79.227 sudo opsctl restore crm: exit status 1

opsctl: crm: base backup checksum mismatch
```

Exits 1. The `ok` lines are on stdout; the rest is on stderr.

Preconditions:

- Live SSO sessions for both profiles.
- The target space exists, its instance is `running`, and `opsctl` is
  installed on it.
- Backups for `crm` exist under `ikigenba.dev/crm/` in the durable account's
  backup bucket.
- The developer's ssh configuration can reach the target instance as
  `ec2-user`.
- `opsctl restore crm` on the target host exits non-zero.

Postconditions:

- The copied set is under the target prefix in the bucket.
- The target's `state/` for `crm` is whatever `opsctl` left.
