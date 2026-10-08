# Stories — apps

An app reaches a host inside a suite release, and only that way: `opsctl
activate` puts a whole release in place at once (`S10-releases.md`), and an
app leaves a host when an activate puts in place a release without it, which
keeps the app's data. The stories here are about the apps a host runs once
they are there: restarting one, disabling and enabling one, and asking what
the host runs. Restarting an app changes nothing on disk. What each host is
running is read back from the host itself, never from a record kept anywhere
else.

`restart`, `disable`, `enable`, and `status` are written for a released host,
one where `/opt/ikigenba/current` exists.
There a service is an app the release `current` names — a directory
`/opt/ikigenba/current/<app>/` holding `bin/<app>` and `etc/manifest.toml` —
or any `/var/opt/ikigenba/<name>/` holding a `state/`; `/opt/<name>/` makes no
service. A service with only its kept state is a data-only service: it has no
units, no nginx block, and no entry in the services file. Every app in the
release reports the release's short sha, the first 7 characters of the commit
it was built from: `c604e32` for the release
`c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18`. `status` also shows the
release's label, the one the last activate of it gave (`r142`), or `-` when
it has none. Each app's units are the ones
activate writes: the service runs `/opt/ikigenba/current/<app>/bin/<app>`,
and the services file is `/run/ikigenba/services.json`.

A host may instead still be laid out the legacy way, per app: it has no
`/opt/ikigenba/current`, and each of its apps sits under `/opt/<app>/`,
holding `bin/`, `etc/`, and `share/`, with units that run
`/opt/<app>/bin/<app>`. No command creates that layout any more; a host that
has it keeps it until its first activate, the cutover (`S10-releases.md`).
On such a per-app host these four commands keep that host's behaviour: a
service is any `/opt/<name>/` holding an `etc/`, or any
`/var/opt/ikigenba/<name>/` holding a `state/`, and a `/opt/<name>/state/`
left by itself is not a service; the version is what the app's binary
answers to `--version`; the label in `status` is always `-`; and the
services file is `/var/lib/ikigenba/services.json`. Their refusals there
keep their old text: `restart`, `disable`, and `enable` of an app with no
`/opt/gmail/` fail with `<step>: failed: gmail is not installed`.

An app's name, the one its manifest declares, is a DNS label that is not
`host`, `deploy`, `snapshots`, `seed`, `backup-host`, `backup-services`,
`renew-certificate`, `services`, or `opsctl`, the prefixes, unit names, and
folders the host already uses. `snapshots` and `seed` are
prefixes under the space's own (see `S08-backup.md`), where an app of that
name would keep its backups and replica beside them. `services` would collide
with `ikigenba-services.service`, the unit that writes the services file at
boot on a released host, and `opsctl` with `opsctl/`, opsctl's own folder in
every release (`S10-releases.md`). devctl's build checks a name too, but
opsctl does not trust that it did: `activate` refuses a release holding an
app of any other name.

The top-level usage gains four lines under `Commands:`:

```
  restart   restart an installed app's service
  disable   stop an installed app and keep it from starting
  enable    let a disabled app start again, and start it
  status    print every service, its release and its state
```

Three configuration keys:

| key | value |
|---|---|
| `aws.region` | the region this host's parameters and backups live in, e.g. `us-east-2` |
| `apps.drain_seconds` | how long every app may spend finishing the requests it holds once told to stop, in whole seconds; unset or empty means `5` |
| `apps.stop_seconds` | how long systemd waits for every app to stop before killing it, in whole seconds; unset or empty means `10` |

The two timing keys are space-wide: no manifest sets them, and every app on
the host gets the same values. Each must be a positive whole number, and
`apps.stop_seconds` must be greater than `apps.drain_seconds`, so an app that
drains for as long as it is allowed still exits on its own before systemd
kills it. `activate` writes them into every app of the release it puts in
place (`S10-releases.md`), and `init` writes them into every app the host
runs (see `S04-init.md`), which is how a changed value reaches apps already
on the host.

