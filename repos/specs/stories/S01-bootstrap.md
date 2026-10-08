# Stories — bootstrap

Running repos at all: help, version, the manifest, the state of its database, exit codes. repos is an app of the platform, the suite's home for git repositories: one Go binary that serves each of a user's repositories over git's smart HTTP at `/<name>.git` (`S11`, `S12`), offers six MCP tools to create, list, show, rename, and delete them and to see the load on them at `/mcp` (`S05` to `S10`), keeps each one as a bare repository on disk that the suite's other apps on the same host read directly (`S15`), and serves a landing page at `/` that says what it is and how to clone (`S03`). On a host it runs as `/opt/ikigenba/current/repos/bin/repos` with `/var/opt/ikigenba/repos` as its working directory and its environment read from `/etc/opt/ikigenba/repos/env`; a developer runs the same binary from the checkout. With no command it serves (`S02`); the commands here are what the build, and an operator, ask of it. They serve nothing, run no git, and record no event, in the trail or on the event bus: both are what repos does while it serves (`S02`). repos keeps its catalog of repositories in its database, the SQLite file `state/repos.db` under its working directory, which it creates and brings up to date when it starts (`S02`). The database's schema is a sequence of numbered migrations, each with a four-digit version, that the binary carries and applies in order; this repos carries one, version `0001`.

## A developer asks which version they have

repos carries no version of its own: nothing in its source or its build names one. The environment tells it which code it is running, through two variables: `IKIGENBA_COMMIT`, the commit it was built from, and `IKIGENBA_RELEASE`, the label of the release, when there is one. From them repos builds its display string, `<display>`, which every later story uses with this meaning. With both set it is the label, one space, and the short commit in parentheses, `<label> (<short sha>)`; with only the commit it is the short commit alone; with only the label it is the label alone. The short commit is the first seven characters of `IKIGENBA_COMMIT`, or all of it when it is shorter; a value ending in `-dirty`, as a developer's sandbox marks a modified tree, is shortened without the suffix and keeps it after, as in `<short sha>-dirty`. Nothing else about either value is checked or changed. repos reads the two variables each time it is run with `--version`; a repos that serves reads them once, when it starts (`S02`). The same string is what the landing page's footer and the about screen show (`S03`) and repos' own `service.started` event carries in the trail (`S02`).

Command:

```
$ repos --version
```

Output:

```
<display>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/repos` exists, built from the checkout with `make`.
- `IKIGENBA_COMMIT` holds a commit, and `IKIGENBA_RELEASE` holds a label or is unset; `<display>` is the string they make.

Postconditions:

- Nothing has changed.

## A developer asks which version they have with no identity set

With neither variable set, or both empty, repos has no identity to show, and `<display>` is the empty string. It still answers, with an empty line, and still succeeds: a missing identity is not a failure.

Command:

```
$ repos --version
```

Output:

```

```

Exits 0. stdout holds one empty line, a single newline and nothing else; stderr is empty.

Preconditions:

- `bin/repos` exists.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed `etc/manifest.toml` is a copy kept so the checkout can be read without a build; the two are byte-identical, and `devctl build` refuses an app where they differ. repos declares its name; its description, the one line that says what repos is for, which the host publishes in its services file, the about screen shows (`S03`), and repos' MCP endpoint gives its clients as instructions (`S05`); that it is not the host's default app; that it is one of the suite's MCP services (`mcp = true`), so the gateway lists it and runs its tools (`S05`); no secrets; its eight settings, which the host writes into its environment file, `/etc/opt/ikigenba/repos/env`, and which it reads when it serves (`S02`): `READ_SLOTS` and `WRITE_SLOTS`, how many git reads and writes run at once, `QUEUE_LENGTH` and `QUEUE_SECONDS`, how many operations may wait for a slot and for how long (`S12`), `OPERATION_SECONDS`, the longest one git operation may run (`S12`), `PUSH_MAX_BYTES` and `REPO_MAX_BYTES`, the largest pack one push may send and the size at which a repository takes no more pushes (`S12`), and `MAINTENANCE_HOURS`, how often each repository is tidied (`S13`); its SQLite database, the catalog of repositories, which the host replicates like auth's; and how much of the host it may take, in opsctl's `[resources]` table, which opsctl writes into its service unit: it caps the memory of repos and every git repos runs together at 256M, of which repos keeps half, 128M, to itself, leaving the rest to git, and a git that runs out of memory is killed and fails alone, its operation with it, while repos keeps serving. It declares no port: repos serves on the socket the host passes it (`S02`), and a manifest carrying `port` is refused by `devctl build` and by opsctl.

Command:

```
$ repos manifest
```

Output:

