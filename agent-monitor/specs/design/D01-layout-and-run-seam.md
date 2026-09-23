# D01-layout-and-run-seam

agent-monitor is one Go binary a developer builds from the checkout and runs
on their own machine. This design is the structural ground the other designs
stand on: where the code lives, which package exports which name, how the
version and the usage text are declared, and the seam through which the
program is run so that every behaviour can be tested in-process, without a
real process or real streams.

The module is `github.com/ikigenba/ikigenba/agent-monitor`, rooted at the
sub-project directory with `go.mod` beside `specs/`. The standard library is
enough; the module requires no other module. There are two packages, not
counting the external `_test` packages tests may add.
`cmd/agent-monitor` is wiring only: it hands the process arguments (without
the program name) and the process's standard output and standard error to the
run seam, and exits with the seam's exit code converted to `int`, the one
place the underlying integer is needed. `internal/cli` owns the
program as a command: the run seam, argument reading, the usage text, the exit
codes, and the version. Dependencies point one way: `cmd/agent-monitor`
imports `internal/cli`, and `internal/cli` imports nothing of this module.

The run seam is `cli.Run`. It takes the arguments and the two output streams
and returns the exit code as an `ExitCode`; it never terminates the calling program. Nothing
below `main` reaches the real process — its arguments, its streams, its exit,
the filesystem, the network — so a test that drives `Run` with buffers sees
the whole program's behaviour. `internal/cli` may import only `io`,
`strings`, `unicode`, and `unicode/utf8` from the standard library — the
last two because a diagnostic escapes the argument it echoes — a short
allow-list a test can check
with `go list`, so it has no package through which to reach the process, the
filesystem, or the network, and the stories' postcondition that nothing has
changed holds by construction. The seam is deliberately minimal: the
program reads nothing and waits on nothing yet. When a later design needs a
context or injected dependencies, the `Run` declaration is re-minted then.

The exit codes are a closed set, so they have their own named type,
`ExitCode`, rather than being bare `int`s: a signature that returns an
`ExitCode` cannot silently return a count or an index. There is one typed
constant per outcome the help text lists: success, output that could not be
written, and a usage error. `D02` fixes when each is returned.

The version is a `var` initialised in its own declaration, the only
declaration in `internal/cli/version.go`, and never injected by the linker, so a developer's
`go build` and a release build report the same string, and a release check
can read the string from that one file. Its value is data: requirements fix
the name, the file, that it is a source-initialised `var`, and its shape
(`D03`), never the value. The usage text is a constant in the same package,
declared here so every exported name of `internal/cli` is declared in one
place; `D03` fixes its value byte for byte.

Two requirements drive the built binary rather than `Run`: the bare run, and
the story's literal write-failure case of standard output on `/dev/full`.
They prove that `main` wires the real streams and the real exit code through
the seam.

## REQUIREMENTS

- R-N69Q-W7X1: The Go module MUST be `github.com/ikigenba/ikigenba/agent-monitor` with its `go.mod` at the sub-project root, and MUST contain exactly two non-test packages, `package main` with import path `github.com/ikigenba/ikigenba/agent-monitor/cmd/agent-monitor` and `package cli` with import path `github.com/ikigenba/ikigenba/agent-monitor/internal/cli`; external test packages (a `_test` package declared in `_test.go` files) MAY exist beside them; `cmd/agent-monitor` MUST import `internal/cli`, and the non-test files of `internal/cli` MUST import no package of this module.
- R-29D1-RWUN: The module MUST require no other module; `go.mod` MUST contain no `require` directive.
- R-2L9P-PJL7: The `internal/cli` package MUST export `func Run(args []string, stdout, stderr io.Writer) ExitCode`, where `args` excludes the program name, and a call to `Run` MUST return its exit code to the caller without terminating the calling program.
- R-2MHM-3BBW: The `package main` at `cmd/agent-monitor` MUST call `cli.Run` exactly once with the process arguments after the program name, `os.Stdout`, and `os.Stderr`, MUST exit the process by calling `os.Exit` with `int(code)`, where `code` is the `ExitCode` `Run` returned, and MUST write nothing to standard output or standard error except through that call.
- R-2ITW-Y03T: The `internal/cli` package MUST export the named type `type ExitCode int`.
- R-2K1T-BRUI: The `internal/cli` package MUST export the constants `ExitSuccess ExitCode = 0`, `ExitWriteFailed ExitCode = 1`, and `ExitUsage ExitCode = 2`, each declared with the type `ExitCode`.
- R-67Z3-IN6V: The `internal/cli` package MUST export `var Version string`, declared with its value set in source in the file `internal/cli/version.go`, and that file MUST contain only the package clause, that one declaration, and comments — no other declaration and no import.
- R-2FGJ-ORK4: `Version` MUST be set by a string-literal initializer in its declaration, so that a binary produced by `go build` with no linker flags reports the same `Version` the source declares.
- R-2GOG-2JAT: The `internal/cli` package MUST export `Usage` as a string constant.
- R-HFHC-NLE5: The non-test `.go` files of `internal/cli` MUST import no package other than the standard-library packages `io`, `strings`, `unicode`, and `unicode/utf8`, so that the direct imports `go list -f '{{.Imports}}'` reports for `./internal/cli` are a subset of `[io strings unicode unicode/utf8]`, and MUST NOT call the builtins `print` or `println`.
- R-2KC5-7UIW: The binary built from `./cmd/agent-monitor`, executed with no arguments, MUST write exactly `"hello, world\n"` to its standard output, write nothing to its standard error, and exit with status 0.
- R-2LK1-LM9L: The binary built from `./cmd/agent-monitor`, executed with no arguments and with its standard output opened on `/dev/full`, MUST write to its standard error exactly one line that begins `agent-monitor: write error: ` and ends in a single `"\n"`, and MUST exit with status 1.
