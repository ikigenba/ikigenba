# Stories — package

The file that carries events to a space. `devctl build events` writes it from the commit checked out, in a tree with no uncommitted changes; it reads no tag. The file is `events/dist/events-<sha>.tar.xz`, where `<sha>` is that commit's full 40-character hexadecimal sha, and `opsctl install` unpacks it into `/opt/events/`. Its contents are the whole of what events ships: the static `linux/amd64` binary, the manifest, the nginx fragment, and `share/icon.svg`, nothing else. The host's nginx serves events with the configuration every app behind the authenticator gets, since the manifest declares no `guests` (`S02`). `etc/nginx.conf` is events' own nginx configuration, which the host includes in the server that answers at events' public name, as it does for any app that ships one; its one effect is that `/emit` answers 404 through nginx for every method (`S17`), so only a process on the host that can reach events' socket can emit, and no story fixes its text beyond that effect. `share/icon.svg` is events' icon, an SVG image a human draws; its presence is what lists events in the platform's service launcher on a space (`S17`), and no story fixes its content beyond its being an SVG. events keeps nothing under `etc/` but the manifest and the fragment and nothing under `share/` but the icon, so no other member exists. The pages' templates are inside the binary, and the files that give the pages their style, their launcher, and their button feedback are appkit's, inside the binary too (`S04`), so no `assets/` directory and no font file ships beside it. The database is not in the file: events creates `state/events.db` under its working directory on first start (`S02`), and the manifest's `[database]` table declares it so that the host keeps it across releases and replicates it. The manifest carries no `[resources]` table. The commit is in the file's name only. No version and no commit is recorded in the binary or in any member, or in a member's path: events learns which code it is running from its environment when it runs (`S01`).

## A developer lists what the file holds

Command:

```
$ tar -tJf events/dist/events-<sha>.tar.xz | sort
```

Output:

```
bin/events
etc/manifest.toml
etc/nginx.conf
share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build events`, run in a clean tree at the commit `<sha>`, wrote `events/dist/events-<sha>.tar.xz`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the file holds

The binary inside the file is what a host will run, so it is asked the two things a host asks it. Asked with neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE` set, it prints an empty line for its version: the binary carries no identity of its own, and the commit in the file's name is nowhere inside it.

Command:

```
$ tar -xJf events/dist/events-<sha>.tar.xz -O bin/events > /tmp/events && chmod +x /tmp/events
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

Each command exits 0. The text is on stdout; stderr is empty. `--version` prints one empty line, a single newline, and the manifest is byte for byte the file's `etc/manifest.toml`, the one `S01` shows. The manifest has no `[resources]` table: the host puts no bounds of the manifest's on events.

Preconditions:

- `devctl build events`, run in a clean tree at the commit `<sha>`, wrote `events/dist/events-<sha>.tar.xz`.
- The developer's machine is `linux/amd64`, or can run such a binary.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty in the developer's environment.

Postconditions:

- Nothing in the checkout has changed.
