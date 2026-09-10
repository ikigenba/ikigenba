# Installing and configuring opsctl on a host

This document is written for an agent, and picks up where `bootstrap.md`
leaves off: a DNS name resolves to a Linux host you can reach over ssh as a
user with sudo, with the wildcard resolving to the same host, and with
credentials in place for DNS and backups. Read it whole before doing anything.
It takes that host through installing `opsctl`, seeding its config store, and
running `init`.

`opsctl` refuses to run as anything but root (only `version` and help are
exempt), so every command that does real work is run through `sudo` on the
host.

## Install

`opsctl` is a single Go binary at `/usr/local/bin/opsctl`. It is built for
`linux/amd64` and copied to the host; there is nothing to compile there.

Direct root login is not assumed — no default host image allows it. Instead
`make deploy` logs in as an ordinary user with sudo and installs the binary
with `sudo install`:

```
make deploy DEPLOY_HOST=<zone> DEPLOY_USER=<user>
```

`DEPLOY_HOST` defaults to `ikigenba.dev`, the live box for this project, and
`DEPLOY_USER` to `ec2-user`, so a plain `make deploy` targets it. The target
cross-compiles the binary, scps it to `/tmp` on the host, `sudo install`s it to
`/usr/local/bin/opsctl`, and runs `opsctl version` as the smoke test. When that
prints a version, the install is done.

The preconditions are just ssh: `<user>@<host>` reachable with the key already
in place, and passwordless `sudo` for that user. Both come from `bootstrap.md`.

## Configure

`opsctl` reads its inputs from the config store, one JSON file at
`/etc/ikigenba/config.json`. Each later design declares the keys it reads. DNS
needs three, and the provider's zone id is supplied by the person — `opsctl`
never reads it from a bootstrap's own files:

```
sudo opsctl config set dns.provider=route53
sudo opsctl config set dns.zones=<zone>
sudo opsctl config set dns.route53.zone.<zone>=<hosted-zone-id>
sudo opsctl config list
```

`dns.zones` is the comma-separated set of zones opsctl owns; a record NAME is
mapped to a zone by longest-suffix match against it.

## init

Not yet designed. When it exists, `init` runs the setup sub-commands behind
one fail-fast preflight; this section will document that sequence.

## Verifying the install

Confirm from the host that `opsctl` runs and its config round-trips:

```
ssh <user>@<zone> opsctl version
ssh <user>@<zone> sudo opsctl config list
```

`dns check` confirms every configured zone is reachable and delegated:

```
ssh <user>@<zone> sudo opsctl dns check
```

A live add/list/remove round-trip through Route 53 is the real proof the
credentials and provider seam work end to end. The record grammar is
`NAME TYPE VALUE`; the zone is inferred from NAME, not passed:

```
ssh <user>@<zone> sudo opsctl dns add    _opsctl-check.<zone> TXT hello-1
ssh <user>@<zone> sudo opsctl dns list   <zone>
ssh <user>@<zone> sudo opsctl dns remove _opsctl-check.<zone> TXT hello-1
```
