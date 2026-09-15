# Stories — apps

An app reaches a host as one object: the `<app>-<tag>.tar.xz` devctl built and
uploaded to `<backup.s3_uri>deploy/`. opsctl fetches it with the host's own
role and installs it — and installing is everything between that object and
the app answering at its own name. Uninstalling is the reverse, and it stops
short of the app's data: `state/` stays on the host, so a later install lands
over it. Restarting an app changes nothing on disk. What each host is running
is read back from the host itself, never from a record kept anywhere else.

The name the manifest declares is checked before anything is written, by the
rule devctl's build story states: a DNS label that is not `host`, `deploy`,
`backup-host`, `backup-services`, or `renew-certificate`, the prefixes and
unit names the host already uses. A file can come from anywhere, so install
does not trust that build checked.

The top-level usage gains four lines under `Commands:`:

```
  install   install an app from a built file
  uninstall take an app off the host, keeping its data
  restart   restart an installed app's service
  status    print every installed app, its version and its state
```

One configuration key:

| key | value |
|---|---|
| `aws.region` | the region this host's parameters, artifacts, and backups live in, e.g. `us-east-2` |

The file's layout is devctl's contract and carries no version inside it:
`bin/<app>`, `etc/manifest.toml`, whatever else the app keeps under `etc/`,
and `share/` when the app has one. The version is nowhere in the file:
`bin/<app>` answers `--version` with it, and that is what opsctl reports.
The manifest names the app, the port it listens on, whether it is the host's
default app, the secrets it needs, an `[env]` table of plain settings, and a
`[database]` table when the app keeps one:

```toml
app = "crm"
port = 3100
default = false
secrets = ["CRM_API_KEY", "CRM_API_SECRET", "CRM_ORG"]

[env]
OUTBOX_RETENTION_DAYS = "7"

[database]
engine = "sqlite"
path = "state/crm.db"
```

A host may have no default app, in which case its own name answers 404 (see
`nginx.md`); it may never have two.

`install` reads the app, the port, `default`, and `secrets`. The `[env]` table
it writes out. The `[database]` table it reads for one purpose only: to
regenerate `/etc/litestream.yml` from every manifest on the host, the way it
regenerates nginx, so that a database arrives on the host and starts being
replicated in the same command. What the table means, and what replication
is, are `backup.md`'s. It is in the manifest rather than the store because it
is a fact about the app, which travels with the app.

Secret *values* never travel in the file. devctl wrote them to the parameter
`/ikigenba/<host.name>/<app>` before the deploy, and the host's own role is
what reads them back; a host can read no other space's parameters.

An app owns its schema, and the file carries what that takes: at every start
the app creates the database its manifest declares if none is there, runs its
migrations forward, and loads its seed data only when it created the database
itself. opsctl never runs a migration and knows nothing about seeds. What the
host promises is the order: a restore lands `etc/`, `state/`, and the database
before the app's unit ever starts, and an install over a running app leaves
`state/` alone, so an app that is restored or upgraded migrates forward over
real data and never seeds over it. A space whose account backs nothing up
holds what its apps seeded and what has been typed into them since.

## An operator asks what `install` can do

Command:

```
$ opsctl install --help
```

```
$ opsctl install -h
```

Output:

```
Usage: opsctl install URI

Install the app at URI, an s3:// object holding an <app>-<tag>.tar.xz built by
devctl. The app name, its port, and the secrets it needs are read from
etc/manifest.toml inside it; the secret values are read from the parameter
/ikigenba/<host.name>/<app>.

Nothing under /opt/<app>/state/ or /opt/<app>/cache/ is touched, so installing
over a running app keeps its data. Safe to re-run.

The nginx configuration and /etc/litestream.yml are regenerated from every app
on the host, so an app that declares a [database] is replicated from the
moment it is installed. litestream.service is restarted only when its
configuration changed.

Configuration keys:
  aws.region  the region this host's parameters and artifacts live in
  host.name   the fully-qualified name this host answers at
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An agent installs an app

`devctl deploy` has uploaded the file and now runs one command over ssh. Each
line of output is one attempted step and reports its success or failure. The
last successful step reports the app, version and state; `status` independently
adds the database journal mode in its four-column report. The `fetch` step is the host reading the object
with its own role: the file never travels over the ssh connection. The
`litestream` step names the database this manifest declares, now in
`/etc/litestream.yml`; it comes before `service` so that replication is in
place before the app writes its first row.

Command:

```
$ sudo opsctl install s3://ikigenba-dev-295229566359/ikigenba.dev/deploy/crm-v0.1.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.1.0.tar.xz, 8.4 MiB)
file: ok (crm, port 3100)
secrets: ok (3 keys)
unpack: ok (/opt/crm)
unit: ok (ikigenba-crm.service)
nginx: ok (crm.ikigenba.dev)
litestream: ok (state/crm.db)
service: ok (crm v0.1.0 active)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `host.name` and `aws.region` are set, and `init` reported the host ready.
- The object holds `bin/crm` and `etc/manifest.toml`, and the host's role can
  read it.
