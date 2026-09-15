# Stories — apps

An app reaches a host as one object: the `<app>-<tag>.tar.xz` devctl built and
uploaded to `<backup.s3_uri>deploy/`. opsctl fetches it with the host's own
role and installs it — and installing is everything between that object and
the app answering at its own name. What each host is running is
read back from the host itself, never from a record kept anywhere else.

The top-level usage gains two lines under `Commands:`:

```
  install   install an app from a built file
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
it writes out; the `[database]` table it does not read at all — that one is
`backup.md`'s, and it is in the manifest rather than the store because it is a
fact about the app, which travels with the app.

Secret *values* never travel in the file. devctl wrote them to the parameter
`/ikigenba/<host.name>/<app>` before the deploy, and the host's own role is
what reads them back; a host can read no other space's parameters.

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
line of output is one step, and the last is exactly the line `status` will
print for this app from now on. The `fetch` step is the host reading the object
with its own role: the file never travels over the ssh connection.

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
- The unit is `active`, and the binary reports `v0.1.0`.
- No other app on the host has changed.

## An agent deploys a new version over a running app

The same command. Its whole point is that the app's data outlives it: the
tarball carries no `state/`, and opsctl writes none.

Command:

```
$ sudo opsctl install s3://ikigenba-dev-295229566359/ikigenba.dev/deploy/crm-v0.2.0.tar.xz
```

Output:

```
file: ok (crm, port 3100)
secrets: ok (4 keys)
unpack: ok (/opt/crm)
unit: ok (ikigenba-crm.service)
nginx: ok (crm.ikigenba.dev)
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
- Installing the same file again produces the same six lines and exit 0.

## An agent installs the host's default app

The app whose manifest sets `default = true` answers at the host's own name as
well as its own, and the nginx step says so.

Command:

```
$ sudo opsctl install s3://ikigenba-dev-295229566359/ikigenba.dev/deploy/dashboard-v0.0.9.tar.xz
```

Output:

```
file: ok (dashboard, port 3200, default)
secrets: ok (0 keys)
unpack: ok (/opt/dashboard)
unit: ok (ikigenba-dashboard.service)
nginx: ok (dashboard.ikigenba.dev, ikigenba.dev)
service: ok (dashboard v0.0.9 active)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `dashboard`'s manifest sets `default = true` and names no secrets.
- No other installed app sets `default = true`.

Postconditions:

- `https://ikigenba.dev` and `https://dashboard.ikigenba.dev` both reach
  `127.0.0.1:3200`; every other name under the host still answers 404.

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
file: ok (dashboard, port 3200, default)
opsctl: dashboard: crm is already the default app
```

Exits 1. The `ok` lines are on stdout; the last line is on stderr.

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
opsctl: gmail: no value for 'GMAIL_CLIENT_SECRET' in /ikigenba/ikigenba.dev/gmail
```

Exits 1. The `ok` lines are on stdout; the last line is on stderr.

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
opsctl: notes.tar.xz: no etc/manifest.toml in the file
```

Exits 2. The `ok` line is on stdout; the last line is on stderr. A manifest
that is not well-formed gives `opsctl: notes.tar.xz: etc/manifest.toml:` and
the decoder's complaint after the same `fetch` line. A path that does not
exist fails before it: `opsctl: notes.tar.xz: no such object` on stderr with
stdout empty. Each exits 2.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.

## An agent installs an app whose service will not come up

The app ran, or failed to, and systemd knows why. opsctl reports the step that
failed and hands over what the host said, so the developer reading devctl's
relayed output has the journal in front of them.

Command:

```
$ sudo opsctl install s3://ikigenba-dev-295229566359/ikigenba.dev/deploy/gmail-v0.1.0.tar.xz
```

Output:

```
file: ok (gmail, port 3300)
secrets: ok (2 keys)
unpack: ok (/opt/gmail)
unit: ok (ikigenba-gmail.service)
nginx: ok (gmail.ikigenba.dev)
opsctl: gmail: service failed to start

ikigenba-gmail.service: Main process exited, code=exited, status=1/FAILURE
gmail: listen tcp 127.0.0.1:3300: bind: address already in use
```

Exits 1. The `ok` lines are on stdout; the rest is on stderr.

Preconditions:

- Another process already holds port 3300.

Postconditions:

- The app is unpacked, its unit is written and enabled, and nginx routes its
  name. The unit is `failed`. Nothing was rolled back: the host is left in
  the state an operator can inspect and fix, and `status` shows
  `gmail v0.1.0 failed -`.

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
a fresh host leaves. It is a service — the host will back it up — and it is
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
