# D4-dns

`opsctl` is the only thing on the host that writes DNS records. Package
`internal/dns` owns the vocabulary and the verbs; a provider package behind
one interface talks to the real service. Route 53 is the first and only
provider, in `internal/dns/route53`, the one package in this module that
imports the AWS SDK. Everything above the seam is standard library and is
tested against a fake provider; the Route 53 package is tested against a fake
HTTP endpoint; nothing in the gates touches AWS.

**Why DNS at all.** Bootstrap created the apex and wildcard records, and
nginx routes by hostname, so a new service needs no DNS change. The one write
`opsctl` must make is the DNS-01 challenge for certbot: a TXT record at
`_acme-challenge.<zone>`. A wildcard certificate covers both `<zone>` and
`*.<zone>`, and the CA issues one token for each, both at the same name, so
the provider must add a value to a record set and remove one value from it —
never overwrite the set. The verbs are named `add` and `remove` for exactly
that reason: the safe behaviour is the only behaviour.

**Configuration.** Read from the config store (D3), never from any other file
on the host — `/etc/ikigenba/env` and its like are a bootstrap's private
business:

| key | value |
|---|---|
| `dns.provider` | the active provider; only `route53` exists |
| `dns.zones` | comma-separated zones `opsctl` owns, e.g. `ikigenba.dev` |
| `dns.<provider>.zone.<zone>` | the provider's id for `<zone>`, e.g. `dns.route53.zone.ikigenba.dev=Z09565073GHK8BYWQ1A78` |

The person supplies the zone id and the agent enters it with `config set`.
A record name is mapped to a zone by longest-suffix match against
`dns.zones`, on label boundaries, so the certbot hooks — which only receive a
domain — need no zone argument.

```go
package dns

const (
    KeyProvider = "dns.provider"
    KeyZones    = "dns.zones"
)

// ZoneKey returns the config key holding provider's id for zone.
func ZoneKey(provider, zone string) string   // "dns.<provider>.zone.<zone>"

var (
    ErrNotConfigured   = errors.New("dns is not configured")
    ErrNoZone          = errors.New("no configured zone contains the name")
    ErrUnknownProvider = errors.New("unknown dns provider")
)

type Zone struct {
    Name string // "ikigenba.dev"
    ID   string // the provider's identifier
}

// Record is one record set: every value at one name and type. Names carry no
// trailing dot and a literal "*"; TXT values are unquoted.
type Record struct {
    Name   string
    Type   string
    TTL    int
    Values []string
}

// Provider is the seam. Add and Remove return once the change is live, or
// with an error wrapping ctx.Err() when ctx ends first.
type Provider interface {
    Records(ctx context.Context, zoneID string) ([]Record, error)
    Add(ctx context.Context, zoneID, name, typ string, ttl int, value string) error
    Remove(ctx context.Context, zoneID, name, typ, value string) error
}

// Env is what the process supplies; both fields are optional.
type Env struct {
    Open     func(ctx context.Context, provider string) (Provider, error) // nil: every provider is unknown
    LookupNS func(ctx context.Context, zone string) ([]string, error)     // nil: net.DefaultResolver
}

type Client struct {
    Provider Provider
    Zones    []Zone
}

func Open(ctx context.Context, store config.Store, env Env) (*Client, error)
func (c *Client) ZoneFor(name string) (Zone, error)
func (c *Client) Records(ctx context.Context, zone Zone) ([]Record, error)
func (c *Client) Add(ctx context.Context, name, typ string, ttl int, value string) error
func (c *Client) Remove(ctx context.Context, name, typ, value string) error
func (c *Client) Check(ctx context.Context, zone Zone) (CheckResult, error)

type CheckResult struct {
    ZoneName    string   // as the provider reports it (from the SOA)
    Nameservers []string // the provider's NS set for the zone
    Delegated   bool     // public DNS delegates the zone to Nameservers
}
```

```go
package route53

const Region = "us-east-1" // Route 53 is global; the SDK still needs a region to build the endpoint

// New returns the Route 53 provider on the SDK's default credential chain.
// endpoint overrides the service URL; "" means the real service.
func New(ctx context.Context, endpoint string) (dns.Provider, error)

// Open is the provider registry cmd/opsctl wires into dns.Env.
func Open(ctx context.Context, provider string) (dns.Provider, error)
```

