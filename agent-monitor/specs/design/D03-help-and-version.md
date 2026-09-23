# D03-help-and-version

The two things a developer asks agent-monitor about itself: what it can do,
and which one they have. `D01-layout-and-run-seam` declares `Usage` and
`Version` in `internal/cli`; this design fixes the value of `Usage` byte for
byte, the shape of `Version`, and what `Run` writes when the first argument
asks for either. How the first argument is found and why nothing after it
matters is `D02-cli-grammar`.

The help text describes what the tool is for — observing the coding agents
on the machine through their logs and hooks — not the greeting it prints
today. Its usage line has no command slot and there is no commands section,
because agent-monitor has no commands yet. It lists the two options, each in
its short and long spelling, and the three exit codes `D01` declares. It is
the whole of standard output for `--help` or `-h`, ends in a newline, and is
never written to standard error.

The version is a `v` followed by a semantic version as semver.org defines
it, prerelease and build metadata allowed. The requirement fixes the shape
as a regular expression a test can apply to `Version`; the value itself is
data, edited in source at release time, and no requirement or test names it.
`--version` or `-V` writes the version and a newline to standard output.

## REQUIREMENTS

- R-33UJ-C6E0: `Usage` MUST be exactly `"Usage: agent-monitor [options]\n\nObserve the coding agents on this machine through their logs and hooks.\n\nOptions:\n  -h, --help      print this help\n  -V, --version   print the version\n\nExit codes:\n  0  success\n  1  the output could not be written\n  2  usage error\n"`.
- R-352F-PY4P: When `args[0]` is `--help` or `-h`, `Run` MUST write exactly `Usage` to `stdout`, write nothing to `stderr`, and return `ExitSuccess`.
- R-36AC-3PVE: When `args[0]` is `--version` or `-V`, `Run` MUST write exactly `Version + "\n"` to `stdout`, write nothing to `stderr`, and return `ExitSuccess`.
- R-37I8-HHM3: `Version` MUST match the regular expression `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)(\.(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*)?(\+[0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*)?$` as interpreted by Go's `regexp` package.
