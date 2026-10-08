# D02-cli-conventions

The CLI owns argument dispatch, help, exit codes and diagnostics. devctl has
no configuration and no top-level option beyond help and version: the platform
is one root domain in one account, and every command that touches the cloud
learns the root and its region from the checkout's root file (D04,
`internal/checkout`), never from an option, the environment, or the developer's
machine. The command set now includes `apex`, `golden`, `seed`, and
`rollback`.

The superuser refusal — the check that devctl is not running with effective
uid 0 — precedes every argument, help and version included. Older requirement
texts in other designs call this check "the root refusal"; since "root" now
names the root domain, the same check is called the superuser refusal
everywhere it is re-minted, and D02 is where the term is defined.

Layering. `cli.Run` parses the top-level grammar, applies the superuser
refusal, dispatches to the command package, and is the only writer of stderr.
A command package returns errors; it never prints a diagnostic. The root file
is read by the command, not by `cli.Run`, so that the command's own usage
errors come first and the read precedes the operand parse and the first cloud
call — that ordering is D04's rule (R-N1LD-IX1I, and R-ST4K-APZN for the
operand) and is not restated here. What `cli.Run` prints for a returned error
is likewise stated once: the checkout and root-file errors map to a single
`devctl: <message>` line and exit 2 under D04 R-EX3T-CTOO, and every error
carrying `ExitCode()` (and optionally `Detail()`) under D05 R-D4G2-IO81. D02
states which commands read the file, that help, version and top-level usage
errors touch nothing, and that `cli.Run` alone speaks on stderr. `build` does
not read the root file; that is D08's rule (R-RBMT-WHDT, and R-G9VS-NNCG for the suite build).

Command products stay on stdout; external diagnostic detail is visibly quoted,
including nested quotes and empty lines.

## REQUIREMENTS

- R-OFH7-TLNF: The top-level grammar MUST be `devctl [options] <command> [arguments]`, accepting exactly the options `-h`/`--help` and `-V`/`--version` before the command and no other top-level options, verified at least by `devctl --account ikigenba.dev version` writing exactly the three lines `devctl: unknown option '--account'`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exiting 2.

- R-OHX0-L54T: An argument beginning with `-` that appears after the command MUST be passed to the command and MUST NOT be parsed as a top-level option, verified at least by `devctl version --help` printing the `version` usage text and exiting 0 and by `devctl version --bogus` being a usage error.

- R-D82W-E67E: An invocation with no command MUST write exactly the three lines `devctl: no command given`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.

- R-D9AS-RXY3: An unknown command MUST write exactly the three lines `devctl: unknown command '<name>'`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.

- R-DAIP-5POS: An unknown top-level option MUST write exactly the three lines `devctl: unknown option '<option>'`, an empty line, and `see 'devctl --help' for usage` to stderr, nothing to stdout, and exit 2.

- R-R2LW-I8W3: The version string that `devctl version`, `devctl -V`, and `devctl --version` print MUST match `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`.

- R-DGM7-2KE9: `devctl version`, `devctl -V`, and `devctl --version` MUST each print exactly the version string followed by a single newline to stdout, write nothing to stderr, and exit 0.

