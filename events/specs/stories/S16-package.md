# Stories — package

The file that carries events to a space is the suite's release. `devctl build <sha|tag>` writes it from the commit it names, a tag being read only to find that commit, as `dist/<sha>.tar.xz` at the checkout root, where `<sha>` is that commit's full 40-character hexadecimal sha; events' part of it is the directory `<sha>/events/`, which a host unpacks into `/opt/ikigenba/releases/<sha>/events/` and runs from `/opt/ikigenba/current/events/` once that release is active. That directory's contents are the whole of what events ships: the static `linux/amd64` binary, the manifest, the nginx fragment, and `share/icon.svg`, nothing else. The host's nginx serves events with the configuration every app behind the authenticator gets, since the manifest declares no `guests` (`S02`). `etc/nginx.conf` is events' own nginx configuration, which the host includes in the server that answers at events' public name, as it does for any app that ships one; its one effect is that `/emit` answers 404 through nginx for every method (`S17`), so only a process on the host that can reach events' socket can emit, and no story fixes its text beyond that effect. `share/icon.svg` is events' icon, an SVG image a human draws; its presence is what lists events in the platform's service launcher on a space (`S17`), and no story fixes its content beyond its being an SVG. events keeps nothing under `etc/` but the manifest and the fragment and nothing under `share/` but the icon, so no other member exists under `<sha>/events/`. The pages' templates are inside the binary, and the files that give the pages their style, their launcher, and their button feedback are appkit's, inside the binary too (`S04`), so no `assets/` directory and no font file ships beside it. The database is not in the file: events creates `state/events.db` under its working directory, `/var/opt/ikigenba/events` on a host, on first start (`S02`), and the manifest's `[database]` table declares it so that the host keeps it across releases and replicates it. The manifest carries no `[resources]` table. The commit is in the file's name and its top-level directory only. No version and no commit is recorded in the binary or in any of events' members, or in a member's path below `<sha>/events/`: events learns which code it is running from its environment when it runs (`S01`).

## A developer lists what the file holds

Command:

```
$ tar -tJf dist/<sha>.tar.xz | grep '^<sha>/events/' | grep -v '/$' | sort
```

Output:

```
<sha>/events/bin/events
<sha>/events/etc/manifest.toml
<sha>/events/etc/nginx.conf
<sha>/events/share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build <sha>` wrote `dist/<sha>.tar.xz`, the release of the commit `<sha>`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the file holds

The binary inside the file is what a host will run, so it is asked the two things a host asks it. Asked with neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE` set, it prints an empty line for its version: the binary carries no identity of its own, and the commit in the file's name is nowhere inside it.

Command:

```
$ tar -xJf dist/<sha>.tar.xz -O <sha>/events/bin/events > /tmp/events && chmod +x /tmp/events
$ /tmp/events --version
$ /tmp/events manifest
```

Output:

```

app = "events"
description = "The suite's internal event bus"
default = false
mcp = true
secrets = []

[env]
EVENTS_DEPTH_MAX = "8"
EVENTS_DELIVERY_TIMEOUT_SECONDS = "5"
EVENTS_DELIVERY_ATTEMPTS = "10"
EVENTS_INFLIGHT_MAX = "4"
EVENTS_RETENTION_DAYS = "2"
EVENTS_DECLARATIONS_SECONDS = "60"

[database]
engine = "sqlite"
path = "state/events.db"
```

Each command exits 0. The text is on stdout; stderr is empty. `--version` prints one empty line, a single newline, and the manifest is byte for byte the file's `<sha>/events/etc/manifest.toml`, the one `S01` shows. The manifest has no `[resources]` table: the host puts no bounds of the manifest's on events.

Preconditions:

- `devctl build <sha>` wrote `dist/<sha>.tar.xz`, the release of the commit `<sha>`.
- The developer's machine is `linux/amd64`, or can run such a binary.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty in the developer's environment.

Postconditions:

- Nothing in the checkout has changed.
