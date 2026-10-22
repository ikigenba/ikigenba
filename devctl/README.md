> [!WARNING]
> This is unsupported AI slop.

# devctl

`devctl` is a CLI that manages the Ikigenba platform from a developer's
machine. It creates and manages spaces (each a complete deployment on one
Linux host) and builds and deploys apps onto them, talking to AWS and, over
ssh, to the hosts. It never runs as root. [`opsctl`](../opsctl) is its
host-side counterpart.

## Installing it

```sh
curl -fsSL https://raw.githubusercontent.com/ikigenba/ikigenba/main/devctl/install.sh | sh
```

This installs the newest stable release (Linux and macOS, amd64 and arm64)
where `go install` would put it: `$GOBIN`, else `$GOPATH/bin`, else
`~/go/bin`, so it and `make install` never leave two copies on your PATH. Set
`DEVCTL_VERSION=vX.Y.Z` to pin a version, or `BINDIR` to change the
destination.

## Using it

```sh
devctl --help
```
