# D02-cli

D02 owns how sandbox reads its command line and how it speaks: the grammar of every command, every help text, every usage error, the version, the exit codes, the refusal to run as root, and the shapes of the diagnostics every command shares. What a well-formed command then does belongs to its own document (D04 `up` and `url`, D07 `down`, `wipe` and `ls`, D08 `status` and `logs`, D09 `token`); those documents name the action text a shared diagnostic carries and never restate a help text or a usage error.

The grammar is `sandbox [options] <command> [arguments]`. sandbox reads the arguments left to right and acts on the first argument that settles the outcome: a help or version option prints and succeeds, a malformed argument is a usage error, and anything after that argument is never looked at. Before the command, the only options are help and version. After it, each command accepts `-h` and `--help`, `logs` also accepts `-n`/`--lines <count>` and `-f`/`--follow`, and options and operands may come in any order. An argument beginning with `-` is always an option where an option may stand, so `-hV`, `--`, `-n5` and `--lines=5` are each one unknown option: short options do not bundle, there is no end-of-options marker, and an option's value is always the next argument. The grammar is kept to exactly what the stories use.

A usage error is three lines on stderr: the message, an empty line, and a trailer pointing at the relevant help. The usage text itself never goes to stderr. All parsing happens before anything else, so a usage error is reported the same way whoever runs sandbox; only a well-formed command line reaches the root refusal, and help and version answer as root because they only print. None of help, version or a usage error looks at the environment, runs a program, reads stdin or touches a file.

A command that fails because another program failed says so in one shape: the action, the exit status, then that program's combined output quoted line by line with `> `, then any advice sandbox adds. A program that could not be run at all, and a file sandbox could not read or write, each have a one-line shape; a runner's error text, and every value typed on the command line that a diagnostic echoes, is written as a printed name, its control characters escaped and every other byte unchanged, so it stays one line. The printed name is declared here and used by every document's diagnostics. Every path a file error names is free of control characters because D03 refuses a worktree or root that holds one.

A `logs` count must be a positive whole number no larger than 2147483647, the largest count `journalctl` accepts; a larger one is a usage error like any other bad value, so journalctl is never asked for a count it would refuse.

## REQUIREMENTS

- R-Z3GN-AWBG: The commands MUST be exactly `up`, `down`, `wipe`, `ls`, `url`, `status`, `logs`, `token` and `version`, the first argument that is not an option naming the command, verified by `<command> --help` returning 0 for each of the nine and by `help`, `start` and `list` each being the unknown-command usage error.

- R-Z4OJ-OO25: Before the command, the options MUST be exactly `-h`, `--help`, `-V` and `--version`, verified by each of the four returning 0 and by `--bogus`, `-v` and `--lines` before the command each being the unknown-option usage error with the top-level trailer.

- R-Z5WG-2FSU: `cli.Run` MUST read its arguments left to right and act on the first argument that settles the outcome, ignoring every argument after it, verified at least by `--help bogus` and `-V --bogus` succeeding as help and version, `--bogus --help` being the unknown-option usage error for `--bogus`, `up --help now` printing the `up` help, `up now --help` being the `up` takes-no-arguments usage error, `down wip --help` printing the `down` help, `down a b --help` being the `down` at-most-one-name usage error, and `token bogus --help` being the unknown-token-subcommand usage error.

- R-YD16-JZIC: Every argument beginning with `-` that stands where an option may stand, other than an option the command declares, MUST be the unknown-option usage error naming the argument as typed, written as a printed name (R-MBDF-W0VP): short options never bundle, `--` is not an end-of-options marker, and no option takes its value inside the same argument, verified at least by `-hV`, `--` and `-` before the command, `-hV`, `-V` and `--version` after `up`, and `-n5`, `--lines=5`, `-fn` and `--` after `logs`.

- R-Z8C8-TZA8: A usage error MUST write to stderr exactly three lines, each ending in a newline: `sandbox: <message>`, an empty line, and the trailer; MUST write nothing to stdout; and MUST make `cli.Run` return 2. The trailer MUST be `see 'sandbox --help' for usage` for an error found before the command is known, `see 'sandbox token --help' for usage` for an error inside `token` or `token set`, and `see 'sandbox <command> --help' for usage` for an error inside any other command.

