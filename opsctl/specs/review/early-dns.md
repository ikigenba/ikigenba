# DNS author review — pending independent verification

## Current and proposed complete consumer tasks

The command grammar and public Go API remain the same. The revised target makes previously informal output, no-write guarantees, timeout semantics and provider record normalization normative. These are review interactions, not executed live probes.

| Complete task | Current documented interaction | Proposed interaction and completion |
|---|---|---|
| Discover | `opsctl dns --help` or `opsctl dns -h`; exact bytes lived only in prose | Same commands for any uid; exact story help now normative, exit 0, empty stderr, no state/network access |
| Configure and check | `opsctl config set dns.provider=route53`, then `opsctl config set dns.zones=ikigenba.dev:Z09565073GHK8BYWQ1A78`, then `opsctl dns check` | Same root commands; stdout `ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated)`; exit 0 |
| Check both zones | Configure `dns.zones=ikigenba.dev:Z09565073GHK8BYWQ1A78,sbx.ikigenba.dev:SECOND`, then `opsctl dns check` | Same commands; delegated first zone gets success line and undelegated second gets `sbx.ikigenba.dev: failed: nameservers not delegated`; exit 1; empty stderr; both checked |
| Add, inspect, remove | `opsctl dns add _probe.ikigenba.dev TXT hello`; `opsctl dns list ikigenba.dev`; `opsctl dns remove _probe.ikigenba.dev TXT hello` | Same sequence; mutations silent exit 0 after live confirmation; list contains `_probe.ikigenba.dev TXT 300 hello`; final set absent, other records unchanged. Repeat add before remove and repeat remove afterward: silent idempotent success |
| Read all values | `opsctl dns list ikigenba.dev` | Same command; sorted `NAME TYPE TTL VALUE` lines with wildcard literal, NS dot removed, TXT unquoted, SOA remainder unchanged; no writes |
| Reject unowned name | `opsctl dns add _probe.example.org TXT hello` | Same command; exit 2; stderr `opsctl: no configured zone contains '_probe.example.org'`, blank line, `configured zones: ikigenba.dev`; no provider method call |
| Reject unowned zone | `opsctl dns list example.org` | Same command; exit 2; stderr `opsctl: zone not configured: example.org`, blank line, `configured zones: ikigenba.dev`; no provider method call |
| Complete paired ACME challenges | Run `CERTBOT_DOMAIN=ikigenba.dev CERTBOT_VALIDATION=first opsctl dns acme-auth`, then same with `second`; run `acme-cleanup` with `first`, then with `second` | Same root hook sequence; assuming the set is initially absent, first add creates TTL 60; second adds beside it; each returns live, silent, exit 0; first cleanup preserves second; second removes set; repeat cleanup silent exit 0. Existing set with non-60 TTL remains unresolved: [issue](../issues/dns-acme-existing-ttl.md) |
| Explain manual hook | With either hook variable empty, run `opsctl dns acme-auth` then `opsctl dns acme-cleanup` | Same commands; each exits 2 and prints its exact hook-purpose diagnostic on stderr without changing state |
| Bound waiting | `opsctl dns add --timeout 10s _probe.ikigenba.dev TXT hello` with provider remaining pending | Same command; exit 1, stderr `opsctl: change to _probe.ikigenba.dev not confirmed within 10s`; no rollback |
| Explain absent config | On empty store run `opsctl dns check`; set provider then repeat; set malformed zones then repeat | Same commands; exit 1, respectively `opsctl: dns.provider not set`, `opsctl: dns.zones not set`, or `opsctl: dns.zones malformed: "bad-entry"`; no provider/network call |
| Explain invalid verb | `opsctl dns`; `opsctl dns update _probe.ikigenba.dev TXT hello` | Same root commands; exact no/unknown-subcommand diagnostic followed by blank line and `see 'opsctl dns --help' for usage`; exit 2, empty stdout, no state change |

Package consumer task (unchanged names and signatures): obtain a D3 `config.Store`, then call `dns.Open(ctx, store, dns.Env{Open: route53.Open})`; return its error on failure. Call `client.ZoneFor("_probe.ikigenba.dev")`, handle its error, then `client.Check(ctx, zone)` and inspect `ZoneName`, `Nameservers`, and `Delegated`. Call `client.Add(ctx, "_probe.ikigenba.dev", "TXT", 300, "hello")`, handle its error, call `client.Records(ctx, zone)` to inspect `Record.Values`, then call `client.Remove(ctx, "_probe.ikigenba.dev", "TXT", "hello")` and handle its error. Supply a deadline-bearing context to mutations. Proposed difference: `Records` and `Check` reject an arbitrary unconfigured name/ID pair before provider calls. `Env.LookupNS` remains the injected resolver retained by the client returned by `Open`; no exported resolver field is added. Layout remains suitable: DNS vocabulary and ownership in `internal/dns`, AWS adaptation in `internal/dns/route53`, CLI grammar in `internal/cli`.

## Input and criterion ledger

