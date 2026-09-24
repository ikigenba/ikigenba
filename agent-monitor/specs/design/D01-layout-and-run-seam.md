# D01-layout-and-run-seam

agent-monitor is one Go binary a developer builds from the checkout and runs
on their own machine. This design is the structural ground the other designs
stand on: which packages exist, which package owns which exported names, which
way the imports point, how the version and the three help texts are declared,
and the seam through which the program is run so that every behaviour can be
tested in-process, without a real process, real streams, or the real
filesystem.

The module is `github.com/ikigenba/ikigenba/agent-monitor`, rooted at the
sub-project directory with `go.mod` beside `specs/`. The standard library is
enough; the module requires no other module. There are nine packages, not
counting the external `_test` packages tests may add, each one concern:

- `cmd/agent-monitor` is wiring only. It reads the process arguments (without
  the program name), the values of `HOME`, `NO_COLOR`, and `TERM`, whether its
  standard output is a terminal, and the machine's filesystem rooted at `/`,
  hands them and the process's standard output and standard error to the run
  seam, and exits with the seam's exit code converted to `int`, the one place
  the underlying integer is needed.
- `internal/cli` owns the program as a command: the run seam `Run`, the
  machine it runs against (`System`), the exit codes (`ExitCode` and its
  constants), the three help texts (`Usage`, `ListUsage`, `TreeUsage`), the
  version (`Version`), the argument grammar of the top level and of its two
  commands, `list` and `tree`, the dispatch of each command to a harness
  package (`list` to its `List`, `tree` to its `Tree`), and every
  diagnostic. `D02` and `D03` fix its behaviour.
- `internal/quote` owns the escaped forms text is printed in so it can forge
  no output: `Arg` for an argument echoed in a diagnostic, `Field` for a table
  field or a path. `D02` defines both.
- `internal/session` owns the vocabulary shared by every harness: a session,
  its status, the table `list` prints, the error that means a harness's data
  could not be read, the complete lines of a log, and `Log`, the incremental
  reader that returns a log's complete lines a pass at a time, reading only
  the bytes appended since the last pass. `D04` declares its names.
- `internal/tree` owns the subagent tree `tree` draws: its nodes, the status
  words a node may carry, the signal that a named session does not exist, and
  the drawing itself. It is pure: it reaches no filesystem. `D09` declares its
  names.
- `internal/proc` owns the facts about processes read from `/proc`: when a
  process started, where it works, and which process holds a lock on which
  file. `D05` declares its names.
- `internal/harness/claude`, `internal/harness/codex`, and
  `internal/harness/grok` each own one harness's on-disk registry and logs and
  turn them into sessions for `list` and into a tree for `tree`. `D06`,
  `D07`, and `D08` declare their names.

Imports point one way, and a requirement fixes, for every package, the
packages of this module it may import: `cmd/agent-monitor` imports
`internal/cli`; `internal/cli` imports `internal/quote`, `internal/session`,
`internal/tree`, and the three harness packages; each harness package imports
`internal/session`, `internal/proc`, and `internal/tree`; `internal/session`
imports `internal/quote` and `internal/proc` (the reader learns a file's
device and inode through `proc.FileIDOf`, so that `syscall` stays in
`internal/proc`); `internal/tree` imports `internal/quote`; `internal/proc`
and `internal/quote` import nothing of this module. No harness package
imports another, and none imports `internal/cli`.

The run seam is `cli.Run`. It takes the arguments, a `System`, and the two
output streams, and returns the exit code as an `ExitCode`; it never
terminates the calling program. A `System` is everything of the machine the
program may see: `Home`, the value of `HOME` (empty when unset or empty);
`Root`, the machine's filesystem rooted at `/` as an `fs.FS`, so the file at
`/proc/locks` is the name `proc/locks`; `NoColor`, the value of `NO_COLOR`
(empty when unset or empty); `Term`, the value of `TERM` (empty when unset);
and `Terminal`, whether the process's standard output is a terminal. The last
three are plain values, not a way to ask, so the zero value of each says "not
a terminal, no preference": a test that builds `System{Home: ..., Root: ...}`
gets the uncoloured output the stories show, and `cli` decides from these
values whether `tree` draws in colour (`D02`) without importing `os`. An
`fs.FS` can only be read, so the stories' postcondition that `list` and
`tree` change nothing holds by construction.
Nothing below `main` reaches the real process — its arguments, its
environment, its streams, its exit, the filesystem other than through `Root`,
the network — so a test that drives `Run` with buffers and a
`testing/fstest.MapFS` sees the whole program's behaviour. The lint gate
enforces this: no package below `main` can import `os`, `os/exec`, `net`,
anything under `net/`, or `path/filepath`; below `main`, only `internal/proc`
may import `syscall` (to read the device and inode numbers from a file's
`Sys()`); and only `main` may use `unsafe`, which gosec's G103 check rejects
everywhere outside `cmd/` (the one exclusion `.golangci.yml` makes).