Every app the host runs has a package directory holding what its release
brought: `bin/<app>`, `etc/manifest.toml`, whatever else the app keeps under
`etc/`, and `share/` when the app has one; an app in a release also holds
`libexec/` and `lib/` (`S10-releases.md`). The package directory is
`/opt/ikigenba/current/<app>/` on a released host and `/opt/<app>/` on a
per-app host.
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
`S05-nginx.md`). On a host without one every app is already open, and `guests`
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
  `ikigenba-apps.slice` that `init` writes (`S04-init.md`); absent means
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

A manifest that breaks one of these rules is refused wherever opsctl reads
it, always in the same words: `activate` refuses the release that carries it
before anything on the host is written (`S10-releases.md`), and `init` stops
at the first app the host runs whose manifest is refused (`S04-init.md`).
The words follow `etc/manifest.toml: `, except the name's, which stand alone:

- A `port`, whatever its value: `'port' is not allowed; the host gives the app
  its socket`.
- A `description` holding a control character, `"Customers\nand deals"` say,
  whatever `mcp` is: `'description' must be one line of text`. This rule is
  judged before the next, so a description holding a tab gets this refusal.
- `mcp = true` with no `description`, or one that is empty or holds only
  whitespace: `'mcp' is true but 'description' is empty; an MCP service must
  say what it offers`. With `mcp` false or absent, a missing or empty
  `description` is accepted, and the app's entry carries the description
  `""`.
- A `description` that is not a string, an `mcp` or a `guests` that is not a
  Boolean (`guests = "yes"` or `guests = 1`, say), or a `resources` that is
  not a table, is refused the way any other key of the wrong type is:
  `<decoder complaint>`, the TOML decoder's own words for the key and the
  type it found, so it varies with the value.
- A `[database]` with no `engine`, or one naming anything but `"sqlite"`
  (`engine = "postgres"`, say): `'database.engine' must be "sqlite"`. SQLite
  is the only engine the platform keeps, and the only one litestream
  replicates.
- A `slice` other than `"core"` or `"apps"` (`"edge"`, `"Core"`, `""`, or
  `1`): `'resources.slice' must be "core" or "apps"`.
- A `memory_max` that is not a string of a positive whole number optionally
  followed by `K`, `M`, or `G` — `0`, `"512MB"`, `"1.5G"`, `"50%"`, or the
  integer `536870912`: `'resources.memory_max' must be a whole number of
  bytes, optionally followed by K, M, or G`.
- A `go_memory_limit` not in that form: `'resources.go_memory_limit' must be
  a whole number of bytes, optionally followed by K, M, or G`.
- A well-formed `go_memory_limit` larger than `memory_max`, or than the
  default `128M` when `memory_max` is absent — `"512M"` beside `memory_max =
  "256M"`: `'resources.go_memory_limit' must not be larger than
  'resources.memory_max'`. Equal is allowed.
- A `cpu_weight` outside 1 to 10000, or not a whole number — `0`, `20000`, or
  `"50"`: `'resources.cpu_weight' must be a whole number from 1 to 10000`.
- A `delegate` that is not a Boolean — `"yes"` or `1`: `'resources.delegate'
  must be true or false`.
- An `oom_policy` other than `"continue"` — `"stop"` or `"kill"`:
  `'resources.oom_policy' must be "continue"`.
- A key the table does not know, `io_weight` or `cpu_quota` say:
  `'resources.io_weight' is not allowed; the resources are slice, memory_max,
  go_memory_limit, cpu_weight, delegate, and oom_policy`. A manifest written
  for an older opsctl, with `io_weight` in it, is refused this way until the
  app drops the key.
- An `app` that is missing or empty, is not a DNS label, or is one of the
  reserved names above: `'<name>' is not a usable app name`, with no
  `etc/manifest.toml: ` before it.

When the `[resources]` table holds more than one fault, only the first is
reported, in the order `slice`, `memory_max`, `go_memory_limit` (its form,
then the comparison), `cpu_weight`, `delegate`, `oom_policy`, then unknown
keys in byte order. Any app may set `delegate` and `oom_policy`; opsctl keeps
no list of which apps should.

