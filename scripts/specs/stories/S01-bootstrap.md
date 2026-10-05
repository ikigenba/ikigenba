# Stories — bootstrap

Running scripts at all: help, version, the manifest, exit codes. scripts is an app of the platform, the suite's script runner: one Go binary that keeps a catalog of each user's scripts, each naming one of their repositories in repos and the ref it runs, runs a script's `main.py` from that ref's commit unpacked into a folder of its own and keeps the run's input, output, files and outcome (`S08`, `S15`), offers nine MCP tools to list, show, create, update, delete and run scripts and to list, read and cancel their runs at `/mcp` (`S05` to `S11`), and serves a catalog of the user's scripts at `/` (`S03`) with a page for each script (`S12`) and for each run (`S13`). On a host it runs as `/opt/scripts/bin/scripts` with `/opt/scripts` as its working directory and its environment read from `/opt/scripts/etc/env`; a developer runs the same binary from the checkout. With no command it serves (`S02`); the commands here are what the build asks of it. They serve nothing, run no git and no script, read no repository, and record no event: the trail is what scripts records while it serves (`S02`).

## A developer asks which version they have

The version is a `var` in the source, never injected at build time, so a developer's build and a deployed binary report the same string. Its shape is `v<semver>`: a `v`, then a semantic version, prerelease and build metadata included. Its value is data and is not fixed here. It is the same version the landing page's footer and the about screen show (`S03`), the MCP endpoint gives as its `serverInfo` (`S05`), and scripts' own `service.started` event carries in the trail (`S02`).

Command:

```
$ scripts --version
```

Output:

```
v<semver>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/scripts` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed `etc/manifest.toml` is a copy kept so the checkout can be read without a build; the two are byte-identical, and `devctl build` refuses an app where they differ. scripts declares its name; its description, the one line that says what scripts is for, which the host publishes in its services file, the about screen shows (`S03`), and scripts' MCP endpoint gives its clients as instructions (`S05`); that it is not the host's default app; that it is one of the suite's MCP services (`mcp = true`), so the gateway lists it and runs its tools (`S05`); no secrets; its seven settings, which the host writes into its `etc/env` and which it reads when it serves (`S02`): `REPOS_DIR`, the directory holding repos' bare repositories, relative to scripts' working directory, so `/opt/scripts/../repos/state/repos` on a host (`S20`), `TREE_MAX_BYTES`, the largest a run's unpacked tree may be, 256 MiB (`S17`), `OUTPUT_MAX_BYTES`, how much of a run's standard output, and separately of its standard error, is kept, 1 MiB (`S17`), `OPERATION_SECONDS`, the longest one git run may take (`S17`), `SCRIPT_SECONDS`, the longest a script may run (`S17`), and `RUN_KEEP_DAYS` and `RUN_KEEP_COUNT`, how many days a run is kept and how many of each script's newest runs are kept whatever their age (`S19`); its SQLite database, the catalog of scripts and the record of every run, which the host replicates like auth's; and how much of the host it may take, in opsctl's `[resources]` table, which opsctl writes into its service unit, so it bounds scripts, every git scripts runs and every script it runs together: `cpu_weight` 50 and `io_weight` 50, half the share of a service that declares none, so unpacking and running scripts yields to the rest of the suite when the host is busy, and `memory_max` `1G`, a ceiling of 1 GiB. It does not declare `guests`: every page and `/mcp` is for a signed-in user, so on a host with an authenticator nginx sends a visitor with no credential to sign in before they reach a page (`S03`) and challenges one at `/mcp` (`S05`). It declares no port: scripts serves on the socket the host passes it (`S02`), and a manifest carrying `port` is refused by `devctl build` and by opsctl. It names no `cache/`: a run's folder is the product, not a cache, and lives under `state/runs/` (`S20`).

Command:

```
$ scripts manifest
```

Output:

```
app = "scripts"
description = "Python scripts run from the suite's repositories"
default = false
mcp = true
secrets = []

[env]
REPOS_DIR = "../repos/state/repos"
TREE_MAX_BYTES = "268435456"
OUTPUT_MAX_BYTES = "1048576"
OPERATION_SECONDS = "600"
SCRIPT_SECONDS = "600"
RUN_KEEP_DAYS = "15"
RUN_KEEP_COUNT = "10"

[database]
engine = "sqlite"
path = "state/scripts.db"

[resources]
cpu_weight = 50
memory_max = "1G"
io_weight = 50
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/scripts` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what scripts can do

Command:

```
$ scripts --help
```

Output:

```
Usage: scripts [command]

Run Python scripts from the suite's repositories, with MCP tools at /mcp
and pages for scripts and their runs at /, on the socket systemd passes in.
With no command, serve.

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

- `bin/scripts` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ scripts bogus
```

Output:

```
scripts: unknown command 'bogus'

see 'scripts --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists.

Postconditions:

- Nothing has changed.

## A developer gives an option scripts does not have

An unknown option is a usage error like an unknown command, and is named back the same way.

Command:

```
$ scripts --bogus
```

Output:

```
scripts: unknown option '--bogus'

see 'scripts --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/scripts` exists.

Postconditions:

- Nothing has changed.
