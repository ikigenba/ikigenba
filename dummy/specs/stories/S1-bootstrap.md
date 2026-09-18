# Stories — bootstrap

Running dummy at all: help, version, the manifest, exit codes. dummy is an
app of the platform: one Go binary that serves one page. On a host it runs as
`/opt/dummy/bin/dummy` with `/opt/dummy` as its working directory and its
environment read from `/opt/dummy/etc/env`; a developer runs the same binary
from the checkout. With no command it serves (`S2-serve.md`); the commands
here are what the build asks of it.

## A developer asks which version they have

The version is a `var` in the source, never injected at build time, so a
developer's build and a deployed binary report the same string. Its shape is
`v<semver>`: a `v`, then a semantic version, prerelease and build metadata
included. Its value is data and is not fixed here.

Command:

```
$ dummy --version
```

Output:

```
v<semver>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/dummy` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The
committed `etc/manifest.toml` is a copy kept so the checkout can be read
without a build; the two are byte-identical, and `devctl build` refuses an
app where they differ. dummy declares its name, its port, that it is not the
host's default app, and no secrets.

Command:

```
$ dummy manifest
```

Output:

```
app = "dummy"
port = 3000
default = false
secrets = []
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/dummy` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what dummy can do

Command:

```
$ dummy --help
```

Output:

```
Usage: dummy [command]

Serve the Dummy page at 127.0.0.1:$PORT. With no command, serve.

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

- `bin/dummy` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ dummy bogus
```

Output:

```
dummy: unknown command 'bogus'

see 'dummy --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. An unknown option, `--bogus`
say, fails the same way with `dummy: unknown option '--bogus'`.

Preconditions:

- `bin/dummy` exists.

Postconditions:

- Nothing has changed.
