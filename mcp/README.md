> [!WARNING]
> This is unsupported AI slop.

# mcp

`mcp` is the Ikigenba suite's MCP gateway, served at `mcp.<host>`. It gives
an MCP client one endpoint, `/mcp`, through which it can discover and call the
tools of every MCP-enabled service on the host; `/mcp/a,b` limits it to the
named services. Its home page tells you how to connect a client.

## Installing it

`mcp` runs on an Ikigenba host. From a checkout at a release tag, build it
and deploy it to a space with [`devctl`](../devctl):

```sh
devctl build mcp
devctl deploy <space> mcp/dist/mcp-vX.Y.Z.tar.xz
```

## Using it

```sh
mcp --help
```
