# Stories — release

opsctl is a tool a host installs, not a thing built on the host. A fresh host
has a shell, `curl`, and nothing of ours; one command has to end with
`/usr/local/bin/opsctl` on it, at a version the caller named. That command is
`install.sh`, published with the binary in opsctl's own release, and it is the
only part of opsctl that is not opsctl.

This group adds no command to the top-level usage.

**The release.** Tagging `opsctl/<version>`, the `<name>/v<semver>` shape
every sub-project in the checkout is tagged with, publishes a release whose
assets are, for that version: `opsctl-<version>-linux-amd64`, the static
binary; `checksums.txt`; and `install.sh`. They are reachable at
`https://github.com/ikigenba/ikigenba/releases/download/opsctl/<version>/<asset>`,
which is the shape a caller builds the installer's URL from.

**The version is the argument, not the URL.** `install.sh` takes the version
it is to install as its one operand, so the copy of the script a host keeps can
move that host to any version without being replaced first. A caller that
fetched the script from one version's assets and names another on the command
line gets the one it named: the two agreeing is not required, and the operand
is what wins.

## An agent installs opsctl on a fresh host

`devctl space create` has an instance with nothing on it and reaches it over
ssh. Two commands: fetch the installer, run it.

Command:

```
$ curl -fsSL -o /tmp/opsctl-install https://github.com/ikigenba/ikigenba/releases/download/opsctl/v0.1.0/install.sh
$ sudo bash /tmp/opsctl-install v0.1.0
```

Output:

```
opsctl v0.1.0 -> /usr/local/bin/opsctl
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- A Linux host with `curl`, `install`, and `sha256sum`, running as root.
- The host can reach `github.com`.
- The release `opsctl/v0.1.0` exists with its three assets.

Postconditions:

- `/usr/local/bin/opsctl` is the `v0.1.0` binary, mode `0755`, owned by root,
  put in place by a rename so no one ever ran a half-written file. `opsctl
  version` prints `v0.1.0`.
- Its sha256 matched the entry in that release's `checksums.txt`.
- `/usr/local/share/ikigenba/opsctl-install.sh` is a copy of the script that
  ran, so the host can be moved to another version without fetching anything
  first.
- Nothing under `/etc/ikigenba/`, `/etc/nginx/`, or `/opt/` was created or
  changed. Installing the binary configures nothing; that is `config set` and
  `init`.

## An operator moves a host to a newer opsctl

The copy the last install left behind takes the new version as its operand.
Nothing is fetched to fetch the fetcher. From the developer's machine this is
`devctl space init <domain> --opsctl <version>`, which runs exactly this
command over ssh and then `init`.

Command:

```
$ sudo bash /usr/local/share/ikigenba/opsctl-install.sh v0.2.0
```

Output:

```
opsctl v0.2.0 -> /usr/local/bin/opsctl
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `v0.1.0` is installed and left its copy of the script.
- The release `opsctl/v0.2.0` exists.

Postconditions:

- `/usr/local/bin/opsctl` is the `v0.2.0` binary and
  `/usr/local/share/ikigenba/opsctl-install.sh` is `v0.2.0`'s script.
- The host's configuration store, its nginx file, its units, and everything
  under `/opt/` are untouched. A new opsctl on an old host is still that
  host; what a new version changes about it is `init`'s to do on the next
  run, which is why `devctl space init` runs the two together.
- Installing `v0.2.0` again writes the same bytes and exits 0.

## An operator installs the version that is already there

Command:

```
$ sudo bash /usr/local/share/ikigenba/opsctl-install.sh v0.2.0
```

Output:

```
opsctl v0.2.0 -> /usr/local/bin/opsctl
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `v0.2.0` is already installed.

Postconditions:

- `/usr/local/bin/opsctl` is byte for byte what it was. The download and the
  checksum ran again; nothing else did.

## An operator runs the installer with no version

There is no default version. A host is at the version someone named, and a
script that guessed would make "which opsctl is on that host" unanswerable
from the command that put it there.

Command:

```
$ sudo bash /tmp/opsctl-install
```

Output:

```
opsctl-install: needs a version

usage: opsctl-install <version>
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- The installer is on the host.

Postconditions:

- Nothing has changed.

## An operator runs the installer as an ordinary user

Command:

```
$ bash /tmp/opsctl-install v0.1.0
```

Output:

```
opsctl-install: must run as root
```

Exits 3. The line is on stderr; stdout is empty. The refusal comes before
anything is downloaded, and it is the same exit code opsctl itself uses.

Preconditions:

- The effective user id is not 0.

Postconditions:

- Nothing has changed.

## An operator names a version that was never released

Command:

```
$ sudo bash /tmp/opsctl-install v9.9.9
```

Output:

```
opsctl-install: no release for v9.9.9

https://github.com/ikigenba/ikigenba/releases/download/opsctl/v9.9.9/checksums.txt: 404
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- No release `opsctl/v9.9.9` exists.

Postconditions:

- Nothing has changed. `/usr/local/bin/opsctl`, if there was one, is the
  version it was, and the saved copy of the installer is the one it was.

## The downloaded binary does not match its checksum

Command:

```
$ sudo bash /tmp/opsctl-install v0.1.0
```

Output:

```
opsctl-install: checksum mismatch for opsctl-v0.1.0-linux-amd64

expected e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
     got 5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The download was truncated, cached wrong, or tampered with.

Postconditions:

- Nothing was installed. The downloaded file was removed, and an opsctl that
  was already on the host is exactly as it was.

## The installed binary reports a different version

The last check before the script claims success: the binary it just put down
is asked which version it is, and it has to be the one that was asked for. A
mismatch means the release was built wrong, and the host must not be left
quietly running something other than what its operator named.

Command:

```
$ sudo bash /tmp/opsctl-install v0.1.0
```

Output:

```
opsctl-install: asked for v0.1.0 but the binary reports v0.0.9
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The release's binary carries a version string other than the tag's.

Postconditions:

- `/usr/local/bin/opsctl` is what it was before the command; the new binary
  was never renamed into place.
