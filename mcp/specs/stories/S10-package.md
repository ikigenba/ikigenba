# Stories — package

The file that carries mcp to a space. `devctl build mcp` writes it from a commit that an `mcp/v<semver>` tag points at, and `opsctl install` unpacks it into `/opt/mcp/`. Its contents are the whole of what mcp ships: the static `linux/amd64` binary, the manifest, and `share/icon.svg`, nothing else. `share/icon.svg` is mcp's icon, the Tabler outline `plug-connected` icon as an SVG image, which a human prepares; its presence is what lists mcp in the platform's service launcher on a space (`S11`), and no story fixes its content beyond its being an SVG. mcp keeps nothing under `etc/` but the manifest and nothing under `share/` but the icon, so no other member exists. The connect page and the setup files are drawn by the binary itself (`S03`, `S12`), and so are the files that give the page its style, its launcher, and its button feedback (`S04`), so no `assets/` directory and no font file ships beside it. The version is in the file's name and in the binary, never in a member's path.

## A developer lists what the file holds

Command:

```
$ tar -tJf mcp/dist/mcp-v<semver>.tar.xz | sort
```

Output:

```
bin/mcp
etc/manifest.toml
share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build mcp` wrote `mcp/dist/mcp-v<semver>.tar.xz`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the file holds

The binary inside the file is what a host will run, so it is asked the two things a host asks it.

Command:

```
$ tar -xJf mcp/dist/mcp-v<semver>.tar.xz -O bin/mcp > /tmp/mcp && chmod +x /tmp/mcp
$ /tmp/mcp --version
$ /tmp/mcp manifest
```

Output:

```
v<semver>
app = "mcp"
description = "Connect AI assistants to your services"
default = false
mcp = false
secrets = []
```

Each command exits 0. The text is on stdout; stderr is empty. The version is the one in the file's name, and the manifest is byte for byte the file's `etc/manifest.toml`.

Preconditions:

- `devctl build mcp` wrote `mcp/dist/mcp-v<semver>.tar.xz`.
- The developer's machine is `linux/amd64`, or can run such a binary.

Postconditions:

- Nothing in the checkout has changed.
