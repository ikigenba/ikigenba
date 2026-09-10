# D5-init

`init` is the one command an agent runs after `setup.md`'s install and
`config set` steps: it evaluates every prerequisite the platform needs, reports
all of them at once, and only then runs the setup sequence. D0 fixed the shape —
"the documented sequence of the setup sub-commands behind one fail-fast
preflight that lists every missing prerequisite at once" — and D2 reserved exit
2 for a failed preflight check. This design makes both concrete. It lives in
`internal/cli`; the checks are thin compositions of `internal/config` and
`internal/dns`, and nothing here yet warrants a package of its own.

**The host's name.** A host answers at one fully-qualified name — `ikigenba.dev`
on the live box — and that name is the value of a new config key, `host.name`.
It is either the apex of a configured zone or a name beneath one, so
`ZoneFor(host.name)` (D4) resolves it; the records bootstrap created are
`<host.name>` and `*.<host.name>`, and the wildcard certificate, the nginx
catch-all, and every service name later designs add all hang off it. This
design declares the key and reads it; later designs that need the host's name
read the same key.

| key | value |
|---|---|
| `host.name` | the fully-qualified name this host answers at, at or under a configured zone, e.g. `ikigenba.dev` |

`init` lowercases the value and strips one trailing dot before using it, as
`ZoneFor` does for record names.

**Preflight.** Every check runs; none short-circuits another, so one run shows
the agent everything that is missing. Each check prints exactly one line to
stdout in the `dns check` shape: `<check>: ok (<detail>)` or
`<check>: failed: <reason>`. A check whose input is another check's output —
the zone checks need an opened provider, the host check needs a parsed zone
list and a host name — is simply absent when what it depends on failed, because
its prerequisite is already on the list. The lines, in order:

| line | ok when |
|---|---|
| `nginx`, `certbot`, `systemctl` | the binary is found on PATH; detail is its path |
| `dns.provider` | set, and the provider opens; detail is the value |
| `dns.zones` | set and well-formed; detail is the comma-separated zone names |
| `host.name` | set; detail is the normalised name |
| `zone <name>`, one per configured zone | `Client.Check` reports the zone as `dns check` does |
| `host <host.name>` | `ZoneFor(host.name)` resolves; detail is the zone name |
| `wildcard <host.name>` | `<host.name>` and `_opsctl-preflight.<host.name>` resolve to the same non-empty address set; detail is the addresses |

The wildcard line is `bootstrap.md`'s own done-condition — apex and wildcard
both point at this host — checked from the host. It resolves a fixed probe
label rather than the literal `*`, because the live box's resolver refuses a
literal `*.ikigenba.dev` while `_opsctl-preflight.ikigenba.dev` resolves to
the apex address (observed 2026-09-10). Whether the address is *this* host's
cannot be known without instance metadata, which D0 forbids; equality of the
two lookups is the check.

Prerequisite facts observed on the live box on 2026-09-10: `command -v nginx
certbot systemctl` prints `/usr/sbin/nginx`, `/usr/bin/certbot`, and
`/usr/bin/systemctl`; `systemctl is-enabled nginx` prints `enabled`; `certbot
--version` prints `certbot 2.6.0`. Whether nginx is *enabled* is not checked
yet: that needs a command runner in `Deps`, which arrives with the first design
that runs a command, and the PATH check is the honest approximation until then.

**Sequence.** The sequence is the setup sub-commands that exist. Today there are
none beyond what the preflight itself verifies, so the sequence is empty and
`init` is its preflight: when every check is ok it exits 0 having written
nothing under `Deps.Root`. Each later design that adds a setup step re-mints
the sequence requirement below to name it. `init` is idempotent, as D0 requires
of every setup command.

**Seams.** Two functions join `Deps` (D1): `LookPath`, nil meaning
`exec.LookPath`, and `LookupHost`, nil meaning `net.DefaultResolver.LookupHost`.
The zone checks use the provider and resolver already in `Deps.DNS`. The gates
run as an ordinary user against fakes; nothing in them touches PATH or the
network.

**Usage.** Byte for byte, printed by `opsctl init --help`:

```
Usage: opsctl init

Run the setup sequence behind one preflight. Every check is evaluated and
reported, one line per check, before anything runs; when any check fails,
nothing runs and init exits 2. Safe to re-run.

Checks, in order:
  nginx, certbot, systemctl  each found on PATH
  dns.provider, dns.zones    set, and the provider opens (see 'opsctl dns --help')
  host.name                  set
  zone NAME                  every configured zone is reachable and delegated
  host NAME                  host.name lies at or under a configured zone
  wildcard NAME              host.name and _opsctl-preflight.host.name resolve alike

Sequence:
  none yet; each setup command adds itself here when it is designed

Configuration keys:
  host.name  the fully-qualified name this host answers at, at or under a configured zone
```

Canonical usage, as an agent would drive it over ssh, first on a host that is
ready and then on one that is not:

