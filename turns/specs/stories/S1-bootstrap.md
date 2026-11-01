# Stories — bootstrap

Running turns at all: help, version, exit codes, the refusal to run as root, and the refusals for a missing or mistyped command or option. Every later group adds a command to this frame. turns runs under a person's own account, by hand at a terminal and from that account's user timer: on a laptop installed by hand, on a host at `/usr/local/bin/turns`. stdout carries only the product: a value, a report, a usage text, with no decoration, colour or progress output. A report is product, bad news included, and goes to stdout whole; on failure stderr says only why the command failed. Every diagnostic goes to stderr as `turns: <message>`, and any further detail follows after exactly one empty line; another program's output is quoted there with every line prefixed `> `, and detail turns writes itself is unprefixed. The usage text is never written to stderr. turns exits 0 on success, 1 when the operation failed, 2 on a usage error, 3 when it refuses to run as root, and 4 when it refuses because it is not configured. Each later group states the line it adds to the top-level usage text under `Commands:`; bootstrap carries the frame with `version` and `help` alone.

## A person asks turns what it can do

A person who has just installed turns, or who has forgotten a command, asks for the usage text. Bootstrap lists only `version` and `help` under `Commands:`. Each later group adds one line there, worded as that group's opening paragraph words it, and nothing else in the text changes; the full text, with every group's line, is shown below too. The exit codes are in the text so a person reading a timer's log can decode them without being told. `turns help` is the same as `--help`.

Command:

```
$ turns --help
```

```
$ turns -h
```

```
$ turns help
```

Output:

```
Usage: turns [options] <command> [arguments]

Push this account's agent session turns to an ikigenba space. Never run as root.

Commands:
  version   print the version
  help      print this help

Options:
  -h, --help     print this help
  --version      print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error
  3  refused: turns must not run as root
  4  refused: not configured

Run 'turns <command> --help' for details on a command.
```

Output (with every group's line):

The same text with the lines configure, install, push and status add, in that order, before `version`:

```
Usage: turns [options] <command> [arguments]

Push this account's agent session turns to an ikigenba space. Never run as root.

Commands:
  configure store the space and token turns pushes with
  install   install the user timer that pushes every five minutes
  push      push new turns to the space
  status    show what is here and what the space holds
  version   print the version
  help      print this help

Options:
  -h, --help     print this help
  --version      print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error
  3  refused: turns must not run as root
  4  refused: not configured

Run 'turns <command> --help' for details on a command.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `turns` is on the person's PATH.

Postconditions:

- Nothing has changed.

## A person asks which turns they have

Both forms print the version turns was released as, a `v<major>.<minor>.<patch>` shape; a build from the checkout and a release report the same string.

Command:

```
$ turns version
```

```
$ turns --version
```

Output:

```
v<major>.<minor>.<patch>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `turns` is on the person's PATH.

Postconditions:

- Nothing has changed.

## A person runs turns with no command

Command:

```
$ turns
```

Output:

```
turns: no command given

see 'turns --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. The usage text itself is never written to stderr.

Preconditions:

- `turns` is on the person's PATH.

Postconditions:

- Nothing has changed.

## A person mistypes a command

The refusal names what it was given, so a timer's log shows the mismatch when a unit names a command this turns does not have.

Command:

```
$ turns bogus
```

Output:

```
turns: unknown command 'bogus'

see 'turns --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `turns` is on the person's PATH.

Postconditions:

- Nothing has changed. No session file was read and nothing was sent.

## A person mistypes an option

Only the help and version options are top-level; every other option belongs to a command.

Command:

```
$ turns --bogus
```

Output:

```
turns: unknown option '--bogus'

see 'turns --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `turns` is on the person's PATH.

Postconditions:

- Nothing has changed. No session file was read and nothing was sent.

## A person runs turns as root by mistake

turns reads the session files in the home of the account it runs as and pushes them under that account's token, and writes nothing a root user should own, so running it as root is a mistake it refuses before it looks at the arguments, help and version included.

Command:

```
$ sudo turns version
```

```
$ sudo turns --help
```

```
$ sudo turns bogus
```

```
$ sudo turns
```

Output:

```
turns: must not run as root
```

Exits 3. The line is on stderr; stdout is empty. The arguments were not looked at.

Preconditions:

- `turns` is on the PATH.
- The effective user id is 0.

Postconditions:

- Nothing has changed. No session file was read and nothing was sent.