Whether `memory_max` fits the host is not a rule of the manifest's: `activate`
judges it against the slice units `init` wrote, not the host's memory, at its
`resources` step, before anything on the host is written
(`S10-releases.md`). It needs `/etc/systemd/system/ikigenba.slice` and each
app's own slice unit, and each that should carry a `MemoryMax` must carry one
it can read: a unit that is missing fails the step with
`/etc/systemd/system/<unit> is missing; run 'opsctl init'`, and one with no
`MemoryMax` it can read with `/etc/systemd/system/<unit> has no MemoryMax;
run 'opsctl init'`. An app whose `memory_max` is more than its ceiling fails
the step with `<app>: etc/manifest.toml: memory_max <m> is more than
<unit>'s MemoryMax <c>`: `memory_max = "2G"` for an apps app beside an
`ikigenba-apps.slice` of `MemoryMax=1024M` reads `memory_max 2048M is more
than ikigenba-apps.slice's MemoryMax 1024M`, and a core app is judged against
`ikigenba.slice` the same way. A `memory_max` equal to the ceiling is
accepted. Sizes are written in whole MiB with `M` when they are whole MiB, in
whole KiB with `K` when they are whole KiB, and in bytes otherwise.

A slice's ceiling bounds what its apps hold together, not what each may ask
for, so the `memory_max` values in a slice may add up to more than the slice
has. `activate` lets them, up to a point: once they add up to more than twice
the slice's `MemoryMax`, it still goes on but says so at the end of its
`resources` line, with `; warning: ikigenba-apps.slice memory_max adds up to
2112M, more than twice its MemoryMax 1024M`, say. It looks at
`ikigenba-apps.slice`, counting the release's apps in it, and then at
`ikigenba.slice`, counting every app of the release and nginx's 128 MiB,
whose clause, when it is over too, follows the first in the same form.
Disabled apps count, and a sum equal to twice the ceiling adds no clause.

An app whose package carries `share/icon.svg` is in the host's service
launcher; nothing in the manifest puts it there. Every command here that
regenerates nginx also rewrites the services file —
`/run/ikigenba/services.json` on a released host,
`/var/lib/ikigenba/services.json` on a per-app host — the list of the host's
services, each with its manifest's `description` and `mcp`, and its icon when
it ships one (see `S09-services.md`), and reports it on a `services` line
right after the `nginx` line. Every app the host runs has an entry there,
icon or not, so the line reports a change for any app that is disabled or
enabled.

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

The stories below use this manifest as it stands, with no `[resources]`,
unless one says otherwise.

A host may have no default app, in which case its own name answers 404 (see
`S05-nginx.md`); it may never have two. The root domain's apex is a separate
choice, made in the store rather than the manifest (`host.apex`, see
`S05-nginx.md`): the app it names answers at `ikigenba.dev` as well, and
`disable` and `enable` report that name the way they report the space's.

The `[env]` table is written out into the app's environment file, and the
`[resources]` table into its service unit. The `[database]` table is read for
one purpose only: to regenerate `/etc/litestream.yml` from every manifest on
the host, the way nginx is regenerated, so that a database starts being
replicated in the same command that brings it to the host. What the table
means, and what replication is, are `S08-backup.md`'s. It is in the manifest
rather than the store because it is a fact about the app, which travels with
the app.

Secret *values* never travel in a release. devctl writes them to the
parameter `/<host.name>/<app>` — `/sbx.ikigenba.dev/crm` for `crm` on the
space `sbx` — and the host's own role is what reads them back; a host can
read no other space's parameters.

An app's environment file is host configuration, not part of what its release
brought: `/etc/opt/ikigenba/<app>/env`, owned `root:root` with mode `0600`.
`activate` writes it whole for every app of the release it puts in place,
from the manifest, the parameter, the store, and the release
(`S10-releases.md`); `restore` writes it whole the same way (see
`S08-backup.md`); and `init` rewrites it (see `S04-init.md`). It is never
backed up: it is generated, so a host writes it again rather than carrying a
stale copy forward. Writing it whole creates `/etc/opt/` and
`/etc/opt/ikigenba/` owned `root:root` with mode `0755` if they are missing,
and `/etc/opt/ikigenba/<app>/` the same way; that directory holds the `env`
file and nothing else. A per-app host laid out before the file lived there
may still hold an app's file at `/opt/<app>/etc/env`; nothing reads it there
except a unit written in those days.

Each app runs as two systemd units, written by `activate`
(`S10-releases.md`) and never backed up:

