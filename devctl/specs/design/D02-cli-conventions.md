# D02-cli-conventions

The CLI owns argument dispatch, help, exit codes and diagnostics. The command
set now includes remove. Root refusal still precedes every argument, including
help. Command products stay on stdout; external diagnostic detail is visibly
quoted, including nested quotes and empty lines.

## REQUIREMENTS

- R-D4F7-8UZB: The top-level grammar MUST be `devctl [options] <command> [arguments]`, accepting exactly the options `-h`/`--help`, `-V`/`--version`, and `--account <name>`/`--account=<name>` before the command and no other top-level options.

- R-9T69-QTTI: An argument beginning with `-` that appears after the command MUST be passed to the command and MUST NOT be parsed as a top-level option, verified at least by `devctl version --help` printing the `version` usage text and exiting 0 and by `devctl version --account <name>` being a usage error.

- R-D82W-E67E: An invocation with no command MUST write exactly the three lines `devctl: no command given`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.

- R-D9AS-RXY3: An unknown command MUST write exactly the three lines `devctl: unknown command '<name>'`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.

- R-DAIP-5POS: An unknown top-level option MUST write exactly the three lines `devctl: unknown option '<option>'`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.

- R-9UE6-4LK7: `--account <name>` MUST take the following argument as the profile name whenever that argument neither begins with `-` nor names a command of the top-level command set, `--account=<name>` MUST take the text after the `=`, and the profile name MUST reach `Deps.Cloud` byte for byte with no trimming, case change, or other normalisation, verified with a fake `Deps.Cloud` that records the profile it is asked for.

- R-9VM2-IDAW: When `--account` appears more than once before the command, the last occurrence MUST be the profile name the command acts in, verified with a fake `Deps.Cloud` that records the profile it is asked for.

- R-9WTY-W51L: `--account` as the last argument, `--account` followed by an argument that begins with `-` or that names a command of the top-level command set, and `--account=` with an empty value MUST each write exactly the three lines `devctl: option '--account' requires a value`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.

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

- R-BYZH-IH0F: The top-level command set MUST be exactly `version`, `space`, `secrets`, `build`, `deploy`, `restore`, and `remove`.

- R-C07D-W8R4: For each of `space`, `secrets`, `build`, `deploy`, `restore`, and `remove`, `devctl <command> --help` and `devctl <command> -h` MUST print the usage text that command's own design declares, byte for byte, to stdout, write nothing to stderr, and exit 0.

- R-C1FA-A0HT: The commands that require `--account` MUST be exactly `space`, `secrets`, `deploy`, `restore`, and `remove`, and `devctl --account <name> build <app>` MUST produce the same stdout, stderr, and exit code as `devctl build <app>`.

- R-3PTI-JABB: `space`, `secrets`, `deploy`, `restore`, and `remove`, invoked without `--account` and without `--help` or `-h` among the command's own arguments, MUST each write exactly the three lines `devctl: --account is required`, an empty line, and `see 'devctl <command> --help' for usage` — where `<command>` is the invoked command's own name — to stderr, write nothing to stdout, call `Deps.Cloud` not at all, and exit 2, and this check MUST precede every other check of the command's own arguments, verified at least by invoking each of the five with no further arguments and with arguments that are themselves invalid.

- R-C3V3-1JZ7: `devctl --help` and `devctl -h` MUST print exactly this text, once, to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl [options] <command> [arguments]

  Manage the ikigenba platform from the developer's machine. Never run as root.

  Commands:
    version   print the version
    space     list, create, destroy, stop, start, initialise, and inspect spaces
    secrets   push and list an app's secrets for a space
    build     build one app into its deployable file
    deploy    put a built app file on a space
    remove    take an app off a space
    restore   put a space's app back from its backups

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

- R-C52Z-FBPW: A diagnostic with detail MUST separate its first line from that detail by exactly one empty line; each line originating from another program MUST carry one additional `> ` prefix, including blank lines and lines already quoted, while devctl-authored advice MUST remain unprefixed. No diagnostic MUST duplicate a report already delivered to stdout.

- R-C6AV-T3GL: Package `internal/seam` MUST export `QuoteOutput(text string) string`, returning an empty string for empty input and otherwise prefixing each line with `> ` after removing trailing newline characters; internal blank lines and all other bytes MUST be preserved.

- R-C7IS-6V7A: Command help MUST take precedence over command-local argument validation when `--help` or `-h` appears among that command’s arguments, subject to the root refusal; it MUST invoke neither cloud nor process runners, including the streaming runner.

- R-C8QO-KMXZ: Missing or empty option values for create, init and restore MUST be diagnosed before external access; a following argument beginning with `-` MUST not be consumed as such a value. Logs’ `--since` exception MUST follow its own declared grammar.

- R-GV4C-IOFR: The value of `internal/cli.version` MUST be initialized in source and MUST NOT be injected or replaced at build time, so a developer build and a release built from the same source report the same version string.