```
$ opsctl config set host.name=ikigenba.dev
$ opsctl init; echo "exit $?"
nginx: ok (/usr/sbin/nginx)
certbot: ok (/usr/bin/certbot)
systemctl: ok (/usr/bin/systemctl)
dns.provider: ok (route53)
dns.zones: ok (ikigenba.dev)
host.name: ok (ikigenba.dev)
zone ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
host ikigenba.dev: ok (zone ikigenba.dev)
wildcard ikigenba.dev: ok (77.112.106.79)
exit 0
$ opsctl config del host.name
$ opsctl init; echo "exit $?"
nginx: ok (/usr/sbin/nginx)
certbot: failed: not found on PATH
systemctl: ok (/usr/bin/systemctl)
dns.provider: ok (route53)
dns.zones: ok (ikigenba.dev)
host.name: failed: not set
zone ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
exit 2
$ opsctl config set host.name=ikigenba.dev
```

The second run shows the two rules together: every independent check still
reports (certbot and host.name both fail, and the zone line still runs), and
the two lines that depend on `host.name` are absent because their prerequisite
is already listed.

## REQUIREMENTS

- R-ED1L-QHES: `opsctl init --help` and `opsctl init -h` MUST print the `init` usage text quoted above, byte for byte, to stdout, write nothing to stderr, and exit 0.
- R-EE9I-495H: `opsctl init` with any argument other than `--help` or `-h` MUST write exactly the three lines `opsctl: init takes no arguments`, an empty line, and `see 'opsctl init --help' for usage` to stderr, nothing to stdout, and exit 2.
- R-EFHE-I0W6: `opsctl init` other than `--help` and `-h` MUST be subject to the root check of D2.
- R-EHX7-9KDK: `init` MUST look up `nginx`, `certbot`, and `systemctl`, in that order, through `Deps.LookPath`, and print for each the line `<name>: ok (<path>)` with the path `LookPath` returned, or `<name>: failed: not found on PATH` when `LookPath` returned an error.
- R-EJ53-NC49: `init` MUST print `dns.provider: failed: not set` when `dns.provider` is unset or empty; otherwise, when `dns.provider` and `dns.zones` are both set and `dns.Open` with `Deps.DNS` returns an error that does not wrap `dns.ErrNotConfigured`, `dns.provider: failed: ` followed by that error's `Error()` text; and otherwise `dns.provider: ok (<value>)`.
- R-EKD0-13UY: `init` MUST print `dns.zones: failed: not set` when `dns.zones` is unset or empty; otherwise, when `dns.provider` is also set and `dns.Open` with `Deps.DNS` returns an error wrapping `dns.ErrNotConfigured`, `dns.zones: failed: ` followed by that error's `Error()` text; and otherwise `dns.zones: ok (<names>)` where `<names>` is the comma-separated `Client.Zones` names in configured order.
- R-ELKW-EVLN: `init` MUST read the config key `host.name` and print `host.name: failed: not set` when it is unset or empty, and otherwise `host.name: ok (<name>)` where `<name>` is the value lowercased with one trailing dot stripped.
- R-EMSS-SNCC: When and only when the `dns.provider` and `dns.zones` lines are both ok, `init` MUST call `Client.Check` for every configured zone in order and print for each one line `zone <zone>: ok (<provider> <id>, <n> nameservers delegated)` when `ZoneName` equals the zone name and `Delegated` is true, or `zone <zone>: failed: <reason>` otherwise or on error.
- R-EO0P-6F31: When and only when the `dns.provider`, `dns.zones`, and `host.name` lines are all ok, `init` MUST print `host <name>: ok (zone <zone>)` with the name of the zone `ZoneFor(<name>)` returns, or `host <name>: failed: no configured zone contains it` when `ZoneFor` returns an error wrapping `dns.ErrNoZone`, where `<name>` is the normalised `host.name`.
- R-EP8L-K6TQ: When and only when the `host.name` line is ok, `init` MUST resolve `<name>` and `_opsctl-preflight.<name>` through `Deps.LookupHost` and print `wildcard <name>: ok (<addresses>)`, `<addresses>` being the sorted, de-duplicated, comma-separated address set, when both lookups succeed and return the same set ignoring order and duplicates; `wildcard <name>: failed: <name> resolves to <a> but _opsctl-preflight.<name> resolves to <b>` with each side rendered like `<addresses>` when they differ; and `wildcard <name>: failed: ` followed by the lookup error's `Error()` text when either lookup fails.
- R-EQGH-XYKF: The stdout of `opsctl init` MUST consist of exactly the `nginx`, `certbot`, `systemctl`, `dns.provider`, `dns.zones`, and `host.name` lines, then the `zone` lines, the `host` line, and the `wildcard` line, in that order, each present exactly under the conditions its requirement states, and nothing else, and every line whose conditions hold MUST be printed even when an earlier line failed.
- R-EROE-BQB4: `opsctl init` MUST exit 0 when every printed line is ok and 2 when any printed line is failed, and in both cases MUST write nothing to stderr.
- R-ESWA-PI1T: When `config.json` is corrupt, `opsctl init` MUST write nothing to stdout, write a single line beginning `opsctl: ` and naming `config.json` to stderr, and exit 1.
- R-EU47-39SI: `opsctl init` MUST create, modify, and remove no file under `Deps.Root`, and two consecutive runs with identical `Deps` MUST produce identical stdout and identical exit codes.
