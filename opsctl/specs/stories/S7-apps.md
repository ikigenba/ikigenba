# Stories — apps

An app reaches a host as one object: the `<app>-<tag>.tar.xz` devctl built and
uploaded to `<backup.s3_uri>deploy/`. opsctl fetches it with the host's own
role and installs it — and installing is everything between that object and
the app answering at its own name. Uninstalling is the reverse, and it stops
short of the app's data: `/var/opt/ikigenba/<app>/state/` stays on the host,
so a later install lands over it. Restarting an app changes nothing on disk.
What each host is running is read back from the host itself, never from a
record kept anywhere else.

The name the manifest declares is checked before anything is written: it is a
DNS label that is not `host`, `deploy`, `snapshots`, `seed`, `backup-host`,
`backup-services`, or `renew-certificate`, the prefixes and unit names the
host already uses. `snapshots` and `seed` are prefixes under the space's own
(see `S8-backup.md`), where an app of that name would keep its backups and
replica beside them. devctl's build checks a name too, but a file can come
from anywhere, so install does not trust that build checked.

The top-level usage gains six lines under `Commands:`:

```
  install   install an app from a built file
  uninstall take an app off the host, keeping its data
  restart   restart an installed app's service
  disable   stop an installed app and keep it from starting
  enable    let a disabled app start again, and start it
  status    print every installed app, its version and its state
```

Three configuration keys:

| key | value |
|---|---|
| `aws.region` | the region this host's parameters, artifacts, and backups live in, e.g. `us-east-2` |
| `apps.drain_seconds` | how long every app may spend finishing the requests it holds once told to stop, in whole seconds; unset or empty means `5` |
| `apps.stop_seconds` | how long systemd waits for every app to stop before killing it, in whole seconds; unset or empty means `10` |

The two timing keys are space-wide: no manifest sets them, and every app on
the host gets the same values. Each must be a positive whole number, and
`apps.stop_seconds` must be greater than `apps.drain_seconds`, so an app that
drains for as long as it is allowed still exits on its own before systemd
kills it. `install` writes them into the app it installs; `init` writes them
into every installed app (see `S4-init.md`), which is how a changed value
reaches apps already on the host.

The file's layout is devctl's contract and carries no version inside it:
`bin/<app>`, `etc/manifest.toml`, whatever else the app keeps under `etc/`,
and `share/` when the app has one. The version is nowhere in the file:
`bin/<app>` answers `--version` with it, and that is what opsctl reports.
The manifest names the app, whether it is the host's default app, the secrets
it needs, an `[env]` table of plain settings, and a `[database]` table when the
app keeps one. It may also carry a `description`, one line of text saying what
the app offers, and `mcp`, a Boolean that is `false` when absent: `true` means
the app's tools belong in the suite's MCP catalog. An app that sets `mcp =
true` must say what it offers, so its `description` may not be missing, empty,
or only whitespace. A `description` is one line of text when it holds no
control character (U+0000–U+001F, U+007F), so no line break. It may also carry
`guests`, a Boolean that is `false` when absent: `true` means the app serves
guests, visitors with no credential. On a host with an authenticator, such a
visitor reaches the app's pages, with no identity, instead of being sent to
sign in, while `/mcp`, `/api`, and git paths stay challenged (see
`S5-nginx.md`). On a host without one every app is already open, and `guests`
changes nothing.
`guests` is not in the services file and no command shows it. The manifest
names no port: no app listens on one, and a manifest that carries a `port` is
refused.

A manifest may also carry a `[resources]` table saying where the app runs and
how much of the host it may take. Every key in it is optional:

```toml
[resources]
slice = "apps"
memory_max = "256M"
go_memory_limit = "128M"
cpu_weight = 100
delegate = true
oom_policy = "continue"
```

- `slice` is `"core"` or `"apps"`, the `ikigenba-core.slice` or
  `ikigenba-apps.slice` that `init` writes (`S4-init.md`); absent means
  `"apps"`. A core app is one the rest of the suite cannot work without:
  it shares nginx's slice and keeps 32 MiB of memory however short its
  neighbours are.
- `memory_max` is the most memory the app may hold, a string that is a
  positive whole number of bytes optionally followed by `K`, `M`, or `G`
  (each 1024 times the one before); absent means `"128M"`. It may not be
  more than the ceiling above it: `ikigenba-apps.slice`'s `MemoryMax` for
  an apps app, and `ikigenba.slice`'s for a core app, whose own slice has
  none.
- `go_memory_limit` is the soft limit the app's Go runtime collects garbage
  against, in the same form as `memory_max`; absent means three quarters of
  `memory_max`, rounded down to a whole byte. It may not be larger than
  `memory_max`.
- `cpu_weight` is the app's share of CPU among its slice's services when the
  host is busy, a whole number from 1 to 10000; absent means `100`.
- `delegate`, a Boolean, hands the app its own part of the control group
  tree to arrange the processes it starts in. Any app may set it; `scripts`
  is the one that does.
- `oom_policy` admits one value, `"continue"`: when the kernel kills one of
  the app's processes for want of memory, the app keeps running rather than
  being stopped whole. Any app may set it; `repos` is the one that does.

Any other key in the table is refused, so a misspelt limit is never silently
dropped; `io_weight`, which an older manifest may carry, is such a key. The
limits are the app's, not a process's: everything the app starts — a `git`
it runs for a client, say — runs inside the app's service and counts against
them, so an app cannot escape its ceiling by doing its work in children.

`install` judges `memory_max` against the slice units `init` wrote, not the
host's memory, at its `file` step, before anything on the host is written. It
needs `/etc/systemd/system/ikigenba.slice` and the app's own slice unit, and
each that should carry a `MemoryMax` must carry one it can read; when one is
missing or has none, the remedy is `opsctl init`.

A slice's ceiling bounds what its apps hold together, not what each may ask
for, so the `memory_max` values in a slice may add up to more than the slice
has. `install` lets them, up to a point: once they add up to more than twice
the slice's `MemoryMax`, it still installs the app but says so on its `unit`
line. It looks at `ikigenba-apps.slice` and at `ikigenba.slice`, which counts
every installed app and nginx's 128 MiB.

A file that carries `share/icon.svg` puts its app in the host's service
launcher; nothing in the manifest does. `install` refuses an icon that is not
an SVG image or is larger than 64 KiB. Every command here that regenerates
nginx also rewrites `/var/lib/ikigenba/services.json`, the list of the host's
installed services, each with its manifest's `description` and `mcp`, and its
icon when it ships one (see `S9-services.md`), and reports it on a `services`
line right after the `nginx` line. Every installed app has an entry there,
icon or not, so the line reports a change for any app that is added, removed,
disabled, or enabled; a reinstall changes the app's entry only when its
`description`, `mcp`, or icon changed.

```toml
app = "crm"
description = "Customers, contacts, and deals"
default = false
mcp = true
secrets = ["CRM_API_KEY", "CRM_API_SECRET", "CRM_ORG"]

[env]
OUTBOX_RETENTION_DAYS = "7"

[database]
engine = "sqlite"
path = "state/crm.db"
```

The stories below install this manifest as it stands, with no
`[resources]`, unless one says otherwise.

A host may have no default app, in which case its own name answers 404 (see
`S5-nginx.md`); it may never have two. The root domain's apex is a separate
choice, made in the store rather than the manifest (`host.apex`, see
`S5-nginx.md`): the app it names answers at `ikigenba.dev` as well, and
`install` and `uninstall` report that name the way they report the space's.

`install` reads the app, `default`, `secrets`, `description`, `mcp`, and
`guests`. The `[env]` table it writes out, into the app's environment file,
and the `[resources]` table into the service unit. The `[database]` table it reads for one purpose only: to
regenerate `/etc/litestream.yml` from every manifest on the host, the way it
regenerates nginx, so that a database arrives on the host and starts being
replicated in the same command. What the table means, and what replication
is, are `S8-backup.md`'s. It is in the manifest rather than the store because it
is a fact about the app, which travels with the app.

Secret *values* never travel in the file. devctl wrote them to the parameter
`/<host.name>/<app>` before the deploy — `/sbx.ikigenba.dev/crm` for `crm`
on the space `sbx` — and the host's own role is what reads them back; a host
can read no other space's parameters.

An app's environment file is host configuration, not part of what the file
brought: `/etc/opt/ikigenba/<app>/env`, owned `root:root` with mode `0600`.
`install` writes it whole on every run from the manifest, the parameter, and
the store, `restore` writes it the same way (see `S8-backup.md`), and `init`
rewrites the store's values in it (see `S4-init.md`). It is never backed
up: it is generated, so a host writes it again rather than carrying a stale
copy forward. `install` creates `/etc/opt/` and
`/etc/opt/ikigenba/` owned `root:root` with mode `0755` if they are missing,
and `/etc/opt/ikigenba/<app>/` the same way; that directory holds the `env`
file and nothing else. A host whose apps were installed before the file lived
there holds it at `/opt/<app>/etc/env`; it goes with the rest of the old
`etc/` when an install replaces it, and with `/opt/<app>/` when an uninstall
removes it. Nothing reads it there except a unit an older install wrote.

Each app runs as two systemd units, both generated by `install` and removed by
`uninstall`, and never backed up:

- `ikigenba-<app>.socket` holds the app's Unix socket,
  `/run/ikigenba/<app>.sock`, owned by the user `ikigenba` and the group
  `nginx` with mode `0660`, so nginx and the platform's own processes can
  connect and nothing else can. It names its listen backlog explicitly, is
  wanted by `sockets.target`, and removes the socket file when it stops. It is
  enabled, so the socket listens from boot.
- `ikigenba-<app>.service` runs `/opt/<app>/bin/<app>` with
  `/var/opt/ikigenba/<app>` as its working directory and
  `/etc/opt/ikigenba/<app>/env` as its environment file, as
  the user `ikigenba` — every app runs as that one user. It requires the
  socket and is ordered after it, is handed the socket as its listener rather
  than opening one, tells systemd when it is ready to serve, and is restarted
  on failure. Its stop timeout is `apps.stop_seconds`. It runs in
  `ikigenba-<slice>.slice`, with the CPU weight and memory ceiling its
  `[resources]` declares or their defaults, and `GOMEMLIMIT` in its
  environment set to the Go memory limit, in bytes. A core app's service
  also keeps 32 MiB that the kernel does not reclaim from it under pressure.
  The service delegates its control group only when `delegate = true`, and
  outlives the kernel killing one of its processes for want of memory only
  when `oom_policy = "continue"`. Stopping it stops every process the app
  started. It
  is wanted by `multi-user.target` and enabled, so the app starts at boot
  rather than at its first request.

Because the socket outlives the service, restarting an app refuses no
connection: requests that arrive while the old process drains and the new one
starts wait on the socket and are answered by the new process. So an
`install` over a running app that moves nothing, and `restart`, restart the
service and never the socket.
Only `uninstall`, `restore`, `retire`, `disable`, and an `install` that moves
data stop the socket, so that no request can start the service again while
they work or after.

Besides its environment file, an app keeps its files in two places.
`/opt/<app>/` holds what the file brought: `bin/`, `etc/`, and `share/`.
`/var/opt/ikigenba/<app>/`, its working directory, holds what the app writes
as it runs: `state/`, its data, and `cache/`, which it can lose. Every
install, at its `data` step, creates `/var/opt/ikigenba/` owned `root:root`
with mode `0755` if it is missing, and makes sure `/var/opt/ikigenba/<app>/`
exists, owned `ikigenba:ikigenba` with mode `0750`. It never creates `state/`
or `cache/`: the app creates them when it starts. A relative path in a
manifest resolves against the working directory, so `state/crm.db` is
`/var/opt/ikigenba/crm/state/crm.db`. Every app's working directory is a
sibling under `/var/opt/ikigenba/`, so a path one app gives into another's
data, such as `../repos/state/repos`, reaches it.

