# Stories — package

What carries sites to a space. sites ships in the suite release, not as a file of its own: `devctl build <sha|tag>` builds every app at that commit alone, whatever the working tree holds, into `dist/<sha>.tar.xz` at the checkout root, where `<sha>` is the commit's full 40-character hexadecimal sha, and sites is that release's directory `<sha>/sites/` (devctl's `S4-build.md`). The host unpacks the release into `/opt/ikigenba/releases/<sha>/`, so once it is activated sites runs from `/opt/ikigenba/current/sites/`. That directory is the whole of what sites ships: the static `linux/amd64` binary, the manifest, and `share/icon.svg`, nothing else. sites ships no `etc/nginx.conf`: the host's nginx serves it with the configuration every app gets, opened to guests outside `/mcp`, `/api`, and the paths under them because the manifest declares `guests = true` (opsctl's `S5-nginx.md`), and sites needs nothing of nginx beyond that. `share/icon.svg` is sites' icon, an SVG image a human draws; its presence is what lists sites in the platform's service launcher on a space (`S21`), and no story fixes its content beyond its being an SVG. sites keeps nothing under `etc/` but the manifest and nothing under `share/` but the icon, so no other member exists. The pages' templates are inside the binary, and the files that give the pages their style, their launcher, their button feedback, and their icon are appkit's, inside the binary too (`S04`), so no `assets/` directory and no font file ships beside it. Neither the database nor any tree is in the release: sites creates `state/sites.db` and `cache/sites/` under its working directory on first start (`S02`), the manifest's `[database]` table declares the database so that the host keeps it across releases, and `cache/` is rebuilt on demand (`S16`). The manifest's `[resources]` table is read by the host, which bounds sites and every `git` it runs with it (opsctl's `S7-apps.md`). The commit is in the release's name and its top-level directory only. No version and no commit is recorded in the binary or in any member under `sites/`, or in a member's path below `<sha>/`: sites learns which code it is running from its environment when it runs (`S01`).

## A developer lists what the release holds of sites

Command:

```
$ tar -tJf dist/<sha>.tar.xz | grep '^<sha>/sites/.*[^/]$' | sort
```

Output:

```
<sha>/sites/bin/sites
<sha>/sites/etc/manifest.toml
<sha>/sites/share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build <sha>` wrote `dist/<sha>.tar.xz`, the release of the commit `<sha>`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the release holds

The binary inside the release is what a host will run, so it is asked the two things a host asks it. Asked with neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE` set, it prints an empty line for its version: the binary carries no identity of its own, and the commit in the release's name is nowhere inside it.

Command:

```
$ tar -xJf dist/<sha>.tar.xz -O <sha>/sites/bin/sites > /tmp/sites && chmod +x /tmp/sites
$ /tmp/sites --version
$ /tmp/sites manifest
```

Output:

```

app = "sites"
description = "Static sites from the suite's repositories"
default = false
mcp = true
guests = true
secrets = []

[env]
REPOS_DIR = "../repos/state/repos"
SITE_MAX_BYTES = "268435456"
OPERATION_SECONDS = "600"

[database]
engine = "sqlite"
path = "state/sites.db"

[resources]
memory_max = "128M"
```

Each command exits 0. The text is on stdout; stderr is empty. `--version` prints one empty line, a single newline, and the manifest is byte for byte the release's `<sha>/sites/etc/manifest.toml`.

Preconditions:

- `devctl build <sha>` wrote `dist/<sha>.tar.xz`, the release of the commit `<sha>`.
- The developer's machine is `linux/amd64`, or can run such a binary.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty in the developer's environment.

Postconditions:

- Nothing in the checkout has changed.
