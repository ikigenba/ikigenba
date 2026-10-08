# Stories — package

The file that carries sites to a space. `devctl build sites` writes it from a commit that a `sites/v<semver>` tag points at, and `opsctl install` unpacks it into `/opt/sites/`. Its contents are the whole of what sites ships: the static `linux/amd64` binary, the manifest, and `share/icon.svg`, nothing else. sites ships no `etc/nginx.conf`: the host's nginx serves it with the configuration every app gets, opened to guests outside `/mcp`, `/api`, and the paths under them because the manifest declares `guests = true` (opsctl's `S5-nginx.md`), and sites needs nothing of nginx beyond that. `share/icon.svg` is sites' icon, an SVG image a human draws; its presence is what lists sites in the platform's service launcher on a space (`S21`), and no story fixes its content beyond its being an SVG. sites keeps nothing under `etc/` but the manifest and nothing under `share/` but the icon, so no other member exists. The pages' templates are inside the binary, and the files that give the pages their style, their launcher, their button feedback, and their icon are appkit's, inside the binary too (`S04`), so no `assets/` directory and no font file ships beside it. Neither the database nor any tree is in the file: sites creates `state/sites.db` and `cache/sites/` under its working directory on first start (`S02`), the manifest's `[database]` table declares the database so that the host keeps it across releases, and `cache/` is rebuilt on demand (`S16`). The manifest's `[resources]` table is read by the host, which bounds sites and every `git` it runs with it (opsctl's `S7-apps.md`). The version is in the file's name and in the binary, never in a member's path.

## A developer lists what the file holds

Command:

```
$ tar -tJf sites/dist/sites-v<semver>.tar.xz | sort
```

Output:

```
bin/sites
etc/manifest.toml
share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build sites` wrote `sites/dist/sites-v<semver>.tar.xz`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the file holds

The binary inside the file is what a host will run, so it is asked the two things a host asks it.

Command:

```
$ tar -xJf sites/dist/sites-v<semver>.tar.xz -O bin/sites > /tmp/sites && chmod +x /tmp/sites
$ /tmp/sites --version
$ /tmp/sites manifest
```

Output:

```
v<semver>
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

Each command exits 0. The text is on stdout; stderr is empty. The version is the one in the file's name, and the manifest is byte for byte the file's `etc/manifest.toml`.

Preconditions:

- `devctl build sites` wrote `sites/dist/sites-v<semver>.tar.xz`.
- The developer's machine is `linux/amd64`, or can run such a binary.

Postconditions:

- Nothing in the checkout has changed.
