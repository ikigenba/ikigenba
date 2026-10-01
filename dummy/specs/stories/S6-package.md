# Stories — package

The file that carries dummy to a space. `devctl build dummy` writes it from a
commit that a `dummy/v<semver>` tag points at, and `opsctl install` unpacks it
into `/opt/dummy/`. Its contents are the whole of what dummy ships: the static
`linux/amd64` binary, the manifest, and `share/icon.svg`, nothing else.
`share/icon.svg` is dummy's icon, an SVG image a human draws; its presence is
what lists dummy in the platform's service launcher on a space
(`S7-on-a-space.md`), and no story fixes its content beyond its being an SVG.
dummy keeps nothing under `etc/` but the manifest and nothing under `share/`
but the icon, so no other member exists. The files that give the panel its
style and its launcher — the stylesheet, the fonts, their licences, and the
launcher's script — are inside the binary (`S8-assets.md`), so no `assets/`
directory and no font file ships beside it.
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
share/icon.svg
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
description = "Demo widgets to list and create"
default = false
mcp = true
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