Source: `specs/stories/dns.md`; SHA-256 `4ac4f4d0d4bc3c280f5baa81f656ff88c8326da834e156b2b3f15a175cd2d6c3`. Each paragraph, command/output block, and individual pre/postcondition is labeled separately below. No explicit Acceptance heading exists; narrative outcomes and postconditions are the acceptance criteria. All mappings are author proposals pending a fresh verifier. Source installation and credential preconditions are environmental assumptions, not a claim that opsctl installs itself or grants credentials.

### INTRO: Document introduction

| Criterion | Source text / locator | Proposed requirements |
|---|---|---|
| DNS.INTRO.INTRO.01 | opsctl is the only thing on the host that writes DNS records. It owns the zones named in its configuration and nothing outside them, and it reaches them through one provider at a time — Route 53 is the first... | R-DDWW-CBQX R-DISH-VEPP R-WL0S-P73L R-YVC3-MFBD R-L2ZA-8HJ4 R-L92S-5C8L R-FIQK-FVGO R-LDYD-OF7D R-LF6A-26Y2 |
| DNS.INTRO.INTRO.02 | The top-level usage gains the line ` dns manage DNS records in the zones opsctl owns` under `Commands:`. | D02 top-level help owner (cross-scope handoff) |
| DNS.INTRO.INTRO.03 | Two configuration keys, both set before any `dns` subcommand will run: | R-DDWW-CBQX R-DISH-VEPP R-WL0S-P73L R-YVC3-MFBD R-L2ZA-8HJ4 R-L92S-5C8L R-FIQK-FVGO R-LDYD-OF7D R-LF6A-26Y2 |
| DNS.INTRO.INTRO.04 | \| key \| value \| \|---\|---\| \| `dns.provider` \| the active provider; only `route53` exists \| \| `dns.zones` \| comma-separated `NAME:ID` pairs of the zones opsctl owns, e.g. `ikigenba.dev:Z09565073GHK8... | R-DDWW-CBQX R-DISH-VEPP R-WL0S-P73L R-YVC3-MFBD R-L2ZA-8HJ4 R-L92S-5C8L R-FIQK-FVGO R-LDYD-OF7D R-LF6A-26Y2 |
| DNS.INTRO.INTRO.05 | The zone id is the provider's and a person supplies it; opsctl never reads it from anywhere but the store, and never from a bootstrap's own files such as `/etc/ikigenba/env`. A record name is mapped to a zon... | R-DDWW-CBQX R-DISH-VEPP R-WL0S-P73L R-YVC3-MFBD R-L2ZA-8HJ4 R-L92S-5C8L R-FIQK-FVGO R-LDYD-OF7D R-LF6A-26Y2 |
| DNS.INTRO.INTRO.06 | The verbs are `add` and `remove`, never an overwrite. A wildcard certificate's DNS-01 challenge puts two TXT values at one name at the same time, and an overwrite would destroy one of them. | R-DDWW-CBQX R-DISH-VEPP R-WL0S-P73L R-YVC3-MFBD R-L2ZA-8HJ4 R-L92S-5C8L R-FIQK-FVGO R-LDYD-OF7D R-LF6A-26Y2 |

### HELP: An agent asks what `dns` can do

| Criterion | Source text / locator | Proposed requirements |
|---|---|---|
| DNS.HELP.COMMAND.01 | $ opsctl dns --help | R-F1NZ-332Y R-FTPN-VT4X R-F8ZD-DPJ4 |
| DNS.HELP.COMMAND.02 | $ opsctl dns -h | R-F1NZ-332Y R-FTPN-VT4X R-F8ZD-DPJ4 |
| DNS.HELP.OUTPUT.01 | Usage: opsctl dns <subcommand> [options] [arguments] Manage DNS records in the zones opsctl owns, through the configured provider. Subcommands: list ZONE print every record in ZONE, one line per value add NA... | R-F1NZ-332Y R-FTPN-VT4X R-F8ZD-DPJ4 |
| DNS.HELP.ACCEPTANCE.01 | Exits 0. The text is on stdout; stderr is empty. It prints for any user. | R-F1NZ-332Y R-FTPN-VT4X R-F8ZD-DPJ4 |
| DNS.HELP.PRE.01 | - `opsctl` is installed on the host. | R-F1NZ-332Y R-FTPN-VT4X R-F8ZD-DPJ4 |
| DNS.HELP.POST.01 | - Nothing has changed. | R-F1NZ-332Y R-FTPN-VT4X R-F8ZD-DPJ4 |

### CHECK_OK: An operator checks the host can reach its zones

| Criterion | Source text / locator | Proposed requirements |
|---|---|---|
| DNS.CHECK_OK.INTRO.01 | The first thing to know about a new host is whether the DNS it was given is real: the provider answers for the zone, and public DNS delegates that zone to the nameservers the provider names. One line per con... | R-F6JK-M61Q R-L5F3-010I R-L6MZ-DSR7 R-FIQK-FVGO R-WL0S-P73L |
| DNS.CHECK_OK.COMMAND.01 | $ sudo opsctl dns check | R-F6JK-M61Q R-L5F3-010I R-L6MZ-DSR7 R-FIQK-FVGO R-WL0S-P73L |
| DNS.CHECK_OK.OUTPUT.01 | ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated) | R-F6JK-M61Q R-L5F3-010I R-L6MZ-DSR7 R-FIQK-FVGO R-WL0S-P73L |
| DNS.CHECK_OK.ACCEPTANCE.01 | Exits 0. The line is on stdout; stderr is empty. | R-F6JK-M61Q R-L5F3-010I R-L6MZ-DSR7 R-FIQK-FVGO R-WL0S-P73L |
| DNS.CHECK_OK.PRE.01 | - `dns.provider` is `route53` and `dns.zones` is `ikigenba.dev:Z09565073GHK8BYWQ1A78`. | R-F6JK-M61Q R-L5F3-010I R-L6MZ-DSR7 R-FIQK-FVGO R-WL0S-P73L |
| DNS.CHECK_OK.PRE.02 | - The host's credentials can read that zone. | R-F6JK-M61Q R-L5F3-010I R-L6MZ-DSR7 R-FIQK-FVGO R-WL0S-P73L |
| DNS.CHECK_OK.PRE.03 | - Public DNS delegates `ikigenba.dev` to the zone's four nameservers. | R-F6JK-M61Q R-L5F3-010I R-L6MZ-DSR7 R-FIQK-FVGO R-WL0S-P73L |
| DNS.CHECK_OK.POST.01 | - Nothing has changed. No record was written. | R-F6JK-M61Q R-L5F3-010I R-L6MZ-DSR7 R-FIQK-FVGO R-WL0S-P73L |

### CHECK_FAILED: An operator checks zones and one of them is wrong

| Criterion | Source text / locator | Proposed requirements |
|---|---|---|
| DNS.CHECK_FAILED.INTRO.01 | A failing zone is a fact about the zone, not about the command, so it is part of the report on stdout and every remaining zone is still checked — an operator sees every problem at once rather than one per ru... | R-F6JK-M61Q R-L5F3-010I R-FIQK-FVGO |
| DNS.CHECK_FAILED.COMMAND.01 | $ sudo opsctl dns check; echo "exit $?" | R-F6JK-M61Q R-L5F3-010I R-FIQK-FVGO |
| DNS.CHECK_FAILED.OUTPUT.01 | ikigenba.dev: ok (route53 Z09565073GHK8BYWQ1A78, 4 nameservers delegated) sbx.ikigenba.dev: failed: nameservers not delegated exit 1 | R-F6JK-M61Q R-L5F3-010I R-FIQK-FVGO |
| DNS.CHECK_FAILED.ACCEPTANCE.01 | Exits 1. The lines are on stdout; stderr is empty. | R-F6JK-M61Q R-L5F3-010I R-FIQK-FVGO |
| DNS.CHECK_FAILED.PRE.01 | - Two zones are configured. | R-F6JK-M61Q R-L5F3-010I R-FIQK-FVGO |
| DNS.CHECK_FAILED.PRE.02 | - The registrar has not been pointed at the second zone's nameservers. | R-F6JK-M61Q R-L5F3-010I R-FIQK-FVGO |
| DNS.CHECK_FAILED.POST.01 | - Nothing has changed. | R-F6JK-M61Q R-L5F3-010I R-FIQK-FVGO |

### READ: An operator reads a zone

| Criterion | Source text / locator | Proposed requirements |
|---|---|---|
| DNS.READ.INTRO.01 | One line per value, `NAME TYPE TTL VALUE`, sorted by name, then type, then value, with the value the rest of the line. Names carry no trailing dot and a literal `*`; TXT values are unquoted, so what is print... | R-LL9R-Z1NJ R-EWSD-K046 R-FME9-L6OR R-FIQK-FVGO R-FBF6-590I |
| DNS.READ.COMMAND.01 | $ sudo opsctl dns list ikigenba.dev | R-LL9R-Z1NJ R-EWSD-K046 R-FME9-L6OR R-FIQK-FVGO R-FBF6-590I |
| DNS.READ.OUTPUT.01 | *.ikigenba.dev A 60 77.112.106.79 ikigenba.dev A 60 77.112.106.79 ikigenba.dev NS 172800 ns-132.awsdns-16.com ikigenba.dev NS 172800 ns-1653.awsdns-14.co.uk ikigenba.dev SOA 900 ns-132.awsdns-16.com. awsdns-... | R-LL9R-Z1NJ R-EWSD-K046 R-FME9-L6OR R-FIQK-FVGO R-FBF6-590I |
| DNS.READ.ACCEPTANCE.01 | Exits 0. The lines are on stdout; stderr is empty. | R-LL9R-Z1NJ R-EWSD-K046 R-FME9-L6OR R-FIQK-FVGO R-FBF6-590I |
| DNS.READ.PRE.01 | - `ikigenba.dev` is a configured zone the host's credentials can read. | R-LL9R-Z1NJ R-EWSD-K046 R-FME9-L6OR R-FIQK-FVGO R-FBF6-590I |
| DNS.READ.POST.01 | - Nothing has changed. | R-LL9R-Z1NJ R-EWSD-K046 R-FME9-L6OR R-FIQK-FVGO R-FBF6-590I |

### MUTATE: An operator adds a record and takes it away again

| Criterion | Source text / locator | Proposed requirements |
|---|---|---|
| DNS.MUTATE.INTRO.01 | Proving by hand that the seam reaches the real service: add a value, see it, remove it. Neither verb prints anything — the answer is the exit code — and both return only once the change is live at the provider. | R-LMHO-CTE8 R-LDYD-OF7D R-LF6A-26Y2 R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 |
| DNS.MUTATE.COMMAND.01 | $ sudo opsctl dns add _probe.ikigenba.dev TXT hello $ sudo opsctl dns list ikigenba.dev \| grep _probe _probe.ikigenba.dev TXT 300 hello $ sudo opsctl dns remove _probe.ikigenba.dev TXT hello | R-LMHO-CTE8 R-LDYD-OF7D R-LF6A-26Y2 R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 |
| DNS.MUTATE.OUTPUT.01 |  | R-LMHO-CTE8 R-LDYD-OF7D R-LF6A-26Y2 R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 |
| DNS.MUTATE.ACCEPTANCE.01 | Both verbs exit 0. Nothing is on stdout or stderr. | R-LMHO-CTE8 R-LDYD-OF7D R-LF6A-26Y2 R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 |
| DNS.MUTATE.PRE.01 | - `ikigenba.dev` is a configured zone the host's credentials can write. | R-LMHO-CTE8 R-LDYD-OF7D R-LF6A-26Y2 R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 |
| DNS.MUTATE.POST.01 | - After `add`, `_probe.ikigenba.dev TXT` holds `hello` and a 300 second TTL, and every other value at that name is untouched. `add` of a value already present changes nothing and still exits 0. | R-LMHO-CTE8 R-LDYD-OF7D R-LF6A-26Y2 R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 |
| DNS.MUTATE.POST.02 | - After `remove`, that record set is gone, since `hello` was its last value. `remove` of a value that is not there changes nothing and still exits 0. | R-LMHO-CTE8 R-LDYD-OF7D R-LF6A-26Y2 R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 |
| DNS.MUTATE.POST.03 | - No other record in the zone has changed. | R-LMHO-CTE8 R-LDYD-OF7D R-LF6A-26Y2 R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 |

### OUTSIDE: An operator names a record in a zone the host does not own

| Criterion | Source text / locator | Proposed requirements |
|---|---|---|
| DNS.OUTSIDE.INTRO.01 | A host writes only inside the zones it was configured with. A name outside them is the operator's mistake, so the refusal lists what the host does own. | R-L2ZA-8HJ4 R-LNPK-QL4X R-FL6D-7EY2 R-FOU2-CQ65 |
| DNS.OUTSIDE.COMMAND.01 | $ sudo opsctl dns add _probe.example.org TXT hello; echo "exit $?" | R-L2ZA-8HJ4 R-LNPK-QL4X R-FL6D-7EY2 R-FOU2-CQ65 |
| DNS.OUTSIDE.OUTPUT.01 | opsctl: no configured zone contains '_probe.example.org' configured zones: ikigenba.dev exit 2 | R-L2ZA-8HJ4 R-LNPK-QL4X R-FL6D-7EY2 R-FOU2-CQ65 |
| DNS.OUTSIDE.ACCEPTANCE.01 | Exits 2. The text is on stderr; stdout is empty. | R-L2ZA-8HJ4 R-LNPK-QL4X R-FL6D-7EY2 R-FOU2-CQ65 |
| DNS.OUTSIDE.PRE.01 | - `ikigenba.dev` is the only configured zone. | R-L2ZA-8HJ4 R-LNPK-QL4X R-FL6D-7EY2 R-FOU2-CQ65 |
| DNS.OUTSIDE.POST.01 | - Nothing has changed. The provider was not called. | R-L2ZA-8HJ4 R-LNPK-QL4X R-FL6D-7EY2 R-FOU2-CQ65 |

### LIST_UNKNOWN: An operator lists a zone that is not configured

| Criterion | Source text / locator | Proposed requirements |
|---|---|---|
| DNS.LIST_UNKNOWN.COMMAND.01 | $ sudo opsctl dns list example.org | R-LL9R-Z1NJ R-FL6D-7EY2 R-FOU2-CQ65 |
| DNS.LIST_UNKNOWN.OUTPUT.01 | opsctl: zone not configured: example.org configured zones: ikigenba.dev | R-LL9R-Z1NJ R-FL6D-7EY2 R-FOU2-CQ65 |
| DNS.LIST_UNKNOWN.ACCEPTANCE.01 | Exits 2. The text is on stderr; stdout is empty. | R-LL9R-Z1NJ R-FL6D-7EY2 R-FOU2-CQ65 |
| DNS.LIST_UNKNOWN.PRE.01 | - `ikigenba.dev` is the only configured zone. | R-LL9R-Z1NJ R-FL6D-7EY2 R-FOU2-CQ65 |
| DNS.LIST_UNKNOWN.POST.01 | - Nothing has changed. | R-LL9R-Z1NJ R-FL6D-7EY2 R-FOU2-CQ65 |

### AUTH: certbot asks for a challenge record

| Criterion | Source text / locator | Proposed requirements |
|---|---|---|
| DNS.AUTH.INTRO.01 | certbot runs the auth hook once per challenge with `CERTBOT_DOMAIN` set to the authorization identifier — the bare zone for a wildcard, never `*.` — and `CERTBOT_VALIDATION` set to the token. A wildcard cert... | UNRESOLVED existing-set TTL; partial mapping only: R-LRD9-VWD0 R-WM8P-2YUA R-LDYD-OF7D R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 R-FTPN-VT4X |
| DNS.AUTH.COMMAND.01 | $ CERTBOT_DOMAIN=ikigenba.dev CERTBOT_VALIDATION=6ukiLNXA opsctl dns acme-auth | R-LRD9-VWD0 R-WM8P-2YUA R-LDYD-OF7D R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 R-FTPN-VT4X |
| DNS.AUTH.OUTPUT.01 |  | R-LRD9-VWD0 R-WM8P-2YUA R-LDYD-OF7D R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 R-FTPN-VT4X |
| DNS.AUTH.ACCEPTANCE.01 | Exits 0. Nothing is on stdout or stderr. | R-LRD9-VWD0 R-WM8P-2YUA R-LDYD-OF7D R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 R-FTPN-VT4X |
| DNS.AUTH.PRE.01 | - `ikigenba.dev` is a configured zone the host's credentials can write. | R-LRD9-VWD0 R-WM8P-2YUA R-LDYD-OF7D R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 R-FTPN-VT4X |
| DNS.AUTH.PRE.02 | - certbot is running the hook. | R-LRD9-VWD0 R-WM8P-2YUA R-LDYD-OF7D R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 R-FTPN-VT4X |
| DNS.AUTH.POST.01 | - `_acme-challenge.ikigenba.dev TXT` holds `6ukiLNXA` with a 60 second TTL, beside any value already there, and the change is live at the provider. | UNRESOLVED existing-set TTL; partial mapping only: R-LRD9-VWD0 R-WM8P-2YUA R-LDYD-OF7D R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 R-FTPN-VT4X |
| DNS.AUTH.POST.02 | - Nothing else in the zone has changed. | R-LRD9-VWD0 R-WM8P-2YUA R-LDYD-OF7D R-EY09-XRUV R-FIQK-FVGO R-FOU2-CQ65 R-FTPN-VT4X |

### CLEANUP: certbot cleans a challenge record up

| Criterion | Source text / locator | Proposed requirements |
|---|---|---|
| DNS.CLEANUP.INTRO.01 | The cleanup hook runs later with the same environment and takes away exactly the one value it was given, leaving any other value at that name — the other half of the wildcard's pair, mid-flight — alone. | R-LSL6-9O3P R-WM8P-2YUA R-LF6A-26Y2 R-FIQK-FVGO R-FOU2-CQ65 |
| DNS.CLEANUP.COMMAND.01 | $ CERTBOT_DOMAIN=ikigenba.dev CERTBOT_VALIDATION=6ukiLNXA opsctl dns acme-cleanup | R-LSL6-9O3P R-WM8P-2YUA R-LF6A-26Y2 R-FIQK-FVGO R-FOU2-CQ65 |
| DNS.CLEANUP.OUTPUT.01 |  | R-LSL6-9O3P R-WM8P-2YUA R-LF6A-26Y2 R-FIQK-FVGO R-FOU2-CQ65 |
| DNS.CLEANUP.ACCEPTANCE.01 | Exits 0. Nothing is on stdout or stderr. | R-LSL6-9O3P R-WM8P-2YUA R-LF6A-26Y2 R-FIQK-FVGO R-FOU2-CQ65 |
| DNS.CLEANUP.PRE.01 | - `ikigenba.dev` is a configured zone the host's credentials can write. | R-LSL6-9O3P R-WM8P-2YUA R-LF6A-26Y2 R-FIQK-FVGO R-FOU2-CQ65 |
| DNS.CLEANUP.POST.01 | - `6ukiLNXA` is no longer a value of `_acme-challenge.ikigenba.dev TXT`. If it was the last value, the record set is gone; if not, the rest remain. | R-LSL6-9O3P R-WM8P-2YUA R-LF6A-26Y2 R-FIQK-FVGO R-FOU2-CQ65 |
| DNS.CLEANUP.POST.02 | - Cleanup of a value that is already gone changes nothing and exits 0. | R-LSL6-9O3P R-WM8P-2YUA R-LF6A-26Y2 R-FIQK-FVGO R-FOU2-CQ65 |

### HOOK_MANUAL: An operator runs a certbot hook by hand

| Criterion | Source text / locator | Proposed requirements |
|---|---|---|
| DNS.HOOK_MANUAL.INTRO.01 | The hooks take their arguments from certbot's environment and nowhere else. Run without it there is nothing to do, and the message says what the command is for, because a non-zero hook exit is only written t... | R-LRD9-VWD0 R-LSL6-9O3P R-WM8P-2YUA R-FOU2-CQ65 |
| DNS.HOOK_MANUAL.COMMAND.01 | $ sudo opsctl dns acme-auth | R-LRD9-VWD0 R-LSL6-9O3P R-WM8P-2YUA R-FOU2-CQ65 |
| DNS.HOOK_MANUAL.OUTPUT.01 | opsctl: acme-auth must be run by certbot as --manual-auth-hook | R-LRD9-VWD0 R-LSL6-9O3P R-WM8P-2YUA R-FOU2-CQ65 |
| DNS.HOOK_MANUAL.ACCEPTANCE.01 | Exits 2. The line is on stderr; stdout is empty. `acme-cleanup` says the same of `--manual-cleanup-hook`. | R-LRD9-VWD0 R-LSL6-9O3P R-WM8P-2YUA R-FOU2-CQ65 |
| DNS.HOOK_MANUAL.PRE.01 | - `CERTBOT_DOMAIN` or `CERTBOT_VALIDATION` is unset or empty. | R-LRD9-VWD0 R-LSL6-9O3P R-WM8P-2YUA R-FOU2-CQ65 |
| DNS.HOOK_MANUAL.POST.01 | - Nothing has changed. | R-LRD9-VWD0 R-LSL6-9O3P R-WM8P-2YUA R-FOU2-CQ65 |

### TIMEOUT: A change does not go live in time

| Criterion | Source text / locator | Proposed requirements |
|---|---|---|
| DNS.TIMEOUT.INTRO.01 | `add` and `remove` return once the provider reports the change live, which takes about thirty seconds on Route 53. When it has not by the deadline, the command reports that it could not confirm it — not that... | R-LOXH-4CVM R-EY09-XRUV R-FTPN-VT4X R-FOU2-CQ65 |
| DNS.TIMEOUT.COMMAND.01 | $ sudo opsctl dns add --timeout 10s _probe.ikigenba.dev TXT hello | R-LOXH-4CVM R-EY09-XRUV R-FTPN-VT4X R-FOU2-CQ65 |
| DNS.TIMEOUT.OUTPUT.01 | opsctl: change to _probe.ikigenba.dev not confirmed within 10s | R-LOXH-4CVM R-EY09-XRUV R-FTPN-VT4X R-FOU2-CQ65 |
| DNS.TIMEOUT.ACCEPTANCE.01 | Exits 1. The line is on stderr; stdout is empty. | R-LOXH-4CVM R-EY09-XRUV R-FTPN-VT4X R-FOU2-CQ65 |
| DNS.TIMEOUT.PRE.01 | - `ikigenba.dev` is a configured zone the host's credentials can write. | R-LOXH-4CVM R-EY09-XRUV R-FTPN-VT4X R-FOU2-CQ65 |
| DNS.TIMEOUT.PRE.02 | - The provider has not reported the change live within ten seconds. | R-LOXH-4CVM R-EY09-XRUV R-FTPN-VT4X R-FOU2-CQ65 |
| DNS.TIMEOUT.POST.01 | - The change was submitted and will very likely become live. Nothing was rolled back. | R-LOXH-4CVM R-EY09-XRUV R-FTPN-VT4X R-FOU2-CQ65 |

### UNCONFIGURED: An agent runs a dns subcommand on an unconfigured host

| Criterion | Source text / locator | Proposed requirements |
|---|---|---|
| DNS.UNCONFIGURED.INTRO.01 | Before `init` has been run, or on a host whose store was never written, there is nothing for `dns` to talk to. Every subcommand says which key is missing. | R-YU47-8NKO R-FF2V-AK8L R-WL0S-P73L R-FOU2-CQ65 |
| DNS.UNCONFIGURED.COMMAND.01 | $ sudo opsctl dns check | R-YU47-8NKO R-FF2V-AK8L R-WL0S-P73L R-FOU2-CQ65 |
| DNS.UNCONFIGURED.OUTPUT.01 | opsctl: dns.provider not set | R-YU47-8NKO R-FF2V-AK8L R-WL0S-P73L R-FOU2-CQ65 |
| DNS.UNCONFIGURED.ACCEPTANCE.01 | Exits 1. The line is on stderr; stdout is empty. With the provider set and the zones not, the line is `opsctl: dns.zones not set`; with a `dns.zones` entry that is not a `NAME:ID` pair, it is `opsctl: dns.zo... | R-YU47-8NKO R-FF2V-AK8L R-WL0S-P73L R-FOU2-CQ65 |
| DNS.UNCONFIGURED.PRE.01 | - `dns.provider` is unset or empty. | R-YU47-8NKO R-FF2V-AK8L R-WL0S-P73L R-FOU2-CQ65 |
| DNS.UNCONFIGURED.POST.01 | - Nothing has changed. No provider was opened and no network call was made. | R-YU47-8NKO R-FF2V-AK8L R-WL0S-P73L R-FOU2-CQ65 |

