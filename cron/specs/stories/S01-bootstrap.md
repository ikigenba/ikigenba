# Stories — bootstrap

Running cron at all: help, version, the manifest, the state of its database, exit codes. cron is an app of the platform, the suite's scheduler: one Go binary that keeps triggers, each a slug and a schedule, offers seven MCP tools to list, show, create, update, pause, resume and delete them at `/mcp` (`S05` to `S10`), emits an event on the suite's event bus each time a trigger fires (`S11`), and serves a page of every trigger in the space at `/` (`S03`). On a host it runs as `/opt/cron/bin/cron` with `/opt/cron` as its working directory and its environment read from `/opt/cron/etc/env`; a developer runs the same binary from the checkout. With no command it serves (`S02`); the commands here are what the build, and an operator, ask of it. They serve nothing, fire no trigger, emit nothing to the bus, and record no event: the trail is what cron records while it serves (`S02`, `S12`). cron keeps its triggers in its database, the SQLite file `state/cron.db` under its working directory, which it creates and brings up to date when it starts (`S02`). The database's schema is a sequence of numbered migrations, each with a four-digit version, that the binary carries and applies in order; this cron carries one, version `0001`.

## A developer asks which version they have

cron carries no version of its own: nothing in its source or its build names one. The environment tells it which code it is running, through two variables: `IKIGENBA_COMMIT`, the commit it was built from, and `IKIGENBA_RELEASE`, the label of the release, when there is one. From them cron builds its display string, `<display>`, which every later story uses with this meaning. With both set it is the label, one space, and the short commit in parentheses, `<label> (<short sha>)`; with only the commit it is the short commit alone; with only the label it is the label alone. The short commit is the first seven characters of `IKIGENBA_COMMIT`, or all of it when it is shorter; a value ending in `-dirty`, as a developer's sandbox marks a modified tree, is shortened without the suffix and keeps it after, as in `<short sha>-dirty`. Nothing else about either value is checked or changed. cron reads the two variables each time it is run with `--version`; a cron that serves reads them once, when it starts (`S02`). It is the same string the landing page's footer and the about screen show (`S03`), the MCP endpoint gives as its `serverInfo` (`S05`), and cron's own `service.started` event carries in the trail (`S02`), each under the environment that cron was started with.

Command:

```
$ cron --version
```

Output:

```
<display>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/cron` exists, built from the checkout with `make`.
- `IKIGENBA_COMMIT` holds a commit, and `IKIGENBA_RELEASE` holds a label or is unset; `<display>` is the string they make.

Postconditions:

- Nothing has changed.

## A developer asks which version they have with no identity set

With neither variable set, or both empty, cron has no identity to show, and `<display>` is the empty string. It still answers, with an empty line, and still succeeds: a missing identity is not a failure.

Command:

```
$ cron --version
```

Output:

```

```

Exits 0. stdout holds one empty line, a single newline and nothing else; stderr is empty.

Preconditions:

