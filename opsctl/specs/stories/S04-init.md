# Stories — init

`init` is the one command an agent runs after the release is unpacked and the
`config set` steps: it evaluates every prerequisite the platform needs, reports
all of them at once, and only then runs the setup sequence. It is safe to
re-run, and it is how a host says whether it is ready.

The top-level usage gains the line `  init      run the setup sequence behind
one preflight` under `Commands:`.

One configuration key:

| key | value |
|---|---|
| `host.name` | the fully-qualified name this host answers at, at or under a configured zone, e.g. `sbx.ikigenba.dev` |

Its preflight and its `apps` step also read `apps.drain_seconds` and
`apps.stop_seconds`, the two app timing keys `S07-apps.md` declares.

A host answers at one name: the space's, one label under the root domain.
The records a bootstrap created are `<host.name>` and `*.<host.name>`, and
the wildcard certificate, the nginx catch-all, and every app's own name all
hang off it. The one host that also answers at the root's apex says so with
`host.apex` (see `S05-nginx.md`); the preflight does not look at that key, and
a value the host cannot carry is refused by the `certificate` step, which is
the first step that reads it.

The sequence is the setup commands that exist. It is empty until a group adds
one to it, and each group that does says so; `S06-certificates.md` adds
`certificate`, this group adds `slices`, `S05-nginx.md` adds `nginx.conf`,
`S08-backup.md` adds `litestream` and `timers`, and `S07-apps.md` adds `apps`,
in that order. `slices` reads no manifest, so it comes before every step that
does: a host whose installed manifests the running opsctl refuses still gets its
slices, which `activate` needs to judge a fixed release. Every
setup command is idempotent, so `init` is too, and a step's inputs are read from
the store every run — which is why changing a period or a zone is `config set`
followed by `init`, and never an edit to something `init` generated.
`devctl space create` runs that pair once, setting its ten keys and running
`init`; on any later day an agent runs it on the host, or the space is
recreated. Two of the generated files also answer to the apps on the
host — `/etc/litestream.yml` to what is under `/var/opt/ikigenba` too — and the
nginx file to which apps are disabled as well. So does the services file
(`S09-services.md`), which `init` rewrites every run without printing a line for
it; `activate`, `rollback`, `restore`, `disable`, and `enable` regenerate those
themselves when they change what they answer to (see `S07-apps.md`,
`S08-backup.md`, and `S10-releases.md`); `init` remains the only command that enables the units
behind them.

`init` runs on three kinds of host. A *released host* is one where
`/opt/ikigenba/current` exists (`S10-releases.md`): its apps are the ones the
release `current` names, the `apps` step writes each one's environment file and
units exactly as `activate` does, together with the boot unit
`ikigenba-services.service` (`S09-services.md`), and the services file is
`/run/ikigenba/services.json`. A *per-app host* has no `current` and is laid out the
legacy way, each app under `/opt/<app>/` (`S07-apps.md`): there `init` keeps
that layout's behaviour, its apps are the ones under `/opt/<app>/`, and the services file is
`/var/lib/ikigenba/services.json`; a story written for that layout says so in
its preconditions. A *fresh host* has neither: `init` does the host's own work —
the certificate, the slices, an nginx file with no apps, an empty
`/run/ikigenba/services.json`, the account `ikigenba`, Litestream, and the
timers — and has no app to write. On any host, once the preflight has passed and
before the `certificate` step, `init` makes `/usr/local/bin/opsctl` a link when
it does not exist, so certbot finds the `opsctl` its hooks name: to
`/opt/ikigenba/current/opsctl/bin/opsctl` when `current` exists, otherwise to
the opsctl that is running. A `/usr/local/bin/opsctl` that exists, link or file,
is left as it is. That link prints no line either.

The suite runs inside three slices, systemd's way of grouping services so
that they share a CPU weight and a memory ceiling. The `slices` step writes
them as unit files under `/etc/systemd/system`; a dash in a slice's name makes
the part before it the parent:

| slice | holds | settings |
|---|---|---|
| `ikigenba.slice` | the other two | `CPUWeight=100`, `MemoryMax` 80% of the host's memory |
| `ikigenba-core.slice` | nginx and the core apps | `CPUWeight=300` |
| `ikigenba-apps.slice` | every other app | `CPUWeight=100`, `MemoryMax` two thirds of `ikigenba.slice`'s, `MemoryHigh` fifteen sixteenths of its own |

