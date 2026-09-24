# D03-help-and-version

The things a developer asks agent-monitor about itself: what it can do, what
`list` can do, and which agent-monitor they have. `D01-layout-and-run-seam`
declares `Usage`, `ListUsage`, and `Version` in `internal/cli`; this design
fixes the values of `Usage` and `ListUsage` byte for byte, the shape of
`Version`, and when `Run` writes each. How the arguments are read, why
nothing after a deciding top-level argument matters, and why these outcomes
are the same whatever the `System` is and touch no file are
`D02-cli-grammar`.

The top-level help describes what the tool is for — observing the coding
agents on the machine through their logs and hooks. Its usage has two lines,
one for the options and one for `list`; it lists the one command, points at
a command's own help, lists the two options, each in its short and long
spelling, and lists the four exit codes `D01` declares, which every command
shares. The bare run prints it too, so with nothing to do agent-monitor shows
what it can do. It is the whole of standard output for `--help` or `-h` as
the first argument, ends in a newline, and is never written to standard
error.

The help of `list` names its one argument, says what it prints, lists the
three harnesses with the product each stands for, and lists its one option.
It does not repeat the exit codes. `--help` or `-h` anywhere after `list`
prints it and wins over every other argument after `list`, an error among
them; it needs no `HOME` and reads nothing.

The version is a `v` followed by a semantic version as semver.org defines
it, prerelease and build metadata allowed. The requirement fixes the shape
as a regular expression a test can apply to `Version`; the value itself is
data, edited in source at release time, and no requirement or test names it.
`--version` or `-V` as the first argument writes the version and a newline to
standard output. After `list` it is an unknown option (`D02`).

## REQUIREMENTS

- R-EVGO-LEF4: `Usage` MUST be exactly `"Usage: agent-monitor [options]\n       agent-monitor list <harness>\n\nObserve the coding agents on this machine through their logs and hooks.\n\nCommands:\n  list <harness>  list the live root sessions of claude, codex, or grok\n\nsee 'agent-monitor <command> --help' for command options\n\nOptions:\n  -h, --help      print this help\n  -V, --version   print the version\n\nExit codes:\n  0  success\n  1  the output could not be written\n  2  usage error\n  3  the harness's session data could not be read\n"`.
- R-EWOK-Z65T: `ListUsage` MUST be exactly `"Usage: agent-monitor list <harness>\n\nList the live root sessions of one harness, newest activity first.\n\nHarnesses:\n  claude  Claude Code\n  codex   OpenAI Codex CLI\n  grok    Grok Build CLI\n\nOptions:\n  -h, --help  print this help\n"`.
- R-EZ4D-QPN7: When `args` is empty, `Run` MUST write exactly `Usage` to `stdout`, write nothing to `stderr`, and return `ExitSuccess`.
- R-352F-PY4P: When `args[0]` is `--help` or `-h`, `Run` MUST write exactly `Usage` to `stdout`, write nothing to `stderr`, and return `ExitSuccess`.
- R-F0CA-4HDW: When `args[0]` is `list` and at least one element of `args[1:]` is byte-for-byte equal to `--help` or `-h`, `Run` MUST write exactly `ListUsage` to `stdout`, write nothing to `stderr`, and return `ExitSuccess`, whatever the other elements of `args[1:]` are, so that `["list", "--help"]`, `["list", "-h"]`, `["list", "claude", "--help"]`, and `["list", "bogus", "extra", "--bogus", "-h"]` all write `ListUsage`.
- R-36AC-3PVE: When `args[0]` is `--version` or `-V`, `Run` MUST write exactly `Version + "\n"` to `stdout`, write nothing to `stderr`, and return `ExitSuccess`.
- R-37I8-HHM3: `Version` MUST match the regular expression `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)(\.(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*)?(\+[0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*)?$` as interpreted by Go's `regexp` package.
