# Stories — bootstrap

Running sites at all: help, version, the manifest, the state of its database, exit codes. sites is an app of the platform, the suite's static site host: one Go binary that serves each site at `/<slug>/` on `sites.<space>` from a tree it unpacks out of one of repos' bare repositories (`S11`, `S12`), redirects the apex host, the root domain the space hangs from when it is routed to sites, to the apex site (`S13`), offers seven MCP tools to list, show, create, publish, update, and delete sites and to choose the apex site at `/mcp` (`S05` to `S10`, `S13`), and serves a landing page at `/` that lists the space's sites and says how to make one (`S03`). On a host it runs as `/opt/sites/bin/sites` with `/opt/sites` as its working directory and its environment read from `/opt/sites/etc/env`; a developer runs the same binary from the checkout. With no command it serves (`S02`); the commands here are what the build, and an operator, ask of it. They serve nothing, run no git, read no repository, and record no event: the trail is what sites records while it serves (`S02`). sites keeps its catalog of sites and the apex setting in its database, the SQLite file `state/sites.db` under its working directory, which it creates and brings up to date when it starts (`S02`). The database's schema is a sequence of numbered migrations, each with a four-digit version, that the binary carries and applies in order; this sites carries one, version `0001`. Of the commands only `db status` reads the database, and it only looks.

## A developer asks which version they have

sites carries no version of its own: nothing in its source or its build names one. The environment tells it which code it is running, through two variables: `IKIGENBA_COMMIT`, the commit it was built from, and `IKIGENBA_RELEASE`, the label of the release, when there is one. From them sites builds its display string, `<display>`, which every later story uses with this meaning. With both set it is the label, one space, and the short commit in parentheses, `<label> (<short sha>)`; with only the commit it is the short commit alone; with only the label it is the label alone. The short commit is the first seven characters of `IKIGENBA_COMMIT`, or all of it when it is shorter; a value ending in `-dirty`, as a developer's sandbox marks a modified tree, is shortened without the suffix and keeps it after, as in `<short sha>-dirty`. Nothing else about either value is checked or changed. sites reads the two variables each time it is run with `--version`; a sites that serves reads them once, when it starts (`S02`). It is the same string the landing page's footer and the about screen show (`S03`), the MCP endpoint gives as its `serverInfo` (`S05`), and sites' own `service.started` event carries in the trail (`S02`).

Command:

```
$ sites --version
```

Output:

```
<display>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/sites` exists, built from the checkout with `make`.
- `IKIGENBA_COMMIT` holds a commit, and `IKIGENBA_RELEASE` holds a label or is unset; `<display>` is the string they make.

Postconditions:

- Nothing has changed.

## A developer asks which version they have with no identity set

With neither variable set, or both empty, sites has no identity to show, and `<display>` is the empty string. It still answers, with an empty line, and still succeeds: a missing identity is not a failure.

Command:

```
$ sites --version
```

Output:

```

```

Exits 0. stdout holds one empty line, a single newline and nothing else; stderr is empty.

Preconditions:

