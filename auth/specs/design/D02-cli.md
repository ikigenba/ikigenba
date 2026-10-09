# D02-cli

auth is one binary. Before it serves anything it is a command-line program, and
this document fixes that surface: the grammar of commands and options it
accepts, the text it prints for `--help`, the manifest it prints for `manifest`,
what `--version` reports, what `db status` reports of the database, how it
refuses a command or option it does not know, and the rule that a command never
looks at the environment. Running the binary
with no command serves the auth service; the serving behavior itself — the
Google settings, the drain deadline, the socket the host passes in, readiness,
the drain on a signal, and the failures of each — is D03's contract, and this
document only fixes that a bare invocation is the one that serves.

The manifest is a fact about the binary, so the binary emits it. The committed
`etc/manifest.toml` in the checkout is a copy kept so the tree can be read
without a build. The manifest declares auth's
name; its description, which is `server.Description` (D05), the line the
about page shows and the host publishes in its services file, so the two
cannot drift; that it is not the host's default app, the secrets it needs, its
Workspace domain, and its SQLite database; its `[resources]` table places it
among the platform's core services and caps its memory at 128M. It declares no
port: auth serves on the socket the host passes it, and a manifest carrying `port` is refused by
`devctl build` and by opsctl. The domain `michaelgreenly.dev` and the database
path are data the manifest fixes, not release versions.

The help text says where auth serves — on the socket systemd passes in — and
names the exit codes: `0` on success, `1` on a failure, and `2` on a usage
error. D03 leans on that split: a start the caller got wrong (a missing
Google setting, a drain deadline that is not a number of seconds, no socket,
several sockets) exits 2, while trouble on the host once auth has its socket —
a database it cannot open, a drain that runs out — exits 1, and so does a
`db status` whose database cannot be read. A database newer than the binary
is not a failure: auth serves it (D03), and `db status` reports it and exits 0.

`db status` is an operator's look at the database: which of the migrations the
binary carries the database at `state/auth.db` has had applied, and when,
which it has not, and any it records that the binary does not carry. What it
prints, line by line, is appkit's `db.Status` (its D16), which auth calls with
the database path (`D01-layout-and-run-seam`: `state/auth.db` resolved against
`Process.Dir`, so relative to the working directory in the binary) and the
migrations `auth.Migrations()` returns, writing straight to `Stdout`. auth adds
nothing to the lines and states nothing about their format; its requirements
say only that the bytes on `Stdout` are the ones `db.Status` writes for that
path and those migrations, so a test compares `Run`'s output with what its own
`db.Status` call writes for the same database, never with a format it spells
out. `db.Status` only looks: it applies nothing and creates nothing, so before
auth's first start `db status` reports every migration pending and leaves the
directory as it was. When it returns an error, auth says so on one line,
`auth: ` and the error's text, and exits 1; a newline in that text is replaced
by a space, so the diagnostic is one line whatever appkit's error holds.
Whatever `db.Status` had already written to `Stdout` stays there: findings are
product, and the repository's command-line conventions send a report to
stdout whole, bad news included. That is the one case in which a failure may
leave anything on `Stdout`. A database that records a migration this auth does
not carry, one a newer auth upgraded, is not a failure: older code runs on
newer data (D03), appkit's `db.Status` lists every line, the unknown version
among them, and returns nil, so `db status` writes nothing to `Stderr` and
exits 0. A database that cannot be read yields no lines at all and exits 1.
`db status` reads no environment and takes no socket, so it needs none of the
Google settings.

`db` takes exactly one command, `status`, and `status` takes nothing after
it. The argument auth complains about is the first one it does not take:
`db` itself when it stands alone, since `db` is not a command by itself; the
word after `db` when that is not `status`; and the word after `db status`.