- `/ikigenba/<host.name>/crm` holds every name the manifest's `secrets`
  array lists.
- `crm` has never been installed on this host.

Postconditions:

- `/opt/crm/bin/`, `/opt/crm/etc/`, and `/opt/crm/share/` are the file's,
  replacing whatever was there. `/opt/crm/state/` and `/opt/crm/cache/` were
  neither created nor touched.
- `/opt/crm/etc/env` has mode `0600` and holds one `NAME=value` line per
  secret the manifest names, every setting in its `[env]` table, and
  `PORT=3100`. Keys in the parameter that the manifest no longer names are
  not written. The values are never printed and never appear on a command
  line.
- `/etc/systemd/system/ikigenba-crm.service` runs `/opt/crm/bin/crm` with
  `/opt/crm` as its working directory and `/opt/crm/etc/env` as its
  environment file, as a non-root user, restarting on failure, wanted by
  `multi-user.target`. systemd has been reloaded and the unit is enabled.
- `/etc/nginx/conf.d/ikigenba.conf` has been regenerated and nginx reloaded,
  so `https://crm.ikigenba.dev` reaches `127.0.0.1:3100`.
- `/etc/litestream.yml` has been regenerated from every manifest under `/opt`
  and now names `/opt/crm/state/crm.db`, replicating to `<backup.s3_uri>crm/`.
  Because the file changed, `litestream.service` was restarted; it is running.
  Every other declared database on the host paused for the restart and is
  replicating again, the same window `backup.md` accepts for a restore.
- The unit is `active`, and the binary reports `v0.1.0`.
- No other app on the host has changed.

## An agent deploys a new version over a running app

The same command. Its whole point is that the app's data outlives it: the
tarball carries no `state/`, and opsctl writes none. The new manifest declares
the same database, so the regenerated `/etc/litestream.yml` is byte for byte
the old one and litestream is left alone: an upgrade of one app does not
interrupt the replication of any.

Command:

```
$ sudo opsctl install s3://ikigenba-dev-295229566359/ikigenba.dev/deploy/crm-v0.2.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.2.0.tar.xz, 8.4 MiB)
file: ok (crm, port 3100)
secrets: ok (4 keys)
unpack: ok (/opt/crm)
unit: ok (ikigenba-crm.service)
nginx: ok (crm.ikigenba.dev)
litestream: ok (unchanged)
service: ok (crm v0.2.0 active)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `crm` is installed and its unit is `active`.
- The new manifest names a fourth secret, and the parameter holds it.

Postconditions:

- Everything the first install's postconditions say, and: `/opt/crm/state/`
  is byte for byte as it was, the unit was restarted rather than started, and
  `status` now shows `crm v0.2.0 active wal`.
- `/etc/litestream.yml` is byte for byte as it was and `litestream.service`
  was not restarted, so its replication of `crm.db` was never interrupted. A
  manifest that changed its `[database]` path would have changed the file, and
  the line would have named the new path and the service been restarted.
- Installing the same file again produces the same eight lines and exit 0.

## An agent installs the host's default app

The app whose manifest sets `default = true` answers at the host's own name as
well as its own, and the nginx step says so. It declares no database, so the
regenerated `/etc/litestream.yml` is what it was.

Command:

```
$ sudo opsctl install s3://ikigenba-dev-295229566359/ikigenba.dev/deploy/dashboard-v0.0.9.tar.xz
```

Output:

```
fetch: ok (dashboard-v0.0.9.tar.xz, 5.2 MiB)
file: ok (dashboard, port 3200, default)
secrets: ok (0 keys)
unpack: ok (/opt/dashboard)
unit: ok (ikigenba-dashboard.service)
nginx: ok (dashboard.ikigenba.dev, ikigenba.dev)
litestream: ok (unchanged)
service: ok (dashboard v0.0.9 active)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `dashboard`'s manifest sets `default = true` and names no secrets.
- No other installed app sets `default = true`.

