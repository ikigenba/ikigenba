> [!WARNING]
> This is unsupported AI slop.

# opsctl

`opsctl` is a CLI that bootstraps and manages the Ikigenba platform on a host.
It runs as root on the platform's Linux host, where it is used by humans and
agents, and by [`devctl`](../devctl) over ssh, to set up the host and install,
restart, back up, and restore apps.

## Installing it

On the platform host, as root, run a release's installer with its version:

```sh
curl -fsSL -o /tmp/opsctl-install.sh https://github.com/ikigenba/ikigenba/releases/download/opsctl/vX.Y.Z/install.sh
sudo bash /tmp/opsctl-install.sh vX.Y.Z
```

## Using it

```sh
opsctl --help
```
