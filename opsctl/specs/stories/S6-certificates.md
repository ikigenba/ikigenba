# Stories — certificates

The host answers at `<host.name>` and at every name under it, so it needs one
certificate covering `<host.name>` and `*.<host.name>`. A wildcard can only be
proved over DNS, and opsctl is already the one thing on the host that writes
DNS records, so certbot proves the challenge through opsctl's own hooks. The
certificate lives where certbot puts it, `/etc/letsencrypt/live/<host.name>/`,
and that directory is backed up.

The one host that holds the root domain's apex (see `S5-nginx.md`) answers at
`ikigenba.dev` as well, and the same certificate covers that name too: when
`host.apex` is set, `obtain` asks for three names instead of two, and the
third is proved at `_acme-challenge.ikigenba.dev`, a record outside the
host's own subtree that the host's role allows only while it holds the apex.
Setting or clearing `host.apex` changes the names the certificate should
carry, and `obtain` is how the change lands: certbot reissues a certificate
whose requested names differ from the ones it holds, whether or not it is due.
There is one lineage, named after `host.name`, whatever it covers.

The top-level usage gains the line `  cert      obtain and inspect the host's
certificate` under `Commands:`, and `init`'s sequence gains the step
`certificate`, before `nginx.conf` — every nginx server block names the
certificate, so nginx has nothing to load until this has run.

Renewal has an owner, and it is opsctl. `init`'s `timers` step writes
`ikigenba-renew-certificate.service`, which runs `certbot renew` as root, and
`ikigenba-renew-certificate.timer`, which fires it twice a day at a random
offset and is `Persistent=true`, so a host that was off when a firing was due
runs it at boot. The timer has no period key and is always enabled: a host
that holds a certificate has to renew it, and there is nothing for a space
to opt out of. The same step masks the certbot package's own
`certbot-renew.timer` when the package ships one, so exactly one thing on the
host renews and the answer to "who renews" does not depend on packaging.
What renewal does is still certbot's: the hooks `obtain` recorded re-prove
the challenge through opsctl and reload nginx, for every name the lineage
holds.

One configuration key:

| key | value |
|---|---|
| `acme.email` | the address the CA sends expiry warnings to, e.g. `ops@ikigenba.dev` |

## An operator asks what `cert` can do

Command:

```
$ opsctl cert --help
```

```
$ opsctl cert -h
```

Output:

```
Usage: opsctl cert <subcommand>

Obtain and inspect the one certificate this host serves: host.name and
*.host.name, plus the parent of host.name when host.apex is set, proved over
DNS-01 through 'opsctl dns acme-auth'.

Subcommands:
  show    print the certificate's names, issuer, and expiry
  obtain  obtain the certificate, renew it if it is due, or reissue it when
          the names it should carry have changed

Configuration keys:
  acme.email  the address the CA sends expiry warnings to
  host.name   the fully-qualified name this host answers at
  host.apex   the app that answers at the parent of host.name; unset means none

Renewal is certbot's: 'certbot renew' re-runs the same hooks and reloads
nginx, with no further configuration. 'opsctl init' writes the timer that
runs it twice a day, ikigenba-renew-certificate.timer.
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An operator obtains the host's certificate

The wildcard forces DNS-01, and DNS-01 with two names issues two challenge
tokens at one record name, which is exactly what `dns acme-auth` was shaped
for. Nothing is printed; the answer is the exit code, and it takes a couple
of minutes because each challenge waits for its record to be live.

Command:

```
$ sudo opsctl cert obtain
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `host.name` is `sbx.ikigenba.dev`, `acme.email` is set, `host.apex` is not
  set, and `certbot` is on the PATH.
- `opsctl dns check` reports the zone holding `host.name` as ok.
- The host can reach the CA.

Postconditions:

- `/etc/letsencrypt/live/sbx.ikigenba.dev/fullchain.pem` and `privkey.pem`
  exist, covering `sbx.ikigenba.dev` and `*.sbx.ikigenba.dev`.
- certbot's renewal configuration for that certificate records the same
  hooks, so `certbot renew` run by anyone re-proves the challenge through
  opsctl and reloads nginx afterwards, with nothing further to configure. The
  one that runs it on schedule is `ikigenba-renew-certificate.timer`, which
  `init`'s `timers` step writes and enables. `devctl space start` runs it once
  more over ssh when a space comes back from a stop: the persistent timer
  would catch up at boot on its own, and start runs it in the foreground so
  the developer sees the answer.
- No challenge record is left behind: every value `acme-auth` added has been
  taken away by `acme-cleanup`.

## An operator obtains a certificate that is already current

certbot will not ask the CA for a certificate that has plenty of life left,
and opsctl does not either. Re-running is how a setup sequence is safe to
re-run.

Command:

```
$ sudo opsctl cert obtain; echo "exit $?"
```

Output:

