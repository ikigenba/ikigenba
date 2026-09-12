# D2-cli-conventions

`devctl` is a multi-command CLI with the grammar

```
devctl [options] <command> [arguments]
```

where the options are `-h`/`--help`, `-V`/`--version`, and `--account <name>`
(also spelled `--account=<name>`), and each command may have subcommands and
arguments of its own. Options precede the command; an option after the
command belongs to the command, and `version` takes none. Commands are added
by later designs; each addition re-mints the top-level usage text and the
command-set requirement below.

**`--account`** names the AWS shared-config profile a command acts in.
Nothing is derived from the name, and no command reads it yet; the design
that first reads it says what the profile is used for. When the option is
repeated the last occurrence wins, stated here in prose only because no
command can yet observe which won. The documented spellings are the two
above; whether a single-dash `-account` is accepted is neither promised nor
forbidden.

**Output contract.** Every command is written to be read by an agent far more
often than by a person. stdout carries only the answer: a value, a list, a
checklist line per step. There is no decoration, colour, or progress output.
Every diagnostic — an error or a warning — goes to stderr in the ordinary Unix
form: its first line is `devctl: <message>`, and any further detail follows
after one blank line, unprefixed — the shape `git foo` uses. The usage text is
never written to stderr; a usage error names the problem and points at
`--help`:

```
$ devctl bogus
devctl: unknown command 'bogus'

see 'devctl --help' for usage
```

**Exit codes.** Four, and the help text lists them, so an agent never needs
to be told out of band:

- `0` — success, including help and version.
- `1` — the operation failed: the command was well-formed but could not do
  what it was asked.
- `2` — usage error, or a preflight check failed: unknown command or option,
  malformed argument, missing prerequisite.
- `3` — refused: `devctl` must not run as root.

**Root check, first.** devctl runs as the developer, never as root: a root
process would write root-owned files into the developer's home and run ssh
under the wrong identity. So every invocation checks `Deps.EUID` before it
reports anything else. If the invocation would print help or the version it
is served regardless of uid, because those change nothing. Every other
invocation with `Deps.EUID == 0` — a real command, but also no command, an
unknown command or option, a missing `--account` value, or `version` with
stray arguments — is refused with one line and exit 3 before any usage error
is reported. The refusal comes first because a root user with a typo has two
things wrong, and running as root is the one to fix first. This is the
opposite order from `opsctl`, which must run as root and reports usage errors
before refusing.

**Usage text.** The top-level usage, byte for byte:

```
Usage: devctl [options] <command> [arguments]

Manage the ikigenba platform from the developer's machine. Never run as root.

Commands:
  version   print the version

Options:
  -h, --help          print this help
  -V, --version       print the version
  --account <name>    AWS shared-config profile to act in

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a preflight check failed
  3  refused: devctl must not run as root

Run 'devctl <command> --help' for details on a command.
```

Each command's own usage text is declared in that command's design and is
printed by `devctl <command> --help`. For `version`, declared here, byte for
byte:

```
Usage: devctl version

Print the version.
```

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
- R-D5N3-MMQ0: The top-level command set MUST be exactly `version`.
- R-D6V0-0EGP: `devctl --help` and `devctl -h` MUST print the top-level usage text quoted above, byte for byte, exactly once to stdout, write nothing to stderr, and exit 0.
- R-D82W-E67E: An invocation with no command MUST write exactly the three lines `devctl: no command given`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.
- R-D9AS-RXY3: An unknown command MUST write exactly the three lines `devctl: unknown command '<name>'`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.
- R-DAIP-5POS: An unknown top-level option MUST write exactly the three lines `devctl: unknown option '<option>'`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.
- R-DCYH-X966: `--account` as the last argument, and `--account=` with an empty value, MUST each write exactly the three lines `devctl: option '--account' requires a value`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.
- R-DE6E-B0WV: `devctl --account <name> version` and `devctl --account=<name> version` MUST each produce the same stdout, stderr, and exit code as `devctl version`.
- R-DFEA-OSNK: Package `internal/cli` MUST declare a package-level `var version string` whose value matches `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`.
- R-DGM7-2KE9: `devctl version`, `devctl -V`, and `devctl --version` MUST each print exactly the version string followed by a single newline to stdout, write nothing to stderr, and exit 0.
- R-DHU3-GC4Y: `devctl version --help` and `devctl version -h` MUST print the `version` usage text quoted above, byte for byte, to stdout, write nothing to stderr, and exit 0.
- R-DJ1Z-U3VN: `devctl version` with any argument other than `--help` or `-h` MUST write exactly the three lines `devctl: version takes no arguments`, an empty line, and `see 'devctl version --help' for usage` to stderr, nothing to stdout, and exit 2.
- R-DK9W-7VMC: With `Deps.EUID` equal to 0, every invocation that with a non-zero `Deps.EUID` would not exit 0 by printing the top-level usage text, the `version` usage text, or the version string MUST write the single line `devctl: must not run as root` to stderr, write nothing to stdout, and exit 3, verified at least for no command, an unknown command, an unknown top-level option, `--account` without a value, and `version` with a stray argument.
- R-DLHS-LND1: `-h`, `--help`, `-V`, `--version`, `version`, `version -h`, and `version --help`, each with and without a preceding `--account <name>`, MUST produce identical stdout, stderr, and exit code with `Deps.EUID` equal to 0 and with `Deps.EUID` not equal to 0.
- R-DMPO-ZF3Q: The first line of every diagnostic `devctl` writes to stderr MUST begin with `devctl: `, the usage text MUST never be written to stderr, and on success a command MUST write nothing to stderr.