`main` learns whether standard output is a terminal the way the C library's
`isatty` does: glibc's `isatty` is `tcgetattr`, which `ioctl_tty(2)`
documents as the `TCGETS` ioctl, so standard output is a terminal exactly
when a `TCGETS` ioctl on its descriptor succeeds. `main` makes that call with
`syscall` and passes the `termios` buffer as an `unsafe.Pointer`, which is why
it alone may use `unsafe`. The obvious alternative, asking whether the file is
a character device, is wrong: a probe while designing showed the `TCGETS`
ioctl failing on `/dev/null`, `/dev/zero`, a pipe, and a regular file and
succeeding on a pseudo-terminal, while `/dev/null` and `/dev/zero` are
character devices all the same.

The exit codes are a closed set, so they have their own named type,
`ExitCode`, rather than being bare `int`s: a signature that returns an
`ExitCode` cannot silently return a count or an index. There is one typed
constant per outcome the help text lists, five in all: success, output that
could not be written, a usage error, session data that could not be read,
and a named session that was not found (which only `tree` returns). `D02`
fixes when each is returned.

The version is a `var` initialised in its own declaration, the only
declaration in `internal/cli/version.go`, and never injected by the linker, so
a developer's `go build` and a release build report the same string, and a
release check can read the string from that one file. Its value is data:
requirements fix the name, the file, that it is a source-initialised `var`,
and its shape (`D03`), never the value. The three help texts are constants
in the same package; `D03` fixes their values byte for byte.

No requirement drives the built binary: `main` is a few lines of wiring,
the terminal check included, and the release workflow runs the built program.

## REQUIREMENTS

- R-2I01-C8YY: The Go module MUST be `github.com/ikigenba/ikigenba/agent-monitor` with its `go.mod` at the sub-project root, and MUST contain exactly nine non-test packages, with these import paths relative to the module path and these package names: `cmd/agent-monitor` (`package main`), `internal/cli` (`package cli`), `internal/quote` (`package quote`), `internal/session` (`package session`), `internal/tree` (`package tree`), `internal/proc` (`package proc`), `internal/harness/claude` (`package claude`), `internal/harness/codex` (`package codex`), and `internal/harness/grok` (`package grok`); external test packages (a `_test` package declared in `_test.go` files) MAY exist beside them.
- R-2J7X-Q0PN: The non-test Go files of each package of the module MUST import no package of this module other than those listed here for that package, import paths relative to the module path: `cmd/agent-monitor` — `internal/cli`; `internal/cli` — `internal/quote`, `internal/session`, `internal/tree`, `internal/harness/claude`, `internal/harness/codex`, and `internal/harness/grok`; each of `internal/harness/claude`, `internal/harness/codex`, and `internal/harness/grok` — `internal/session`, `internal/proc`, and `internal/tree`; `internal/session` — `internal/quote` and `internal/proc`; `internal/tree` — `internal/quote`; `internal/proc` — none; `internal/quote` — none.
- R-29D1-RWUN: The module MUST require no other module; `go.mod` MUST contain no `require` directive.
- R-XK3U-55KN: The `internal/cli` package MUST export `func Run(args []string, sys System, stdout, stderr io.Writer) ExitCode`.
- R-XLBQ-IXBC: `Run` MUST treat `args` as the program's arguments excluding the program name, and a call to `Run` MUST return its exit code to the caller without terminating the calling program.
- R-68PX-SBKO: The `internal/cli` package MUST export the struct type `type System struct { Home string; Root fs.FS; NoColor string; Term string; Terminal bool }`, with exactly those five fields in that order, where `fs` is the standard library's `io/fs`.
- R-2ITW-Y03T: The `internal/cli` package MUST export the named type `type ExitCode int`.
- R-2K1T-BRUI: The `internal/cli` package MUST export the constants `ExitSuccess ExitCode = 0`, `ExitWriteFailed ExitCode = 1`, and `ExitUsage ExitCode = 2`, each declared with the type `ExitCode`.
- R-DHGS-WT0N: The `internal/cli` package MUST export the constant `ExitDataUnreadable ExitCode = 3`, declared with the type `ExitCode`.
- R-2KFU-3SGC: The `internal/cli` package MUST export the constant `ExitSessionNotFound ExitCode = 4`, declared with the type `ExitCode`.
- R-67Z3-IN6V: The `internal/cli` package MUST export `var Version string`, declared with its value set in source in the file `internal/cli/version.go`, and that file MUST contain only the package clause, that one declaration, and comments — no other declaration and no import.
- R-2FGJ-ORK4: `Version` MUST be set by a string-literal initializer in its declaration, so that a binary produced by `go build` with no linker flags reports the same `Version` the source declares.
- R-2GOG-2JAT: The `internal/cli` package MUST export `Usage` as a string constant.
- R-DIOP-AKRC: The `internal/cli` package MUST export `ListUsage` as a string constant.
- R-2LNQ-HK71: The `internal/cli` package MUST export `TreeUsage` as a string constant.