The host's memory is the `MemTotal` line of `/proc/meminfo`, read when the
step runs. Each `MemoryMax` is rounded to the nearest 64 MiB and written in
whole MiB; `MemoryHigh` is cut down to a whole MiB. On a t3.small, whose
`MemTotal` is 1909 MiB, that is `MemoryMax=1536M` for `ikigenba.slice`, and
`MemoryMax=1024M` with `MemoryHigh=960M` for `ikigenba-apps.slice`. The numbers
are fixed when the step writes them: a host resized to more or less memory
keeps the old ones until `init` runs again. The same step writes nginx's
drop-in, `/etc/systemd/system/nginx.service.d/ikigenba.conf`, which puts
nginx in `ikigenba-core.slice` with `CPUWeight=100`, `MemoryMax=128M`, and
`MemoryLow=32M`, so the front door keeps its memory when the apps are short of
theirs. Each file is the same bytes on every run with the same memory. systemd
is reloaded only when one of the four files changed, and nginx is restarted,
not just reloaded, only when its drop-in changed, because a running service
moves to another slice only when it starts. Where each app goes, and how much
it may hold, is `S07-apps.md`'s; `activate` checks an app against the slice
unit this step wrote, never against the host's memory.

The host's programs — `nginx`, `certbot`, `systemctl`, `litestream`, and
`git` — are installed by the space's first boot, not by opsctl. `init` only
finds each on PATH and reports where it is, so a host that lacks one is told
so before anything runs, and the fix is the package manager followed by
`init` again. `git` is there for the apps rather than for opsctl itself: an
app may run it, as one that serves git repositories does, and every app may
rely on finding it.

Every check runs; none short-circuits another, so one run shows an agent
everything that is missing rather than one thing per run. A check whose input
is another check's output is simply absent when what it depends on failed,
because its prerequisite is already on the list. Each check's line is a fact
about the host, so the whole report is on stdout even when it says the host is
not ready; stderr is for the case where `init` cannot look at all.

## An agent asks what `init` can do

Command:

```
$ opsctl init --help
```

```
$ opsctl init -h
```

Output:

```
Usage: opsctl init

Run the setup sequence behind one preflight. Every check is evaluated and
reported, one line per check, before anything runs; when any check fails,
nothing runs and init exits 2. Safe to re-run.

Checks, in order:
  nginx, certbot, systemctl  each found on PATH
  litestream                 found on PATH
  git                        found on PATH
  dns.provider, dns.zones    set, and the provider opens (see 'opsctl dns --help')
  host.name                  set
  timeouts                   apps.drain_seconds and apps.stop_seconds are positive
                             whole seconds, stop greater than drain
  zone NAME                  every configured zone is reachable and delegated
  host NAME                  host.name lies at or under a configured zone
  wildcard NAME              host.name and _opsctl-preflight.host.name resolve alike

Sequence:
  certificate  obtain the host's certificate, or renew it if it is due
  slices       write ikigenba.slice, ikigenba-core.slice and
               ikigenba-apps.slice, sized from the host's memory, and the
               drop-in that puts nginx in ikigenba-core.slice; restart nginx
               when the drop-in changed
  nginx.conf   generate /etc/nginx/conf.d/ikigenba.conf and reload nginx; an
               app whose manifest is no longer valid, or on a host without
               releases an installed app whose state/ is still under
               /opt/APP/, stops init here, before this step or any after
               it writes anything
  litestream   generate /etc/litestream.yml and enable litestream.service
  timers       write the backup and renewal units, enabling each backup timer
               whose period is set and the renewal timer always
  apps         write the drain and stop settings into every app,
               restarting each enabled app whose settings changed; a
               disabled app is rewritten and left disabled. On a host that
               runs releases the apps are the current release's, each
               given the environment and units activate writes, and
               ikigenba-services.service is written and enabled. The
               resources an app's manifest declares are kept as activate
               wrote them

When /usr/local/bin/opsctl does not exist, init makes it a link before the
sequence runs: to /opt/ikigenba/current/opsctl/bin/opsctl when that exists,
otherwise to the running opsctl, so certbot's hooks find opsctl on PATH.

Configuration keys:
  aws.region          the region this host's parameters live in
  host.name           the fully-qualified name this host answers at, at or under a configured zone
  apps.drain_seconds  how long an app may drain when stopped (default 5)
  apps.stop_seconds   how long systemd waits for an app to stop (default 10)
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An agent initialises a host that is ready

An agent on a host that runs releases asks it to check itself and redo its
own setup, through `opsctl` on the PATH. Every line is `ok`, so the
sequence ran and the exit code is 0. The apps it writes are the ones the
release `current` names; nothing under `/opt/<app>/` is read or written.

Command:

```
$ sudo opsctl init; echo "exit $?"
```

Output:

```
nginx: ok (/usr/sbin/nginx)
certbot: ok (/usr/bin/certbot)
systemctl: ok (/usr/bin/systemctl)
litestream: ok (/usr/bin/litestream)
git: ok (/usr/bin/git)
dns.provider: ok (route53)
dns.zones: ok (ikigenba.dev)
host.name: ok (sbx.ikigenba.dev)
timeouts: ok (drain 5s, stop 10s)
zone ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
host sbx.ikigenba.dev: ok (zone ikigenba.dev)
wildcard sbx.ikigenba.dev: ok (77.112.106.79)
exit 0
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `nginx`, `certbot`, `systemctl`, `litestream`, and `git` are on the
  host's PATH.