```
exit 0
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- The certificate exists, covers exactly the names the store asks for, and
  is not yet due for renewal.

Postconditions:

- Nothing has changed. The certificate on disk is the one that was there, no
  DNS record was written, and the CA was not asked for anything.

## An operator obtains the certificate after the apex changed

`devctl apex set` has just set `host.apex` on this host, regenerated the
host's role so it may write `_acme-challenge.ikigenba.dev`, and now runs
this over ssh before it points the apex record here — the certificate has to
carry the name before the name resolves to this host, or the first visitor
sees a handshake for the wrong names. The certificate on disk is current,
but it covers two names and the store asks for three, so certbot reissues it
rather than keeping it. Three challenges run instead of two, the third at the
apex's own record name.

The reverse is the same command: after `devctl apex clear` has removed the
key, `obtain` reissues with two names, so the lineage stops carrying a name
the host's narrowed role can no longer prove and the next `certbot renew`
does not fail on it.

Command:

```
$ sudo opsctl cert obtain
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `host.name` is `sbx.ikigenba.dev`, `acme.email` is set, and `host.apex` is
  `crm`.
- The certificate exists, covers `sbx.ikigenba.dev` and `*.sbx.ikigenba.dev`,
  and is not due for renewal.
- The host's role may write `_acme-challenge.ikigenba.dev`.

Postconditions:

- `/etc/letsencrypt/live/sbx.ikigenba.dev/` holds a new certificate covering
  `sbx.ikigenba.dev`, `*.sbx.ikigenba.dev`, and `ikigenba.dev`, under the same
  lineage name and with the same recorded hooks.
- `cert show` lists the three names. `certbot renew` renews all three.
- No challenge record is left behind at either record name.
- Running `obtain` again changes nothing: the names now match.

## An operator reads the certificate the host is serving

Command:

```
$ sudo opsctl cert show
```

Output:

```
names: sbx.ikigenba.dev, *.sbx.ikigenba.dev
issuer: R11
expires: 2026-12-11T14:02:55Z
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev` and the certificate exists, covering the
  two names.

Postconditions:

- Nothing has changed. On the host that holds the apex, the first line reads
  `names: sbx.ikigenba.dev, *.sbx.ikigenba.dev, ikigenba.dev`. The names are
  read from the certificate, not from the store, so a store that has changed
  since the last `obtain` is visible here as a mismatch.

## An operator reads the certificate on a host that has none

Command:

```
$ sudo opsctl cert show
```

Output:

```
opsctl: no certificate for sbx.ikigenba.dev
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `host.name` is `sbx.ikigenba.dev`.
- `/etc/letsencrypt/live/sbx.ikigenba.dev/` does not exist.

Postconditions:

- Nothing has changed.

## An operator obtains a certificate and the CA refuses

The CA is another program on the far end of a network, and what it said is
the only useful thing anyone has. certbot's output follows the error line,
each line quoted with `> `.

Command:

```
$ sudo opsctl cert obtain
```

Output:

```
opsctl: certbot certonly: exit status 1

> Some challenges have failed.
> Detail: DNS problem: NXDOMAIN looking up TXT for _acme-challenge.sbx.ikigenba.dev
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `host.name` and `acme.email` are set, and `certbot` is on the PATH.
- The CA could not see the challenge record, or refused for any other reason.
  On a host whose `host.apex` is set but whose role was not widened to the
  apex's challenge record, the auth hook itself fails at that record, and
  certbot's quoted output says so.

Postconditions:

- No certificate was written. An earlier certificate, if there was one, is
  exactly as it was and nginx is still serving it.
- Any challenge record certbot's cleanup hook reached has been removed.

## An agent obtains a certificate on an unconfigured host

Command:

```
$ sudo opsctl cert obtain
```

Output:

```
opsctl: acme.email not set
```

Exits 1. The line is on stderr; stdout is empty. With `host.name` unset the
line is `opsctl: host.name not set`.

Preconditions:

- `acme.email` is unset or empty.

Postconditions:

- Nothing has changed. certbot was not run.

## An agent obtains with an apex the host name cannot carry

The same refusal `nginx` makes, for the same reason: a `host.apex` on a host
whose name has no parent domain asks for a name that does not exist, and the
CA is not asked for it.

Command:

```
$ sudo opsctl cert obtain
```

Output:

```
opsctl: host.apex is set but host.name 'ikigenba.dev' has no parent domain
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `host.name` is `ikigenba.dev`, `acme.email` is set, and `host.apex` is
  `crm`.

Postconditions:

- Nothing has changed. certbot was not run and no DNS record was written.

## An operator runs `cert` with no subcommand, or one that does not exist

Command:

```
$ sudo opsctl cert
```

Output:

```
opsctl: no cert subcommand given

see 'opsctl cert --help' for usage
```

Command:

```
$ sudo opsctl cert renew
```

Output:

```
opsctl: unknown cert subcommand 'renew'

see 'opsctl cert --help' for usage
```

Both exit 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed. Renewal is certbot's own, not a subcommand here; the
  timer `init` writes runs it, and `systemctl start
  ikigenba-renew-certificate.service` runs it now.
