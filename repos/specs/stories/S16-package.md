# Stories — package

The file that carries repos to a space is the suite's release. `devctl build <sha|tag>` writes it from the commit it names, a tag being read only to find that commit, as `dist/<sha>.tar.xz` at the checkout root, where `<sha>` is that commit's full 40-character hexadecimal sha; repos' part of it is the directory `<sha>/repos/`, which a host unpacks into `/opt/ikigenba/releases/<sha>/repos/` and runs from `/opt/ikigenba/current/repos/` once that release is active. That directory's contents are the whole of what repos ships: the static `linux/amd64` binary, the manifest, the nginx fragment, and `share/icon.svg`, nothing else. `etc/nginx.conf` is repos' own nginx configuration, which the host includes in the server that answers at repos' public name, as it does for any app that ships one; its effects are that nginx sets no limit on the size of a request body, so a push is refused only by repos' own `PUSH_MAX_BYTES` (`S12-limits.md`), that nginx passes a request body to repos as it arrives rather than spooling it first, so a push streams through, that nginx keeps its connection to repos open while repos works, giving up only after an hour in which nothing has passed either way, longer than a git operation waits for its slot and runs under the default `QUEUE_SECONDS` and `OPERATION_SECONDS`, and that nginx still buffers repos' answers, so a slow client does not hold a git process open, and that nginx answers `/events` and `/declarations` 404 to the public for every method, so they stay on repos' socket (`S17-on-a-space.md`); no story fixes its text beyond those effects. The challenge git needs before it sends its credential is not the fragment's: the host's nginx gives it on every app's server (opsctl's `S5-nginx.md`). `share/icon.svg` is repos' icon, an SVG image a human draws; its presence is what lists repos in the platform's service launcher on a space (`S17-on-a-space.md`), and no story fixes its content beyond its being an SVG. repos keeps nothing under `etc/` but the manifest and the fragment and nothing under `share/` but the icon, so no other member exists under `<sha>/repos/`. The files that give the pages their style, their launcher, their button feedback, and their favicon — the stylesheet, the fonts, their licences, the launcher's script, the button feedback script, and the favicon — are inside the binary (`S04-assets.md`), so no `assets/` directory and no font file ships beside it. Neither the database nor any repository is in the file: repos creates `state/repos.db` and `state/repos/` under its working directory, `/var/opt/ikigenba/repos` on a host, on first start (`S02-serve.md`), and the manifest's `[database]` table declares the database so that the host keeps it across releases. The manifest's `[resources]` table is read by the host, which bounds repos and every `git` it runs with it (opsctl's `S7-apps.md`). The commit is in the file's name and its top-level directory only. No version and no commit is recorded in the binary or in any of repos' members, or in a member's path below `<sha>/repos/`: repos learns which code it is running from its environment when it runs (`S01-bootstrap.md`).

## A developer lists what the file holds

Command:

```
$ tar -tJf dist/<sha>.tar.xz | grep '^<sha>/repos/' | grep -v '/$' | sort
```

Output:

```
<sha>/repos/bin/repos
<sha>/repos/etc/manifest.toml
<sha>/repos/etc/nginx.conf
<sha>/repos/share/icon.svg
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
$ tar -xJf dist/<sha>.tar.xz -O <sha>/repos/bin/repos > /tmp/repos && chmod +x /tmp/repos
$ /tmp/repos --version
$ /tmp/repos manifest
```

Output:

```

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

Each command exits 0. The text is on stdout; stderr is empty. `--version` prints one empty line, a single newline, and the manifest is byte for byte the file's `<sha>/repos/etc/manifest.toml`.

Preconditions:

- `devctl build <sha>` wrote `dist/<sha>.tar.xz`, the release of the commit `<sha>`.
- The developer's machine is `linux/amd64`, or can run such a binary.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty in the developer's environment.

Postconditions:

- Nothing in the checkout has changed.