A host whose apps were installed before their data lived there holds `state/`
and `cache/` under `/opt/<app>/`. The `data` step moves each one that is
there to `/var/opt/ikigenba/<app>/` whole, its contents, ownership, and modes
unchanged, when the new place holds none. It moves nothing while the app
runs: when something has to move it stops the app's socket and service
first; the `unit` step brings the socket back, and the `service` step starts
the service after `litestream` has named the new path, as in a first
install. When nothing has to move the socket stays up. A `cache/` in both
places is disposable, so the one under `/opt/<app>/` is dropped, and
dropping it is not a move. A `state/` in both places is refused before
anything changes, and the operator decides by hand which to keep.

A service is any `/opt/<name>/` holding an `etc/`, or any
`/var/opt/ikigenba/<name>/` holding a `state/`. A `/opt/<name>/state/` left
by itself is not a service.

An app owns its schema, and the file carries what that takes: at every start
the app creates the database its manifest declares if none is there, runs its
migrations forward. A migration carries schema and never rows, so a database
the app creates starts empty; no app seeds itself. opsctl never runs a
migration. What the host promises is the order: a restore lands `state/`
and the database before the app's unit starts again, and an install
over a running app leaves `state/` alone, so an app that is restored or
upgraded migrates forward over real data. Data reaches a fresh space only by a
restore. A space that backs nothing up holds only what has been typed into
its apps since they were installed or last restored.

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
devctl. The app name and the secrets it needs are read from
etc/manifest.toml inside it; the secret values are read from the parameter
/<host.name>/<app>. They and the manifest's [env] settings are written to
/etc/opt/ikigenba/<app>/env, the service's environment file, which every
install rewrites whole; an old /opt/<app>/etc/env goes with the old etc/.

The app's data lives in its working directory, /var/opt/ikigenba/<app>/, which
is created if missing; nothing in it is touched, so installing over a running
app keeps its data. A state/ or cache/ still under /opt/<app>/ is moved there,
with the app stopped; a cache/ in both places drops the old one, and a state/
in both places fails the install before anything changes. Safe to re-run. An
app that is disabled stays disabled: its files and units are replaced, but
neither unit is enabled or started until 'opsctl enable'.

The nginx configuration, /var/lib/ikigenba/services.json, and
/etc/litestream.yml are regenerated from every app on the host, so an app that
ships share/icon.svg appears in the service launcher and an app that declares
a [database] is replicated from the moment it is installed. litestream.service
is restarted only when its configuration changed.

The manifest's [resources] table, if any, places the service and bounds the
app and every process it starts together:
  slice            "core" or "apps" (default "apps"): ikigenba-core.slice or
                   ikigenba-apps.slice, which 'opsctl init' writes
  memory_max       memory ceiling, bytes with an optional K, M or G (default
                   128M); no more than ikigenba-apps.slice's MemoryMax, or
                   ikigenba.slice's for a core app
  go_memory_limit  GOMEMLIMIT for the app, in the same form (default 75% of
                   memory_max, rounded down); no more than memory_max
  cpu_weight       CPU share within the slice, 1-10000 (default 100)
  delegate         true to delegate the service's control group (used by
                   scripts)
  oom_policy       "continue" to keep the service running when the kernel
                   kills one of its processes (used by repos)
A core app also keeps 32M of memory under pressure. When the memory_max values
in ikigenba-apps.slice, or in ikigenba.slice with nginx's 128M, add up to more
than twice the slice's ceiling, the unit line says so; the install goes on.

Configuration keys:
  aws.region          the region this host's parameters and artifacts live in
  host.name           the fully-qualified name this host answers at
  apps.drain_seconds  how long the app may drain when stopped (default 5)
  apps.stop_seconds   how long systemd waits for it to stop (default 10)
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
adds the socket's state and the database journal mode in its five-column
report. The `fetch` step is the host reading the object with its own role:
the file never travels over the ssh connection. The `data` step makes
`/var/opt/ikigenba/crm/` ready as the app's working directory; on a host that
has never had `crm` there is nothing to move. The `unit` step writes both
units and brings the socket up, so the socket is listening before the `nginx`
step routes the app's name to it. The `services` step adds the app's entry to
the services file; because the file ships an icon, that entry puts the app in
the service launcher. The `litestream` step names the database this manifest declares, now in
`/etc/litestream.yml`; it comes before `service` so that replication is in
place before the app writes its first row.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.1.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.1.0.tar.xz, 8.4 MiB)
file: ok (crm)
secrets: ok (3 keys)
data: ok (/var/opt/ikigenba/crm)
unpack: ok (/opt/crm)
unit: ok (ikigenba-crm.socket, ikigenba-crm.service)
nginx: ok (crm.sbx.ikigenba.dev)
services: ok (crm added)
litestream: ok (state/crm.db)
service: ok (crm v0.1.0 active)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `host.name` and `aws.region` are set, and `init` reported the host ready.
- `apps.drain_seconds` and `apps.stop_seconds` are unset.
- The object holds `bin/crm`, `etc/manifest.toml`, and `share/icon.svg`, an
  SVG image of at most 64 KiB, and the host's role can read it. The manifest
  is the one the group shows.
- `/<host.name>/crm` holds every name the manifest's `secrets`
  array lists.
- `crm` has never been installed on this host.

Postconditions:

- `/opt/crm/bin/`, `/opt/crm/etc/`, and `/opt/crm/share/` are the file's,
  replacing whatever was there, and `/opt/crm/` holds nothing else.
  `/opt/crm/etc/` holds no `env`.
- `/var/opt/ikigenba/` exists, owned `root:root` with mode `0755`, and
  `/var/opt/ikigenba/crm/` exists, owned `ikigenba:ikigenba` with mode
  `0750`. opsctl wrote nothing inside it: no `state/` or `cache/` was created
  by the install, and whatever is there now the app created when it started.
- `/etc/opt/`, `/etc/opt/ikigenba/`, and `/etc/opt/ikigenba/crm/` exist,
  each owned `root:root` with mode `0755`, and `/etc/opt/ikigenba/crm/` holds
  `env` and nothing else.
- `/etc/opt/ikigenba/crm/env` is owned `root:root` with mode `0600` and
  holds one `NAME=value` line per secret the manifest names, every setting in its `[env]` table,
  `DRAIN_SECONDS=5`, the value of `apps.drain_seconds` or its default, and
  `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json`, which every app gets,
  launcher service or not. It holds no `PORT`. Keys in the parameter that the manifest no longer names are
  not written. The values are never printed and never appear on a command
  line.
- `/etc/systemd/system/ikigenba-crm.socket` and
  `/etc/systemd/system/ikigenba-crm.service` are the two units the group
  describes; the service's working directory is `/var/opt/ikigenba/crm`,
  its environment file is `/etc/opt/ikigenba/crm/env`, and
  its stop timeout is `10` seconds, the value of
  `apps.stop_seconds` or its default. The manifest has no `[resources]`, so the
  service runs with the defaults: `systemctl show ikigenba-crm.service -p
  Slice -p CPUWeight -p MemoryMax -p MemoryLow -p Environment` prints
  `Slice=ikigenba-apps.slice`, `CPUWeight=100`, `MemoryMax=134217728`,
  `MemoryLow=0`, and `Environment=GOMEMLIMIT=100663296`. systemd has been
  reloaded and both units are enabled. The socket
  is listening at `/run/ikigenba/crm.sock` — since before nginx was reloaded,
  so nginx never routed to a socket that was not there — and the service was
  started explicitly rather than left for the first request, so a release
  that cannot start fails this command.
- `/etc/nginx/conf.d/ikigenba.conf` has been regenerated and nginx reloaded,
  so `https://crm.sbx.ikigenba.dev` reaches `crm` through
  `/run/ikigenba/crm.sock`.
- Were the installed app named `auth`, the same regeneration would wire every
  other app's server block to the authenticator's `/check` (`S5-nginx.md`), so
  each begins requiring a valid session, except that an app whose manifest
  sets `guests = true` still lets a visitor with none reach its pages. The
  `nginx:` line still reports only
  the installed app's own name.
- `/var/lib/ikigenba/services.json` has been rewritten and now lists `crm`,
  with the URL `https://crm.sbx.ikigenba.dev`, the description `Customers,
  contacts, and deals`, the socket `/run/ikigenba/crm.sock`, enabled, `mcp`
  true, and the contents of `/opt/crm/share/icon.svg`.
- `/etc/litestream.yml` has been regenerated from every manifest under `/opt`
  and now names `/var/opt/ikigenba/crm/state/crm.db`, replicating to
  `<backup.s3_uri>crm/`.
  Because the file changed, `litestream.service` was restarted; it is running.
  Every other declared database on the host paused for the restart and is
  replicating again, the same window `S8-backup.md` accepts for a restore.
- The service is `active`, and the binary reports `v0.1.0`.
- No other app on the host has changed.

## An agent deploys a new version over a running app

The same command. Its whole point is that the app's data outlives it: the
tarball carries no `state/`, and opsctl writes none. The new manifest declares
the same database, so the regenerated `/etc/litestream.yml` is byte for byte
the old one and litestream is left alone: an upgrade of one app does not
interrupt the replication of any.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.2.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.2.0.tar.xz, 8.4 MiB)
file: ok (crm)
secrets: ok (4 keys)
data: ok (/var/opt/ikigenba/crm)
unpack: ok (/opt/crm)
unit: ok (ikigenba-crm.socket, ikigenba-crm.service)
nginx: ok (crm.sbx.ikigenba.dev)
services: ok (unchanged)
litestream: ok (unchanged)
service: ok (crm v0.2.0 active)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `crm` is installed, its socket is listening, and its service is `active`.
- `crm`'s `state/` and `cache/` are under `/var/opt/ikigenba/crm/`, and
  `/opt/crm/` holds neither.
- The new manifest names a fourth secret, and the parameter holds it. Its
  `description` and `mcp` are the installed manifest's.
- The new file's `share/icon.svg` is byte for byte the installed one.

Postconditions:

- Everything the first install's postconditions say, and:
  `/var/opt/ikigenba/crm/state/` and `/var/opt/ikigenba/crm/cache/` are byte
  for byte as they were, nothing was moved, the `data` step did not stop the
  app, the service was restarted rather than started,
  and `status` now shows `crm v0.2.0 active active wal`.
- The socket was not stopped or restarted: it listened throughout, so a
  request that arrived while `v0.1.0` drained and `v0.2.0` started waited on
  it and was answered by `v0.2.0`. No request was refused.
- `/etc/litestream.yml` is byte for byte as it was and `litestream.service`
  was not restarted, so its replication of `crm.db` was never interrupted. A
  manifest that changed its `[database]` path would have changed the file, and
  the line would have named the new path and the service been restarted.
- `/var/lib/ikigenba/services.json` is byte for byte as it was. A new file
  whose icon, `description`, or `mcp` differed, or that gained or dropped an
  icon, would have changed `crm`'s entry, and the line would have read
  `services: ok (crm updated)`.
- Installing the same file again produces the same ten lines and exit 0.

## An agent deploys a new version over a disabled app

