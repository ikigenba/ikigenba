# Bootstrapping an ikigenba host

This document is written for an agent. A person will point you at it and
ask you to bootstrap a host. Read it whole before doing anything.

Bootstrap is finished when one thing is true: **a DNS name resolves to a
Linux host that you can reach as root over ssh**, with the wildcard under
that name resolving to the same host, and with the host holding credentials
that `opsctl` will later use for DNS and backups. That name is the host's
name — `setup.md` enters it as `host.name` — and this document calls it
`<name>`. Nothing about `opsctl` itself happens here. Installing and
configuring it is the next document, `setup.md`, which assumes this one is
done.

This document does not say how to create any of that. It says what must
exist and leaves the how to you and the person you are working with. They
may have a repository, a skill, or a procedure that already answers most of
the questions below; ask, and read it before asking anything it already
answers. If they do not, work it out with them one question at a time, with
a recommendation each time. The intended host is an AWS instance, but nothing
here depends on that, and a host on any provider that satisfies the
conditions below is a valid outcome.

## What must exist

**A name in a zone.** The host answers at one fully-qualified name, `<name>`,
which is either the apex of a DNS zone the person controls — `ikigenba.dev`
answering at `ikigenba.dev` — or a name beneath such a zone, such as
`dev1.ikigenba.dev` under `ikigenba.dev`. The platform routes HTTP traffic by
hostname, so every service will get a name under `<name>`, and the certificate
is a wildcard for it. The host need not own the whole zone and need not be the
only thing answering within it; it needs `<name>` and `*.<name>` to be free to
point at it. A project creates and tears down many such hosts over time, each
independently, so a zone holding several hosts' names side by side is the
ordinary case, not a special one.

**A host.** A modern Linux distribution with systemd, on a machine that
keeps the same public IP address across reboots and rebuilds. The host is
the only thing that answers for `<name>` and everything under it, and it
terminates TLS itself, so it needs inbound TCP 80 and 443 open to the world.
Inbound 22 should be open only to wherever ssh comes from. `nginx` and
`certbot` must be installed from the distribution's package manager, with
the systemd units those packages ship, and nothing else is required. Avoid a
distribution that has no `certbot` package; installing certbot by other
means is exactly the kind of improvisation this document exists to prevent.

**DNS pointing at it.** Two records in the zone, both to the host's public
address: `<name>` itself, and the wildcard `*.<name>` under it. Once the
address is fixed these never change, so a rebuild of the host does not touch
DNS as long as the address is kept. `opsctl` will later check exactly this
from the host: that `<name>` and a name under it resolve to the same address.

**Root over ssh.** You must be able to run commands as root on the host
from wherever you are working. Logging in as an unprivileged user and
becoming root with `sudo` is fine; the requirement is that `ssh <host>
sudo true` or `ssh root@<host> true` succeeds non-interactively. `opsctl`
refuses to run as anything but root.

**Credentials on the host.** `opsctl` will do two things against the
outside world, and the host's environment must already let it do them
without `opsctl` being told how. It reads and writes records in the zone
that holds `<name>`, including TXT records for certificate challenges and A
records for services. And it lists, writes, reads, and deletes objects under one prefix
in one object-storage bucket used for backups. On AWS this means an
instance role with Route 53 change and list rights on the zone and S3
object and list rights on the bucket, with the zone's hosted zone possibly
living in another account and reached through an assumable role; other
providers have their own equivalents. `opsctl` never sees an access key
and never reads instance metadata itself. It relies on the standard
credential resolution of the provider's SDK, so whatever mechanism the
provider offers for "this machine is allowed to do these things" is what
must be in place.

**A bucket.** One object-storage bucket for backups, private, encrypted at
rest, denying unencrypted transport. Name it after the zone, with dashes in
place of dots and any uniqueness suffix the provider needs, for example
`ikigenba-dev-<accountid>`. Retention and lifecycle are the bucket's
concern, not `opsctl`'s.

## Working with the person

Before touching anything, establish with them: the name the host will
answer at and the zone it lies in, the provider and account the host will
live in, the distribution, how ssh access is granted, where the DNS zone is
hosted and whether it is in the same account as the host, and the bucket
name. Then establish how each thing will be created.
Ask one question at a time and recommend an answer with each. If they hand
you a procedure that already covers this, follow it and come back here only
to verify.

Expect this to be repeated. A project creates and tears down many hosts over
time, and any one of them will be rebuilt several times while the platform is
being proved out, so prefer arrangements where the address, the DNS records,
the bucket, and the credentials survive a rebuild and only the machine itself
is replaced — and where a second host's name, records, and bucket can sit
beside the first's without disturbing it.

## Verifying you are done

Do not declare bootstrap finished on the strength of having run the steps.
Verify each condition from where you are working, and show the person the
results.

```
dig +short <name>              # the host's public address
dig +short anything.<name>     # the same address
ssh root@<name> true           # or: ssh <user>@<name> sudo true
ssh root@<name> 'command -v nginx certbot && systemctl is-enabled nginx'
```

Confirm the credentials from the host rather than from your workstation,
since the host's environment is what `opsctl` will use. The provider's
own CLI is the easiest probe; on AWS, listing the bucket and listing the
zone's records from the host with no configured keys proves the instance
role works.

When every check passes, bootstrap is complete, and `setup.md` picks up
from here.
