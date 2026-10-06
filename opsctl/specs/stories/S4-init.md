# Stories — init

`init` is the one command an agent runs after the install and the `config set`
steps: it evaluates every prerequisite the platform needs, reports all of them
at once, and only then runs the setup sequence. It is safe to re-run, and it
is how a host says whether it is ready.

The top-level usage gains the line `  init      run the setup sequence behind
one preflight` under `Commands:`.

One configuration key:

| key | value |
|---|---|
| `host.name` | the fully-qualified name this host answers at, at or under a configured zone, e.g. `sbx.ikigenba.dev` |

Its preflight and its `apps` step also read `apps.drain_seconds` and
`apps.stop_seconds`, the two app timing keys `S7-apps.md` declares.

A host answers at one name: the space's, one label under the root domain.
The records a bootstrap created are `<host.name>` and `*.<host.name>`, and
the wildcard certificate, the nginx catch-all, and every app's own name all
hang off it. The one host that also answers at the root's apex says so with
`host.apex` (see `S5-nginx.md`); the preflight does not look at that key, and
a value the host cannot carry is refused by the `certificate` step, which is
the first step that reads it.

The sequence is the setup commands that exist. It is empty until a group adds
one to it, and each group that does says so; `S6-certificates.md` adds
`certificate`, this group adds `slices`, `S5-nginx.md` adds `nginx.conf`,
`S8-backup.md` adds `litestream` and `timers`, and `S7-apps.md` adds `apps`,
in that order. `slices` reads no manifest, so it comes before every step that
does: a host whose installed manifests the running opsctl refuses still gets
its slices, which `install` needs to judge a fixed release. Every setup
command is idempotent, so `init` is too, and a step's inputs are read from
the store every run — which is why
changing a period or a zone is `config set` followed by `init`, and never an
edit to something `init` generated. From the developer's machine that pair is
`devctl space init`, which sets the keys `create` set and runs `init` again;
`create` runs it once and `space init` runs it on any later day. Two of the
generated files also answer to
what is under `/opt`, and the nginx file to which apps are disabled as well.
So does `/var/lib/ikigenba/services.json`, the services file
(`S9-services.md`), which `init` rewrites every run without printing a line
for it;
`install`, `uninstall`, `restore`, `disable`, and `enable` regenerate those
themselves when they change what they answer to (see `S7-apps.md` and
`S8-backup.md`); `init` remains the only
command that enables the units behind them.

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
it may hold, is `S7-apps.md`'s; `install` checks an app against the slice unit
this step wrote, never against the host's memory.

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
  nginx.conf   generate /etc/nginx/conf.d/ikigenba.conf and reload nginx
  litestream   generate /etc/litestream.yml and enable litestream.service
  timers       write the backup and renewal units, enabling each backup timer
               whose period is set and the renewal timer always
  apps         write the drain and stop settings into every installed app,
               restarting each enabled app whose settings changed; a
               disabled app is rewritten and left disabled. The resources
               an app's manifest declares are kept as install wrote them;
               a manifest that is no longer valid stops init before
               any app is rewritten

Configuration keys:
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

`devctl space create` has installed opsctl, set the keys, and now asks the
host to check itself and finish its own setup. Every line is `ok`, so the
sequence ran and the exit code is 0.

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

Postconditions:

- The setup sequence has run: the host holds its certificate, the nginx file
  generated from the store and what is under `/opt`, the three slices and
  nginx's drop-in, `/etc/litestream.yml`
  naming every declared database with `litestream.service` enabled, the two
  backup unit pairs with each timer enabled whose period the store gives as
  non-zero, and the certificate renewal pair with its timer enabled.
- `/var/lib/ikigenba/services.json` has been rewritten from the store, what
  is under `/opt`, and which apps are disabled. No line reports it.
- Every installed app's `etc/env` holds `DRAIN_SECONDS=5` and
  `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json`, and its service unit a
  stop timeout of `10` seconds and the resource settings its installed
  manifest declares, or their defaults (`S7-apps.md`). An app that already
  held those values was not restarted.
- Every setup command is idempotent, so a host that was already set up is
  unchanged by the run.

The wildcard line is the bootstrap's own done-condition — the space's name
and its wildcard both point at this host — checked from the host. It resolves
a fixed probe label rather than a literal `*`, because a resolver will refuse
a literal `*.sbx.ikigenba.dev` while `_opsctl-preflight.sbx.ikigenba.dev`
resolves to the space's address. Whether that address is *this* host's cannot
be known without asking the cloud, which opsctl never does; that the two
lookups agree is the check.

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

- Nothing has changed. No setup command ran, no file under `/etc`, `/opt`, or
  `/var/lib/ikigenba` was created, modified, or removed.

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
installed, so every app `install` later places has a slice to go into and a
ceiling to be checked against. The step prints no line of its own; the run's
output is the ready host's.

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
recomputes them: `install` reads the slice units as they are.

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
every installed app and restarts only the apps whose values changed. A
restart refuses no request, because each app's socket keeps listening while
its service restarts (`S7-apps.md`).

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

- The host is ready, and `crm`, `dashboard`, and `notes` are installed with
  `DRAIN_SECONDS=5` and a stop timeout of `10` seconds. `notes` was disabled
  with `opsctl disable notes`.
- `crm`'s and `dashboard`'s sockets are listening, and neither is disabled.
- `/opt/gmail/` holds a `state/` and no `bin/gmail`: it is a service, not an
  installed app.

Postconditions:

- `/opt/crm/etc/env`, `/opt/dashboard/etc/env`, and `/opt/notes/etc/env`
  hold `DRAIN_SECONDS=20`; every other line in them is as it was. All three
  service units stop with a timeout of `30` seconds, and each still carries
  the resources its app's manifest declares, as install wrote them. systemd
  has been reloaded.
- `ikigenba-crm.service` and `ikigenba-dashboard.service` were restarted,
  so each now runs with the new values. Their sockets were not restarted.
- `notes` was not started, restarted, or enabled: both its units are still
  disabled and inactive and its names still answer `503`. When it is enabled
  it starts with the new values.
- `/opt/gmail/` was not touched and no unit was written for it.
- Running `init` again writes the same values, restarts no app, and prints
  the same lines.

## An agent initialises a host holding an app whose manifest is no longer valid

nginx, the services file, the backup configuration and the apps' units are
all generated from every installed app's manifest, so a manifest the running
opsctl refuses stops `init` at its `nginx.conf` step, the first that reads
them, before anything they generate is rewritten. This is the host just
upgraded from an opsctl that wrote no slices: the release it holds was
installed under the older opsctl and carries `io_weight`, a key this one does
not know. The `slices` step, which reads no manifest, has already run, so
the host has the slices that installing a fixed release needs. `init` judges the
manifest's form only. It does not check an app's `memory_max` against its
slice or warn about a slice that is oversubscribed; those are `install`'s.

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
other fault `install` would refuse in the manifest is reported the same way,
in `install`'s words, after `opsctl: <app>: etc/manifest.toml: `.

Preconditions:

- Every preflight check passes, and the host's certificate is not due for
  renewal.
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
- No app changed: every installed app's unit and `etc/env` are as they
  were, and none was restarted, `repos` included.
- The fix is to install a `repos` release whose manifest is valid, which
  `install` can judge now that the slices are there; `init` then runs to the
  end. A `memory_max` of `2G`, more than the apps slice's
  1024M on a t3.small, is not `init`'s to refuse, but that install refuses
  it.

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

- Nothing has changed. No setup command ran, no app's `etc/env` or unit was
  rewritten, `/var/lib/ikigenba/services.json` was not rewritten, and no app
  was restarted.

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