An operator disabled `crm` with `opsctl disable crm`, and a deploy arrives
anyway. Disabling is the operator's decision and a deploy does not undo it:
the new release is put in place, both units are rewritten, and neither is
enabled or started. The `nginx` line and the last line say the app is
disabled rather than reporting a state the command did not produce.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.2.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.2.0.tar.xz, 8.4 MiB)
file: ok (crm)
secrets: ok (4 keys)
data: ok (/var/opt/ikigenba/crm)
unpack: ok (/opt/crm)
unit: ok (ikigenba-crm.socket, ikigenba-crm.service)
nginx: ok (crm.sbx.ikigenba.dev disabled)
services: ok (unchanged)
litestream: ok (unchanged)
service: ok (crm v0.2.0 disabled)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `crm` is installed and disabled: both its units are disabled and inactive.
- `crm`'s `state/` and `cache/` are under `/var/opt/ikigenba/crm/`, and
  `/opt/crm/` holds neither.
- The new manifest names a fourth secret, and the parameter holds it. Its
  `description` and `mcp` are the installed manifest's.
- The new file's `share/icon.svg` is byte for byte the installed one.

Postconditions:

- `/opt/crm/bin/`, `/opt/crm/etc/`, and `/opt/crm/share/` are the new file's
  and `/etc/opt/ikigenba/crm/env` is rewritten as for any install;
  `/var/opt/ikigenba/crm/state/` is byte for byte as it was.
- Both unit files are rewritten and systemd has been reloaded. Both units are
  still disabled and inactive; neither was started, and
  `/run/ikigenba/crm.sock` does not exist.
- `/etc/nginx/conf.d/ikigenba.conf` has been regenerated and nginx reloaded;
  `crm`'s names still answer `503`.
- `/var/lib/ikigenba/services.json` is byte for byte as it was: it still
  lists `crm` as not enabled.
- The version in the last line is what the new binary answers to
  `--version`. Whether the release starts is not known until `opsctl enable
  crm` starts it.

## An agent installs an app whose data has not moved yet

The host's apps were installed before their data lived under
`/var/opt/ikigenba/`, so `crm`'s `state/` and `cache/` are still under
`/opt/crm/`. A deploy is what brings them across: the `data` step stops the
running socket and service and moves both directories. The `unit` step
brings the socket back, and the `service` step starts the service only after
`litestream` names the new path, so replication is in place before the app
writes; the rest goes on as it does over a running app. The `data` line
names what it moved. Because the database's path changes, the regenerated
`/etc/litestream.yml` changes, and the `litestream` line names the database.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.2.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.2.0.tar.xz, 8.4 MiB)
file: ok (crm)
secrets: ok (4 keys)
data: ok (/var/opt/ikigenba/crm; moved /opt/crm/state, /opt/crm/cache)
unpack: ok (/opt/crm)
unit: ok (ikigenba-crm.socket, ikigenba-crm.service)
nginx: ok (crm.sbx.ikigenba.dev)
services: ok (unchanged)
litestream: ok (state/crm.db)
service: ok (crm v0.2.0 active)
```

Exits 0. The lines are on stdout; stderr is empty. Had only `state/` been
under `/opt/crm/`, the line would read `data: ok (/var/opt/ikigenba/crm;
moved /opt/crm/state)`.

Preconditions:

- `crm` `v0.1.0` is installed, its socket is listening, and its service is
  `active` with `/opt/crm` as its working directory.
- `/opt/crm/` holds `bin/`, `etc/`, `share/`, `state/`, and `cache/`.
  `/var/opt/ikigenba/crm/` does not exist, and neither may
  `/var/opt/ikigenba/`.
- The service's environment file is `/opt/crm/etc/env`, and
  `/etc/opt/ikigenba/crm/` does not exist.
- `/etc/litestream.yml` names `/opt/crm/state/crm.db`.
- The new manifest names a fourth secret, and the parameter holds it. Its
  `description` and `mcp` are the installed manifest's, and the new file's
  `share/icon.svg` is byte for byte the installed one.

Postconditions:

- `/var/opt/ikigenba/crm/state/` and `/var/opt/ikigenba/crm/cache/` are the
  directories that were under `/opt/crm/`, byte for byte, with every file's
  ownership and mode unchanged. `/opt/crm/` holds no `state/` or `cache/`;
  it holds `bin/`, `etc/`, and `share/`, which are the new file's, and
  `/opt/crm/etc/env` went with the old `etc/`.
- `/etc/opt/ikigenba/crm/env` is written as for any install, and the
  rewritten service names it as its environment file.
- `/var/opt/ikigenba/` is owned `root:root` with mode `0755`, and
  `/var/opt/ikigenba/crm/` is owned `ikigenba:ikigenba` with mode `0750`.
- The socket and the service were both stopped before anything moved, so no
  request could start the service during the move. The `unit` step brought
  the socket back, and the `service` step started the service after the
  `litestream` step had named `/var/opt/ikigenba/crm/state/crm.db`, so
  replication was in place before the app wrote; no write landed under
  `/opt/crm/` after the move. This is the one install over a running app
  that stops the socket.
  `systemctl show ikigenba-crm.service -p WorkingDirectory` prints
  `WorkingDirectory=/var/opt/ikigenba/crm`.
- `/etc/litestream.yml` names `/var/opt/ikigenba/crm/state/crm.db`,
  replicating to `<backup.s3_uri>crm/`, and no longer names
  `/opt/crm/state/crm.db`. Because the file changed, `litestream.service` was
  restarted; it is running.
- The running app uses the moved data: every row it held before the install
  is there, and `status` shows `crm v0.2.0 active active wal`.
- Installing the same file again prints `data: ok (/var/opt/ikigenba/crm)`,
  moves nothing, and stops neither the socket nor the service.
- No other app on the host has changed.

## An agent installs an app whose environment file has not moved yet

`crm` was installed after its data moved under `/var/opt/ikigenba/` but
before its environment file left `/opt/crm/etc/`, so its unit still names
`/opt/crm/etc/env`. The environment file is generated, not data, so nothing
has to move and nothing stops the app early: the `unpack` step replaces
`/opt/crm/etc/` with the file's, the old `env` going with it, the new one is
written under `/etc/opt/ikigenba/crm/`, and the `unit` step rewrites the
service to name it. The output is the ten lines of any deploy over a running
app.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.2.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.2.0.tar.xz, 8.4 MiB)
file: ok (crm)
secrets: ok (4 keys)
data: ok (/var/opt/ikigenba/crm)
unpack: ok (/opt/crm)
unit: ok (ikigenba-crm.socket, ikigenba-crm.service)
nginx: ok (crm.sbx.ikigenba.dev)
services: ok (unchanged)
litestream: ok (unchanged)
service: ok (crm v0.2.0 active)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `crm` `v0.1.0` is installed, its socket is listening, and its service is
  `active` with `/var/opt/ikigenba/crm` as its working directory and
  `/opt/crm/etc/env` as its environment file.
- `crm`'s `state/` and `cache/` are under `/var/opt/ikigenba/crm/`, and
  `/opt/crm/` holds neither.
- `/etc/opt/ikigenba/crm/` does not exist, and neither may
  `/etc/opt/ikigenba/`.
- The new manifest and icon are as in "An agent deploys a new version over a
  running app".

Postconditions:

- `/opt/crm/` holds `bin/`, `etc/`, and `share/`, which are the new file's,
  and nothing else; `/opt/crm/etc/env` is gone.
- `/etc/opt/ikigenba/` and `/etc/opt/ikigenba/crm/` exist, owned
  `root:root` with mode `0755`, and `/etc/opt/ikigenba/crm/env` holds what
  any install writes, with mode `0600`.
- The rewritten service names `/etc/opt/ikigenba/crm/env` as its environment
  file, and the new process was started with it.
- The socket was not stopped or restarted, as in any deploy over a running
  app that moves nothing; the service was restarted.
- `/var/opt/ikigenba/crm/state/` and `/var/opt/ikigenba/crm/cache/` are byte
  for byte as they were, and `/etc/litestream.yml` is unchanged.
- No other app on the host has changed.

## An agent installs an app whose cache is in both places

A `cache/` holds nothing the app cannot rebuild, so when `/opt/crm/` and
`/var/opt/ikigenba/crm/` both hold one, the old one is dropped and the new
one kept. The `data` line says so.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.2.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.2.0.tar.xz, 8.4 MiB)
file: ok (crm)
secrets: ok (4 keys)
data: ok (/var/opt/ikigenba/crm; dropped /opt/crm/cache)
unpack: ok (/opt/crm)
unit: ok (ikigenba-crm.socket, ikigenba-crm.service)
nginx: ok (crm.sbx.ikigenba.dev)
services: ok (unchanged)
litestream: ok (unchanged)
service: ok (crm v0.2.0 active)
```

Exits 0. The lines are on stdout; stderr is empty. When the same run also
moves something, the moved list comes first: `data: ok
(/var/opt/ikigenba/crm; moved /opt/crm/state; dropped /opt/crm/cache)`.

Preconditions:

- `crm` is installed and running with `/var/opt/ikigenba/crm` as its working
  directory, and its `state/` is already under `/var/opt/ikigenba/crm/`.
- Both `/opt/crm/cache/` and `/var/opt/ikigenba/crm/cache/` exist, and
  `/opt/crm/state/` does not.
- The new manifest and icon are as in "An agent deploys a new version over a
  running app".

Postconditions:

- `/opt/crm/cache/` is gone. `/var/opt/ikigenba/crm/cache/` and
  `/var/opt/ikigenba/crm/state/` are byte for byte as they were.
- Nothing was moved, so the `data` step stopped neither the socket nor the
  app; the socket listened throughout. The `service`
  step restarted it, as in a deploy over a running app.
- `/opt/crm/` holds `bin/`, `etc/`, and `share/`, which are the new file's,
  and nothing else. The service runs `v0.2.0`, and `status` shows
  `crm v0.2.0 active active wal`.

## An agent installs an app whose state is in both places

Two `state/` directories are two versions of the app's data, and opsctl
cannot tell which is the one to keep. The `data` step checks this before it
stops or moves anything, so the refusal leaves the host as it was and the app
running as before. Which copy to keep is the operator's decision, made by
hand; the install can then be run again.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.2.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.2.0.tar.xz, 8.4 MiB)
file: ok (crm)
secrets: ok (4 keys)
data: failed: /opt/crm/state and /var/opt/ikigenba/crm/state both exist
opsctl: install failed
```

Exits 1. The step outcome lines are on stdout; the last line is on stderr.

Preconditions:

- `crm` `v0.1.0` is installed and its service is `active`.
- Both `/opt/crm/state/` and `/var/opt/ikigenba/crm/state/` exist.

Postconditions:

- Nothing has changed. Both `state/` directories, and any `cache/` in either
  place, are untouched; nothing was moved or dropped.
- The service was not stopped: `crm` keeps running `v0.1.0` as before.
  Nothing under `/opt/crm/` was unpacked, no unit was written, nginx was not
  reloaded, `/var/lib/ikigenba/services.json` was not rewritten, and
  `/etc/litestream.yml` was not regenerated.

## An agent installs the host's default app

The app whose manifest sets `default = true` answers at the host's own name as
well as its own, and the nginx step says so. It ships no icon, so its new
entry in the services file keeps it out of the launcher. It declares no
database, so the regenerated `/etc/litestream.yml` is what it was.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/dashboard-v0.0.9.tar.xz
```

Output:

