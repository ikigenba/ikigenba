# Stories — bootstrap

Running auth at all: help, version, the manifest, the state of its database,
exit codes. auth is an app of the platform: one Go binary that serves the auth
service. On a host it runs as `/opt/auth/bin/auth` with `/opt/auth` as its
working directory and its environment read from `/opt/auth/etc/env`; a
developer runs the same binary from the checkout. With no command it serves
(`S02-serve.md`); the commands here are what the build, and an operator, ask of
it. They serve nothing and record no event: the trail of events is what auth
records while it serves (`S02-serve.md`). auth keeps its users, sessions,
sign-ins in flight and tokens in its database, the SQLite file `state/auth.db`
under its working directory, which it creates and brings up to date when it
starts (`S02-serve.md`). The database's schema is a sequence of numbered
migrations, each with a four-digit version, that the binary carries and
applies in order; this auth carries three, versions `0001`, `0002` and
`0003`.

## A developer asks which version they have

auth carries no version of its own: nothing in its source or its build names
one. The environment tells it which code it is running, through two
variables: `IKIGENBA_COMMIT`, the commit it was built from, and
`IKIGENBA_RELEASE`, the label of the release, when there is one. From them
auth builds its display string, `<display>`, which every later story uses
with this meaning. With both set it is the label, one space, and the short
commit in parentheses, `<label> (<short sha>)`; with only the commit it is the
short commit alone; with only the label it is the label alone. The short
commit is the first seven characters of `IKIGENBA_COMMIT`, or all of it when
it is shorter; a value ending in `-dirty`, as a developer's sandbox marks a
modified tree, is shortened without the suffix and keeps it after, as in
`<short sha>-dirty`. Nothing else about either value is checked or changed.
auth reads the two variables each time it is run with `--version`; an auth
that serves reads them once, when it starts, and records the string as the
`version` of its `service.started` (`S02-serve.md`), so the trail names the
code that was running.

Command:

```
$ auth --version
```

Output:

```
<display>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/auth` exists, built from the checkout with `make`.
- `IKIGENBA_COMMIT` holds a commit, and `IKIGENBA_RELEASE` holds a label or
  is unset; `<display>` is the string they make.

Postconditions:

- Nothing has changed.

## A developer asks which version they have with no identity set

With neither variable set, or both empty, auth has no identity to show, and
`<display>` is the empty string. It still answers, with an empty line, and
still succeeds: a missing identity is not a failure.

Command:

```
$ auth --version
```

Output:

```

```

Exits 0. stdout holds one empty line, a single newline and nothing else;
stderr is empty.

Preconditions:

- `bin/auth` exists.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed
`etc/manifest.toml` is a copy kept so the checkout can be read without a build;
the two are byte-identical, and `devctl build` refuses an app where they
differ. auth declares its name, that it is not the host's default app, the
secrets it needs, its Workspace domain, and its SQLite database, at
`state/auth.db` under its working directory, which the host replicates. Its
`[resources]` table places it among the platform's core services, which keep
a small memory reserve ahead of other apps, and caps its memory at 128M. It
declares no port: auth serves on the socket the host passes it
(`S02-serve.md`), and a manifest carrying `port` is refused by `devctl build`
and by opsctl.

Command:

```
$ auth manifest
```

Output:

```
app = "auth"
default = false
secrets = ["GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"]

[env]
WORKSPACE_DOMAIN = "michaelgreenly.dev"

[database]
engine = "sqlite"
path = "state/auth.db"

[resources]
slice = "core"
memory_max = "128M"
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/auth` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what auth can do

Command:

```
$ auth --help
```

Output:

```
Usage: auth [command]

Serve the auth service on the socket systemd passes in. With no command,
serve.

Commands:
  manifest    print the app manifest
  db status   print applied and pending migrations

Options:
  --help      print this help
  --version   print the version

Exit codes:
  0  success
  1  failure
  2  usage error
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/auth` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ auth bogus
```

Output:

```
auth: unknown command 'bogus'

see 'auth --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. An unknown option, `--bogus`
say, fails the same way with `auth: unknown option '--bogus'`.

Preconditions:

- `bin/auth` exists.

Postconditions:

- Nothing has changed.

## An operator checks a database that is up to date

