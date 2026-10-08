# Stories — package

What auth ships in the suite release. auth has no file of its own: it ships
in the one release of the whole suite, which `devctl build <sha|tag>` writes as
`dist/<sha>.tar.xz` at the checkout root, `<sha>` being the commit's full
40-character hexadecimal sha, and `devctl deploy <space> <sha|tag>` unpacks on
a host into `/opt/ikigenba/releases/<sha>/`. auth's tree in it is
`<sha>/auth/`, which the host runs as `/opt/ikigenba/current/auth/` once that
release is active. That tree is the whole of what auth ships: the static
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
it. The commit is in the release's name only. No version and no commit is
recorded in the binary or in any member, or in a member's path: auth learns
which code it is running from its environment when it runs
(`S01-bootstrap.md`).

## A developer lists what auth's tree holds

Command:

```
$ tar -tJf dist/<sha>.tar.xz <sha>/auth | grep -v '/$' | sort
```

Output:

```
<sha>/auth/bin/auth
<sha>/auth/etc/manifest.toml
<sha>/auth/share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build <sha>` wrote `dist/<sha>.tar.xz`, the suite release at the
  commit `<sha>`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the file holds

The binary inside the release is what a host will run, so it is asked the two
things a host asks it. Asked with neither `IKIGENBA_COMMIT` nor
`IKIGENBA_RELEASE` set, it prints an empty line for its version: the binary
carries no identity of its own, and the commit in the release's name is
nowhere inside it.

Command:

```
$ tar -xJf dist/<sha>.tar.xz -O <sha>/auth/bin/auth > /tmp/auth && chmod +x /tmp/auth
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
release's `<sha>/auth/etc/manifest.toml`.

Preconditions:

- `devctl build <sha>` wrote `dist/<sha>.tar.xz`, the suite release at the
  commit `<sha>`.
- The developer's machine is `linux/amd64`, or can run such a binary.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty in the
  developer's environment.

Postconditions:

- Nothing in the checkout has changed.
