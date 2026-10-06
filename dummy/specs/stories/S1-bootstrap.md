# Stories — bootstrap

Running dummy at all: help, version, the manifest, the state of its database,
exit codes. dummy is an app of the platform: one Go binary that serves a
control panel, and offers the same widgets to MCP clients at `/mcp`
(`S9-mcp.md`). On a host it runs as `/opt/dummy/bin/dummy` with `/opt/dummy`
as its working directory and its environment read from `/opt/dummy/etc/env`; a
developer runs the same binary from the checkout. With no command it serves
(`S2-serve.md`); the commands here are what the build, and an operator, ask of
it. dummy keeps its widgets in its database, the SQLite file `state/dummy.db`
under its working directory, which it creates and brings up to date when it
starts (`S2-serve.md`). The database's schema is a sequence of numbered
migrations, each with a four-digit version, that the binary carries and
applies in order; this dummy carries one, version `0001`.

## A developer asks which version they have

The version is a `var` in the source, never injected at build time, so a
developer's build and a deployed binary report the same string. Its shape is
`v<semver>`: a `v`, then a semantic version, prerelease and build metadata
included. Its value is data and is not fixed here.

Command:

```
$ dummy --version
```

Output:

```
v<semver>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/dummy` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed
`etc/manifest.toml` is a copy kept so the checkout can be read without a
build; the two are byte-identical, and `devctl build` refuses an app where
they differ. dummy declares its name; its description, the one line that says
what dummy is for, which the host publishes in its services file and which
dummy's MCP endpoint gives its clients as instructions (`S9-mcp.md`); that it
is not the host's default app; that it offers an MCP endpoint, at `/mcp`, so
the platform's MCP gateway may reach it (`S9-mcp.md`); no secrets; and its
SQLite database, at `state/dummy.db` under its working directory, which the
host replicates, in that order. Its `[resources]` table caps its memory at
64M. It declares no port: dummy serves on the socket the host passes it
(`S2-serve.md`), and a manifest carrying `port` is refused by `devctl build`
and by opsctl.

Command:

```
$ dummy manifest
```

Output:

```
app = "dummy"
description = "Demo widgets to list and create"
default = false
mcp = true
secrets = []

[database]
engine = "sqlite"
path = "state/dummy.db"

[resources]
memory_max = "64M"
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/dummy` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what dummy can do

Command:

```
$ dummy --help
```

Output:

```
Usage: dummy [command]

Serve the dummy control panel, and its MCP tools at /mcp, on the socket
systemd passes in. With no command, serve.

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

- `bin/dummy` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ dummy bogus
```

Output:

```
dummy: unknown command 'bogus'

see 'dummy --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. An unknown option, `--bogus`
say, fails the same way with `dummy: unknown option '--bogus'`.

Preconditions:

- `bin/dummy` exists.

Postconditions:

- Nothing has changed.

## An operator checks a database that is up to date

After a deploy, or before one, an operator wants to see the schema the binary
expects beside what the database holds: which of the binary's migrations the
database has had applied, and when. `dummy db status` prints one line for each
version, in ascending order: `<version> applied <applied-at>` for a migration
the database has had applied, where `<applied-at>` is when it was applied, in
UTC to the microsecond; `<version> pending` for one the binary carries that
the database has not had applied; and `<version> unknown <applied-at>` for one
the database records that the binary does not carry. It reads
`state/dummy.db` relative to its working directory and only looks: it applies
nothing and changes nothing. Like `manifest` and `--version`, it needs no
socket and reads no environment.

Command:

```
$ dummy db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
```

Exits 0. The line is on stdout; stderr is empty. The time is the one the
database records for version `0001`.

Preconditions:

- `bin/dummy` exists.
- The working directory holds `state/dummy.db`, which a dummy of this version
  created or brought up to date, applying version `0001` at
  `2026-10-05T14:03:07.123456Z`. On a host the working directory is
  `/opt/dummy` and the operator is a user who can read the database.

Postconditions:

- Nothing has changed.

## An operator checks before the database exists

Before dummy has first started there is no database. `dummy db status` then
reports every migration the binary carries as pending, which is what the next
start will apply, and creates nothing: neither `state/` nor `state/dummy.db`.

Command:

```
$ dummy db status
```

Output:

```
0001 pending
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/dummy` exists.
- `state/dummy.db` does not exist in the working directory; `state/` may be
  absent too.

Postconditions:

- Nothing has changed. No file or directory was created.

## An operator checks a database a newer dummy has upgraded

A newer dummy has applied a migration this binary does not carry, as when a
deploy is rolled back to an older binary over a database the newer one
upgraded. This dummy cannot serve such a database (`S2-serve.md`), and
`dummy db status` shows why: it prints every line as usual, the version it
does not know among them as `unknown`, then says so on stderr and fails.

Command:

```
$ dummy db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
0002 unknown 2026-10-06T09:12:44.000017Z
dummy: <reason>
```

Exits 1. The `0001` and `0002` lines are on stdout; the last line is on
stderr. `<reason>` names the unknown version, `0002`.

Preconditions:

- `bin/dummy` exists, carrying only migration `0001`.
- The working directory holds `state/dummy.db`, which records version `0001`
  applied at `2026-10-05T14:03:07.123456Z` and version `0002` applied at
  `2026-10-06T09:12:44.000017Z`.

Postconditions:

- Nothing has changed.

## An operator checks a database that cannot be read

`state/dummy.db` exists but is not a database dummy can read: its contents are
not a SQLite database, or it is a directory, or the operator cannot read it.
dummy prints no lines, since it cannot tell what the database holds, and says
why on stderr.

Command:

```
$ dummy db status
```

Output:

```
dummy: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the
underlying failure.

Preconditions:

- `bin/dummy` exists.
- The working directory holds `state/dummy.db`, a file whose contents are not
  a SQLite database.

Postconditions:

- Nothing has changed.

## An operator mistypes the database command

`db` takes exactly one command, `status`, and `status` takes no arguments. The
argument dummy complains about is the first one it does not take: `dummy db`
alone names `db`, which is not a command by itself; `dummy db bogus` names
`bogus`; and `dummy db status extra` names `extra`.

Command:

```
$ dummy db bogus
```

Output:

```
dummy: unknown command 'bogus'

see 'dummy --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. `dummy db` fails the same way
with `dummy: unknown command 'db'`, and `dummy db status extra` with
`dummy: unknown command 'extra'`.

Preconditions:

- `bin/dummy` exists.

Postconditions:

- Nothing has changed. No database was read, and no file or directory was
  created.