- `bin/cron` exists.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed `etc/manifest.toml` is a copy kept so the checkout can be read without a build; the two are byte-identical, and `devctl build` refuses an app where they differ. cron declares its name; its description, the one line that says what cron is for, which the host publishes in its services file, the about screen shows (`S03`), and cron's MCP endpoint gives its clients as instructions (`S05`); that it is not the host's default app; that it is one of the suite's MCP services (`mcp = true`), so the gateway lists it and runs its tools (`S05`); that it serves no guests (`guests = false`): every page and `/mcp` is for a signed-in user, so on a host with an authenticator nginx sends a visitor with no credential to sign in before they reach a page (`S03`) and challenges one at `/mcp` (`S05`); no secrets; no settings of its own, so it has no `[env]` table: the one value it reads from its environment beside the services file is the space's `DRAIN_SECONDS`, which a manifest never sets (`S02`); its SQLite database, the triggers, which the host replicates like auth's; and how much of the host it may take, in opsctl's `[resources]` table, which opsctl writes into its service unit (opsctl's `S7-apps.md`): a memory ceiling of 128 MiB. It declares no port: cron serves on the socket the host passes it (`S02`), and a manifest carrying `port` is refused by `devctl build` and by opsctl. It names no `cache/`: it keeps nothing but its database.

Command:

```
$ cron manifest
```

Output:

```
app = "cron"
description = "Triggers that emit events on a schedule"
default = false
mcp = true
guests = false
secrets = []

[database]
engine = "sqlite"
path = "state/cron.db"

[resources]
memory_max = "128M"
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/cron` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what cron can do

Command:

```
$ cron --help
```

Output:

```
Usage: cron [command]

Keep triggers that emit events on the suite's event bus on a schedule,
with MCP tools at /mcp and a page of triggers at /, on the socket
systemd passes in.
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

- `bin/cron` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ cron bogus
```

Output:

```
cron: unknown command 'bogus'

see 'cron --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/cron` exists.

Postconditions:

- Nothing has changed.

## A developer gives an option cron does not have

An unknown option is a usage error like an unknown command, and is named back the same way.

Command:

```
$ cron --bogus
```

Output:

```
cron: unknown option '--bogus'

see 'cron --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/cron` exists.

Postconditions:

- Nothing has changed.

## An operator checks a database that is up to date

After a deploy, or before one, an operator wants to see the schema the binary expects beside what the database holds: which of the binary's migrations the database has had applied, and when. `cron db status` prints one line for each version, in ascending order: `<version> applied <applied-at>` for a migration the database has had applied, where `<applied-at>` is when it was applied, in UTC to the microsecond; `<version> pending` for one the binary carries that the database has not had applied; and `<version> unknown <applied-at>` for one the database records that the binary does not carry. It reads `state/cron.db` relative to its working directory and only looks: it applies nothing and changes nothing. Like `manifest`, it needs no socket and reads no environment.

Command:

```
$ cron db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
```

Exits 0. The line is on stdout; stderr is empty. The time is the one the database records for that version.

Preconditions:

- `bin/cron` exists.
- The working directory holds `state/cron.db`, which a cron carrying the same migrations created, applying version `0001` at `2026-10-05T14:03:07.123456Z`. On a host the working directory is `/opt/cron` and the operator is a user who can read the database.

Postconditions:

- Nothing has changed.

## An operator checks before the database exists

Before cron has first started there is no database. `cron db status` then reports every migration the binary carries as pending, which is what the next start will apply, and creates nothing: neither `state/` nor `state/cron.db`.

Command:

```
$ cron db status
```

Output:

```
0001 pending
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/cron` exists.
- `state/cron.db` does not exist in the working directory; `state/` may be absent too.

Postconditions:

- Nothing has changed. No file or directory was created.

## An operator checks a database a newer cron has upgraded

A newer cron has applied a migration this binary does not carry, as when a deploy is rolled back to an older binary over a database the newer one upgraded. Older code runs on newer data: this cron still serves such a database, applying nothing to it and warning that it is ahead (`S02`). `cron db status` shows what the database holds: it prints every line as usual, the version it does not know among them as `unknown`, and succeeds. A database ahead of the binary is a state to report, not a failure.

Command:

```
$ cron db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
0002 unknown 2026-10-06T09:12:44.000017Z
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `bin/cron` exists, carrying only migration `0001`.
- The working directory holds `state/cron.db`, which records version `0001` applied at `2026-10-05T14:03:07.123456Z` and version `0002` applied at `2026-10-06T09:12:44.000017Z`.

Postconditions:

- Nothing has changed.

## An operator checks a database that cannot be read

`state/cron.db` exists but is not a database cron can read: its contents are not a SQLite database, or it is a directory, or the operator cannot read it. cron prints no lines, since it cannot tell what the database holds, and says why on stderr.

Command:

```
$ cron db status
```

Output:

```
cron: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying failure.

Preconditions:

- `bin/cron` exists.
- The working directory holds `state/cron.db`, a file whose contents are not a SQLite database.

Postconditions:

- Nothing has changed.

## An operator mistypes the database command

`db` takes exactly one command, `status`, and `status` takes no arguments. The argument cron complains about is the first one it does not take: `cron db` alone names `db`, which is not a command by itself; `cron db bogus` names `bogus`; and `cron db status extra` names `extra`.

Command:

```
$ cron db bogus
```

Output:

```
cron: unknown command 'bogus'

see 'cron --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. `cron db` fails the same way with `cron: unknown command 'db'`, and `cron db status extra` with `cron: unknown command 'extra'`.

Preconditions:

- `bin/cron` exists.

Postconditions:

- Nothing has changed. No database was read, and no file or directory was created.