- R-YBTA-67RN: Every diagnostic that echoes a value typed on the command line (a command, an option, an option's value, an operand, a name given to `down` or `wipe`, an app given to `logs`) MUST write that value as a printed name (R-MBDF-W0VP), each byte below 0x20 and the byte 0x7F written as `\x` and two lowercase hexadecimal digits and every byte from 0x80 up written unchanged, so the line holding it stays one line, verified at least by `bo`, a newline and `gus` writing `sandbox: unknown command 'bo\x0agus'`, by `up` then `--x`, a tab and `y` writing `sandbox: unknown option '--x\x09y'`, by `logs -n` then `1`, a newline and `2` writing `sandbox: -n needs a positive whole number, not '1\x0a2'`, by `token` then `a` and the byte 0x7F writing `sandbox: unknown token subcommand 'a\x7f'`, and by `down` then `a`, a newline and `b` writing `sandbox: no sandbox 'a\x0ab'`, each as one line, and by `down` then `caf` followed by the bytes 0xC3 0xA9 writing `sandbox: no sandbox 'caf`, the bytes 0xC3 0xA9, `'` and a newline.

- R-MBDF-W0VP: A name written as a printed name MUST be its bytes unchanged, except that each byte below 0x20 and the byte 0x7F MUST be written as `\x` and two lowercase hexadecimal digits, so a diagnostic holding one stays one line, verified at least through the unusable-app-name diagnostic by names holding a newline, a tab and the byte 0x7F being written with `\x0a`, `\x09` and `\x7f`.

- R-Z9K5-7R0X: `cli.Run` with no arguments MUST be the usage error with message `no command given` and the top-level trailer.

- R-YE92-XR91: A first non-option argument that is not a command MUST be the usage error with message `unknown command '<argument>'` and the top-level trailer, the argument quoted as typed, written as a printed name (R-MBDF-W0VP), verified at least by `bogus` and `help`.

- R-YFGZ-BIZQ: An unknown option MUST be the usage error with message `unknown option '<argument>'`, the argument quoted as typed, written as a printed name (R-MBDF-W0VP), with the top-level trailer before the command and the command's trailer after it, verified at least by `--bogus` and `-hV` at the top level and `--bogus` after each of the nine commands and after `token set`.

- R-ZEFQ-QTZP: An operand given to `up`, `url`, `ls`, `status` or `version` MUST be the usage error with message `<command> takes no arguments` and that command's trailer, verified at least by `up now`, `url dummy`, `ls wip`, `status dummy` and `version x`.

- R-ZFNN-4LQE: A second operand given to `down` or `wipe` MUST be the usage error with message `<command> takes at most one name` and that command's trailer, verified by `down wip other` and `wipe wip other`.

- R-ZGVJ-IDH3: A second operand given to `logs` MUST be the usage error with message `logs takes at most one app` and the `logs` trailer, verified by `logs auth dummy`.

- R-YGOV-PAQF: A first operand of `token` other than `set` MUST be the usage error with message `unknown token subcommand '<operand>'` and the `token` trailer, the operand quoted as typed, written as a printed name (R-MBDF-W0VP), verified at least by `token bogus`.

- R-ZJBC-9WYH: An operand given after `token set` MUST be the usage error with message `token set takes no arguments` and the `token` trailer, verified by `token set ikp_3f9a2c7e1b4d8f60a5c2e9b7d4f1a8c3`.

- R-ZKJ8-NOP6: For `logs`, the argument after `-n` or `--lines` MUST be taken as that option's value whatever it is, even when it begins with `-`, verified at least by `logs -n -5` being the bad-value usage error for `-5` and `logs -n --help` being the bad-value usage error for `--help`.

- R-ZLR5-1GFV: `-n` or `--lines` as the last argument of `logs` MUST be the usage error with message `<option> needs a value`, the option named exactly as typed, and the `logs` trailer, verified by `logs -n` and `logs --lines`.

- R-YHWS-32H4: A value of `-n` or `--lines` MUST be accepted exactly when it is one or more ASCII digits not all `0` whose number, read in decimal, is at most 2147483647, leading zeros allowed; any other value MUST be the usage error with message `<option> needs a positive whole number, not '<value>'`, the option and the value each as typed, the value written as a printed name (R-MBDF-W0VP), and the `logs` trailer, verified at least by `-n 2147483647` and `-n 02147483647` being accepted and by `-n x`, `--lines x`, `-n 0`, `-n 00`, `-n -5`, `-n +5`, `-n 1.5`, `-n ' 5'`, `-n ''`, `-n 2147483648`, `--lines 2147483648` and `-n 12345678901234567890` each being this usage error.

- R-0WMO-FPD8: Two well-formed `logs` command lines that denote the same app, the same count and the same follow MUST ask `Deps.Stream` for identical `seam.Cmd` values, where the app is the operand if any, the count is the value of the last `-n` or `--lines` given with leading zeros removed and is `100` when neither is given, follow is whether `-f` or `--follow` appears, and order among options and the operand does not matter; verified at least by `logs -n 20 dummy`, `logs --lines 20 dummy`, `logs dummy -n 20`, `logs -n 5 -n 20 dummy` and `logs -n 020 dummy` agreeing, `logs` agreeing with `logs -n 100`, and `logs -f` agreeing with `logs --follow`. The exact `seam.Cmd` is D08's.

- R-IZPL-JMUM: Two well-formed `logs` command lines that differ in the app, the count or follow (as defined for their equivalence) MUST ask `Deps.Stream` for different `seam.Cmd` values, verified at least by `logs -n 20` against `logs -n 21`, `logs -f` against `logs`, and `logs dummy` against `logs auth`, in a sandbox whose registry entry records both apps.

- R-ZPEU-6RNY: `--help` and `-h` before the command MUST write exactly the following text to stdout, each line ending in a newline, write nothing to stderr, and make `cli.Run` return 0:

  ```
  Usage: sandbox [options] <command> [arguments]

  Run the whole suite from this worktree on this machine. Never run as root.

  Commands:
    up        build every app and start the sandbox
    down      stop a sandbox and keep its data
    wipe      delete a stopped sandbox's data
    ls        list every known sandbox
    url       print the sandbox's URLs
    status    show the state of each unit
    logs      print the sandbox's journal
    token     print or store the sandbox's bearer token
    version   print the version

  Options:
    -h, --help     print this help
    -V, --version  print the version

  Exit codes:
    0  success
    1  the operation failed
    2  usage error, or a precondition was not met
    3  refused: sandbox must not run as root

  Run 'sandbox <command> --help' for details on a command.
  ```

- R-ZQMQ-KJEN: `up --help` and `up -h` MUST write exactly the following text to stdout, each line ending in a newline, write nothing to stderr, and make `cli.Run` return 0:

  ```
  Usage: sandbox up

  Build every app in this worktree and start its sandbox, or redeploy it if it
  is already up. Prints one URL per app.

  Options:
    -h, --help  print this help
  ```

- R-ZRUM-YB5C: `url --help` and `url -h` MUST write exactly the following text to stdout, each line ending in a newline, write nothing to stderr, and make `cli.Run` return 0:

  ```
  Usage: sandbox url

  Print the URLs of this worktree's sandbox, as the last 'sandbox up' printed
  them.

  Options:
    -h, --help  print this help
  ```

- R-ZT2J-C2W1: `down --help` and `down -h` MUST write exactly the following text to stdout, each line ending in a newline, write nothing to stderr, and make `cli.Run` return 0:

  ```
  Usage: sandbox down [<name>]

  Stop a sandbox and remove its units and generated files, keeping its data,
  token and port. Without a name, the sandbox is this worktree's.

  Options:
    -h, --help  print this help
  ```

- R-ZUAF-PUMQ: `wipe --help` and `wipe -h` MUST write exactly the following text to stdout, each line ending in a newline, write nothing to stderr, and make `cli.Run` return 0:

  ```
  Usage: sandbox wipe [<name>]

  Delete a stopped sandbox's data, token and registry entry, freeing its port.
  Without a name, the sandbox is this worktree's.

  Options:
    -h, --help  print this help
  ```

- R-ZVIC-3MDF: `ls --help` and `ls -h` MUST write exactly the following text to stdout, each line ending in a newline, write nothing to stderr, and make `cli.Run` return 0:

  ```
  Usage: sandbox ls

  List every known sandbox: its name, port, state and worktree, marking a
  worktree that no longer exists.

  Options:
    -h, --help  print this help
  ```

- R-ZWQ8-HE44: `status --help` and `status -h` MUST write exactly the following text to stdout, each line ending in a newline, write nothing to stderr, and make `cli.Run` return 0:

  ```
  Usage: sandbox status

  Show the state of nginx and of each app's service in this worktree's
  sandbox.

  Options:
    -h, --help  print this help
  ```

- R-ZZ61-8XLI: `logs --help` and `logs -h` MUST write exactly the following text to stdout, each line ending in a newline, write nothing to stderr, and make `cli.Run` return 0:

  ```
  Usage: sandbox logs [options] [<app>]

  Print the journal of this worktree's sandbox: nginx and every app, or only
  <app>.

  Options:
    -n, --lines <count>  print the last <count> lines (default 100)
    -f, --follow         keep printing new lines until interrupted
    -h, --help           print this help
  ```

- R-00DX-MPC7: `token --help`, `token -h`, `token set --help` and `token set -h` MUST each write exactly the following text to stdout, each line ending in a newline, write nothing to stderr, and make `cli.Run` return 0:

  ```
  Usage: sandbox token
         sandbox token set

  Print the bearer token stored for this worktree's sandbox, or store one read
  from stdin.

  Subcommands:
    set  store the bearer token read from stdin, replacing any earlier one

  Options:
    -h, --help  print this help
  ```

- R-01LU-0H2W: `version --help` and `version -h` MUST write exactly the following text to stdout, each line ending in a newline, write nothing to stderr, and make `cli.Run` return 0:

  ```
  Usage: sandbox version

  Print the version of sandbox.

  Options:
    -h, --help  print this help
  ```

- R-02TQ-E8TL: `version`, `--version` and `-V` MUST each write exactly `cli.Version` followed by one newline to stdout, write nothing to stderr, and make `cli.Run` return 0.

- R-041M-S0KA: Help, version and every usage error MUST NOT call `Deps.Getenv`, `Deps.Exec` or `Deps.Stream` and MUST NOT read stdin, verified with fakes that fail the test when called, a stdin that fails the test when read, and `Deps.Dir` a temporary directory outside any git checkout, for top-level help and version in every form, each command's help, `version`, and each usage error of this document.

- R-059J-5SAZ: With `Deps.EUID` 0, help and version in every form, top-level or a command's own, MUST write the same bytes and return the same exit code as with a non-zero `Deps.EUID`.

- R-06HF-JK1O: With `Deps.EUID` 0, every usage error MUST be reported exactly as with a non-zero `Deps.EUID`, returning 2, verified at least by `bogus`, `--bogus`, `up now` and `logs -n x`.

- R-0Z2H-78UM: With `Deps.EUID` 0, a well-formed command line that is neither help nor version MUST write exactly the line `sandbox: refusing to run as root` and a newline to stderr, write nothing to stdout, make `cli.Run` return 3, call none of `Deps.Getenv`, `Deps.Exec` and `Deps.Stream`, read nothing from stdin, and read no file, verified at least by `up`, `url`, `down`, `down wip`, `wipe`, `wipe wip`, `ls`, `status`, `logs`, `logs -n 5 -f dummy`, `token` and `token set`, each giving the same output and exit code whether `Deps.Dir` is a readable temporary directory, a path that does not exist, or a directory with mode 000, with `Deps.Getenv` returning the empty string for every key.

- R-08X8-B3J2: `cli.Run` MUST return only 0 (success, help and version included), 1 (the operation failed: a run of an external program failed or could not start, or a file could not be read or written), 2 (a usage error, or a precondition was not met) or 3 (refused because `Deps.EUID` is 0), verified by asserting of every `cli.Run` call in the test suite that it returned one of the four.

- R-Y39Z-HTKS: The external-failure diagnostic for a run of an external program that returned a `seam.Result` with a non-zero `ExitCode` `n` MUST write to stderr the line `sandbox: <action>: exit status <n>`; then, when `Result.Output` is not empty, an empty line followed by the quoted block of `Result.Output`; then, when the command adds detail, an empty line followed by the detail lines unprefixed; MUST add nothing to stdout; and MUST make `cli.Run` return 1, where `<action>` and the detail are the texts the command's own design names; verified at least by `ls` (D07) with a registry holding one sandbox and a `Deps.Exec` answering its `systemctl` run with `ExitCode` 1 and `Output` `Failed to connect to bus: No medium found` and a newline, which MUST write exactly `sandbox: systemctl --user: exit status 1`, an empty line, and `> Failed to connect to bus: No medium found`, each ending in a newline, and nothing to stdout; and, for detail, by `up` (D04) whose restart run of `sandbox-wip-dummy.service` is answered with `ExitCode` 1 and `Output` `failed` and a newline, which MUST write exactly `sandbox: start sandbox-wip-dummy.service: exit status 1`, an empty line, `> failed`, an empty line, and `run 'sandbox logs dummy' for its journal`, each ending in a newline, and nothing to stdout.

- R-11I9-YSC0: When `Result.Output` is empty, the external-failure diagnostic MUST hold no quoted block and no empty line for it, verified by `ls` (D07) with its `systemctl` run answered with `ExitCode` 1 and empty `Output` writing exactly the one line `sandbox: systemctl --user: exit status 1` and a newline to stderr and nothing to stdout.

- R-0CKX-GER5: The quoted block of a byte string MUST be its lines, split at each newline, each written as `> `, the line's bytes unchanged, and a newline, where a final newline ends the last line and does not begin another, a last line without a final newline is quoted the same, and an empty line is quoted as `> ` alone; verified through the external-failure diagnostic with `Output` `a` newline newline `b` quoting as `> a`, `> `, `> b`, with `Output` `a` newline `b` newline quoting as `> a`, `> b`, and with `Output` of a single newline quoting as `> `.

- R-YJ4O-GU7T: The runner-error diagnostic for a run of an external program whose runner returned a non-nil error MUST write to stderr exactly the line `sandbox: <action>: <error>` and a newline, where `<action>` is the text the command's own design names and `<error>` is the error's `Error()` text written as a printed name (R-MBDF-W0VP), so an error that quotes a path holding a newline stays one line, write no quoted block, and add nothing to stdout; `cli.Run` MUST then return 1 unless the requirement that cites this diagnostic states another outcome (as D08 does for a followed `logs` ended by cancellation); verified at least by `url` with a `Deps.Exec` whose `git rev-parse --show-toplevel` run (D03) returns the error `boom` writing exactly `sandbox: git rev-parse --show-toplevel: boom` to stderr, nothing to stdout, and returning 1, and by the error `chdir /tmp/a`, a newline, `b: no such file or directory` writing exactly `sandbox: git rev-parse --show-toplevel: chdir /tmp/a\x0ab: no such file or directory` as one line.

- R-AE6H-QEEG: The file-error diagnostic for a file or directory sandbox could not read, create, write, rename or remove, or whose content it could not parse, MUST write to stderr exactly the line `sandbox: <path>: <reason>` and a newline, where `<path>` is the absolute path sandbox addressed and `<reason>` is, for an operating-system error, its message alone without the operation or path that Go's error wraps around it, and otherwise the error's `Error()` text, and MUST add nothing to stdout; `cli.Run` MUST then return 1 unless the requirement that cites this diagnostic states another exit code; verified at least by `ls` (D07) when the registry path (D03) is a directory writing exactly `sandbox: <registry path>: is a directory` to stderr, nothing to stdout, and returning 1.
