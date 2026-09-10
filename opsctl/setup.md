# Installing and configuring opsctl on a host

This document is written for an agent, and picks up where `bootstrap.md`
leaves off: a name resolves to a Linux host you can reach over ssh as a user
with sudo, with the wildcard under that name resolving to the same host, and
with credentials in place for DNS and backups. Read it whole before doing
anything. It takes that host through installing `opsctl`, seeding its config
store, and running `init`.

Throughout, `<name>` is the fully-qualified name the host answers at — the
name `bootstrap.md` ended with, for example `ikigenba.dev` — and `<zone>` is
the DNS zone that name lies in, which may be the same as `<name>` or a parent
of it.

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
make deploy DEPLOY_HOST=<name> DEPLOY_USER=<user>
```

`DEPLOY_HOST` defaults to `ikigenba.dev`, the live box for this project, and
`DEPLOY_USER` to `ec2-user`, so a plain `make deploy` targets it. The target
cross-compiles the binary, scps it to `/tmp` on the host, `sudo install`s it to
`/usr/local/bin/opsctl`, and runs `opsctl version` as the smoke test. When that
prints a version, the install is done.

The preconditions are just ssh: `<user>@<name>` reachable with the key already
in place, and passwordless `sudo` for that user. Both come from `bootstrap.md`.

## Configure

`opsctl` reads its inputs from the config store, one JSON file at
`/etc/ikigenba/config.json`. Each design declares the keys it reads; today
there are three, and the provider's zone id is supplied by the person —
`opsctl` never reads it from a bootstrap's own files:

```
sudo opsctl config set dns.provider=route53
sudo opsctl config set dns.zones=<zone>:<hosted-zone-id>
sudo opsctl config set host.name=<name>
sudo opsctl config list
```

`dns.zones` is the comma-separated set of `NAME:ID` pairs for the zones
`opsctl` owns; a record name is mapped to a zone by longest-suffix match
against the zone names. `host.name` is the name this host answers at; it must
be the apex of a configured zone or a name beneath one, and the records
bootstrap created are `<name>` and `*.<name>`.

## init

`init` is the setup sequence behind one preflight. The preflight evaluates
every check and prints one line per check to stdout — `<check>: ok (<detail>)`
or `<check>: failed: <reason>` — so a single run shows everything that is
missing. When any check fails, `init` prints the whole list, runs nothing, and
exits 2; fix what it names and run it again. When every check passes it runs
the sequence and exits 0. It is safe to re-run at any time.

The checks, in order: `nginx`, `certbot`, and `systemctl` found on PATH;
`dns.provider`, `dns.zones`, and `host.name` set (and the provider opening);
every configured zone reachable and delegated; `host.name` lying within a
configured zone; and `<name>` and `_opsctl-preflight.<name>` resolving to the
same address from the host, which is `bootstrap.md`'s done-condition checked
from the inside. A check that depends on another check's result is left off
the list when that other check failed; its prerequisite is already listed.

The sequence is the setup sub-commands that exist. Today there are none, so
`init` is exactly its preflight; each design that adds a setup command adds
itself to the sequence, and `opsctl init --help` always shows the current one.

```
sudo opsctl init
```

## Verifying the install

Confirm from the host that `opsctl` runs and its config round-trips:

```
ssh <user>@<name> opsctl version
ssh <user>@<name> sudo opsctl config list
```

`init` confirms every prerequisite at once and exits 0 with every line `ok`:

```
ssh <user>@<name> sudo opsctl init
```

`dns check` confirms every configured zone is reachable and delegated on its
own, and a live add/list/remove round-trip through Route 53 is the real proof
the credentials and provider seam work end to end. The record grammar is
`NAME TYPE VALUE`; the zone is inferred from NAME, not passed:

```
ssh <user>@<name> sudo opsctl dns check
ssh <user>@<name> sudo opsctl dns add    _opsctl-check.<name> TXT hello-1
ssh <user>@<name> sudo opsctl dns list   <zone>
ssh <user>@<name> sudo opsctl dns remove _opsctl-check.<name> TXT hello-1
```
