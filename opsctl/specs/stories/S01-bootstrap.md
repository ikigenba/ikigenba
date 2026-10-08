# Stories — bootstrap

Running opsctl at all: help, version, exit codes, and the refusal to run as
anyone but root. Every later group adds a command to this frame.

opsctl is run on one host, as root, over ssh, by an operator at a terminal and
by an agent — `devctl` from a developer's machine, or a systemd timer on the
host itself. Every story names which. A host runs one complete deployment of
the platform and knows nothing about any other host.

opsctl is read by an agent over ssh far more often than by a person. stdout
carries only the answer — a value, a list, a checklist line per step — with no
decoration, colour, or progress output. Every diagnostic goes to stderr as
`opsctl: <message>`, and any further detail follows after exactly one blank
line. Another program's output is quoted there with every line prefixed `> `,
so a reader can see at a glance which program is speaking; detail opsctl
writes itself, such as the next command to run, is unprefixed. The usage text
is never written to stderr.

Each group states the line it adds to the top-level usage text under
`Commands:`; bootstrap carries the frame with `version` alone. Each also
declares the configuration keys it introduces, and only those; a command's
help text lists every key that command reads, including keys another group
declared.

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
  version   print the release this opsctl belongs to

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

An opsctl belongs to the suite release it was unpacked with, so it answers
with that release, never a version of its own. It finds the release from where
its executable is, symlinks resolved:
`/opt/ikigenba/releases/<sha>/opsctl/bin/opsctl` belongs to the release
`<sha>`. On a released host `/usr/local/bin/opsctl` is a link to
`/opt/ikigenba/current/opsctl/bin/opsctl`, and `current` is a link to
`/opt/ikigenba/releases/<sha>`, so `opsctl` on the PATH answers with the
release the host runs. The answer is the release's display string: its label
and the first seven characters of its sha, `r142 (c604e32)`. The sha is the
one `/opt/ikigenba/releases/<sha>/release.json` names, and the label is the
contents of `/opt/ikigenba/releases/<sha>/label`, which the last `activate` of
that release wrote (`S10-releases.md`).

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
r142 (c604e32)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `/opt/ikigenba/current` is a link to
  `/opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18`, and
  `/usr/local/bin/opsctl` is a link to
  `/opt/ikigenba/current/opsctl/bin/opsctl`.
- `/opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/release.json`
  names the sha `c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18`, and the release's
  `label` holds `r142`.

Postconditions:

- Nothing has changed.

## An operator asks which opsctl a host has, and its release has no label

A release activated without a label is named by its sha alone.

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
c604e32
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- As for the previous story, except that
  `/opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/label`
  does not exist: the release's last `activate` gave no label.

Postconditions:

- Nothing has changed.

## An operator asks an opsctl that belongs to no release

An opsctl whose executable is not inside a release folder, such as one a
developer copied onto a host by hand, has no release to name. It prints an
empty line and still succeeds, so a caller that reads the line is never
refused.

Command:

```
$ /usr/local/bin/opsctl version
```

```
$ /usr/local/bin/opsctl -V
```

```
$ /usr/local/bin/opsctl --version
```

Output: one empty line.

```

```

Exits 0. The empty line is on stdout; stderr is empty.

Preconditions:

- `/usr/local/bin/opsctl` is a file, not a link into
  `/opt/ikigenba/releases/`.

Postconditions:

- Nothing has changed.

## An operator runs opsctl as an ordinary user

Every command opsctl has reads or writes something only root may touch —
`/etc/ikigenba/`, `/etc/nginx/`, `/etc/systemd/system/`, `/etc/opt/`,
`/opt/`, `/var/opt/` — so running it as anyone else is a mistake it refuses before it
touches anything.

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

Output: the usage text and the version line of the stories above, and the
`config` usage text.

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