```
fetch: ok (dashboard-v0.0.9.tar.xz, 5.2 MiB)
file: ok (dashboard, default)
secrets: ok (0 keys)
data: ok (/var/opt/ikigenba/dashboard)
unpack: ok (/opt/dashboard)
unit: ok (ikigenba-dashboard.socket, ikigenba-dashboard.service)
nginx: ok (dashboard.sbx.ikigenba.dev, sbx.ikigenba.dev)
services: ok (dashboard added)
litestream: ok (unchanged)
service: ok (dashboard v0.0.9 active)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `dashboard`'s manifest sets `default = true`, names no secrets, and carries
  no `description` or `mcp`.
- The file ships no `share/icon.svg`.
- `dashboard` has never been installed on this host, and no other installed
  app sets `default = true`.

Postconditions:

- `https://sbx.ikigenba.dev` and `https://dashboard.sbx.ikigenba.dev` both
  reach `dashboard` through `/run/ikigenba/dashboard.sock`; every other name
  under the host still answers 404.
- `/var/lib/ikigenba/services.json` has been rewritten and now lists
  `dashboard`, with the URL `https://dashboard.sbx.ikigenba.dev`, the
  description `""`, the socket `/run/ikigenba/dashboard.sock`, enabled, `mcp`
  false, and no icon: it is not in the launcher. Its
  `/etc/opt/ikigenba/dashboard/env` holds
  `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` like every app's.
- `/etc/litestream.yml` is byte for byte as it was and `litestream.service`
  was not restarted.

## An agent installs the apex app

`devctl apex set crm.sbx` named `crm` the apex app before `crm` was ever
deployed here, so `ikigenba.dev` has been answering 404 from the host's
catch-all. This deploy moves the name to `crm`'s block, and the nginx step
says so. `crm` is not the default app, so the space's own name is not in the
line.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.1.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.1.0.tar.xz, 8.4 MiB)
file: ok (crm)
secrets: ok (3 keys)
data: ok (/var/opt/ikigenba/crm)
unpack: ok (/opt/crm)
unit: ok (ikigenba-crm.socket, ikigenba-crm.service)
nginx: ok (crm.sbx.ikigenba.dev, ikigenba.dev)
services: ok (crm added)
litestream: ok (state/crm.db)
service: ok (crm v0.1.0 active)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and `host.apex` is `crm`.
- The host's certificate covers `ikigenba.dev` and the apex record points
  here: both are `devctl apex set`'s doing, before this deploy.
- `crm`'s manifest does not set `default = true`, and the file ships
  `share/icon.svg`.

Postconditions:

- Everything the first install's postconditions say, and
  `https://ikigenba.dev` reaches `crm` through `/run/ikigenba/crm.sock`.
- `crm`'s entry in `/var/lib/ikigenba/services.json` still has the URL
  `https://crm.sbx.ikigenba.dev`: the launcher names an app by its own name,
  never the apex.
- An app that is both default and apex reports all three names:
  `nginx: ok (crm.sbx.ikigenba.dev, sbx.ikigenba.dev, ikigenba.dev)`.
- Installing any other app on this host leaves the apex where it is; only
  `host.apex` names the apex app, and `install` never sets it.

## An agent installs an app that declares its resources

An app that does heavy work in child processes — `repos`, which runs `git`
for every clone and push — says in its manifest how much of the host it may
take, so a burst of that work slows the app down rather than the host. It
runs among the apps, with twice the default ceiling, and keeps its Go heap
well under it, because the `git` processes it starts share the same ceiling.
When one of them is killed for want of memory, the clone or push it served
fails and `repos` goes on serving the rest. The output is the same ten kinds
of line as any install; the resources are in the unit the `unit` step writes.

```toml
app = "repos"
description = "Git repositories for agents"
mcp = true
secrets = []

[database]
engine = "sqlite"
path = "state/repos.db"

[resources]
memory_max = "256M"
go_memory_limit = "128M"
oom_policy = "continue"
```

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/repos-v0.1.0.tar.xz
```

Output:

```
fetch: ok (repos-v0.1.0.tar.xz, 9.1 MiB)
file: ok (repos)
secrets: ok (0 keys)
data: ok (/var/opt/ikigenba/repos)
unpack: ok (/opt/repos)
unit: ok (ikigenba-repos.socket, ikigenba-repos.service)
nginx: ok (repos.sbx.ikigenba.dev)
services: ok (repos added)
litestream: ok (state/repos.db)
service: ok (repos v0.1.0 active)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev`, and `init` reported the host ready on a
  t3.small, so `ikigenba-apps.slice` has `MemoryMax=1024M` and
  `ikigenba.slice` `MemoryMax=1536M`.
- The file's manifest is the one above, and it ships no `share/icon.svg`.
- `repos` has never been installed on this host, and the apps already
  installed hold far less than twice either slice's ceiling.

Postconditions:

- Everything the first install's postconditions say, for `repos`.
- `systemctl show ikigenba-repos.service -p Slice -p CPUWeight -p MemoryMax
  -p MemoryLow -p OOMPolicy -p Environment` prints
  `Slice=ikigenba-apps.slice`, `CPUWeight=100`, `MemoryMax=268435456`,
  `MemoryLow=0`, `OOMPolicy=continue`, and `Environment=GOMEMLIMIT=134217728`.
  The service does not delegate its control group.
- A `git` the running app starts runs in `ikigenba-repos.service`'s control
  group: its CPU and memory count against the same limits as the app's own,
  and stopping or restarting the service stops it too.
- `ikigenba-repos.socket` carries no resource setting, and no other app's
  units changed.
- Installing a later release whose manifest drops `memory_max` and
  `go_memory_limit` rewrites the unit with the defaults, and the restarted
  service runs with `MemoryMax=134217728` and `GOMEMLIMIT=100663296`; one that
  changes a value runs with the new value. One that drops `oom_policy` gives a
  service the kernel's killing of one process stops whole, systemd's default.

## An agent installs a core app

`telemetry` keeps the suite's trail of events, and every other app posts to
it, so it runs in `ikigenba-core.slice` beside nginx and `auth`: the apps'
slice can run short of memory without starving it. It leaves its Go memory
limit to the default.

```toml
app = "telemetry"
description = "The suite's trail of events"
mcp = true
secrets = []

[database]
engine = "sqlite"
path = "state/telemetry.db"

[resources]
slice = "core"
memory_max = "256M"
```

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/telemetry-v0.1.0.tar.xz
```

Output:

```
fetch: ok (telemetry-v0.1.0.tar.xz, 8.8 MiB)
file: ok (telemetry)
secrets: ok (0 keys)
data: ok (/var/opt/ikigenba/telemetry)
unpack: ok (/opt/telemetry)
unit: ok (ikigenba-telemetry.socket, ikigenba-telemetry.service)
nginx: ok (telemetry.sbx.ikigenba.dev)
services: ok (telemetry added)
litestream: ok (state/telemetry.db)
service: ok (telemetry v0.1.0 active)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev`, and `init` reported the host ready on a
  t3.small.
- The file's manifest is the one above, and it ships no `share/icon.svg`.
- `telemetry` has never been installed on this host, and the apps already
  installed hold far less than twice `ikigenba.slice`'s ceiling.

Postconditions:

- Everything the first install's postconditions say, for `telemetry`.
- `systemctl show ikigenba-telemetry.service -p Slice -p CPUWeight -p
  MemoryMax -p MemoryLow -p Environment` prints
  `Slice=ikigenba-core.slice`, `CPUWeight=100`, `MemoryMax=268435456`,
  `MemoryLow=33554432`, and `Environment=GOMEMLIMIT=201326592`, three
  quarters of its `memory_max`.
- The service neither delegates its control group nor sets an OOM policy;
  no other app's units changed.

## An agent installs an app that arranges its own control groups

`scripts` runs each script in a control group of its own beneath its
service, so its service must hand it that part of the tree. Its scripts are
the largest thing on the host, so it takes most of the apps' slice, while
its own Go heap stays small.

```toml
app = "scripts"
description = "Runs Python scripts kept in repos"
mcp = true
secrets = []

[database]
engine = "sqlite"
path = "state/scripts.db"

[resources]
memory_max = "896M"
go_memory_limit = "128M"
delegate = true
```

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/scripts-v0.1.0.tar.xz
```

Output:

```
fetch: ok (scripts-v0.1.0.tar.xz, 7.6 MiB)
file: ok (scripts)
secrets: ok (0 keys)
data: ok (/var/opt/ikigenba/scripts)
unpack: ok (/opt/scripts)
unit: ok (ikigenba-scripts.socket, ikigenba-scripts.service)
nginx: ok (scripts.sbx.ikigenba.dev)
services: ok (scripts added)
litestream: ok (state/scripts.db)
service: ok (scripts v0.1.0 active)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev`, and `init` reported the host ready on a
  t3.small, so `ikigenba-apps.slice` has `MemoryMax=1024M`.
- The file's manifest is the one above, and it ships no `share/icon.svg`.
- `scripts` has never been installed on this host. `mcp`, `sites`, `dummy`,
  and `repos` are installed with the `memory_max` values `128M`, `128M`,
  `64M`, and `256M`, so with `scripts` the apps' slice adds up to 1472 MiB,
  less than twice its 1024 MiB, and the installed apps with nginx hold far
  less than twice `ikigenba.slice`'s ceiling.

Postconditions:

- Everything the first install's postconditions say, for `scripts`.
- `systemctl show ikigenba-scripts.service -p Slice -p MemoryMax -p Delegate
  -p Environment` prints `Slice=ikigenba-apps.slice`, `MemoryMax=939524096`,
  `Delegate=yes`, and `Environment=GOMEMLIMIT=134217728`.
- Every script the app runs, runs beneath `ikigenba-scripts.service`'s
  control group, so its memory counts against the app's 896 MiB and
  stopping the service stops it too.
- A later release whose manifest drops `delegate`, or sets it `false`, gives
  a service that does not delegate.

## An agent installs an app that oversubscribes its slice

The ceilings in a slice may add up to more than it has, because apps seldom
all reach theirs at once; past twice the slice's `MemoryMax` that bet is
worth knowing about. `install` still installs the app, and says so at the
end of its `unit` line.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.2.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.2.0.tar.xz, 8.5 MiB)
file: ok (crm)
secrets: ok (3 keys)
data: ok (/var/opt/ikigenba/crm)
unpack: ok (/opt/crm)
unit: ok (ikigenba-crm.socket, ikigenba-crm.service; warning: ikigenba-apps.slice memory_max adds up to 2112M, more than twice its MemoryMax 1024M)
nginx: ok (crm.sbx.ikigenba.dev)
services: ok (crm added)
litestream: ok (state/crm.db)
service: ok (crm v0.2.0 active)
```

Exits 0. The lines are on stdout; stderr is empty. When `ikigenba.slice` is
over too — every installed app's `memory_max` and nginx's 128M added
together, more than twice its `MemoryMax` — a second clause of the same form
follows the first: `; warning: ikigenba.slice memory_max adds up to 3200M,
more than twice its MemoryMax 1536M`. When only `ikigenba.slice` is over, its
clause is the only one.

Preconditions:

- `init` reported the host ready on a t3.small, so `ikigenba-apps.slice` has
  `MemoryMax=1024M` and `ikigenba.slice` `MemoryMax=1536M`.
- `auth` (`128M`) and `telemetry` (`256M`) are installed in the core slice,
  and `mcp` (`128M`), `sites` (`128M`), `dummy` (`64M`), `repos` (`256M`),
  and `scripts` (`896M`) in the apps slice. `dummy` is disabled.
- `crm` has never been installed. Its manifest is the group's with
  `[resources]` `memory_max = "640M"`.

Postconditions:

- Everything the first install's postconditions say, for `crm` at
  `v0.2.0`, with `MemoryMax=671088640` and `GOMEMLIMIT=503316480`.
