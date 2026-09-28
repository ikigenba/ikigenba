# D01-layout-and-run-seam

agent-monitor is one Go binary a developer builds from the checkout and runs
on their own machine. This design is the structural ground the other designs
stand on: which packages exist, which package owns which exported names, which
way the imports point, how the version and the four help texts are declared,
and the seam through which the program is run so that every behaviour can be
tested in-process, without a real process, real streams, or the real
filesystem.

The module is `github.com/ikigenba/ikigenba/agent-monitor`, rooted at the
sub-project directory with `go.mod` beside `specs/`. The standard library is
enough; the module requires no other module. There are ten packages, not
counting the external `_test` packages tests may add, each one concern:

- `cmd/agent-monitor` is wiring, and the one place the machine's own
  signals are heard. It reads the process arguments (without the program
  name), the values of `HOME`, `NO_COLOR`, and `TERM`, whether its standard
  output is a terminal, and the machine's filesystem rooted at `/`; it builds
  the machine's `Watcher` and a channel it closes on ctrl+c; it hands them
  and the process's standard output and standard error to the run seam, and
  exits with the seam's exit code converted to `int`, the one place the
  underlying integer is needed.
- `internal/cli` owns the program as a command: the run seam `Run`, the
  machine it runs against (`System`), the exit codes (`ExitCode` and its
  constants), the four help texts (`Usage`, `ListUsage`, `TreeUsage`,
  `ChatUsage`), the version (`Version`), the argument grammar of the top
  level and of its three commands, `list`, `tree`, and `chat`, the dispatch
  of each command to a harness package (`list` to its `List`, `tree` to its
  `Tree`, `chat` to its `Chat`), following — keeping a command's view up to
  date with `-f` until interrupted, through the `Watcher` it is handed — and
  every diagnostic. `D02` and `D03` fix its behaviour, and `D11` its
  following. The harness packages know nothing of following.
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
- `internal/chat` owns the chat `chat` prints: its entries, their kinds,
  token usage and which counts a harness records, the printed form of an
  entry and of the totals line, the signal that a named agent is not in its
  session, and `Transcript`, the reader that follows one agent's transcript
  with a `session.Log` and turns each record, once, into entries and running
  totals through a harness's decoder. It reaches no filesystem except
  through the `fs.FS` a pass is handed. `D10` declares its names.
- `internal/proc` owns the facts about processes read from `/proc`: when a
  process started, where it works, and which process holds a lock on which
  file. `D05` declares its names.
- `internal/harness/claude`, `internal/harness/codex`, and
  `internal/harness/grok` each own one harness's on-disk registry and logs and
  turn them into sessions for `list`, into a tree for `tree`, and into one
  agent's transcript for `chat`. `D06`, `D07`, and `D08` declare their names,
  except `Chat`, which `D10` declares once for all three.

Imports point one way, and a requirement fixes, for every package, the
packages of this module it may import: `cmd/agent-monitor` imports
`internal/cli`; `internal/cli` imports `internal/quote`, `internal/session`,
`internal/tree`, `internal/chat`, and the three harness packages; each harness
package imports `internal/session`, `internal/proc`, `internal/tree`, and
`internal/chat`; `internal/chat` imports `internal/session`; `internal/session`
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
`Terminal`, whether the process's standard output is a terminal;
`Watcher`, what tells a following command that something it reads may have
changed; and `Interrupt`, a channel closed when the developer presses
ctrl+c. `NoColor`, `Term`, and `Terminal` are plain values, not a way to
ask, so the zero value of each says "not a terminal, no preference": a test
that builds `System{Home: ..., Root: ...}` gets the uncoloured output the
stories show, and `cli` decides from these values whether `tree` draws in
colour (`D02`) without importing `os`. The last two are zero-valued the same
way: a nil `Watcher` means no change ever arrives, and a nil `Interrupt`
means the run is never interrupted. A snapshot never looks at either, so
every test written before following still means what it did.