Postconditions:

- `https://ikigenba.dev` and `https://dashboard.ikigenba.dev` both reach
  `127.0.0.1:3200`; every other name under the host still answers 404.
- `/etc/litestream.yml` is byte for byte as it was and `litestream.service`
  was not restarted.

## An operator installs a second default app

Only one app answers at the host's own name. `install` reads the manifest of
every app already under `/opt` and refuses before it writes anything.
Installing the default app over itself is the upgrade path, not a conflict;
only another app claiming the apex is.

Command:

```
$ sudo opsctl install s3://ikigenba-dev-295229566359/ikigenba.dev/deploy/dashboard-v0.0.9.tar.xz
```

Output:

```
fetch: ok (dashboard-v0.0.9.tar.xz, 5.2 MiB)
file: failed: dashboard: crm is already the default app
opsctl: install failed
```

Exits 1. The fetch and file outcome lines are on stdout; the last line is on stderr.

Preconditions:

- `dashboard`'s manifest sets `default = true`.
- `crm` is installed and its manifest sets `default = true`.

Postconditions:

- Nothing has changed. `/opt/dashboard/` was not created, no unit was
  written, and nginx was not reloaded.
- Making `dashboard` the default means installing `crm` again with
  `default = false` first.

## An agent installs an app whose secret has never been pushed

Nothing is written until everything the app needs is in hand, so a missing
secret refuses the install rather than half-doing it.

Command:

```
$ sudo opsctl install s3://ikigenba-dev-295229566359/ikigenba.dev/deploy/gmail-v0.1.0.tar.xz
```

Output:

```
fetch: ok (gmail-v0.1.0.tar.xz, 6.1 MiB)
file: ok (gmail, port 3300)
secrets: failed: gmail: no value for 'GMAIL_CLIENT_SECRET' in /ikigenba/ikigenba.dev/gmail
opsctl: install failed
```

Exits 1. The step outcome lines are on stdout; the last line is on stderr.

Preconditions:

- The manifest names `GMAIL_CLIENT_SECRET` and the parameter does not hold
  it, or the parameter does not exist at all.

Postconditions:

- Nothing has changed. `/opt/gmail/` was not created, no unit was written,
  and nginx was not reloaded.

## An operator installs a file that is not an app

Command:

```
$ sudo opsctl install s3://ikigenba-dev-295229566359/ikigenba.dev/deploy/notes.tar.xz
```

Output:

```
fetch: ok (notes.tar.xz, 2.0 MiB)
file: failed: notes.tar.xz: no etc/manifest.toml in the file
opsctl: install failed
```

Exits 2. The fetch and file outcome lines are on stdout; the last line is on
stderr. A manifest that is not well-formed gives `file: failed: notes.tar.xz:
etc/manifest.toml: <decoder complaint>` on stdout after the same fetch line,
with `opsctl: install failed` on stderr. A missing object instead gives
`fetch: failed: notes.tar.xz: no such object` on stdout and
`opsctl: artifact download failed` on stderr, without attempting file
validation. Each exits 2.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.

## An operator installs an app with a reserved name

Command:

```
$ sudo opsctl install s3://ikigenba-dev-295229566359/ikigenba.dev/deploy/host-v0.1.0.tar.xz
```

Output:

```
fetch: ok (host-v0.1.0.tar.xz, 2.0 MiB)
file: failed: 'host' is not a usable app name
opsctl: install failed
```

Exits 2. The fetch and file outcome lines are on stdout; the last line is on stderr. A name that
is not a DNS label fails the same way.

Preconditions:

- `opsctl` is running as root.
- The file's `etc/manifest.toml` declares `app = "host"`.

Postconditions:

- Nothing has changed. Nothing under `/opt/` was written.

## An agent installs an app whose service will not come up

The app ran, or failed to, and systemd knows why. opsctl reports the step that
failed and hands over what the host said, each line quoted with `> `, so the
developer reading devctl's relayed output has the journal in front of them.

Command:

```
$ sudo opsctl install s3://ikigenba-dev-295229566359/ikigenba.dev/deploy/gmail-v0.1.0.tar.xz
```

Output:

