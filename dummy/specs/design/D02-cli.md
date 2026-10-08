# D02-cli

dummy as a command: what `cli.Run` does with the arguments that
`D01-layout-and-run-seam` hands it through `Process`. This design covers the
four commands (`--version`, `manifest`, `--help`, `db status`), the usage
error, and the rule that a command never looks at the environment. When
`Args` is empty, dummy serves, and everything from there — the drain
deadline, the socket the host passes in, the database, readiness, the drain
on a signal, and the failures of each — is `D03-serve`; what the panel then
answers is `D04-panel` and the designs it leads to.

`Run` takes at most two arguments and the arguments decide everything. An
empty `Args` means serve. The three recognised single arguments each write
one product to `Stdout` and return `ExitSuccess` without touching the
environment: `--version` writes `Process.Version`, the display string `main`
read (`D01-layout-and-run-seam`), and a newline, which is one empty line when
the host set no identity, `manifest` writes
`Manifest` exactly as declared, and `--help` writes `Usage`, the constant
`D01-layout-and-run-seam` declares and whose value this design fixes byte for
byte, so the help text is contract. The one two-word command is `db status`.
Any other `Args` is a usage error. The argument dummy complains about is the
first one it does not take: the first element when that is not a recognised
word; `db` itself when it stands alone, since `db` is not a command by
itself; the word after `db` when that is not `status`; the word after `db
status`, which takes nothing; and otherwise the second element, which is
surplus after `--version`, `manifest` or `--help` whatever it says. An
argument that starts with `-` is an unknown option; any other is an unknown
command. Both diagnostics follow the repository's command-line conventions:
the first line begins `dummy: `, and after exactly one empty line comes the
hint to run `dummy --help`, unprefixed because dummy is speaking for itself.
The usage text is never written to `Stderr`.

`db status` is an operator's look at the database: which of the migrations
the binary carries the database `state/dummy.db` has had applied, and when,
which it has not, and any it records that the binary does not carry. What it
prints, line by line, is appkit's `db.Status` (its D16), which dummy calls
with the path `state/dummy.db` resolved against `Process.Dir` (the process's
working directory when `Dir` is empty, as `main` leaves it) and the
migrations `dummy.Migrations()` returns, writing straight to `Stdout`. dummy
adds nothing to the lines and states nothing about their format; its
requirements say only that the bytes on `Stdout` are the ones `db.Status`
writes for that path and those migrations, so a test compares `Run`'s output
with what its own `db.Status` call writes for the same database, never with
a format it spells out. `db.Status` only looks: it applies nothing, changes
nothing, and creates nothing, so before dummy's first start `db status`
reports every migration pending and leaves the directory as it was. When it
returns an error, dummy says so on one line, `dummy: ` and the error's text,
and exits 1; a newline in that text is replaced by a space, as scripts does
with its store's errors, so the diagnostic is one line whatever appkit's
error holds. Whatever `db.Status` had already written to `Stdout` stays
there: the findings are product, and the repository's command-line
conventions send a report to stdout whole, bad news included. That is the one
case in which a failure may leave anything on `Stdout`. A database that
records a migration this dummy does not carry, one a newer dummy upgraded, is
not a failure: older code runs on newer data (`D03-serve`), appkit's
`db.Status` lists every line, the unknown version among them, and returns
nil, so `db status` writes nothing to `Stderr` and exits 0. A database that
cannot be read yields no lines at all and exits 1. `db status` reads no
environment and takes no socket.

The help text says what dummy serves — the control panel, and its MCP tools
at `/mcp` — and where — on the socket systemd passes in — and names the exit
codes, which is where the contract the rest of the design
realises is declared: 0 for success, 1 for a failure, 2 for a usage
error. `D03-serve` leans on that split: a start the caller got wrong (no
socket, several sockets, a drain deadline that is not a number of seconds)
exits 2, while trouble on the host — a socket that cannot be taken, a
database that cannot be opened, a drain that runs out — exits 1, and so does
a `db status` whose database cannot be read. The
constant for 1 keeps its name, `ExitServerFailed`; only the help text's word
for it changed.

Arguments are checked before the environment is read. A command never needs
a socket, so `dummy --version` succeeds with nothing passed in and
`dummy bogus` fails as an unknown command whatever the environment holds;
`Run` touches none of the environment, the inherited descriptor or systemd's
notification socket unless `Args` is empty. Only `db status` reads anything
under `Dir`, and it creates nothing there; every other command, and every
usage error, leaves `Dir` as it found it, so `dummy db bogus` reads no
database and creates nothing.

Every failure but that one `db status` case leaves `Stdout` empty, so a
caller that reads the streams separately sees a product or a complaint, never
a mixture. Every diagnostic is one write to `Stderr`, so a two-line
diagnostic lands whole even when another writer such as journald interleaves
with the same stream.

