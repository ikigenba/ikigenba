# Stories — bootstrap

Running events at all: help, version, the manifest, the state of its database, exit codes. events is an app of the platform, the suite's internal event bus: one Go binary that takes the events every service emits to its socket at `/emit` (`S07`), checks each against what its producer declared (`S06`), keeps them in a retained log in its own SQLite database (`S13`), and delivers each, in order, to every service that accepts it (`S10`, `S11`); it offers five MCP tools at `/mcp` to read the catalog of events, search the log, and watch, skip and resume subscribers (`S05`, `S08` to `S12`), and serves a landing page at `/` that lists its subscribers (`S03`). On a host it runs as `/opt/events/bin/events` with `/opt/events` as its working directory and its environment read from `/opt/events/etc/env`; a developer runs the same binary from the checkout. With no command it serves (`S02`); the commands here are what the build, and an operator, ask of it. They serve nothing, take no event, deliver none, and record nothing in the trail: the trail is what events records while it serves (`S02`, `S14`). events keeps its log and each subscriber's place in it in its database, the SQLite file `state/events.db` under its working directory, which it creates and brings up to date when it starts (`S02`). The database's schema is a sequence of numbered migrations, each with a four-digit version, that the binary carries and applies in order; this events carries one, version `0001`. Every diagnostic events writes begins `events: `, and a usage error exits 2.

## A developer asks which version they have

events carries no version of its own: nothing in its source or its build names one. The environment tells it which code it is running, through two variables: `IKIGENBA_COMMIT`, the commit it was built from, and `IKIGENBA_RELEASE`, the label of the release, when there is one. From them events builds its display string, `<display>`, which every later story uses with this meaning. With both set it is the label, one space, and the short commit in parentheses, `<label> (<short sha>)`; with only the commit it is the short commit alone; with only the label it is the label alone. The short commit is the first seven characters of `IKIGENBA_COMMIT`, or all of it when it is shorter; a value ending in `-dirty`, as a developer's sandbox marks a modified tree, is shortened without the suffix and keeps it after, as in `<short sha>-dirty`. Nothing else about either value is checked or changed. events reads the two variables each time it is run with `--version`; an events that serves reads them once, when it starts (`S02`), and shows that string wherever it shows a version: the landing page's footer and the about screen (`S03`), the MCP endpoint's `serverInfo` (`S05`), and its own `service.started` event in the trail (`S02`).

Command:

```
$ events --version
```

Output:

```
<display>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/events` exists, built from the checkout with `make`.
- `IKIGENBA_COMMIT` holds a commit, and `IKIGENBA_RELEASE` holds a label or is unset; `<display>` is the string they make.

Postconditions:

- Nothing has changed.

## A developer asks which version they have with no identity set

With neither variable set, or both empty, events has no identity to show, and `<display>` is the empty string. It still answers, with an empty line, and still succeeds: a missing identity is not a failure.

Command:

```
$ events --version
```

Output:

```

```

Exits 0. stdout holds one empty line, a single newline and nothing else; stderr is empty.

Preconditions:

- `bin/events` exists.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed `etc/manifest.toml` is a copy kept so the checkout can be read without a build; the two are byte-identical, and `devctl build` refuses an app where they differ. events declares its name; its description, the one line that says what events is for, which the host publishes in its services file, the about screen shows (`S03`), and events' MCP endpoint gives its clients as instructions (`S05`); that it is not the host's default app; that it is one of the suite's MCP services (`mcp = true`), so the gateway lists it and runs its tools (`S05`); no secrets; its six settings, which the host writes into its `etc/env` and which it reads when it serves (`S02`): `EVENTS_DEPTH_MAX`, the deepest an event's chain of causes may go, 8 (`S07`), `EVENTS_DELIVERY_TIMEOUT_SECONDS`, how long one delivery may take, 5 seconds (`S11`), `EVENTS_DELIVERY_ATTEMPTS`, how many times one event is tried before its subscriber is paused, 10 (`S11`), `EVENTS_INFLIGHT_MAX`, how many deliveries may be in flight at once, 4 (`S11`), `EVENTS_RETENTION_DAYS`, how long an event stays in the log, 2 days (`S13`), and `EVENTS_DECLARATIONS_SECONDS`, how often events asks the services again what they emit and accept, 60 seconds (`S06`); and its SQLite database, the log, which the host replicates like auth's. It declares no `[resources]` table, so the host bounds events no more tightly than any service that declares none. It does not declare `guests`: every page and `/mcp` is for a signed-in user, so on a host with an authenticator nginx sends a visitor with no credential to sign in before they reach a page (`S03`) and challenges one at `/mcp` (`S05`). It declares no port: events serves on the socket the host passes it (`S02`), and a manifest carrying `port` is refused by `devctl build` and by opsctl.