- The apps slice adds up to 2112 MiB, the disabled `dummy` included. The
  whole suite, with nginx, adds up to 2624 MiB, under twice 1536 MiB, so
  there is one clause.
- Installing `crm` again with the same manifest prints the same clause: an
  app being reinstalled counts once, at its new `memory_max`. Installing it
  with `memory_max = "512M"` brings the apps slice to 1984 MiB and the `unit`
  line has no warning.

## An agent installs an app that serves guests

`sites` serves public static sites to anyone, so its manifest sets
`guests = true`. The output is the same ten kinds of line as any install;
`guests` is in the nginx configuration the `nginx` step regenerates, and in
no line. `sites` declares no database and ships no icon, so the regenerated
`/etc/litestream.yml` is what it was and its new entry in the services file
keeps it out of the launcher.

```toml
app = "sites"
guests = true
secrets = []
```

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/sites-v0.1.0.tar.xz
```

Output:

```
fetch: ok (sites-v0.1.0.tar.xz, 4.3 MiB)
file: ok (sites)
secrets: ok (0 keys)
data: ok (/var/opt/ikigenba/sites)
unpack: ok (/opt/sites)
unit: ok (ikigenba-sites.socket, ikigenba-sites.service)
nginx: ok (sites.sbx.ikigenba.dev)
services: ok (sites added)
litestream: ok (unchanged)
service: ok (sites v0.1.0 active)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev`, and `init` reported the host ready.
- `auth` is installed, so every other app's server block is wired to the
  authenticator (`S5-nginx.md`).
- The file's manifest is the one above, and it ships no `share/icon.svg`.
- `sites` has never been installed on this host.

Postconditions:

- Everything the first install's postconditions say, for `sites`, except
  that `/etc/litestream.yml` is byte for byte as it was and
  `litestream.service` was not restarted.
- `/etc/nginx/conf.d/ikigenba.conf` has been regenerated and nginx reloaded,
  so a visitor with no credential who asks for a page at
  `https://sites.sbx.ikigenba.dev` reaches `sites`, with no identity, rather
  than being sent to sign in. A request to its `/mcp`, its `/api`, or a git
  path is still challenged. Every other app's block is as it was, so its
  visitors are still sent to sign in.
- `/var/lib/ikigenba/services.json` has been rewritten and now lists `sites`,
  with the URL `https://sites.sbx.ikigenba.dev`, the description `""`, the
  socket `/run/ikigenba/sites.sock`, enabled, `mcp` false, and no icon. The
  entry says nothing about guests.
- Installing a later release whose manifest differs only in `guests` produces
  the same ten kinds of line with `services: ok (unchanged)`: only the nginx
  configuration changes. With `guests` false or absent, `sites` is behind
  sign-in like every other app.

## An operator installs a second default app

Only one app answers at the host's own name. `install` reads the manifest of
every app already under `/opt` and refuses before it writes anything.
Installing the default app over itself is the upgrade path, not a conflict;
only another app claiming the apex is.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/dashboard-v0.0.9.tar.xz
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

- Nothing has changed. Neither `/opt/dashboard/` nor
  `/var/opt/ikigenba/dashboard/` was created, no unit was written, nginx was
  not reloaded, and `/var/lib/ikigenba/services.json` was not rewritten.
- Making `dashboard` the default means installing `crm` again with
  `default = false` first.

## An agent installs an app whose secret has never been pushed

Nothing is written until everything the app needs is in hand, so a missing
secret refuses the install rather than half-doing it.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/gmail-v0.1.0.tar.xz
```

Output:

```
fetch: ok (gmail-v0.1.0.tar.xz, 6.1 MiB)
file: ok (gmail)
secrets: failed: gmail: no value for 'GMAIL_CLIENT_SECRET' in /sbx.ikigenba.dev/gmail
opsctl: install failed
```

Exits 1. The step outcome lines are on stdout; the last line is on stderr.

Preconditions:

- The manifest names `GMAIL_CLIENT_SECRET` and the parameter does not hold
  it, or the parameter does not exist at all.

Postconditions:

- Nothing has changed. Neither `/opt/gmail/` nor `/var/opt/ikigenba/gmail/`
  was created, no unit was written, nginx was not reloaded, and
  `/var/lib/ikigenba/services.json` was not rewritten.

## An operator installs a file that is not an app

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/notes.tar.xz
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

## An agent installs an app whose manifest names a port

An app never chooses where it listens: the host hands it its socket. A
manifest naming a `port` asks for something no host does, so install refuses
it outright rather than ignoring the key and installing an app that may try
to open a port of its own.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.1.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.1.0.tar.xz, 8.4 MiB)
file: failed: crm-v0.1.0.tar.xz: etc/manifest.toml: 'port' is not allowed; the host gives the app its socket
opsctl: install failed
```

Exits 2. The fetch and file outcome lines are on stdout; the last line is on
stderr.

Preconditions:

- `opsctl` is running as root.
- The file's `etc/manifest.toml` names `port = 3100`, whatever its value.

Postconditions:

- Nothing has changed. Neither `/opt/crm/` nor `/var/opt/ikigenba/crm/` was
  created or touched, no unit was written, nginx was not reloaded, and
  `/var/lib/ikigenba/services.json` was not rewritten. An installed `crm`
  keeps running the release it had.

## An agent installs an MCP app that does not say what it offers

An app whose tools belong in the suite's MCP catalog is listed there by its
`description`: it is what an agent reads to decide whether the app is worth
calling. A manifest that sets `mcp = true` and leaves that empty is refused
before anything is written, rather than installing an app no agent can
make sense of. With `mcp` false or absent, a missing or empty `description`
is accepted, and the app's entry carries the description `""`. A description
holding a control character, a tab say, gets the one-line refusal of the next
story instead, because the one-line rule is judged first.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.1.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.1.0.tar.xz, 8.4 MiB)
file: failed: crm-v0.1.0.tar.xz: etc/manifest.toml: 'mcp' is true but 'description' is empty; an MCP service must say what it offers
opsctl: install failed
```

Exits 2. The fetch and file outcome lines are on stdout; the last line is on
stderr. A `description` that is not a string, or an `mcp` that is not a
Boolean, is refused the way any other key of the wrong type is: `file: failed:
crm-v0.1.0.tar.xz: etc/manifest.toml: <decoder complaint>` on stdout and
`opsctl: install failed` on stderr, exit 2.

Preconditions:

- `opsctl` is running as root.
- The file's `etc/manifest.toml` sets `mcp = true` and has no `description`,
  or one that is empty or holds only whitespace.

Postconditions:

- Nothing has changed. Neither `/opt/crm/` nor `/var/opt/ikigenba/crm/` was
  created or touched, no unit was written, nginx was not reloaded, and
  `/var/lib/ikigenba/services.json` was not rewritten. An installed `crm`
  keeps running the release it had.

## An agent installs an app whose description is not one line

A description is one line of text wherever it is shown, so a line break or
any other control character in it is refused before anything is written.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.1.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.1.0.tar.xz, 8.4 MiB)
file: failed: crm-v0.1.0.tar.xz: etc/manifest.toml: 'description' must be one line of text
opsctl: install failed
```

Exits 2. The fetch and file outcome lines are on stdout; the last line is on
stderr.

Preconditions:

- `opsctl` is running as root.
- The file's `etc/manifest.toml` has a `description` holding a control
  character (U+0000–U+001F or U+007F): `"Customers\nand deals"`, say, whatever
  `mcp` is.

Postconditions:

- Nothing has changed. Neither `/opt/crm/` nor `/var/opt/ikigenba/crm/` was
  created or touched, no unit was written, nginx was not reloaded, and
  `/var/lib/ikigenba/services.json` was not rewritten. An installed `crm`
  keeps running the release it had.

## An agent installs an app whose guests flag is not a Boolean

Whether an app lets visitors in without signing in is not something install
guesses at. A `guests` that is not `true` or `false` is refused the way any
other key of the wrong type is, before anything is written.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/sites-v0.1.0.tar.xz
```

Output:

```
fetch: ok (sites-v0.1.0.tar.xz, 4.3 MiB)
file: failed: sites-v0.1.0.tar.xz: etc/manifest.toml: <decoder complaint>
opsctl: install failed
```

Exits 2. The fetch and file outcome lines are on stdout; the last line is on
stderr. `<decoder complaint>` is the TOML decoder's own words for the key and
the type it found, so it varies with the value.

Preconditions:

- `opsctl` is running as root.
- The file's `etc/manifest.toml` sets `guests = "yes"`, or `guests = 1`, or
  any other value that is not a Boolean.

Postconditions:

- Nothing has changed. Neither `/opt/sites/` nor `/var/opt/ikigenba/sites/`
  was created or touched, no unit was written, nginx was not reloaded, and
  `/var/lib/ikigenba/services.json` was not rewritten. An installed `sites`
  keeps running the release it had.

## An agent installs an app whose resources are not valid

A setting the host cannot apply is refused before anything is written,
rather than installing an app that runs without the bound or the place its
manifest asked for.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/repos-v0.1.0.tar.xz
```

Output:

```
fetch: ok (repos-v0.1.0.tar.xz, 9.1 MiB)
file: failed: repos-v0.1.0.tar.xz: etc/manifest.toml: 'resources.slice' must be "core" or "apps"
opsctl: install failed
```

Exits 2. The fetch and file outcome lines are on stdout; the last line is on
stderr. Each fault names its key in the same words:

- `memory_max` that is not a string of a positive whole number optionally
  followed by `K`, `M`, or `G` — `0`, `"512MB"`, `"1.5G"`, `"50%"`, or the
  integer `536870912` say: `'resources.memory_max' must be a whole number of
  bytes, optionally followed by K, M, or G`.
- `go_memory_limit` not in that form: `'resources.go_memory_limit' must be a
  whole number of bytes, optionally followed by K, M, or G`.
- A well-formed `go_memory_limit` larger than `memory_max`, or than the
  default `128M` when `memory_max` is absent — `"512M"` beside `memory_max =
  "256M"` say: `'resources.go_memory_limit' must not be larger than
  'resources.memory_max'`. Equal is allowed.
- `cpu_weight` outside 1 to 10000, or not a whole number — `0`, `20000`, or
  `"50"` say: `'resources.cpu_weight' must be a whole number from 1 to
  10000`.
- `delegate` that is not a Boolean — `"yes"` or `1` say:
  `'resources.delegate' must be true or false`.
- `oom_policy` other than `"continue"` — `"stop"` or `"kill"` say:
  `'resources.oom_policy' must be "continue"`.
- A key the table does not know, `io_weight` or `cpu_quota` say:
  `'resources.io_weight' is not allowed; the resources are slice, memory_max,
  go_memory_limit, cpu_weight, delegate, and oom_policy`. A manifest written
  for an older opsctl, with `io_weight` in it, is refused this way until the
  app drops the key.
- `resources` that is not a table is refused as any key of the wrong type
  is: `file: failed:
  repos-v0.1.0.tar.xz: etc/manifest.toml: <decoder complaint>`.

When the table holds more than one fault, only the first is reported, in the
order `slice`, `memory_max`, `go_memory_limit` (its form, then the
comparison), `cpu_weight`, `delegate`, `oom_policy`, then unknown keys in
byte order. Any app may set `delegate` and `oom_policy`; opsctl keeps no list
of which apps should. A `memory_max` that is well formed but more than its
slice allows is the next story's.

Preconditions:

- `opsctl` is running as root.
- The file's manifest is `repos`'s from "An agent installs an app that
  declares its resources" with `slice = "edge"`, or `"Core"`, or `""`, or
  `1`.