### INVALID: An operator runs `dns` with no subcommand, or one that does not exist

| Criterion | Source text / locator | Proposed requirements |
|---|---|---|
| DNS.INVALID.COMMAND.01 | $ sudo opsctl dns | R-LITZ-7I65 R-F8ZD-DPJ4 R-FOU2-CQ65 |
| DNS.INVALID.OUTPUT.01 | opsctl: no dns subcommand given see 'opsctl dns --help' for usage | R-LITZ-7I65 R-F8ZD-DPJ4 R-FOU2-CQ65 |
| DNS.INVALID.COMMAND.02 | $ sudo opsctl dns update _probe.ikigenba.dev TXT hello | R-LITZ-7I65 R-F8ZD-DPJ4 R-FOU2-CQ65 |
| DNS.INVALID.OUTPUT.02 | opsctl: unknown dns subcommand 'update' see 'opsctl dns --help' for usage | R-LITZ-7I65 R-F8ZD-DPJ4 R-FOU2-CQ65 |
| DNS.INVALID.ACCEPTANCE.01 | Both exit 2. The text is on stderr; stdout is empty. | R-LITZ-7I65 R-F8ZD-DPJ4 R-FOU2-CQ65 |
| DNS.INVALID.PRE.01 | - `opsctl` is running as root. | R-LITZ-7I65 R-F8ZD-DPJ4 R-FOU2-CQ65 |
| DNS.INVALID.POST.01 | - Nothing has changed. | R-LITZ-7I65 R-F8ZD-DPJ4 R-FOU2-CQ65 |

