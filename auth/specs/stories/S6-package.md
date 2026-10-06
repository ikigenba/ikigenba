# Stories — package

The file that carries auth to a space. `devctl build auth` writes it from a
commit that an `auth/v<semver>` tag points at, and `opsctl install` unpacks it
into `/opt/auth/`. Its contents are the whole of what auth ships: the static
`linux/amd64` binary, the manifest, and `share/icon.svg`, nothing else.
`share/icon.svg` is auth's icon, an SVG image a human authors from the Tabler
outline icon `fingerprint`; its presence is what lists auth in the platform's
service launcher on a space (`S7-on-a-space.md`), and no story fixes its
content further than that. auth carries its pages, the platform's shared
files, and its database schema inside the binary — its HTML is embedded, the
stylesheet, fonts, licences, launcher script, and button feedback script it
serves at `/_appkit/` need no file beside it (`S8-assets.md`), and the
migrations it applies to its database are in the binary too —
so it keeps nothing under `etc/` but the manifest and nothing under `share/`
but the icon, and no other member exists. No `assets/` directory and no font
file ships beside the binary. The version is in the file's name and in the
binary, never in a member's path. The database is not in the file: auth
creates `state/auth.db` under its working directory on first start
(`S2-serve.md`), and the manifest's `[database]` table declares it so that the
host replicates it.

## A developer lists what the file holds

Command:

```
$ tar -tJf auth/dist/auth-v<semver>.tar.xz | sort
```

Output:

```
bin/auth
etc/manifest.toml
share/icon.svg
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `devctl build auth` wrote `auth/dist/auth-v<semver>.tar.xz`.

Postconditions:

- Nothing has changed.

## A developer checks the binary the file holds

The binary inside the file is what a host will run, so it is asked the two
things a host asks it.

Command:

```
$ tar -xJf auth/dist/auth-v<semver>.tar.xz -O bin/auth > /tmp/auth && chmod +x /tmp/auth
$ /tmp/auth --version
$ /tmp/auth manifest
```

Output:

```
v<semver>
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

Each command exits 0. The text is on stdout; stderr is empty. The version is
the one in the file's name, and the manifest is byte for byte the file's
`etc/manifest.toml`.

Preconditions:

- `devctl build auth` wrote `auth/dist/auth-v<semver>.tar.xz`.
- The developer's machine is `linux/amd64`, or can run such a binary.

Postconditions:

- Nothing in the checkout has changed.
