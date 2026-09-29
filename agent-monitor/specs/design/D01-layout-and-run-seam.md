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
enough; the module requires no other module. There are eleven packages, not
counting the external `_test` packages tests may add, each one concern:

- `cmd/agent-monitor` is wiring, and the one place the machine's own
  signals are heard. It reads the process arguments (without the program
  name), the values of `HOME`, `NO_COLOR`, and `TERM`, whether its standard
  input and its standard output are terminals, and the machine's filesystem
  rooted at `/`; it builds the machine's `Watcher`, a channel it closes on
  ctrl+c, and the machine's `Console` — the terminal's raw mode, its keys,
  and its size; it hands them and the process's standard output and standard
  error to the run seam, puts the terminal back as it found it on every
  exit it can intercept — a returning `Run`, a panicking one, and a
  terminating signal — and exits with the seam's exit code converted to `int`, the one place the underlying
  integer is needed.
- `internal/cli` owns the program as a command: the run seam `Run`, the
  machine it runs against (`System`), the exit codes (`ExitCode` and its
  constants), the four help texts (`Usage`, `ListUsage`, `TreeUsage`,
  `ChatUsage`), the version (`Version`), the argument grammar of the top
  level and of its three commands, `list`, `tree`, and `chat`, the dispatch
  of each command to a harness package (`list` to its `List`, `tree` to its
  `Tree`, `chat` to its `Chat`), following — keeping a command's view up to
  date with `-f` until interrupted, through the `Watcher` it is handed —
  the decision that a bare run browses, the terminal's raw mode around
  browsing, the enter and leave sequences written before and after the
  browser's own output, and every diagnostic. `D02` and `D03`
  fix its behaviour, `D11` its following, and this design what it does
  around browsing. The harness packages know nothing of following.
- `internal/browse` owns the interactive browser a bare run opens in a
  terminal: its four screens, what each key does, how a screen is drawn at
  the terminal's size and kept up to date, and when browsing ends. It is
  handed everything it sees — the home directory, the filesystem, the colour
  decision, a `Watcher`, the terminal's keys and size, and the interrupt as
  a context — and writes only its screens to the output it is handed, never
  the enter or leave sequence, which are `cli`'s; it never touches the
  process. `D12` declares
  its names.
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
`internal/tree`, `internal/chat`, `internal/browse`, and the three harness
packages; `internal/browse` imports `internal/quote`, `internal/session`,
`internal/tree`, `internal/chat`, and the three harness packages; each harness
package imports `internal/session`, `internal/proc`, `internal/tree`, and
`internal/chat`; `internal/chat` imports `internal/session`; `internal/session`
imports `internal/quote` and `internal/proc` (the reader learns a file's
device and inode through `proc.FileIDOf`, so that `syscall` stays in
`internal/proc`); `internal/tree` imports `internal/quote`; `internal/proc`
and `internal/quote` import nothing of this module. No harness package
imports another, and none imports `internal/cli` or `internal/browse`;
nothing but `internal/cli` imports `internal/browse`, and `internal/browse`
never imports `internal/cli`.

The run seam is `cli.Run`. It takes the arguments, a `System`, and the two
output streams, and returns the exit code as an `ExitCode`; it never
terminates the calling program. A `System` is everything of the machine the
program may see: `Home`, the value of `HOME` (empty when unset or empty);
`Root`, the machine's filesystem rooted at `/` as an `fs.FS`, so the file at
`/proc/locks` is the name `proc/locks`; `NoColor`, the value of `NO_COLOR`
(empty when unset or empty); `Term`, the value of `TERM` (empty when unset);
`Terminal`, whether the process's standard output is a terminal;
`StdinTerminal`, whether its standard input is a terminal;
`Watcher`, what tells a following command that something it reads may have
changed; `Interrupt`, a channel closed when the developer presses
ctrl+c; and `Console`, the terminal the browser talks to. `NoColor`, `Term`,
`Terminal`, and `StdinTerminal` are plain values, not a way to
ask, so the zero value of each says "not a terminal, no preference": a test
that builds `System{Home: ..., Root: ...}` gets the uncoloured output the
stories show, and `cli` decides from these values whether `tree` draws in
colour (`D02`) without importing `os`. The last two are zero-valued the same
way: a nil `Watcher` means no change ever arrives, and a nil `Interrupt`
means the run is never interrupted, in a follow and in a browsing run
alike. A snapshot that does not browse never looks at either — a browsing
bare run is in `D02`'s snapshot mode (`R-TJOX-Y7CY`) all the same, and it
does look — so every test written before following still means what it did. A zero
`StdinTerminal` means a bare run prints the help, so every test written
before browsing still means what it did too, and no run but a browsing one
calls a method of `Console`.

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
`tree`, `chat`, and the browser change nothing holds by construction.

