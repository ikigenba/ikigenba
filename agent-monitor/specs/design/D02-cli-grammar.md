# D02-cli-grammar

What `cli.Run` does with the arguments and the `System`
`D01-layout-and-run-seam` hands it: what the bare run does, how the top level reads an argument, how
`list`, `tree`, and `chat` read the arguments after them, when each command
looks at the machine and which harness package it asks, when a command follows instead of printing
one snapshot, the diagnostics for
a usage error, for data that cannot be read, for a session or agent that
does not exist, and for output that cannot be written, and which exit code
each outcome returns.
It also defines the two escaped forms in `internal/quote`. The help texts and
the version string themselves, and when they are written, are
`D03-help-and-version`. What a command does while it follows is
`D11-follow`; this design says only when it follows and that it starts
exactly as its snapshot does.

## The top level

Arguments are read strictly left to right, and the first argument that
decides the outcome wins; nothing after it is looked at by the top level.
Every argument at the top level decides, so the first argument alone
determines everything, unless it is a command, `list`, `tree`, or `chat`, which hands
every argument after it to that command. An argument that begins with `-` is
an option. It is a known option only when it is exactly one of `--help`,
`-h`, `--version`, or `-V`; any other option is unknown and is named back as
typed, in escaped form. There is no bundling of short options and no
`--name=value` form, and `--` has no end-of-options meaning, so `-hV`,
`--help=x`, `-`, and `--` are all unknown options. An argument that does not
begin with `-` is a command; the commands are `list`, `tree`, and `chat`,
spelled exactly so, and every other, the empty string, `List`, `Tree`, and
`Chat` included, is an unknown command. So `agent-monitor --help list` and
`agent-monitor --help chat` print the top-level help, and
`agent-monitor bogus tree` fails on `bogus`. `-f` and `--follow` are options of the
three commands, not of agent-monitor, so as the first argument each is an
unknown option like any other.

## The bare run

With no arguments at all, agent-monitor first looks at where it is running,
and at nothing else. When standard input and standard output are both
terminals it *browses* (`R-KSC4-HRUU`, `D01`); otherwise there is no one to
browse with, and it prints the top-level help exactly as `--help` does
(`D03`), on standard output, with exit 0 and nothing on standard error. That
help, like every help, is the same whatever the `System` is otherwise: it
needs no `HOME`, reads nothing, and never touches the terminal, so a bare run
in a pipe or a script behaves as it always did.

Only a run that browses goes on to the environment, in the order `D01`
fixes: first `HOME`, whose absence fails exactly as it does for `list`, with
the same one-line diagnostic and exit 3, before the terminal is touched and
before anything is read (`R-KVZT-N32X`); then the terminal's size, which,
below the minimum the browser needs, fails with the one-line
terminal-too-small diagnostic and exit 2, the usage exit code, before the
screen is opened (`R-ROX5-PC7X`). Otherwise it opens the browser and hands
`browse.Run` the `System` as it is: `HOME` and `Root` unchanged, the
`Watcher` and the `Console` with no adapter between them, `Interrupt` as the
context's own `Done` channel, and colour decided from `NO_COLOR` and `TERM`
alone, since a browsing run's output is a terminal by definition. A nil
interface stays nil when it is handed on (the Go specification's
assignability of one interface type to another carries over the dynamic type
and value, and a nil one has neither), so a nil `Watcher` reaches the browser
as a nil `Watcher`, which `D12` treats as one that never reports a change; a
nil `Console` never gets that far (`D01`), and a nil `Interrupt` is a `Done`
channel that never closes, which `D12` and the `context` package both allow.
The browser reads the harnesses' data through `System.Root` as the commands
do, as often as it needs to, and ends
with exit 0 when the developer quits, or with exit 1 when its output cannot
be written (`D01`, `D12-browse`).

## list

`list` reads its own arguments by its own rules. If any of them is exactly
`--help` or `-h`, wherever it stands, the help of `list` wins over everything
else (`D03`). Otherwise they are read left to right and the first error wins.
An argument exactly `-f` or `--follow` is the follow option: it may stand
anywhere after `list`, as often as the developer likes, and it is never in
error and never fills a place, exactly as `tree`'s `--no-color` is. Any
other argument beginning with `-` is an unknown option (`list` takes no
other option but help, so the top-level `--version` is unknown here, and so
are `--follow=x`, `-f=x`, and `-fx`, since there is no `--name=value` form
and no bundling); the first argument that is not an option is the harness,
which must be exactly `claude`, `codex`, or `grok`; any later argument that
is not an option is unexpected. With nothing after `list` but the follow
option, if that, the harness is missing. The arguments that list sessions —
the *list arguments* — are therefore `list` and one known harness, with any
number of follow options among or after them. The `--no-color` that `tree`
takes is an unknown option here.

The arguments are checked before the environment: a usage error, the help,
and the version are the same whatever the `System` is, and none of them, nor
the missing-`HOME` failure, touches `System.Root`. Only then does `list` look
at `System.Home`. An empty `Home` — `HOME` unset or empty — fails with exit 3
and reads nothing; the home directory is never looked up elsewhere.
Otherwise `list` calls the chosen harness package's `List` (`claude.List`,
`codex.List`, or `grok.List`, declared in `D06`–`D08`) with `System.Root`
and `System.Home`, and no other harness function. On success it prints
`session.Table` of the sessions (`D04`) — the header alone when there are
none. When the harness's data cannot be read, `List` returns a
`*session.ReadError`, and `list` prints nothing on standard output and names
the path it tried and the cause on standard error, with exit 3. With the
follow option, the first `List` call is the snapshot's: when it fails, it
fails exactly so, and nothing is followed.

## tree