Total extracted criteria: 93.

## New requirement trace

| Requirement | Source / supporting constraint |
|---|---|
| R-DDWW-CBQX | INTRO.configuration; necessary shared declaration |
| R-DISH-VEPP | INTRO.configuration; necessary shared declaration |
| R-DMG7-0PXS | INTRO.configuration and ownership; necessary shared declaration |
| R-DQ3W-615V | INTRO.configuration and ownership; necessary shared declaration |
| R-DTRL-BCDY | INTRO.configuration and ownership; necessary shared declaration |
| R-DW7E-2VVC | INTRO; check/read/mutate stories; necessary shared declaration |
| R-DYN6-UFCQ | INTRO; check/read/mutate stories; necessary shared declaration |
| R-E12Z-LYU4 | INTRO; check/read/mutate stories; necessary shared declaration |
| R-E3IS-DIBI | INTRO; check/read/mutate stories; necessary shared declaration |
| R-WIKZ-XNM7 | INTRO; check/read/mutate stories; necessary shared declaration |
| R-E9MA-AD0Z | INTRO; check/read/mutate stories; necessary shared declaration |
| R-EC23-1WID | INTRO; check/read/mutate stories; necessary shared declaration |
| R-EEHV-TFZR | INTRO; check/read/mutate stories; necessary shared declaration |
| R-EGXO-KZH5 | INTRO; check/read/mutate stories; necessary shared declaration |
| R-EJDH-CIYJ | INTRO; check/read/mutate stories; necessary shared declaration |
| R-ELTA-42FX | INTRO; check/read/mutate stories; necessary shared declaration |
| R-7JWE-YKBM | INTRO.provider; necessary provider declaration |
| R-ERWS-0X5E | INTRO.provider; necessary provider declaration |
| R-EUCK-SGMS | INTRO.provider; necessary provider declaration |
| R-EWSD-K046 | READ.intro/output |
| R-EY09-XRUV | MUTATE.intro/post; TIMEOUT.intro/post |
| R-F1NZ-332Y | HELP.command/output/pre/post |
| R-WL0S-P73L | INTRO.configuration; UNCONFIGURED.command/output/post |
| R-F6JK-M61Q | CHECK_OK.command/output; CHECK_FAILED.intro/command/output |
| R-F8ZD-DPJ4 | HELP.output; INVALID.pre; project root policy |
| R-FBF6-590I | INTRO.ownership; READ.pre; necessary package behavior |
| R-FF2V-AK8L | INTRO.configuration; UNCONFIGURED.output; deterministic configuration failures |
| R-FIQK-FVGO | CHECK_OK.post; CHECK_FAILED.post; READ.post; MUTATE.post; AUTH.post; CLEANUP.post |
| R-FL6D-7EY2 | OUTSIDE.output/post; LIST_UNKNOWN.output/post; INTRO.ownership |
| R-FME9-L6OR | READ.intro/output |
| R-FOU2-CQ65 | MUTATE.output; AUTH.output; CLEANUP.output; HOOK_MANUAL.post; INVALID.output/post; UNCONFIGURED.post |
| R-WM8P-2YUA | AUTH.intro; CLEANUP.intro; HOOK_MANUAL.intro/pre/post |
| R-FTPN-VT4X | HELP.output.options; AUTH.post; TIMEOUT.command/output; necessary command grammar |
| R-FW5G-NCMB | READ.command; CHECK_OK.command; necessary command failure behavior |