- `bin/sites` exists.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed `etc/manifest.toml` is a copy kept so the checkout can be read without a build; the two are byte-identical, and `devctl build` refuses an app where they differ. sites declares its name; its description, the one line that says what sites is for, which the host publishes in its services file, the about screen shows (`S03`), and sites' MCP endpoint gives its clients as instructions (`S05`); that it is not the host's default app; that it is one of the suite's MCP services (`mcp = true`), so the gateway lists it and runs its tools (`S05`); that it serves guests (`guests = true`), visitors with no credential at all, so on a host with an authenticator nginx lets such a visitor through to sites' pages with no identity instead of sending them to sign in, while `/mcp` stays challenged (opsctl's `S5-nginx.md`, `S7-apps.md`): that is how a public site reaches anyone (`S11`), and sites itself sends a guest to sign in where it needs a user (`S03`, `S12`); no secrets; its three settings, which the host writes into its `etc/env` and which it reads when it serves (`S02`): `REPOS_DIR`, the directory holding repos' bare repositories, relative to sites' working directory, so `/opt/sites/../repos/state/repos` on a host (`S18`), `SITE_MAX_BYTES`, the largest a site's tree may be, 256 MiB (`S17`), and `OPERATION_SECONDS`, the longest one git run may take (`S17`); and its SQLite database, the catalog of sites, which the host replicates like auth's. Its `[resources]` table caps its memory at 128M, for sites and every git it runs together. It declares no port: sites serves on the socket the host passes it (`S02`), and a manifest carrying `port` is refused by `devctl build` and by opsctl. It names no `cache/`: the unpacked trees there are disposable and rebuilt on demand (`S16`), so the host neither keeps nor replicates them.

Command:

```
$ sites manifest
```

Output:

```
app = "sites"
description = "Static sites from the suite's repositories"
default = false
mcp = true
guests = true
secrets = []

[env]
REPOS_DIR = "../repos/state/repos"
SITE_MAX_BYTES = "268435456"
OPERATION_SECONDS = "600"

[database]
engine = "sqlite"
path = "state/sites.db"

[resources]
memory_max = "128M"
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/sites` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what sites can do

Command:

```
$ sites --help
```

Output:

```
Usage: sites [command]

Serve static sites from the suite's repositories at /<slug>/, MCP tools at
/mcp, and a landing page at /, on the socket systemd passes in. With no
command, serve.

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

- `bin/sites` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ sites bogus
```

Output:

```
sites: unknown command 'bogus'

see 'sites --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/sites` exists.

Postconditions:

- Nothing has changed.

## A developer gives an option sites does not have

An unknown option is a usage error like an unknown command, and is named back the same way.

Command:

```
$ sites --bogus
```

Output:

```
sites: unknown option '--bogus'

see 'sites --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/sites` exists.

Postconditions:

- Nothing has changed.

## An operator checks a database that is up to date

After a deploy, or before one, an operator wants to see the schema the binary expects beside what the database holds: which of the binary's migrations the database has had applied, and when. `sites db status` prints one line for each version, in ascending order: `<version> applied <applied-at>` for a migration the database has had applied, where `<applied-at>` is when it was applied, in UTC to the microsecond; `<version> pending` for one the binary carries that the database has not had applied; and `<version> unknown <applied-at>` for one the database records that the binary does not carry. It reads `state/sites.db` relative to its working directory and only looks: it applies nothing and changes nothing. Like `manifest`, it needs no socket, no git and no repository, and reads no environment.

Command:

```
$ sites db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
```

Exits 0. The line is on stdout; stderr is empty. The time is the one the database records for version `0001`.

Preconditions:

- `bin/sites` exists.
- The working directory holds `state/sites.db`, which a sites carrying the same migrations created or brought up to date, applying version `0001` at `2026-10-05T14:03:07.123456Z`. On a host the working directory is `/opt/sites` and the operator is a user who can read the database.

Postconditions:

- Nothing has changed.

## An operator checks before the database exists

Before sites has first started there is no database. `sites db status` then reports every migration the binary carries as pending, which is what the next start will apply, and creates nothing: neither `state/` nor `state/sites.db`.

Command:

```
$ sites db status
```

Output:

```
0001 pending
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/sites` exists.
- `state/sites.db` does not exist in the working directory; `state/` may be absent too.

Postconditions:

- Nothing has changed. No file or directory was created.

## An operator checks a catalog from before sites recorded its migrations

A sites from before the database recorded its migrations kept the same catalog in `state/sites.db`, with no record of any migration. `sites db status` reports every migration the binary carries as pending, as for a database that does not exist: the next start applies `0001`, which finds the catalog's schema already there, changes nothing in it, and records `0001` as applied (`S02`).

Command:

```
$ sites db status
```

Output:

```
0001 pending
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/sites` exists.
- The working directory holds `state/sites.db`, a catalog an earlier sites kept, holding sites and recording no migration.

Postconditions:

- Nothing has changed: the catalog holds the sites it held, and still records no migration.

## An operator checks a database a newer sites has upgraded

A newer sites has applied a migration this binary does not carry, as when a deploy is rolled back to an older binary over a database the newer one upgraded. Older code runs on newer data: this sites still serves such a database, applying nothing to it and warning that it is ahead (`S02`). `sites db status` shows what the database holds: it prints every line as usual, the version it does not know among them as `unknown`, and succeeds. A database ahead of the binary is a state to report, not a failure.

Command:

```
$ sites db status
```

Output:

```
0001 applied 2026-10-05T14:03:07.123456Z
0002 unknown 2026-10-06T09:12:44.000017Z
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `bin/sites` exists, carrying only migration `0001`.
- The working directory holds `state/sites.db`, which records version `0001` applied at `2026-10-05T14:03:07.123456Z` and version `0002` applied at `2026-10-06T09:12:44.000017Z`.

Postconditions:

- Nothing has changed.

## An operator checks a database that cannot be read

`state/sites.db` exists but is not a database sites can read: its contents are not a SQLite database, or it is a directory, or the operator cannot read it. sites prints no lines, since it cannot tell what the database holds, and says why on stderr.

Command:

```
$ sites db status
```

Output:

```
sites: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying failure.

Preconditions:

- `bin/sites` exists.
- The working directory holds `state/sites.db`, a file whose contents are not a SQLite database.

Postconditions:

- Nothing has changed.

## An operator mistypes the database command

`db` takes exactly one command, `status`, and `status` takes no arguments. The argument sites complains about is the first one it does not take: `sites db` alone names `db`, which is not a command by itself; `sites db bogus` names `bogus`; and `sites db status extra` names `extra`.

Command:

```
$ sites db bogus
```

Output:

```
sites: unknown command 'bogus'

see 'sites --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. `sites db` fails the same way with `sites: unknown command 'db'`, and `sites db status extra` with `sites: unknown command 'extra'`.

Preconditions:

- `bin/sites` exists.

Postconditions:

- Nothing has changed. No database was read, and no file or directory was created.
