# D11-follow

`-f`, or `--follow`, turns `list`, `tree`, and `chat` from a snapshot into a
view kept up to date until the developer presses ctrl+c. This design is what
`cli.Run` does once it is following: when it looks at the machine again, what
it writes and when, what ends it, and which directories it asks to be told
about. The grammar that accepts the option and the help texts that list it
are `D02-cli-grammar` and `D03-help-and-version`; the two things of the
machine following needs beyond the snapshot's — `cli.Watcher`, which says
that something may have changed, and the `Interrupt` channel of
`cli.System`, which closes on ctrl+c — are declared in
`D01-layout-and-run-seam`, together with how `main` builds them. Following
lives entirely in `internal/cli`: the harness packages are called with the snapshot's
arguments, through a read-only view of `Root`, and know nothing of it.

## One loop for three commands

Following starts as the snapshot does. Which calls follow at all — the
follow option on a valid command with a home directory — and that every
error the snapshot meets before its first output is reported with the same
text and exit code are `D02-cli-grammar`'s; this design adds that such a
failure touches no watcher and waits for nothing. Otherwise the first view is written and the
loop waits. Each value that arrives from the watcher is one more render, and
whatever that render writes is written whole before the next value is taken;
when the interrupt channel closes, the render in progress, if any, finishes,
the closing bytes are written, and `Run` returns success with nothing on
standard error. Nothing else ends it: a list going empty, a root session
ending, an agent finishing are only changes in what is drawn. A value may be
spurious; a render that finds nothing new writes nothing.

After startup, a render that would make the snapshot itself fail — the
harness's data unreadable, the session gone, the transcript unreadable —
writes nothing: the last view stays, and the next value reads again. A detail
the snapshot draws with a fallback is not a failure: every view is exactly
what the snapshot would print at that moment. Only a failed write ends
following, with the write-error diagnostic of `D02-cli-grammar` and exit 1,
however much was already written.

## list and tree: the view is the snapshot

For `list` and `tree`, each render produces the text the snapshot would
print — `session.Table` of the sessions, or `tree.Draw` of the tree with the
snapshot's colour decision — and writes only when that text differs from the
last text written. In a terminal the view is kept in place on the normal
screen: the cursor is hidden once at the start, each draw is cursor-home,
clear-screen, and the view, and ctrl+c shows the cursor again; a view always
ends in a newline, so nothing more is needed for the prompt. Anywhere else,
the first view is printed as the snapshot prints it, and each different
later view is appended after one empty line, with no escape sequences.

## chat: the transcript is read once, a pass at a time

For `chat`, the harness's `Chat` is called once, as the snapshot calls it,
and the `chat.Transcript` it returns (`D10-chat`) is kept for the whole run:
every later render is one more pass of it, which reads only the bytes the
agent's transcript gained since the last pass, so new entries are appended
in the snapshot's entry form. Outside a terminal, the totals line is never
printed. In a terminal, the totals line is pinned as the last line, written
without its newline; when a pass brings new entries or new usage, the line is
erased with a carriage return and erase-line, the new entries are written,
and the totals line follows again. Ctrl+c shows the cursor and ends the
totals line with a newline.

Two cases the snapshot never meets need a rule. A transcript whose file is
replaced makes its pass reset (`D10-chat`): the pass returns every entry of
the new file and the totals start over; since what is written cannot be
unwritten, those entries are appended like any others, and the totals line
shows the new file's totals — the snapshot's totals at that moment. A
transcript the harness found no file for has an empty path, and its passes
read nothing forever, although a snapshot taken once the harness writes the
file would find it; so while the followed transcript has no path, each
render asks `Chat` again, and the first transcript that comes back takes its
place.

## What is watched

The watcher is told which directories matter, and the renders say which
those are. While following, the harness functions and the transcript passes
are handed not `Root` itself but the follow root, a thin view of it with
only the plain read methods, so that every name they use — through `Glob`
and `Sub` too — passes through it and is seen. The directories a render
listed or opened through it, and the parent of every file it opened,
stat'd, or read, whether or not the file exists, are the render's watched
set. A missing file's parent is watched so its
creation is seen. `/proc` is left out: nothing there reports changes, and
the watcher's periodic value covers a process that exits. The set is handed
to `Watch` after the first render and again whenever a render's set differs
from the last one handed over, so a new subagent's directory is watched from
the render that first reads it.