`tree` reads its own arguments the way `list` does, with one more place
and one option. If any of them is exactly `--help` or `-h`, wherever it
stands, the help of `tree` wins over everything else (`D03`). Otherwise they
are read left to right and the first error wins. An argument exactly
`--no-color` is the no-colour option: it may stand anywhere after `tree`, as
often as the developer likes, and it is never in error and never fills a
place, so it is skipped when the places are counted. Any other argument
beginning with `-` is an unknown option — `--no-color=x`, `--no-colour`, and
`--No-Color` included, since there is no `--name=value` form and spelling is
exact. The follow option is read as for `list`, and combines with
`--no-color`. The first argument that is not an option is the harness, which must
be exactly `claude`, `codex`, or `grok`; the second is the session id, taken
as given, whatever its bytes; any later argument that is not an option is
unexpected. Only when no argument is in error does a missing place count:
with nothing after `tree` but `--no-color` and the follow option, if that,
the harness is missing, and with only a known harness the session id is
missing. So
`agent-monitor tree claud` and `agent-monitor tree --no-color claud` report
the unknown harness, not the missing session id, and
`agent-monitor tree claude --bogus` reports the unknown option. The
arguments that draw a tree — the *drawing arguments* — are therefore `tree`,
one known harness, and one session id that does not begin with `-`, with
any number of `--no-color` and follow options among or after them.
`--no-color` is an option of `tree` only: before `tree` it is a top-level
unknown option.

The arguments are all checked before the environment and before any session
is looked for, exactly as for `list`: an empty `Home` fails with exit 3 and
reads nothing. Otherwise `tree` calls the chosen harness package's `Tree`
(`claude.Tree`, `codex.Tree`, or `grok.Tree`, declared in `D06`–`D08`) with
`System.Root`, `System.Home`, and the session id, and nothing else of any
harness; following, every call is still that harness's `Tree`, and the
first one is the snapshot's. `D09-tree` closes what `Tree` may return: a tree and no error, the
error `tree.ErrNotFound`, or a `*session.ReadError`, and the two errors are
told apart without knowing the harness. On success `tree` prints
`tree.Draw` of the tree as its one product, in colour or not (`D09` fixes
what each looks like). The choice is made here, from the arguments and the
`System` alone: colour is on only when `System.Terminal` says standard
output is a terminal, `System.NoColor` is empty, `System.Term` is not exactly
`dumb`, and no `--no-color` was given. A `NO_COLOR` set to the empty string
leaves colour on, and a `TERM` that is unset, or anything but `dumb` byte
for byte, does not by itself turn it off. When the session does not exist
it prints nothing on standard output and says so on standard error in one
line, naming the harness and echoing the id as an argument is echoed, with
exit 4, `ExitNotFound`. When the harness's locating
directory cannot be read it fails exactly as `list` does, with exit 3. With
the follow option these failures happen only at the first `Tree` call; a
later one does not end following (`D11`).

## chat

`chat` reads its own arguments the way `tree` does, with one more place and
no option but the follow option and help. If any of them is exactly
`--help` or `-h`, wherever it stands, the help of `chat` wins over
everything else (`D03`). Otherwise they are read left to right and the first
error wins. The follow option is read as for `list`. Every other argument
beginning with `-` is an unknown option: `chat` never prints colour, so
`--no-color` is unknown here, and so are the top-level `--version` and `-V`. The first
argument that is not an option is the harness, which must be exactly
`claude`, `codex`, or `grok`; the second is the session id and the third the
agent id, each taken as given, whatever its bytes; any later argument that
is not an option is unexpected. Only when no argument is in error does a
missing place count: with nothing after `chat` but the follow option, if
that, the harness is missing, and with only a known harness the session id
is missing. The agent id is optional. The arguments that print a chat — the
*chat arguments* — are therefore `chat`, one known harness, one session id,
and at most one agent id, none of them beginning with `-`, with any number
of follow options among or after them.

The arguments are all checked before the environment and before any session
is looked for, exactly as for `tree`: an empty `Home` fails with exit 3 and
reads nothing. Otherwise `chat` calls the chosen harness package's `Chat`
(`claude.Chat`, `codex.Chat`, or `grok.Chat`, declared in `D10-chat`) once,
with `System.Root`, `System.Home`, the session id, and the agent id — or,
when no agent id was given, the session id again, since the root's id names
the root — and nothing else of any harness. `D10` closes what `Chat` may
return: a transcript that has made its first pass with that pass's entries,
or `tree.ErrNotFound`, `chat.ErrAgentNotFound`, or a `*session.ReadError`.
On success the one product is every entry in `chat.Format` form, in order,
followed by `chat.TotalsLine` of the transcript's usage and recorded counts;
without the follow option `chat` makes no second pass, so what it prints is
one snapshot. Following, how often `Chat` is called, and the later
passes of the transcript it returns, are `D11`'s. It never
colours, whatever the `System` says. A session that does not exist is
reported as for `tree`; an agent the session does not hold is reported in
one line naming the harness and echoing both ids as arguments are echoed;
both exit 4, `ExitNotFound`. Data that cannot be read — the locating
directory or the agent's transcript — fails as `list` does, with exit 3,
naming the path.

## Following

A call of `Run` is in *follow mode* when its arguments hold the follow
option, are list, drawing, or chat arguments, and `HOME` is set; every other
call is in *snapshot mode*, and every requirement here that speaks of
arguments that do not hold the follow option is a snapshot-mode requirement.
Startup is the snapshot's: arguments that hold the follow option but are in
error, or a missing `HOME`, fail exactly as the same arguments without it,
and the first harness call of a follow-mode run fails exactly as the
snapshot's does, before anything is written. What follows the first view —
the harness calls, the redraws, the end on ctrl+c — is `D11-follow`.

