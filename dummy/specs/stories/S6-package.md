# Stories — package

The file that carries dummy to a space. `devctl build dummy` writes it from
the commit checked out, in a tree with no uncommitted changes; it reads no
tag. The file is `dummy/dist/dummy-<sha>.tar.xz`, where `<sha>` is that
commit's full 40-character hexadecimal sha, and `opsctl install` unpacks it
into `/opt/dummy/`. Its contents are the whole of what dummy ships: the static
`linux/amd64` binary, the manifest, and `share/icon.svg`, nothing else.
`share/icon.svg` is dummy's icon, an SVG image a human draws; its presence is
what lists dummy in the platform's service launcher on a space
(`S7-on-a-space.md`), and no story fixes its content beyond its being an SVG.
dummy keeps nothing under `etc/` but the manifest and nothing under `share/`
but the icon, so no other member exists. The files that give the panel its
style, its launcher, its button feedback, and its icon — the stylesheet, the
fonts, their licences, the launcher's script, the button feedback script, and
the favicon — are
inside the binary (`S8-assets.md`), so no `assets/`
directory and no font file ships beside it.
The database is not in the file: dummy creates `state/dummy.db` under its
working directory on first start (`S2-serve.md`), and the manifest's
`[database]` table declares it so that the host replicates it.
The commit is in the file's name only. No version and no commit is recorded
in the binary or in any member, or in a member's path: dummy learns which code
it is running from its environment when it runs (`S1`).

## A developer lists what the file holds

Command:

```
$ tar -tJf dummy/dist/dummy-<sha>.tar.xz | sort
```

Output:

```
bin/dummy
etc/manifest.toml
share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build dummy`, run in a clean tree at the commit `<sha>`, wrote
  `dummy/dist/dummy-<sha>.tar.xz`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the file holds

The binary inside the file is what a host will run, so it is asked the two
things a host asks it. Asked with neither `IKIGENBA_COMMIT` nor
`IKIGENBA_RELEASE` set, it prints an empty line for its version: the binary
carries no identity of its own, and the commit in the file's name is nowhere
inside it.

Command:

```
$ tar -xJf dummy/dist/dummy-<sha>.tar.xz -O bin/dummy > /tmp/dummy && chmod +x /tmp/dummy
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
file's `etc/manifest.toml`.

Preconditions:

- `devctl build dummy`, run in a clean tree at the commit `<sha>`, wrote
  `dummy/dist/dummy-<sha>.tar.xz`.
- The developer's machine is `linux/amd64`, or can run such a binary.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty in the
  developer's environment.

Postconditions:

- Nothing in the checkout has changed.
