# Stories — bootstrap

Running agent-monitor at all: the bare run, help, version, the exit codes,
and the usage errors. agent-monitor is a local development tool, one binary a
developer runs on their own machine; it is never deployed to a space. The
binary is built from the checkout. Its commands are added to this frame by
later groups; the one it has is `list`.

## A developer runs agent-monitor

With nothing to do, agent-monitor shows what it can do: the bare run prints
the same help text as `agent-monitor --help`.

Command:

```
$ agent-monitor
```

Output:

```
Usage: agent-monitor [options]
       agent-monitor list <harness>

Observe the coding agents on this machine through their logs and hooks.

Commands:
  list <harness>  list the live root sessions of claude, codex, or grok

see 'agent-monitor <command> --help' for command options

Options:
  -h, --help      print this help
  -V, --version   print the version

Exit codes:
  0  success
  1  the output could not be written
  2  usage error
  3  the harness's session data could not be read
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer asks what agent-monitor can do

The description names what the tool is for. The help lists the commands
and the exit codes every command shares; a command's own help is
`agent-monitor <command> --help`.

Command:

```
$ agent-monitor --help
```

```
$ agent-monitor -h
```

Options:

- `--help`, `-h`: print the help text and exit.

Output:

```
Usage: agent-monitor [options]
       agent-monitor list <harness>

Observe the coding agents on this machine through their logs and hooks.

Commands:
  list <harness>  list the live root sessions of claude, codex, or grok

see 'agent-monitor <command> --help' for command options

Options:
  -h, --help      print this help
  -V, --version   print the version

Exit codes:
  0  success
  1  the output could not be written
  2  usage error
  3  the harness's session data could not be read
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.

Postconditions:

- Nothing has changed.

## A developer asks which agent-monitor they have

The version's shape is `v<semver>`: a `v`, then a semantic version,
prerelease and build metadata included. Its value is data and is not fixed
here.

Command:

```
$ agent-monitor --version
```

```
$ agent-monitor -V
```

Options:

- `--version`, `-V`: print the version and exit.

Output:

```
v<semver>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.

Postconditions:

- Nothing has changed.

## A developer asks for help alongside other arguments

Arguments are read strictly left to right, and the first argument that
decides the outcome wins; nothing after it is looked at. `--help` or `-h`
prints the help text and exits 0; `--version` or `-V` prints the version and
exits 0; an unknown option fails as an unknown option; an argument that is
not an option and not a command fails as an unknown command; a command,
`list`, hands the rest of the arguments to that command, which reads them by
its own rules. So when help comes first, any argument after it, known or
not, a command included, is ignored: `agent-monitor --help list` prints
this help, not the help of `list`. When the version option comes
first, as in `agent-monitor --version --help`, the version is printed
instead of the help text.

Command:

```
$ agent-monitor --help bogus
```

```
$ agent-monitor --help --version
```

```
$ agent-monitor -h --bogus
```

Options:

- `--help`, `-h`: print the help text and exit; nothing after it is looked
  at.
- `--version`, `-V`: print the version and exit; nothing after it is looked
  at, so after `--help` it is ignored.

Output:

```
Usage: agent-monitor [options]
       agent-monitor list <harness>

Observe the coding agents on this machine through their logs and hooks.

Commands:
  list <harness>  list the live root sessions of claude, codex, or grok

see 'agent-monitor <command> --help' for command options

Options:
  -h, --help      print this help
  -V, --version   print the version

Exit codes:
  0  success
  1  the output could not be written
  2  usage error
  3  the harness's session data could not be read
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/agent-monitor` exists.

Postconditions:

- Nothing has changed.

## A developer's output cannot be written

When agent-monitor cannot write its output, it says so on stderr and fails.
`<reason>` is the system's description of the failure and varies. The same
holds for any output agent-monitor writes: the help text, the version, or
the sessions `agent-monitor list <harness>` prints.

Command:

```
$ agent-monitor > /dev/full
```

Output:

```
agent-monitor: write error: <reason>
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `bin/agent-monitor` exists.
- stdout is a device that refuses writes (`/dev/full`).

Postconditions:

- Nothing has changed.

## A developer mistypes a command

agent-monitor's one command is `list`, so any other argument that is not an
option is an unknown command. Arguments are read left to right, so it fails at
the first argument that is not an option even when a help, version, or unknown
option follows it: `agent-monitor bogus --help` and
`agent-monitor bogus --bogus` both fail with unknown command 'bogus'.

The argument is echoed between single quotes as it was typed, except that
`\`, `'`, control characters, DEL, and bytes that are not valid UTF-8 are
escaped with a backslash (`\\`, `\'`, `\n`, `\t`, `\r`, and `\xHH` for any
other such byte), and characters that do not print, such as a line separator
or a right-to-left override, are escaped as `\uXXXX` (`\UXXXXXXXX` beyond
U+FFFF), hex in lowercase.
Printable text in any script, accents and emoji included, is echoed as
typed. So an argument can never forge a line of output:
`agent-monitor $'a\tb'` fails with `agent-monitor: unknown command 'a\tb'`,
and `agent-monitor $'a\u202eb'` fails with
`agent-monitor: unknown command 'a\u202eb'`.

Command:

```
$ agent-monitor bogus
```

```
$ agent-monitor bogus --help
```

Output:

```
agent-monitor: unknown command 'bogus'

see 'agent-monitor --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. The usage text itself is
never written to stderr.

Preconditions:

- `bin/agent-monitor` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes an option

The option is named as it was typed: a short one, `-x` say, fails the same
way with `agent-monitor: unknown option '-x'`. Short options do not bundle,
so `-hV` is one unknown option, `agent-monitor: unknown option '-hV'`. An
unknown option that comes before `--help` is read first, so it fails this
way and the help text is not printed. Likewise `agent-monitor --bogus bogus`
reports the option, not the command, because the option is seen first.

The option is echoed exactly as an unknown command is: between single quotes
as typed, except that `\`, `'`, control characters, DEL, and bytes that are
not valid UTF-8 are escaped with a backslash (`\\`, `\'`, `\n`, `\t`, `\r`,
and `\xHH` for any other such byte), and characters that do not print, such as
a line separator or a right-to-left override, are escaped as `\uXXXX`
(`\UXXXXXXXX` beyond U+FFFF), hex in lowercase; printable text in any script,
accents and emoji included, is echoed as typed. So it can never forge a line
of output.

Command:

```
$ agent-monitor --bogus
```

```
$ agent-monitor --bogus --help
```

Output:

```
agent-monitor: unknown option '--bogus'

see 'agent-monitor --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. The usage text itself is
never written to stderr.

Preconditions:

- `bin/agent-monitor` exists.

Postconditions:

- Nothing has changed.
