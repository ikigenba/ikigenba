# Stories — bootstrap

Running sites at all: help, version, the manifest, exit codes. sites is an app of the platform, the suite's static site host: one Go binary that serves each site at `/<slug>/` on `sites.<space>` from a tree it unpacks out of one of repos' bare repositories (`S11`, `S12`), redirects the apex host, the root domain the space hangs from when it is routed to sites, to the apex site (`S13`), offers seven MCP tools to list, show, create, publish, update, and delete sites and to choose the apex site at `/mcp` (`S05` to `S10`, `S13`), and serves a landing page at `/` that lists the space's sites and says how to make one (`S03`). On a host it runs as `/opt/sites/bin/sites` with `/opt/sites` as its working directory and its environment read from `/opt/sites/etc/env`; a developer runs the same binary from the checkout. With no command it serves (`S02`); the commands here are what the build asks of it. They serve nothing, run no git, read no repository, and record no event: the trail is what sites records while it serves (`S02`).

## A developer asks which version they have

The version is a `var` in the source, never injected at build time, so a developer's build and a deployed binary report the same string. Its shape is `v<semver>`: a `v`, then a semantic version, prerelease and build metadata included. Its value is data and is not fixed here. It is the same version the landing page's footer and the about screen show (`S03`), the MCP endpoint gives as its `serverInfo` (`S05`), and sites' own `service.started` event carries in the trail (`S02`).

Command:

```
$ sites --version
```

Output:

```
v<semver>
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/sites` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer asks for the manifest

The manifest is a fact about the binary, so the binary emits it. The committed `etc/manifest.toml` is a copy kept so the checkout can be read without a build; the two are byte-identical, and `devctl build` refuses an app where they differ. sites declares its name; its description, the one line that says what sites is for, which the host publishes in its services file, the about screen shows (`S03`), and sites' MCP endpoint gives its clients as instructions (`S05`); that it is not the host's default app; that it is one of the suite's MCP services (`mcp = true`), so the gateway lists it and runs its tools (`S05`); that it serves guests (`guests = true`), visitors with no credential at all, so on a host with an authenticator nginx lets such a visitor through to sites' pages with no identity instead of sending them to sign in, while `/mcp` stays challenged (opsctl's `S5-nginx.md`, `S7-apps.md`): that is how a public site reaches anyone (`S11`), and sites itself sends a guest to sign in where it needs a user (`S03`, `S12`); no secrets; its three settings, which the host writes into its `etc/env` and which it reads when it serves (`S02`): `REPOS_DIR`, the directory holding repos' bare repositories, relative to sites' working directory, so `/opt/sites/../repos/state/repos` on a host (`S18`), `SITE_MAX_BYTES`, the largest a site's tree may be, 256 MiB (`S17`), and `OPERATION_SECONDS`, the longest one git run may take (`S17`); its SQLite database, the catalog of sites, which the host replicates like auth's; and how much of the host it may take, in opsctl's `[resources]` table, which opsctl writes into its service unit, so it bounds sites and every git sites runs together: `cpu_weight` 50 and `io_weight` 50, half the share of a service that declares none, so unpacking a site yields to the rest of the suite when the host is busy, and `memory_max` `1G`, a ceiling of 1 GiB. It declares no port: sites serves on the socket the host passes it (`S02`), and a manifest carrying `port` is refused by `devctl build` and by opsctl. It names no `cache/`: the unpacked trees there are disposable and rebuilt on demand (`S16`), so the host neither keeps nor replicates them.

Command:

```
$ sites manifest
```

Output:

```
app = "sites"
description = "Static sites from the suite's repositories"
default = false
mcp = true
guests = true
secrets = []

[env]
REPOS_DIR = "../repos/state/repos"
SITE_MAX_BYTES = "268435456"
OPERATION_SECONDS = "600"

[database]
engine = "sqlite"
path = "state/sites.db"

[resources]
cpu_weight = 50
memory_max = "1G"
io_weight = 50
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/sites` exists.
- `etc/manifest.toml` in the checkout holds exactly the text above.

Postconditions:

- Nothing has changed.

## A developer asks what sites can do

Command:

```
$ sites --help
```

Output:

```
Usage: sites [command]

Serve static sites from the suite's repositories at /<slug>/, MCP tools at
/mcp, and a landing page at /, on the socket systemd passes in. With no
command, serve.

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

- `bin/sites` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ sites bogus
```

Output:

```
sites: unknown command 'bogus'

see 'sites --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/sites` exists.

Postconditions:

- Nothing has changed.

## A developer gives an option sites does not have

An unknown option is a usage error like an unknown command, and is named back the same way.

Command:

```
$ sites --bogus
```

Output:

```
sites: unknown option '--bogus'

see 'sites --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/sites` exists.

Postconditions:

- Nothing has changed.
