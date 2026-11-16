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

## Custom domains

A custom domain is a name outside `host.name` whose A record points at the
host. A primary name is served by the `sites` app; a redirect name answers 301
to its primary. Each name gets a certificate of its own by HTTP-01 from
`/var/www/acme`, renewed by the host's existing certbot timer.

```sh
opsctl domain add example.com
opsctl domain add www.example.com --redirect example.com
opsctl domain list
opsctl domain remove www.example.com
```

`add` and `remove` apply nginx themselves; `opsctl domain --help` has the
rest.