- `ikigenba-<app>.socket` holds the app's Unix socket,
  `/run/ikigenba/<app>.sock`, owned by the user `ikigenba` and the group
  `nginx` with mode `0660`, so nginx and the platform's own processes can
  connect and nothing else can. It names its listen backlog explicitly, is
  wanted by `sockets.target`, and removes the socket file when it stops. It is
  enabled, so the socket listens from boot.
- `ikigenba-<app>.service` runs `bin/<app>` from the app's package directory
  with `/var/opt/ikigenba/<app>` as its working directory and
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
activate's rolling restart, and `restart`, restart the service and never the
socket. Only `restore`, `retire`, `disable`, and an activate that puts in
place a release without the app stop the socket, so that no request can
start the service again while they work or after.

Besides its environment file, an app keeps its files in two places. Its
package directory holds what its release brought: `bin/`, `etc/`, and
`share/`. `/var/opt/ikigenba/<app>/`, its working directory, holds what the
app writes as it runs: `state/`, its data, and `cache/`, which it can lose.
`activate` creates `/var/opt/ikigenba/` owned `root:root` with mode `0755` if
it is missing, and `/var/opt/ikigenba/<app>/`, owned `ikigenba:ikigenba` with
mode `0750`, for an app that has none (`S10-releases.md`). It never creates
`state/` or `cache/`: the app creates them when it starts. A relative path in
a manifest resolves against the working directory, so `state/crm.db` is
`/var/opt/ikigenba/crm/state/crm.db`. Every app's working directory is a
sibling under `/var/opt/ikigenba/`, so a path one app gives into another's
data, such as `../repos/state/repos`, reaches it.

A per-app host laid out before the data lived there may still hold an app's
`state/` under `/opt/<app>/`. No command moves it; the commands that would
otherwise name data that is not where they say refuse such a host instead
(`S04-init.md`, `S08-backup.md`, `S10-releases.md`).

An app owns its schema, and its release carries what that takes: at every
start the app creates the database its manifest declares if none is there,
runs its migrations forward. A migration carries schema and never rows, so a
database the app creates starts empty; no app seeds itself. opsctl never runs
a migration. What the host promises is the order: a restore lands `state/`
and the database before the app's unit starts again, and an activate leaves
`state/` alone, so an app that is restored or upgraded migrates forward over
real data. Data reaches a fresh space only by a restore. A space that backs
nothing up holds only what has been typed into its apps since it came up or
was last restored.

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

Restart ikigenba-APP.service and report the service as 'opsctl activate'
reports each app. The socket is never restarted: it keeps listening, so
requests that arrive during the restart wait and are answered by the new
process. Nothing on disk changes: the binary, the environment file, and the
units are what the last activate wrote, so a secret pushed since then is not
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

`devctl space restart` runs this over ssh. The one line is activate's
`service` line for one app: the same question asked of the same unit, with
the short sha of the release `current` names where the app's version would
be.

Command:

```
$ sudo opsctl restart crm
```

Output:

```
service: ok (crm c604e32 active)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `/opt/ikigenba/current` points at
  `/opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18`, whose
  release holds `crm`.
- `crm` is enabled. Its service may be `active`, `inactive`, or `failed`, and
  its unit names `/etc/opt/ikigenba/crm/env` as its environment file.

Postconditions:

- `ikigenba-crm.service` is `active` and its main process is a new one,
  running `/opt/ikigenba/current/crm/bin/crm`.
- `ikigenba-crm.socket` is listening and was not restarted: a request that
  arrived during the restart waited on it and was answered by the new
  process. Had the socket been stopped, starting the service started it too.
- Nothing under `/opt/ikigenba/` or `/etc/` was written. The environment
  systemd gave the new process is the file its unit names,
  `/etc/opt/ikigenba/crm/env`, as it was last written.
- No unit was enabled or disabled, nginx was not reloaded, and
  `litestream.service` was not touched: the app closed and reopened its
  database, and litestream never stopped watching it.
- No other app on the host has changed.

## A developer restarts an app whose service will not come back

The same failure activate reports, reported the same way: the step that failed
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

- `crm` is in the release `current` names, `c604e32`, and its binary exits
  at start.

Postconditions:

- The service is `failed`, and `status` shows `crm c604e32 r142 failed active wal`.
  The socket is still listening. Nothing
  on disk changed and nothing was rolled back: the host is left where an
  operator can look at it.

## A developer restarts a disabled app

A disabled app stays disabled through every operation but `opsctl enable`,
an activate included (`S10-releases.md`), so `restart` starts nothing. The
line reports the app as it is, the way activate reports a disabled app, and
the command succeeds: the host is in the state the operator chose.

Command:

```
$ sudo opsctl restart crm
```

Output:

```
service: ok (crm c604e32 disabled)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `crm` is in the release `current` names, `c604e32`, and disabled: both its
  units are disabled and inactive.

