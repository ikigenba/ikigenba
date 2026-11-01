# D02-cli

Package `internal/cli` owns the top-level grammar, the top-level usage text, the `version` and `help` commands, the refusals for a missing or mistyped command or option, the root refusal, the exit codes and the form of every diagnostic. It also states once the frame every command's own arguments are parsed in, so the documents for `configure` (D03), `push` (D10), `install` (D11) and `status` (D12) state only what is particular to their command and rely on this one for the rest.

turns runs under a person's own account, by hand and from that account's user timer, and never as root: it reads the session files of the account it runs as and pushes them under that account's token. So the root refusal comes before anything else, before a single argument is looked at, help and version included. Every other requirement on `cli.Run`, here and in the other documents, describes a call made as an ordinary user.

`cli.Run` is the whole program: `cmd/turns` (D01) passes it the process's arguments without the program name, its standard streams and the real `seam.Deps`, and exits with what it returns. Everything a test needs to prove the CLI goes through that call with injected `Deps`.

The order in every invocation is: the root refusal; then the top-level options, read only before the command word, where help wins over everything else among them, and otherwise an option turns does not have is refused, beside `--version` too; then the command word; then the command's arguments in the shared frame, where `-h` or `--help` anywhere wins over everything else the command would check; then the command itself. An argument after the command word is never a top-level option, so `turns push --version` is push's unknown option, not the version.

The version is data. The release machinery sets `Version` in `internal/cli/run.go`; no requirement and no test names its value, and a test checks only its shape.

The stories give no usage text for `version` and `help` and no refusal for either given an argument; the texts here are authored in this document, in the style of the other commands' usage texts, and the refusals follow the shared frame.

stdout carries only the product: a value, a report, a usage text. Every diagnostic goes to stderr as one `turns: ` line, then, when there is detail, exactly one empty line and the detail. Another program's output in that detail is quoted with `seam.QuoteOutput` (D01), and detail turns writes itself is not. A usage error's detail is the one line pointing at the usage text that applies; the usage text itself is never written to stderr.

A positional argument a command does not take is refused in the words S4 gives for push: a command that takes none says `<command> takes no arguments`, which also covers `version` and `help`; only a command that takes some positional arguments and is given more names the extra one, as `unexpected argument '<arg>'`.

## REQUIREMENTS

- R-V7QC-7CRL: Package `internal/cli` MUST export `func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps seam.Deps) int`, where `args` are the command-line arguments without the program name.

- R-V8Y8-L4IA: Package `internal/cli` MUST export the untyped integer constants `ExitOK = 0`, `ExitFailed = 1`, `ExitUsage = 2`, `ExitRoot = 3` and `ExitNotConfigured = 4`.

- R-VA64-YW8Z: `cli.Run` MUST return only `ExitOK`, `ExitFailed`, `ExitUsage`, `ExitRoot` or `ExitNotConfigured`, verified by asserting of every `cli.Run` call in the test suite that its return value is one of those five.

- R-LR9B-R2VA: Package `internal/cli` MUST export the string constant `Version`.

- R-VCLX-QFQD: `cli.Version` MUST match `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`.

- R-VDTU-47H2: `turns version` and `turns --version` MUST each write exactly `cli.Version` followed by one LF to stdout, write nothing to stderr, and make `cli.Run` return `ExitOK`.

- R-VF1Q-HZ7R: The top-level grammar MUST be `turns [options] <command> [arguments]`, accepting exactly the options `-h`, `--help` and `--version` before the command and no other top-level option, verified at least by `turns -V` writing exactly the three lines `turns: unknown option '-V'`, an empty line, and `see 'turns --help' for usage` to stderr, writing nothing to stdout, and returning `ExitUsage`.

- R-VG9M-VQYG: The top-level command set MUST be exactly `configure`, `install`, `push`, `status`, `version` and `help`.

- R-VHHJ-9IP5: An argument that follows the command word MUST be passed to the command and MUST NOT be read as a top-level option, verified at least by `turns version --help` writing the `version` usage text of R-VUWF-GZUS and by `turns version --version` writing exactly the three lines `turns: unknown option '--version'`, an empty line, and `see 'turns version --help' for usage` to stderr, writing nothing to stdout, and returning `ExitUsage`.

- R-VIPF-NAFU: `turns --help` and `turns -h` and `turns help` MUST each write exactly this text to stdout, ending in exactly one LF after its last line, write nothing to stderr, and make `cli.Run` return `ExitOK`:

  ```
  Usage: turns [options] <command> [arguments]

  Push this account's agent session turns to an ikigenba space. Never run as root.

  Commands:
    configure store the space and token turns pushes with
    install   install the user timer that pushes every five minutes
    push      push new turns to the space
    status    show what is here and what the space holds
    version   print the version
    help      print this help

  Options:
    -h, --help     print this help
    --version      print the version

  Exit codes:
    0  success
    1  the operation failed
    2  usage error
    3  refused: turns must not run as root
    4  refused: not configured

  Run 'turns <command> --help' for details on a command.
  ```

