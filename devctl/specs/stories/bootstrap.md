# Stories — bootstrap

Running devctl at all: help, version, the account option, exit codes, and the
refusal to run as root. Every later group adds a command to this frame.

## A developer asks devctl what it can do

A developer who has just built devctl, or who has forgotten a command, asks
for the usage text. Each later group adds one line under `Commands:`; nothing
else in the text changes. The single-dash forms of the two options are
accepted and not listed.

Command:

```
$ devctl --help
```

Output:

```
Usage: devctl [options] <command> [arguments]

Manage the ikigenba platform from the developer's machine. Never run as root.

Commands:
  version   print the version

Options:
  --help              print this help
  --version           print the version
  --account <name>    AWS shared-config profile to act in

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a preflight check failed
  3  refused: devctl must not run as root

Run 'devctl <command> --help' for details on a command.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer asks which devctl they have

The version is a `var` in the source with a `v<major>.<minor>.<patch>` shape,
never injected at build time, so a developer's build and a release report the
same string.

Command:

```
$ devctl version
```

```
$ devctl --version
```

Output:

```
v0.1.0
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer names the account a command acts in

Every command that touches AWS acts in exactly one account. The developer
names it with `--account <name>`, where `<name>` is an AWS shared-config
profile; for this project that is the account id, `602773793009` or
`295229566359`. Nothing is derived from the name; devctl hands it to the AWS
SDK's shared-config loader and nothing more. This group declares and parses
the option; the commands in later groups read it. Here it is given to
`version`, which ignores it.

Command:

```
$ devctl --account 602773793009 version
```

Output:

```
v0.1.0
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made; the profile name was accepted,
  not checked.

## A developer gives the account option no value

Command:

```
$ devctl --account version
```

```
$ devctl --account= version
```

Output:

```
devctl: option '--account' requires a value

see 'devctl --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer runs devctl with no command

Command:

```
$ devctl
```

Output:

```
devctl: no command given

see 'devctl --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. The usage text itself is
never written to stderr.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ devctl bogus
```

Output:

```
devctl: unknown command 'bogus'

see 'devctl --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes an option

Command:

```
$ devctl --bogus
```

Output:

```
devctl: unknown option '--bogus'

see 'devctl --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer runs devctl as root by mistake

devctl acts under the developer's own identity and writes nothing a root user
should own, so running it as root is a mistake it refuses before it looks at
the arguments, help and version included.

Command:

```
$ sudo devctl version
```

```
$ sudo devctl --help
```

```
$ sudo devctl bogus
```

```
$ sudo devctl
```

Output:

```
devctl: must not run as root
```

Exits 3. The line is on stderr; stdout is empty. The arguments were not
looked at.

Preconditions:

- `bin/devctl` exists.
- The effective user id is 0.

Postconditions:

- Nothing has changed.