Postconditions:

- Nothing has changed. Neither `/opt/repos/` nor `/var/opt/ikigenba/repos/`
  was created or touched, no unit was written, nginx was not reloaded, and
  `/var/lib/ikigenba/services.json` was not rewritten. An installed `repos`
  keeps running the release it had, with the limits it had.

## An agent installs an app that asks for more memory than its slice holds

An app may not ask for more than the slice above it can ever give it, since
the kernel would stop it at the slice's ceiling first. The manifest is well
formed; it is refused on this host, whose slices `init` sized from its memory,
and would be accepted on a larger one.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/repos-v0.1.0.tar.xz
```

Output:

```
fetch: ok (repos-v0.1.0.tar.xz, 9.1 MiB)
file: failed: repos-v0.1.0.tar.xz: etc/manifest.toml: memory_max 2048M is more than ikigenba-apps.slice's MemoryMax 1024M
opsctl: install failed
```

Exits 2. The fetch and file outcome lines are on stdout; the last line is on
stderr. A core app is judged against `ikigenba.slice` the same way: `slice =
"core"` with `memory_max = "2G"` gives `memory_max 2048M is more than
ikigenba.slice's MemoryMax 1536M`. A `memory_max` equal to the ceiling is
accepted.

Preconditions:

- `init` reported the host ready on a t3.small, so `ikigenba-apps.slice` has
  `MemoryMax=1024M` and `ikigenba.slice` `MemoryMax=1536M`.
- The file's manifest is `repos`'s from "An agent installs an app that
  declares its resources" with `memory_max = "2G"`.

Postconditions:

- Nothing has changed. Neither `/opt/repos/` nor `/var/opt/ikigenba/repos/`
  was created or touched, no unit was written, nginx was not reloaded, and
  `/var/lib/ikigenba/services.json` was not rewritten. An installed `repos`
  keeps running the release it had, with the limits it had.

## An agent installs an app on a host whose slices are not there

`install` reads its ceilings from the slice units, so a host where they are
missing — one whose opsctl was upgraded and `init` not run since, or one
where a unit was removed by hand — cannot judge the app, and refuses it
before anything is written. The remedy is the host's, not the file's.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/repos-v0.1.0.tar.xz
```

Output:

```
fetch: ok (repos-v0.1.0.tar.xz, 9.1 MiB)
file: failed: /etc/systemd/system/ikigenba-apps.slice is missing; run 'opsctl init'
opsctl: install failed
```

Exits 1. The fetch and file outcome lines are on stdout; the last line is on
stderr. A slice unit that is there but carries no `MemoryMax` that can be
read, where it should carry one, gives `file: failed:
/etc/systemd/system/ikigenba-apps.slice has no MemoryMax; run 'opsctl init'`
the same way, exit 1. A core app needs `ikigenba.slice` and
`ikigenba-core.slice` instead, and an apps app `ikigenba.slice` as well as
its own.

Preconditions:

- `/etc/systemd/system/ikigenba.slice` exists with a `MemoryMax`, and
  `/etc/systemd/system/ikigenba-apps.slice` does not exist.
- The file's manifest is `repos`'s from "An agent installs an app that
  declares its resources", valid in every key.

Postconditions:

- Nothing has changed. Neither `/opt/repos/` nor `/var/opt/ikigenba/repos/`
  was created or touched, no unit was written, nginx was not reloaded, and
  `/var/lib/ikigenba/services.json` was not rewritten.
- Running `opsctl init` writes the slice, and the same install then
  succeeds.

## An agent installs an app whose database is not SQLite

SQLite is the only engine the platform keeps, and the only one litestream
replicates, so `engine` admits one value. A manifest naming any other is
refused before anything is written, rather than installing an app whose
database nothing would back up.

```toml
[database]
engine = "postgres"
path = "state/crm.db"
```

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.1.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.1.0.tar.xz, 8.4 MiB)
file: failed: crm-v0.1.0.tar.xz: etc/manifest.toml: 'database.engine' must be "sqlite"
opsctl: install failed
```

Exits 2. The fetch and file outcome lines are on stdout; the last line is on
stderr. A `[database]` with no `engine` fails the same way.

Preconditions:

- `opsctl` is running as root.
- The file's manifest is `crm`'s from the opening of this group with the
  `[database]` table above.

Postconditions:

- Nothing has changed. Neither `/opt/crm/` nor `/var/opt/ikigenba/crm/` was
  created or touched, no unit was written, nginx was not reloaded,
  `/var/lib/ikigenba/services.json` was not rewritten, and
  `/etc/litestream.yml` was not regenerated. An installed `crm` keeps running
  the release it had.

## An agent installs an app whose icon is not an SVG image

The icon goes into the services file as it is, and every app on the host
reads that file, so install checks it before anything is written. An SVG
image is a file that parses as XML with the root element `svg`.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.1.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.1.0.tar.xz, 8.4 MiB)
file: failed: share/icon.svg is not an SVG image
opsctl: install failed
```

Exits 2. The fetch and file outcome lines are on stdout; the last line is on
stderr.

Preconditions:

- `opsctl` is running as root.
- The file's `share/icon.svg` does not parse as XML with the root element
  `svg`: it is a PNG under that name, say.

Postconditions:

- Nothing has changed. Neither `/opt/crm/` nor `/var/opt/ikigenba/crm/` was
  created or touched, no unit was written, nginx was not reloaded, and
  `/var/lib/ikigenba/services.json` was not rewritten. An installed `crm`
  keeps running the release it had.

## An agent installs an app whose icon is larger than 64 KiB

Every app on the host reads the services file, which carries every icon in
it, so install holds each icon to 64 KiB.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.1.0.tar.xz
```

Output:

```
fetch: ok (crm-v0.1.0.tar.xz, 8.4 MiB)
file: failed: share/icon.svg is larger than 64 KiB
opsctl: install failed
```

Exits 2. The fetch and file outcome lines are on stdout; the last line is on
stderr.

Preconditions:

- `opsctl` is running as root.
- The file's `share/icon.svg` is an SVG image larger than 64 KiB (65,536
  bytes).

Postconditions:

- Nothing has changed. Neither `/opt/crm/` nor `/var/opt/ikigenba/crm/` was
  created or touched, no unit was written, nginx was not reloaded, and
  `/var/lib/ikigenba/services.json` was not rewritten. An installed `crm`
  keeps running the release it had.

## An agent installs an app while the timing settings are invalid

The two timing keys are read before anything is fetched, so a value `install`
could not write into the app refuses the whole command. It is the store that
is wrong, not the file, so no step line is printed.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/crm-v0.1.0.tar.xz
```

Output:

```
opsctl: apps.stop_seconds (5) is not greater than apps.drain_seconds (5)
```

Exits 1. The line is on stderr; stdout is empty. A value that is not a
positive whole number gives `opsctl: apps.drain_seconds is not a positive
whole number of seconds: '2.5'`, naming whichever key holds it, the same way.

Preconditions:

- `apps.drain_seconds` and `apps.stop_seconds` are both `5`.

Postconditions:

- Nothing has changed. No object was fetched, nothing under `/opt/` or
  `/var/opt/ikigenba/` was written, no unit was written, nginx was not
  reloaded, and `/var/lib/ikigenba/services.json` was not rewritten.

## An operator installs an app with a reserved name

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/host-v0.1.0.tar.xz
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

- Nothing has changed. Nothing under `/opt/` or `/var/opt/ikigenba/` was
  written.

## An agent installs an app whose service will not come up

The app ran, or failed to, and systemd knows why. opsctl reports the step that
failed and hands over what the host said, each line quoted with `> `, so the
developer reading devctl's relayed output has the journal in front of them.

Command:

```
$ sudo opsctl install s3://ikigenba.dev/sbx/deploy/gmail-v0.1.0.tar.xz
```

Output:

```
fetch: ok (gmail-v0.1.0.tar.xz, 6.1 MiB)
file: ok (gmail)
secrets: ok (2 keys)
data: ok (/var/opt/ikigenba/gmail)
unpack: ok (/opt/gmail)
unit: ok (ikigenba-gmail.socket, ikigenba-gmail.service)
nginx: ok (gmail.sbx.ikigenba.dev)
services: ok (gmail added)
litestream: ok (unchanged)
service: failed: gmail: service failed to start
opsctl: install failed

> ikigenba-gmail.service: Main process exited, code=exited, status=1/FAILURE
> gmail: open /opt/gmail/etc/labels.json: no such file or directory
```

Exits 1. The step outcome lines are on stdout; the command diagnostic and quoted
journal are on stderr.

Preconditions:

- `gmail` has never been installed on this host.
- `gmail`'s binary exits at start, before it tells systemd it is ready.
- `gmail`'s manifest declares no database, and its file ships no
  `share/icon.svg`.

Postconditions:

- `/var/opt/ikigenba/gmail/` exists, owned `ikigenba:ikigenba` with mode
  `0750`, the app is unpacked, both units are written and enabled, the socket
  is listening, and nginx routes its name. The service is `failed`. Nothing was
  rolled back: the host is left in the state an operator can inspect and fix,
  and `status` shows `gmail v0.1.0 failed active -`.
- `/var/lib/ikigenba/services.json` has been rewritten and lists `gmail` as
  enabled, with no icon: the entry is written before the service starts, and
  a failed service does not undo it any more than it undoes nginx.
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

Take APP off the host: stop ikigenba-APP.socket and ikigenba-APP.service,
socket first so no request starts the service again, disable both, remove
both units (which ends a disabled APP's disabled state: a later install is a
first install and comes up enabled), then remove /opt/APP/,
/etc/opt/ikigenba/APP/ with its environment file, and
/var/opt/ikigenba/APP/cache/. /var/opt/ikigenba/APP/state/ is kept untouched,
so APP is still a service the host backs up, and a later install lands over
its data, which every install leaves untouched. Removing state/ is a decision
made by hand, never here.

A state/ or cache/ still under /opt/APP/ is first moved to
/var/opt/ikigenba/APP/, once APP is stopped, as 'opsctl install' moves it; a
state/ in both places fails the uninstall with nothing removed.

The nginx configuration, /var/lib/ikigenba/services.json, and
/etc/litestream.yml are regenerated from every app left on the host, so APP's
name stops answering, APP leaves the service launcher, and a database APP
declared stops being replicated once litestream has shipped what it holds.

The parameter /<host.name>/APP is not touched: it is devctl's.

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
configuration stops naming it. That is the same sync `retire` relies on. The
socket stops before the service, so no request can start the service again
while it is taken down. A unit that was already inactive or failed is
disabled and removed all the same,
and the `stop` line names it `already inactive`. `crm`'s data is already
under `/var/opt/ikigenba/crm/`, so there is nothing to move and no `data`
line.

Command:

```
$ sudo opsctl uninstall crm
```

Output:

```
stop: ok (ikigenba-crm.socket, ikigenba-crm.service stopped, disabled)
unit: ok (removed ikigenba-crm.socket, ikigenba-crm.service)
files: ok (removed /opt/crm, /etc/opt/ikigenba/crm, /var/opt/ikigenba/crm/cache; kept /var/opt/ikigenba/crm/state)
nginx: ok (crm.sbx.ikigenba.dev removed)
services: ok (crm removed)
litestream: ok (state/crm.db removed)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `host.name` is set.
- `crm` is installed, its socket is listening, and its service is `active`.
  Its manifest declares a
  `[database]` at `state/crm.db`, and `litestream.service` is replicating it.
