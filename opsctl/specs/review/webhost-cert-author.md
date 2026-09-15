# Certificate author review

## Canonical consumer tasks

These are proposed consumer interactions. Implementation is absent; historical
D00 is superseded by the stories, so there is no existing certificate API to
preserve.

An operator reads `opsctl cert --help` as any user, then as root configures
`opsctl config set host.name=ikigenba.dev` and `opsctl config set acme.email=ops@ikigenba.dev`. With DNS configured, certbot installed, and CA access,
`opsctl cert obtain` returns 0 silently. Running it again while current returns
0 silently and preserves certificate/DNS state. `opsctl cert show` returns
`names: ikigenba.dev, *.ikigenba.dev`, `issuer: R11`, and
`expires: 2026-12-11T14:02:55Z` on three lines for the story's certificate.
The values describe the certificate read; they are not hard-coded issuer or
expiry constants. Issuance/renewal behavior still needs live observation before
check-spec; see evidence below.

An operator on a fresh unconfigured host sees `opsctl: host.name not set` from
`opsctl cert obtain`, then after setting that key sees `opsctl: acme.email not
set`. Both fail with exit 1 before certbot runs. With only host.name configured,
`opsctl cert show` fails with `opsctl: no certificate for ikigenba.dev` if the
lineage is absent. A CA failure reports `opsctl: certbot certonly: exit status
1`, a blank line, then certbot's arbitrary captured output lines prefixed `> `;
stdout stays empty and the old certificate remains available.

An operator asking `opsctl cert` gets exit 2 and `opsctl: no cert subcommand
given`, a blank line, and `see 'opsctl cert --help' for usage`. Asking
`opsctl cert renew` similarly gets `opsctl: unknown cert subcommand 'renew'`.
Scheduled renewal belongs to the init-installed timer; `systemctl start
ikigenba-renew-certificate.service` requests it immediately. D12 owns this
unit's implementation and observable timer behavior.

A package consumer obtains the certificate as a complete task: construct a
`host.Env` from its supplied root, environment accessor, executor and clock;
read `config.Store{Root: env.Root}.Get("host.name")` and `.Get("acme.email")`,
map unset keys to empty strings, then call `cert.Obtain(ctx, env, hostName, email)`. `Obtain` itself rejects an empty host name first, then an empty email, under R-FRFQ-VNTM before execution or changes; the cert CLI also retains its own D07 key checks.
A nil result completes the task. On error return the error to the CLI, which
uses `errors.As` to access `*host.CommandError` and render its captured result
under D02. Init uses that same obtain call in its certificate step before
calling `nginx.Apply(ctx, env, hostName)`.

An inspection consumer calls `cert.Inspect(env, hostName)`; on a nil error it
joins `Info.Names` using comma-space, uses `Info.Issuer`, and formats
`Info.Expires.UTC()` with `time.RFC3339`. On an error matching
`cert.ErrNotFound` through `errors.Is`, it reports the error's text; other
errors are operational failures. This does not require acme.email, an
executor, DNS configuration, or network access. Expired data still prints.

## Stable source coverage

Source: `specs/stories/certificates.md`, as read for this draft. Labels below
are review locators rather than minted requirements. Requirements are in
D07 unless a different owner is named. Author coverage awaits a fresh verifier.

