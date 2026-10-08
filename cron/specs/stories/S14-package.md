# Stories — package

The file that carries cron to a space. `devctl build cron` writes it from the commit checked out, in a tree with no uncommitted changes; it reads no tag. The file is `cron/dist/cron-<sha>.tar.xz`, where `<sha>` is that commit's full 40-character lowercase hexadecimal sha, and `opsctl install` unpacks it into `/opt/cron/`. Its contents are the whole of what cron ships: the static `linux/amd64` binary, the manifest, the nginx fragment, and `share/icon.svg`, nothing else. The host's nginx serves cron with the configuration every app behind the authenticator gets, since the manifest declares `guests = false` (`S01`; opsctl's `S5-nginx.md`, `An operator reads the configuration of a host running apps behind the authenticator`). `etc/nginx.conf` is cron's own nginx configuration, which the host includes in the server that answers at cron's public name, as it does for any app that ships one; its one effect is that `/events` and `/declarations` answer 404 through nginx for every method (`S15`), and no story fixes its text beyond that effect. `share/icon.svg` is cron's icon, an SVG image a human draws; its presence is what lists cron in the platform's service launcher on a space (`S15`), and no story fixes its content beyond its being an SVG. cron keeps nothing under `etc/` but the manifest and the fragment and nothing under `share/` but the icon, so no other member exists. The pages' templates are inside the binary, and the files that give the pages their style, their launcher, their button feedback, and their favicon are appkit's, inside the binary too (`S04`), so no `assets/` directory and no font file ships beside it. The database is not in the file: cron creates `state/cron.db` under its working directory on first start (`S02`), and the manifest's `[database]` table declares it so that the host keeps it across releases and replicates it. The manifest's `[resources]` table is read by the host, which bounds cron with it (`S01`; opsctl's `S7-apps.md`).

## A developer lists what the file holds

Command:

```
$ tar -tJf cron/dist/cron-<sha>.tar.xz | sort
```

Output:

```
bin/cron
etc/manifest.toml
etc/nginx.conf
share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build cron`, run in a clean tree at the commit `<sha>`, wrote `cron/dist/cron-<sha>.tar.xz`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the file holds

The binary inside the file is what a host will run, so it is asked the two things a host asks it. Asked with neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE` set, it prints an empty line for its version: the binary carries no identity of its own, and the commit in the file's name is nowhere inside it.

Command:

```
$ tar -xJf cron/dist/cron-<sha>.tar.xz -O bin/cron > /tmp/cron && chmod +x /tmp/cron
$ /tmp/cron --version
$ /tmp/cron manifest
```

Output:

```

app = "cron"
description = "Triggers that emit events on a schedule"
default = false
mcp = true
guests = false
secrets = []

[database]
engine = "sqlite"
path = "state/cron.db"

[resources]
memory_max = "128M"
```

Each command exits 0. The text is on stdout; stderr is empty. `--version` prints one empty line, a single newline, and the manifest is byte for byte the file's `etc/manifest.toml`, the one `S01` shows.

Preconditions:

- `devctl build cron`, run in a clean tree at the commit `<sha>`, wrote `cron/dist/cron-<sha>.tar.xz`.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty in the developer's environment.
- The developer's machine is `linux/amd64`, or can run such a binary.

Postconditions:

- Nothing in the checkout has changed.