- R-LSH8-4ULZ: When `-h` or `--help` appears among the top-level options, `cli.Run` MUST write the top-level usage text of R-VIPF-NAFU to stdout, write nothing to stderr, and return `ExitOK`, whatever other options appear among them in either order, `--version` and options turns does not have included, and whatever command word and arguments follow them, the command being neither run nor checked, verified at least by `turns --version --help`, `turns -h --version`, `turns --help --bogus`, `turns --bogus --help`, `turns -h --bogus`, `turns --bogus -h`, `turns --help push extra` and `turns --help bogus`.

- R-LTP4-IMCO: When `--version` appears among the top-level options and every other top-level option is also `--version`, `cli.Run` MUST write exactly `cli.Version` followed by one LF to stdout, write nothing to stderr, and return `ExitOK`, whatever command word and arguments follow, the command being neither run nor checked, verified at least by `turns --version push` and `turns --version bogus extra`.

- R-VMD4-SLNX: An invocation with an empty argument list MUST write exactly the three lines `turns: no command given`, an empty line, and `see 'turns --help' for usage` to stderr, write nothing to stdout, and return `ExitUsage`.

- R-VNL1-6DEM: An unknown command MUST write exactly the three lines `turns: unknown command '<name>'`, an empty line, and `see 'turns --help' for usage` to stderr, `<name>` being the command word as given, write nothing to stdout, and return `ExitUsage`, verified at least by `turns bogus`.

- R-LUX0-WE3D: When neither `-h` nor `--help` appears among the top-level options, an argument before the command word that begins with `-` and is not `--version` MUST write exactly the three lines `turns: unknown option '<option>'`, an empty line, and `see 'turns --help' for usage` to stderr, `<option>` being that argument as given, write nothing to stdout, and return `ExitUsage`, also when `--version` appears before or after it, verified at least by `turns --bogus`, `turns --version --bogus` and `turns --bogus --version`.

- R-LW4X-A5U2: `turns --help`, `turns -h`, `turns help`, `turns --version`, `turns version`, and every invocation refused with a top-level usage error (no command, an unknown command, an unknown top-level option) MUST leave a recording fake `Deps.Exec` with no call and a fake `Deps.HTTP` transport with no request, read nothing from `stdin`, and leave unchanged every file and directory under the temporary tree the test injects, that is the directories `Deps.Getenv` names as `HOME`, `XDG_CONFIG_HOME` and `XDG_STATE_HOME` and the directory `Deps.Etc` names.

- R-CIE0-1WAF: Every requirement on `cli.Run` in this document and in every other turns design document, except R-LXCT-NXKR and R-LYKQ-1PBG, MUST hold for a `Deps.EUID` other than 0 and MUST NOT be read as stating anything about a call with `Deps.EUID` equal to 0, which R-LXCT-NXKR and R-LYKQ-1PBG alone govern; every test of such a requirement MUST inject a non-zero `Deps.EUID`.

- R-LXCT-NXKR: With `Deps.EUID` equal to 0, `cli.Run` MUST write exactly the one line `turns: must not run as root` to stderr, write nothing to stdout, and return `ExitRoot`, whatever its arguments are, verified at least for no arguments, `--help`, `-h`, `--version`, `help`, `version`, `bogus`, `--bogus`, `push --help`, `push --bogus`, `configure us.ikigenba.dev extra`, and a well-formed invocation of each of `configure`, `install`, `push` and `status`.

- R-LYKQ-1PBG: With `Deps.EUID` equal to 0, `cli.Run` MUST leave a recording fake `Deps.Exec` with no call and a fake `Deps.HTTP` transport with no request, read nothing from `stdin`, and leave unchanged every file and directory under the temporary tree the test injects, that is the directories `Deps.Getenv` names as `HOME`, `XDG_CONFIG_HOME` and `XDG_STATE_HOME` and the directory `Deps.Etc` names, for each argument list of R-LXCT-NXKR.

- R-VUWF-GZUS: `turns version --help` and `turns version -h` MUST each write exactly this text to stdout, ending in exactly one LF after its last line, write nothing to stderr, and return `ExitOK`:

  ```
  Usage: turns version

  Print the version turns was released as.

  Options:
    -h, --help   print this help
  ```

- R-VW4B-URLH: `turns help --help` and `turns help -h` MUST each write exactly this text to stdout, ending in exactly one LF after its last line, write nothing to stderr, and return `ExitOK`:

  ```
  Usage: turns help

  Print the usage text, the same as 'turns --help'.

  Options:
    -h, --help   print this help
  ```

- R-LZSM-FH25: For every command, when an argument after the command word is exactly `-h` or `--help`, wherever it stands among that command's arguments, `cli.Run` MUST write that command's usage text, as R-VUWF-GZUS, R-VW4B-URLH or the command's own design declares it, to stdout, write nothing to stderr, and return `ExitOK`, whatever else those arguments hold, verified at least for each of `configure`, `install`, `push`, `status`, `version` and `help` with each of `-h` and `--help` after an unknown option (as `turns push --bogus --help`) and after an unexpected positional argument (as `turns push extra -h`).

