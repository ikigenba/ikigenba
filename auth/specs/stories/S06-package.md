# Stories — package

The file that carries auth to a space. `devctl build auth` writes it from
the commit checked out, in a tree with no uncommitted changes; it reads no
tag. The file is `auth/dist/auth-<sha>.tar.xz`, where `<sha>` is that
commit's full 40-character hexadecimal sha, and `opsctl install` unpacks it
into `/opt/auth/`. Its contents are the whole of what auth ships: the static
`linux/amd64` binary, the manifest, and `share/icon.svg`, nothing else.
`share/icon.svg` is auth's icon, an SVG image a human authors from the Tabler
outline icon `user-circle`; its presence is what lists auth in the platform's
service launcher on a space (`S07-on-a-space.md`), and no story fixes its
content further than that. auth carries its pages, the platform's shared
files, and its database schema inside the binary — its HTML is embedded, the
stylesheet, fonts, licences, launcher script, button feedback script, and
favicon it serves at `/_appkit/` need no file beside it (`S08-assets.md`), and
the migrations it applies to its database are in the binary too —
so it keeps nothing under `etc/` but the manifest and nothing under `share/`
but the icon, and no other member exists. No `assets/` directory and no font
file ships beside the binary. The database is not in the file: auth creates
`state/auth.db` under its working directory on first start (`S02-serve.md`),
and the manifest's `[database]` table declares it so that the host replicates
it. The commit is in the file's name only. No version and no commit is
recorded in the binary or in any member, or in a member's path: auth learns
which code it is running from its environment when it runs
(`S01-bootstrap.md`).

## A developer lists what the file holds

Command:

```
$ tar -tJf auth/dist/auth-<sha>.tar.xz | sort
```

Output:

```
bin/auth
etc/manifest.toml
share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build auth`, run in a clean tree at the commit `<sha>`, wrote
  `auth/dist/auth-<sha>.tar.xz`.

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
$ tar -xJf auth/dist/auth-<sha>.tar.xz -O bin/auth > /tmp/auth && chmod +x /tmp/auth
$ /tmp/auth --version
$ /tmp/auth manifest
```

Output:

```

app = "auth"
default = false
secrets = ["GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"]

[env]
WORKSPACE_DOMAIN = "michaelgreenly.dev"

[database]
engine = "sqlite"
path = "state/auth.db"

[resources]
slice = "core"
memory_max = "128M"
```

Each command exits 0. The text is on stdout; stderr is empty. `--version`
prints one empty line, a single newline, and the manifest is byte for byte the
file's `etc/manifest.toml`.

Preconditions:

- `devctl build auth`, run in a clean tree at the commit `<sha>`, wrote
  `auth/dist/auth-<sha>.tar.xz`.
- The developer's machine is `linux/amd64`, or can run such a binary.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty in the
  developer's environment.

Postconditions:

- Nothing in the checkout has changed.
