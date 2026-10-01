# Stories — bootstrap

Running sandbox at all: help, version, the grammar, the usage errors, and the refusal to run as root. Every later group adds a command to this frame. The grammar is `sandbox [options] <command> [arguments]`, read left to right; short options do not bundle, and there is no `help` command. sandbox exits 0 on success, 1 when the operation failed (a build, a run of `git`, `go`, `systemctl`, `journalctl` or `nginx`, a file write), 2 on a usage error or an unmet precondition, and 3 when it refuses to run as root. Its product goes to stdout and its diagnostics to stderr. A diagnostic's first line begins `sandbox: `; any detail follows after exactly one empty line; another program's output is quoted there with every line prefixed `> `, and detail sandbox writes itself, such as the next command to run, is unprefixed. A usage error ends with a trailer after one empty line: `see 'sandbox --help' for usage` at the top level, or `see 'sandbox <command> --help' for usage` inside a command. The usage text is never written to stderr. A command whose product is a report (`ls`, `status`, `logs`, `url`) writes the whole report to stdout, findings included, and exits 0. sandbox parses its arguments before it does anything else, so a usage error is reported, with exit 2, whoever runs it; for a well-formed command line, the refusal to run as root comes before any other action, and help and version, top-level or a command's own, answer as root as they do for anyone.

## A developer asks sandbox what it can do

A developer who has just built sandbox, or who has forgotten a command, asks for the usage text. Help reads nothing and needs no git checkout. A command's own help is `sandbox <command> --help`, told in that command's group.

Command:

```
$ sandbox --help
```

```
$ sandbox -h
```

Output:

```
Usage: sandbox [options] <command> [arguments]

Run the whole suite from this worktree on this machine. Never run as root.

Commands:
  up        build every app and start the sandbox
  down      stop a sandbox and keep its data
  wipe      delete a stopped sandbox's data
  ls        list every known sandbox
  url       print the sandbox's URLs
  status    show the state of each unit
  logs      print the sandbox's journal
  token     print or store the sandbox's bearer token
  version   print the version

Options:
  -h, --help     print this help
  -V, --version  print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a precondition was not met
  3  refused: sandbox must not run as root

Run 'sandbox <command> --help' for details on a command.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `sandbox` is on `PATH`, built and installed from the checkout.

Postconditions:

- Nothing has changed.

## A developer asks which version of sandbox they have

The version is a variable the source declares, with a `v<major>.<minor>.<patch>` shape; the binary prints it verbatim. All three forms print the same line. Like help, version reads nothing and needs no git checkout.

Command:

```
$ sandbox version
```

```
$ sandbox --version
```

```
$ sandbox -V
```

Output:

```
<version>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer asks how to use `version`

`version` takes `--help` like every command, and like every command it takes no arguments.

Command:

```
$ sandbox version --help
```

```
$ sandbox version -h
```

Output:

```
Usage: sandbox version

Print the version of sandbox.

Options:
  -h, --help  print this help
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer passes version an argument

Command:

```
$ sandbox version x
```

Output:

```
sandbox: version takes no arguments

see 'sandbox version --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer runs sandbox with no command

Command:

```
$ sandbox
```

Output:

```
sandbox: no command given

see 'sandbox --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ sandbox bogus
```

Output:

```
sandbox: unknown command 'bogus'

see 'sandbox --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer asks for help as a command

There is no `help` command; help is the `--help` option. `help` is an unknown command like any other word.

Command:

```
$ sandbox help
```

Output:

```
sandbox: unknown command 'help'

see 'sandbox --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer mistypes an option

Command:

```
$ sandbox --bogus
```

Output:

```
sandbox: unknown option '--bogus'

see 'sandbox --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer bundles two short options

Short options do not bundle: `-hV` is one unknown option, not `-h` followed by `-V`.

Command:

```
$ sandbox -hV
```

Output:

```
sandbox: unknown option '-hV'

see 'sandbox --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.

Postconditions:

- Nothing has changed.

## A developer runs sandbox as root by mistake

sandbox runs as the developer: its units are the developer's user units, and its data, configuration and secrets are under the developer's own directories. Run as root, it would write all of that where the developer cannot use it, so every command refuses before it touches anything. The arguments are parsed first, so only a well-formed command line reaches the refusal; help and version answer as root, as the stories after this one tell. `up` and `ls` stand here for every command.

Command:

```
$ sudo sandbox up
```

```
$ sudo sandbox ls
```

Output:

```
sandbox: refusing to run as root
```

Exits 3. The line is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.
- The effective user id is 0.

Postconditions:

- Nothing has changed. No `git`, `go`, `systemctl`, `journalctl` or `nginx` was run, and no file was read or written.

## A developer asks for help as root

Help only prints, so it answers as root exactly as it does for anyone. A command's own help, such as `sudo sandbox up --help`, answers as root too.

Command:

```
$ sudo sandbox --help
```

Output: the usage text of `sandbox --help`, exactly as in the first story.

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `sandbox` is on `PATH`.
- The effective user id is 0.

Postconditions:

- Nothing has changed.

## A developer asks for the version as root

Version only prints, so it answers as root exactly as it does for anyone, in all three forms.

Command:

```
$ sudo sandbox version
```

```
$ sudo sandbox --version
```

```
$ sudo sandbox -V
```

Output:

```
<version>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `sandbox` is on `PATH`.
- The effective user id is 0.

Postconditions:

- Nothing has changed.

## A developer mistypes a command as root

The arguments are parsed before the effective user is looked at, so a usage error is reported as a usage error, exit 2, even as root. Any other usage error, such as `sudo sandbox up now`, is reported the same way.

Command:

```
$ sudo sandbox bogus
```

Output:

```
sandbox: unknown command 'bogus'

see 'sandbox --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `sandbox` is on `PATH`.
- The effective user id is 0.

Postconditions:

- Nothing has changed. No `git`, `go`, `systemctl`, `journalctl` or `nginx` was run.
