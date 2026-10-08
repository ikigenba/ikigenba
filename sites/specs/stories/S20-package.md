# Stories — package

The file that carries sites to a space. `devctl build sites` writes it from the commit checked out, in a tree with no uncommitted changes; it reads no tag. The file is `sites/dist/sites-<sha>.tar.xz`, where `<sha>` is that commit's full 40-character hexadecimal sha, and `opsctl install` unpacks it into `/opt/sites/`. Its contents are the whole of what sites ships: the static `linux/amd64` binary, the manifest, and `share/icon.svg`, nothing else. sites ships no `etc/nginx.conf`: the host's nginx serves it with the configuration every app gets, opened to guests outside `/mcp`, `/api`, and the paths under them because the manifest declares `guests = true` (opsctl's `S5-nginx.md`), and sites needs nothing of nginx beyond that. `share/icon.svg` is sites' icon, an SVG image a human draws; its presence is what lists sites in the platform's service launcher on a space (`S21`), and no story fixes its content beyond its being an SVG. sites keeps nothing under `etc/` but the manifest and nothing under `share/` but the icon, so no other member exists. The pages' templates are inside the binary, and the files that give the pages their style, their launcher, their button feedback, and their icon are appkit's, inside the binary too (`S04`), so no `assets/` directory and no font file ships beside it. Neither the database nor any tree is in the file: sites creates `state/sites.db` and `cache/sites/` under its working directory on first start (`S02`), the manifest's `[database]` table declares the database so that the host keeps it across releases, and `cache/` is rebuilt on demand (`S16`). The manifest's `[resources]` table is read by the host, which bounds sites and every `git` it runs with it (opsctl's `S7-apps.md`). The commit is in the file's name only. No version and no commit is recorded in the binary or in any member, or in a member's path: sites learns which code it is running from its environment when it runs (`S01`).

## A developer lists what the file holds

Command:

```
$ tar -tJf sites/dist/sites-<sha>.tar.xz | sort
```

Output:

```
bin/sites
etc/manifest.toml
share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build sites`, run in a clean tree at the commit `<sha>`, wrote `sites/dist/sites-<sha>.tar.xz`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the file holds

The binary inside the file is what a host will run, so it is asked the two things a host asks it. Asked with neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE` set, it prints an empty line for its version: the binary carries no identity of its own, and the commit in the file's name is nowhere inside it.

Command:

```
$ tar -xJf sites/dist/sites-<sha>.tar.xz -O bin/sites > /tmp/sites && chmod +x /tmp/sites
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

Each command exits 0. The text is on stdout; stderr is empty. `--version` prints one empty line, a single newline, and the manifest is byte for byte the file's `etc/manifest.toml`.

Preconditions:

- `devctl build sites`, run in a clean tree at the commit `<sha>`, wrote `sites/dist/sites-<sha>.tar.xz`.
- The developer's machine is `linux/amd64`, or can run such a binary.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty in the developer's environment.

Postconditions:

- Nothing in the checkout has changed.