```
fetch: ok (gmail-v0.1.0.tar.xz, 6.1 MiB)
file: ok (gmail, port 3300)
secrets: ok (2 keys)
unpack: ok (/opt/gmail)
unit: ok (ikigenba-gmail.service)
nginx: ok (gmail.ikigenba.dev)
litestream: ok (unchanged)
service: failed: gmail: service failed to start
opsctl: install failed

> ikigenba-gmail.service: Main process exited, code=exited, status=1/FAILURE
> gmail: listen tcp 127.0.0.1:3300: bind: address already in use
```

Exits 1. The step outcome lines are on stdout; the command diagnostic and quoted
journal are on stderr.

Preconditions:

- Another process already holds port 3300.
- `gmail`'s manifest declares no database.

Postconditions:

- The app is unpacked, its unit is written and enabled, and nginx routes its
  name. The unit is `failed`. Nothing was rolled back: the host is left in
  the state an operator can inspect and fix, and `status` shows
  `gmail v0.1.0 failed -`.
- Had the manifest declared a database, the `litestream` line would have
  named it and the failure would still be the service's: a failed unit does
  not undo a regenerated `/etc/litestream.yml`, and litestream replicates
  whatever the app manages to write.

## An operator runs `install` with no file, or more than one

Command:

```
$ sudo opsctl install
```

Output:

```
opsctl: install needs URI

see 'opsctl install --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. More than one operand gives
`opsctl: install takes one URI`. An operand that is not an `s3://` URI gives
`opsctl: install takes an s3:// URI`.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.

## An operator asks what `uninstall` can do

Command:

```
$ opsctl uninstall --help
```

```
$ opsctl uninstall -h
```

Output:

```
Usage: opsctl uninstall APP

Take APP off the host: stop and disable ikigenba-APP.service and remove it,
then remove /opt/APP/bin/, etc/, share/, and cache/. /opt/APP/state/ is kept
untouched, so APP is still a service the host backs up, and a later install
lands over its data the way an install over a restore does. Removing state/ is
a decision made by hand, never here.

The nginx configuration and /etc/litestream.yml are regenerated from every app
left on the host, so APP's name stops answering, and a database APP declared
stops being replicated once litestream has shipped what it holds.

The parameter /ikigenba/<host.name>/APP is not touched: it is devctl's.

Configuration keys:
  host.name  the fully-qualified name this host answers at
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An agent uninstalls an app

`devctl remove` runs this over ssh. Each line of output is one step, in the
reverse of install's order: the service goes first so nothing writes while the
rest is taken apart, and litestream goes last so that its restart's shutdown
sync ships every WAL frame of the database before the regenerated
configuration stops naming it. That is the same sync `retire` relies on. A
unit that was already inactive or failed is disabled and removed all the same,
and the `stop` line says `already inactive`.

Command:

```
$ sudo opsctl uninstall crm
```

Output:

```
stop: ok (ikigenba-crm.service stopped, disabled)
unit: ok (removed ikigenba-crm.service)
files: ok (removed /opt/crm/bin, etc, share, cache; kept state)
nginx: ok (crm.ikigenba.dev removed)
litestream: ok (state/crm.db removed)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `host.name` is set.
- `crm` is installed and its unit is `active`. Its manifest declares a
  `[database]` at `state/crm.db`, and `litestream.service` is replicating it.

Postconditions:

- `ikigenba-crm.service` is inactive and disabled,
  `/etc/systemd/system/ikigenba-crm.service` is gone, and systemd has been
  reloaded.
- `/opt/crm/` holds `state/` and nothing else. `bin/`, `etc/` with its
  `env` file, `share/`, and `cache/` are gone. Nothing under `state/` was
  read or written: the database, its `-wal` and `-shm`, and litestream's
  metadata directory are as the service left them.
- `/etc/nginx/conf.d/ikigenba.conf` has been regenerated and nginx reloaded:
  `crm.ikigenba.dev` answers 404 under the host's wildcard block. Had `crm`
  been the default app, the line would have read `crm.ikigenba.dev,
  ikigenba.dev removed` and the apex would answer 404 again.
- `/etc/litestream.yml` has been regenerated from the manifests left under
  `/opt` and no longer names `/opt/crm/state/crm.db`. Because the file
  changed, `litestream.service` was restarted. It was stopped after the
  service, so no writer was open and its shutdown sync shipped every frame it
  held: the replica under `<backup.s3_uri>crm/` holds `crm.db` as of the last
  committed transaction. Every other declared database paused for the restart
  and is replicating again.