- R-M10I-T8SU: For every command, an invocation that R-LZSM-FH25 answers with the command's usage text MUST leave a recording fake `Deps.Exec` with no call and a fake `Deps.HTTP` transport with no request, read nothing from `stdin`, and leave unchanged every file and directory under the temporary tree the test injects, that is the directories `Deps.Getenv` names as `HOME`, `XDG_CONFIG_HOME` and `XDG_STATE_HOME` and the directory `Deps.Etc` names, also when `Deps.Getenv` supplies no configuration and no session files.

- R-W27T-RMAY: For every command, an argument after the command word that begins with `-`, is not `-h` or `--help`, is not an option that command's design declares, and is not consumed as the value of such an option MUST, when neither `-h` nor `--help` is among the command's arguments, write exactly the three lines `turns: unknown option '<option>'`, an empty line, and `see 'turns <command> --help' for usage` to stderr, `<option>` being that argument as given, write nothing to stdout, and return `ExitUsage`, verified at least for each of `configure`, `install`, `push`, `status`, `version` and `help` given `--bogus`.

- R-W3FQ-5E1N: For a command whose design declares no positional argument (`install`, `push`, `status`, `version`, `help`), an argument after the command word that does not begin with `-` and is not consumed as the value of a declared option MUST, when neither `-h` nor `--help` is among the command's arguments, write exactly the three lines `turns: <command> takes no arguments`, an empty line, and `see 'turns <command> --help' for usage` to stderr, write nothing to stdout, and return `ExitUsage`, verified at least for each of those five given `extra`.

- R-W4NM-J5SC: For a command whose design declares positional arguments (`configure`), an argument after the command word that does not begin with `-`, is not consumed as the value of a declared option, and comes after every positional argument the command takes MUST, when neither `-h` nor `--help` is among the command's arguments, write exactly the three lines `turns: unexpected argument '<arg>'`, an empty line, and `see 'turns <command> --help' for usage` to stderr, `<arg>` being that argument as given, write nothing to stdout, and return `ExitUsage`, verified at least by `turns configure us.ikigenba.dev extra`.

- R-W5VI-WXJ1: When a command's arguments hold more than one argument refused by R-W27T-RMAY, R-W3FQ-5E1N or R-W4NM-J5SC, `cli.Run` MUST write the refusal for the first of them in argument order and no other, verified at least by `turns push extra --bogus` writing the `push takes no arguments` refusal and `turns push --bogus extra` writing the `unknown option '--bogus'` refusal.

- R-M28F-70JJ: For every command, an invocation refused by R-W27T-RMAY, R-W3FQ-5E1N or R-W4NM-J5SC MUST leave a recording fake `Deps.Exec` with no call and a fake `Deps.HTTP` transport with no request, read nothing from `stdin`, and leave unchanged every file and directory under the temporary tree the test injects, that is the directories `Deps.Getenv` names as `HOME`, `XDG_CONFIG_HOME` and `XDG_STATE_HOME` and the directory `Deps.Etc` names.

- R-M3GB-KSA8: For every command, `cli.Run` MUST apply the checks on that command's arguments in this order: a `-h` or `--help` anywhere among them answers with the command's usage text (R-LZSM-FH25); otherwise the frame's refusals (R-W27T-RMAY, R-W3FQ-5E1N, R-W4NM-J5SC, for the first offending argument as R-W5VI-WXJ1 says); only then the command's own validation and work, so that a frame refusal is written whatever that validation would have said and whatever configuration `Deps.Getenv` supplies, none included, verified at least by `turns configure BAD extra` writing the `unexpected argument 'extra'` refusal and by `turns push --bogus`, `turns push extra` and `turns status --bogus` with no configuration each writing their frame refusal and returning `ExitUsage`.

- R-W9J8-28R4: The first line of every diagnostic `cli.Run` writes to stderr MUST begin with `turns: `, and every line it writes to stderr MUST end in LF, verified by asserting of every `cli.Run` call in the test suite whose stderr is non-empty that its first line begins with `turns: ` and that stderr ends in LF.

- R-WAR4-G0HT: A diagnostic with detail MUST separate its first line from that detail by exactly one empty line, and a diagnostic without detail MUST be exactly its one first line, verified by asserting of every `cli.Run` call in the test suite whose stderr holds more than one line that its second line is empty and its third is not.

- R-WBZ0-TS8I: When a diagnostic's detail carries another program's output, that output MUST appear in the detail exactly as `seam.QuoteOutput` returns it for that output, while detail turns writes itself, including the `see 'turns ... --help' for usage` line of a usage error, MUST carry no `> ` prefix.

- R-WD6X-7JZ7: `cli.Run` MUST never write a usage text to stderr, verified by asserting of every `cli.Run` call in the test suite that no line of its stderr begins with `Usage: turns`.

- R-WEET-LBPW: When `cli.Run` returns `ExitOK` it MUST have written nothing to stderr, verified by asserting it of every `cli.Run` call in the test suite.

- R-929V-QNBT: `cli.Run` MUST write only product to stdout: no line it writes there may begin with `turns: ` or hold the ESC byte (0x1B), verified by asserting it of every `cli.Run` call in the test suite.