A bare run *browses* when both standard input and standard output are
terminals, `Terminal` and `StdinTerminal` both true; with either false it
prints the help, as it always did. Browsing needs three things of the
terminal beyond its output stream: raw mode, so each key arrives as it is
pressed, unechoed, with ctrl+c as a byte instead of a signal; the keys
themselves; and the terminal's size, now and whenever it changes. Those are
the `Console`, and the work is split so the machine stays in `main` and
everything a test must see stays in `Run`.

`main` owns the machine side. `Console.Raw` switches the terminal to raw mode
and returns the function that switches it back; `main` also calls that
function itself after `Run` returns, which does nothing when `Run` already
has. From a call of `Raw` until its restore function is called — the *raw
window* — the terminal may be raw and showing the alternate screen with the
cursor hidden, and `main` sees to it that no exit it can intercept leaves it
so (below). `Console.Keys` returns the channel the keys arrive on: each value is
the bytes of one read of standard input, never empty, never reused by
`main` once sent, in the order read, and the channel is closed after the
last value when a read reports end of input or fails, as when the terminal
goes away. `Console.Size` returns the terminal's size, in columns and rows,
when it is called, and `Console.Resized` returns the same channel every time,
on which a value means the size may have changed; like a `Watcher` value, it
may be spurious, and a burst of changes may arrive as one value.

`Run` owns the screen. It decides whether to browse and reports a missing
`HOME` exactly as `list` does before the terminal is touched. It then asks
`Console.Size` for the terminal's size, before anything else of the
terminal, and when the terminal is narrower than `browse.MinCols` columns or
shorter than `browse.MinRows` rows (`D12`) it never opens the browser: it
writes the terminal-too-small diagnostic, which names the minimum as those
two constants make it and the size it read, writes nothing to `stdout`,
touches nothing else of the terminal, and returns the usage exit code. The
same two constants are the minimum the open browser holds a resized
terminal to (`D12`), so the check at the start and the running browser never
disagree. A nil `Console` has a size of zero by zero, so a browsing run
without one ends there. Otherwise `Run` *opens the browser*: it
calls `Raw` before its first byte of output and the restore function after its
last, and before any diagnostic, so a diagnostic reaches a terminal in its
normal mode. The escape sequences that enter and leave the alternate screen
and hide and show the cursor are output like any other, and `Run` itself,
not the browser, writes them to the `stdout` it is handed, so a test sees
them: the *enter sequence* before it calls `browse.Run`, and the *leave
sequence* after `browse.Run` returns, whether or not a write failed (below). Between the two, the
browser writes only its screens, and every byte it writes reaches `stdout`
unchanged. Browsing *ends* — on a quitting key (`D12`), on `Interrupt`
closing, or on `Keys` closing — when `browse.Run` returns nil; `Run` then
writes the leave sequence, restores the terminal, and returns success,
unless that leave write itself fails: then it writes nothing more, restores
the terminal, and reports the write error as any failed write is reported
(below).