**Facts about Route 53, observed against the live zone.** The default config
carries no region on the host and the client fails with `Missing Region`
unless one is set. The instance role denies `GetHostedZone`, so the zone name
and nameservers come from the apex SOA and NS records that
`ListResourceRecordSets` returns. TXT values must be quoted or the change is
rejected with `InvalidCharacterString`. Two values at one TXT name are one
record set. A change reports `PENDING` then `INSYNC`, about 30 seconds later.
Names come back with a trailing dot and `*` escaped as `\052`.

**The `dns` command.** Usage, byte for byte, printed by `opsctl dns --help`:

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
  dns.provider                the active provider; only 'route53' is supported
  dns.zones                   comma-separated zones opsctl owns
  dns.<provider>.zone.<zone>  the provider's id for <zone>

NAME is mapped to a zone by longest suffix match against dns.zones.
```

Options come before the positional arguments. `list` prints
`NAME TYPE TTL VALUE`, VALUE being the rest of the line. `check` prints one
line per configured zone and keeps going after a failure so an agent sees
every problem at once:

```
$ opsctl dns check
ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
$ opsctl dns check; echo "exit $?"
ikigenba.dev: failed: dns.route53.zone.ikigenba.dev not set
exit 1
```

**The certbot hooks.** certbot 2.6 (observed in its source on the host) runs
the auth hook once per challenge with `CERTBOT_DOMAIN` set to the
authorization identifier — the bare zone for a wildcard, never `*.` — and
`CERTBOT_VALIDATION` to the token; the cleanup hook runs later with the same
environment. A non-zero hook exit is only logged by certbot, so the
diagnostic must explain itself in certbot's log. `acme-auth` adds the
validation as a TXT value at `_acme-challenge.<domain>` with a 60 second TTL
and waits for it to be live; `acme-cleanup` removes that one value and leaves
any others at the name alone. Which certbot invocation wires the hooks is the
certificate design's business.

Canonical usage, as an agent would drive it over ssh:

```
$ opsctl config set dns.provider=route53
$ opsctl config set dns.zones=ikigenba.dev
$ opsctl config set dns.route53.zone.ikigenba.dev=Z09565073GHK8BYWQ1A78
$ opsctl dns check
ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)
$ opsctl dns add _probe.ikigenba.dev TXT hello
$ opsctl dns list ikigenba.dev
*.ikigenba.dev A 300 77.112.106.79
_probe.ikigenba.dev TXT 300 hello
ikigenba.dev A 300 77.112.106.79
ikigenba.dev NS 172800 ns-132.awsdns-16.com
...
$ opsctl dns remove _probe.ikigenba.dev TXT hello
$ opsctl dns add _probe.example.org TXT hello; echo "exit $?"
opsctl: no configured zone contains '_probe.example.org'