## REQUIREMENTS

- R-32FR-5LB4: `Usage` MUST be exactly `"Usage: dummy [command]\n\nServe the dummy control panel, and its MCP tools at /mcp, on the socket\nsystemd passes in. With no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"`.
- R-J3FT-88NH: When `Args` is exactly `["--version"]`, `Run` MUST write `p.Version` followed by a single `"\n"` to `Stdout` and nothing else, write nothing to `Stderr`, and return `ExitSuccess`, so that an empty `p.Version` writes exactly `"\n"`.
- R-RKC3-RLCM: When `Args` is exactly `["manifest"]`, `Run` MUST write exactly `Manifest` to `Stdout` and nothing else, write nothing to `Stderr`, and return `ExitSuccess`.
- R-S6AA-NGP4: When `Args` is exactly `["--help"]`, `Run` MUST write exactly `Usage` to `Stdout` and nothing else, write nothing to `Stderr`, and return `ExitSuccess`.
- R-363G-AWJ7: When `Args` is exactly `["db", "status"]`, `Run` MUST write to `Stdout` exactly the bytes, and nothing else, that appkit's `db.Status` (package `github.com/ikigenba/ikigenba/appkit/db`) writes to its writer when called with a context that is not done and a `db.Config` whose `Path` is `filepath.Join(p.Dir, "state", "dummy.db")` and whose `Migrations` is `dummy.Migrations()`, over the database as it is when `Run` is called; so that `Run` reports the migrations of `state/dummy.db` under `p.Dir`, or under the process working directory when `p.Dir` is empty.
- R-37BC-OO9W: When `Args` is exactly `["db", "status"]` and the `db.Status` call R-363G-AWJ7 describes returns nil, `Run` MUST write nothing to `Stderr` and return `ExitSuccess`.
- R-J4NP-M0E6: When `Args` is exactly `["db", "status"]` and the `db.Status` call R-363G-AWJ7 describes returns a non-nil error `err`, `Run` MUST write exactly `"dummy: " + r + "\n"` to `Stderr`, where `r` is `err.Error()` with every newline character replaced by one space, and return `ExitServerFailed`, having written to `Stdout` what R-363G-AWJ7 states.
- R-J5VL-ZS4V: When `Args` is exactly `["db", "status"]` and `p.Dir`'s `state/dummy.db` is a SQLite database the process can read whose `schema_migrations` holds, besides the version of every migration `dummy.Migrations()` holds, a version that `dummy.Migrations()` does not hold, `Run` MUST write to `Stdout` what R-363G-AWJ7 states, write nothing to `Stderr`, and return `ExitSuccess`, so that a database a newer dummy upgraded is reported line by line, its unknown version among the lines, and is not a failure.
- R-39R5-G7RA: When `Args` is exactly `["db", "status"]` and `p.Dir` names an empty directory, that directory MUST still be empty when `Run` returns.
- R-3AZ1-TZHZ: When `Args` is not empty and is not exactly `["db", "status"]`, `Run` MUST create, remove or change nothing under `p.Dir`, so that when `p.Dir` names an empty directory it is still empty when `Run` returns.
- R-33NN-JD1T: `Run` MUST treat `Args` as a usage error unless `Args` is empty or is exactly one of `["--version"]`, `["manifest"]`, `["--help"]`, or `["db", "status"]`.
- R-34VJ-X4SI: On a usage error from `Args`, the offending argument MUST be `Args[0]` when `Args[0]` is none of `--version`, `manifest`, `--help`, or `db`, or when `Args` is exactly `["db"]`; `Args[2]` when `Args[0]` is `db` and `Args[1]` is `status`; and `Args[1]` otherwise.
- R-TV99-RZRU: On a usage error from `Args`, `Run` MUST write exactly `"dummy: unknown option '" + arg + "'\n\nsee 'dummy --help' for usage\n"` to `Stderr` when the offending argument `arg` begins with `-`, and exactly `"dummy: unknown command '" + arg + "'\n\nsee 'dummy --help' for usage\n"` otherwise, MUST write nothing to `Stdout`, and MUST return `ExitUsage`.
- R-UFZK-A3DN: When `Args` is not empty, `Run` MUST return without calling `LookupEnv`, `Unsetenv`, or `Inherit`, without taking file descriptor 3, and without sending anything to a notification socket, so that the outcome of a command or a usage error is the same whatever the environment holds.
- R-3C6Y-7R8O: Whenever `Run` returns a value other than `ExitSuccess` it MUST have written nothing to `Stdout` other than, when `Args` is exactly `["db", "status"]`, the bytes R-363G-AWJ7 states, and every diagnostic `Run` writes MUST be delivered as a single call to `Stderr.Write` whose first line begins `dummy: `.
