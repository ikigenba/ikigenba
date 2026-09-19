# Stories — restore

A space's backups belong to that space. `opsctl` on the host writes them to
the space's own prefix in the bucket, `s3://ikigenba.dev/<label>/`, and reads
them back from there, and nothing else ever reads or writes that prefix.
`devctl restore` drives that from the developer's machine: it runs `opsctl
restore` on the space over ssh and reports its exit, exactly as `deploy` runs
`opsctl install`. No object moves, and nothing travels over the ssh
connection. `<space>` is the space's label or its full domain, as everywhere
(see `S2-space-lifecycle.md`).

A restore is per app. What it puts back is the app's `etc/` and `state/` from
the newest tarball and, when the app declares a `[database]`, that database
from litestream. `--at` asks for an earlier moment instead of the newest. What
a restore does on the host is `opsctl`'s; devctl runs it and reports its exit.

## A developer asks what `restore` can do

The top-level usage gains the line `  restore   put a space's app back from
its backups` under `Commands:`.

Command:

```
$ devctl restore --help
```

Output:

```
Usage: devctl restore <space> <app> [--at <timestamp>]

Have opsctl on the space put <app> back from the space's own backups. The
app's etc/ and state/ come from the newest tarball, and its database, when it
declares one, from litestream. <app>'s unit is stopped for the restore and
started again after it.

Options:
  --at <timestamp>   restore the app as it was at this RFC 3339 moment

--at governs both halves: the files come from the newest tarball written at or
before that moment, and the database is rebuilt to the moment itself.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer puts a space's app back

The one line of output is opsctl's exit, the same shape `deploy` uses for the
step it runs on the host. What opsctl printed is not relayed: it succeeded.

Command:

```
$ devctl restore sbx1 crm
```

Output:

```
restore: ok (opsctl restore crm)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists, its instance is `running`, and `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.

Postconditions:

- `sudo opsctl restore crm` has been run on the host over ssh and has exited
  0, so `crm`'s `etc/`, `state/`, and database on that host are what its own
  backups held. `opsctl` alone decides what a restore does on the host.
- No other app on the space has changed, and no other space was read.

## A developer puts a space's app back as it was at a moment

`--at` is passed through unchanged; opsctl decides what it means.

Command:

```
$ devctl restore sbx1 crm --at 2026-09-11T18:00:00Z
```

Output:

```
restore: ok (opsctl restore crm --at 2026-09-11T18:00:00Z)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists, its instance is `running`, and `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `opsctl restore crm --at 2026-09-11T18:00:00Z` on that host exits 0.

Postconditions:

- `crm`'s `etc/` and `state/` are the newest tarball written at or before that
  moment, and its database is what it was at the moment itself.
- No other app on the space has changed.

## A developer restores to a space that does not exist

Command:

```
$ devctl restore gone crm
```

Output:

```
devctl: no space at 'gone.ikigenba.dev'
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- No instance is tagged `Space=gone.ikigenba.dev`.

Postconditions:

- Nothing has changed. No ssh connection was opened.

## A developer runs `restore` without a space or an app

Command:

```
$ devctl restore sbx1
```

Output:

```
devctl: restore needs <space> and <app>

see 'devctl restore --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. An `--at` that is not an
RFC 3339 timestamp gives `devctl: --at takes an RFC 3339 timestamp`.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made and no ssh connection was opened.

## A developer's restore fails on the host

`opsctl`'s output follows the error line, each line quoted with `> ` so it is
plainly the other program's and not devctl's.

Command:

```
$ devctl restore sbx1 crm
```

Output:

```
devctl: restore: ssh ec2-user@18.118.7.42 sudo opsctl restore crm: exit status 1

> opsctl: no backups for crm under s3://ikigenba.dev/sbx1/
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space exists, its instance is `running`, and `opsctl` is installed on
  it.
- The developer's ssh configuration can reach the instance as `ec2-user`.
- `opsctl restore crm` on that host exits non-zero.

Postconditions:

- `crm` on the host is whatever `opsctl` left; `space status` reports it. The
  space's backup prefix was neither written nor deleted from.
- No other app on the space has changed.
