# D03-help-and-version

The things a developer asks agent-monitor about itself: what it can do, what
`list` and `tree` can do, and which agent-monitor they have.
`D01-layout-and-run-seam` declares `Usage`, `ListUsage`, `TreeUsage`, and
`Version` in `internal/cli`; this design fixes the values of the three help
texts byte for byte, the shape of `Version`, and when `Run` writes each. How
the arguments are read, why nothing after a deciding top-level argument
matters, and why these outcomes are the same whatever the `System` is and
touch no file are `D02-cli-grammar`.

The top-level help describes what the tool is for — observing the coding
agents on the machine through their logs and hooks. Its usage has three
lines, one for the options and one for each command; it lists the two
commands, `list` and `tree`, with their arguments aligned in one column,
points at a command's own help, lists the two options, each in its short and
long spelling, and lists the five exit codes `D01` declares, the codes the
commands use between them (only `tree` finds a session not found). The bare
run prints it too, so with nothing to do agent-monitor shows what it can do.
It is the whole of standard output for `--help` or `-h` as the first
argument, ends in a newline, and is never written to standard error.

The help of `list` names its one argument, says what it prints, lists the
three harnesses with the product each stands for, and lists its one option.
It does not repeat the exit codes. `--help` or `-h` anywhere after `list`
prints it and wins over every other argument after `list`, an error among
them; it needs no `HOME` and reads nothing.

The help of `tree` is built the same way: its usage line shows
`[--no-color]` and its two arguments, it says what it draws, lists the same
three harnesses, and lists its two options, `--no-color` and help, without
the exit codes. The top-level usage line for `tree` does not show
`--no-color`; the top-level help points at a command's own help for its
options. `--help` or `-h` anywhere after `tree` prints it and wins over
every other argument after `tree` — a missing, unknown, or extra argument
or an unknown option among them; it needs no `HOME` and reads nothing.

The version is a `v` followed by a semantic version as semver.org defines
it, prerelease and build metadata allowed. The requirement fixes the shape
as a regular expression a test can apply to `Version`; the value itself is
data, edited in source at release time, and no requirement or test names it.
`--version` or `-V` as the first argument writes the version and a newline to
standard output. After `list` or `tree` it is an unknown option (`D02`).

## REQUIREMENTS

- R-QCVS-17QG: `Usage` MUST be exactly `"Usage: agent-monitor [options]\n       agent-monitor list <harness>\n       agent-monitor tree <harness> <session-id>\n\nObserve the coding agents on this machine through their logs and hooks.\n\nCommands:\n  list <harness>               list the live root sessions of claude, codex, or grok\n  tree <harness> <session-id>  draw the subagent tree of one session\n\nsee 'agent-monitor <command> --help' for command options\n\nOptions:\n  -h, --help      print this help\n  -V, --version   print the version\n\nExit codes:\n  0  success\n  1  the output could not be written\n  2  usage error\n  3  the harness's session data could not be read\n  4  the session was not found\n"`.
- R-EWOK-Z65T: `ListUsage` MUST be exactly `"Usage: agent-monitor list <harness>\n\nList the live root sessions of one harness, newest activity first.\n\nHarnesses:\n  claude  Claude Code\n  codex   OpenAI Codex CLI\n  grok    Grok Build CLI\n\nOptions:\n  -h, --help  print this help\n"`.
- R-6NCQ-DKH0: `TreeUsage` MUST be exactly `"Usage: agent-monitor tree [--no-color] <harness> <session-id>\n\nDraw the subagent tree of one session.\n\nHarnesses:\n  claude  Claude Code\n  codex   OpenAI Codex CLI\n  grok    Grok Build CLI\n\nOptions:\n  --no-color  print without colour\n  -h, --help  print this help\n"`.
- R-EZ4D-QPN7: When `args` is empty, `Run` MUST write exactly `Usage` to `stdout`, write nothing to `stderr`, and return `ExitSuccess`.
- R-352F-PY4P: When `args[0]` is `--help` or `-h`, `Run` MUST write exactly `Usage` to `stdout`, write nothing to `stderr`, and return `ExitSuccess`.
- R-F0CA-4HDW: When `args[0]` is `list` and at least one element of `args[1:]` is byte-for-byte equal to `--help` or `-h`, `Run` MUST write exactly `ListUsage` to `stdout`, write nothing to `stderr`, and return `ExitSuccess`, whatever the other elements of `args[1:]` are, so that `["list", "--help"]`, `["list", "-h"]`, `["list", "claude", "--help"]`, and `["list", "bogus", "extra", "--bogus", "-h"]` all write `ListUsage`.
- R-QGJH-6IYJ: When `args[0]` is `tree` and at least one element of `args[1:]` is byte-for-byte equal to `--help` or `-h`, `Run` MUST write exactly `TreeUsage` to `stdout`, write nothing to `stderr`, and return `ExitSuccess`, whatever the other elements of `args[1:]` are, so that `["tree", "--help"]`, `["tree", "-h"]`, `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "--help"]`, and `["tree", "bogus", "extra", "more", "--bogus", "-h"]` all write `TreeUsage`.
- R-36AC-3PVE: When `args[0]` is `--version` or `-V`, `Run` MUST write exactly `Version + "\n"` to `stdout`, write nothing to `stderr`, and return `ExitSuccess`.
- R-37I8-HHM3: `Version` MUST match the regular expression `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)(\.(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*)?(\+[0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*)?$` as interpreted by Go's `regexp` package.
