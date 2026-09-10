# D2-cli-conventions

`opsctl` is a multi-command CLI with the grammar

```
opsctl [options] <command> [arguments]
```

where the options are only `-h`/`--help` and `-V`/`--version`, and each
command may have subcommands and arguments of its own. Parsing uses the
standard library `flag` package; options precede the command. Commands are
added by later designs; each addition re-mints the top-level usage text and
the command-set requirement below.

**Output contract.** Every command is written to be read by an agent over ssh
far more often than by a person. stdout carries only the answer: a value, a
list, a checklist line per step. There is no decoration, colour, or progress
output. Every diagnostic — an error or a warning — goes to stderr in the
ordinary Unix form: its first line is `opsctl: <message>`, and any further
detail follows after one blank line, unprefixed — the shape `git foo`
uses. The usage text is never written to stderr; a usage error names the
problem and points at `--help`:

```
$ opsctl bogus
opsctl: unknown command 'bogus'

see 'opsctl --help' for usage
```

**Exit codes.** Four, and the help text lists them, so an agent never needs
to be told out of band:

- `0` — success, including help and version.
- `1` — the operation failed: the command was well-formed but could not do
  what it was asked (a key not set, a file it could not write).
- `2` — usage error, or a preflight check failed: unknown command or option,
  malformed argument, missing prerequisite.
- `3` — refused: `opsctl` must run as root.

**Root check.** Every invocation that dispatches to a command's action first
checks `Deps.EUID == 0` and, if it is not, prints one line to stderr and exits
3 before touching anything. Help and version output are the only exemptions,
so `opsctl --help` and `opsctl config --help` work for any user.

**Usage text.** The top-level usage, byte for byte:

```
Usage: opsctl [options] <command> [arguments]

Operate the ikigenba platform host. Must run as root.

Commands:
  config    read and write the host configuration store
  dns       manage DNS records in the zones opsctl owns
  init      run the setup sequence behind one preflight
  version   print the version

Options:
  -h, --help     print this help
  -V, --version  print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a preflight check failed
  3  refused: opsctl must run as root

Run 'opsctl <command> --help' for details on a command.
```

Each command's own usage text is declared in that command's design and is
printed by `opsctl <command> --help`.

**Version.** A single `var version` in `internal/cli` carries the version
string in source, never injected at build time, so dev and released builds
report the same string. The spec fixes only its shape, a `v`-prefixed
`MAJOR.MINOR.PATCH`; its value is release data. `opsctl version`, `-V`, and
`--version` all print it bare on its own line.

## REQUIREMENTS

- R-N211-TYS0: The top-level grammar MUST be `opsctl [options] <command> [arguments]`, accepting exactly the options `-h`/`--help` and `-V`/`--version` before the command and no other top-level options.
- R-EALS-YXXE: The top-level command set MUST be exactly `config`, `dns`, `init`, and `version`.
- R-EBTP-CPO3: `opsctl --help` and `opsctl -h` MUST print the top-level usage text quoted above, byte for byte, exactly once to stdout, write nothing to stderr, and exit 0.
- R-CYFL-TDTY: An invocation with no command MUST write exactly the three lines `opsctl: no command given`, an empty line, and `see 'opsctl --help' for usage` to stderr, nothing to stdout, and exit 2.
- R-CZNI-75KN: An unknown command MUST write exactly the three lines `opsctl: unknown command '<name>'`, an empty line, and `see 'opsctl --help' for usage` to stderr, nothing to stdout, and exit 2.
- R-D0VE-KXBC: An unknown top-level option MUST write exactly the three lines `opsctl: unknown option '<option>'`, an empty line, and `see 'opsctl --help' for usage` to stderr, nothing to stdout, and exit 2.
- R-N9CG-4L86: Package `internal/cli` MUST declare a package-level `var version string` whose value matches `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`.
- R-NAKC-ICYV: `opsctl version`, `opsctl -V`, and `opsctl --version` MUST each print exactly the version string followed by a single newline to stdout, write nothing to stderr, and exit 0.
- R-NBS8-W4PK: Any invocation that would dispatch to a command's action with `Deps.EUID` not equal to 0 MUST write the single line `opsctl: must run as root` to stderr, write nothing to stdout, and exit 3 without reading or writing any file under `Deps.Root`.
- R-ND05-9WG9: `--help`, `-h`, `--version`, `-V`, `version`, and `<command> --help` MUST succeed with `Deps.EUID` not equal to 0.
- R-R0P9-H29Y: The first line of every diagnostic `opsctl` writes to stderr MUST begin with `opsctl: `, and the usage text MUST never be written to stderr.
- R-NGNU-F7OC: On success a command MUST write nothing to stderr.
