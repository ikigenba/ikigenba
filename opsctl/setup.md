# Configuring opsctl and activating a release on a host

This document is written for an agent, and picks up where `bootstrap.md`
leaves off: a name resolves to a Linux host you can reach over ssh as a user
with sudo, with the wildcard under that name resolving to the same host, and
with credentials in place for DNS and backups. Read it whole before doing
anything. It takes that host, with a release unpacked on it, through seeding the config
store, an optional host restore, `init`, optional service restores, and
`activate`.

The order is fixed: `config set` for each key, `host restore` if the old
host's certificate and config are wanted back, `init`, `restore <app>` for
each service whose data is wanted back, then `activate <sha> [label]`.

Throughout, `<name>` is the fully-qualified name the host answers at — the
name `bootstrap.md` ended with, for example `ikigenba.dev` — and `<zone>` is
the DNS zone that name lies in, which may be the same as `<name>` or a parent
of it.

`opsctl` refuses to run as anything but root (only `version` and help are
exempt), so every command that does real work is run through `sudo` on the
host.

## The release

`opsctl` has no installer. It ships inside the suite release, which devctl
builds and unpacks on the host as `/opt/ikigenba/releases/<sha>/` (`<sha>` is
the full 40-character commit sha). That folder holds the release's own opsctl
at `/opt/ikigenba/releases/<sha>/opsctl/bin/opsctl`. Unpacking is devctl's, not
`opsctl`'s; this document starts once the folder is there.

On a fresh host `/usr/local/bin/opsctl` does not exist yet, so until the
release is activated every command below is run by the absolute path of the
release's own opsctl:

```
O=/opt/ikigenba/releases/<sha>/opsctl/bin/opsctl
sudo $O version
```

`init` makes `/usr/local/bin/opsctl` a link to the opsctl that ran it, and
`activate` re-creates it as a link to
`/opt/ikigenba/current/opsctl/bin/opsctl`, so afterwards plain `opsctl` is
always the running release's. `make deploy` is only for trying a change to
`opsctl` on a host; the file it installs lasts until the next activate.

The preconditions are ssh to `<user>@<name>` with the key already in place
and passwordless `sudo` for that user. Both come from `bootstrap.md`.

## Configure

`opsctl` reads its inputs from the config store, one JSON file at
`/etc/ikigenba/config.json`. Each design declares the keys it reads; today
the ones `init` needs are three, and the provider's zone id is supplied by the
person — `opsctl` never reads it from a bootstrap's own files. `activate` also
reads `aws.region` and `backup.s3_uri` (and `host.apex`, when an app answers at
the parent name); the story for each key says what it is:

```
sudo $O config set dns.provider=route53
sudo $O config set dns.zones=<zone>:<hosted-zone-id>
sudo $O config set host.name=<name>
sudo $O config set aws.region=<region>
sudo $O config set backup.s3_uri=<s3-prefix-uri>
sudo $O config list
```

`dns.zones` is the comma-separated set of `NAME:ID` pairs for the zones
`opsctl` owns; a record name is mapped to a zone by longest-suffix match
against the zone names. `host.name` is the name this host answers at; it must
be the apex of a configured zone or a name beneath one, and the records
bootstrap created are `<name>` and `*.<name>`.

## host restore

On a rebuilt host, bring back the old host's `/etc/ikigenba/` (the config
store included) and `/etc/letsencrypt/` from the backup bucket instead of
issuing a certificate again. It needs `aws.region` and `backup.s3_uri` set, so
it comes after those `config set` lines; skip it on a host with no backup.

```
sudo $O host restore
```

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

The sequence is the setup sub-commands that exist, and `opsctl init --help`
always shows the current one. On a fresh host it does the host's own work and
has no app to write: the certificate, the slices, an nginx file with no apps,
the account `ikigenba`, Litestream and the timers. It also makes
`/usr/local/bin/opsctl` a link to the opsctl that ran it when none exists, so
certbot's hooks find it.

```
sudo $O init
```

## restore and activate

If a service's data is wanted back, restore it now, after `init` and before the
release runs it (`opsctl restore --help` has the options):

```
sudo $O restore <app>
```

Then make the release the one the host runs. `<label>` is optional; without
it the release is shown by its short sha.

```
sudo $O activate <sha> [<label>]
```

`activate` checks everything before it changes anything, writes each app's
environment file and units, switches `/opt/ikigenba/current` and `previous`,
re-creates `/usr/local/bin/opsctl` as a link to current's opsctl, regenerates
nginx and the services file, and restarts the apps one at a time. It prints one
line per step and stops at the first failure; a later `rollback` goes back one
release.

## Verifying the install

After `activate`, plain `opsctl` is the running release's. Confirm from the
host that it runs and its config round-trips:

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
