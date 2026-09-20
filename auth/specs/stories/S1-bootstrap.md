# Stories — bootstrap

Running auth at all: help, version, the manifest, exit codes. auth is an app
of the platform: one Go binary that serves the auth service. On a host it runs
as `/opt/auth/bin/auth` with `/opt/auth` as its working directory and its
environment read from `/opt/auth/etc/env`; a developer runs the same binary
from the checkout. With no command it serves (`S2-serve.md`); the commands
here are what the build asks of it.

## A developer asks which version they have

The version is a `var` in the source, never injected at build time, so a
developer's build and a deployed binary report the same string. Its shape is
`v<semver>`: a `v`, then a semantic version, prerelease and build metadata
included. Its value is data and is not fixed here.

Command:

```
$ auth --version
```

Output:

```
v<semver>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/auth` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed
`etc/manifest.toml` is a copy kept so the checkout can be read without a build;
the two are byte-identical, and `devctl build` refuses an app where they
differ. auth declares its name, its port, that it is not the host's default
app, the secrets it needs, its Workspace domain, and its SQLite database.

Command:

```
$ auth manifest
```

Output:

```
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

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/auth` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what auth can do

Command:

```
$ auth --help
```

Output:

```
Usage: auth [command]

Serve the auth service at 127.0.0.1:$PORT. With no command, serve.

Commands:
  manifest   print the app manifest

Options:
  --help      print this help
  --version   print the version

Exit codes:
  0  success
  1  the server failed
  2  usage error
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/auth` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ auth bogus
```

Output:

```
auth: unknown command 'bogus'

see 'auth --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. An unknown option, `--bogus`
say, fails the same way with `auth: unknown option '--bogus'`.

Preconditions:

- `bin/auth` exists.

Postconditions:

- Nothing has changed.
