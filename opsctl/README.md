> [!WARNING]
> This is unsupported AI slop.

# opsctl

`opsctl` is a CLI that bootstraps and manages the Ikigenba platform on a host.
It runs as root on the platform's Linux host, where it is used by humans and
agents, and by [`devctl`](../devctl) over ssh, to set up the host, activate
suite releases, and restart, back up, and restore apps.

## Installing it

opsctl has no installer of its own. It ships inside the suite release: devctl
builds the release and unpacks it on the host at
`/opt/ikigenba/releases/<sha>/`, and that release's
`opsctl/bin/opsctl activate` makes it the one the host runs and links
`/usr/local/bin/opsctl` to `/opt/ikigenba/current/opsctl/bin/opsctl`. See
[`setup.md`](setup.md) for a new host.

## Using it

```sh
opsctl --help
```