Postconditions:

- Nothing has changed. Neither unit was started or enabled, and `crm`'s
  names still answer `503`.

## An operator restarts an app that is not in the current release

A `/var/opt/ikigenba/gmail/` with a `state/` is a service, and the host backs
it up, but with no `gmail` in the release `current` names there is nothing to
run. A name that is no service at all is a stronger case of the same.

Command:

```
$ sudo opsctl restart gmail
```

Output:

```
service: failed: gmail is not in the current release
opsctl: restart failed
```

Exits 1. The service outcome is on stdout; the command diagnostic is on stderr.
A name that is no service at all, neither in the release `current` names nor
a `/var/opt/ikigenba/gmail/` holding a `state/`, gives
`service: failed: no service 'gmail'` on stdout and the same command diagnostic,
also exit 1. With no operand the diagnostic is `opsctl: restart needs APP`,
with more than one `opsctl: restart takes one APP`, each followed by the usage
hint and exit 2; these grammar failures have no service outcome and empty stdout.

Preconditions:

- `/opt/ikigenba/current` exists and its release holds no `gmail`.
- `/var/opt/ikigenba/gmail/` holds a `state/`, and there is no
  `ikigenba-gmail.socket` or `ikigenba-gmail.service`; or `gmail` is no
  service at all.

Postconditions:

- Nothing has changed.

## An operator asks what `disable` and `enable` can do

An app in the current release starts at boot and answers whenever its socket
is reached. Disabling it keeps it in place — its units, its data, and its
place in the release stay — while neither of its units starts, at boot or on
a request, until it is enabled again. An activate keeps a disabled app
disabled, in the new release as in the old (`S10-releases.md`).

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
request. The nginx configuration and /run/ikigenba/services.json are then
regenerated, so APP's names answer 503 and the service launcher shows APP
disabled until it is enabled. Nothing on disk under /opt/ikigenba/ or
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
regenerate the nginx configuration and /run/ikigenba/services.json so APP's
names reach it again and the service launcher shows it enabled, then start
the service and report it as 'opsctl activate' reports each app.
Nothing on disk under /opt/ikigenba/ changes.

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
every name the app answers at.

Preconditions:

- `crm` is in the release `current` names, `c604e32`; both its units are
  enabled, its socket is listening, and its service is `active`.
- `/run/ikigenba/services.json` lists `crm` as enabled.

Postconditions:

- `ikigenba-crm.socket` and `ikigenba-crm.service` are inactive and disabled,
  and `/run/ikigenba/crm.sock` is gone. The socket stopped first, so no
  request started the service again.
- Neither unit starts at the next boot, and no request can start the
  service: there is no socket to reach.
- `/etc/nginx/conf.d/ikigenba.conf` has been regenerated and nginx reloaded:
  `https://crm.sbx.ikigenba.dev` answers `503` (`S05-nginx.md`). Between the
  stop and the reload, a request found no socket and nginx answered it with
  an error of its own.
- `/run/ikigenba/services.json` has been rewritten and lists `crm` as not
  enabled.
- Both unit files and everything under `/opt/ikigenba/` and
  `/var/opt/ikigenba/crm/` are as they were.
  `status` shows `crm c604e32 r142 inactive disabled wal`.