- `dns.provider`, `dns.zones`, and `host.name` are set, and the host's
  credentials can read the configured zone.
- `apps.drain_seconds` and `apps.stop_seconds` are unset, so the defaults
  apply.
- Public DNS delegates the zone, and `sbx.ikigenba.dev` and
  `_opsctl-preflight.sbx.ikigenba.dev` resolve to the same address.
- The host is a released host: `/opt/ikigenba/current` is a link to
  `/opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18`, whose last
  `activate` gave the label `r142`, and `/usr/local/bin/opsctl` is a link to
  `/opt/ikigenba/current/opsctl/bin/opsctl`.

Postconditions:

- The setup sequence has run: the host holds its certificate, the nginx file
  generated from the store and the release `current` names, the three slices and
  nginx's drop-in, `/etc/litestream.yml` naming every database the current
  release's manifests declare, with `litestream.service` enabled, the two backup
  unit pairs with each timer enabled whose period the store gives as non-zero,
  and the certificate renewal pair with its timer enabled.
- `/run/ikigenba/services.json` has been rewritten from the store, the
  current release, and which apps are disabled. No line reports it.
- Every app in the current release has the environment file and units `activate`
  writes (`S10-releases.md`): `/etc/opt/ikigenba/<app>/env` holds, besides its
  secrets and its manifest's `[env]`, `DRAIN_SECONDS=5`,
  `IKIGENBA_SERVICES=/run/ikigenba/services.json`,
  `IKIGENBA_COMMIT=c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18`, and
  `IKIGENBA_RELEASE=r142`; its service unit runs
  `/opt/ikigenba/current/<app>/bin/<app>` in `/var/opt/ikigenba/<app>`, names
  that environment file, has a stop timeout of `10` seconds, and carries the
  resource settings the app's manifest in the current release declares, or their
  defaults (`S07-apps.md`). An app that already held those values was not
  restarted.
- `ikigenba-services.service`, the oneshot that runs
  `/usr/local/bin/opsctl services apply` at boot before every app's service,
  is written and enabled, as `activate` leaves it.
- No unit names a path under `/opt/<app>/`, and no `/opt/<app>/` was
  created.
- Every setup command is idempotent, so a host that was already set up is
  unchanged by the run.

The wildcard line is the bootstrap's own done-condition — the space's name
and its wildcard both point at this host — checked from the host. It resolves
a fixed probe label rather than a literal `*`, because a resolver will refuse
a literal `*.sbx.ikigenba.dev` while `_opsctl-preflight.sbx.ikigenba.dev`
resolves to the space's address. Whether that address is *this* host's cannot
be known without asking the cloud, which opsctl never does; that the two
lookups agree is the check.

## An agent initialises a per-app host that is ready

A host laid out per app, with its apps under `/opt/<app>/`, which has not yet
had its first `activate`, keeps that layout's behaviour: `init` writes the
installed apps' settings where their units already look. The output is the
ready host's.

Command:

```
$ sudo opsctl init; echo "exit $?"
```

Output: the twelve `ok` lines of the ready host, and `exit 0`.

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- Every preflight check passes, as for the ready host, and the timing keys
  are unset.
- The host is a per-app host: `/opt/ikigenba/current` does not exist, and
  each installed app's `bin/`, `etc/`, and `share/` are under `/opt/<app>/`,
  as the per-app layout keeps them.

Postconditions:

- The setup sequence has run, generated from the store and what is under
  `/opt`.
- `/var/lib/ikigenba/services.json` has been rewritten from the store, what
  is under `/opt`, and which apps are disabled. No line reports it.
  `/run/ikigenba/services.json` was not written.
- Every installed app's environment file, `/etc/opt/ikigenba/<app>/env`,
  holds `DRAIN_SECONDS=5` and
  `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json`, and its service unit
  names that file, a stop timeout of `10` seconds, and the resource settings
  its installed manifest declares, or their defaults (`S07-apps.md`). An app
  that already held those values was not restarted.
- Every setup command is idempotent, so a host that was already set up is
  unchanged by the run.

## An agent initialises a fresh host before its first activate