A `Watcher` has two methods. `Watch` replaces the set of watched names with
the ones it is given: names in `Root`, as `Root` names them (`home/dev/.claude/sessions`
for `/home/dev/.claude/sessions`), each of a directory. `Changes` returns the
same channel every time; a value on it means something in a watched
directory may have changed, or that a periodic re-check is due. Values may
be spurious, and a following command treats every value alike: one more
look, one more view if the view changed. Which names `cli` watches, and what
it does with each value and with `Interrupt`, is `D11`. A test drives
following with a fake `Watcher` over an unbuffered channel it sends on and
an `Interrupt` it closes, so every step is deterministic and nothing sleeps. An
`fs.FS` can only be read, so the stories' postcondition that `list`,
`tree`, and `chat` change nothing holds by construction.
Nothing below `main` reaches the real process — its arguments, its
environment, its streams, its exit, the filesystem other than through `Root`,
the network, its signals — so a test that drives `Run` with buffers, a
`testing/fstest.MapFS`, a fake `Watcher`, and a channel it closes sees the
whole program's behaviour. The lint gate
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
and a named session or agent that was not found (which `tree` and `chat`
return). The last is `ExitNotFound`, not a session-only name, because
`chat` returns it for an agent too. `D02` fixes when each is returned.

The version is a `var` initialised in its own declaration, the only
declaration in `internal/cli/version.go`, and never injected by the linker, so
a developer's `go build` and a release build report the same string, and a
release check can read the string from that one file. Its value is data:
requirements fix the name, the file, that it is a source-initialised `var`,
and its shape (`D03`), never the value. The four help texts are constants
in the same package; `D03` fixes their values byte for byte.