## Testing it without a clock

Every requirement below can be checked in-process: a `testing/fstest.MapFS`
root behind a mutex so a test can change a file between renders, buffers for
the two streams, a fake watcher whose changes channel is unbuffered, and an
interrupt channel the test closes. Because a value is received only after the
previous render's output is written, a completed send tells the test that the
render before it is done, and closing the interrupt channel makes `Run`
return once the last render finishes. Nothing sleeps.

## REQUIREMENTS

- R-I6OP-C6T3: A call of `Run` *follows* if and only if it is in follow mode (`R-TJOX-Y7CY`, `D02-cli-grammar`); the *snapshot arguments* of such a call MUST be its `args` with every element of `args[1:]` that is the follow option (`R-SHW2-3BDF`) removed, the other elements kept in order, so that the snapshot arguments of `["list", "-f", "claude", "--follow"]` are `["list", "claude"]` and those of `["tree", "--no-color", "-f", "grok", "01a0c4f2-7b18-7d3a-9e61-3c8a0f5d2b47"]` are `["tree", "--no-color", "grok", "01a0c4f2-7b18-7d3a-9e61-3c8a0f5d2b47"]`.
- R-IBKA-V9RV: When `Run` follows, every filesystem it passes as the root argument of a harness `List`, `Tree`, or `Chat` call, and as the `fsys` of every pass of a `*chat.Transcript`, MUST be one value, the *follow root*, whose method set is exactly the methods of `fs.FS`, `fs.ReadDirFS`, `fs.ReadFileFS`, `fs.StatFS`, and `fs.ReadLinkFS`; each of its methods MUST return exactly what the same-named function of `io/fs` (`fs.ReadDir`, `fs.ReadFile`, `fs.Stat`, `fs.ReadLink`, `fs.Lstat`), or for `Open` the `Open` method, returns for `sys.Root` and the same name, and MUST pass `sys.Root` that same name unchanged, so that `fs.Glob` and `fs.Sub` over the follow root reach it only through those methods and every name the harnesses and passes use reaches the watched set.
- R-I7WL-PYJS: When `Run` follows and the harness `List`, `Tree`, or `Chat` call of its first render returns a non-nil error, `Run` MUST call no method of `sys.Watcher` and MUST return, with the diagnostic and exit code `D02-cli-grammar` gives that error, without waiting for `sys.Interrupt` to close.
- R-ED12-GH8M: When `Run` follows and the snapshot arguments are `["list", h]` or drawing arguments, the *view* of a render MUST be exactly the bytes that `Run`, called with the snapshot arguments and a `System` with the same `Home`, `NoColor`, `Term`, and `Terminal` and a `Root` holding the content `sys.Root` holds during the render, writes to `stdout` when that call returns `ExitSuccess`; a render for which that call would return `ExitDataUnreadable` or `ExitNotFound` MUST have no view; so that a view is the snapshot's table or tree at that moment, drawn with the snapshot's colour decision and with every fallback the snapshot draws.
- R-I94I-3QAH: When `Run` follows with snapshot arguments that are list arguments or drawing arguments, each of its renders, the first included, MUST make exactly one call of the harness function `D02-cli-grammar` names for those arguments, with the follow root as its root argument.
- R-3LNI-HE3O: When `Run` follows, it MUST, after the output of its first render has been written, repeatedly wait until it receives a value from the channel `sys.Watcher.Changes()` returns or `sys.Interrupt` is closed; for each value it receives it MUST make exactly one render, and MUST complete that render — every call to `stdout.Write` the render causes and the call to `Watch` it causes, if any — before it next receives from that channel; and it MUST NOT return other than as `R-HII3-JDHF`, `R-I7WL-PYJS`, and `R-HJPZ-X584` state, whatever its renders find, so that a list that becomes the header alone, a tree whose root becomes `ended`, and a chat whose agent adds no more entries are each still followed.
- R-HII3-JDHF: When `Run` follows, the harness call of its first render has returned a nil error, no call `Run` has made to `stdout.Write` has returned a non-nil error, and `sys.Interrupt` is closed, `Run` MUST complete any render in progress, MUST NOT receive from `sys.Watcher.Changes()` at any wait at which `sys.Interrupt` is already closed, even when a value is ready to be received there, MUST then write its interrupt output and nothing else, and MUST return `ExitSuccess` having made no call to `stderr.Write` — unless a call to `stdout.Write` made while completing that render or writing the interrupt output returns a non-nil error, in which case `R-HJPZ-X584` and `R-7BPS-795K` (`D02-cli-grammar`) govern and no further interrupt output is written; when `sys.Interrupt` is closed before `Run` is called, the first render's output MUST still be written before the interrupt output.
- R-EKCG-R3OS: While `Run` follows, every call it makes to `stdout.Write` MUST pass exactly one whole output this design defines — the first render's output, a later render's output, or the interrupt output — each output that is not empty MUST be delivered as exactly one call to `stdout.Write`, and an output that is empty MUST cause no call to `stdout.Write`.
- R-ELKD-4VFH: When `Run` follows with snapshot arguments `["list", h]` or drawing arguments and `sys.Terminal` is true, the first render's output MUST be `"\x1b[?25l\x1b[H\x1b[2J"` followed by its view; a later render's output MUST be `"\x1b[H\x1b[2J"` followed by its view when the render has a view that differs byte for byte from the last view written to `stdout`, and empty otherwise; and the interrupt output MUST be exactly `"\x1b[?25h"`.
- R-EMS9-IN66: When `Run` follows with snapshot arguments `["list", h]` or drawing arguments and `sys.Terminal` is false, the first render's output MUST be exactly its view; a later render's output MUST be `"\n"` followed by its view when the render has a view that differs byte for byte from the last view written to `stdout`, and empty otherwise; and the interrupt output MUST be empty; so that the whole of `stdout` is the distinct consecutive views separated by one empty line, with no escape sequence added and no view written twice in a row.
- R-F1F2-3W2I: A later render of `Run` following with snapshot arguments `["list", h]` or drawing arguments that has no view MUST write nothing to `stdout` or `stderr` and MUST leave the last view written to `stdout` as the view the next render is compared with, so that a view equal to the one on screen before the failed render is not written again.
- R-XBO0-PGW8: When `Run` follows with snapshot arguments that are chat arguments, its first render MUST be one call of the harness `Chat` that `D02-cli-grammar` names for those arguments, with the follow root (`R-IBKA-V9RV`) as its root argument, and the transcript that call returns is the *followed transcript*; each later render MUST be exactly one pass `t.Read(r)` of the followed transcript `t`, with `r` the follow root, except as `R-XCVX-38MX` states; `Run` MUST make no call of that `Chat` other than as this requirement and `R-XCVX-38MX` state; the *new entries* of the first render MUST be the entries that `Chat` call returned, and those of a later render the entries its pass or its `Chat` call returned with a nil error, none when it returned a non-nil error.
- R-EP82-A6NK: When `Run` follows with snapshot arguments that are chat arguments and `sys.Terminal` is false, the output of each render, the first included, MUST be exactly the concatenation of `chat.Format(x)` for each of its new entries `x`, in order, empty when it has none, and the interrupt output MUST be empty; so that no totals line and no escape sequence is ever written.
- R-EQFY-NYE9: When `Run` follows with snapshot arguments that are chat arguments and `sys.Terminal` is true, the *footer* after a render MUST be `chat.TotalsLine(t.Usage(), t.Recorded())` of the followed transcript `t` after that render with its final `"\n"` removed; the first render's output MUST be `"\x1b[?25l"`, then the concatenation of `chat.Format(x)` for each of its new entries `x` in order, then its footer; a later render's output MUST be `"\r\x1b[2K"`, then the concatenation of `chat.Format(x)` for each of its new entries `x` in order, then its footer, when the render has at least one new entry or its footer differs byte for byte from the last footer written, and empty otherwise; and the interrupt output MUST be exactly `"\x1b[?25h\n"`.
- R-ERNV-1Q4Y: A later render of `Run` following with chat snapshot arguments whose pass or `Chat` call returns a non-nil error MUST write nothing to `stdout` or `stderr`, MUST NOT end following, and MUST leave the followed transcript as it was, so that the next render is a pass of the same transcript that takes in what this one could not read.
- R-ESVR-FHVN: When a later render's pass of the followed transcript returns `reset` true and a nil error, `Run` MUST treat every entry that pass returned as a new entry of the render, written after everything already written as `R-EP82-A6NK` and `R-EQFY-NYE9` state, and MUST erase or rewrite nothing already written but the footer, so that after the render the footer shows the totals of the replaced file alone.
- R-XCVX-38MX: While the followed transcript's `Path()` is the empty string, each later render MUST, instead of a pass of it, call the same harness `Chat` again with the same arguments as the first render's call, the follow root (`R-IBKA-V9RV`) as its root argument; when that call returns a transcript `t2` and a nil error, `t2` MUST become the followed transcript from then on and the entries returned with it MUST be the render's new entries; when it returns a non-nil error, the followed transcript MUST stay as it was.
- R-XAG4-BP5J: When `Run` follows with chat snapshot arguments and, from the first render on, the agent's transcript file keeps its identity and only has bytes appended to it, the concatenation of `chat.Format(x)` over the new entries of every render up to and including any render MUST equal the bytes that `Run`, called at that render's moment with the snapshot arguments and the same `System` content, writes to `stdout` before its totals line; so that a half-written last line is written by no render until a later render finds it complete.
- R-IE03-MT99: The *watched set* of a render MUST be the set of names, without duplicates and in ascending bytewise order, made from each name `n` passed as the `name` argument of a call the render causes to a method of the follow root, other than `proc` and every name that begins with `proc/`, as follows: `n` itself when `fs.Stat(sys.Root, n)` during the render returns a nil error and a `fs.FileInfo` whose `IsDir` is true, and `path.Dir(n)` otherwise; so that a render that lists `home/dev/.claude/sessions`, opens `home/dev/.claude/sessions/41822.json` and `proc/41822/stat`, and stats the missing `home/dev/.grok` has exactly the watched set `home/dev`, `home/dev/.claude/sessions`.
- R-X80B-K5O5: When `Run` follows and `sys.Watcher` is not nil, it MUST call `sys.Watcher.Watch` with the first render's watched set once the first render's harness call has returned a nil error and that render's output, if any, has been written without error, and before its first wait for a value from `sys.Watcher.Changes()` or for `sys.Interrupt` — so also when `sys.Interrupt` was closed before `Run` was called; and it MUST call `Watch` with a later render's watched set, after that render's output, if any, has been written without error and before its next receive, whenever that set differs from the set last passed to `Watch`; an empty set MUST be passed as a slice of length zero; `Run` MUST make no other call to `Watch`, in particular none after a first render whose harness call returned a non-nil error and none after a call to `stdout.Write` that returned a non-nil error; and a render that has no view or no new entries MUST be treated the same as any other in this.
- R-HJPZ-X584: When a call `Run` makes to `stdout.Write` while it follows returns a non-nil error — for the first render's output, a later render's output, or the interrupt output, whatever was written before it — `Run` MUST end following at once, as `R-7BPS-795K` (`D02-cli-grammar`) states for the write itself: it MUST make no further receive from `sys.Watcher.Changes()`, no further call to `Watch`, and no further render, and MUST return without waiting for `sys.Interrupt` to close.
- R-F075-Q4BT: While `Run` follows, it MUST NOT call, on `sys.Root` or on any file or other value obtained through it, any method named `Write`, `WriteAt`, `WriteString`, `WriteFile`, `Create`, `OpenFile`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `Truncate`, `Chmod`, `Chown`, `Chtimes`, `Symlink`, `Link`, or `Sync`, even when the value has such a method, so that following creates, changes, and removes nothing.