A new space has its packages from first boot, the suite release unpacked,
and the store's keys set (`S02-config.md`), but nothing has activated a
release, so `/usr/local/bin/opsctl` does not exist yet and the agent runs the
release's own opsctl by its full path. `init` does the host's own work and no
app's. It makes `/usr/local/bin/opsctl` a link to the opsctl that is running
before it asks certbot for the certificate, so the hooks certbot records find
`opsctl` on the PATH. After it, the same opsctl may restore services from
their backups (`S08-backup.md`), and then that release's `activate`
(`S10-releases.md`) makes the link point through `current`, as every
`activate` does.

Command:

```
$ sudo /opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl init; echo "exit $?"
```

Output: the twelve `ok` lines of the ready host, and `exit 0`.

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `nginx`, `certbot`, `systemctl`, `litestream`, and `git` are on the
  host's PATH, installed by the space's first boot.
- The ten keys of a configured host are set (`S02-config.md`), and every
  other preflight check passes.
- The release is unpacked at
  `/opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/`.
- The host is a fresh host: neither `/opt/ikigenba/current` nor
  `/opt/ikigenba/previous` exists, no `/opt/<app>/` and no app unit exists,
  and `/usr/local/bin/opsctl` does not exist.
- The account `ikigenba` does not exist yet.

Postconditions:

- `/usr/local/bin/opsctl` is a link to
  `/opt/ikigenba/releases/c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18/opsctl/bin/opsctl`.
- The host holds its certificate, covering `sbx.ikigenba.dev` and
  `*.sbx.ikigenba.dev`, with hooks that name `opsctl` on the PATH.
- The three slices and nginx's drop-in are written, sized from the host's
  memory, and nginx runs in `ikigenba-core.slice`.
- `/etc/nginx/conf.d/ikigenba.conf` is the configuration of a host with no
  apps (`S05-nginx.md`), and nginx has been reloaded.
- `/run/ikigenba/services.json` holds an empty `services` list
  (`S09-services.md`).
- The account `ikigenba` exists, created by `init`.
- `/etc/litestream.yml` names no database and `litestream.service` is
  enabled; the backup and renewal units are written, each timer enabled as on
  any host.
- No app was written: no `/etc/opt/ikigenba/<app>/`, no app unit, and no
  `/opt/<app>/` exists, and `/opt/ikigenba/current` still does not exist.

## An operator initialises a released host whose opsctl link is missing

Someone removed `/usr/local/bin/opsctl`, so certbot's next renewal would not
find the hooks' `opsctl`. The operator runs the current release's opsctl by
its path, and `init` puts the link back. It points through `current`, not at
the release folder, so it follows every later `activate` and `rollback`.

Command:

```
$ sudo /opt/ikigenba/current/opsctl/bin/opsctl init; echo "exit $?"
```

Output: the twelve `ok` lines of the ready host, and `exit 0`.

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The host is the ready released host, except that `/usr/local/bin/opsctl`
  does not exist.

Postconditions:

- `/usr/local/bin/opsctl` is a link to
  `/opt/ikigenba/current/opsctl/bin/opsctl`.
- Everything else is as after the ready host's run.

## An agent initialises a host that is not ready

Two independent checks fail and both are reported, along with every other
check that can still run. The two lines that depend on `host.name` are absent,
because the reason they could not run is already on the list. Nothing in the
sequence ran.

Command:

```
$ sudo opsctl init; echo "exit $?"
```

Output:

```
nginx: ok (/usr/sbin/nginx)
certbot: failed: not found on PATH
systemctl: ok (/usr/bin/systemctl)
litestream: ok (/usr/bin/litestream)
git: ok (/usr/bin/git)
dns.provider: ok (route53)
dns.zones: ok (ikigenba.dev)
host.name: failed: not set
timeouts: ok (drain 5s, stop 10s)
zone ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
exit 2
```

Exits 2. The lines are on stdout; **stderr is empty**. A failed check is a
fact about the host, which is what `init` was asked to look at; `init` itself
did not fail.

Preconditions:

- `certbot` is not on the host's PATH.
- `host.name` is unset or empty.
- `dns.provider` and `dns.zones` are set and the zone is reachable and
  delegated.

Postconditions:

- Nothing has changed. No setup command ran, no file under `/etc`, `/opt`,
  `/var/opt/ikigenba`, `/var/lib/ikigenba`, or `/run/ikigenba` was created,
  modified, or removed, and `/usr/local/bin/opsctl` was not created.

## An operator runs init again after fixing what it found

Command:

```
$ sudo dnf install -y certbot
$ sudo opsctl config set host.name=sbx.ikigenba.dev
$ sudo opsctl init; echo "exit $?"
```

Output: the twelve `ok` lines of the ready host, and `exit 0`.

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The two failures the previous run reported have been fixed.

Postconditions:

- The setup sequence has run. Running `init` a third time produces byte for
  byte the same output and the same exit code: the whole command is
  idempotent, and a preflight that passes writes nothing of its own.

## An agent's first init puts the suite in its slices

