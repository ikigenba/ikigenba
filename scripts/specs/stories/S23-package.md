# Stories — package

The file that carries scripts to a space. `devctl build scripts` writes it from a commit that a `scripts/v<semver>` tag points at, and `opsctl install` unpacks it into `/opt/scripts/`. Its contents are the whole of what scripts ships: the static `linux/amd64` binary, the manifest, the nginx fragment, and `share/icon.svg`, nothing else. The host's nginx serves scripts with the configuration every app behind the authenticator gets, since the manifest declares no `guests` (`S01`; opsctl's `S5-nginx.md`, `An operator reads the configuration of a host running apps behind the authenticator`). `etc/nginx.conf` is scripts' own nginx configuration, which the host includes in the server that answers at scripts' public name, as it does for any app that ships one; its one effect is that `/events` and `/declarations` answer 404 through nginx for every method (`S24`), and no story fixes its text beyond that effect. `share/icon.svg` is scripts' icon, an SVG image a human draws; its presence is what lists scripts in the platform's service launcher on a space (`S24`), and no story fixes its content beyond its being an SVG. scripts keeps nothing under `etc/` but the manifest and the fragment and nothing under `share/` but the icon, so no other member exists. The pages' templates are inside the binary, and the files that give the pages their style, their launcher, and their button feedback are appkit's, inside the binary too (`S04`), so no `assets/` directory and no font file ships beside it. Neither `git` nor `python3.12` is in the file: both are the host's, `git` installed by the space's first boot (opsctl's `init` finds it on the `PATH`, opsctl's `S4-init.md`) and `python3.12` by the space's first boot or, on a space launched before that, by the operator (`S22`). Neither the database nor any run folder is in the file: scripts creates `state/scripts.db` and `state/runs/` under its working directory on first start (`S02`), the manifest's `[database]` table declares the database so that the host keeps it across releases and replicates it, and the run folders under `state/runs/` stay on the host across releases, as everything under `state/` does, but are not replicated (`S20`). The manifest's `[resources]` table is read by the host, which bounds scripts, every `git` it runs and every script it runs together with it (`S01`; opsctl's `S7-apps.md`). The version is in the file's name and in the binary, never in a member's path.

## A developer lists what the file holds

Command:

```
$ tar -tJf scripts/dist/scripts-v<semver>.tar.xz | sort
```

Output:

```
bin/scripts
etc/manifest.toml
etc/nginx.conf
share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build scripts` wrote `scripts/dist/scripts-v<semver>.tar.xz`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the file holds

The binary inside the file is what a host will run, so it is asked the two things a host asks it.

Command:

```
$ tar -xJf scripts/dist/scripts-v<semver>.tar.xz -O bin/scripts > /tmp/scripts && chmod +x /tmp/scripts
$ /tmp/scripts --version
$ /tmp/scripts manifest
```

Output:

```
v<semver>
app = "scripts"
description = "Python scripts run from the suite's repositories"
default = false
mcp = true
secrets = []

[env]
REPOS_DIR = "../repos/state/repos"
TREE_MAX_BYTES = "268435456"
OUTPUT_MAX_BYTES = "1048576"
OPERATION_SECONDS = "600"
SCRIPT_SECONDS = "600"
RUN_KEEP_DAYS = "15"
RUN_KEEP_COUNT = "10"

[database]
engine = "sqlite"
path = "state/scripts.db"

[resources]
cpu_weight = 50
memory_max = "1G"
io_weight = 50
```

Each command exits 0. The text is on stdout; stderr is empty. The version is the one in the file's name, and the manifest is byte for byte the file's `etc/manifest.toml`, the one `S01` shows.

Preconditions:

- `devctl build scripts` wrote `scripts/dist/scripts-v<semver>.tar.xz`.
- The developer's machine is `linux/amd64`, or can run such a binary.

Postconditions:

- Nothing in the checkout has changed.
