# Stories — bootstrap

Running mcp at all: help, version, the manifest, exit codes. mcp is an app of the platform, the suite's MCP gateway: one Go binary that offers MCP clients four tools over the suite's MCP services at `/mcp` (`S05`), and a connect page at `/` that tells a person how to point a client at it (`S03`). On a host it runs as `/opt/mcp/bin/mcp` with `/opt/mcp` as its working directory and its environment read from `/opt/mcp/etc/env`. With no command it serves (`S02`); the commands here are what the build asks of it.

## A developer asks which version they have

The version is a `var` in the source, never injected at build time, so a developer's build and a deployed binary report the same string. Its shape is `v<semver>`: a `v`, then a semantic version, prerelease and build metadata included. Its value is data and is not fixed here. It is the same version the connect page's footer shows (`S03`) and mcp's `service.started` event carries in the trail (`S02`).

Command:

```
$ mcp --version
```

Output:

```
v<semver>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/mcp` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed `etc/manifest.toml` is a copy kept so the checkout can be read without a build; the two are byte-identical, and `devctl build` refuses an app where they differ. mcp declares its name; its description, the one line that says what mcp is for, which the host publishes in its services file; that it is not the host's default app; that it is not one of the suite's MCP services (`mcp = false`): though it serves an MCP endpoint at `/mcp`, its own tools are not in the gateway's catalogue, so the gateway never lists itself (`S06`); and no secrets, in that order. It declares no port: mcp serves on the socket the host passes it (`S02`), and a manifest carrying `port` is refused by `devctl build` and by opsctl.

Command:

```
$ mcp manifest
```

Output:

```
app = "mcp"
description = "Connect AI assistants to your services"
default = false
mcp = false
secrets = []
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/mcp` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what mcp can do

Command:

```
$ mcp --help
```

Output:

```
Usage: mcp [command]

Serve the MCP gateway at /mcp, and its connect page at /, on the socket
systemd passes in. With no command, serve.

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

- `bin/mcp` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ mcp bogus
```

Output:

```
mcp: unknown command 'bogus'

see 'mcp --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. An unknown option, `--bogus` say, fails the same way with `mcp: unknown option '--bogus'`.

Preconditions:

- `bin/mcp` exists.

Postconditions:

- Nothing has changed.
