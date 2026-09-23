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
`certificate`, `S5-nginx.md` adds `nginx.conf`, `S8-backup.md` adds `litestream`
and `timers`, and `S7-apps.md` adds `apps`, in that order. Every setup command is idempotent, so `init` is
too, and a step's inputs are read from the store every run — which is why
changing a period or a zone is `config set` followed by `init`, and never an
edit to something `init` generated. From the developer's machine that pair is
`devctl space init`, which sets the keys `create` set and runs `init` again;
`create` runs it once and `space init` runs it on any later day. Two of the
generated files also answer to
what is under `/opt`, and the nginx file to which apps are disabled as well;
`install`, `uninstall`, `restore`, `disable`, and `enable` regenerate those
themselves when they change what they answer to (see `S7-apps.md` and
`S8-backup.md`); `init` remains the only
command that enables the units behind them.

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
  dns.provider, dns.zones    set, and the provider opens (see 'opsctl dns --help')
  host.name                  set
  timeouts                   apps.drain_seconds and apps.stop_seconds are positive
                             whole seconds, stop greater than drain
  zone NAME                  every configured zone is reachable and delegated
  host NAME                  host.name lies at or under a configured zone
  wildcard NAME              host.name and _opsctl-preflight.host.name resolve alike

Sequence:
  certificate  obtain the host's certificate, or renew it if it is due
  nginx.conf   generate /etc/nginx/conf.d/ikigenba.conf and reload nginx
  litestream   generate /etc/litestream.yml and enable litestream.service
  timers       write the backup and renewal units, enabling each backup timer
               whose period is set and the renewal timer always
  apps         write the drain and stop settings into every installed app,
               restarting each enabled app whose settings changed; a
               disabled app is rewritten and left disabled

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

- `nginx`, `certbot`, `systemctl`, and `litestream` are on the host's PATH.
- `dns.provider`, `dns.zones`, and `host.name` are set, and the host's
  credentials can read the configured zone.
- `apps.drain_seconds` and `apps.stop_seconds` are unset, so the defaults
  apply.
- Public DNS delegates the zone, and `sbx.ikigenba.dev` and
  `_opsctl-preflight.sbx.ikigenba.dev` resolve to the same address.

Postconditions:

- The setup sequence has run: the host holds its certificate, the nginx file
  generated from the store and what is under `/opt`, `/etc/litestream.yml`
  naming every declared database with `litestream.service` enabled, the two
  backup unit pairs with each timer enabled whose period the store gives as
  non-zero, and the certificate renewal pair with its timer enabled.
- Every installed app's `etc/env` holds `DRAIN_SECONDS=5` and its service
  unit a stop timeout of `10` seconds. An app that already held those values
  was not restarted.
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

- Nothing has changed. No setup command ran, no file under `/etc` or `/opt`
  was created, modified, or removed.

## An operator runs init again after fixing what it found

Command:

```
$ sudo dnf install -y certbot
$ sudo opsctl config set host.name=sbx.ikigenba.dev
$ sudo opsctl init; echo "exit $?"
```

Output: the eleven `ok` lines of the ready host, and `exit 0`.

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The two failures the previous run reported have been fixed.

Postconditions:

- The setup sequence has run. Running `init` a third time produces byte for
  byte the same output and the same exit code: the whole command is
  idempotent, and a preflight that passes writes nothing of its own.

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

Output: the eleven `ok` lines of the ready host, with the `timeouts` line
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
  service units stop with a timeout of `30` seconds. systemd has been
  reloaded.
- `ikigenba-crm.service` and `ikigenba-dashboard.service` were restarted,
  so each now runs with the new values. Their sockets were not restarted.
- `notes` was not started, restarted, or enabled: both its units are still
  disabled and inactive and its names still answer `503`. When it is enabled
  it starts with the new values.
- `/opt/gmail/` was not touched and no unit was written for it.
- Running `init` again writes the same values, restarts no app, and prints
  the same lines.

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
  rewritten, and no app was restarted.

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

This is the one case where `init` writes no report at all. Only the three PATH
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