```
app = "repos"
description = "Git repositories for the suite's content"
default = false
mcp = true
secrets = []

[env]
READ_SLOTS = "8"
WRITE_SLOTS = "2"
QUEUE_LENGTH = "16"
QUEUE_SECONDS = "30"
OPERATION_SECONDS = "600"
PUSH_MAX_BYTES = "104857600"
REPO_MAX_BYTES = "1073741824"
MAINTENANCE_HOURS = "24"

[database]
engine = "sqlite"
path = "state/repos.db"

[resources]
memory_max = "256M"
go_memory_limit = "128M"
oom_policy = "continue"
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/repos` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what repos can do

Command:

```
$ repos --help
```

Output:

```
Usage: repos [command]

Serve git repositories: smart HTTP at /<name>.git, MCP tools at /mcp, and a
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

- `bin/repos` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ repos bogus
```

Output:

```
repos: unknown command 'bogus'

see 'repos --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. An unknown option, `--bogus` say, fails the same way with `repos: unknown option '--bogus'`.

Preconditions:

- `bin/repos` exists.

Postconditions:

- Nothing has changed.

## An operator checks a database that is up to date

After a deploy, or before one, an operator wants to see the schema the binary expects beside what the database holds: which of the binary's migrations the database has had applied, and when. `repos db status` prints one line for each version, in ascending order: `<version> applied <applied-at>` for a migration the database has had applied, where `<applied-at>` is when it was applied, in UTC to the microsecond; `<version> pending` for one the binary carries that the database has not had applied; and `<version> unknown <applied-at>` for one the database records that the binary does not carry. It reads `state/repos.db` relative to its working directory and only looks: it applies nothing, changes nothing, and neither reads nor rebuilds the catalog's repositories. Like `manifest`, it needs no socket and no git and reads no environment.

Command:

```
$ repos db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
```

Exits 0. The line is on stdout; stderr is empty. The time is the one the database records for version `0001`.

Preconditions:

- `bin/repos` exists.
- The working directory holds `state/repos.db`, which a repos carrying the same migrations created or brought up to date, applying version `0001` at `2026-10-05T14:03:07.123456Z`. On a host the working directory is `/var/opt/ikigenba/repos` and the operator is a user who can read the database.

Postconditions:

- Nothing has changed.

## An operator checks before the database exists

Before repos has first started there is no database. `repos db status` then reports every migration the binary carries as pending, which is what the next start will apply, and creates nothing: neither `state/`, nor `state/repos.db`, nor `state/repos/`.

Command:

```
$ repos db status
```

Output:

```
0001 pending
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/repos` exists.
- `state/repos.db` does not exist in the working directory; `state/` may be absent too.

Postconditions:

- Nothing has changed. No file or directory was created.

## An operator checks a database a newer repos has upgraded

A newer repos has applied a migration this binary does not carry, as when a deploy is rolled back to an older binary over a database the newer one upgraded. Older code runs on newer data: this repos still serves such a database, applying nothing to it and warning that it is ahead (`S02`). `repos db status` shows what the database holds: it prints every line as usual, the version it does not know among them as `unknown`, and succeeds. A database ahead of the binary is a state to report, not a failure.

Command:

```
$ repos db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
0002 unknown 2026-10-06T09:12:44.000017Z
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `bin/repos` exists, carrying only migration `0001`.
- The working directory holds `state/repos.db`, which records version `0001` applied at `2026-10-05T14:03:07.123456Z` and version `0002` applied at `2026-10-06T09:12:44.000017Z`.

Postconditions:

- Nothing has changed.

## An operator checks a database that cannot be read

`state/repos.db` exists but is not a database repos can read: its contents are not a SQLite database, or it is a directory, or the operator cannot read it. repos prints no lines, since it cannot tell what the database holds, and says why on stderr.

Command:

```
$ repos db status
```

Output:

```
repos: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying failure.

Preconditions:

- `bin/repos` exists.
- The working directory holds `state/repos.db`, a file whose contents are not a SQLite database.

Postconditions:

- Nothing has changed.

## An operator mistypes the database command

`db` takes exactly one command, `status`, and `status` takes no arguments. The argument repos complains about is the first one it does not take: `repos db` alone names `db`, which is not a command by itself; `repos db bogus` names `bogus`; and `repos db status extra` names `extra`.

Command:

```
$ repos db bogus
```

Output:

```
repos: unknown command 'bogus'

see 'repos --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. `repos db` fails the same way with `repos: unknown command 'db'`, and `repos db status extra` with `repos: unknown command 'extra'`.

Preconditions:

- `bin/repos` exists.

Postconditions:

- Nothing has changed. No database was read, and no file or directory was created.
