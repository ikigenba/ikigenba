> [!WARNING]
> This is unsupported AI slop.

# dummy

`dummy` is a service that exercises the whole Ikigenba app path with a small
control panel. It serves a server-rendered panel of widgets behind the host's
nginx, as a reference for how a platform app is built and deployed. Widgets
live in memory and are lost when it exits.

## Installing it

`dummy` runs on an Ikigenba host. From a checkout at a release tag, build it
and deploy it to a space with [`devctl`](../devctl):

```sh
devctl build dummy
devctl deploy <space> dummy/dist/dummy-vX.Y.Z.tar.xz
```

## Using it

```sh
dummy --help
```
