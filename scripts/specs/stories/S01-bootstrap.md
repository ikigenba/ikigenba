# Stories — bootstrap

Running scripts at all: help, version, the manifest, the state of its database, exit codes. scripts is an app of the platform, the suite's script runner: one Go binary that keeps a catalog of each user's scripts, each naming one of their repositories in repos and the ref it runs, runs a script's `main.py` from that ref's commit unpacked into a folder of its own and keeps the run's input, output, files and outcome (`S08`, `S15`), offers eleven MCP tools to list, show, create, update, delete and run scripts, to subscribe them to events and unsubscribe them, and to list, read and cancel their runs at `/mcp` (`S05` to `S11`, `S26`), runs a script when an event it is subscribed to is delivered to it (`S27`), and serves a catalog of the user's scripts at `/` (`S03`) with a page for each script (`S12`) and for each run (`S13`). On a host it runs as `/opt/scripts/bin/scripts` with `/opt/scripts` as its working directory and its environment read from `/opt/scripts/etc/env`; a developer runs the same binary from the checkout. With no command it serves (`S02`); the commands here are what the build, and an operator, ask of it. They serve nothing, run no git and no script, read no repository, and record no event: the trail is what scripts records while it serves (`S02`). scripts keeps its catalog of scripts and the record of every run in its database, the SQLite file `state/scripts.db` under its working directory, which it creates and brings up to date when it starts (`S02`). The database's schema is a sequence of numbered migrations, each with a four-digit version, that the binary carries and applies in order; this scripts carries two, versions `0001` and `0002`.

## A developer asks which version they have

The version is a `var` in the source, never injected at build time, so a developer's build and a deployed binary report the same string. Its shape is `v<semver>`: a `v`, then a semantic version, prerelease and build metadata included. Its value is data and is not fixed here. It is the same version the landing page's footer and the about screen show (`S03`), the MCP endpoint gives as its `serverInfo` (`S05`), and scripts' own `service.started` event carries in the trail (`S02`).

Command:

```
$ scripts --version
```

Output:

```
v<semver>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/scripts` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed `etc/manifest.toml` is a copy kept so the checkout can be read without a build; the two are byte-identical, and `devctl build` refuses an app where they differ. scripts declares its name; its description, the one line that says what scripts is for, which the host publishes in its services file, the about screen shows (`S03`), and scripts' MCP endpoint gives its clients as instructions (`S05`); that it is not the host's default app; that it is one of the suite's MCP services (`mcp = true`), so the gateway lists it and runs its tools (`S05`); no secrets; its seven settings, which the host writes into its `etc/env` and which it reads when it serves (`S02`): `REPOS_DIR`, the directory holding repos' bare repositories, relative to scripts' working directory, so `/opt/scripts/../repos/state/repos` on a host (`S20`), `TREE_MAX_BYTES`, the largest a run's unpacked tree may be, 256 MiB (`S17`), `OUTPUT_MAX_BYTES`, how much of a run's standard output, and separately of its standard error, is kept, 1 MiB (`S17`), `OPERATION_SECONDS`, the longest one git run may take (`S17`), `SCRIPT_SECONDS`, the longest a script may run (`S17`), and `RUN_KEEP_DAYS` and `RUN_KEEP_COUNT`, how many days a run is kept and how many of each script's newest runs are kept whatever their age (`S19`); its SQLite database, the catalog of scripts and the record of every run, which the host replicates like auth's; and how much of the host it may take, in opsctl's `[resources]` table, which opsctl writes into its service unit, so it bounds scripts, every git scripts runs and every script it runs together: `cpu_weight` 50 and `io_weight` 50, half the share of a service that declares none, so unpacking and running scripts yields to the rest of the suite when the host is busy, and `memory_max` `1G`, a ceiling of 1 GiB. It does not declare `guests`: every page and `/mcp` is for a signed-in user, so on a host with an authenticator nginx sends a visitor with no credential to sign in before they reach a page (`S03`) and challenges one at `/mcp` (`S05`). It declares no port: scripts serves on the socket the host passes it (`S02`), and a manifest carrying `port` is refused by `devctl build` and by opsctl. It names no `cache/`: a run's folder is the product, not a cache, and lives under `state/runs/` (`S20`).

Command:

```
$ scripts manifest
```

Output:

```
app = "scripts"
description = "Python scripts run from the suite's repositories"
default = false
mcp = true
secrets = []

[env]
REPOS_DIR = "../repos/state/repos"
TREE_MAX_BYTES = "268435456"
OUTPUT_MAX_BYTES = "1048576"
OPERATION_SECONDS = "600"
SCRIPT_SECONDS = "600"
RUN_KEEP_DAYS = "15"
RUN_KEEP_COUNT = "10"

[database]
engine = "sqlite"
path = "state/scripts.db"

[resources]
cpu_weight = 50
memory_max = "1G"
io_weight = 50
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/scripts` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what scripts can do

