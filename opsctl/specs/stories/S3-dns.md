# Stories — dns

opsctl is the only thing on the host that writes DNS records. It owns the
zones named in its configuration and nothing outside them, and it reaches them
through one provider at a time — Route 53 is the first and only one.

The top-level usage gains the line `  dns       manage DNS records in the
zones opsctl owns` under `Commands:`.

Two configuration keys, both set before any `dns` subcommand will run:

| key | value |
|---|---|
| `dns.provider` | the active provider; only `route53` exists |
| `dns.zones` | comma-separated `NAME:ID` pairs of the zones opsctl owns, e.g. `ikigenba.dev:Z09565073GHK8BYWQ1A78` |

The zone id is the provider's and a person supplies it; opsctl never reads it
from anywhere but the store, and never from a bootstrap's own files such as
`/etc/ikigenba/env`. A record name is mapped to a zone by the longest of the
configured names that matches it on a label boundary, so a caller that has
only a domain — certbot, above all — needs no zone argument. The zone is the
root domain, `ikigenba.dev`, shared by every space; which names inside it this
host may write is the host's role's business, not opsctl's, and a name is
never refused for lying outside `host.name`.

The verbs are `add` and `remove`, never an overwrite. A wildcard certificate's
DNS-01 challenge puts two TXT values at one name at the same time, and an
overwrite would destroy one of them.

## An agent asks what `dns` can do

Command:

```
$ opsctl dns --help
```

```
$ opsctl dns -h
```

Output:

```
Usage: opsctl dns <subcommand> [options] [arguments]

Manage DNS records in the zones opsctl owns, through the configured provider.

Subcommands:
  list ZONE               print every record in ZONE, one line per value
  add NAME TYPE VALUE     add VALUE to the record NAME/TYPE, creating it if absent
  remove NAME TYPE VALUE  remove VALUE from the record NAME/TYPE; succeeds if absent
  check                   verify every configured zone is reachable and delegated
  acme-auth               certbot --manual-auth-hook: add the DNS-01 challenge record
  acme-cleanup            certbot --manual-cleanup-hook: remove the challenge record

Options (add, remove, acme-auth, acme-cleanup):
  --timeout DURATION  how long to wait for the change to be live (default 2m)
  --ttl SECONDS       TTL when add creates a record (default 300; acme-auth uses 60)

Configuration keys:
  dns.provider  the active provider; only 'route53' is supported
  dns.zones     comma-separated NAME:ID pairs of the zones opsctl owns

NAME is mapped to a zone by longest suffix match against the zone names.
```

Exits 0. The text is on stdout; stderr is empty. It prints for any user.

Preconditions:

- `opsctl` is installed on the host.

Postconditions:

- Nothing has changed.

## An operator checks the host can reach its zones

The first thing to know about a new host is whether the DNS it was given is
real: the provider answers for the zone, and public DNS delegates that zone to
the nameservers the provider names. One line per configured zone.

Command:

```
$ sudo opsctl dns check
```

Output:

```
ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `dns.provider` is `route53` and `dns.zones` is
  `ikigenba.dev:Z09565073GHK8BYWQ1A78`.
- The host's credentials can read that zone.
- Public DNS delegates `ikigenba.dev` to the zone's four nameservers.

Postconditions:

- Nothing has changed. No record was written.

## An operator checks zones and one of them is wrong

A failing zone is a fact about the zone, not about the command, so it is part
of the report on stdout and every remaining zone is still checked — an
operator sees every problem at once rather than one per run. The exit code
says the report contains a failure.

Command:

```
$ sudo opsctl dns check; echo "exit $?"
```

Output:

```
ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
ikigenba.net: failed: nameservers not delegated
exit 1
```

Exits 1. The lines are on stdout; stderr is empty.

Preconditions:

- Two zones are configured.
- The registrar has not been pointed at the second zone's nameservers.

Postconditions:

- Nothing has changed.

## An operator reads a zone

One line per value, `NAME TYPE TTL VALUE`, sorted by name, then type, then
value, with the value the rest of the line. Names carry no trailing dot and a
literal `*`; TXT values are unquoted, so what is printed is what `add` and
`remove` take.

Command:

```
$ sudo opsctl dns list ikigenba.dev
```

Output:

```
*.sbx.ikigenba.dev A 60 77.112.106.79
ikigenba.dev A 60 77.112.106.79
ikigenba.dev NS 172800 ns-132.awsdns-16.com
ikigenba.dev NS 172800 ns-1653.awsdns-14.co.uk
ikigenba.dev SOA 900 ns-132.awsdns-16.com. awsdns-hostmaster.amazon.com. 1 7200 900 1209600 86400
sbx.ikigenba.dev A 60 77.112.106.79
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- `ikigenba.dev` is a configured zone the host's credentials can read.
- The zone holds one space, `sbx`, whose two records `devctl space create`
  wrote, and the apex record `devctl apex set` pointed at the same address.
  The whole zone is listed, every space's records included: reading it is
  not limited to this host's own names.

Postconditions:

- Nothing has changed.

## An operator adds a record and takes it away again

Proving by hand that the seam reaches the real service: add a value, see it,
remove it. Neither verb prints anything — the answer is the exit code — and
both return only once the change is live at the provider.

Command:

```
$ sudo opsctl dns add _probe.ikigenba.dev TXT hello
$ sudo opsctl dns list ikigenba.dev | grep _probe
_probe.ikigenba.dev TXT 300 hello
$ sudo opsctl dns remove _probe.ikigenba.dev TXT hello
```

Output:

```
```

Both verbs exit 0. Nothing is on stdout or stderr.

Preconditions:

- `ikigenba.dev` is a configured zone the host's credentials can write.

Postconditions:

