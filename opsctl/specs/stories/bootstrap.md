# Stories — bootstrap

Running opsctl at all: help, version, exit codes, and the refusal to run as
anyone but root. Every later group adds a command to this frame.

opsctl is read by an agent over ssh far more often than by a person. stdout
carries only the answer — a value, a list, a checklist line per step — with no
decoration, colour, or progress output. Every diagnostic goes to stderr as
`opsctl: <message>`, and any further detail follows after one blank line,
unprefixed. The usage text is never written to stderr.

## An operator asks opsctl what it can do

An operator who has just installed opsctl on a host, or an agent that has
forgotten a command, asks for the usage text. Each later group adds one line
under `Commands:`; nothing else in the text changes. The exit codes are in the
text so an agent never has to be told them out of band.

Command:

```
$ opsctl --help
```

```
$ opsctl -h
```

Output:

```
Usage: opsctl [options] <command> [arguments]

Operate the ikigenba platform host. Must run as root.

Commands:
  version   print the version

Options:
  -h, --help     print this help
  -V, --version  print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a preflight check failed
  3  refused: opsctl must run as root

Run 'opsctl <command> --help' for details on a command.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An operator asks which opsctl a host has

The version is a `var` in the source with a `v<major>.<minor>.<patch>` shape,
never injected at build time, so a developer's build and a release report the
same string. It is the string the installer was asked for.

Command:

```
$ opsctl version
```

```
$ opsctl -V
```

```
$ opsctl --version
```

Output:

```
v0.1.0
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An operator runs opsctl as an ordinary user

Every command opsctl has reads or writes something only root may touch —
`/etc/ikigenba/`, `/etc/nginx/`, `/etc/systemd/system/`, `/opt/` — so running
it as anyone else is a mistake it refuses before it touches anything.

Command:

```
$ opsctl init
```

```
$ opsctl config list
```

Output:

```
opsctl: must run as root
```

Exits 3. The line is on stderr; stdout is empty. No file under `/etc` was
read or written.

Preconditions:

- `opsctl` is installed on the host.
- The effective user id is not 0.

Postconditions:

- Nothing has changed.

## An operator asks for help as an ordinary user

Help and version are the only exemptions from the root check, so anyone on
the host can find out what opsctl is and which one it is.

Command:

```
$ opsctl --help
```

```
$ opsctl version
```

```
$ opsctl config --help
```

Output: the texts of the two stories above, and the `config` usage text.

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `opsctl` is installed on the host.
- The effective user id is not 0.

Postconditions:

- Nothing has changed.

## An agent runs opsctl with no command

Command:

```
$ opsctl
```

Output:

```
opsctl: no command given

see 'opsctl --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. The usage text itself is
never written to stderr.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An agent names a command that does not exist

An agent driving opsctl over ssh may be working from a newer or older
contract than the host has installed. The refusal names what it was given so
the mismatch is visible in the agent's own log.

Command:

```
$ opsctl bogus
```

Output:

```
opsctl: unknown command 'bogus'

see 'opsctl --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An agent names an option that does not exist

Only `-h`/`--help` and `-V`/`--version` are top-level options, and they come
before the command. Every other option belongs to a command.

Command:

```
$ opsctl --bogus
```

Output:

```
opsctl: unknown option '--bogus'

see 'opsctl --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.
