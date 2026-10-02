# Stories — bootstrap

Running telemetry at all: help, version, the manifest, exit codes. telemetry is an app of the platform, the suite's trail of events: one Go binary that takes the events every service on the host posts to its socket at `/ingest` (`S06`), keeps them in its own SQLite database for a retention window (`S07`), offers four read-only MCP tools over them at `/mcp` (`S05`, `S08` to `S11`), and serves a landing page at `/` that says what it is (`S03`). On a host it runs as `/opt/telemetry/bin/telemetry` with `/opt/telemetry` as its working directory and its environment read from `/opt/telemetry/etc/env`; a developer runs the same binary from the checkout. With no command it serves (`S02`); the commands here are what the build asks of it. They serve nothing and record no event: the trail is what telemetry records while it serves (`S02`, `S12`).

## A developer asks which version they have

The version is a `var` in the source, never injected at build time, so a developer's build and a deployed binary report the same string. Its shape is `v<semver>`: a `v`, then a semantic version, prerelease and build metadata included. Its value is data and is not fixed here. It is the same version the landing page's footer and the about screen show (`S03`) and telemetry's own `service.started` event carries in the trail (`S02`).

Command:

```
$ telemetry --version
```

Output:

```
v<semver>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/telemetry` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed `etc/manifest.toml` is a copy kept so the checkout can be read without a build; the two are byte-identical, and `devctl build` refuses an app where they differ. telemetry declares its name; its description, the one line that says what telemetry is for, which the host publishes in its services file and the about screen shows (`S03`); that it is not the host's default app; that it is one of the suite's MCP services (`mcp = true`), so the gateway lists it and runs its tools (`S05`); no secrets; its one setting, `RETENTION_DAYS`, the retention window in days, which the host writes into its `etc/env` and which it reads when it serves (`S02`, `S07`); and its SQLite database, which the host replicates like auth's. It declares no port: telemetry serves on the socket the host passes it (`S02`), and a manifest carrying `port` is refused by `devctl build` and by opsctl.

Command:

```
$ telemetry manifest
```

Output:

```
app = "telemetry"
description = "The suite's trail of events"
default = false
mcp = true
secrets = []

[env]
RETENTION_DAYS = "15"

[database]
engine = "sqlite"
path = "state/telemetry.db"
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/telemetry` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what telemetry can do

Command:

```
$ telemetry --help
```

Output:

```
Usage: telemetry [command]

Serve the suite's trail of events: ingest at /ingest, MCP tools at /mcp, and
a landing page at /, on the socket systemd passes in. With no command, serve.

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

- `bin/telemetry` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ telemetry bogus
```

Output:

```
telemetry: unknown command 'bogus'

see 'telemetry --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. An unknown option, `--bogus` say, fails the same way with `telemetry: unknown option '--bogus'`.

Preconditions:

- `bin/telemetry` exists.

Postconditions:

- Nothing has changed.
