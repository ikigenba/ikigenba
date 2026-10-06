# Stories — package

The file that carries telemetry to a space. `devctl build telemetry` writes it from a commit that a `telemetry/v<semver>` tag points at, and `opsctl install` unpacks it into `/opt/telemetry/`. Its contents are the whole of what telemetry ships: the static `linux/amd64` binary, the manifest, the nginx fragment, and `share/icon.svg`, nothing else. `etc/nginx.conf` is telemetry's own nginx configuration, which the host includes in the server that answers at telemetry's public name, as it does for any app that ships one; its one effect is that `/ingest` answers 404 to the public (`S14-on-a-space.md`), and no story fixes its text beyond that effect. `share/icon.svg` is telemetry's icon, an SVG image a human draws; its presence is what lists telemetry in the platform's service launcher on a space (`S14-on-a-space.md`), and no story fixes its content beyond its being an SVG. telemetry keeps nothing under `etc/` but the manifest and the fragment and nothing under `share/` but the icon, so no other member exists. The files that give the pages their style, their launcher, and their button feedback are inside the binary (`S04-assets.md`), so no `assets/` directory and no font file ships beside it. The database is not in the file: telemetry creates `state/telemetry.db` under its working directory on first start (`S02-serve.md`), and the manifest's `[database]` table declares it so that the host keeps it across releases. The version is in the file's name and in the binary, never in a member's path.

## A developer lists what the file holds

Command:

```
$ tar -tJf telemetry/dist/telemetry-v<semver>.tar.xz | sort
```

Output:

```
bin/telemetry
etc/manifest.toml
etc/nginx.conf
share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build telemetry` wrote `telemetry/dist/telemetry-v<semver>.tar.xz`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the file holds

The binary inside the file is what a host will run, so it is asked the two things a host asks it.

Command:

```
$ tar -xJf telemetry/dist/telemetry-v<semver>.tar.xz -O bin/telemetry > /tmp/telemetry && chmod +x /tmp/telemetry
$ /tmp/telemetry --version
$ /tmp/telemetry manifest
```

Output:

```
v<semver>
app = "telemetry"
description = "The suite's trail of events"
default = false
mcp = true
secrets = []

[env]
RETENTION_DAYS = "15"

[database]
engine = "sqlite"
path = "state/telemetry.db"

[resources]
slice = "core"
memory_max = "256M"
```

Each command exits 0. The text is on stdout; stderr is empty. The version is the one in the file's name, and the manifest is byte for byte the file's `etc/manifest.toml`.

Preconditions:

- `devctl build telemetry` wrote `telemetry/dist/telemetry-v<semver>.tar.xz`.
- The developer's machine is `linux/amd64`, or can run such a binary.

Postconditions:

- Nothing in the checkout has changed.
