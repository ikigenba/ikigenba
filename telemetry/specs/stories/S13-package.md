# Stories — package

What telemetry ships in the suite release. telemetry has no file of its own: it ships in the one release of the whole suite, which `devctl build <sha|tag>` writes as `dist/<sha>.tar.xz` at the checkout root, `<sha>` being the commit's full 40-character hexadecimal sha, and `devctl deploy <space> <sha|tag>` unpacks on a host into `/opt/ikigenba/releases/<sha>/`. telemetry's tree in it is `<sha>/telemetry/`, which the host runs as `/opt/ikigenba/current/telemetry/` once that release is active. That tree is the whole of what telemetry ships: the static `linux/amd64` binary, the manifest, the nginx fragment, and `share/icon.svg`, nothing else. `etc/nginx.conf` is telemetry's own nginx configuration, which the host includes in the server that answers at telemetry's public name, as it does for any app that ships one; its one effect is that `/ingest` answers 404 to the public (`S14-on-a-space.md`), and no story fixes its text beyond that effect. `share/icon.svg` is telemetry's icon, an SVG image a human draws; its presence is what lists telemetry in the platform's service launcher on a space (`S14-on-a-space.md`), and no story fixes its content beyond its being an SVG. telemetry keeps nothing under `etc/` but the manifest and the fragment and nothing under `share/` but the icon, so no other member exists. The files that give the pages their style, their launcher, and their button feedback are inside the binary (`S04-assets.md`), so no `assets/` directory and no font file ships beside it. The database is not in the release: telemetry creates `state/telemetry.db` under its working directory on first start (`S02-serve.md`), and the manifest's `[database]` table declares it so that the host keeps it across deploys. The commit is in the release's name only. No version and no commit is recorded in the binary or in any member, or in a member's path: telemetry learns which code it is running from its environment when it runs (`S01-bootstrap.md`).

## A developer lists what telemetry's tree holds

Command:

```
$ tar -tJf dist/<sha>.tar.xz <sha>/telemetry | grep -v '/$' | sort
```

Output:

```
<sha>/telemetry/bin/telemetry
<sha>/telemetry/etc/manifest.toml
<sha>/telemetry/etc/nginx.conf
<sha>/telemetry/share/icon.svg
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
$ tar -xJf dist/<sha>.tar.xz -O <sha>/telemetry/bin/telemetry > /tmp/telemetry && chmod +x /tmp/telemetry
$ /tmp/telemetry --version
$ /tmp/telemetry manifest
```

Output:

```

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

Each command exits 0. The text is on stdout; stderr is empty. `--version` prints one empty line, a single newline, and the manifest is byte for byte the release's `<sha>/telemetry/etc/manifest.toml`.

Preconditions:

- `devctl build <sha>` wrote `dist/<sha>.tar.xz`, the suite release at the commit `<sha>`.
- The developer's machine is `linux/amd64`, or can run such a binary.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty in the developer's environment.

Postconditions:

- Nothing in the checkout has changed.