A bare run holds no follow option, so it is in snapshot mode whether or not
it browses, as `D01` has it. Browsing is not a third mode: a browsing run
writes many times and watches for changes much as following does, but that
is `D01`'s and `D12`'s, and the few requirements here about how a snapshot
writes say so by excepting the run that browses.

## Diagnostics and escaped forms

Every usage-error diagnostic follows the repository's command-line
conventions: the first line begins `agent-monitor: ` and names the offending
argument in single quotes (the missing harness and the missing session id
have none to name), then exactly one empty line, then the unprefixed hint to
run `agent-monitor --help`. The two exit-3 diagnostics and the two exit-4
diagnostics are one line each, and so is the terminal-too-small diagnostic
of a browsing bare run (`D01`), which names no argument and gives no hint,
since no argument was wrong, but exits 2 like a usage error. The usage text is never written to standard
error, and a failure writes nothing to standard output.

An argument is echoed in the form `quote.Arg` gives it, so that no argument
can forge output: ordinary printable text, ASCII or not, passes through
unchanged, so every plain argument reads exactly as typed, while `\`, `'`,
control bytes, DEL, bytes that are not valid UTF-8, and non-printable Unicode
characters (line and paragraph separators, bidirectional overrides, C1
controls, non-ASCII spaces) are escaped Go-style. A newline therefore never
reaches standard error from an argument, an empty argument shows as `''`, and
a terminal escape sequence is shown rather than rendered. The session id in
the not-found diagnostics and the agent id in the agent-not-found
diagnostic are such arguments. `quote.Field` is the same form
with one difference: `'` passes through as typed, because a table field, a
tree label, or a path is printed without surrounding quotes. `session.Table`
and `tree.Draw` print their text with `quote.Field`, and the cannot-read
diagnostic prints its path with it.

## Writing

Every product — the help texts, the version line, the session table, the
drawn tree, the chat — is handed to standard output in one write when
printed as a snapshot (following writes as `D11` says), and every diagnostic
to standard error in one write, so a diagnostic lands whole and a failed
product write is one well-defined event. When that write fails, agent-monitor
says so on standard error with the write error's own text as the reason,
writes nothing more to standard output, and exits 1. If standard error cannot
be written either, there is nowhere left to report to: the exit code stays
the one the outcome already chose — 1, 2, 3, or 4 — and nothing is retried.
Following writes many times, and a failed write ends it the same way, after
whatever it had already written. Browsing writes many times too, and a failed
write ends it the same way with one exception: unless the write that failed
was already the one that puts the terminal's normal screen back, it makes one
more write, of that sequence, whose own result it ignores, because a screen left scrambled is
worse than one more write to an output already known to fail (`D01`).

## REQUIREMENTS

