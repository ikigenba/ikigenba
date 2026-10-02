> [!WARNING]
> This is unsupported AI slop.

# telemetry

`telemetry` is the Ikigenba suite's trail of events, served at
`telemetry.<host>`. Every service on the host records what it does here: each
request it serves, each call it makes to a sibling, each MCP tool it runs, and
the events of its own domain. Agents search the trail through four MCP tools,
`catalog`, `search`, `count` and `trace`, reached through the MCP gateway. Its
home page says what it is.

## Installing it

`telemetry` runs on an Ikigenba host. From a checkout at a release tag, build
it and deploy it to a space with [`devctl`](../devctl):

```sh
devctl build telemetry
devctl deploy <space> telemetry/dist/telemetry-vX.Y.Z.tar.xz
```

## Using it

```sh
telemetry --help
```
