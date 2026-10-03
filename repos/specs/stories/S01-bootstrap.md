# Stories — bootstrap

Running repos at all: help, version, the manifest, exit codes. repos is an app of the platform, the suite's home for git repositories: one Go binary that serves each of a user's repositories over git's smart HTTP at `/<name>.git` (`S11`, `S12`), offers six MCP tools to create, list, show, rename, and delete them and to see the load on them at `/mcp` (`S05` to `S10`), keeps each one as a bare repository on disk that the suite's other apps on the same host read directly (`S15`), and serves a landing page at `/` that says what it is and how to clone (`S03`). On a host it runs as `/opt/repos/bin/repos` with `/opt/repos` as its working directory and its environment read from `/opt/repos/etc/env`; a developer runs the same binary from the checkout. With no command it serves (`S02`); the commands here are what the build asks of it. They serve nothing, run no git, and record no event: the trail is what repos records while it serves (`S02`).

## A developer asks which version they have

The version is a `var` in the source, never injected at build time, so a developer's build and a deployed binary report the same string. Its shape is `v<semver>`: a `v`, then a semantic version, prerelease and build metadata included. Its value is data and is not fixed here. It is the same version the landing page's footer and the about screen show (`S03`) and repos' own `service.started` event carries in the trail (`S02`).

Command:

```
$ repos --version
```

Output:

```
v<semver>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/repos` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed `etc/manifest.toml` is a copy kept so the checkout can be read without a build; the two are byte-identical, and `devctl build` refuses an app where they differ. repos declares its name; its description, the one line that says what repos is for, which the host publishes in its services file, the about screen shows (`S03`), and repos' MCP endpoint gives its clients as instructions (`S05`); that it is not the host's default app; that it is one of the suite's MCP services (`mcp = true`), so the gateway lists it and runs its tools (`S05`); no secrets; its eight settings, which the host writes into its `etc/env` and which it reads when it serves (`S02`): `READ_SLOTS` and `WRITE_SLOTS`, how many git reads and writes run at once, `QUEUE_LENGTH` and `QUEUE_SECONDS`, how many operations may wait for a slot and for how long (`S12`), `OPERATION_SECONDS`, the longest one git operation may run (`S12`), `PUSH_MAX_BYTES` and `REPO_MAX_BYTES`, the largest pack one push may send and the size at which a repository takes no more pushes (`S12`), and `MAINTENANCE_HOURS`, how often each repository is tidied (`S13`); its SQLite database, the catalog of repositories, which the host replicates like auth's; and how much of the host it may take, in opsctl's `[resources]` table, which opsctl writes into its service unit, so it bounds repos and every git repos runs together: `cpu_weight` 50 and `io_weight` 50, half the share of a service that declares none, so git work yields to the rest of the suite when the host is busy, and `memory_max` `1G`, a ceiling of 1 GiB. It declares no port: repos serves on the socket the host passes it (`S02`), and a manifest carrying `port` is refused by `devctl build` and by opsctl.

Command:

```
$ repos manifest
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
cpu_weight = 50
memory_max = "1G"
io_weight = 50
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/repos` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what repos can do

Command:

```
$ repos --help
```

Output:

```
Usage: repos [command]

Serve git repositories: smart HTTP at /<name>.git, MCP tools at /mcp, and a
landing page at /, on the socket systemd passes in. With no command, serve.

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

- `bin/repos` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ repos bogus
```

Output:

```
repos: unknown command 'bogus'

see 'repos --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. An unknown option, `--bogus` say, fails the same way with `repos: unknown option '--bogus'`.

Preconditions:

- `bin/repos` exists.

Postconditions:

- Nothing has changed.
