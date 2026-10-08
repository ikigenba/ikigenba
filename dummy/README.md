> [!WARNING]
> This is unsupported AI slop.

# dummy

`dummy` is a service that exercises the whole Ikigenba app path with a small
control panel. It serves a server-rendered panel of widgets behind the host's
nginx, as a reference for how a platform app is built and deployed. Widgets
live in memory and are lost when it exits.

## Installing it

`dummy` runs on an Ikigenba host. From a checkout, build the release and
deploy it to a space with [`devctl`](../devctl):

```sh
devctl build <sha|tag>
devctl deploy <space> <sha|tag>
```

## Using it

```sh
dummy --help
```
