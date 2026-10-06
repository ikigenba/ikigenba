# Stories — bootstrap

Running telemetry at all: help, version, the manifest, the state of its database, exit codes. telemetry is an app of the platform, the suite's trail of events: one Go binary that takes the events every service on the host posts to its socket at `/ingest` (`S06`), keeps them in its own SQLite database for a retention window (`S07`), offers four read-only MCP tools over them at `/mcp` (`S05`, `S08` to `S11`), and serves a landing page at `/` that says what it is (`S03`). On a host it runs as `/opt/telemetry/bin/telemetry` with `/opt/telemetry` as its working directory and its environment read from `/opt/telemetry/etc/env`; a developer runs the same binary from the checkout. With no command it serves (`S02`); the commands here are what the build, and an operator, ask of it. They serve nothing and record no event: the trail is what telemetry records while it serves (`S02`, `S12`). telemetry keeps its trail in its database, the SQLite file `state/telemetry.db` under its working directory, which it creates and brings up to date when it starts (`S02`). The database's schema is a sequence of numbered migrations, each with a four-digit version, that the binary carries and applies in order; this telemetry carries one, version `0001`.

## A developer asks which version they have

The version is a `var` in the source, never injected at build time, so a developer's build and a deployed binary report the same string. Its shape is `v<semver>`: a `v`, then a semantic version, prerelease and build metadata included. Its value is data and is not fixed here. It is the same version the landing page's footer and the about screen show (`S03`) and telemetry's own `service.started` event carries in the trail (`S02`).

Command:

```
$ telemetry --version
```

Output:

```
v<semver>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/telemetry` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed `etc/manifest.toml` is a copy kept so the checkout can be read without a build; the two are byte-identical, and `devctl build` refuses an app where they differ. telemetry declares its name; its description, the one line that says what telemetry is for, which the host publishes in its services file and the about screen shows (`S03`); that it is not the host's default app; that it is one of the suite's MCP services (`mcp = true`), so the gateway lists it and runs its tools (`S05`); no secrets; its one setting, `RETENTION_DAYS`, the retention window in days, which the host writes into its `etc/env` and which it reads when it serves (`S02`, `S07`); and its SQLite database, which the host replicates like auth's. It declares no port: telemetry serves on the socket the host passes it (`S02`), and a manifest carrying `port` is refused by `devctl build` and by opsctl.

Command:

```
$ telemetry manifest
```

Output:

```
app = "telemetry"
description = "The suite's trail of events"
default = false
mcp = true
secrets = []

[env]
RETENTION_DAYS = "15"

[database]
engine = "sqlite"
path = "state/telemetry.db"
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/telemetry` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what telemetry can do

Command:

```
$ telemetry --help
```

Output:

```
Usage: telemetry [command]

Serve the suite's trail of events: ingest at /ingest, MCP tools at /mcp, and
a landing page at /, on the socket systemd passes in. With no command, serve.

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

- `bin/telemetry` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ telemetry bogus
```

Output:

```
telemetry: unknown command 'bogus'

see 'telemetry --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. An unknown option, `--bogus` say, fails the same way with `telemetry: unknown option '--bogus'`.

Preconditions:

- `bin/telemetry` exists.

Postconditions:

- Nothing has changed.

## An operator checks a database that is up to date

After a deploy, or before one, an operator wants to see the schema the binary expects beside what the database holds: which of the binary's migrations the database has had applied, and when. `telemetry db status` prints one line for each version, in ascending order: `<version> applied <applied-at>` for a migration the database has had applied, where `<applied-at>` is when it was applied, in UTC to the microsecond; `<version> pending` for one the binary carries that the database has not had applied; and `<version> unknown <applied-at>` for one the database records that the binary does not carry. It reads `state/telemetry.db` relative to its working directory and only looks: it applies nothing, sweeps nothing and changes nothing. Like `manifest` and `--version`, it needs no socket and reads no environment.

Command:

```
$ telemetry db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
```

Exits 0. The line is on stdout; stderr is empty. The time is the one the database records for version `0001`.

Preconditions:

- `bin/telemetry` exists.
- The working directory holds `state/telemetry.db`, which a telemetry of this version created or brought up to date, applying version `0001` at `2026-10-05T14:03:07.123456Z`. On a host the working directory is `/opt/telemetry` and the operator is a user who can read the database.

Postconditions:

- Nothing has changed.

## An operator checks before the database exists

Before telemetry has first started there is no database. `telemetry db status` then reports every migration the binary carries as pending, which is what the next start will apply, and creates nothing: neither `state/` nor `state/telemetry.db`.

Command:

```
$ telemetry db status
```

Output:

```
0001 pending
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/telemetry` exists.
- `state/telemetry.db` does not exist in the working directory; `state/` may be absent too.

Postconditions:

- Nothing has changed. No file or directory was created.

## An operator checks a database an older telemetry created

A telemetry from before telemetry carried migrations created its database and recorded no migration in it. `telemetry db status` reports every migration the binary carries as pending, as for a database that does not exist; the next start applies them and keeps every record the database holds (`S02`).

Command:

```
$ telemetry db status
```

Output:

```
0001 pending
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/telemetry` exists.
- The working directory holds `state/telemetry.db`, created by a telemetry that carried no migrations, holding records of the trail and recording no migration.

Postconditions:

- Nothing has changed.

## An operator checks a database a newer telemetry has upgraded

A newer telemetry has applied a migration this binary does not carry, as when a deploy is rolled back to an older binary over a database the newer one upgraded. This telemetry cannot serve such a database (`S02`), and `telemetry db status` shows why: it prints every line as usual, the version it does not know among them as `unknown`, then says so on stderr and fails.

Command:

```
$ telemetry db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
0002 unknown 2026-10-06T09:12:44.000017Z
telemetry: <reason>
```

Exits 1. The `0001` and `0002` lines are on stdout; the last line is on stderr. `<reason>` names the unknown version, `0002`.

Preconditions:

- `bin/telemetry` exists, carrying only migration `0001`.
- The working directory holds `state/telemetry.db`, which records version `0001` applied at `2026-10-05T14:03:07.123456Z` and version `0002` applied at `2026-10-06T09:12:44.000017Z`.

Postconditions:

- Nothing has changed.

## An operator checks a database that cannot be read

`state/telemetry.db` exists but is not a database telemetry can read: its contents are not a SQLite database, or it is a directory, or the operator cannot read it. telemetry prints no lines, since it cannot tell what the database holds, and says why on stderr.

Command:

```
$ telemetry db status
```

Output:

```
telemetry: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying failure.

Preconditions:

- `bin/telemetry` exists.
- The working directory holds `state/telemetry.db`, a file whose contents are not a SQLite database.

Postconditions:

- Nothing has changed.

## An operator mistypes the database command

`db` takes exactly one command, `status`, and `status` takes no arguments. The argument telemetry complains about is the first one it does not take: `telemetry db` alone names `db`, which is not a command by itself; `telemetry db bogus` names `bogus`; and `telemetry db status extra` names `extra`.

Command:

```
$ telemetry db bogus
```

Output:

```
telemetry: unknown command 'bogus'

see 'telemetry --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. `telemetry db` fails the same way with `telemetry: unknown command 'db'`, and `telemetry db status extra` with `telemetry: unknown command 'extra'`.

Preconditions:

- `bin/telemetry` exists.

Postconditions:

- Nothing has changed. No database was read, and no file or directory was created.