The first `init` on a fresh host writes the slices before any app is
activated, so every app `activate` later places has a slice to go into and a ceiling to be checked against. The step prints no line of its own;
the run's output is the ready host's.

Command:

```
$ sudo opsctl init; echo "exit $?"
```

Output: the twelve `ok` lines of the ready host, and `exit 0`.

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The host is a t3.small: `/proc/meminfo` has `MemTotal: 1954816 kB`, which
  is 1909 MiB.
- Every preflight check passes.
- None of `/etc/systemd/system/ikigenba.slice`, `ikigenba-core.slice`,
  `ikigenba-apps.slice`, or `nginx.service.d/ikigenba.conf` exists, and nginx
  is running in `system.slice`, where systemd puts a service by default.

Postconditions:

- `/etc/systemd/system/ikigenba.slice` sets `CPUWeight=100` and
  `MemoryMax=1536M`; `/etc/systemd/system/ikigenba-core.slice` sets
  `CPUWeight=300`; `/etc/systemd/system/ikigenba-apps.slice` sets
  `CPUWeight=100`, `MemoryMax=1024M`, and `MemoryHigh=960M`. Each is under
  `[Slice]`.
- `/etc/systemd/system/nginx.service.d/ikigenba.conf` holds, under
  `[Service]`, `Slice=ikigenba-core.slice`, `CPUWeight=100`,
  `MemoryMax=128M`, and `MemoryLow=32M`.
- systemd has been reloaded and nginx restarted, so `systemctl show
  nginx.service -p Slice -p MemoryMax -p MemoryLow` prints
  `Slice=ikigenba-core.slice`, `MemoryMax=134217728`, and
  `MemoryLow=33554432`, and `systemctl show ikigenba-apps.slice -p MemoryMax
  -p MemoryHigh` prints `MemoryMax=1073741824` and `MemoryHigh=1006632960`.
- Running `init` again writes the same four files byte for byte, does not
  reload systemd for them, and does not restart nginx.

## An operator resizes the host and runs init again

The slices are sized from the memory the host had when `init` last ran, not
the memory it has now, so a host stopped and started as a bigger instance
runs with the old ceilings until the operator runs `init`. Nothing else
recomputes them: `activate` reads the slice units as they are.

Command:

```
$ sudo opsctl init; echo "exit $?"
```

Output: the twelve `ok` lines of the ready host, and `exit 0`.

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The host was initialised as a t3.small, so its slice units hold
  `MemoryMax=1536M`, `MemoryMax=1024M`, and `MemoryHigh=960M`.
- It has since been resized to a t3.medium: `/proc/meminfo` now has
  `MemTotal: 3964928 kB`, which is 3872 MiB.
- Every preflight check passes.

Postconditions:

- `/etc/systemd/system/ikigenba.slice` sets `MemoryMax=3072M`, and
  `/etc/systemd/system/ikigenba-apps.slice` sets `MemoryMax=2048M` and
  `MemoryHigh=1920M`; their CPU weights are as they were.
  `/etc/systemd/system/ikigenba-core.slice` is byte for byte as it was.
- systemd has been reloaded, so the running slices hold the new ceilings at
  once; no app was restarted for it.
- nginx's drop-in is byte for byte as it was, so nginx was not restarted.
- An app's own `memory_max` is unchanged: a slice grows, and what each app may
  hold is still what its manifest says.

## An operator changes how long apps may drain

The two timing settings are store inputs like any other, so a change is
`config set` followed by `init`. The `apps` step writes the new values into
every app of the current release and restarts only the apps whose values
changed. A restart refuses no request, because each app's socket keeps
listening while its service restarts (`S07-apps.md`).

Command:

```
$ sudo opsctl config set apps.drain_seconds=20
$ sudo opsctl config set apps.stop_seconds=30
$ sudo opsctl init; echo "exit $?"
```

Output: the twelve `ok` lines of the ready host, with the `timeouts` line
reading

```
timeouts: ok (drain 20s, stop 30s)
```

and `exit 0`.

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The host is the ready released host, and the current release holds `crm`,
  `dashboard`, and `notes`, each with `DRAIN_SECONDS=5` and a stop timeout
  of `10` seconds. `notes` was disabled with `opsctl disable notes`.
- `crm`'s and `dashboard`'s sockets are listening, and neither is disabled.
- `/var/opt/ikigenba/gmail/` holds a `state/` and the current release holds
  no `gmail`: it is a data-only service, not an app.

Postconditions:

- `/etc/opt/ikigenba/crm/env`, `/etc/opt/ikigenba/dashboard/env`, and
  `/etc/opt/ikigenba/notes/env` hold `DRAIN_SECONDS=20`; every other line
  in them is as it was. All three service units stop with a timeout of
  `30` seconds, and each still carries the resources its app's manifest
  declares, as activate wrote them. systemd has been reloaded.
