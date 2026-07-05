# dashboard

The **dashboard** for the ikigenba single-tenant suite. It is the privileged,
per-box "apex" app, deployed at `<account>.ikigenba.com/` (e.g.
`int.ikigenba.com/`). First demo account: **int**.

This is a greenfield repo. **Read the decisions first — do not re-derive them:**

- `../crm.bak/` — the prior fused crm+dashboard codebase. **Reference only**, do
  not depend on it. It is ~80% dashboard already; port from it.

## Build phases

We build in phases — see `project/notes/phases.md`. Each phase is bounded-breadth /
production-depth and is **not done until it works both on localhost and deployed
on its real DNS name (`int.ikigenba.com`) with real TLS.**

- **Phase 0 (current): structural web app, no auth.** A plain Go web app with all
  the bits in the right structure — serves the index page + static assets, does
  structured logging. Chassis (config, SQLite+migrations, logging, server, CLI,
  banner) + the full deploy spine (manifest/deploy env, the appkit one-binary
  contract shipped via the shared `bin/ship` → `opsctl stage`/`deploy`, systemd via the
  platform launcher, the apex nginx `server` block + HTTP-01 TLS). **No
  auth, no identity, no tokens.** Phase 0 is **fully deployed and serving the
  index over real TLS on `int.ikigenba.com` before Phase 1 begins** — the deploy
  architecture is proven before any auth complexity confounds it.
- **Phase 1: login, identity-aware index, logout.** Layers Google Workspace
  federation + web sessions onto the deployed Phase 0 app; the index becomes
  identity-aware (shows the owner, offers sign-out).
- **Phase 2 and later: MCP and the token leg** — opaque tokens, OAuth AS,
  `/internal/authn`, then plugin/inventory/push.

Full per-phase scope, definition of done, and open decisions are in
`project/notes/phases.md`.

## What this app is

The apex/`DEFAULT=true` app and the suite's **OAuth authorization server**. An
external IdP (Google) authenticates the human; this app mints its **own opaque
tokens** for use against the services. Services carry no token logic — they trust
identity headers nginx injects after calling this app. Small business, ≤100 users
per box: SQLite, one box, in-process everything is correct and deliberate.

## What it owns

Port from `../crm.bak` (these are already dashboard-shaped there):
`googleidp`, `oauth` (token chains/PKCE/refresh-reuse), `oauthstate`, `session`,
`agentsevents`, `ratelimit`, the `ui/` (login + grants/revocation), the OAuth AS
endpoints. Audit is **per-service** here = auth/token/grant events only.

Build new:
- **`POST /internal/authn`** — loopback-only introspection endpoint nginx calls
  via `auth_request` on every service request. It is the `requireBearer` logic
  from `../crm.bak/internal/server/contacts.go` lifted out of the request path:
  validate the opaque token, check resource binding + workspace + per-token rate
  limit, return `200` + identity headers (`X-Owner-Email`, `X-Client-Id`) or
  `401` (with the MCP `WWW-Authenticate` challenge) / `429`.
- **Push** — VAPID keypair, subscription store, internal send API. Every push
  carries `source` (service name) + `category` for per-source mute. Services are
  publishers; they never own VAPID or subscriptions.
- **Public landing page** — self-templating from the request host; serves the
  one-paste install snippet (no secrets in it).
- **Service inventory** — an endpoint listing the box's services (name, mount,
  MCP resource URL) so the suite plugin's connect skill can wire up each MCP.
- **`plugin/`** — the **one suite plugin per box** lives here (skills for every
  service on the box + the `connect`/doctor skill). This repo is its
  marketplace (`.claude-plugin/marketplace.json`, `source: "./plugin"`). Internal
  only — git-repo source during dev, dashboard-served in prod. NOT the public
  Claude catalog. The skill set — including the CRM skills — lives here, not in
  the crm repo.

## What it owns on the box (nginx + TLS)

The apex/box-global substrate the dashboard depends on is provisioned by `opsctl
init-box`: the **single apex `server` block**, the **one** apex TLS cert (HTTP-01
`--webroot`) + renewal, the ACME-challenge location, the `/_authn` internal
location, and `include /etc/nginx/conf.d/locations/*.conf;`. Services only drop
`location` fragments into that dir (their own `opsctl setup <svc>`).

## Manifest / deploy

The dashboard is one static appkit binary, the apex/`DEFAULT=true` case of the
contract: `appkit.Main(appkit.Spec{… Default:true …})`, the fixed verbs
(`serve`/`version`/`manifest`/`migrate`/`schema`). Backup and restore are no
longer binary verbs — they are box-level `opsctl` operations (S3 snapshot of
`state/`); for the apex, `opsctl backup` additionally captures the TLS cert tree
as a separate stream. `etc/manifest.env`
(`APP=dashboard`, `MOUNT=/`, `DEFAULT=true`, `PORT=3000`, no `MCP`) is emitted by
`dashboard manifest`. The dashboard **derives** its OAuth-AS resource list at
startup from the on-box service manifests (`/opt/*/etc/manifest.env`, `MCP=true`,
via `DASHBOARD_MANIFEST_ROOT`) — there is **no** hardcoded env resource list.
Shipping is the shared repo-root `bin/ship dashboard` (no version arg; version is
the committed `dashboard/VERSION`, advanced by `bin/bump dashboard <field>`) →
`opsctl stage` + `opsctl deploy`; provisioning is `opsctl init-box` (box-global) +
`opsctl setup dashboard` (per-app). The only `bin/*` scripts it still carries are `start`/`stop`
(systemd control), `secrets` (SSM seeding), and `teardown` (box removal — no
opsctl verb yet). Drop everything `contacts`/`mcp-crm` from the port — that is the
crm service's.

> **Cutover = reset + deploy (no DB preservation).** The dashboard's migrations
> were renumbered name/timestamp-keyed → integer-keyed for the appkit runner. A
> fresh DB migrates correctly, but the **live `ai` box**
> `/opt/dashboard/state/dashboard.db` applied the OLD name-keyed ledger, which the
> integer runner will not recognize — so a plain `opsctl deploy` against the
> existing DB would fail to boot. Per the 2026-06-05 directive **no databases need
> to be preserved**, the cutover therefore just resets the DB: **stop → (optional
> backup) → drop/reset the DB → `bin/ship dashboard` → `opsctl stage` + `opsctl
> deploy` (fresh DB migrates clean to v5) → restart → verify**. Off-box code needs
> no change.
> See `docs/runbook-dashboard-box-cutover.md` (and the cutover note in the root
> `AGENTS.md` Deployments section).