- `crm`'s `state/` and `cache/` are under `/var/opt/ikigenba/crm/`, and
  `/opt/crm/` holds neither.
- `/var/lib/ikigenba/services.json` lists `crm`, as it lists every installed
  app.

Postconditions:

- `ikigenba-crm.socket` and `ikigenba-crm.service` are inactive and
  disabled, `/run/ikigenba/crm.sock` is gone,
  `/etc/systemd/system/ikigenba-crm.socket` and
  `/etc/systemd/system/ikigenba-crm.service` are gone, and systemd has been
  reloaded.
- `/opt/crm/` is gone: `bin/`, `etc/`, and `share/` with it.
  `/etc/opt/ikigenba/crm/` is gone with its `env` file;
  `/etc/opt/ikigenba/` stays. `/var/opt/ikigenba/crm/` holds `state/` and
  nothing else; its `cache/`
  is gone. Nothing under `state/` was read or written: the database, its
  `-wal` and `-shm`, and litestream's metadata directory are as the service
  left them.
- `/etc/nginx/conf.d/ikigenba.conf` has been regenerated and nginx reloaded:
  `crm.sbx.ikigenba.dev` answers 404 under the host's wildcard block. Had `crm`
  been the default app, the line would have read `crm.sbx.ikigenba.dev,
  sbx.ikigenba.dev removed` and the space's name would answer 404 again. Had
  it been the apex app, the line would have read `crm.sbx.ikigenba.dev,
  ikigenba.dev removed`: `host.apex` is not touched, so `ikigenba.dev` moves
  to the 404 block and answers from there until `crm` is installed again.
- Were the uninstalled app named `auth`, the same regeneration would strip the
  authenticator wiring from every other app's server block (`S5-nginx.md`), so
  each returns to fail-open. The `nginx:` line still reports only the
  uninstalled app's own name.
- `/var/lib/ikigenba/services.json` has been rewritten and no longer lists
  `crm`.
- `/etc/litestream.yml` has been regenerated from the manifests left under
  `/opt` and no longer names `/var/opt/ikigenba/crm/state/crm.db`. Because
  the file changed, `litestream.service` was restarted. It was stopped after the
  service, so no writer was open and its shutdown sync shipped every frame it
  held: the replica under `<backup.s3_uri>crm/` holds `crm.db` as of the last
  committed transaction. Every other declared database paused for the restart
  and is replicating again.
- `status` shows `crm - - - -`: a service with a
  `/var/opt/ikigenba/crm/state/` and no manifest.
- Had `crm` been disabled, uninstalling it would work the same way, with the
  `stop` line naming both units `already inactive`. Removing the units ends
  the disabled state, which nothing else records: installing `crm` again is a
  first install, and it comes up enabled and running. `opsctl backup` now tars the whole
  of `state/`, the quiet database included, because no manifest declares it.
- `/<host.name>/crm` and the object under `<backup.s3_uri>deploy/`
  are untouched. Installing `crm` again lands over
  `/var/opt/ikigenba/crm/state/`, which it leaves alone, as every install
  does.
- No other app on the host has changed.

## An operator uninstalls the host's default app

The app declared no database, so the regenerated `/etc/litestream.yml` is what
it was and litestream is left alone. The nginx line names both names the app
answered at. The app shipped no icon, so it was never in the launcher, but it
had an entry in the services file all the same, and that entry goes.

Command:

```
$ sudo opsctl uninstall dashboard
```

Output:

```
stop: ok (ikigenba-dashboard.socket, ikigenba-dashboard.service stopped, disabled)
unit: ok (removed ikigenba-dashboard.socket, ikigenba-dashboard.service)
files: ok (removed /opt/dashboard, /etc/opt/ikigenba/dashboard, /var/opt/ikigenba/dashboard/cache; kept /var/opt/ikigenba/dashboard/state)
nginx: ok (dashboard.sbx.ikigenba.dev, sbx.ikigenba.dev removed)
services: ok (dashboard removed)
litestream: ok (unchanged)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `dashboard` is installed, its manifest sets `default = true`, and it
  declares no database.
- `dashboard` shipped no `share/icon.svg`; `/var/lib/ikigenba/services.json`
  lists it with no icon.

Postconditions:

- `https://sbx.ikigenba.dev` and `https://dashboard.sbx.ikigenba.dev` both
  answer 404. The host has no default app until an install brings one.
- `/var/lib/ikigenba/services.json` has been rewritten and no longer lists
  `dashboard`.
- `/etc/litestream.yml` is byte for byte as it was and `litestream.service`
  was not restarted.
- `/opt/dashboard/` and `/etc/opt/ikigenba/dashboard/` are gone, and
  `/var/opt/ikigenba/dashboard/` holds
  `state/` and nothing else.

## An operator uninstalls an app whose data has not moved yet

`crm` was installed before its data lived under `/var/opt/ikigenba/`, and no
install has moved it since. Uninstall leaves the data where every later
command looks for it: once the app is stopped, and before anything is
removed, it moves `state/` and `cache/` across as `install` does, and says so
on a `data` line. Its environment file is still `/opt/crm/etc/env` and
goes with `/opt/crm/`; there is no `/etc/opt/ikigenba/crm/`, so the `files`
line does not name one. The rest is the ordinary uninstall.

Command:

```
$ sudo opsctl uninstall crm
```

Output:

```
stop: ok (ikigenba-crm.socket, ikigenba-crm.service stopped, disabled)
data: ok (/var/opt/ikigenba/crm; moved /opt/crm/state, /opt/crm/cache)
unit: ok (removed ikigenba-crm.socket, ikigenba-crm.service)
files: ok (removed /opt/crm, /var/opt/ikigenba/crm/cache; kept /var/opt/ikigenba/crm/state)
nginx: ok (crm.sbx.ikigenba.dev removed)
services: ok (crm removed)
litestream: ok (state/crm.db removed)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `host.name` is set.
- `crm` is installed and its service is `active`. `/opt/crm/` holds `bin/`,
  `etc/`, `share/`, `state/`, and `cache/`, and `/var/opt/ikigenba/crm/`
  does not exist. Its manifest declares a `[database]` at `state/crm.db`.
- The service's environment file is `/opt/crm/etc/env`, and
  `/etc/opt/ikigenba/crm/` does not exist.

Postconditions:

- `/opt/crm/` is gone. `/var/opt/ikigenba/crm/` exists, owned
  `ikigenba:ikigenba` with mode `0750`, and holds `state/` and nothing else:
  the directory that was `/opt/crm/state/`, byte for byte, with every file's
  ownership and mode unchanged. The moved `cache/` is gone.
- The units, nginx, and `/var/lib/ikigenba/services.json` are as "An agent
  uninstalls an app" leaves them. `/etc/litestream.yml` names neither
  `/opt/crm/state/crm.db` nor `/var/opt/ikigenba/crm/state/crm.db`, and
  `status` shows `crm - - - -`.
- Installing `crm` again lands over `/var/opt/ikigenba/crm/state/`, with
  nothing left to move.

## An operator uninstalls an app whose state is in both places

The same refusal `install` makes, met after the stop: uninstall cannot tell
which `state/` holds the app's data, so it removes nothing and leaves the
choice to the operator.

Command:

```
$ sudo opsctl uninstall crm
```

Output:

```
stop: ok (ikigenba-crm.socket, ikigenba-crm.service stopped, disabled)
data: failed: /opt/crm/state and /var/opt/ikigenba/crm/state both exist
opsctl: uninstall failed
```

Exits 1. The step outcome lines are on stdout; the last line is on stderr.

Preconditions:

- `crm` is installed and its service is `active`.
- Both `/opt/crm/state/` and `/var/opt/ikigenba/crm/state/` exist.

Postconditions:

- The stop step ran: `ikigenba-crm.socket` and `ikigenba-crm.service` are
  inactive and disabled, and `/run/ikigenba/crm.sock` is gone.
- Nothing was removed. Both unit files are still in place, both `state/`
  directories and any `cache/` in either place are untouched, and
  `/opt/crm/bin/`, `etc/`, and `share/` are as they were. nginx was not
  reloaded, `/var/lib/ikigenba/services.json` was not rewritten, and
  `/etc/litestream.yml` was not regenerated.
- Once the operator has settled by hand which `state/` to keep and no longer
  has two, running the uninstall again completes it, its `stop` line naming
  both units `already inactive`.

## An operator uninstalls an app that is not installed

A `/var/opt/ikigenba/gmail/` with a `state/` and no `/opt/gmail/` is a
service, and the host backs it up, but there is nothing installed to take
off. Nothing is a stronger case of
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
A name that is no service at all, with neither an `/opt/gmail/` holding an
`etc/` nor a `/var/opt/ikigenba/gmail/` holding a `state/`, gives
`stop: failed: no service 'gmail'` on stdout and the same command diagnostic,
also exit 1.

Preconditions:

- `/var/opt/ikigenba/gmail/` holds a `state/`, `/opt/gmail/` does not
  exist, and there is no `ikigenba-gmail.socket` or
  `ikigenba-gmail.service`; or `gmail` is no service at all.

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
'opsctl install' does. The socket is never restarted: it keeps listening, so
requests that arrive during the restart wait and are answered by the new
process. Nothing on disk changes: the binary, the environment file, and the
units are what the last install wrote, so a secret pushed since then is not
picked up here, nor a timing setting changed since then ('opsctl init'
applies those). A service that is inactive or failed is started, and so is
its socket if it was stopped. A disabled app is not started: it stays
disabled until 'opsctl enable'.
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

- `crm` is installed and enabled. Its service may be `active`, `inactive`, or
  `failed`, and its unit names `/etc/opt/ikigenba/crm/env` as its
  environment file.

Postconditions:

- `ikigenba-crm.service` is `active` and its main process is a new one.
- `ikigenba-crm.socket` is listening and was not restarted: a request that
  arrived during the restart waited on it and was answered by the new
  process. Had the socket been stopped, starting the service started it too.
- Nothing under `/opt/crm/` or `/etc/` was written. The environment systemd
  gave the new process is the file its unit names,
  `/etc/opt/ikigenba/crm/env`, as it was last written.
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
> crm: open /var/opt/ikigenba/crm/state/crm.db: permission denied
```

Exits 1. The service outcome is on stdout; the command diagnostic and quoted
journal are on stderr.

Preconditions:

- `crm` is installed and its binary exits at start.

Postconditions:

- The service is `failed`, and `status` shows `crm v0.1.0 failed active wal`.
  The socket is still listening. Nothing
  on disk changed and nothing was rolled back: the host is left where an
  operator can look at it.

## A developer restarts a disabled app

A disabled app stays disabled through every operation but `opsctl enable`, so
`restart` starts nothing. The line reports the app as it is, the way `install`
reports a disabled app, and the command succeeds: the host is in the state
the operator chose.

Command:

```
$ sudo opsctl restart crm
```

Output:

```
service: ok (crm v0.1.0 disabled)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `crm` is installed and disabled: both its units are disabled and inactive.

Postconditions:

- Nothing has changed. Neither unit was started or enabled, and `crm`'s
  names still answer `503`.

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
A name that is no service at all, with neither an `/opt/gmail/` holding an
`etc/` nor a `/var/opt/ikigenba/gmail/` holding a `state/`, gives
`service: failed: no service 'gmail'` on stdout and the same command diagnostic,
also exit 1. With no operand the diagnostic is `opsctl: restart needs APP`,
with more than one `opsctl: restart takes one APP`, each followed by the usage
hint and exit 2; these grammar failures have no service outcome and empty stdout.

Preconditions:

- `/var/opt/ikigenba/gmail/` holds a `state/`, `/opt/gmail/` does not
  exist, and there is no `ikigenba-gmail.socket` or
  `ikigenba-gmail.service`; or `gmail` is no service at all.

Postconditions:

- Nothing has changed.

## An operator asks what `disable` and `enable` can do

An installed app starts at boot and answers whenever its socket is reached.
Disabling it keeps it installed — its files, its units, and its data stay —
while neither of its units starts, at boot or on a request, until it is
enabled again.

Command:

```
$ opsctl disable --help
```

```
$ opsctl disable -h
```

Output:

```
Usage: opsctl disable APP