- `ikigenba-crm.service` and `ikigenba-dashboard.service` were restarted,
  so each now runs with the new values. Their sockets were not restarted.
- `notes` was not started, restarted, or enabled: both its units are still
  disabled and inactive and its names still answer `503`. When it is enabled
  it starts with the new values.
- `/var/opt/ikigenba/gmail/` was not touched, no `/etc/opt/ikigenba/gmail/`
  or `/opt/gmail/` was created, and no unit was written for it.
- Running `init` again writes the same values, restarts no app, and prints
  the same lines.

## An agent initialises a released host whose app's secret was never pushed

On a released host the `apps` step writes each app's whole environment file,
its secrets included, from the parameter `/<host.name>/<app>`, as `activate`
does. A secret the manifest names that the parameter does not hold stops
`init` at that step, in the words of `activate`'s `secrets` step after
`opsctl: `. Like
`activate`, it writes no app until every app's secrets are in hand, so no app
is left with an environment file missing a value. The steps before `apps` have
run.

Command:

```
$ sudo opsctl init; echo "exit $?"
```

Output:

```
nginx: ok (/usr/sbin/nginx)
certbot: ok (/usr/bin/certbot)
systemctl: ok (/usr/bin/systemctl)
litestream: ok (/usr/bin/litestream)
git: ok (/usr/bin/git)
dns.provider: ok (route53)
dns.zones: ok (ikigenba.dev)
host.name: ok (sbx.ikigenba.dev)
timeouts: ok (drain 5s, stop 10s)
zone ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
host sbx.ikigenba.dev: ok (zone ikigenba.dev)
wildcard sbx.ikigenba.dev: ok (77.112.106.79)
opsctl: auth: no value for 'GOOGLE_CLIENT_SECRET' in /sbx.ikigenba.dev/auth
exit 1
```

Exits 1. The `ok` lines are on stdout; the `opsctl:` line is on stderr.

Preconditions:

- Every preflight check passes, and the host's certificate is not due for
  renewal.
- The host is the ready released host, and `auth`, an app of the current
  release, names `GOOGLE_CLIENT_SECRET` in its manifest, but
  `/sbx.ikigenba.dev/auth` does not hold it, or does not exist.

Postconditions:

- The steps before `apps` ran as on the ready host: the slices, the nginx
  file, `/run/ikigenba/services.json`, `/etc/litestream.yml`, and the timers
  are as they generate them.
- No app changed: every app's environment file and units, and
  `ikigenba-services.service`, are as they were, and no app was restarted.
- Running `init` again before the secret is pushed stops at the same place
  with the same output. After `/sbx.ikigenba.dev/auth` holds it, `init` runs
  to the end.

## An agent initialises a host holding an app whose manifest is no longer valid

nginx, the services file, the backup configuration and the apps' units are
all generated from every installed app's manifest, so a manifest the running
opsctl refuses stops `init` at its `nginx.conf` step, the first that reads
them, before anything they generate is rewritten. This is the host just
upgraded from an opsctl that wrote no slices: the release it holds was
put in place under the older opsctl and carries `io_weight`, a key this one
does not know. The `slices` step, which reads no manifest, has already run,
so the host has the slices that activating a fixed release needs. `init`
judges the manifest's form only. It does not check an app's `memory_max`
against its slice or warn about a slice that is oversubscribed; those are
`activate`'s.

Command:

```
$ sudo opsctl init; echo "exit $?"
```

Output:

```
nginx: ok (/usr/sbin/nginx)
certbot: ok (/usr/bin/certbot)
systemctl: ok (/usr/bin/systemctl)
litestream: ok (/usr/bin/litestream)
git: ok (/usr/bin/git)
dns.provider: ok (route53)
dns.zones: ok (ikigenba.dev)
host.name: ok (sbx.ikigenba.dev)
timeouts: ok (drain 5s, stop 10s)
zone ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
host sbx.ikigenba.dev: ok (zone ikigenba.dev)
wildcard sbx.ikigenba.dev: ok (77.112.106.79)
opsctl: repos: etc/manifest.toml: 'resources.io_weight' is not allowed; the resources are slice, memory_max, go_memory_limit, cpu_weight, delegate, and oom_policy
exit 1
```

Exits 1. The `ok` lines are on stdout; the `opsctl:` line is on stderr. Any
other manifest fault `S07-apps.md` names is reported the same way, in its
words, after `opsctl: <app>: etc/manifest.toml: `.

Preconditions:

- Every preflight check passes, and the host's certificate is not due for
  renewal.
- The host is a per-app host: `/opt/ikigenba/current` does not exist.
- Neither the three slice units nor nginx's drop-in exists: the host has
  only ever run an opsctl that did not write them.
