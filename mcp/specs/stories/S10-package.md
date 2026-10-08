# Stories — package

The file that carries mcp to a space. `devctl build mcp` writes it from the commit checked out, in a tree with no uncommitted changes; it reads no tag. The file is `mcp/dist/mcp-<sha>.tar.xz`, where `<sha>` is that commit's full 40-character hexadecimal sha, and `opsctl install` unpacks it into `/opt/mcp/`. Its contents are the whole of what mcp ships: the static `linux/amd64` binary, the manifest, and `share/icon.svg`, nothing else. `share/icon.svg` is mcp's icon, the Tabler outline `plug-connected` icon as an SVG image, which a human prepares; its presence is what lists mcp in the platform's service launcher on a space (`S11`), and no story fixes its content beyond its being an SVG. mcp keeps nothing under `etc/` but the manifest and nothing under `share/` but the icon, so no other member exists. The connect page and the protected-resource metadata come from the binary itself (`S03`, `S12`), and so are the files that give the page its style, its launcher, and its button feedback (`S04`), so no `assets/` directory and no font file ships beside it. The commit is in the file's name only. No version and no commit is recorded in the binary or in any member, or in a member's path: mcp learns which code it is running from its environment when it runs (`S01`).

## A developer lists what the file holds

Command:

```
$ tar -tJf mcp/dist/mcp-<sha>.tar.xz | sort
```

Output:

```
bin/mcp
etc/manifest.toml
share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build mcp`, run in a clean tree at the commit `<sha>`, wrote `mcp/dist/mcp-<sha>.tar.xz`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the file holds

The binary inside the file is what a host will run, so it is asked the two things a host asks it. Asked with neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE` set, it prints an empty line for its version: the binary carries no identity of its own, and the commit in the file's name is nowhere inside it.

Command:

```
$ tar -xJf mcp/dist/mcp-<sha>.tar.xz -O bin/mcp > /tmp/mcp && chmod +x /tmp/mcp
$ /tmp/mcp --version
$ /tmp/mcp manifest
```

Output:

```

app = "mcp"
description = "Connect AI assistants to your services"
default = false
mcp = false
guests = true
secrets = []

[resources]
memory_max = "128M"
```

Each command exits 0. The text is on stdout; stderr is empty. `--version` prints one empty line, a single newline, and the manifest is byte for byte the file's `etc/manifest.toml`.

Preconditions:

- `devctl build mcp`, run in a clean tree at the commit `<sha>`, wrote `mcp/dist/mcp-<sha>.tar.xz`.
- The developer's machine is `linux/amd64`, or can run such a binary.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty in the developer's environment.

Postconditions:

- Nothing in the checkout has changed.