- After `add`, `_probe.ikigenba.dev TXT` holds `hello` and a 300 second TTL,
  and every other value at that name is untouched. `add` of a value already
  present changes nothing and still exits 0.
- After `remove`, that record set is gone, since `hello` was its last value.
  `remove` of a value that is not there changes nothing and still exits 0.
- No other record in the zone has changed.

## An operator names a record in a zone the host does not own

A host writes only inside the zones it was configured with. A name outside
them is the operator's mistake, so the refusal lists what the host does own.

Command:

```
$ sudo opsctl dns add _probe.example.org TXT hello; echo "exit $?"
```

Output:

```
opsctl: no configured zone contains '_probe.example.org'

configured zones: ikigenba.dev
exit 2
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `ikigenba.dev` is the only configured zone.

Postconditions:

- Nothing has changed. The provider was not called.

## An operator lists a zone that is not configured

Command:

```
$ sudo opsctl dns list example.org
```

Output:

```
opsctl: zone not configured: example.org

configured zones: ikigenba.dev
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `ikigenba.dev` is the only configured zone.

Postconditions:

- Nothing has changed.

## certbot asks for a challenge record

certbot runs the auth hook once per challenge with `CERTBOT_DOMAIN` set to the
authorization identifier — the bare zone for a wildcard, never `*.` — and
`CERTBOT_VALIDATION` set to the token. A wildcard certificate covers both
`<name>` and `*.<name>`, and the CA issues one token for each, both at the
same record name, so the second hook run must add a value beside the first,
not replace it. The TTL is 60 seconds so the record expires quickly after the
challenge. The hook waits for the value to be live before it returns, because
certbot asks the CA to look the moment it comes back.

When the host holds the apex (see `S6-certificates.md`), certbot runs the hook
a third time with `CERTBOT_DOMAIN=ikigenba.dev`, and the record goes to
`_acme-challenge.ikigenba.dev`: a name outside the host's own subtree, mapped
to the same zone by the same suffix rule, and written with the same verb.

Command:

```
$ CERTBOT_DOMAIN=sbx.ikigenba.dev CERTBOT_VALIDATION=6ukiLNXA opsctl dns acme-auth
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `ikigenba.dev` is a configured zone the host's credentials can write.
- certbot is running the hook.

Postconditions:

- `_acme-challenge.sbx.ikigenba.dev TXT` holds `6ukiLNXA` with a 60 second
  TTL, beside any value already there, and the change is live at the
  provider.
- Nothing else in the zone has changed.

## certbot cleans a challenge record up

The cleanup hook runs later with the same environment and takes away exactly
the one value it was given, leaving any other value at that name — the other
half of the wildcard's pair, mid-flight — alone.

Command:

```
$ CERTBOT_DOMAIN=sbx.ikigenba.dev CERTBOT_VALIDATION=6ukiLNXA opsctl dns acme-cleanup
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `ikigenba.dev` is a configured zone the host's credentials can write.

Postconditions:

- `6ukiLNXA` is no longer a value of `_acme-challenge.sbx.ikigenba.dev TXT`.
  If it was the last value, the record set is gone; if not, the rest remain.
- Cleanup of a value that is already gone changes nothing and exits 0.

## An operator runs a certbot hook by hand

The hooks take their arguments from certbot's environment and nowhere else.
Run without it there is nothing to do, and the message says what the command
is for, because a non-zero hook exit is only written to certbot's log and has
to explain itself there.

Command:

```
$ sudo opsctl dns acme-auth
```

Output:

```
opsctl: acme-auth must be run by certbot as --manual-auth-hook
```

Exits 2. The line is on stderr; stdout is empty. `acme-cleanup` says the same
of `--manual-cleanup-hook`.

Preconditions:

- `CERTBOT_DOMAIN` or `CERTBOT_VALIDATION` is unset or empty.

Postconditions:

- Nothing has changed.

## A change does not go live in time

`add` and `remove` return once the provider reports the change live, which
takes about thirty seconds on Route 53. When it has not by the deadline, the
command reports that it could not confirm it — not that it failed, because the
change may yet land.

Command:

```
$ sudo opsctl dns add --timeout 10s _probe.ikigenba.dev TXT hello
```

Output:

```
opsctl: change to _probe.ikigenba.dev not confirmed within 10s
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- `ikigenba.dev` is a configured zone the host's credentials can write.
- The provider has not reported the change live within ten seconds.

Postconditions:

- The change was submitted and will very likely become live. Nothing was
  rolled back.

## An agent runs a dns subcommand on an unconfigured host

Before `init` has been run, or on a host whose store was never written, there
is nothing for `dns` to talk to. Every subcommand says which key is missing.

Command:

```
$ sudo opsctl dns check
```

Output:

```
opsctl: dns.provider not set
```

Exits 1. The line is on stderr; stdout is empty. With the provider set and
the zones not, the line is `opsctl: dns.zones not set`; with a `dns.zones`
entry that is not a `NAME:ID` pair, it is `opsctl: dns.zones malformed:` and
the offending entry quoted.

Preconditions:

- `dns.provider` is unset or empty.

Postconditions:

- Nothing has changed. No provider was opened and no network call was made.

## An operator runs `dns` with no subcommand, or one that does not exist

Command:

```
$ sudo opsctl dns
```

Output:

```
opsctl: no dns subcommand given

see 'opsctl dns --help' for usage
```

Command:

```
$ sudo opsctl dns update _probe.ikigenba.dev TXT hello
```

Output:

```
opsctl: unknown dns subcommand 'update'

see 'opsctl dns --help' for usage
```

Both exit 2. The text is on stderr; stdout is empty.

Preconditions:

- `opsctl` is running as root.

Postconditions:

- Nothing has changed.
