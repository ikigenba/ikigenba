> [!WARNING]
> This is unsupported AI slop.

# events

`events` is the Ikigenba suite's internal event bus, served at
`events.<host>`: services emit events to it and it delivers them to the
services that accept them. Agents read the catalog of events, search the
retained log, and watch, skip and resume subscribers through five MCP tools,
`catalog`, `search`, `subscribers`, `skip` and `resume`, reached through the
MCP gateway. Its home page lists each subscriber's status, cursor and lag.

## Installing it

`events` runs on an Ikigenba host beside the services that emit to it and
accept its events. From a checkout, build the release and deploy it to a space
with [`devctl`](../devctl):

```sh
devctl build <sha|tag>
devctl deploy <space> <sha|tag>
```

## Using it

```sh
events --help
```