Arguments are checked before the environment is read. A command never needs a
socket, and only `db status` reads the database, so `auth --version` succeeds with nothing passed in and `auth bogus`
fails as an unknown command whatever the environment holds; `Run` touches none
of the environment, the inherited descriptor or systemd's notification socket
unless `Args` is empty. The environment reads that happen before `Run` are
appkit's: `main` reads the display string with `version.Display` and builds
the banner kit with `page.New`, which captures `IKIGENBA_SERVICES`, before it
calls `Run` whatever the arguments (D01), and neither read can fail or change
a command's outcome other than the text `--version` prints; `Run` never calls
the banner source for a command (D03). A command serves nothing, so it records
no event either: the trail is what auth records while it serves, and `Run`
hands nothing to `Process.Sink` for a command or a usage error (D03,
R-8B6Y-ZBZR). Only `db status` reads anything under `Process.Dir`, and it
creates nothing there; every other command, and every usage error, leaves
`Dir` as it found it, so `auth db bogus` reads no database and creates
nothing.

The program follows the repository's stream and exit conventions: the product
of a command goes to stdout, a diagnostic goes to stderr with a first line that
begins `auth: `, and every failure but that one `db status` case leaves stdout
empty, so a caller that reads
the streams separately sees a product or a complaint, never a mixture. Every
diagnostic is one write to stderr, so a two-line diagnostic lands whole even
when another writer such as journald interleaves with the same stream. The
version `--version` reports is `Process.Version`, the display string `main`
read (D01), and a newline, which is one empty line when the host set no
identity; auth writes no version literal.

## REQUIREMENTS

- R-7HXD-SU73: auth's command-line grammar MUST recognize exactly these forms and no others: an invocation with no command (which serves), the `manifest` subcommand, the `db status` subcommand (the argument `db` followed by the argument `status` and nothing else), the `--help` option, and the `--version` option.
- R-7J5A-6LXS: The usage text MUST be exactly the following, and nothing else (a trailing newline follows the last line):
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
- R-RG9K-6CZV: The app manifest MUST be exactly the following text, and nothing else (a trailing newline follows the last line), where `<description>` stands for exactly the text of the `internal/server` package's `Description` (D05):
  ```
  app = "auth"
  description = "<description>"
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
- R-OYUS-DBTQ: Invoking auth with no command MUST serve the auth service (the serving behavior is D03's contract); it MUST NOT print the usage text or the manifest.
- R-8IPU-AMS5: The `auth` executable run with exactly the argument `--version` MUST write the display string of that run of the executable (D01, R-8CMC-DS2O), followed by a single newline, to stdout, write nothing to stderr, and exit `0`, so that a run whose environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE` writes exactly one newline.
- R-8JXQ-OEIU: When `Args` is exactly `["--version"]`, `Run` MUST write `p.Version` followed by a single `"\n"` to `Stdout` and nothing else, write nothing to `Stderr`, and return `0`, so that an empty `p.Version` writes exactly `"\n"`.
- R-P1AL-4VB4: `auth --help` MUST write the usage text to stdout, write nothing to stderr, and exit `0`.
- R-P2IH-IN1T: `auth manifest` MUST write the app manifest to stdout, write nothing to stderr, and exit `0`.
- R-7KD6-KDOH: Given a first argument that is none of `--version`, `--help`, `manifest`, and `db`, and does not begin with `--`, auth MUST write to stderr exactly the line `auth: unknown command '<x>'` (where `<x>` is that argument), then one empty line, then the line `see 'auth --help' for usage`; it MUST write nothing to stdout and exit `2`.
- R-P7E3-1Q0L: Given an unrecognized option of the form `--<x>`, auth MUST write to stderr exactly the line `auth: unknown option '--<x>'`, then one empty line, then the line `see 'auth --help' for usage`; it MUST write nothing to stdout and exit `2`.
- R-P8LZ-FHRA: On a usage error, auth MUST emit only the diagnostic described above; it MUST NOT write the usage text to stderr or to stdout.
- R-7LL2-Y5F6: When `Args` is not empty, `Run` MUST return without calling `LookupEnv`, `Unsetenv`, or `Inherit`, without taking file descriptor 3, and without sending anything to a notification socket, so that the outcome of a command or a usage error is the same whatever the environment holds.
- R-7MSZ-BX5V: Whenever `Run` returns a value other than `0` it MUST have written nothing to `Stdout` other than, when `Args` is exactly `["db", "status"]`, the bytes R-7P8S-3GN9 states, and every diagnostic `Run` writes MUST be delivered as a single call to `Stderr.Write` whose first line begins `auth: `.
- R-7P8S-3GN9: When `Args` is exactly `["db", "status"]`, `Run` MUST write to `Stdout` exactly the bytes, and nothing else, that appkit's `db.Status` (package `github.com/ikigenba/ikigenba/appkit/db`) writes to its writer when called with a context that is not done and a `db.Config` whose `Path` is the database path (R-7GPH-F2GE) and whose `Migrations` is the root package's `auth.Migrations()`, over the database as it is when `Run` is called; so that `Run` reports the migrations of `state/auth.db` under `p.Dir`, or under the process working directory when `p.Dir` is empty.
- R-7QGO-H8DY: When `Args` is exactly `["db", "status"]` and the `db.Status` call R-7P8S-3GN9 describes returns nil, `Run` MUST write nothing to `Stderr` and return `0`.
- R-8MDJ-FY08: When `Args` is exactly `["db", "status"]` and the `db.Status` call R-7P8S-3GN9 describes returns a non-nil error `err`, `Run` MUST write exactly `"auth: " + r + "\n"` to `Stderr`, where `r` is `err.Error()` with every newline character replaced by one space, and return `1`, having written to `Stdout` what R-7P8S-3GN9 states; so that a `p.Dir` whose `state/auth.db` is a regular file that is not a SQLite database gets nothing on `Stdout` and that one line on `Stderr`.
- R-8NLF-TPQX: When `Args` is exactly `["db", "status"]` and `p.Dir`'s `state/auth.db` is a SQLite database the process can read whose `schema_migrations` holds, besides the version of every migration `auth.Migrations()` holds, a version that `auth.Migrations()` does not hold, `Run` MUST write to `Stdout` what R-7P8S-3GN9 states, write nothing to `Stderr`, and return `0`, so that a database a newer auth upgraded is reported line by line, its unknown version among the lines, and is not a failure.
- R-7SWH-8RVC: When `Args` is exactly `["db", "status"]` and `p.Dir` names an empty directory, that directory MUST still be empty when `Run` returns.
- R-7U4D-MJM1: When `Args` is not empty and is not exactly `["db", "status"]`, `Run` MUST create, remove or change nothing under `p.Dir`, so that when `p.Dir` names an empty directory it is still empty when `Run` returns.
- R-7VCA-0BCQ: When `Args[0]` is `db` and `Args` is not exactly `["db", "status"]`, `Run` MUST treat `Args` as a usage error whose offending argument `arg` is `Args[0]` when `Args` is exactly `["db"]`, `Args[1]` when `Args[1]` is not `status`, and `Args[2]` otherwise; it MUST write to `Stderr` exactly `"auth: unknown option '" + arg + "'\n\nsee 'auth --help' for usage\n"` when `arg` begins with `--` and exactly `"auth: unknown command '" + arg + "'\n\nsee 'auth --help' for usage\n"` otherwise, write nothing to `Stdout`, and return `2`.

