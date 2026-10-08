# Stories — package

What mcp ships in the suite release. mcp has no file of its own: it ships in the one release of the whole suite, which `devctl build <sha|tag>` writes as `dist/<sha>.tar.xz` at the checkout root, `<sha>` being the commit's full 40-character hexadecimal sha, and `devctl deploy <space> <sha|tag>` unpacks on a host into `/opt/ikigenba/releases/<sha>/`. mcp's tree in it is `<sha>/mcp/`, which the host runs as `/opt/ikigenba/current/mcp/` once that release is active. That tree is the whole of what mcp ships: the static `linux/amd64` binary, the manifest, and `share/icon.svg`, nothing else. `share/icon.svg` is mcp's icon, the Tabler outline `plug-connected` icon as an SVG image, which a human prepares; its presence is what lists mcp in the platform's service launcher on a space (`S11`), and no story fixes its content beyond its being an SVG. mcp keeps nothing under `etc/` but the manifest and nothing under `share/` but the icon, so no other member exists. The connect page and the protected-resource metadata come from the binary itself (`S03`, `S12`), and so are the files that give the page its style, its launcher, and its button feedback (`S04`), so no `assets/` directory and no font file ships beside it. The commit is in the release's name only. No version and no commit is recorded in the binary or in any member, or in a member's path: mcp learns which code it is running from its environment when it runs (`S01`).

## A developer lists what mcp's tree holds

Command:

```
$ tar -tJf dist/<sha>.tar.xz <sha>/mcp | grep -v '/$' | sort
```

Output:

```
<sha>/mcp/bin/mcp
<sha>/mcp/etc/manifest.toml
<sha>/mcp/share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build <sha>` wrote `dist/<sha>.tar.xz`, the suite release at the commit `<sha>`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the release holds

The binary inside the release is what a host will run, so it is asked the two things a host asks it. Asked with neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE` set, it prints an empty line for its version: the binary carries no identity of its own, and the commit in the release's name is nowhere inside it.

Command:

```
$ tar -xJf dist/<sha>.tar.xz -O <sha>/mcp/bin/mcp > /tmp/mcp && chmod +x /tmp/mcp
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

Each command exits 0. The text is on stdout; stderr is empty. `--version` prints one empty line, a single newline, and the manifest is byte for byte the release's `<sha>/mcp/etc/manifest.toml`.

Preconditions:

- `devctl build <sha>` wrote `dist/<sha>.tar.xz`, the suite release at the commit `<sha>`.
- The developer's machine is `linux/amd64`, or can run such a binary.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty in the developer's environment.

Postconditions:

- Nothing in the checkout has changed.
