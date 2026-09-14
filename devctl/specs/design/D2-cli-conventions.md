# D2-cli-conventions

`devctl` is a multi-command CLI: zero or more top-level options, then a command,
then that command's arguments. The options are `-h`/`--help`, `-V`/`--version`,
and `--account <name>` (also spelled `--account=<name>`), and each command may
have subcommands, arguments and options of its own. Options precede the command;
an option after the command belongs to the command, so `devctl build --help`
prints `build`'s usage, not this one's, and `devctl version --account x` is a
usage error because `version` takes no arguments. The single-dash `-h` and `-V`
are accepted and not listed in the usage text.

There are six commands, and this design fixes the set: `version` here,
`space`, `secrets`, `build`, `deploy` and `restore` in D5–D9. Each owns its own
subcommand grammar and declares its own usage text, printed by
`devctl <command> --help`; the top-level text's closing line promises that, so
the promise is a requirement here and the text is a requirement there. `version`
is the one command small enough to live in this document.

**`--account`** names the AWS shared-config profile a command acts in. Nothing
is derived from the name: devctl hands it to the SDK's shared-config loader
untouched, which in this design's vocabulary means it reaches `Deps.Cloud` byte
for byte. Four commands require it — `space`, `secrets`, `deploy` and `restore`,
whose usage lines all begin `Usage: devctl --account <name> ...`. `version` and
`build` neither need nor mind it. A repeated `--account` is the last occurrence
wins, which a fake opener can now observe because commands read the profile.

The separated form takes the following argument as its value unless that
argument begins with `-` or names one of the six commands — so `devctl --account
version` is the developer who forgot the value, not a profile named `version`.
A profile really named like a command is still reachable as
`--account=version`; that is the whole cost of diagnosing the common typo.

**One missing-account rule, stated once.** Every command that requires
`--account` and is invoked without it refuses identically, in the same words,
and points the developer at its own help rather than at the top-level help.

That belongs here, as one requirement the four commands obey, not restated in
each of D5–D9: the sentence is the same sentence, the exit code is the same
code, and a command's design that repeated it would own a copy of text it does
not own. The check runs before the command looks at its own arguments, because
`--account` is the outer grammar: `devctl deploy` with neither account nor
operands reports the account. Asking for help is the one exception — every
story that prints a command's usage text does so with no `--account` at all, and
help that demanded a profile first would be help you could not reach — so
`--help` or `-h` anywhere in a command's arguments wins over this check, and
`devctl secrets --help` and `devctl space create --help` print their text and
exit 0 under any account or none.

**Output contract.** Every command is written to be read by an agent far more
often than by a person. stdout carries only the answer: a value, a list, a
checklist line per step. There is no decoration, colour, or progress output.
Every diagnostic — an error or a warning — goes to stderr in the ordinary Unix
form: its first line is `devctl: <message>`, and any further detail follows
after one blank line, unprefixed — the shape `git foo` uses, and the shape the
stories use when a diagnostic carries another program's output or a next step,
so that a failed remote install reports devctl's own summary of the command that
failed first, and the remote program's own complaint underneath it.

The usage text is never written to stderr; a usage error names the problem on
the `devctl: ` line and then, after the blank line, points at `--help`.

**Exit codes.** Four, and the help text lists them, so an agent never needs
to be told out of band:

- `0` — success, including help and version.
- `1` — the operation failed: the command was well-formed but could not do
  what it was asked.
- `2` — usage error, or a preflight check failed: unknown command or option,
  malformed argument, missing prerequisite, a secret the space lacks.
- `3` — refused: `devctl` must not run as root.

`cli.Run` returns one of those four and nothing else; every command in D5–D9
classifies its outcomes by this taxonomy.

**Root check, first — before the arguments are read at all.** devctl runs as
the developer, never as root: a root process would write root-owned files into
the developer's home and run ssh under the wrong identity. So `Deps.EUID` is
the first thing every invocation looks at, and a zero uid is refused with one
line and exit 3 whatever the arguments are — `sudo devctl --help` and
`sudo devctl version` included. Help and version are not exempt: the refusal is
about the process, not about what it was asked to do, and a developer who typed
`sudo` has one thing to fix before anything else is worth saying. This is the
opposite order from `opsctl`, which must run as root and reports usage errors
before refusing.

**Usage text.** The top-level usage text and `version`'s are declared byte for
byte in the requirements that carry them. The commands are listed in the order
the platform grew them, which is the order their designs come in. Each command's
own usage text is declared in that command's design and is printed by
`devctl <command> --help`.

`devctl version` with any argument other than `--help` or `-h` is a usage
error, as `docker version foo` and `kubectl version foo` are, and as every
opsctl command that takes no arguments is. A mistyped invocation is surfaced,
never swallowed.

**Version.** A single `var version` in `internal/cli` carries the version
string in source, never injected at build time, so dev and released builds
report the same string. The spec fixes only its shape, a `v`-prefixed
`MAJOR.MINOR.PATCH`; its value is release data, edited directly. The first
value is `v0.1.0`. `devctl version`, `-V`, and `--version` all print it bare
on its own line.

## REQUIREMENTS

