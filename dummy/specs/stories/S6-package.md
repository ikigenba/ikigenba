# Stories — package

The file that carries dummy to a space. `devctl build dummy` writes it from a
commit that a `dummy/v<semver>` tag points at, and `opsctl install` unpacks it
into `/opt/dummy/`. Its contents are the whole of what dummy ships: the static
`linux/amd64` binary and the manifest, nothing else. dummy keeps nothing
under `etc/` but the manifest and has no `share/`, so no other member exists.
The version is in the file's name and in the binary, never in a member's
path.

## A developer lists what the file holds

Command:

```
$ tar -tJf dummy/dist/dummy-v<semver>.tar.xz | sort
```

Output:

```
bin/dummy
etc/manifest.toml
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build dummy` wrote `dummy/dist/dummy-v<semver>.tar.xz`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the file holds

The binary inside the file is what a host will run, so it is asked the two
things a host asks it.

Command:

```
$ tar -xJf dummy/dist/dummy-v<semver>.tar.xz -O bin/dummy > /tmp/dummy && chmod +x /tmp/dummy
$ /tmp/dummy --version
$ /tmp/dummy manifest
```

Output:

```
v<semver>
app = "dummy"
default = false
secrets = []
```

Each command exits 0. The text is on stdout; stderr is empty. The version is
the one in the file's name, and the manifest is byte for byte the file's
`etc/manifest.toml`.

Preconditions:

- `devctl build dummy` wrote `dummy/dist/dummy-v<semver>.tar.xz`.
- The developer's machine is `linux/amd64`, or can run such a binary.

Postconditions:

- Nothing in the checkout has changed.