| Criterion | Source locator and outcome | Requirement mapping |
|---|---|---|
| CERT-BASE | opening: apex/wildcard, DNS hooks, certbot lineage | R-GFIN-FMDJ, R-YMBY-ZHP5 |
| CERT-BACKUP | opening: lineage directory backed up | R-YYIY-T743; D13 R-YCP3-2YZF owns inclusion of the entire `/etc/letsencrypt` tree |
| CERT-ENTRY | top usage and init certificate before nginx.conf | D02 top help; R-YYIY-T743; D05 init order |
| CERT-TIMER | renewal owner paragraph: root renew, twice daily, random, persistent, enabled, no key, package timer masked | R-YYIY-T743; D12 timer owner |
| CERT-CONFIG | configuration table: acme.email | R-YJW6-7Y7R; D03 arbitrary store keys |
| CERT-HELP | “An operator asks what cert can do”: both flags, exact stdout, any user, no changes | R-YHGD-GEQD |
| CERT-OBTAIN | “An operator obtains the host's certificate”: silent success, pair issued, saved hooks, cleanup | R-GFIN-FMDJ, R-YMBY-ZHP5, R-YORR-R16J, R-YXB2-FFDE |
| CERT-CURRENT | “An operator obtains a certificate that is already current”: silent no-op, no CA or DNS writes | R-YNJV-D9FU, R-YXB2-FFDE |
| CERT-SHOW | “An operator reads the certificate the host is serving”: names/issuer/expiry, read-only | R-YR7K-IKNX, R-YTND-A45B |
| CERT-MISSING | “An operator reads the certificate on a host that has none”: exact missing diagnostic, no changes | R-YSFG-WCEM, R-YUV9-NVW0 |
| CERT-REFUSED | “An operator obtains a certificate and the CA refuses”: quoted failure, old cert and nginx preserved, reached cleanup | R-YORR-R16J, R-YPZO-4SX8, R-YXB2-FFDE; D02 quoted capture |
| CERT-UNCONFIGURED | “An agent obtains a certificate on an unconfigured host”: absent/empty email or host, no certbot | R-YJW6-7Y7R, R-FRFQ-VNTM |
| CERT-GRAMMAR | “An operator runs cert with no subcommand, or one that does not exist”: exact usage failures, no renewal command | R-YIO9-U6H2, R-YYIY-T743 |

Supporting public declarations: R-YBCV-JK0W (ownership), R-YCKR-XBRL
(Obtain), R-YDSO-B3IA (Info), R-YF0K-OV8Z (Inspect), R-YG8H-2MZO
(ErrNotFound). Each supports obtaining or inspection consumer tasks.

## Interfaces and integration dependencies

- D01 supplies `host.Env`, `Command`, `Result`, and `CommandError` exactly as
  agreed; cert imports host only. D01 currently permits cert to import config
  too, which is broader than necessary; request its owner narrow that edge.
- CLI alone reads config. `Obtain(ctx, env, hostName, email)` agrees with D05.
- D04 hooks add/remove only their individual TXT values, so apex and wildcard
  challenge values coexist at one name until cleanup.
- D06 references the same fullchain and private-key paths. The deploy hook
  reloads active nginx after success and does not start inactive nginx. This
  supports D05 initial issuance before nginx.conf exists without prescribing
  private hook construction. Later nginx Apply requires a reloadable service;
  live host nginx is inactive, so end-to-end init needs prerequisite evidence
  from the init/nginx owners, not an invented certificate startup behavior.
- D12 owns `backup.SetupTimers(ctx, env, store) error`, renewal unit bytes,
  schedule, enablement and package timer masking. The certificate API does
  not write timers or add a configuration key for their period.
- Host-backup owner includes all `/etc/letsencrypt`, retaining certbot's
  lineage symlinks and their referenced archive state, not just a dangling
  live directory.
- The story's `devctl space start` behavior describes another actor and is
  outside opsctl ownership. No sibling source or design was read.

## Evidence and remaining observations

Read-only live observations and their commands/provenance are recorded in
`webhost-observations.md` and raw `webhost-live-help.txt`. Installed certbot
help confirms certonly, renew, two domain arguments, manual DNS preference,
hook flags, certificate naming, due-only behavior, and directory overrides.
Installed systemctl help confirms is-active, quiet and reload grammar.
These observations do not establish issuance success, saved hook behavior,
challenge cleanup, no-op renewal, or deploy-hook behavior. Those outcomes,
plus D12's renewal service/timer behavior, remain observation gates before
check-spec. No live probes were performed by this author.

No unresolved product choice was needed for this scope. The inactive live
nginx prerequisite concern was sent to the coordinator for cross-scope
handling. No source, tests, build gates, check-spec, or commits were changed
or run. The local draft adds 19 requirements; R-YL42-LPYG was minted then
replaced by R-GFIN-FMDJ when the deploy-hook contract was refined, and is not
part of the current design. All 19 current D07 IDs are absent from the current
project test files and therefore appear as additions in the canonical gap.

## Correction pass

New R-FRFQ-VNTM closes the domain/init empty-input seam without changing any API or existing D07 requirement text. D07 now has 20 requirements. CERT-ENTRY and CERT-UNCONFIGURED additionally map to this requirement; complete failure consumers appear in [webhost-corrections.md](webhost-corrections.md). Fresh verification is pending and the initial verifier record remains unchanged. Host-backup integration belongs to D13-host-backup.md, while D14-service-restore.md owns service restore.