- R-D4F7-8UZB: The top-level grammar MUST be `devctl [options] <command> [arguments]`, accepting exactly the options `-h`/`--help`, `-V`/`--version`, and `--account <name>`/`--account=<name>` before the command and no other top-level options.
- R-9QQG-ZAC4: The top-level command set MUST be exactly `version`, `space`, `secrets`, `build`, `deploy`, and `restore`.
- R-9T69-QTTI: An argument beginning with `-` that appears after the command MUST be passed to the command and MUST NOT be parsed as a top-level option, verified at least by `devctl version --help` printing the `version` usage text and exiting 0 and by `devctl version --account <name>` being a usage error.
- R-T8QF-I1XA: `devctl --help` and `devctl -h` MUST print exactly this text, once, to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl [options] <command> [arguments]

  Manage the ikigenba platform from the developer's machine. Never run as root.

  Commands:
    version   print the version
    space     list, create, destroy, stop, start, and inspect spaces
    secrets   push and list an app's secrets for a space
    build     build one app into its deployable file
    deploy    put a built app file on a space
    restore   give a space's app another space's data

  Options:
    --help              print this help
    --version           print the version
    --account <name>    AWS shared-config profile to act in

  Exit codes:
    0  success
    1  the operation failed
    2  usage error, or a preflight check failed
    3  refused: devctl must not run as root

  Run 'devctl <command> --help' for details on a command.
  ```
- R-A5D9-KJ8G: For each of `space`, `secrets`, `build`, `deploy`, and `restore`, `devctl <command> --help` and `devctl <command> -h` MUST print the usage text that command's own design declares, byte for byte, to stdout, write nothing to stderr, and exit 0.
- R-D82W-E67E: An invocation with no command MUST write exactly the three lines `devctl: no command given`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.
- R-D9AS-RXY3: An unknown command MUST write exactly the three lines `devctl: unknown command '<name>'`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.
- R-DAIP-5POS: An unknown top-level option MUST write exactly the three lines `devctl: unknown option '<option>'`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.
- R-9UE6-4LK7: `--account <name>` MUST take the following argument as the profile name whenever that argument neither begins with `-` nor names a command of the top-level command set, `--account=<name>` MUST take the text after the `=`, and the profile name MUST reach `Deps.Cloud` byte for byte with no trimming, case change, or other normalisation, verified with a fake `Deps.Cloud` that records the profile it is asked for.
- R-9VM2-IDAW: When `--account` appears more than once before the command, the last occurrence MUST be the profile name the command acts in, verified with a fake `Deps.Cloud` that records the profile it is asked for.
- R-9WTY-W51L: `--account` as the last argument, `--account` followed by an argument that begins with `-` or that names a command of the top-level command set, and `--account=` with an empty value MUST each write exactly the three lines `devctl: option '--account' requires a value`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.
- R-9Y1V-9WSA: The commands that require `--account` MUST be exactly `space`, `secrets`, `deploy`, and `restore`, and `devctl --account <name> build <app>` MUST produce the same stdout, stderr, and exit code as `devctl build <app>`.
- R-EZ7H-7RRP: `space`, `secrets`, `deploy`, and `restore`, invoked without `--account` and without `--help` or `-h` among the command's own arguments, MUST each write exactly the three lines `devctl: --account is required`, an empty line, and `see 'devctl <command> --help' for usage` — where `<command>` is the invoked command's own name — to stderr, write nothing to stdout, call `Deps.Cloud` not at all, and exit 2, and this check MUST precede every other check of the command's own arguments, verified at least by invoking each of the four with no further arguments and with arguments that are themselves invalid.
- R-DE6E-B0WV: `devctl --account <name> version` and `devctl --account=<name> version` MUST each produce the same stdout, stderr, and exit code as `devctl version`.
- R-UYZW-0UKR: `devctl --account <name> version` and `devctl --account=<name> version` MUST call `Deps.Cloud` not at all, verified with a recording fake `Deps.Cloud` that is left with no call.
- R-DFEA-OSNK: Package `internal/cli` MUST declare a package-level `var version string` whose value matches `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`.
- R-DGM7-2KE9: `devctl version`, `devctl -V`, and `devctl --version` MUST each print exactly the version string followed by a single newline to stdout, write nothing to stderr, and exit 0.
- R-T9YB-VTNZ: `devctl version --help` and `devctl version -h` MUST print exactly this text to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl version

  Print the version.
  ```
- R-DJ1Z-U3VN: `devctl version` with any argument other than `--help` or `-h` MUST write exactly the three lines `devctl: version takes no arguments`, an empty line, and `see 'devctl version --help' for usage` to stderr, nothing to stdout, and exit 2.
- R-A1PK-F80D: With `Deps.EUID` equal to 0, every invocation MUST write the single line `devctl: must not run as root` to stderr, write nothing to stdout, and exit 3 whatever its arguments are, verified at least for no arguments, `--help`, `-h`, `--version`, `-V`, `version`, an unknown command, an unknown top-level option, `--account` without a value, and a well-formed invocation of each command of the top-level command set.
- R-A2XG-SZR2: `cli.Run` MUST return only `0` — success, including help and version — `1` — the command was well-formed but could not do what it was asked — `2` — a usage error, or a preflight check failed — or `3` — refused because `Deps.EUID` is 0 — verified by asserting of every `cli.Run` call in the test suite that its return value is one of those four.
- R-DMPO-ZF3Q: The first line of every diagnostic `devctl` writes to stderr MUST begin with `devctl: `, the usage text MUST never be written to stderr, and on success a command MUST write nothing to stderr.
- R-A45D-6RHR: A diagnostic that carries further detail — another program's output, or the next command to run — MUST write that detail to stderr after exactly one empty line following the `devctl: ` line, and MUST NOT prefix the detail's lines with `devctl: `.