Command:

```
$ scripts --help
```

Output:

```
Usage: scripts [command]

Run Python scripts from the suite's repositories, with MCP tools at /mcp
and pages for scripts and their runs at /, on the socket systemd passes in.
With no command, serve.

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

- `bin/scripts` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ scripts bogus
```

Output:

```
scripts: unknown command 'bogus'

see 'scripts --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists.

Postconditions:

- Nothing has changed.

## A developer gives an option scripts does not have

An unknown option is a usage error like an unknown command, and is named back the same way.

Command:

```
$ scripts --bogus
```

Output:

```
scripts: unknown option '--bogus'

see 'scripts --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists.

Postconditions:

- Nothing has changed.

## An operator checks a database that is up to date

After a deploy, or before one, an operator wants to see the schema the binary expects beside what the database holds: which of the binary's migrations the database has had applied, and when. `scripts db status` prints one line for each version, in ascending order: `<version> applied <applied-at>` for a migration the database has had applied, where `<applied-at>` is when it was applied, in UTC to the microsecond; `<version> pending` for one the binary carries that the database has not had applied; and `<version> unknown <applied-at>` for one the database records that the binary does not carry. It reads `state/scripts.db` relative to its working directory and only looks: it applies nothing and changes nothing. Like `manifest` and `--version`, it needs no socket, reads no environment, and needs neither `git` nor `python3.12` on the `PATH`.

Command:

```
$ scripts db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
0002 applied 2026-10-05T14:03:07.124031Z
```

Exits 0. The lines are on stdout; stderr is empty. Each time is the one the database records for that version.

Preconditions:

- `bin/scripts` exists.
- The working directory holds `state/scripts.db`, which a scripts of this version created or brought up to date, applying version `0001` at `2026-10-05T14:03:07.123456Z` and version `0002` at `2026-10-05T14:03:07.124031Z`. On a host the working directory is `/opt/scripts` and the operator is a user who can read the database.

Postconditions:

- Nothing has changed.

## An operator checks before the database exists

Before scripts has first started there is no database. `scripts db status` then reports every migration the binary carries as pending, which is what the next start will apply, and creates nothing: neither `state/` nor `state/scripts.db`.

Command:

```
$ scripts db status
```

Output:

```
0001 pending
0002 pending
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `bin/scripts` exists.
- `state/scripts.db` does not exist in the working directory; `state/` may be absent too.

Postconditions:

- Nothing has changed. No file or directory was created.

## An operator checks a database a newer scripts has upgraded

A newer scripts has applied a migration this binary does not carry, as when a deploy is rolled back to an older binary over a database the newer one upgraded. This scripts cannot serve such a database (`S02`), and `scripts db status` shows why: it prints every line as usual, the version it does not know among them as `unknown`, then says so on stderr and fails.

Command:

```
$ scripts db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
0002 applied 2026-10-05T14:03:07.124031Z
0003 unknown 2026-10-06T09:12:44.000017Z
scripts: <reason>
```

Exits 1. The `0001`, `0002` and `0003` lines are on stdout; the last line is on stderr. `<reason>` names the unknown version, `0003`.

Preconditions:

- `bin/scripts` exists, carrying only migrations `0001` and `0002`.
- The working directory holds `state/scripts.db`, which records version `0001` applied at `2026-10-05T14:03:07.123456Z`, version `0002` applied at `2026-10-05T14:03:07.124031Z`, and version `0003` applied at `2026-10-06T09:12:44.000017Z`.

Postconditions:

- Nothing has changed.

## An operator checks a database that cannot be read

`state/scripts.db` exists but is not a database scripts can read: its contents are not a SQLite database, or it is a directory, or the operator cannot read it. scripts prints no lines, since it cannot tell what the database holds, and says why on stderr.

Command:

```
$ scripts db status
```

Output:

```
scripts: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying failure.

Preconditions:

- `bin/scripts` exists.
- The working directory holds `state/scripts.db`, a file whose contents are not a SQLite database.

Postconditions:

- Nothing has changed.

## An operator mistypes the database command

`db` takes exactly one command, `status`, and `status` takes no arguments. The argument scripts complains about is the first one it does not take: `scripts db` alone names `db`, which is not a command by itself; `scripts db bogus` names `bogus`; and `scripts db status extra` names `extra`.

Command:

```
$ scripts db bogus
```

Output:

```
scripts: unknown command 'bogus'

see 'scripts --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. `scripts db` fails the same way with `scripts: unknown command 'db'`, and `scripts db status extra` with `scripts: unknown command 'extra'`.

Preconditions:

- `bin/scripts` exists.

Postconditions:

- Nothing has changed. No database was read, and no file or directory was created.