- `repos` is installed, and `/opt/repos/etc/manifest.toml` has `[resources]`
  with `cpu_weight = 50`, `memory_max = "2G"`, and `io_weight = 50`.

Postconditions:

- The `certificate` step found nothing to renew and changed nothing.
- The `slices` step ran: the three slice units and nginx's drop-in are
  written, sized from the host's memory, systemd was reloaded, and nginx
  was restarted once, into `ikigenba-core.slice`.
- Nothing from `nginx.conf` on ran: `/etc/nginx/conf.d/ikigenba.conf`,
  `/var/lib/ikigenba/services.json`, `/etc/litestream.yml`, and the timers
  are as they were, and nginx was not reloaded.
- Running `init` again before the fix stops at the same place with the same
  output; the slice units and drop-in are present and unchanged, so systemd
  is not reloaded for them and nginx is neither reloaded nor restarted.
- No app changed: every installed app's unit and environment file are as they
  were, and none was restarted, `repos` included.
- Once `/opt/repos/etc/manifest.toml` is valid, `init` runs to the end. A
  `memory_max` of `2G`, more than the apps slice's 1024M on a t3.small, is
  not `init`'s to refuse, though `activate` refuses a release that asks for
  it.

## An agent initialises a host holding an app whose data has not moved

An app laid out per app while a service's `state/` still lived under
`/opt/<name>/` keeps it there, and its unit still runs it from there; no
command moves the data or rewrites that unit. Everything `init` generates from the installed apps would otherwise name data that is not where
it says, so the check runs where the manifest check does, at the `nginx.conf`
step, the first that reads the installed apps, and stops `init` before
anything they generate is rewritten. The `slices` step, which reads no app,
has already run. The line names the app. When several installed apps' `state/` is
still under `/opt/<name>/`, only the first in name order is reported, as the
manifest check reports one fault. The same check covers an app whose
environment file has not moved (the next story). A `cache/` left
under `/opt/<name>/` with no `state/` beside it is no reason to refuse.

Command:

```
$ sudo opsctl init; echo "exit $?"
```

Output:

```
nginx: ok (/usr/sbin/nginx)
certbot: ok (/usr/bin/certbot)
systemctl: ok (/usr/bin/systemctl)
litestream: ok (/usr/bin/litestream)
git: ok (/usr/bin/git)
dns.provider: ok (route53)
dns.zones: ok (ikigenba.dev)
host.name: ok (sbx.ikigenba.dev)
timeouts: ok (drain 5s, stop 10s)
zone ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
host sbx.ikigenba.dev: ok (zone ikigenba.dev)
wildcard sbx.ikigenba.dev: ok (77.112.106.79)
opsctl: crm: /opt/crm/state has not moved
exit 1
```

Exits 1. The `ok` lines are on stdout; the `opsctl:` line is on stderr.

Preconditions:

- Every preflight check passes, and the host's certificate is not due for
  renewal.
- The host is a per-app host: `/opt/ikigenba/current` does not exist.
- `crm` and `dashboard` are installed, each with a valid manifest, and their
  sockets are listening.
- `/opt/crm/` holds `bin/`, `etc/`, `share/`, and `state/`, and
  `/var/opt/ikigenba/crm/` does not exist. `crm`'s environment file has not
  moved either: it is `/opt/crm/etc/env`, and `/etc/opt/ikigenba/crm/` does
  not exist.
- `dashboard`'s `state/` is under `/var/opt/ikigenba/dashboard/`, and its
  environment file is `/etc/opt/ikigenba/dashboard/env`.

Postconditions:

- The `certificate` step found nothing to renew and changed nothing. The
  `slices` step ran as on any host.
- Nothing from `nginx.conf` on ran: `/etc/nginx/conf.d/ikigenba.conf`,
  `/var/lib/ikigenba/services.json`, `/etc/litestream.yml`, and the timers
  are as they were, and nginx was not reloaded.
- No app changed: every installed app's unit and environment file are as
  they were, `dashboard`'s included, and none was restarted. `crm` is still running as
  it was.
- Nothing under `/opt/crm/` was moved or removed, and
  `/var/opt/ikigenba/crm/` was not created.
- Running `init` again while `/opt/crm/state` exists stops at the same place
  with the same output. Once `crm`'s `state/` is under
  `/var/opt/ikigenba/crm/` and its environment file is
  `/etc/opt/ikigenba/crm/env`, `init` runs to the end.

## An agent initialises a host holding an app whose environment file has not moved

