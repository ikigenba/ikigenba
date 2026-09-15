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
| `host.name` | the fully-qualified name this host answers at, at or under a configured zone, e.g. `ikigenba.dev` |

A host answers at one name. The records a bootstrap created are `<host.name>`
and `*.<host.name>`, and the wildcard certificate, the nginx catch-all, and
every app's own name all hang off it.

The sequence is the setup commands that exist. It is empty until a group adds
one to it, and each group that does says so; `certificates.md` adds
`certificate`, `nginx.md` adds `nginx.conf`, and `backup.md` adds `litestream`
and `timers`, in that order. Every setup command is idempotent, so `init` is
too, and a step's inputs are read from the store every run — which is why
changing a period or a zone is `config set` followed by `init`, and never an
edit to something `init` generated. From the developer's machine that pair is
`devctl space init`, which sets the keys `create` set and runs `init` again;
`create` runs it once and `space init` runs it on any later day. Two of the
generated files also answer to
what is under `/opt`, and `install` and `restore` regenerate those themselves
when they change it (see `apps.md` and `backup.md`); `init` remains the only
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
  zone NAME                  every configured zone is reachable and delegated
  host NAME                  host.name lies at or under a configured zone
  wildcard NAME              host.name and _opsctl-preflight.host.name resolve alike

Sequence:
  certificate  obtain the host's certificate, or renew it if it is due
  nginx.conf   generate /etc/nginx/conf.d/ikigenba.conf and reload nginx
  litestream   generate /etc/litestream.yml and enable litestream.service
  timers       write the backup units, enabling each timer whose period is set

Configuration keys:
  host.name  the fully-qualified name this host answers at, at or under a configured zone
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
litestream: ok (/usr/local/bin/litestream)
dns.provider: ok (route53)
dns.zones: ok (ikigenba.dev)
host.name: ok (ikigenba.dev)
zone ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
host ikigenba.dev: ok (zone ikigenba.dev)
wildcard ikigenba.dev: ok (77.112.106.79)
exit 0
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `nginx`, `certbot`, `systemctl`, and `litestream` are on the host's PATH.
- `dns.provider`, `dns.zones`, and `host.name` are set, and the host's
  credentials can read the configured zone.
- Public DNS delegates the zone, and `ikigenba.dev` and
  `_opsctl-preflight.ikigenba.dev` resolve to the same address.

Postconditions:

- The setup sequence has run: the host holds its certificate, the nginx file
  generated from the store and what is under `/opt`, `/etc/litestream.yml`
  naming every declared database with `litestream.service` enabled, and the two
  backup unit pairs with each timer enabled whose period the store gives as
  non-zero.
- Every setup command is idempotent, so a host that was already set up is
  unchanged by the run.

The wildcard line is the bootstrap's own done-condition — the apex and the
wildcard both point at this host — checked from the host. It resolves a fixed
probe label rather than a literal `*`, because a resolver will refuse a
literal `*.ikigenba.dev` while `_opsctl-preflight.ikigenba.dev` resolves to
the apex address. Whether that address is *this* host's cannot be known
without asking the cloud, which opsctl never does; that the two lookups agree
is the check.

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
litestream: ok (/usr/local/bin/litestream)
dns.provider: ok (route53)
dns.zones: ok (ikigenba.dev)
host.name: failed: not set
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
$ sudo opsctl config set host.name=ikigenba.dev
$ sudo opsctl init; echo "exit $?"
```

Output: the ten `ok` lines of the ready host, and `exit 0`.

Preconditions:

- The two failures the previous run reported have been fixed.

Postconditions:

- The setup sequence has run. Running `init` a third time produces byte for
  byte the same output and the same exit code: the whole command is
  idempotent, and a preflight that passes writes nothing of its own.

## An operator gives init an argument

`init` has nothing to name and nothing to choose. An argument means the
operator expected a different command.

Command:

```
$ sudo opsctl init ikigenba.dev
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