- R-T9YB-VTNZ: `devctl version --help` and `devctl version -h` MUST print exactly this text to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl version

  Print the version.
  ```

- R-DJ1Z-U3VN: `devctl version` with any argument other than `--help` or `-h` MUST write exactly the three lines `devctl: version takes no arguments`, an empty line, and `see 'devctl version --help' for usage` to stderr, nothing to stdout, and exit 2.

- R-OJ4W-YWVI: The superuser refusal: with `Deps.EUID` equal to 0, every invocation MUST write the single line `devctl: must not run as root` to stderr, write nothing to stdout, call `Deps.Cloud` not at all, pass no `seam.Cmd` to `Deps.Exec` or `Deps.Stream`, and exit 3 whatever its arguments are, verified at least for no arguments, `--help`, `-h`, `--version`, `-V`, `version`, an unknown command, an unknown top-level option, `space list` with `Deps.Dir` outside any git checkout, and a well-formed invocation of each command of the top-level command set.

- R-A2XG-SZR2: `cli.Run` MUST return only `0` — success, including help and version — `1` — the command was well-formed but could not do what it was asked — `2` — a usage error, or a preflight check failed — or `3` — refused because `Deps.EUID` is 0 — verified by asserting of every `cli.Run` call in the test suite that its return value is one of those four.

- R-DMPO-ZF3Q: The first line of every diagnostic `devctl` writes to stderr MUST begin with `devctl: `, the usage text MUST never be written to stderr, and on success a command MUST write nothing to stderr.

- R-OP8E-VRKZ: `devctl --help`, `devctl -h`, `devctl --version`, `devctl -V`, `devctl version`, and every invocation that fails with a top-level usage error — no command, an unknown command, or an unknown top-level option — MUST call `Deps.Cloud` not at all and MUST pass no `seam.Cmd` to `Deps.Exec` or `Deps.Stream`, so that none of them finds a checkout or reads the root file, verified with recording fakes left with no call and `Deps.Dir` set to a directory that is not inside a git checkout.

- R-R3TS-W0MS: When a command fails, `cli.Run` MUST write to `stderr` only the diagnostic for the error the command returned, verified at least by `devctl space list` with `Deps.Dir` outside any git checkout, in a temporary checkout that has no root file, and in one whose root file holds `{"domain": "ikigenba.dev"}` each writing exactly one line to stderr and nothing to stdout.

- R-C52Z-FBPW: A diagnostic with detail MUST separate its first line from that detail by exactly one empty line; each line originating from another program MUST carry one additional `> ` prefix, including blank lines and lines already quoted, while devctl-authored advice MUST remain unprefixed. No diagnostic MUST duplicate a report already delivered to stdout.

- R-C6AV-T3GL: Package `internal/seam` MUST export `QuoteOutput(text string) string`, returning an empty string for empty input and otherwise prefixing each line with `> ` after removing trailing newline characters; internal blank lines and all other bytes MUST be preserved.

- R-1MN4-VC6Y: Command help MUST take precedence over command-local argument validation when `--help` or `-h` appears among that command’s arguments, subject to the root refusal; it MUST invoke neither cloud nor process runners, including the streaming runner.

- R-C8QO-KMXZ: Missing or empty option values for create, init and restore MUST be diagnosed before external access; a following argument beginning with `-` MUST not be consumed as such a value. Logs’ `--since` exception MUST follow its own declared grammar.

- R-SRWN-WY8Y: The binary built from `./cmd/devctl` by `go build` with no flags, run with the single argument `--version` as a non-root user, MUST print to stdout exactly what `cli.Run` prints to stdout for `--version`, write nothing to stderr, and exit 0.

- R-UFOF-APUG: The top-level command set MUST be exactly `version`, `space`, `secrets`, `build`, `deploy`, `rollback`, `restore`, `remove`, `golden`, `seed`, and `apex`.

- R-UGWB-OHL5: For each of `space`, `secrets`, `build`, `deploy`, `rollback`, `restore`, `remove`, `golden`, `seed`, and `apex`, `devctl <command> --help` and `devctl <command> -h` MUST print the usage text that command's own design declares, byte for byte, to stdout, write nothing to stderr, and exit 0.

- R-UI48-29BU: The commands that read the root file MUST be exactly `space`, `secrets`, `deploy`, `rollback`, `restore`, `remove`, `golden`, `seed`, and `apex`, and each of them MUST make every call to `Deps.Cloud` with the `Domain` of the `checkout.RootFile` it read as the profile and that file's `Region` as the region, taking neither value from anywhere else, verified with a recording fake `Deps.Cloud` by a well-formed invocation of each of the nine in a temporary checkout whose root file holds `{"domain": "example.test", "region": "eu-west-1"}` leaving the fake with calls whose profile is exactly `example.test` and whose region is exactly `eu-west-1`.

- R-UJC4-G12J: `devctl --help` and `devctl -h` MUST print exactly this text, once, to stdout, write nothing to stderr, and exit 0:

  ```
  Usage: devctl [options] <command> [arguments]

  Manage the platform from the developer's machine. Never run as root.

  Commands:
    version   print the version
    space     list, create, destroy, stop, start, initialise, and inspect spaces
    secrets   push and list an app's secrets for a space
    build     build the suite or one app into a deployable file
    deploy    put a release or a built app file on a space
    rollback  put a space back on the release it ran before
    remove    take an app off a space
    restore   put a space's app back from its backups
    golden    capture a space's data as a named golden set
    seed      give a space a golden set's or another space's data
    apex      point the root domain at one app on one space

  Options:
    --help              print this help
    --version           print the version

  Exit codes:
    0  success
    1  the operation failed
    2  usage error, or a preflight check failed
    3  refused: devctl must not run as root

  Run 'devctl <command> --help' for details on a command.
  ```