- The next activate leaves `crm` disabled and does not start it
  (`S10-releases.md`).
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
service: ok (crm c604e32 active)
```

Exits 0. The lines are on stdout; stderr is empty. A service that will not
come up is reported as `restart` reports it: `service: failed: crm: service
failed to start` on stdout, `opsctl: enable failed` and the quoted journal on
stderr, exit 1, with both units left enabled, nginx routing the app's names
to its socket, and `/run/ikigenba/services.json` listing it as enabled.

Preconditions:

- `crm` is in the release `current` names, `c604e32`, and both its units are
  disabled and inactive.
- `/run/ikigenba/services.json` lists `crm` as not enabled.

Postconditions:

- Both units are enabled, the socket is listening at
  `/run/ikigenba/crm.sock` — since before nginx was reloaded — and the
  service is `active`. Both start at the next boot, and the next activate
  starts `crm` with the rest.
- `/etc/nginx/conf.d/ikigenba.conf` has been regenerated and nginx reloaded:
  `https://crm.sbx.ikigenba.dev` reaches `crm` again.
- `/run/ikigenba/services.json` has been rewritten and lists `crm` as
  enabled.
- Nothing under `/opt/ikigenba/` or `/etc/` was written other than systemd's
  own enablement links and the nginx configuration. The only other file
  written is `/run/ikigenba/services.json`.
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
service: ok (crm c604e32 active)
```

Each exits 0. The lines are on stdout; stderr is empty. The `service` line of
`enable` reports the service as it is after the command: an enabled app whose
service was inactive or failed is started, as `enable` always starts both
units.

Preconditions:

- `crm` is in the release `current` names, `c604e32`.
- For `disable`, both its units are already disabled and inactive. For
  `enable`, both are already enabled and the service is `active`.

Postconditions:

- Nothing has changed. No unit was enabled, disabled, started, or stopped,
  and nginx was not reloaded: the configuration it would have written is
  byte for byte the one in place. `/run/ikigenba/services.json` is byte
  for byte as it was.

## An operator tries to disable the authenticator

An app named `auth` is the authenticator (`S05-nginx.md`): every other app's
requests are checked against it. Disabling it would leave the other apps
either open to anyone or answering errors, so `disable` refuses it always,
whatever else is on the host, and checks this before it touches anything.

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

- `auth` is in the release `current` names.

Postconditions:

- Nothing has changed. Both of `auth`'s units are as they were, nginx was
  neither regenerated nor reloaded, and `/run/ikigenba/services.json` was
  not rewritten. Only an activate of a release without `auth` takes the
  authenticator off the host (`S10-releases.md`).

## An operator disables or enables an app that is not in the current release

Command:

```
$ sudo opsctl disable gmail
```

Output:

```
stop: failed: gmail is not in the current release
opsctl: disable failed
```

Command:

```
$ sudo opsctl enable gmail
```

Output:

```
enable: failed: gmail is not in the current release
opsctl: enable failed
```

Each exits 1. The outcome line is on stdout; the command diagnostic is on
stderr. A name that is no service at all, neither in the release `current`
names nor a `/var/opt/ikigenba/gmail/` holding a `state/`, gives
`failed: no service 'gmail'` in the outcome line instead, also exit 1.

Preconditions:

- `/opt/ikigenba/current` exists and its release holds no `gmail`.
- `/var/opt/ikigenba/gmail/` holds a `state/`, and there is no
  `ikigenba-gmail.socket` or `ikigenba-gmail.service`; or `gmail` is no
  service at all.

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

Print one line per service on this host, in name order: its name, the short
commit of the release it runs, that release's label or '-' when it has none,
the state of its service unit, the state of its socket unit, and the journal
mode of the database its manifest declares.
A service is an app the current release (/opt/ikigenba/current) holds, or any
/var/opt/ikigenba/<name>/ with a state/ directory. A service with only its
kept state is data only, and every field after its name is '-'. '-' means
opsctl could not ask, or there was nothing to ask.

On a host not yet running releases, a service is any /opt/<name>/ with an
etc/ directory or any /var/opt/ikigenba/<name>/ with a state/ directory, and
the second field is the version its own binary reports and the third is
always '-'.

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

The answer comes from the host and nowhere else: the apps in the release
`/opt/ikigenba/current` names and the kept state under `/var/opt/ikigenba`,
each app shown with that release's short sha and its label, each app's
service and socket units asked their states, and each declared database asked its journal mode.
Every app in the release shows the same sha and label: they were built and
activated together. The label is the one the last activate of the release
gave, read from its `label` file; a release activated without one shows `-`
in that field. Each state is the unit's own systemd state: `active`, `inactive`,
or `failed` — except that the socket's field is `disabled` when systemd
reports the socket unit disabled, whatever its state. One line per service,
in name order. `devctl space status` runs exactly this over ssh and copies
the output to the developer's terminal byte for byte.

The sixth field is `-` for a service that declares no database, because there
was nothing to ask — the same `-` the other fields use.

Command:

```
$ sudo opsctl status
```

Output:

```
crm c604e32 r142 active active wal
dashboard c604e32 r142 active active -
gmail c604e32 r142 failed active -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `/opt/ikigenba/current` points at
  `/opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18`, whose
  release holds `crm`, `dashboard`, and `gmail`, and no other service is on
  the host. The release was last activated with the label `r142`, so its
  folder holds a `label` file reading `r142`.
