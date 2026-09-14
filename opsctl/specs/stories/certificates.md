# Stories — certificates

The host answers at `<host.name>` and at every name under it, so it needs one
certificate covering `<host.name>` and `*.<host.name>`. A wildcard can only be
proved over DNS, and opsctl is already the one thing on the host that writes
DNS records, so certbot proves the challenge through opsctl's own hooks. The
certificate lives where certbot puts it, `/etc/letsencrypt/live/<host.name>/`,
and that directory is backed up.

The top-level usage gains the line `  cert      obtain and inspect the host's
certificate` under `Commands:`, and `init`'s sequence gains the step
`certificate`, before `nginx.conf` — every nginx server block names the
certificate, so nginx has nothing to load until this has run.

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
*.host.name, proved over DNS-01 through 'opsctl dns acme-auth'.

Subcommands:
  show    print the certificate's names, issuer, and expiry
  obtain  obtain the certificate, or renew it if it is due

Configuration keys:
  acme.email  the address the CA sends expiry warnings to
  host.name   the fully-qualified name this host answers at

Renewal is certbot's: 'certbot renew' re-runs the same hooks and reloads
nginx, with no further configuration.
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

- `host.name` and `acme.email` are set, and `certbot` is on the PATH.
- `opsctl dns check` reports the zone holding `host.name` as ok.
- The host can reach the CA.

Postconditions:

- `/etc/letsencrypt/live/<host.name>/fullchain.pem` and `privkey.pem` exist,
  covering `<host.name>` and `*.<host.name>`.
- certbot's renewal configuration for that certificate records the same
  hooks, so `certbot renew` run by anyone — a systemd timer on the host, or
  `devctl space start` over ssh — re-proves the challenge through opsctl and
  reloads nginx afterwards, with nothing further to configure.
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

- The certificate exists and is not yet due for renewal.

Postconditions:

- Nothing has changed. The certificate on disk is the one that was there, no
  DNS record was written, and the CA was not asked for anything.

## An operator reads the certificate the host is serving

Command:

```
$ sudo opsctl cert show
```

Output:

```
names: ikigenba.dev, *.ikigenba.dev
issuer: R11
expires: 2026-12-11T14:02:55Z
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `host.name` is `ikigenba.dev` and the certificate exists.

Postconditions:

- Nothing has changed.

## An operator reads the certificate on a host that has none

Command:

```
$ sudo opsctl cert show
```

Output:

```
opsctl: no certificate for ikigenba.dev
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `host.name` is `ikigenba.dev`.
- `/etc/letsencrypt/live/ikigenba.dev/` does not exist.

Postconditions:

- Nothing has changed.

## An operator obtains a certificate and the CA refuses

The CA is another program on the far end of a network, and what it said is
the only useful thing anyone has. certbot's output follows the error line.

Command:

```
$ sudo opsctl cert obtain
```

Output:

```
opsctl: certbot certonly: exit status 1

Some challenges have failed.
Detail: DNS problem: NXDOMAIN looking up TXT for _acme-challenge.ikigenba.dev
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `host.name` and `acme.email` are set, and `certbot` is on the PATH.
- The CA could not see the challenge record, or refused for any other reason.

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

- Nothing has changed. Renewal is certbot's own, not a subcommand here.