configured zones: ikigenba.dev
exit 2
$ CERTBOT_DOMAIN=ikigenba.dev CERTBOT_VALIDATION=tok opsctl dns acme-auth
```

The proof that the seam reaches the real service is manual: build, install
on the host, run `dns check`, an `add`, and a `remove`, exactly as above.
It is not a gate.

## REQUIREMENTS

- R-KT83-6BLK: Package `internal/dns` MUST export the constants `KeyProvider = "dns.provider"` and `KeyZones = "dns.zones"`, the function `ZoneKey(provider, zone string) string` returning `"dns." + provider + ".zone." + zone`, and the errors `ErrNotConfigured`, `ErrNoZone`, and `ErrUnknownProvider`.
- R-KUFZ-K3C9: Package `internal/dns` MUST export the struct `Zone` with fields exactly `Name string` and `ID string`, and the struct `Record` with fields exactly `Name string`, `Type string`, `TTL int`, and `Values []string`.
- R-KVNV-XV2Y: Package `internal/dns` MUST export the interface `Provider` with methods exactly `Records(ctx context.Context, zoneID string) ([]Record, error)`, `Add(ctx context.Context, zoneID, name, typ string, ttl int, value string) error`, and `Remove(ctx context.Context, zoneID, name, typ, value string) error`.
- R-KWVS-BMTN: Package `internal/dns` MUST export the struct `Env` with fields exactly `Open func(ctx context.Context, provider string) (Provider, error)` and `LookupNS func(ctx context.Context, zone string) ([]string, error)`, and the struct `CheckResult` with fields exactly `ZoneName string`, `Nameservers []string`, and `Delegated bool`.
- R-KY3O-PEKC: Package `internal/dns` MUST export the struct `Client` with fields exactly `Provider Provider` and `Zones []Zone`, the function `Open(ctx context.Context, store config.Store, env Env) (*Client, error)`, and the methods `(*Client) ZoneFor(name string) (Zone, error)`, `(*Client) Records(ctx context.Context, zone Zone) ([]Record, error)`, `(*Client) Add(ctx context.Context, name, typ string, ttl int, value string) error`, `(*Client) Remove(ctx context.Context, name, typ, value string) error`, and `(*Client) Check(ctx context.Context, zone Zone) (CheckResult, error)`.
- R-KZBL-36B1: `Open` MUST return an error wrapping `ErrNotConfigured` whose message contains the offending key when `dns.provider` is unset or empty, when `dns.zones` is unset or empty, or when any zone listed in `dns.zones` has no `ZoneKey(provider, zone)` entry, and MUST not call `Env.Open` in those cases.
- R-L0JH-GY1Q: `Open` MUST split `dns.zones` on commas, trim whitespace, lowercase, and strip one trailing dot from each entry, preserving order, and populate `Client.Zones` with each name and the value of its `ZoneKey` entry.
- R-L1RD-UPSF: `Open` MUST call `Env.Open` with the value of `dns.provider` and return its error unchanged, and when `Env.Open` is nil MUST return an error wrapping `ErrUnknownProvider` that names the provider.
- R-L2ZA-8HJ4: `ZoneFor` MUST lowercase the name and strip one trailing dot, return the configured zone with the longest name that equals the name or is a suffix of it preceded by a dot, and return an error wrapping `ErrNoZone` when no zone matches.
- R-L476-M99T: `Client.Add` and `Client.Remove` MUST resolve the zone with `ZoneFor` and call the provider's `Add` or `Remove` with that zone's `ID`, the normalised name, the type uppercased, and the value unchanged, and MUST return the provider's error unchanged.
- R-L5F3-010I: `Check` MUST call `Provider.Records` for the zone, set `ZoneName` to the name of the `SOA` record and `Nameservers` to the values of the `NS` record at that name, return an error when no `SOA` record is present, and set `Delegated` to true exactly when `Env.LookupNS(zone.Name)` returns the same set of names as `Nameservers` ignoring order, case, and trailing dots.
- R-L6MZ-DSR7: `Check` with a nil `Env.LookupNS` MUST resolve NS records through `net.DefaultResolver`.
- R-L7UV-RKHW: Package `internal/dns/route53` MUST export the constant `Region = "us-east-1"`, the function `New(ctx context.Context, endpoint string) (dns.Provider, error)`, and the function `Open(ctx context.Context, provider string) (dns.Provider, error)`.
- R-L92S-5C8L: `route53.Open` MUST return the provider from `New(ctx, "")` for `"route53"` and an error wrapping `dns.ErrUnknownProvider` naming the provider for any other value.
- R-LAAO-J3ZA: The provider returned by `New` MUST send every Route 53 request signed for region `Region` to `endpoint` when it is non-empty, verified against a fake HTTP endpoint, and MUST read credentials only through the SDK's default credential chain.
- R-LBIK-WVPZ: `Records` of the Route 53 provider MUST return one `Record` per record set with the trailing dot removed from the name, `\052` decoded to `*`, TXT values with their enclosing double quotes removed, and MUST follow pagination until the listing is complete.
- R-LDYD-OF7D: `Add` of the Route 53 provider MUST create the record set with `ttl` when it is absent, MUST otherwise submit the set with the existing values, the existing TTL, and the new value appended, MUST send TXT values enclosed in double quotes, and MUST return nil without submitting a change when the value is already present.
- R-LF6A-26Y2: `Remove` of the Route 53 provider MUST submit the set without the value when other values remain, MUST delete the set when the value is its last, and MUST return nil without submitting a change when the set or the value is absent.
- R-LGE6-FYOR: `Add` and `Remove` of the Route 53 provider MUST return nil only after `GetChange` reports the change `INSYNC`, and MUST return an error wrapping `ctx.Err()` when the context ends first.
- R-LHM2-TQFG: `opsctl dns --help` and `opsctl dns -h` MUST print the `dns` usage text quoted above, byte for byte, to stdout and exit 0.
- R-LITZ-7I65: `opsctl dns` with no subcommand MUST write exactly the three lines `opsctl: no dns subcommand given`, an empty line, and `see 'opsctl dns --help' for usage` to stderr and exit 2, and with an unknown subcommand MUST write exactly the three lines `opsctl: unknown dns subcommand '<name>'`, an empty line, and `see 'opsctl dns --help' for usage` to stderr and exit 2.
- R-LK1V-L9WU: Every `dns` subcommand MUST open the store through `dns.Open` with `Deps.DNS` as the environment, and when `Open` returns an error wrapping `dns.ErrNotConfigured` MUST write the single line `opsctl: <key> not set` naming the offending key to stderr and exit 1.
- R-LL9R-Z1NJ: `opsctl dns list ZONE` MUST print one line `NAME TYPE TTL VALUE` per value of every record in ZONE, sorted by name, then type, then value, to stdout and exit 0, and MUST write `opsctl: zone not configured: ZONE` followed by an empty line and `configured zones: <comma-separated Client.Zones names>` to stderr and exit 2 when ZONE is not a configured zone.
- R-LMHO-CTE8: `opsctl dns add [--ttl SECONDS] [--timeout DURATION] NAME TYPE VALUE` MUST call `Client.Add` with TTL defaulting to 300 and a context whose deadline is DURATION from now defaulting to 2 minutes, print nothing to stdout, and exit 0 on success; `opsctl dns remove [--timeout DURATION] NAME TYPE VALUE` MUST likewise call `Client.Remove`.
- R-LNPK-QL4X: `add` and `remove` with a wrong number of positional arguments, a `--ttl` that is not a positive integer, or a `--timeout` that is not a positive duration MUST exit 2 with a diagnostic on stderr, and when `ZoneFor` fails MUST write `opsctl: no configured zone contains '<NAME>'` followed by an empty line and `configured zones: <comma-separated Client.Zones names>` to stderr and exit 2.
- R-LOXH-4CVM: When the provider returns an error wrapping `context.DeadlineExceeded`, `add`, `remove`, `acme-auth`, and `acme-cleanup` MUST write the single line `opsctl: change to <NAME> not confirmed within <DURATION>` to stderr and exit 1, and for any other provider error MUST write `opsctl: <error>` and exit 1.
- R-LQ5D-I4MB: `opsctl dns check` MUST call `Client.Check` for every configured zone in order, print for each one line `<zone>: ok (<provider> <id>, <n> nameservers delegated)` when `ZoneName` equals the zone name and `Delegated` is true, or `<zone>: failed: <reason>` otherwise or on error, to stdout, and exit 0 when every zone is ok and 1 otherwise.
- R-LRD9-VWD0: `opsctl dns acme-auth [--timeout DURATION]` MUST read `CERTBOT_DOMAIN` and `CERTBOT_VALIDATION` through `Deps.Getenv`, exit 2 with the single line `opsctl: acme-auth must be run by certbot as --manual-auth-hook` on stderr when either is empty, and otherwise call `Client.Add` with name `_acme-challenge.<CERTBOT_DOMAIN>`, type `TXT`, TTL 60, and the validation as the value, printing nothing to stdout and exiting 0 on success.
- R-LSL6-9O3P: `opsctl dns acme-cleanup [--timeout DURATION]` MUST read the same variables through `Deps.Getenv`, exit 2 with the single line `opsctl: acme-cleanup must be run by certbot as --manual-cleanup-hook` on stderr when either is empty, and otherwise call `Client.Remove` with name `_acme-challenge.<CERTBOT_DOMAIN>`, type `TXT`, and the validation as the value, printing nothing to stdout and exiting 0 on success.
- R-LTT2-NFUE: Every `dns` subcommand other than `--help` MUST be subject to the root check of D2.