## Boundary notes and drafting choices

- D01 retains `Deps.DNS dns.Env`; public DNS names and shapes are unchanged. Combined structural requirements were split into one declaration each.
- D02 owns the top-level help line from DNS.INTRO. Root/early and boundary coordinator were notified.
- Init consumes `dns.Open`, `ZoneFor`, and `Check`; its independent config report may parse stored zone text when provider config is missing. `dns.Open` itself requires provider then valid zones and returns no usable client on failure.
- Help lists `--ttl` for all mutation commands and says it applies when add creates a record, while hooks specify TTL 60. Draft therefore accepts the flag for the listed commands, applies it only to ordinary add, retains auth TTL 60, and ignores it on remove/cleanup. This is a technical grammar interpretation of the supplied help, not a user-approved new option policy.
- Unresolved: DNS.AUTH.INTRO.01 and DNS.AUTH.POST.01 require TTL 60 even beside existing values, while the existing provider contract preserves a preexisting TTL. Neither criterion is fully covered. See [existing-set TTL issue](../issues/dns-acme-existing-ttl.md); no new resolution is authored here.

## External observations and evidence limitations

The prior D04 prose records live-zone observations: the host lacks a configured region and fails with `Missing Region`; the instance role denies `GetHostedZone`; apex SOA and NS were read via `ListResourceRecordSets`; unquoted TXT was rejected with `InvalidCharacterString`; two TXT values occupy one set; changes moved from `PENDING` to `INSYNC` after roughly thirty seconds; names returned with trailing dots and wildcard `\052`. This existing observation record is retained here and need not be repeated solely because prose moved. Original provenance: pre-revision D04, section “Facts about Route 53, observed against the live zone”; no raw transcript or timestamp was present.