After a deploy, or before one, an operator wants to see the schema the binary
expects beside what the database holds: which of the binary's migrations the
database has had applied, and when. `auth db status` prints one line for each
version, in ascending order: `<version> applied <applied-at>` for a migration
the database has had applied, where `<applied-at>` is when it was applied, in
UTC to the microsecond; `<version> pending` for one the binary carries that
the database has not had applied; and `<version> unknown <applied-at>` for one
the database records that the binary does not carry. It reads `state/auth.db`
relative to its working directory and only looks: it applies nothing and
changes nothing. Like `manifest`, it needs no socket and reads no
environment, so it needs none of the Google settings.

Command:

```
$ auth db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
0002 applied 2026-10-05T14:03:07.125003Z
0003 applied 2026-10-05T14:03:07.126518Z
```

Exits 0. The lines are on stdout; stderr is empty. Each time is the one the
database records for that version.

Preconditions:

- `bin/auth` exists.
- The working directory holds `state/auth.db`, which an auth carrying the same
  migrations created or brought up to date, applying version `0001` at
  `2026-10-05T14:03:07.123456Z`, version `0002` at
  `2026-10-05T14:03:07.125003Z` and version `0003` at
  `2026-10-05T14:03:07.126518Z`. On a host the working directory is
  `/opt/auth` and the operator is a user who can read the database.

Postconditions:

- Nothing has changed.

## An operator checks before the database exists

Before auth has first started there is no database. `auth db status` then
reports every migration the binary carries as pending, which is what the next
start will apply, and creates nothing: neither `state/` nor `state/auth.db`.

Command:

```
$ auth db status
```

Output:

```
0001 pending
0002 pending
0003 pending
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `bin/auth` exists.
- `state/auth.db` does not exist in the working directory; `state/` may be
  absent too.

Postconditions:

- Nothing has changed. No file or directory was created.

## An operator checks a database an earlier auth wrote

An auth from before the migrations kept its database without recording any
migration. Until this auth first starts on it, `auth db status` reports all
three of the binary's migrations as pending: the next start applies them,
`0001` changing nothing that is already there, `0002` giving the tokens the
ids they now carry, and `0003` making each token a personal token, changing
nothing else about it (`S05-tokens.md`).

Command:

```
$ auth db status
```

Output:

```
0001 pending
0002 pending
0003 pending
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `bin/auth` exists.
- The working directory holds `state/auth.db`, written by an auth from before
  the migrations, which records no migration.

Postconditions:

- Nothing has changed. The database was not brought up to date.

## An operator checks a database a newer auth has upgraded

A newer auth has applied a migration this binary does not carry, as when a
deploy is rolled back to an older binary over a database the newer one
upgraded. Older code runs on newer data: this auth still serves such a
database, applying nothing to it and warning that it is ahead
(`S02-serve.md`). `auth db status` shows what the database holds: it prints
every line as usual, the version it does not know among them as `unknown`,
and succeeds. A database ahead of the binary is a state to report, not a
failure.

Command:

```
$ auth db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
0002 applied 2026-10-05T14:03:07.125003Z
0003 applied 2026-10-05T14:03:07.126518Z
0004 unknown 2026-10-06T09:12:44.000017Z
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `bin/auth` exists, carrying migrations `0001`, `0002` and `0003` only.
- The working directory holds `state/auth.db`, which records version `0001`
  applied at `2026-10-05T14:03:07.123456Z`, version `0002` applied at
  `2026-10-05T14:03:07.125003Z`, version `0003` applied at
  `2026-10-05T14:03:07.126518Z`, and version `0004` applied at
  `2026-10-06T09:12:44.000017Z`.

Postconditions:

- Nothing has changed.

## An operator checks a database that cannot be read

`state/auth.db` exists but is not a database auth can read: its contents are
not a SQLite database, or it is a directory, or the operator cannot read it.
auth prints no lines, since it cannot tell what the database holds, and says
why on stderr.

Command:

```
$ auth db status
```

Output:

```
auth: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying
failure.

Preconditions:

- `bin/auth` exists.
- The working directory holds `state/auth.db`, a file whose contents are not a
  SQLite database.

Postconditions:

- Nothing has changed.

## An operator mistypes the database command

`db` takes exactly one command, `status`, and `status` takes no arguments. The
argument auth complains about is the first one it does not take: `auth db`
alone names `db`, which is not a command by itself; `auth db bogus` names
`bogus`; and `auth db status extra` names `extra`.

Command:

```
$ auth db bogus
```

Output:

```
auth: unknown command 'bogus'

see 'auth --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. `auth db` fails the same way
with `auth: unknown command 'db'`, and `auth db status extra` with
`auth: unknown command 'extra'`.

Preconditions:

- `bin/auth` exists.

Postconditions:

- Nothing has changed. No database was read, and no file or directory was
  created.