`main`'s `Watcher` uses Linux inotify through the standard library's
`syscall` (`InotifyInit1`, `InotifyAddWatch`, `InotifyRmWatch`, as
`inotify(7)` and Go's `syscall` package for Linux document them). It watches
each name it is given as the directory `/` joined with that name, for
entries created, deleted, and moved in or out, and for files modified or
closed after writing; a burst of events becomes one pending value on
`Changes`, never a queue of them. It also puts a value there every 2
seconds, for what no file signals: a process that exits, and anything under
`/proc`. If inotify cannot be set up, or a directory cannot be watched, it
falls back to the periodic value alone, so following is slower but never
fails for it. `main` closes `Interrupt` on the first `SIGINT`, learned through
`os/signal`, and then restores `SIGINT`'s default disposition
(`signal.Reset`, after `signal.Stop` on its channel), so ctrl+c is never
swallowed. The trade-off is accepted: a snapshot never looks at
`Interrupt`, so the first ctrl+c during a snapshot only closes it, and a
snapshot that hangs, on a stalled read say, needs a second ctrl+c, which the
default disposition turns into the usual kill. A follow that fails to end on
the first is stopped the same way. None of this changes the package layout or the lint rules:
the `Watcher` lives in `main`, which already may import `os` and `syscall`
and use `unsafe`.

No requirement drives the built binary: `main` is wiring, the terminal
check, the inotify `Watcher`, and the interrupt included, and the release
workflow runs the built program.

## REQUIREMENTS

- R-JD3N-0JRI: The Go module MUST be `github.com/ikigenba/ikigenba/agent-monitor` with its `go.mod` at the sub-project root, and MUST contain exactly ten non-test packages, with these import paths relative to the module path and these package names: `cmd/agent-monitor` (`package main`), `internal/cli` (`package cli`), `internal/quote` (`package quote`), `internal/session` (`package session`), `internal/tree` (`package tree`), `internal/chat` (`package chat`), `internal/proc` (`package proc`), `internal/harness/claude` (`package claude`), `internal/harness/codex` (`package codex`), and `internal/harness/grok` (`package grok`); external test packages (a `_test` package declared in `_test.go` files) MAY exist beside them.
- R-JEBJ-EBI7: The non-test Go files of each package of the module MUST import no package of this module other than those listed here for that package, import paths relative to the module path: `cmd/agent-monitor` — `internal/cli`; `internal/cli` — `internal/quote`, `internal/session`, `internal/tree`, `internal/chat`, `internal/harness/claude`, `internal/harness/codex`, and `internal/harness/grok`; each of `internal/harness/claude`, `internal/harness/codex`, and `internal/harness/grok` — `internal/session`, `internal/proc`, `internal/tree`, and `internal/chat`; `internal/chat` — `internal/session`; `internal/session` — `internal/quote` and `internal/proc`; `internal/tree` — `internal/quote`; `internal/proc` — none; `internal/quote` — none.
- R-29D1-RWUN: The module MUST require no other module; `go.mod` MUST contain no `require` directive.
- R-XK3U-55KN: The `internal/cli` package MUST export `func Run(args []string, sys System, stdout, stderr io.Writer) ExitCode`.
- R-XLBQ-IXBC: `Run` MUST treat `args` as the program's arguments excluding the program name, and a call to `Run` MUST return its exit code to the caller without terminating the calling program.
- R-TR0C-8TT4: The `internal/cli` package MUST export the struct type `type System struct { Home string; Root fs.FS; NoColor string; Term string; Terminal bool; Watcher Watcher; Interrupt <-chan struct{} }`, with exactly those seven fields in that order, where `fs` is the standard library's `io/fs` and `Watcher` is the interface type `Watcher` of `internal/cli`.
- R-TS88-MLJT: The `internal/cli` package MUST export the interface type `type Watcher interface { Watch(names []string); Changes() <-chan struct{} }`, with exactly those two methods.
- R-TTG5-0DAI: A call of `Run` in snapshot mode (`R-TJOX-Y7CY`) MUST call no method of `sys.Watcher`, so that the bytes it writes to each stream and the value it returns are the same whatever `sys.Watcher` and `sys.Interrupt` are, a nil `Watcher`, a nil `Interrupt`, and an `Interrupt` already closed included.
- R-EJ9J-A3CZ: A call of `Run` in follow mode (`R-TJOX-Y7CY`) whose `sys.Watcher` is nil MUST write to each stream the same bytes, and return the same value, as the same call with a `sys.Watcher` whose `Changes` channel never delivers a value, so that with a nil `Watcher` and a closed `sys.Interrupt`, when its first harness call succeeds, it writes its first view and then ends as an interrupted follow ends (`D11-follow.md`), returning `ExitSuccess`.
- R-N8JL-F6LI: A call of `Run` in follow mode (`R-TJOX-Y7CY`) whose `sys.Interrupt` is nil MUST treat it as a channel that is never closed: it MUST NOT return `ExitSuccess`, so that it returns only when its first harness call fails as `D02-cli-grammar.md` states for the first call, returning `ExitDataUnreadable` or `ExitNotFound`, or when a call to `stdout.Write` fails, returning `ExitWriteFailed`.
- R-2ITW-Y03T: The `internal/cli` package MUST export the named type `type ExitCode int`.
- R-2K1T-BRUI: The `internal/cli` package MUST export the constants `ExitSuccess ExitCode = 0`, `ExitWriteFailed ExitCode = 1`, and `ExitUsage ExitCode = 2`, each declared with the type `ExitCode`.
- R-DHGS-WT0N: The `internal/cli` package MUST export the constant `ExitDataUnreadable ExitCode = 3`, declared with the type `ExitCode`.
- R-JFJF-S38W: The `internal/cli` package MUST export the constant `ExitNotFound ExitCode = 4`, declared with the type `ExitCode`.
- R-67Z3-IN6V: The `internal/cli` package MUST export `var Version string`, declared with its value set in source in the file `internal/cli/version.go`, and that file MUST contain only the package clause, that one declaration, and comments — no other declaration and no import.
- R-2FGJ-ORK4: `Version` MUST be set by a string-literal initializer in its declaration, so that a binary produced by `go build` with no linker flags reports the same `Version` the source declares.
- R-2GOG-2JAT: The `internal/cli` package MUST export `Usage` as a string constant.
- R-DIOP-AKRC: The `internal/cli` package MUST export `ListUsage` as a string constant.
- R-2LNQ-HK71: The `internal/cli` package MUST export `TreeUsage` as a string constant.
- R-JGRC-5UZL: The `internal/cli` package MUST export `ChatUsage` as a string constant.
