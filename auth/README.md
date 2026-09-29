> [!WARNING]
> This is unsupported AI slop.

# auth

`auth` is a service that authenticates users for every app on an Ikigenba
host. It serves the auth service behind the host's nginx, which also sends it
the identity subrequest for every other app.

## Installing it

`auth` runs on an Ikigenba host. From a checkout at a release tag, build it
and deploy it to a space with [`devctl`](../devctl):

```sh
devctl build auth
devctl deploy <space> auth/dist/auth-vX.Y.Z.tar.xz
```

## Using it

```sh
auth --help
```
