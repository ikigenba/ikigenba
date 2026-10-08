# Stories — bootstrap

Running mcp at all: help, version, the manifest, exit codes. mcp is an app of the platform, the suite's MCP gateway: one Go binary that offers MCP clients four tools over the suite's MCP services at `/mcp` (`S05`), and a connect page at `/` that tells a person how to point a client at it (`S03`). On a host it runs as `/opt/mcp/bin/mcp` with `/opt/mcp` as its working directory and its environment read from `/opt/mcp/etc/env`. With no command it serves (`S02`); the commands here are what the build asks of it.

## A developer asks which version they have

mcp carries no version of its own: nothing in its source or its build names one. The environment tells it which code it is running, through two variables: `IKIGENBA_COMMIT`, the commit it was built from, and `IKIGENBA_RELEASE`, the label of the release, when there is one. From them mcp builds its display string, `<display>`, which every later story uses with this meaning. With both set it is the label, one space, and the short commit in parentheses, `<label> (<short sha>)`; with only the commit it is the short commit alone; with only the label it is the label alone. The short commit is the first seven characters of `IKIGENBA_COMMIT`, or all of it when it is shorter; a value ending in `-dirty`, as a developer's sandbox marks a modified tree, is shortened without the suffix and keeps it after, as in `<short sha>-dirty`. Nothing else about either value is checked or changed. mcp reads the two variables each time it is run with `--version`; an mcp that serves reads them once, when it starts (`S02`), and shows the same string in the connect page's footer (`S03`), in its `service.started` event in the trail (`S02`), in its MCP `serverInfo` (`S05`), and in the `clientInfo` it names itself with to a backend (`S08`).

Command:

```
$ mcp --version
```

Output:

```
<display>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/mcp` exists, built from the checkout with `make`.
- `IKIGENBA_COMMIT` holds a commit, and `IKIGENBA_RELEASE` holds a label or is unset; `<display>` is the string they make.

Postconditions:

- Nothing has changed.

## A developer asks which version they have with no identity set

With neither variable set, or both empty, mcp has no identity to show, and `<display>` is the empty string. It still answers, with an empty line, and still succeeds: a missing identity is not a failure.

Command:

```
$ mcp --version
```

Output:

```

```

Exits 0. stdout holds one empty line, a single newline and nothing else; stderr is empty.

Preconditions:

- `bin/mcp` exists.
- `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` are both unset or empty.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed `etc/manifest.toml` is a copy kept so the checkout can be read without a build; the two are byte-identical, and `devctl build` refuses an app where they differ. mcp declares its name; its description, the one line that says what mcp is for, which the host publishes in its services file; that it is not the host's default app; that it is not one of the suite's MCP services (`mcp = false`): though it serves an MCP endpoint at `/mcp`, its own tools are not in the gateway's catalogue, so the gateway never lists itself (`S06`); that it serves guests (`guests = true`), so the host's nginx lets a visitor with no credential reach its paths outside `/mcp`, where the protected-resource metadata is served to anyone (`S12`) and a guest asking for the connect page is sent to sign in (`S03`); and no secrets, in that order. Its `[resources]` table caps its memory at 128M. It declares no port: mcp serves on the socket the host passes it (`S02`), and a manifest carrying `port` is refused by `devctl build` and by opsctl.

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
guests = true
secrets = []

[resources]
memory_max = "128M"
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