An app laid out per app before environment files lived under
`/etc/opt/ikigenba/` keeps its file at `/opt/<name>/etc/env`, and its unit
still names it there; no command writes the new file for it. On a per-app
host the `apps` step rewrites every installed app's unit to name
`/etc/opt/ikigenba/<name>/env` and writes the store's values into that file,
but it reads no parameter, so it cannot write the secrets a missing file
needs; a unit naming a file that is
not there would not start. So an installed app with no
`/etc/opt/ikigenba/<name>/env` stops `init` the way an app whose data has not
moved does: at the `nginx.conf` step, before anything generated from the
installed apps is rewritten, with a line that names the app. It is the same
check as the one for data. Of the installed apps whose `state/` or environment
file has not moved, only the first in name order is reported, and an app
whose `state/` has not moved either is reported for its `state/`.

Command:

```
$ sudo opsctl init; echo "exit $?"
```

Output:

```
nginx: ok (/usr/sbin/nginx)
certbot: ok (/usr/bin/certbot)
systemctl: ok (/usr/bin/systemctl)
litestream: ok (/usr/bin/litestream)
git: ok (/usr/bin/git)
dns.provider: ok (route53)
dns.zones: ok (ikigenba.dev)
host.name: ok (sbx.ikigenba.dev)
timeouts: ok (drain 5s, stop 10s)
zone ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
host sbx.ikigenba.dev: ok (zone ikigenba.dev)
wildcard sbx.ikigenba.dev: ok (77.112.106.79)
opsctl: crm: /opt/crm/etc/env has not moved
exit 1
```

Exits 1. The `ok` lines are on stdout; the `opsctl:` line is on stderr.

Preconditions:

- Every preflight check passes, and the host's certificate is not due for
  renewal.
- The host is a per-app host: `/opt/ikigenba/current` does not exist.
- `crm` and `dashboard` are installed, each with a valid manifest, and their
  sockets are listening. Both have their `state/` under
  `/var/opt/ikigenba/<name>/`.
- `crm`'s unit names `/opt/crm/etc/env`, which exists, and
  `/etc/opt/ikigenba/crm/` does not exist. `dashboard`'s environment file is
  `/etc/opt/ikigenba/dashboard/env`.

Postconditions:

- The `certificate` step found nothing to renew and changed nothing. The
  `slices` step ran as on any host.
- Nothing from `nginx.conf` on ran: `/etc/nginx/conf.d/ikigenba.conf`,
  `/var/lib/ikigenba/services.json`, `/etc/litestream.yml`, and the timers
  are as they were, and nginx was not reloaded.
- No app changed: every installed app's unit and environment file are as
  they were, `dashboard`'s included, and none was restarted. `crm` is still
  running as it was, from `/opt/crm/etc/env`.
- `/etc/opt/ikigenba/crm/` was not created.
- Running `init` again while `/etc/opt/ikigenba/crm/env` does not exist
  stops at the same place with the same output. Once it exists, `init` runs
  to the end.

## An operator sets a stop timeout no longer than the drain deadline

A stop timeout that is not greater than the drain deadline would let systemd
kill an app still draining, so the preflight refuses it like any other fact
about the host that is wrong, and nothing in the sequence runs.

Command:

```
$ sudo opsctl config set apps.drain_seconds=30
$ sudo opsctl init; echo "exit $?"
```

Output: the lines of the ready host, with the `timeouts` line reading

```
timeouts: failed: apps.stop_seconds (10) is not greater than apps.drain_seconds (30)
```

and `exit 2`.

Exits 2. The lines are on stdout; stderr is empty. A value that is not a
positive whole number — `0`, `-1`, `2.5`, `ten` — gives
`timeouts: failed: apps.drain_seconds is not a positive whole number of
seconds: '2.5'`, naming whichever key holds it.

Preconditions:

- `apps.stop_seconds` is unset, so its default `10` applies, and every other
  check passes.

Postconditions:

- Nothing has changed. No setup command ran, no app's environment file or
  unit was rewritten, the services file was not rewritten, and no app was
  restarted.

## An operator gives init an argument

`init` has nothing to name and nothing to choose. An argument means the
operator expected a different command.

Command:

```
$ sudo opsctl init sbx.ikigenba.dev
```

```
$ sudo opsctl init --force
```

Output:

```
opsctl: init takes no arguments

see 'opsctl init --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed. No check ran.

## An agent initialises a host whose config file is corrupt

This is the one case where `init` writes no report at all. Only the PATH
lookups could run; every check from `dns.provider` down reads the store, so
the report would be missing everything it is for. A corrupt store is a failure
of the command, not a finding about the host, so it goes to stderr and stdout
stays empty.

Command:

```
$ sudo opsctl init; echo "exit $?"
```

Output:

```
opsctl: /etc/ikigenba/config.json is corrupt
exit 1
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `/etc/ikigenba/config.json` exists and is not a JSON object whose values
  are all strings.

Postconditions:

- Nothing has changed. The corrupt file is exactly as it was.
