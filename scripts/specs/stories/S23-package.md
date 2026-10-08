# Stories — package

scripts' part of the suite release, the file that carries the whole suite to a space. `devctl build <sha|tag>` writes the release from the commit it names, reading a tag only to find that commit and nothing of the working tree (devctl's `S4-build.md`). The release is `dist/<sha>.tar.xz` at the checkout root, where `<sha>` is that commit's full 40-character lowercase hexadecimal sha; its one top-level directory is `<sha>/`, which a deploy unpacks on the host as `/opt/ikigenba/releases/<sha>/`, and once that release is activated `/opt/ikigenba/current` names it. scripts' part is the directory `<sha>/scripts/`, and its contents are the whole of what scripts ships: the static `linux/amd64` binary, the manifest, the nginx fragment, and `share/icon.svg`, nothing else. The host's nginx serves scripts with the configuration every app behind the authenticator gets, since the manifest declares no `guests` (`S01`; opsctl's `S5-nginx.md`, `An operator reads the configuration of a host running apps behind the authenticator`). `etc/nginx.conf` is scripts' own nginx configuration, which the host includes in the server that answers at scripts' public name, as it does for any app that ships one; its one effect is that `/events` and `/declarations` answer 404 through nginx for every method (`S24`), and no story fixes its text beyond that effect. `share/icon.svg` is scripts' icon, an SVG image a human draws; its presence is what lists scripts in the platform's service launcher on a space (`S24`), and no story fixes its content beyond its being an SVG. scripts keeps nothing under `etc/` but the manifest and the fragment and nothing under `share/` but the icon, so no other member exists. The pages' templates are inside the binary, and the files that give the pages their style, their launcher, their button feedback, and their favicon are appkit's, inside the binary too (`S04`), so no `assets/` directory and no font file ships beside it. Neither `git` nor `python3.12` is in the release: both are the host's, `git` installed by the space's first boot (opsctl's `init` finds it on the `PATH`, opsctl's `S4-init.md`) and `python3.12` by the space's first boot or, on a space launched before that, by the operator (`S22`). Neither the database nor any run folder is in the release: scripts creates `state/scripts.db` and `state/runs/` under its working directory, `/var/opt/ikigenba/scripts` on a host, on first start (`S02`), the manifest's `[database]` table declares the database so that the host keeps it across releases and replicates it, and the run folders under `state/runs/` stay on the host across releases, as everything under `state/` does, but are not replicated (`S20`). The manifest's `[resources]` table is read by the host, which runs scripts among its other apps, bounds scripts, every `git` it runs and every script it runs together with it, and hands scripts its service's part of the control group tree, in which scripts bounds each run on its own (`S01`; opsctl's `S7-apps.md`). The commit is in the release's name and its top-level directory only. No version and no commit is recorded in the binary or in any member of scripts' directory, or in such a member's path: scripts learns which code it is running from its environment when it runs (`S01`).

## A developer lists what the release holds of scripts

Command:

```
$ tar -tJf dist/<sha>.tar.xz | grep '^<sha>/scripts/' | grep -v '/$' | sort
```

Output:

```
<sha>/scripts/bin/scripts
<sha>/scripts/etc/manifest.toml
<sha>/scripts/etc/nginx.conf
<sha>/scripts/share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build <sha>` wrote `dist/<sha>.tar.xz` at the checkout root (devctl's `S4-build.md`).

Postconditions:

- Nothing has changed.

## A developer checks the binary the release holds for scripts

The binary inside the release is what a host will run, so it is asked the two things a host asks it. Asked with neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE` set, it prints an empty line for its version: the binary carries no identity of its own, and the commit in the release's name is nowhere inside it.

Command:

```
$ tar -xJf dist/<sha>.tar.xz -O <sha>/scripts/bin/scripts > /tmp/scripts && chmod +x /tmp/scripts
$ /tmp/scripts --version
$ /tmp/scripts manifest
```

Output:

```

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
RUN_MEMORY_MAX_BYTES = "268435456"
RUNS_MEMORY_MAX_BYTES = "536870912"
RUNS_CPU_PERCENT = "100"
RUN_PIDS_MAX = "64"
RUN_MAX_ACTIVE = "2"
RUN_MAX_QUEUED = "10"
RUN_KEEP_DAYS = "15"
RUN_KEEP_COUNT = "10"

[database]
engine = "sqlite"
path = "state/scripts.db"

[resources]
slice = "apps"
memory_max = "896M"
go_memory_limit = "128M"
delegate = true
```

Each command exits 0. The text is on stdout; stderr is empty. `--version` prints one empty line, a single newline, and the manifest is byte for byte the release's `<sha>/scripts/etc/manifest.toml`, the one `S01` shows.

Preconditions:

- `devctl build <sha>` wrote `dist/<sha>.tar.xz` at the checkout root (devctl's `S4-build.md`).
- The developer's machine is `linux/amd64`, or can run such a binary.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty in the developer's environment.

Postconditions:

- Nothing in the checkout has changed.