A failed write stops browsing too, and still puts the screen back. The
output has failed, so `Run` stops at once, not calling the browser if it
has not yet, and makes exactly one more write, of the leave sequence, whose
result it ignores; then it restores the terminal and reports the first
failure as every command does. The leave write is attempted even when the
failed write was the enter sequence's own: a `Write` that fails may still
have written part of what it was handed (`io.Writer`: "It returns the
number of bytes written from p (0 <= n <= len(p)) and any error
encountered"), so the terminal may already be in the alternate screen with
the cursor hidden; on a terminal that never left the normal screen the
leave sequence shows a cursor already shown and at worst moves it to the
last saved position, a small cost beside a scrambled screen. That one write
is the exception, for browsing alone, that `D02`'s rule on a failed write
(`R-7BPS-795K`) allows: after a failed one, `Run` makes no further call to
`stdout.Write` but that one. The writer `Run` hands the browser passes nothing on after a
failure, so the browser cannot add a second write.

The key bytes carry no timing, so none is used to tell the Esc key from the
escape sequences other keys send. `main` delivers one read as one value, and
the decision is made from a value's bytes alone: a value that is exactly the
single byte ESC is the Esc key, and a value that starts with ESC and goes on
is never Esc followed by other keys. The rule rests on an expectation, not a
guarantee: a terminal is expected to write the whole sequence of one key at
once, and a read returns what has arrived, so that sequence usually arrives
as one value. A test sends `"\x1b"` for Esc and `"\x1b[A"` for ↑ as separate
values and gets the same answer every run. Two costs are accepted: a
sequence split across two reads, as a slow link may split it, is taken as
Esc followed by other bytes; and a value that starts with ESC and holds any
byte after it is never a bare Esc, whatever follows — Esc then `j` typed
quickly enough to arrive in one read, `"\x1bj"`, and two presses of Esc
arriving together, `"\x1b\x1b"`, are neither of them a press of Esc. A value may hold several keys, typed
quickly or pasted; what each key does is `D12`'s.

The browser itself is `internal/browse` (`D12`), and `Run` hands it
`sys.Watcher` and `sys.Console` as they are: `Console`'s methods include
those of `browse.Terminal`, and `Watcher`'s are those of `browse.Watcher`, so
no adapter sits between them. `Interrupt` reaches it as the context `Run`
hands `browse.Run`: one whose `Done` channel is `sys.Interrupt` itself, not
a second channel a goroutine closes a moment later, so the browser sees the
close exactly when `Run` would; a nil `Interrupt` makes a context that is
never done, which the standard library's `context` allows ("Done may return
nil if this context can never be canceled"). That keeps an interrupt
deterministic for a test: once `Interrupt` is closed, no key, size change,
or `Watcher` value is taken, even one already waiting. The first screen is
always written in full, however early `Interrupt` closed — before `Run` was
called or while that screen was being written — as a follow always writes
its first view (`D11`'s `R-HII3-JDHF`), so nothing checks `Interrupt` on the
way in; after the first, the screen being written is finished and no other
is written; then `browse.Run` returns and `Run` writes the leave sequence.
`D12` states the browser's side of this. A nil `Watcher` in a browsing run
is one whose changes never arrive. A nil `Console` is a terminal of zero by
zero, too small to open the browser, so a test that means to see a screen
hands `Run` a fake `Console` at least `browse.MinCols` by `browse.MinRows`,
over channels it sends on or leaves silent; it may leave the `Watcher` out
and close `Interrupt` to see just the first screen.

Nothing below `main` reaches the real process — its arguments, its
environment, its streams, its exit, the filesystem other than through `Root`,
the network, its signals, its terminal — so a test that drives `Run` with
buffers, a `testing/fstest.MapFS`, a fake `Watcher`, a fake `Console` over
channels it sends on, and a channel it closes sees the whole program's
behaviour. The lint gate
enforces this: no package below `main` can import `os`, `os/exec`, `net`,
anything under `net/`, or `path/filepath`; below `main`, only `internal/proc`
may import `syscall` (to read the device and inode numbers from a file's
`Sys()`); and only `main` may use `unsafe`, which gosec's G103 check rejects
everywhere outside `cmd/` (the one exclusion `.golangci.yml` makes).
`internal/browse` sits below `main` like the rest, and a requirement also
names what it may not import, so a structure check sees it directly.

`main` learns whether standard input and standard output are terminals the
way the C library's `isatty` does: glibc's `isatty` is `tcgetattr`, which
`TCSETS(2const)` (reached from `ioctl_tty(2)`) documents as the `TCGETS`
ioctl, so a descriptor is a terminal exactly when a `TCGETS` ioctl on it
succeeds. `main` makes that call on descriptors 0 and 1 with `syscall` and
passes the `termios` buffer as an `unsafe.Pointer`, which is why it alone may
use `unsafe`. The obvious alternative, asking whether the file is
a character device, is wrong: a probe while designing showed the `TCGETS`
ioctl failing on `/dev/null`, `/dev/zero`, a pipe, and a regular file and
succeeding on a pseudo-terminal, while `/dev/null` and `/dev/zero` are
character devices all the same.

`main`'s `Console` uses the same ioctls. `Raw` saves the `termios` a `TCGETS`
on standard input returns, clears in a copy the flags `termios(3)` lists
for `cfmakeraw` (`IGNBRK`, `BRKINT`, `PARMRK`, `ISTRIP`, `INLCR`, `IGNCR`,
`ICRNL`, and `IXON` in `c_iflag`; `OPOST` in `c_oflag`; `ECHO`, `ECHONL`,
`ICANON`, `ISIG`, and `IEXTEN` in `c_lflag`; `CSIZE` and `PARENB` in
`c_cflag`, then sets `CS8`), also sets `c_cc[VMIN]` to 1 and `c_cc[VTIME]`
to 0, which that list leaves as they were — the case `termios(3)` names
"MIN > 0, TIME == 0 (blocking read)", in which a read blocks until at least
one byte is available and returns up to the number requested, so every read
of the key reader returns what has arrived and never an empty value — and
applies it with `TCSETS`, which
`TCSETS(2const)` documents as `tcsetattr(fd, TCSANOW, …)`; the restore
function applies the saved `termios` the same way, once, however often it is
called. With `ISIG` clear, ctrl+c arrives as the byte 0x03 on `Keys` rather
than as `SIGINT`. If raw mode cannot be set, `Raw` leaves the terminal as it
is and browsing goes on without it, which a terminal that answered `TCGETS`
has no reason to cause. `Raw` also starts the one goroutine that reads
standard input into `Keys`, so a run that does not browse never reads its
input. `Size` is the `TIOCGWINSZ` ioctl on standard output, whose
`struct winsize` carries `ws_row` and `ws_col` (the man7.org page
man-pages/man2/TIOCGWINSZ.2const.html, titled `TIOCSWINSZ(2const)`, which
documents both `TIOCGWINSZ` and `TIOCSWINSZ`), and `Resized` delivers a value
for each `SIGWINCH`, the signal that same page says is sent to the
foreground process group when the window size changes
and whose default disposition `signal(7)` lists as ignore, learned through
`os/signal`. None of this changes the lint rules: `main` already may import
`os` and `syscall` and use `unsafe`.

Every exit from a browsing run puts the terminal back, and `main` covers the
exits `Run` cannot. Before it calls `Run`, `main` defers a function that,
when the raw window is still open, writes the leave sequence to standard
output, ignoring the result, and applies the saved `termios`; it does not
recover, so a panic in `Run` goes on to print its message and end the
process as it would have. It covers a panic on the goroutine that called
`Run`. A panic on any other goroutine runs that goroutine's own deferred
functions and then ends the process; `main`'s deferred restore, on another
goroutine, does not run, so the terminal is left raw, which is why nothing
below `main` should let one escape. On the normal path `main` calls `os.Exit`, which runs no deferred
function, after `Run` has closed the raw window itself. While the raw
window is open, `main` also catches, through `os/signal`, the signals that
would otherwise end the program: `SIGHUP`, `SIGTERM`, `SIGQUIT`, `SIGABRT`,
and every `SIGINT` after the first (the first closes `Interrupt`). Those are
the asynchronous signals `Notify` can take over that, by the package's
"Default behavior of signals in Go programs" (pkg.go.dev/os/signal), make a
Go program exit: "A SIGHUP, SIGINT, or SIGTERM signal causes the program to
exit. A SIGQUIT, SIGILL, SIGTRAP, SIGABRT, SIGSTKFLT, SIGEMT, or SIGSYS
signal causes the program to exit with a stack dump". Of those, the ones
caught are the ones sent to ask a program to end — by `kill`, by a hangup,
by a quit or abort request; the rest of that second list report faults and
traps in the program itself and keep their defaults. On one of them `main`
writes the leave sequence to standard output, ignoring the result, applies
the saved `termios`, restores that signal's default behaviour with
`signal.Reset`, and sends the same signal to its own process with
`syscall.Kill`, so the program ends exactly as it would have ended had
`main` never caught it — exiting, or exiting with a stack dump for
`SIGQUIT` and `SIGABRT` — and `Run`'s exit code is never used. `main` never catches a signal that
was ignored when the program started — `SIGHUP` under `nohup`, say, or
`SIGINT` for a job a non-interactive shell runs in the background — and
asks which those are with `signal.Ignored` ("Ignored reports whether sig is
currently ignored", pkg.go.dev/os/signal) before it calls `Notify`. Only
`SIGHUP` and `SIGINT` can be among them, as Go keeps only those two
ignored from startup ("If
the Go program is started with either SIGHUP or SIGINT ignored (signal
handler set to SIG_IGN), they will remain ignored", the same page). Catching one would undo the ignoring — "If the program was
started with SIGHUP or SIGINT ignored, and Notify is called for either
signal, a signal handler will be installed for that signal and it will no
longer be ignored" — and the `signal.Reset` before the re-raise would
ignore it again, so the program would go on running after restoring the
terminal. Left alone, such a signal stays ignored, as before, and does to
the program exactly what it would have done had `main` never caught it:
nothing. That holds for the first `SIGINT` too, so an ignored `SIGINT` never closes
`Interrupt`. The leave
sequence may land between two of the browser's writes, or after part of
one; that write is best effort, and the `termios` it is followed by is what
a shell needs back. Once the raw window closes, these signals go back to
their defaults, so a run that never browses behaves as before. With raw mode
set, ctrl+c cannot raise `SIGINT` (`ISIG` is clear, and it arrives as the
byte 0x03 on `Keys`) and ctrl+\ cannot raise `SIGQUIT`; a `SIGINT` from
`kill`, or from ctrl+c when `Raw` could not set raw mode, closes `Interrupt`
as ever, and browsing ends by the normal path, which writes the leave
sequence and restores. `SIGKILL` cannot be caught ("The signals SIGKILL and
SIGSTOP cannot be caught, blocked, or ignored", `signal(7)`), so a killed
run still leaves the terminal raw, and `reset` repairs it; a stopped one is
not an exit, and resumes as it was when continued.

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
swallowed — except inside the raw window, where a later `SIGINT` is caught
as the terminating signals are and restores the terminal before it ends the
program. The trade-off is accepted: a snapshot that does not browse never
looks at `Interrupt`, so the first ctrl+c during a snapshot only closes it, and a
snapshot that hangs, on a stalled read say, needs a second ctrl+c, which the
default disposition turns into the usual kill. A follow that fails to end on
the first is stopped the same way. None of this changes the package layout or the lint rules:
the `Watcher` lives in `main`, which already may import `os` and `syscall`
and use `unsafe`.

No requirement drives the built binary: `main` is wiring, the terminal
checks, the `Console`, the inotify `Watcher`, the interrupt, and the
restoring of the terminal on a panic or a signal included, so this design
states `main`'s part in prose, and the release workflow runs the built
program.

The escape sequences are xterm's, as "XTerm Control Sequences"
(invisible-island.net/xterm/ctlseqs/ctlseqs.html) documents them: ESC is the
byte 0x1b (`ascii(7)`), and the 7-bit form of CSI is `ESC [` (the page's list
of equivalent 7-bit and 8-bit controls: "ESC [ Control Sequence Introducer
(CSI is 0x9b)"), so `CSI ? 1049 h` is the bytes `"\x1b[?1049h"` and so on;
`CSI ? 1049 h` (DECSET 1049) saves the cursor and
switches to the Alternate Screen Buffer, clearing it first; `CSI ? 1049 l`
(DECRST 1049) uses the Normal Screen Buffer and restores the cursor; `CSI ?
25 l` hides the cursor and `CSI ? 25 h` shows it (DECTCEM). The enter
sequence is `"\x1b[?1049h\x1b[?25l"` and the leave sequence
`"\x1b[?25h\x1b[?1049l"`, the second undoing the first in reverse order.

## REQUIREMENTS

- R-KJST-TDNZ: The Go module MUST be `github.com/ikigenba/ikigenba/agent-monitor` with its `go.mod` at the sub-project root, and MUST contain exactly eleven non-test packages, with these import paths relative to the module path and these package names: `cmd/agent-monitor` (`package main`), `internal/cli` (`package cli`), `internal/quote` (`package quote`), `internal/session` (`package session`), `internal/tree` (`package tree`), `internal/chat` (`package chat`), `internal/browse` (`package browse`), `internal/proc` (`package proc`), `internal/harness/claude` (`package claude`), `internal/harness/codex` (`package codex`), and `internal/harness/grok` (`package grok`); external test packages (a `_test` package declared in `_test.go` files) MAY exist beside them.
- R-KL0Q-75EO: The non-test Go files of each package of the module MUST import no package of this module other than those listed here for that package, import paths relative to the module path: `cmd/agent-monitor` — `internal/cli`; `internal/cli` — `internal/quote`, `internal/session`, `internal/tree`, `internal/chat`, `internal/browse`, `internal/harness/claude`, `internal/harness/codex`, and `internal/harness/grok`; `internal/browse` — `internal/quote`, `internal/session`, `internal/tree`, `internal/chat`, `internal/harness/claude`, `internal/harness/codex`, and `internal/harness/grok`; each of `internal/harness/claude`, `internal/harness/codex`, and `internal/harness/grok` — `internal/session`, `internal/proc`, `internal/tree`, and `internal/chat`; `internal/chat` — `internal/session`; `internal/session` — `internal/quote` and `internal/proc`; `internal/tree` — `internal/quote`; `internal/proc` — none; `internal/quote` — none.
- R-KM8M-KX5D: The non-test Go files of `internal/browse` MUST NOT import the standard library packages `os`, `syscall`, `unsafe`, `net`, or `path/filepath`, nor any package whose import path begins with `os/` or `net/`.
- R-29D1-RWUN: The module MUST require no other module; `go.mod` MUST contain no `require` directive.
- R-XK3U-55KN: The `internal/cli` package MUST export `func Run(args []string, sys System, stdout, stderr io.Writer) ExitCode`.
- R-XLBQ-IXBC: `Run` MUST treat `args` as the program's arguments excluding the program name, and a call to `Run` MUST return its exit code to the caller without terminating the calling program.
- R-KNGI-YOW2: The `internal/cli` package MUST export the struct type `type System struct { Home string; Root fs.FS; NoColor string; Term string; Terminal bool; StdinTerminal bool; Watcher Watcher; Interrupt <-chan struct{}; Console Console }`, with exactly those nine fields in that order, where `fs` is the standard library's `io/fs`, `Watcher` is the interface type `Watcher` of `internal/cli`, and `Console` is the interface type `Console` of `internal/cli`.
- R-KOOF-CGMR: The `internal/cli` package MUST export the interface type `type Console interface { Raw() (restore func()); Keys() <-chan []byte; Size() (cols, rows int); Resized() <-chan struct{} }`, with exactly those four methods.
- R-KPWB-Q8DG: A value of the type `Console` of `internal/cli` MUST be assignable to a variable of the type `Terminal` of `internal/browse`, and a value of the type `Watcher` of `internal/cli` to a variable of the type `Watcher` of `internal/browse` (`D12-browse`).
- R-TS88-MLJT: The `internal/cli` package MUST export the interface type `type Watcher interface { Watch(names []string); Changes() <-chan struct{} }`, with exactly those two methods.
- R-AFT8-G88I: A call of `Run` in snapshot mode (`R-TJOX-Y7CY`, `D02-cli-grammar`) that does not browse (`R-KSC4-HRUU`) MUST call no method of `sys.Watcher`, so that the bytes it writes to each stream and the value it returns are the same whatever `sys.Watcher` and `sys.Interrupt` are, a nil `Watcher`, a nil `Interrupt`, and an `Interrupt` already closed included.
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
- R-KSC4-HRUU: A call of `Run` MUST *browse* if and only if `args` is empty, `sys.Terminal` is true, and `sys.StdinTerminal` is true.
- R-KTK0-VJLJ: A call of `Run` that does not browse (`R-KSC4-HRUU`) MUST call no method of `sys.Console`; and when `args` is not empty, the bytes `Run` writes to each stream and the value it returns MUST be the same whatever `sys.StdinTerminal` and `sys.Console` are.
- R-U39R-3JE8: A call of `Run` that browses (`R-KSC4-HRUU`) with a nil `sys.Console` and a `sys.Home` that is not the empty string MUST take the terminal's size to be 0 columns by 0 rows, so that it writes exactly the terminal-too-small diagnostic `R-ROX5-PC7X` states with `cols` and `rows` both 0 to `stderr`, writes nothing to `stdout`, calls no method of `sys.Watcher` or `sys.Root`, never calls `browse.Run`, and returns `ExitUsage`.
- R-KVZT-N32X: When `Run` browses (`R-KSC4-HRUU`) and `sys.Home` is the empty string, `Run` MUST write exactly the home-directory diagnostic `"agent-monitor: cannot find the home directory: HOME is not set\n"` to `stderr`, write nothing to `stdout`, call no method of `sys.Console`, `sys.Watcher`, or `sys.Root`, and return `ExitDataUnreadable`.
- R-ROX5-PC7X: When `Run` browses (`R-KSC4-HRUU`), `sys.Home` is not the empty string, and `sys.Console` is not nil (`R-U39R-3JE8` governs a nil one), `Run` MUST call `sys.Console.Size` before it calls any other method of `sys.Console` and before its first call to `stdout.Write`; when the `cols` that call returns is less than `browse.MinCols` or the `rows` it returns is less than `browse.MinRows` (`D12-browse`), `Run` MUST write exactly the terminal-too-small diagnostic `"agent-monitor: terminal too small: need at least " + strconv.Itoa(browse.MinCols) + "x" + strconv.Itoa(browse.MinRows) + ", have " + strconv.Itoa(cols) + "x" + strconv.Itoa(rows) + "\n"` to `stderr`, where `strconv` is the standard library's `strconv`, write nothing to `stdout`, make no further call to any method of `sys.Console` and no call to any method of `sys.Watcher` or `sys.Root`, not call `browse.Run`, and return `ExitUsage`; otherwise `Run` *opens the browser*.
- R-U4HN-HB4X: When `Run` opens the browser (`R-ROX5-PC7X`), it MUST call `sys.Console.Raw` exactly once, after the call to `sys.Console.Size` that `R-ROX5-PC7X` states and before its first call to `stdout.Write` and any further call to a method of `sys.Console`, and MUST call the restore function that call returned exactly once, after its last call to `stdout.Write`, before its first call to `stderr.Write`, and before it returns.
- R-U5PJ-V2VM: When `Run` opens the browser (`R-ROX5-PC7X`), the bytes it writes to `stdout`, taken together in order, MUST begin with the *enter sequence* `"\x1b[?1049h\x1b[?25l"`.
- R-59UF-KGDJ: When `Run` browses (`R-KSC4-HRUU`), `sys.Home` is not the empty string, and it *ends browsing* with no call to `stdout.Write` having returned a non-nil error, the bytes it writes to `stdout`, taken together in order, MUST end with the *leave sequence* `"\x1b[?25h\x1b[?1049l"`; when no call to `stdout.Write` has returned a non-nil error, the call that writes the leave sequence included, `Run` MUST then call the restore function `sys.Console.Raw` returned, write nothing to `stderr`, and return `ExitSuccess`; when the call that writes the leave sequence returns a non-nil error, `R-ADDF-OOR4` governs instead.
- R-ADDF-OOR4: When `Run` has opened the browser (`R-ROX5-PC7X`) and a call it makes to `stdout.Write` other than the one that writes the leave sequence (`R-59UF-KGDJ`) returns a non-nil error `err`, the call that writes the enter sequence included, `Run` MUST stop browsing at once, without waiting for a value from `sys.Console.Keys()`, `sys.Console.Resized()`, or `sys.Watcher.Changes()` or for `sys.Interrupt` to close; MUST then make exactly one further call to `stdout.Write`, passing it exactly the leave sequence (`R-59UF-KGDJ`) and ignoring what it returns; MUST then call the restore function `sys.Console.Raw` returned; and MUST then write exactly `"agent-monitor: write error: " + err.Error() + "\n"` to `stderr` and return `ExitWriteFailed` — that one leave write being the one further call to `stdout.Write` that `R-7BPS-795K` (`D02-cli-grammar`) allows a run that has opened the browser to make after a call to `stdout.Write` has returned a non-nil error. When the call that writes the leave sequence is the first to return a non-nil error `err`, `Run` MUST make no further call to `stdout.Write`, MUST call that restore function, and MUST then write exactly `"agent-monitor: write error: " + err.Error() + "\n"` to `stderr` and return `ExitWriteFailed`.
- R-53QX-NLO2: When `Run` opens the browser (`R-ROX5-PC7X`), `Run` itself MUST make its first call to `stdout.Write` with exactly the enter sequence (`R-U5PJ-V2VM`); MUST call `browse.Run` (`D12-browse`) at most once, and only after that call has returned a nil error, handing it a writer each of whose writes is one call to `stdout.Write` with the same bytes, returning what that call returns, until one such call returns a non-nil error, after which the writer makes no further call to `stdout.Write` and returns 0 and that error; and MUST, after `browse.Run` returns or, when the enter sequence's call failed, without calling it, make one call to `stdout.Write` with exactly the leave sequence (`R-59UF-KGDJ`); so that the calls `Run` makes to `stdout.Write` are, in order, the one with the enter sequence, then one for each write `browse.Run` makes to its writer up to and including the first that fails, then the one with the leave sequence, and no other.
- R-1AU7-1JQS: A call of `Run` that browses (`R-KSC4-HRUU`) with a nil `sys.Watcher` MUST write to each stream the same bytes, and return the same value, as the same call with a `sys.Watcher` whose `Watch` does nothing and whose `Changes` channel never delivers a value and is never closed.
- R-54YU-1DER: A call of `Run` that opens the browser (`R-ROX5-PC7X`) with a nil `sys.Interrupt` MUST treat `sys.Interrupt` as a channel that is never closed: it MUST NOT return other than when it ends browsing on a quitting key (`D12-browse`) or on the close of the channel `sys.Console.Keys()` returns (`R-57EM-SWW5`), in either case returning `ExitSuccess`, or when a call to `stdout.Write` returns a non-nil error (`R-ADDF-OOR4`), returning `ExitWriteFailed`.
- R-566Q-F55G: When `Run` opens the browser (`R-ROX5-PC7X`) and `sys.Interrupt` is closed — before `Run` is called or while it browses — `Run` MUST NOT receive from `sys.Console.Keys()`, `sys.Console.Resized()`, or `sys.Watcher.Changes()` at any wait at which `sys.Interrupt` is already closed, even when a value is ready to be received there; MUST, when the output of the first screen (`D12-browse`) is not yet completely written, write it completely, however early `sys.Interrupt` closed; MUST otherwise complete writing the output of the screen it is writing, if any; MUST write the output of no further screen; and MUST then end browsing (`R-59UF-KGDJ`) — unless a call to `stdout.Write` returns a non-nil error, in which case `R-ADDF-OOR4` governs.
- R-57EM-SWW5: When `Run` opens the browser (`R-ROX5-PC7X`) and the channel `sys.Console.Keys()` returns is closed, `Run` MUST end browsing (`R-59UF-KGDJ`) once it has taken every value received before the close, without waiting for a value from `sys.Console.Resized()` or `sys.Watcher.Changes()` or for `sys.Interrupt` to close; when `sys.Interrupt` is closed as well, `R-566Q-F55G` governs which values are taken.
- R-L5R0-P90H: When `Run` browses (`R-KSC4-HRUU`), it MUST decide whether a value received from `sys.Console.Keys()` holds the Esc key from that value's bytes alone: a value that is exactly the one byte 0x1b (ESC) MUST be taken as one press of Esc, and a value of two or more bytes whose first byte is 0x1b MUST NOT be taken as a press of Esc followed by other keys, so that the keys a value is taken to hold never depend on when it arrives or on whether another value follows it.