Command:

```
$ events manifest
```

Output:

```
app = "events"
description = "The suite's internal event bus"
default = false
mcp = true
secrets = []

[env]
EVENTS_DEPTH_MAX = "8"
EVENTS_DELIVERY_TIMEOUT_SECONDS = "5"
EVENTS_DELIVERY_ATTEMPTS = "10"
EVENTS_INFLIGHT_MAX = "4"
EVENTS_RETENTION_DAYS = "2"
EVENTS_DECLARATIONS_SECONDS = "60"

[database]
engine = "sqlite"
path = "state/events.db"
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/events` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what events can do

Command:

```
$ events --help
```

Output:

```
Usage: events [command]

Serve the suite's internal event bus: emit at /emit, MCP tools at /mcp, and a
landing page at /, on the socket systemd passes in. With no command, serve.

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

- `bin/events` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ events bogus
```

Output:

```
events: unknown command 'bogus'

see 'events --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/events` exists.

Postconditions:

- Nothing has changed.

## A developer gives an option events does not have

An unknown option is a usage error like an unknown command, and is named back the same way.

Command:

```
$ events --bogus
```

Output:

```
events: unknown option '--bogus'

see 'events --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/events` exists.

Postconditions:

- Nothing has changed.

## An operator checks a database that is up to date

After a deploy, or before one, an operator wants to see the schema the binary expects beside what the database holds: which of the binary's migrations the database has had applied, and when. `events db status` prints one line for each version, in ascending order: `<version> applied <applied-at>` for a migration the database has had applied, where `<applied-at>` is when it was applied, in UTC to the microsecond; `<version> pending` for one the binary carries that the database has not had applied; and `<version> unknown <applied-at>` for one the database records that the binary does not carry. It reads `state/events.db` relative to its working directory and only looks: it applies nothing and changes nothing. Like `manifest`, it needs no socket and reads no environment.

Command:

```
$ events db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
```

Exits 0. The line is on stdout; stderr is empty. The time is the one the database records for version `0001`.

Preconditions:

- `bin/events` exists.
- The working directory holds `state/events.db`, which an events carrying the same migrations created or brought up to date, applying version `0001` at `2026-10-05T14:03:07.123456Z`. On a host the working directory is `/opt/events` and the operator is a user who can read the database.

Postconditions:

- Nothing has changed.

## An operator checks before the database exists

Before events has first started there is no database. `events db status` then reports every migration the binary carries as pending, which is what the next start will apply, and creates nothing: neither `state/` nor `state/events.db`.

Command:

```
$ events db status
```

Output:

```
0001 pending
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/events` exists.
- `state/events.db` does not exist in the working directory; `state/` may be absent too.

Postconditions:

- Nothing has changed. No file or directory was created.

## An operator checks a database a newer events has upgraded

A newer events has applied a migration this binary does not carry, as when a deploy is rolled back to an older binary over a database the newer one upgraded. Older code runs on newer data: this events still serves such a database, applying nothing to it and warning that it is ahead (`S02`). `events db status` shows what the database holds: it prints every line as usual, the version it does not know among them as `unknown`, and succeeds. A database ahead of the binary is a state to report, not a failure.

Command:

```
$ events db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
0002 unknown 2026-10-06T09:12:44.000017Z
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `bin/events` exists, carrying only migration `0001`.
- The working directory holds `state/events.db`, which records version `0001` applied at `2026-10-05T14:03:07.123456Z` and version `0002` applied at `2026-10-06T09:12:44.000017Z`.

Postconditions:

- Nothing has changed.

## An operator checks a database that cannot be read

`state/events.db` exists but is not a database events can read: its contents are not a SQLite database, or it is a directory, or the operator cannot read it. events prints no lines, since it cannot tell what the database holds, and says why on stderr.

Command:

```
$ events db status
```

Output:

```
events: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying failure.

Preconditions:

- `bin/events` exists.
- The working directory holds `state/events.db`, a file whose contents are not a SQLite database.

Postconditions:

- Nothing has changed.

## An operator mistypes the database command

`db` takes exactly one command, `status`, and `status` takes no arguments. The argument events complains about is the first one it does not take: `events db` alone names `db`, which is not a command by itself; `events db bogus` names `bogus`; and `events db status extra` names `extra`.

Command:

```
$ events db bogus
```

Output:

```
events: unknown command 'bogus'

see 'events --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. `events db` fails the same way with `events: unknown command 'db'`, and `events db status extra` with `events: unknown command 'extra'`.

Preconditions:

- `bin/events` exists.

Postconditions:

- Nothing has changed. No database was read, and no file or directory was created.