- R-JHZ8-JMQA: Every call to `Run` MUST return one of `ExitSuccess`, `ExitWriteFailed`, `ExitUsage`, `ExitDataUnreadable`, or `ExitNotFound`, and no other value.
- R-JJ74-XEGZ: `Run` MUST classify `args[0]` as a known option if and only if it is byte-for-byte equal to one of `--help`, `-h`, `--version`, or `-V`; as the command `list` if and only if it is byte-for-byte equal to `list`; as the command `tree` if and only if it is byte-for-byte equal to `tree`; as the command `chat` if and only if it is byte-for-byte equal to `chat`; as an unknown option if and only if it begins with `-` and is not a known option; and as an unknown command if and only if it does not begin with `-` and is none of `list`, `tree`, and `chat`, the empty string, `List`, `Tree`, and `Chat` included.
- R-JKF1-B67O: When `args` is not empty and `args[0]` is none of `list`, `tree`, and `chat`, `Run` MUST behave exactly as it does for `args[:1]` — the same bytes written to `stdout`, the same bytes written to `stderr`, and the same return value — whatever the later elements of `args` are, so that `["--help", "list"]`, `["--help", "tree"]`, and `["--help", "chat"]` behave as `["--help"]` and `["bogus", "chat"]` as `["bogus"]`.
- R-2VB8-NS75: `Run` MUST treat each of `-`, `--`, `-hV`, `-Vh`, `--help=x`, `--version=x`, `--HELP`, and `-H` as an unknown option, and MUST give `--` no end-of-options meaning, so that `Run` with `args` `["--", "--help"]` reports unknown option `--`.
- R-TM4Q-PQUC: `Run` MUST treat each of `-f`, `--follow`, `-f=x`, and `--follow=x` as an unknown option when it is `args[0]`, so that `["-f", "list", "claude"]` writes the unknown-option diagnostic for `-f` and `["--follow", "tree", "grok", "01a0c4f2-7b18-7d3a-9e61-3c8a0f5d2b47"]` the one for `--follow`.
- R-E3EZ-SOD5: The `internal/quote` package MUST export `func Arg(s string) string`.
- R-E4MW-6G3U: The `internal/quote` package MUST export `func Field(s string) string`.
- R-XGG4-ZUCK: `quote.Arg(s)` MUST return the concatenation, reading `s` from its first byte to its last and taking at each position the element `utf8.DecodeRuneInString` decodes there, of: for a byte that does not begin a valid UTF-8 encoding (the decode yields `utf8.RuneError` with width 1), `\x` followed by the byte's value as two lowercase hexadecimal digits; for byte 0x5C (`\`), `\\`; for byte 0x27 (`'`), `\'`; for byte 0x0A, `\n`; for byte 0x09, `\t`; for byte 0x0D, `\r`; for any other byte 0x00–0x1F and for byte 0x7F, `\x` followed by the byte's value as two lowercase hexadecimal digits; for any other byte 0x20–0x7E, that byte unchanged; for a validly encoded rune `r` of U+0080 or above with `unicode.IsPrint(r)` true, its encoding unchanged; and for a validly encoded rune `r` of U+0080 or above with `unicode.IsPrint(r)` false, `\u` followed by `r` as four lowercase hexadecimal digits when `r` is at most U+FFFF, otherwise `\U` followed by `r` as eight lowercase hexadecimal digits — so that, for example, `bogus` maps to `bogus`, `a` TAB `b` to `a\tb`, `it's` to `it\'s`, `é` to `é`, the single byte 0xFF to `\xff`, ESC `[31m` to `\x1b[31m`, U+202E to `\u202e`, and U+E0001 to `\U000e0001`.
- R-XHO1-DM39: `quote.Field(s)` MUST return exactly the string `quote.Arg(s)` returns except that each byte 0x27 (`'`) of `s` is represented by the single byte `'` instead of by `\'`, so that, for example, `fix Bob's checkout` maps to `fix Bob's checkout`, `it's` TAB to `it's\t`, `a\b` to `a\\b`, and U+202E to `\u202e`.
- R-SHW2-3BDF: When `args[0]` is `list`, `tree`, or `chat`, an element of `args[1:]` MUST be the *follow option* if and only if it is byte-for-byte equal to `-f` or `--follow`, and `args` MUST *hold the follow option* if and only if `args[0]` is one of `list`, `tree`, or `chat` and at least one element of `args[1:]` is the follow option; so that `-f` and `--follow` are the same option, holding it more than once is the same as holding it once, and `--follow=x`, `-f=x`, `-fx`, `-F`, `--Follow`, and `--follow ` are not the follow option.
- R-SJ3Y-H344: `args` MUST be *list arguments*, with harness argument `h`, if and only if `args[0]` is `list` and the elements of `args[1:]` that are not the follow option, taken in order, are exactly `[h]` with `h` one of `claude`, `codex`, or `grok`, so that `["list", "claude"]`, `["list", "-f", "claude"]`, `["list", "--follow", "claude"]`, `["list", "claude", "-f"]`, and `["list", "-f", "claude", "--follow"]` are list arguments with `h` `claude`, while `["list"]`, `["list", "-f"]`, `["list", "--follow=x", "claude"]`, `["list", "-f", "claud"]`, and `["list", "claude", "extra"]` are not.
- R-SKBU-UUUT: When `args[0]` is `list` and no element of `args[1:]` is `--help` or `-h`, `Run` MUST classify each element of `args[1:]` as follows: the follow option is never an unknown option, a harness argument, or an unexpected argument, however many times and wherever it appears; every other element that begins with `-` is an unknown option, `--follow=x`, `-f=x`, `-fx`, `--no-color`, `--version`, `-V`, `-`, and `--` included; the first element that does not begin with `-` is the harness argument, a known harness if and only if it is byte-for-byte equal to one of `claude`, `codex`, or `grok`, and otherwise an unknown harness, the empty string and `Claude` included; and every later element that does not begin with `-` is an unexpected argument.
- R-E9IH-PJ2M: When `args[0]` is `list`, no element of `args[1:]` is `--help` or `-h`, and some element of `args[1:]` is an unknown option, an unknown harness, or an unexpected argument, the first such element in order MUST decide the outcome and the elements after it MUST NOT affect it, so that `["list", "claud", "--bogus"]` and `["list", "claud", "extra"]` report unknown harness `claud`, `["list", "--bogus", "claud"]` and `["list", "claude", "--bogus"]` report unknown option `--bogus`, and `["list", "claude", "extra", "more"]` reports unexpected argument `extra`.
- R-EBYA-H2K0: When `args[0]` is an unknown option `arg`, or `args[0]` is `list` and the element of `args[1:]` that decides the outcome is an unknown option `arg`, `Run` MUST write exactly the unknown-option diagnostic `"agent-monitor: unknown option '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-ED66-UUAP: When `args[0]` is an unknown command `arg`, `Run` MUST write exactly the unknown-command diagnostic `"agent-monitor: unknown command '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-EFLZ-MDS3: When `args[0]` is `list` and the element of `args[1:]` that decides the outcome is an unknown harness `arg`, `Run` MUST write exactly the unknown-harness diagnostic `"agent-monitor: unknown harness '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-EGTW-05IS: When `args[0]` is `list` and the element of `args[1:]` that decides the outcome is an unexpected argument `arg`, `Run` MUST write exactly the unexpected-argument diagnostic `"agent-monitor: unexpected argument '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-SLJR-8MLI: When `args[0]` is `list` and every element of `args[1:]` is the follow option, there being zero or more of them, `Run` MUST write exactly the missing-harness diagnostic `"agent-monitor: missing harness\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`, so that `["list"]`, `["list", "-f"]`, and `["list", "--follow", "-f"]` each write it.
- R-SMRN-MEC7: `args` MUST be *drawing arguments*, with harness argument `h` and session-id argument `id`, if and only if `args[0]` is `tree` and the elements of `args[1:]` that are neither byte-for-byte equal to `--no-color` nor the follow option, taken in order, are exactly `[h, id]` with `h` one of `claude`, `codex`, or `grok` and `id` a string that does not begin with `-`, so that `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]`, `["tree", "--no-color", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]`, `["tree", "claude", "--no-color", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "--no-color"]`, `["tree", "-f", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]`, and `["tree", "claude", "--follow", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "--no-color", "-f"]` are drawing arguments with `h` `claude` and `id` `7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93`, while `["tree", "claude"]`, `["tree", "-f", "claude"]`, `["tree", "--no-color=x", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]`, `["tree", "--follow=x", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]`, and `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "extra"]` are not.
- R-SNZK-062W: When `args[0]` is `tree` and no element of `args[1:]` is `--help` or `-h`, `Run` MUST classify each element of `args[1:]` as follows: an element byte-for-byte equal to `--no-color` is the no-colour option, and it and the follow option are never an unknown option, a harness argument, a session-id argument, or an unexpected argument, however many times and wherever they appear; every other element that begins with `-` is an unknown option, `--no-color=x`, `--no-colour`, `--No-Color`, `--follow=x`, `-f=x`, `-fx`, `--version`, `-V`, `-`, and `--` included; the first element that does not begin with `-` is the harness argument, a known harness if and only if it is byte-for-byte equal to one of `claude`, `codex`, or `grok`, and otherwise an unknown harness, the empty string and `Claude` included; the second element that does not begin with `-` is the session-id argument, whatever its bytes, the empty string included; and every later element that does not begin with `-` is an unexpected argument.
- R-PQXL-5CDY: When `args[0]` is `tree`, no element of `args[1:]` is `--help` or `-h`, and some element of `args[1:]` is an unknown option, an unknown harness, or an unexpected argument, the first such element in order MUST decide the outcome and the elements after it MUST NOT affect it, so that `["tree", "claud"]`, `["tree", "claud", "--bogus"]`, and `["tree", "claud", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "extra"]` report unknown harness `claud`, `["tree", "--bogus", "claud"]`, `["tree", "claude", "--bogus"]`, and `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "--bogus", "extra"]` report unknown option `--bogus`, and `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "extra", "--bogus"]` and `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "extra", "more"]` report unexpected argument `extra`.
- R-PS5H-J44N: When `args[0]` is `tree` and the element of `args[1:]` that decides the outcome is an unknown option `arg`, `Run` MUST write exactly the unknown-option diagnostic `"agent-monitor: unknown option '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-PULA-ANM1: When `args[0]` is `tree` and the element of `args[1:]` that decides the outcome is an unknown harness `arg`, `Run` MUST write exactly the unknown-harness diagnostic `"agent-monitor: unknown harness '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-PVT6-OFCQ: When `args[0]` is `tree` and the element of `args[1:]` that decides the outcome is an unexpected argument `arg`, `Run` MUST write exactly the unexpected-argument diagnostic `"agent-monitor: unexpected argument '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-SP7G-DXTL: When `args[0]` is `tree` and every element of `args[1:]` is byte-for-byte equal to `--no-color` or is the follow option, there being zero or more of them, `Run` MUST write exactly the missing-harness diagnostic `"agent-monitor: missing harness\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`, so that `["tree"]`, `["tree", "--no-color"]`, `["tree", "--no-color", "--no-color"]`, `["tree", "-f"]`, and `["tree", "--follow", "--no-color", "-f"]` each write it.
- R-SQFC-RPKA: When `args[0]` is `tree` and the elements of `args[1:]` that are neither byte-for-byte equal to `--no-color` nor the follow option are exactly `[h]` with `h` one of `claude`, `codex`, or `grok`, `Run` MUST write exactly the missing-session-id diagnostic `"agent-monitor: missing session id\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`, so that `["tree", "claude"]`, `["tree", "--no-color", "grok", "--no-color"]`, and `["tree", "-f", "claude", "--follow"]` each write it.
- R-X0PD-PEO4: Whenever `args` is neither list arguments, nor drawing arguments, nor chat arguments, the bytes `Run` writes to each stream and the value it returns MUST be the same for every `sys` with which the call does not browse (`R-KSC4-HRUU`, `D01-layout-and-run-seam`), so that every usage error, the four help texts, the version, and the help of a bare run that does not browse are reported identically when `sys.Home` is empty and whatever `sys.Root`, `sys.NoColor`, `sys.Term`, `sys.Watcher`, `sys.Interrupt`, and `sys.Console` are, and, when `args` is not empty, whatever `sys.Terminal` and `sys.StdinTerminal` are.
- R-TJOX-Y7CY: A call of `Run` MUST be in *follow mode* if and only if `args` holds the follow option, `args` is list arguments, drawing arguments, or chat arguments, and `sys.Home` is not the empty string; every other call of `Run` is in *snapshot mode*. A call in follow mode MUST follow the view of its command, `list`, `tree`, or `chat`, as `D11-follow.md` defines.
- R-TKWU-BZ3N: When `args` holds the follow option and the call of `Run` is in snapshot mode, `Run` MUST write the same bytes to each stream and return the same value as it does for the same `sys` and the `args` obtained by removing every element of `args[1:]` that is the follow option, so that `["list", "-f", "claud"]` reports unknown harness `claud`, `["tree", "-f"]` the missing harness, `["chat", "--follow", "claude"]` the missing session id, `["list", "claude", "--follow=x", "-f"]` unknown option `--follow=x`, and `["list", "-f", "claude"]` with an empty `sys.Home` the home-directory diagnostic.
- R-X1XA-36ET: `Run` MUST make no call to any method of `sys.Root`, itself or through any function it calls, unless `sys.Home` is not the empty string and either `args` is list arguments, drawing arguments, or chat arguments, or the call opens the browser (`R-ROX5-PC7X`, `D01-layout-and-run-seam`).
- R-Z3TP-UHRR: When `Run` opens the browser (`R-ROX5-PC7X`, `D01-layout-and-run-seam`), its one call of `browse.Run` (`R-L6YX-30R6`, `D12-browse`; at most one, `R-53QX-NLO2`) MUST pass: as `ctx`, a `context.Context` whose `Done` method returns `sys.Interrupt` itself, a nil channel when `sys.Interrupt` is nil; as `cfg`, a `browse.Config` (`R-YGNM-KUOK`) whose `Home` is `sys.Home`, whose `Root` is `sys.Root`, whose `Color` is as `R-AH14-TZZ7` decides, whose `Watcher` is `sys.Watcher` converted to `browse.Watcher` and whose `Terminal` is `sys.Console` converted to `browse.Terminal` (`R-KPWB-Q8DG`), so that a nil `sys.Watcher` gives a nil `cfg.Watcher`; and as `stdout`, the pass-through writer `R-53QX-NLO2` states.
- R-T06J-TVHU: When `args` is list arguments and `sys.Home` is the empty string, `Run` MUST write exactly the home-directory diagnostic `"agent-monitor: cannot find the home directory: HOME is not set\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`.
- R-T1EG-7N8J: When `args` is list arguments with harness argument `h` and `sys.Home` is not the empty string, every harness function `Run` calls MUST be the `List` function of the package `internal/harness/<h>` — `claude.List` for `claude`, `codex.List` for `codex`, `grok.List` for `grok` — called with `sys.Home` as its home argument, and `Run` MUST call the `List` function of no other harness package and the `Tree` and `Chat` functions of no harness package, whether or not `args` holds the follow option.
- R-T2MC-LEZ8: When `args` is list arguments with harness argument `h` that do not hold the follow option and `sys.Home` is not the empty string, `Run` MUST call the `List` function of the package `internal/harness/<h>` exactly once, passing `sys.Root` and `sys.Home`.
- R-X5KZ-8HMW: When `args` is list arguments that do not hold the follow option and the harness `List` call `Run` makes returns sessions `s` and a nil error, `Run` MUST write exactly `session.Table(s)` to `stdout`, and, unless that write returns an error, MUST write nothing to `stderr` and return `ExitSuccess`.
- R-X6SV-M9DL: When `args` is list arguments and the first harness `List` call `Run` makes returns a non-nil error `err` such that `errors.As(err, &e)` holds for a variable `e` of type `*session.ReadError`, `Run` MUST write exactly the cannot-read diagnostic `"agent-monitor: cannot read " + quote.Field(e.Path) + ": " + e.Err.Error() + "\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`, whether or not `args` holds the follow option, so that, for example, a `Path` of `/home/dev/.claude/sessions` with a cause whose text is `permission denied` writes `agent-monitor: cannot read /home/dev/.claude/sessions: permission denied`.
- R-6H98-GPRJ: When `args` is drawing arguments and `sys.Home` is the empty string, `Run` MUST write exactly the home-directory diagnostic `"agent-monitor: cannot find the home directory: HOME is not set\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`.
- R-T6A1-QQ7B: When `args` is drawing arguments with harness argument `h` and session-id argument `id`, and `sys.Home` is not the empty string, every harness function `Run` calls MUST be the `Tree` function of the package `internal/harness/<h>` — `claude.Tree` for `claude`, `codex.Tree` for `codex`, `grok.Tree` for `grok` — called with `sys.Home` as its home argument and `id` as its session id, and `Run` MUST call the `Tree` function of no other harness package and the `List` and `Chat` functions of no harness package, whether or not `args` holds the follow option.
- R-T7HY-4HY0: When `args` is drawing arguments with harness argument `h` and session-id argument `id` that do not hold the follow option, and `sys.Home` is not the empty string, `Run` MUST call the `Tree` function of the package `internal/harness/<h>` exactly once, passing `sys.Root`, `sys.Home`, and `id`.
- R-X80S-014A: When `args` is drawing arguments that do not hold the follow option and the harness `Tree` call `Run` makes returns a tree `t` and a nil error, `Run` MUST write exactly `tree.Draw(t, c)` to `stdout`, where `c` is the colour decision for `args` and `sys`, and, unless that write returns an error, MUST write nothing to `stderr` and return `ExitSuccess`.
- R-6KWX-M0ZM: The colour decision for drawing arguments `args` and a `System` `sys` MUST be true if and only if `sys.Terminal` is true, `sys.NoColor` is the empty string, `sys.Term` is not byte-for-byte equal to `dumb`, and no element of `args[1:]` is byte-for-byte equal to `--no-color`, so that it is true for `Terminal` true, `NoColor` `""`, and each of `Term` `""`, `"xterm-256color"`, `"Dumb"`, and `"dumb "` with no `--no-color`, and false when, the rest being so, `Terminal` is false, or `NoColor` is `"1"` or `"0"`, or `Term` is `"dumb"`, or `args[1:]` holds `--no-color` once or more.
- R-X98O-DSUZ: When `args` is drawing arguments and the first harness `Tree` call `Run` makes returns a non-nil error `err` such that `errors.As(err, &e)` holds for a variable `e` of type `*session.ReadError`, `Run` MUST write exactly the cannot-read diagnostic `"agent-monitor: cannot read " + quote.Field(e.Path) + ": " + e.Err.Error() + "\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`, whether or not `args` holds the follow option, so that, for example, a `Path` of `/home/dev/.codex/sessions` with a cause whose text is `permission denied` writes `agent-monitor: cannot read /home/dev/.codex/sessions: permission denied`.
- R-TB5N-9T63: When the first harness `Tree` call `Run` makes for drawing arguments with harness argument `h` and session-id argument `id` returns a non-nil error `err` for which `errors.Is(err, tree.ErrNotFound)` holds, `Run` MUST write exactly the session-not-found diagnostic `"agent-monitor: no " + h + " session '" + quote.Arg(id) + "'\n"` to `stderr`, write nothing to `stdout`, and return `ExitNotFound`, whether or not `args` holds the follow option, so that `["tree", "claude", "bogus"]`, `["tree", "--no-color", "claude", "bogus"]`, and `["tree", "-f", "claude", "bogus"]` naming no Claude Code session each write exactly `agent-monitor: no claude session 'bogus'` and a newline.
- R-SRN9-5HAZ: `args` MUST be *chat arguments*, with harness argument `h`, session-id argument `id`, and agent argument `a`, if and only if `args[0]` is `chat` and the elements of `args[1:]` that are not the follow option, taken in order, are two or three elements, none of which begins with `-`, the first of which is `h`, one of `claude`, `codex`, or `grok`; `id` is then the second of them, and `a` is the third when there are three and `id` when there are two; so that `["chat", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]` is chat arguments with `a` equal to `id`, `["chat", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "a1c4e7f09b2d38561"]` and `["chat", "claude", "-f", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "a1c4e7f09b2d38561", "--follow"]` are chat arguments with `a` `a1c4e7f09b2d38561`, `["chat", "codex", "", ""]` is chat arguments with `id` and `a` empty, `["chat", "-f", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]` is chat arguments with `a` equal to `id`, and `["chat", "claude"]`, `["chat", "-f", "claude"]`, `["chat", "--no-color", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]`, `["chat", "--follow=x", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]`, and `["chat", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "a1c4e7f09b2d38561", "extra"]` are not.
- R-SSV5-J91O: When `args[0]` is `chat` and no element of `args[1:]` is `--help` or `-h`, `Run` MUST classify each element of `args[1:]` as follows: the follow option is never an unknown option, a harness argument, a session-id argument, an agent argument, or an unexpected argument, however many times and wherever it appears; every other element that begins with `-` is an unknown option, `--follow=x`, `-f=x`, `-fx`, `--no-color`, `--version`, `-V`, `-`, and `--` included; the first element that does not begin with `-` is the harness argument, a known harness if and only if it is byte-for-byte equal to one of `claude`, `codex`, or `grok`, and otherwise an unknown harness, the empty string and `Claude` included; the second element that does not begin with `-` is the session-id argument and the third the agent argument, each whatever its bytes, the empty string included; and every later element that does not begin with `-` is an unexpected argument.
- R-JSYB-ZKEJ: When `args[0]` is `chat`, no element of `args[1:]` is `--help` or `-h`, and some element of `args[1:]` is an unknown option, an unknown harness, or an unexpected argument, the first such element in order MUST decide the outcome and the elements after it MUST NOT affect it, so that `["chat", "claud"]`, `["chat", "claud", "--bogus"]`, and `["chat", "claud", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]` report unknown harness `claud`, `["chat", "--bogus"]`, `["chat", "--bogus", "claud"]`, and `["chat", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "--bogus"]` report unknown option `--bogus`, `["chat", "--no-color", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]` reports unknown option `--no-color`, and `["chat", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "a1c4e7f09b2d38561", "extra"]` and `["chat", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "a1c4e7f09b2d38561", "extra", "more"]` report unexpected argument `extra`.
- R-JU68-DC58: When `args[0]` is `chat` and the element of `args[1:]` that decides the outcome is an unknown option `arg`, `Run` MUST write exactly the unknown-option diagnostic `"agent-monitor: unknown option '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-JVE4-R3VX: When `args[0]` is `chat` and the element of `args[1:]` that decides the outcome is an unknown harness `arg`, `Run` MUST write exactly the unknown-harness diagnostic `"agent-monitor: unknown harness '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-JWM1-4VMM: When `args[0]` is `chat` and the element of `args[1:]` that decides the outcome is an unexpected argument `arg`, `Run` MUST write exactly the unexpected-argument diagnostic `"agent-monitor: unexpected argument '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-SU31-X0SD: When `args[0]` is `chat` and every element of `args[1:]` is the follow option, there being zero or more of them, `Run` MUST write exactly the missing-harness diagnostic `"agent-monitor: missing harness\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`, so that `["chat"]`, `["chat", "-f"]`, and `["chat", "--follow", "-f"]` each write it.
- R-SVAY-ASJ2: When `args[0]` is `chat` and the elements of `args[1:]` that are not the follow option are exactly `[h]` with `h` one of `claude`, `codex`, or `grok`, `Run` MUST write exactly the missing-session-id diagnostic `"agent-monitor: missing session id\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`, so that `["chat", "claude"]` and `["chat", "-f", "grok", "--follow"]` each write it.
- R-K09Q-A6UP: When `args` is chat arguments and `sys.Home` is the empty string, `Run` MUST write exactly the home-directory diagnostic `"agent-monitor: cannot find the home directory: HOME is not set\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`.
- R-J88L-KMGB: When `args` is chat arguments with harness argument `h`, session-id argument `id`, and agent argument `a`, and `sys.Home` is not the empty string, every harness function `Run` calls MUST be the `Chat` function of the package `internal/harness/<h>` — `claude.Chat` for `claude`, `codex.Chat` for `codex`, `grok.Chat` for `grok` — called with `sys.Home`, `id`, and `a` as its home, session id, and agent id, and `Run` MUST call the `Chat` function of no other harness package and the `List` and `Tree` functions of no harness package, whether or not `args` holds the follow option; in snapshot mode `Run` MUST call that `Chat` exactly once, and in follow mode it calls it as `D11-follow.md` defines.
- R-TDLG-1CNH: When `args` is chat arguments that do not hold the follow option and `sys.Home` is not the empty string, the root argument of the harness `Chat` call `Run` makes MUST be `sys.Root`.
- R-XAGK-RKLO: When `args` is chat arguments that do not hold the follow option and the harness `Chat` call `Run` makes returns a transcript `t`, entries `e`, and a nil error, `Run` MUST write to `stdout` exactly the concatenation of `chat.Format(x)` for each element `x` of `e`, in order, followed by `chat.TotalsLine(t.Usage(), t.Recorded())`, MUST make no call to `t.Read`, and, unless that write returns an error, MUST write nothing to `stderr` and return `ExitSuccess`; so that entries `e` that are empty give exactly the totals line.
- R-TH95-6NVK: When `args` is chat arguments that do not hold the follow option, the bytes `Run` writes to each stream and the value it returns MUST be the same whatever `sys.NoColor`, `sys.Term`, and `sys.Terminal` are.
- R-XBOH-5CCD: When `args` is chat arguments and the first harness `Chat` call `Run` makes returns a non-nil error `err` such that `errors.As(err, &e)` holds for a variable `e` of type `*session.ReadError`, `Run` MUST write exactly the cannot-read diagnostic `"agent-monitor: cannot read " + quote.Field(e.Path) + ": " + e.Err.Error() + "\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`, whether or not `args` holds the follow option, so that, for example, a `Path` of `/home/dev/.claude/projects/-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93/subagents/agent-a2d5f8e1c3b049672.jsonl` with a cause whose text is `permission denied` writes `agent-monitor: cannot read /home/dev/.claude/projects/-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93/subagents/agent-a2d5f8e1c3b049672.jsonl: permission denied`.
- R-NC7A-KHTL: When the first harness `Chat` call `Run` makes for chat arguments with harness argument `h` and session-id argument `id` returns a non-nil error `err` for which `errors.Is(err, tree.ErrNotFound)` holds, `Run` MUST write exactly the session-not-found diagnostic `"agent-monitor: no " + h + " session '" + quote.Arg(id) + "'\n"` to `stderr`, write nothing to `stdout`, and return `ExitNotFound`, whether or not `args` holds the follow option, so that `["chat", "claude", "bogus"]` and `["chat", "claude", "bogus", "a1c4e7f09b2d38561"]` naming no Claude Code session each write exactly `agent-monitor: no claude session 'bogus'` and a newline.
- R-NDF6-Y9KA: When the first harness `Chat` call `Run` makes for chat arguments with harness argument `h`, session-id argument `id`, and agent argument `a` returns a non-nil error `err` for which `errors.Is(err, chat.ErrAgentNotFound)` holds, `Run` MUST write exactly the agent-not-found diagnostic `"agent-monitor: no " + h + " agent '" + quote.Arg(a) + "' in session '" + quote.Arg(id) + "'\n"` to `stderr`, write nothing to `stdout`, and return `ExitNotFound`, whether or not `args` holds the follow option, so that `["chat", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "a0b1c2d3e4f506172"]` whose session holds no such agent writes exactly `agent-monitor: no claude agent 'a0b1c2d3e4f506172' in session '7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93'` and a newline.
- R-5CA8-BZUX: `Run` MUST deliver each diagnostic it writes to `stderr` as exactly one call to `stderr.Write`, and, when the call of `Run` is in snapshot mode and does not browse (`R-KSC4-HRUU`, `D01-layout-and-run-seam`), each product it writes to `stdout` as exactly one call to `stdout.Write`; in follow mode its calls to `stdout.Write` are as `D11-follow.md` defines, and when it browses they are as `R-53QX-NLO2` and `D12-browse` define.
- R-7BPS-795K: When a call `Run` makes to `stdout.Write` is the first to return a non-nil error `err`, whatever was being written — a product or, while `Run` browses (`R-KSC4-HRUU`, `D01-layout-and-run-seam`), the enter sequence, a screen, or the leave sequence — `Run` MUST make no further call to `stdout.Write` except that, when it has opened the browser (`R-ROX5-PC7X`), it MAY make at most one further call, passing exactly the leave sequence `"\x1b[?25h\x1b[?1049l"`, whether that call returns a nil or a non-nil error; MUST write exactly `"agent-monitor: write error: " + err.Error() + "\n"` to `stderr`; and MUST return `ExitWriteFailed`.
- R-2YYX-T3F8: When `Run` is writing the write-error diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitWriteFailed`.
- R-X4D2-UPW7: When `Run` is writing an unknown-option, unknown-command, unknown-harness, unexpected-argument, missing-harness, missing-session-id, or terminal-too-small (`R-ROX5-PC7X`, `D01-layout-and-run-seam`) diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitUsage`.
- R-ERSZ-G371: When `Run` is writing the home-directory diagnostic or a cannot-read diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitDataUnreadable`.
- R-KA0X-CCS9: When `Run` is writing the session-not-found or the agent-not-found diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitNotFound`.
- R-7CXO-L0W9: When `Run` returns `ExitUsage` it MUST have made no call to `stdout.Write`, and when it returns `ExitWriteFailed` the first call to `stdout.Write` that returned a non-nil error MUST be its last call to `stdout.Write` or, when it has opened the browser (`R-ROX5-PC7X`, `D01-layout-and-run-seam`), be followed by at most one further call to `stdout.Write`, passing exactly the leave sequence `"\x1b[?25h\x1b[?1049l"`, whatever that call returns, and, when the call of `Run` is in snapshot mode and does not browse (`R-KSC4-HRUU`), MUST be its only call to `stdout.Write`.
- R-ET0V-TUXQ: When `Run` returns `ExitDataUnreadable` it MUST have made no call to `stdout.Write`.
- R-KB8T-Q4IY: When `Run` returns `ExitNotFound` it MUST have made no call to `stdout.Write`.
- R-X356-GY5I: When `Run` returns `ExitSuccess` it MUST have made no call to `stderr.Write`, and every call `Run` makes to `stderr.Write` MUST pass exactly an unknown-option, unknown-command, unknown-harness, unexpected-argument, missing-harness, missing-session-id, home-directory, terminal-too-small (`R-ROX5-PC7X`, `D01-layout-and-run-seam`), cannot-read, session-not-found, agent-not-found, or write-error diagnostic, so that `Run` never writes `Usage`, `ListUsage`, `TreeUsage`, or `ChatUsage` to `stderr`.