- `status` shows `crm - - -`: a service with a `state/` and no manifest, the
  shape a restore into a fresh host leaves. `opsctl backup` now tars the whole
  of `state/`, the quiet database included, because no manifest declares it.
- `/ikigenba/<host.name>/crm` and the object under `<backup.s3_uri>deploy/`
  are untouched. Installing `crm` again lands over its `state/` exactly as a
  deploy over a restore does.
- No other app on the host has changed.

## An operator uninstalls the host's default app

The app declared no database, so the regenerated `/etc/litestream.yml` is what
it was and litestream is left alone. The nginx line names both names the app
answered at.

Command:

```
$ sudo opsctl uninstall dashboard
```

Output:

```
stop: ok (ikigenba-dashboard.service stopped, disabled)
unit: ok (removed ikigenba-dashboard.service)
files: ok (removed /opt/dashboard/bin, etc, share, cache; kept state)
nginx: ok (dashboard.ikigenba.dev, ikigenba.dev removed)
litestream: ok (unchanged)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `dashboard` is installed, its manifest sets `default = true`, and it
  declares no database.

Postconditions:

- `https://ikigenba.dev` and `https://dashboard.ikigenba.dev` both answer
  404. The host has no default app until an install brings one.
- `/etc/litestream.yml` is byte for byte as it was and `litestream.service`
  was not restarted.
- `/opt/dashboard/` holds `state/` and nothing else.

## An operator uninstalls an app that is not installed

A directory with a `state/` and no binary is a service, and the host backs it
up, but there is nothing installed to take off. Nothing is a stronger case of
the same.

Command:

```
$ sudo opsctl uninstall gmail
```

Output:

```
stop: failed: gmail is not installed
opsctl: uninstall failed
```

Exits 1. The stop outcome is on stdout; the command diagnostic is on stderr.
A name with no directory under `/opt/` at all gives
`stop: failed: no service 'gmail'` on stdout and the same command diagnostic,
also exit 1.

Preconditions:

- `/opt/gmail/` holds a `state/` and no `bin/gmail`, and there is no
  `ikigenba-gmail.service`; or `/opt/gmail/` does not exist.

Postconditions:

- Nothing has changed.

## An operator runs `uninstall` with no app, or more than one

Command:

```
$ sudo opsctl uninstall
```

Output:

```
opsctl: uninstall needs APP

see 'opsctl uninstall --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. More than one operand gives
`opsctl: uninstall takes one APP`.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.

## An operator asks what `restart` can do

Command:

```
$ opsctl restart --help
```

```
$ opsctl restart -h
```

Output:

```
Usage: opsctl restart APP

Restart ikigenba-APP.service and report the service as the last line of
'opsctl install' does. Nothing on disk changes: the binary, the environment
file, and the unit are what the last install wrote, so a secret pushed since
then is not picked up here. A unit that is inactive or failed is started.
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## A developer restarts an app

`devctl space restart` runs this over ssh. The one line is install's
`service` line: the same question asked of the same unit.

Command:

```
$ sudo opsctl restart crm
```

Output:

```
service: ok (crm v0.1.0 active)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `crm` is installed. Its unit may be `active`, `inactive`, or `failed`.

Postconditions:

- `ikigenba-crm.service` is `active` and its main process is a new one.
- Nothing under `/opt/crm/` or `/etc/` was written. The environment systemd
  gave the new process is `/opt/crm/etc/env` as the last install wrote it.
- No unit was enabled or disabled, nginx was not reloaded, and
  `litestream.service` was not touched: the app closed and reopened its
  database, and litestream never stopped watching it.
- No other app on the host has changed.

## A developer restarts an app whose service will not come back

The same failure install reports, reported the same way: the step that failed
and what the host said, each line quoted with `> `.

Command:

```
$ sudo opsctl restart crm
```

Output:

```
service: failed: crm: service failed to start
opsctl: restart failed