The previous document says certbot 2.6 hook environment was observed in its source on the host. That is weaker than observing a real hook invocation. No actual certbot hook transcript, live pagination response, signed endpoint/credential-chain observation, or TXT quote/escape edge-case response was found in this bounded inspection. Those external claims need adequate observations before `check-spec`; no probe was run, no deployment changed, and no live DNS writes were authorized by this author task. Missing evidence is an observation blocker, not an unresolved product decision.

## Mechanical handoff

Current cumulative delta from HEAD: 35 added, 11 removed, 19 retained byte-identical requirement lines. Added IDs already tagged in tests: 0. Removed IDs still tagged in tests: 11. Mechanical accounting only; no tests or gates run. Root owns canonical project gap.

Added: R-7JWE-YKBM, R-DDWW-CBQX, R-DISH-VEPP, R-DMG7-0PXS, R-DQ3W-615V, R-DTRL-BCDY, R-DW7E-2VVC, R-DYN6-UFCQ, R-E12Z-LYU4, R-E3IS-DIBI, R-E9MA-AD0Z, R-EC23-1WID, R-EEHV-TFZR, R-EGXO-KZH5, R-EJDH-CIYJ, R-ELTA-42FX, R-ERWS-0X5E, R-EUCK-SGMS, R-EWSD-K046, R-EY09-XRUV, R-F1NZ-332Y, R-F6JK-M61Q, R-F8ZD-DPJ4, R-FBF6-590I, R-FF2V-AK8L, R-FIQK-FVGO, R-FL6D-7EY2, R-FME9-L6OR, R-FOU2-CQ65, R-FTPN-VT4X, R-FW5G-NCMB, R-WIKZ-XNM7, R-WJSW-BFCW, R-WL0S-P73L, R-WM8P-2YUA.