Stop ikigenba-APP.socket and ikigenba-APP.service, socket first so no request
starts the service again, and disable both, so neither starts at boot or on a
request. The nginx configuration and /var/lib/ikigenba/services.json are then
regenerated, so APP's names answer 503 and the service launcher shows APP
disabled until it is enabled. Nothing on disk under /opt/APP/ or
/var/opt/ikigenba/APP/ changes.
'opsctl enable APP' undoes it.

auth, the authenticator every other app is checked against, is never
disabled.

Configuration keys:
  host.name  the fully-qualified name this host answers at
  host.apex  the app that answers at the parent of host.name; unset means none
```

Command:

```
$ opsctl enable --help
```

```
$ opsctl enable -h
```

Output:

```
Usage: opsctl enable APP

Enable ikigenba-APP.socket and ikigenba-APP.service and start the socket,
regenerate the nginx configuration and /var/lib/ikigenba/services.json so
APP's names reach it again and the service launcher shows it enabled, then
start the service and report it as the last line of 'opsctl install' does.
Nothing on disk under /opt/APP/ changes.

Configuration keys:
  host.name  the fully-qualified name this host answers at
  host.apex  the app that answers at the parent of host.name; unset means none
```

Each exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An operator disables an app

Command:

```
$ sudo opsctl disable crm
```

Output:

```
stop: ok (ikigenba-crm.socket, ikigenba-crm.service stopped, disabled)
nginx: ok (crm.sbx.ikigenba.dev disabled)
services: ok (crm disabled)
```

Exits 0. The lines are on stdout; stderr is empty. The `nginx` line names
every name the app answers at, as `install`'s does.

Preconditions:

- `crm` is installed, both its units are enabled, its socket is listening,
  and its service is `active`.
- `/var/lib/ikigenba/services.json` lists `crm` as enabled.

Postconditions:

- `ikigenba-crm.socket` and `ikigenba-crm.service` are inactive and disabled,
  and `/run/ikigenba/crm.sock` is gone. The socket stopped first, so no
  request started the service again.
- Neither unit starts at the next boot, and no request can start the
  service: there is no socket to reach.
- `/etc/nginx/conf.d/ikigenba.conf` has been regenerated and nginx reloaded:
  `https://crm.sbx.ikigenba.dev` answers `503` (`S5-nginx.md`). Between the
  stop and the reload, a request found no socket and nginx answered it with
  an error of its own.
- `/var/lib/ikigenba/services.json` has been rewritten and lists `crm` as not
  enabled.
- Both unit files and everything under `/opt/crm/` and
  `/var/opt/ikigenba/crm/` are as they were.
  `status` shows `crm v0.1.0 inactive disabled wal`.
- No other app on the host has changed.

## An operator enables a disabled app

Command:

```
$ sudo opsctl enable crm
```

Output:

```
enable: ok (ikigenba-crm.socket, ikigenba-crm.service)
nginx: ok (crm.sbx.ikigenba.dev)
services: ok (crm enabled)
service: ok (crm v0.1.0 active)
```

Exits 0. The lines are on stdout; stderr is empty. A service that will not
come up is reported as `restart` reports it: `service: failed: crm: service
failed to start` on stdout, `opsctl: enable failed` and the quoted journal on
stderr, exit 1, with both units left enabled, nginx routing the app's names
to its socket, and `/var/lib/ikigenba/services.json` listing it as enabled.

Preconditions:

- `crm` is installed and both its units are disabled and inactive.
- `/var/lib/ikigenba/services.json` lists `crm` as not enabled.

Postconditions:

- Both units are enabled, the socket is listening at
  `/run/ikigenba/crm.sock` — since before nginx was reloaded — and the
  service is `active`. Both start at the next boot.
- `/etc/nginx/conf.d/ikigenba.conf` has been regenerated and nginx reloaded:
  `https://crm.sbx.ikigenba.dev` reaches `crm` again.
- `/var/lib/ikigenba/services.json` has been rewritten and lists `crm` as
  enabled.
- Nothing under `/opt/crm/` or `/etc/` was written other than systemd's own
  enablement links and the nginx configuration. The only other file written
  is `/var/lib/ikigenba/services.json`.
- No other app on the host has changed.

## An operator disables an app that is already disabled, or enables one that is already enabled

Both commands are about the state the app is in afterwards, like `config
del`, so asking for the state it is already in succeeds and changes nothing.
The lines say so.

Command:

```
$ sudo opsctl disable crm
```

Output:

```
stop: ok (ikigenba-crm.socket, ikigenba-crm.service already inactive, disabled)
nginx: ok (unchanged)
services: ok (unchanged)
```

Command:

```
$ sudo opsctl enable crm
```

Output:

```
enable: ok (ikigenba-crm.socket, ikigenba-crm.service already enabled)
nginx: ok (unchanged)
services: ok (unchanged)
service: ok (crm v0.1.0 active)
```

Each exits 0. The lines are on stdout; stderr is empty. The `service` line of
`enable` reports the service as it is after the command: an enabled app whose
service was inactive or failed is started, as `enable` always starts both
units.

Preconditions:

- For `disable`, `crm` is installed and both its units are already disabled
  and inactive. For `enable`, both are already enabled and the service is
  `active`.

Postconditions:

- Nothing has changed. No unit was enabled, disabled, started, or stopped,
  and nginx was not reloaded: the configuration it would have written is
  byte for byte the one in place. `/var/lib/ikigenba/services.json` is byte
  for byte as it was.

## An operator tries to disable the authenticator

An installed app named `auth` is the authenticator (`S5-nginx.md`): every
other app's requests are checked against it. Disabling it would leave the
other apps either open to anyone or answering errors, so `disable` refuses it
always, whatever else is on the host, and checks this before it touches
anything.

Command:

```
$ sudo opsctl disable auth
```

Output:

```
stop: failed: auth is the authenticator and cannot be disabled
opsctl: disable failed
```

Exits 1. The outcome line is on stdout; the command diagnostic is on stderr.

Preconditions:

- `auth` is installed.

Postconditions:

- Nothing has changed. Both of `auth`'s units are as they were, nginx was
  neither regenerated nor reloaded, and `/var/lib/ikigenba/services.json` was
  not rewritten. `uninstall` remains the only way to take
  the authenticator off the host.

## An operator disables or enables an app that is not installed

Command:

```
$ sudo opsctl disable gmail
```

Output:

```
stop: failed: gmail is not installed
opsctl: disable failed
```

Command:

```
$ sudo opsctl enable gmail
```

Output:

```
enable: failed: gmail is not installed
opsctl: enable failed
```

Each exits 1. The outcome line is on stdout; the command diagnostic is on
stderr. A name that is no service at all, with neither an `/opt/gmail/`
holding an `etc/` nor a `/var/opt/ikigenba/gmail/` holding a `state/`, gives
`failed: no service 'gmail'` in the outcome line instead, also exit 1.

Preconditions:

- `/var/opt/ikigenba/gmail/` holds a `state/`, `/opt/gmail/` does not
  exist, and there is no `ikigenba-gmail.socket` or
  `ikigenba-gmail.service`; or `gmail` is no service at all.

Postconditions:

- Nothing has changed.

## An operator runs `disable` or `enable` with no app, or more than one

Command:

```
$ sudo opsctl disable
```

Output:

```
opsctl: disable needs APP

see 'opsctl disable --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. More than one operand gives
`opsctl: disable takes one APP`. `enable` answers the same way with its own
name: `opsctl: enable needs APP` and `opsctl: enable takes one APP`, followed
by `see 'opsctl enable --help' for usage`.

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
its own binary reports, the state of its service unit, the state of its socket
unit, and the journal mode of the database its manifest declares. A service is
any /opt/<name>/ with an etc/ directory or any /var/opt/ikigenba/<name>/ with
a state/ directory; '-' means opsctl could not ask, or there was nothing to
ask.

A service that is inactive behind an active socket is idle, not down: its
socket starts it again when the next request arrives. The socket's field reads
'disabled' when its unit is disabled, as 'opsctl disable' leaves it: neither
unit starts until 'opsctl enable'.

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

The answer comes from the host and nowhere else: the services under `/opt`
and `/var/opt/ikigenba`,
each app's own binary asked its version with `--version`, each app's service
and socket units asked their states, and each declared database asked its
journal mode. Each state is the unit's own systemd state: `active`,
`inactive`, or `failed` — except that the socket's field is `disabled` when
systemd reports the socket unit disabled, whatever its state. One line
per app, in name order. `devctl space status` runs exactly this over ssh and
copies the output to the developer's terminal byte for byte.

The fifth field is `-` for a service that declares no database, because there
was nothing to ask — the same `-` the other fields use.

Command:

```
$ sudo opsctl status
```

Output:

```
crm v0.1.0 active active wal
dashboard v0.0.9 active active -
gmail v0.1.0 failed active -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- Three apps are installed and every socket is listening. `gmail`'s service
  is `failed`.
- `crm`'s manifest declares a `[database]` and that database is in WAL mode.
  Neither of the others declares one.

Postconditions:

- Nothing has changed.

A failed unit is a fact about the host, which is what `status` was asked to
look at, so it is part of the report and not a failure of the command: the
exit code is 0 whatever the report says.

## A developer asks about a host with a disabled app

`crm` was disabled with `opsctl disable crm`. Its service is inactive and its
socket field says why: the app is disabled, not stopped by hand or failed.

Command:

```
$ sudo opsctl status
```

Output:

```
crm v0.1.0 inactive disabled wal
dashboard v0.0.9 active active -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `crm` is installed and disabled: both its units are disabled and inactive.
  Its manifest declares a `[database]` in WAL mode.
- `dashboard` is installed and enabled, and its service is `active`.

Postconditions:

- Nothing has changed. A disabled app is a fact about the host, reported in
  the line with exit 0 like a failed unit.

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

- No directory under `/opt/` holds an `etc/`, and none under
  `/var/opt/ikigenba/` holds a `state/`.

Postconditions:

- Nothing has changed.

## A developer asks about a host holding a service opsctl did not install

A `/var/opt/ikigenba/<name>/` with a `state/` and no `/opt/<name>/` is what
`uninstall` leaves behind. It is a service — the host will back it up — and it is
not something opsctl can ask a version or a unit state of, so all three are
`-`.

Command:

```
$ sudo opsctl status
```

Output:

```
crm v0.1.0 active active wal
gmail - - - -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `/var/opt/ikigenba/gmail/` holds a `state/`, `/opt/gmail/` does not
  exist, and there is no `ikigenba-gmail.socket` or
  `ikigenba-gmail.service`.

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
crm v0.1.0 active active delete
dashboard v0.0.9 active active -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `crm`'s manifest declares a `[database]` and that database's journal mode is
  `delete`.

Postconditions:

- Nothing has changed. status reads the journal mode and never sets it:
  putting the database back into WAL mode is the app's to do, not opsctl's.

`crm` is healthy by every other measure in the line, which is the point of
reporting this one. Its units are active, its version is what was installed, and
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