> ikigenba-crm.service: Main process exited, code=exited, status=1/FAILURE
> crm: open /opt/crm/state/crm.db: permission denied
```

Exits 1. The service outcome is on stdout; the command diagnostic and quoted
journal are on stderr.

Preconditions:

- `crm` is installed and its binary exits at start.

Postconditions:

- The unit is `failed`, and `status` shows `crm v0.1.0 failed wal`. Nothing
  on disk changed and nothing was rolled back: the host is left where an
  operator can look at it.

## An operator restarts an app that is not installed

Command:

```
$ sudo opsctl restart gmail
```

Output:

```
service: failed: gmail is not installed
opsctl: restart failed
```

Exits 1. The service outcome is on stdout; the command diagnostic is on stderr.
A name with no directory under `/opt/` at all gives
`service: failed: no service 'gmail'` on stdout and the same command diagnostic,
also exit 1. With no operand the diagnostic is `opsctl: restart needs APP`,
with more than one `opsctl: restart takes one APP`, each followed by the usage
hint and exit 2; these grammar failures have no service outcome and empty stdout.

Preconditions:

- `/opt/gmail/` holds a `state/` and no `bin/gmail`, and there is no
  `ikigenba-gmail.service`; or `/opt/gmail/` does not exist.

Postconditions:

- Nothing has changed.

## An operator asks what `status` can do

Command:

```
$ opsctl status --help
```

```
$ opsctl status -h
```

Output:

```
Usage: opsctl status

Print one line per service on this host, in name order: its name, the version
its own binary reports, the state of its systemd unit, and the journal mode of
the database its manifest declares. A service is any /opt/<name>/ with an etc/
or state/ directory; '-' means opsctl could not ask, or there was nothing to
ask.

A declared database must stay in WAL mode: litestream cannot replicate one in
any other mode, so a service reporting anything but 'wal' is a service whose
data is not reaching S3.

The exit code is 0 whatever the report says. A failed unit and an unreplicable
database are facts about the host, not failures of this command.
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## A developer asks what a host is running

The answer comes from the host and nowhere else: the services under `/opt`,
each app's own binary asked its version with `--version`, each app's unit
asked its state, and each declared database asked its journal mode. One line
per app, in name order. `devctl space status` runs exactly this over ssh and
copies the output to the developer's terminal byte for byte.

The fourth field is `-` for a service that declares no database, because there
was nothing to ask — the same `-` the other fields use.

Command:

```
$ sudo opsctl status
```

Output:

```
crm v0.1.0 active wal
dashboard v0.0.9 active -
gmail v0.1.0 failed -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- Three apps are installed; `gmail`'s unit is `failed`.
- `crm`'s manifest declares a `[database]` and that database is in WAL mode.
  Neither of the others declares one.

Postconditions:

- Nothing has changed.

A failed unit is a fact about the host, which is what `status` was asked to
look at, so it is part of the report and not a failure of the command: the
exit code is 0 whatever the report says.

## A developer asks what a host with no apps is running

Command:

```
$ sudo opsctl status
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- No directory under `/opt/` holds an `etc/` or a `state/`.

Postconditions:

- Nothing has changed.

## A developer asks about a host holding a service opsctl did not install

A directory under `/opt` with a `state/` and no binary is what a restore into
a fresh host leaves, and what `uninstall` leaves behind. It is a service — the host will back it up — and it is
not something opsctl can ask a version or a unit state of, so both are `-`.

Command:

```
$ sudo opsctl status
```

Output:

```
crm v0.1.0 active wal
gmail - - -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `/opt/gmail/` holds a `state/` and no `bin/gmail`, there is no
  `ikigenba-gmail.service`, and its manifest declares no database.

Postconditions:

- Nothing has changed.

## A developer asks about a host whose database stopped being replicated

Declaring a `[database]` obliges the app to keep it in WAL mode. litestream
replicates nothing else, so an app that switches journal mode ends its own
replication — and ends it silently, because the database goes on working, the
unit goes on running, and the backup timer goes on writing a tarball that
excludes the very file that is no longer being shipped. The failure surfaces
at the next restore, which is the worst possible moment to learn about it.

So status asks. The journal mode is a fact about the host, which is what status
was asked to look at, so it is reported the way a failed unit is: in the line,
with exit 0.

Command:

```
$ sudo opsctl status
```

Output:

```
crm v0.1.0 active delete
dashboard v0.0.9 active -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `crm`'s manifest declares a `[database]` and that database's journal mode is
  `delete`.

Postconditions:

- Nothing has changed. status reads the journal mode and never sets it:
  putting the database back into WAL mode is the app's to do, not opsctl's.

`crm` is healthy by every other measure in the line, which is the point of
reporting this one. Its unit is active, its version is what was installed, and
its data has not been reaching S3 since whenever the mode changed.

## An operator gives status an argument

Command:

```
$ sudo opsctl status crm
```

Output:

```
opsctl: status takes no arguments

see 'opsctl status --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.
