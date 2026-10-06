# Stories — package

The file that carries repos to a space. `devctl build repos` writes it from a commit that a `repos/v<semver>` tag points at, and `opsctl install` unpacks it into `/opt/repos/`. Its contents are the whole of what repos ships: the static `linux/amd64` binary, the manifest, the nginx fragment, and `share/icon.svg`, nothing else. `etc/nginx.conf` is repos' own nginx configuration, which the host includes in the server that answers at repos' public name, as it does for any app that ships one; its effects are that nginx sets no limit on the size of a request body, so a push is refused only by repos' own `PUSH_MAX_BYTES` (`S12-limits.md`), that nginx passes a request body to repos as it arrives rather than spooling it first, so a push streams through, that nginx keeps its connection to repos open while repos works, giving up only after an hour in which nothing has passed either way, longer than a git operation waits for its slot and runs under the default `QUEUE_SECONDS` and `OPERATION_SECONDS`, and that nginx still buffers repos' answers, so a slow client does not hold a git process open, and that nginx answers `/events` and `/declarations` 404 to the public for every method, so they stay on repos' socket (`S17-on-a-space.md`); no story fixes its text beyond those effects. The challenge git needs before it sends its credential is not the fragment's: the host's nginx gives it on every app's server (opsctl's `S5-nginx.md`). `share/icon.svg` is repos' icon, an SVG image a human draws; its presence is what lists repos in the platform's service launcher on a space (`S17-on-a-space.md`), and no story fixes its content beyond its being an SVG. repos keeps nothing under `etc/` but the manifest and the fragment and nothing under `share/` but the icon, so no other member exists. The files that give the pages their style, their launcher, and their button feedback — the stylesheet, the fonts, their licences, the launcher's script, and the button feedback script — are inside the binary (`S04-assets.md`), so no `assets/` directory and no font file ships beside it. Neither the database nor any repository is in the file: repos creates `state/repos.db` and `state/repos/` under its working directory on first start (`S02-serve.md`), and the manifest's `[database]` table declares the database so that the host keeps it across releases. The manifest's `[resources]` table is read by the host, which bounds repos and every `git` it runs with it (opsctl's `S7-apps.md`). The version is in the file's name and in the binary, never in a member's path.

## A developer lists what the file holds

Command:

```
$ tar -tJf repos/dist/repos-v<semver>.tar.xz | sort
```

Output:

```
bin/repos
etc/manifest.toml
etc/nginx.conf
share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build repos` wrote `repos/dist/repos-v<semver>.tar.xz`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the file holds

The binary inside the file is what a host will run, so it is asked the two things a host asks it.

Command:

```
$ tar -xJf repos/dist/repos-v<semver>.tar.xz -O bin/repos > /tmp/repos && chmod +x /tmp/repos
$ /tmp/repos --version
$ /tmp/repos manifest
```

Output:

```
v<semver>
app = "repos"
description = "Git repositories for the suite's content"
default = false
mcp = true
secrets = []

[env]
READ_SLOTS = "8"
WRITE_SLOTS = "2"
QUEUE_LENGTH = "16"
QUEUE_SECONDS = "30"
OPERATION_SECONDS = "600"
PUSH_MAX_BYTES = "104857600"
REPO_MAX_BYTES = "1073741824"
MAINTENANCE_HOURS = "24"

[database]
engine = "sqlite"
path = "state/repos.db"

[resources]
memory_max = "256M"
go_memory_limit = "128M"
oom_policy = "continue"
```

Each command exits 0. The text is on stdout; stderr is empty. The version is the one in the file's name, and the manifest is byte for byte the file's `etc/manifest.toml`.

Preconditions:

- `devctl build repos` wrote `repos/dist/repos-v<semver>.tar.xz`.
- The developer's machine is `linux/amd64`, or can run such a binary.

Postconditions:

- Nothing in the checkout has changed.