## Canonical usage for review

```
$ IKIGENBA_COMMIT=<sha> IKIGENBA_RELEASE=<label> auth --version
<label> (<short sha>)
$ echo $?
0

$ auth --version          # neither variable set

$ echo $?
0

$ auth manifest
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

$ auth --help
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

$ auth db status
0001 applied 2026-10-05T14:03:07.123456Z
0002 applied 2026-10-05T14:03:07.125003Z
0003 applied 2026-10-05T14:03:07.126518Z
$ echo $?
0

$ auth db status          # before the first start
0001 pending
0002 pending
0003 pending

$ auth db status          # a newer auth has applied 0004
0001 applied 2026-10-05T14:03:07.123456Z
0002 applied 2026-10-05T14:03:07.125003Z
0003 applied 2026-10-05T14:03:07.126518Z
0004 unknown 2026-10-06T09:12:44.000017Z
$ echo $?
0

$ auth db bogus
auth: unknown command 'bogus'

see 'auth --help' for usage
$ echo $?
2

$ auth bogus
auth: unknown command 'bogus'

see 'auth --help' for usage
$ echo $?
2

$ auth --bogus
auth: unknown option '--bogus'

see 'auth --help' for usage
$ echo $?
2
```

`<sha>`, `<label>` and `<short sha>` above are placeholders; `--version`
prints the display string appkit builds from the environment it runs in.