- Every socket is listening. `gmail`'s service is `failed`.
- `crm`'s manifest declares a `[database]` and that database is in WAL mode.
  Neither of the others declares one.

Postconditions:

- Nothing has changed.

A failed unit is a fact about the host, which is what `status` was asked to
look at, so it is part of the report and not a failure of the command: the
exit code is 0 whatever the report says.

## A developer asks about a host whose release has no label

The last activate of the release `current` names gave no label, so the
release has none to report and the third field is `-` on every app's line.
The field is still there, so every line has the same six fields.

Command:

```
$ sudo opsctl status
```

Output:

```
crm c604e32 - active active wal
dashboard c604e32 - active active -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `/opt/ikigenba/current` points at
  `/opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18`, whose
  release holds `crm` and `dashboard`, and no other service is on the host.
- The release was last activated without a label, so its folder holds no
  `label` file.
- Both apps are enabled and their services are `active`. `crm`'s manifest
  declares a `[database]` in WAL mode; `dashboard`'s declares none.

Postconditions:

- Nothing has changed.

## A developer asks about a host with a disabled app

`crm` was disabled with `opsctl disable crm`. Its service is inactive and its
socket field says why: the app is disabled, not stopped by hand or failed.

Command:

```
$ sudo opsctl status
```

Output:

```
crm c604e32 r142 inactive disabled wal
dashboard c604e32 r142 active active -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The release `current` names, `c604e32`, holds `crm` and `dashboard`, and
  no other service is on the host. Its label is `r142`.
- `crm` is disabled: both its units are disabled and inactive. Its manifest
  declares a `[database]` in WAL mode.
- `dashboard` is enabled, and its service is `active`.

Postconditions:

- Nothing has changed. A disabled app is a fact about the host, reported in
  the line with exit 0 like a failed unit.

## A developer asks what a host with no apps is running

A fresh host, with neither a release nor an app laid out per app, has no
service to report.

Command:

```
$ sudo opsctl status
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `/opt/ikigenba/current` does not exist, no directory under `/opt/` holds
  an `etc/`, and none under `/var/opt/ikigenba/` holds a `state/`.

Postconditions:

- Nothing has changed.

## A developer asks about a host holding a data-only service

A `/var/opt/ikigenba/<name>/` with a `state/` whose app is not in the release
`current` names is what an activate leaves when it puts in place a release
without that app (`S10-releases.md`). It is a service — the host will back it up — but it has no
units and no release to report, so every field after its name is `-`.

Command:

```
$ sudo opsctl status
```

Output:

```
crm c604e32 r142 active active wal
gmail - - - - -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The release `current` names, `c604e32`, holds `crm` and no `gmail`. Its
  label is `r142`.
- `/var/opt/ikigenba/gmail/` holds a `state/`, and there is no
  `ikigenba-gmail.socket` or `ikigenba-gmail.service`.

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
crm c604e32 r142 active active delete
dashboard c604e32 r142 active active -
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The release `current` names, `c604e32`, holds `crm` and `dashboard`. Its
  label is `r142`.
- `crm`'s manifest declares a `[database]` and that database's journal mode is
  `delete`.

Postconditions:

- Nothing has changed. status reads the journal mode and never sets it:
  putting the database back into WAL mode is the app's to do, not opsctl's.

`crm` is healthy by every other measure in the line, which is the point of
reporting this one. Its units are active, it runs the current release, and
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
