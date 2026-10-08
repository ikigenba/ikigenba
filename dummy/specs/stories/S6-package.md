# Stories — package

What dummy ships in the suite release. dummy has no file of its own: it ships
in the one release of the whole suite, which `devctl build <sha|tag>` writes as
`dist/<sha>.tar.xz` at the checkout root, `<sha>` being the commit's full
40-character hexadecimal sha, and `devctl deploy <space> <sha|tag>` unpacks on
a host into `/opt/ikigenba/releases/<sha>/`. dummy's tree in it is
`<sha>/dummy/`, which the host runs as `/opt/ikigenba/current/dummy/` once
that release is active. That tree is the whole of what dummy ships: the static
`linux/amd64` binary, the manifest, and `share/icon.svg`, nothing else.
`share/icon.svg` is dummy's icon, an SVG image a human draws; its presence is
what lists dummy in the platform's service launcher on a space
(`S7-on-a-space.md`), and no story fixes its content beyond its being an SVG.
dummy keeps nothing under `etc/` but the manifest and nothing under `share/`
but the icon, so no other member exists. The files that give the panel its
style, its launcher, its button feedback, and its icon — the stylesheet, the
fonts, their licences, the launcher's script, the button feedback script, and
the favicon — are inside the binary (`S8-assets.md`), so no `assets/`
directory and no font file ships beside it. The database is not in the release:
dummy creates `state/dummy.db` under its working directory on first start
(`S2-serve.md`), and the manifest's `[database]` table declares it so that the
host replicates it. The commit is in the release's name only. No version and no
commit is recorded in the binary or in any member, or in a member's path:
dummy learns which code it is running from its environment when it runs
(`S1`).

## A developer lists what dummy's tree holds

Command:

```
$ tar -tJf dist/<sha>.tar.xz <sha>/dummy | grep -v '/$' | sort
```

Output:

```
<sha>/dummy/bin/dummy
<sha>/dummy/etc/manifest.toml
<sha>/dummy/share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build <sha>` wrote `dist/<sha>.tar.xz`, the suite release at the
  commit `<sha>`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the release holds

The binary inside the release is what a host will run, so it is asked the two
things a host asks it. Asked with neither `IKIGENBA_COMMIT` nor
`IKIGENBA_RELEASE` set, it prints an empty line for its version: the binary
carries no identity of its own, and the commit in the release's name is
nowhere inside it.

Command:

```
$ tar -xJf dist/<sha>.tar.xz -O <sha>/dummy/bin/dummy > /tmp/dummy && chmod +x /tmp/dummy
$ /tmp/dummy --version
$ /tmp/dummy manifest
```

Output:

```

app = "dummy"
description = "Demo widgets to list and create"
default = false
mcp = true
secrets = []

[database]
engine = "sqlite"
path = "state/dummy.db"

[resources]
memory_max = "64M"
```

Each command exits 0. The text is on stdout; stderr is empty. `--version`
prints one empty line, a single newline, and the manifest is byte for byte the
release's `<sha>/dummy/etc/manifest.toml`.

Preconditions:

- `devctl build <sha>` wrote `dist/<sha>.tar.xz`, the suite release at the
  commit `<sha>`.
- The developer's machine is `linux/amd64`, or can run such a binary.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty in the
  developer's environment.

Postconditions:

- Nothing in the checkout has changed.