Removed: R-KUFZ-K3C9, R-KWVS-BMTN, R-KY3O-PEKC, R-L7UV-RKHW, R-LBIK-WVPZ, R-LGE6-FYOR, R-LQ5D-I4MB, R-LTT2-NFUE, R-YSWA-UVTZ, R-YWK0-0722, R-YXRW-DYSR.

## Round 1 corrections awaiting verification

R-WIKZ-XNM7 scopes the Client shape to exported fields. R-WJSW-BFCW makes per-client resolver retention normative for CHECK_OK and CHECK_FAILED. Consumer isolation task: open two clients with different supplied LookupNS functions, call Check on each after both opens, and observe each uses only its own resolver with its Check context and configured zone name. The nil-resolver client uses the default resolver.

R-WL0S-P73L and R-WM8P-2YUA resolve HOOK_MANUAL and UNCONFIGURED precedence. With absent, corrupt, or inaccessible config and either hook variable empty, root invocation of either hook exits 2 with its exact hook-purpose diagnostic, no stdout, no configuration read, and no provider or network call. With both variables nonempty and valid grammar, configuration failures retain exit 1.

Existing-set TTL scope: the root leaves the non-60 case unresolved. New sets at TTL 60 and existing sets already at TTL 60 satisfy the ordinary paired-hook task; this does not close the unrestricted source TTL postcondition. Generic Add preservation remains unchanged.
