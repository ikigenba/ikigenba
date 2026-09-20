# Stories — package

The file that carries auth to a space. `devctl build auth` writes it from a
commit that an `auth/v<semver>` tag points at, and `opsctl install` unpacks it
into `/opt/auth/`. Its contents are the whole of what auth ships: the static
`linux/amd64` binary and the manifest, nothing else. auth carries its pages
and its database schema inside the binary — its HTML, JavaScript, and CSS are
embedded and it creates its schema on first start — so it keeps nothing under
`share/` and nothing under `etc/` but the manifest, and no other member
exists. The version is in the file's name and in the binary, never in a
member's path.

## A developer lists what the file holds

Command:

```
$ tar -tJf auth/dist/auth-v<semver>.tar.xz | sort
```

Output:

```
bin/auth
etc/manifest.toml
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
port = 3001
default = false
secrets = ["GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"]

[env]
WORKSPACE_DOMAIN = "michaelgreenly.dev"

[database]
engine = "sqlite"
path = "state/auth.db"
```

Each command exits 0. The text is on stdout; stderr is empty. The version is
the one in the file's name, and the manifest is byte for byte the file's
`etc/manifest.toml`.

Preconditions:

- `devctl build auth` wrote `auth/dist/auth-v<semver>.tar.xz`.
- The developer's machine is `linux/amd64`, or can run such a binary.

Postconditions:

- Nothing in the checkout has changed.
