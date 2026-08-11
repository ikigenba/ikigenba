# Research — external ground truth

Non-contractual evidence design references instead of re-deriving. Facts here
were verified live against the `int` AWS account on 2026-07-18 unless noted.

## The box's Host-matching and ACME behavior (probed live 2026-07-24)

Read-only probes against the live `int` box (`16.59.0.148`), plus a read of its
`/etc/nginx`, establish the substrate the parked-domain decision (D13) sits on:

- **No `default_server` exists anywhere.** `/etc/nginx/conf.d/` holds exactly
  one server file, `dashboard.conf` (the rendered apex block), and it declares
  no `default_server` on either port. Unmatched `Host` therefore falls through
  to it as nginx's implicit "first block wins" default. A parked block may
  claim `default_server` on `:80` and `:443` with no conflict to resolve.
- **Only one certificate lineage is on the box:** `/etc/letsencrypt/live/`
  contains `int.ikigenba.com` and nothing else. A second, independently-renewed
  lineage can be added beside it without touching the first.
- **The ACME challenge location already answers for *any* `Host`.** A request
  for `/.well-known/acme-challenge/<probe>` returns **404 from the webroot**,
  not a 301, whether sent with `Host: metaspot.org`, `Host: int.ikigenba.com`,
  or no `Host` at all. The apex block's `location ^~ /.well-known/acme-challenge/`
  outranks its `location /` redirect for every `Host`, so HTTP-01 webroot
  issuance for a parked name **succeeds before any parked server block exists**.
  This removes the chicken-and-egg that forced init-box's `certbot --standalone`
  bootstrap: cert first, then write the block, then reload.
- **Parked domains already fail TLS today**, so a `default_server` catch-all
  cannot regress any working client. Per-Host probes:

  | probe | observed |
  |---|---|
  | `http://<ip>/` with a parked `Host` | `301 → https://<parked>/` |
  | `https://<parked>/` (cert validating) | connection fails — cert mismatch |
  | `https://<parked>/` (validation skipped) | `200`, dashboard responds |

  The only client that reaches the dashboard through an unknown `Host` today is
  one that skips certificate validation; anything aimed at the raw IP over
  HTTPS is already broken. After D13, unknown-`Host` HTTP behavior is unchanged
  (still a 301 to HTTPS) and unknown-`Host` HTTPS is unchanged for names absent
  from the parked certificate (still a cert mismatch).
- **Access logs cannot attribute traffic to a `Host`.** `log_format main` in
  `/etc/nginx/nginx.conf` carries `$remote_addr`, `$request`, `$status`,
  `$http_referer`, `$http_user_agent`, and `$http_x_forwarded_for` — but **not
  `$host`**. Retroactively proving that nothing addresses the box by IP is
  therefore impossible from logs, which is why the question was settled by
  probe (above) rather than by log analysis.

## The parked domain portfolio (registrar state, re-verified 2026-08-11)

Eleven domains are registered and pointed at the int box's IP. Auto-renew and
expiry drive which of them may sit inside a shared certificate:

| domain | expires | auto-renew | note |
|---|---|---|---|
| `ikigai-group.io` | 2026-09-02 | **no** | adopted via import block |
| `ikigenba.com` | 2027-06-06 | yes | `int.ikigenba.com` delegation is live |
| `ikigenba.dev` | 2027-06-06 | yes | |
| `logic-refinery.com` | 2027-01-31 | yes | Google Workspace MX |
| `logic-refinery.io` | 2026-11-19 | yes | |
| `logic-refinery.net` | 2026-11-19 | yes | |
| `logic-refinery.tv` | 2026-12-17 | yes | |
| `metaspot.net` | 2027-03-16 | yes | |
| `metaspot.org` | 2026-09-22 | yes | Workspace MX + site-verification TXT; kept indefinitely |
| `michaelgreenly.com` | 2027-08-05 | yes | previously the live static site; already dark |
| `michaelgreenly.dev` | 2027-08-10 | yes | registered 2026-08-10, parked from day one; `.dev` is HSTS-preloaded, so it is unusable without a valid certificate |

- **`ikigai-group.io` is the one domain not on auto-renew**, and it expires
  within weeks. Inside a shared SAN certificate a lapsed domain stops resolving
  to the box, HTTP-01 fails for that name, and certbot fails the **whole
  lineage** — so one unrenewed domain would strip TLS from all the others. Its
  `no` is deliberate: the domain is being let go. That makes auto-renew state a
  thing to check at the moment a domain joins the lineage, not a stable
  property. `michaelgreenly.dev` also read `no` on registration, which is the
  Route 53 default rather than a decision, and was turned on before it joined.
- **Certificate limits are not a constraint here.** Let's Encrypt's
  certificates-per-registered-domain limit is 50 per week, and a multi-name
  certificate counts once against each registered domain it names. One combined
  certificate over ten registrations spends one of fifty for each.
- **MX and TXT records are unaffected** by A-record targeting or by what nginx
  serves, so the Workspace mail on `logic-refinery.com` / `metaspot.org` and
  `metaspot.org`'s site-verification TXT are out of this decision's path.
- **`certbot renew` walks lineages independently.** The existing
  `ikigenba-certbot-renew.timer` therefore renews a parked lineage with no new
  timer, and a parked-lineage renewal failure cannot affect the
  `int.ikigenba.com` lineage.

## AWS SSM Parameter Store (us-east-2)

- **Standard-tier parameters cap at 4,096 bytes** of value. The suite's former
  shared secrets blob (`/ikigenba/int/app-config`, one JSON object keyed by
  app) measured 3,995 bytes on 2026-07-18 — one more key of any size would
  have failed the next write. Advanced tier raises the cap to 8 KB but costs
  $0.05/parameter/month and only defers the wall.
- **Standard-tier parameters are free** and the per-account quota (10,000
  parameters per region) is far above the suite's needs. Fourteen per-app
  parameters replace the one blob at zero cost; the largest single app's
  secret set (github, whose RSA App private key PEM dominates) is ~1.8 KB —
  comfortable Standard-tier headroom per app.
- **`put-parameter --overwrite` creates the parameter when it does not
  exist.** This is what lets per-app parameter *existence* be script-managed:
  the first push creates, later pushes overwrite, and no Terraform resource
  per parameter is needed.
- **Parameter names are hierarchical by `/`, and a leaf may coexist with
  children.** `/ikigenba/int/app-config` (the old blob) and
  `/ikigenba/int/app-config/<app>` (the per-app parameters) are simultaneously
  valid, which made the migration's coexistence window possible.
  `get-parameters-by-path --path /ikigenba/int/app-config` enumerates the
  children without the leaf.
- **IAM can grant by path pattern**: a `Resource` of the parameter ARN plus
  `<arn>/*` covers the blob and all per-app children in one statement.
  Applied to the int instance role's `app-config` policy on 2026-07-18
  (metaspot commit `e771ff3`).

## The live migration (completed 2026-07-18, recorded as context)

The per-app split is **already live** on `int.ikigenba.com`: the launcher
(`metaspot/templates/ikigenba-launch`, installed out-of-band on the box)
fetches `/ikigenba/${IKIGENBA_ENV}/app-config/<app>` directly and hard-fails
on any fetch error including `ParameterNotFound`; all fourteen per-app
parameters are seeded (secret-less apps hold an explicit `{}`); all fourteen
services restarted healthy, with `github` proving end-to-end secret flow
(`github_auth: ok`). The old blob parameter remains in place, unread, as a
rollback path until the repo-side cleanup (the pending plan phases) lands.
The blob-era launcher is preserved on-box as
`/usr/local/bin/ikigenba-launch.blob-era`.
