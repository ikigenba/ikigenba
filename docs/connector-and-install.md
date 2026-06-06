# Connector + install layer (client side)

Status: adopted 2026-05-29. Companion to `path-routing-architecture.md` (the
server-side topology + auth contract). This doc covers everything between the
customer's Claude Code and the box: how a service's MCP connector is installed,
authenticated, and made useful via a Claude Code plugin.

The experiment this layer answers: **how simple can install become?** Answer:
one paste + one `/mcp` login, where the login is the deliberate consent moment.

## Four artifacts, cleanly separated

| Artifact | Is | Lives in | Cardinality |
|---|---|---|---|
| **Suite plugin** | *judgment* — skills for every service on the box + one connect/doctor skill | a `plugin/` subfolder of the **dashboard** repo (its natural home), served/distributed by the dashboard | **one per box** — never one per service |
| **Connector** | *capability* — a remote-HTTP MCP server entry + OAuth | the project's `.mcp.json` on the customer's machine | one per service on the box (each `/<svc>/mcp` is its own OAuth resource) |
| **Service** | the REST + MCP API (no UI) | service repo | one per service |
| **Dashboard** | OAuth AS + IAM + push + the public landing/install page + the box's **service inventory** | dashboard repo | one per box |

**One plugin per box, not per service.** The customer installs and thinks about a
single plugin that covers the whole suite — a dozen plugins is exactly the
dozen-services burden this whole architecture collapses. The plugin bundles the
skills for every service on the box plus one connect/doctor skill; that skill is
what loops, wiring up each service's MCP connector individually. Single plugin, N
connectors.

Why the plugin and connector are still split: each MCP endpoint is per-customer
(`<account>.metaspot.org/<svc>/mcp`) and needs OAuth against *that* customer's
dashboard. A published plugin can't carry a per-customer URL cleanly. So
**capability** (the per-service connectors, in `.mcp.json`) and **judgment** (the
skills, in the one plugin) decouple. Adding a service to a box adds a connector +
its skills to the *existing* suite plugin — the customer never installs a second
plugin.

> Open verification: confirm empirically whether a plugin can bundle a *remote*
> HTTP MCP server. Current understanding is plugins bundle only stdio servers,
> which is *why* the connector lives in project `.mcp.json` rather than inside
> the plugin. Even if remote bundling is supported, we keep the connector out of
> the plugin so the published plugin stays host-agnostic.

## The minimal install flow

1. Customer visits their own dashboard landing page at `<account>.metaspot.org`
   (pre-login — see below) and copies the install snippet.
2. They paste it to their agent. The snippet is a natural-language instruction
   ("add the metaspot suite marketplace at `<internal-repo-or-dashboard-url>`,
   enable the suite plugin, set `METASPOT_HOST=<account>.metaspot.org`"); the
   agent translates it into a `.claude/settings.json` edit
   (`extraKnownMarketplaces` + `enabledPlugins`). A literal config block can back
   the prose as a belt-and-suspenders fallback.
3. The now-installed suite plugin's **`connect`** skill takes over and is
   deterministic from here: it asks the dashboard for the box's service inventory,
   then for each service writes an `.mcp.json` connector entry from
   `${METASPOT_HOST}` + the service's mount, and coaches the human through auth.
4. Human runs `/mcp`, authenticates each service's connector in the browser
   (dashboard -> Google federation), approves. This is the consent/grant moment —
   a human belongs here.
5. The connect skill verifies each connector by calling its `<svc>_whoami` and
   confirms: "connected to crm as alice@acme.com."

Only one step (the file edit in step 2) relies on the agent interpreting prose,
and it is small and well-defined; everything after the plugin is installed is
deterministic skill behavior.

## The dashboard landing page

- **Self-templating, zero config.** The page reads its own origin
  (`Host`/`window.location`) and bakes that host into the snippet. Same code on
  every box, automatically correct per customer, nothing to wire up.
- **No secret in the snippet.** It contains no credentials — a marketplace
  reference plus the customer's hostname. It can sit on the landing page
  (login-gated or not, as you choose); you don't need a secret to obtain the
  thing that lets you authenticate. The secret exchange happens later, in the
  OAuth flow.
- **It is the demo.** "Here's your URL, paste this one line, your agent now talks
  to your CRM." As the dashboard grows into the IAM tool, the post-login view
  becomes "manage who can connect what"; the landing page stays the front door
  and install source.

## Plugin layout and distribution

**These are internal business plugins, not public-marketplace listings.** Nothing
here goes to the public Claude plugin catalog. Distribution differs by phase:

- **Development:** the marketplace source is the **git repo** directly (the
  `dashboard` repo, via its `.claude-plugin/marketplace.json`). Fast iteration,
  no serving infrastructure.
- **Production (long-term):** the dashboard **serves** the plugin/marketplace off
  the box, so a non-technical customer needs nothing but their own URL — no git
  auth. The plugin contents are identical; only the marketplace source in the
  install snippet changes (git URL → dashboard URL).

- There is **one suite plugin per box** (skills for all services on the box + the
  `connect`/doctor skill). It is *not* per service and does *not* live inside a
  service repo. Its natural home is a **`plugin/` subfolder of the dashboard
  repo** — the dashboard is the per-box apex that already serves the landing page
  and the service inventory, so it owns and distributes the per-box plugin too.
- The marketplace (`.claude-plugin/marketplace.json`, `source: "./plugin"`) is
  the dashboard repo / dashboard-served URL. The install snippet points the
  customer at it via an internal git URL or a dashboard URL — not a public
  `owner/repo` listing.
- Skills authorship for the demo: author them directly in the suite plugin. At
  product scale, if you want skills versioned next to their service, author them
  in each service repo and **aggregate** into the one suite plugin at build time —
  distribution stays a single plugin either way.

## The connect / doctor skill (`connect`)

One skill, the whole suite. It pulls the box's service inventory from the
dashboard and runs the same connect-and-verify loop for each service on the box —
so the customer connects "to their server," not to a list of services they have
to track. The agent cannot type `/mcp` or click in a browser, so the skill is
**choreography + verification**, not automation. Per service, it diagnoses state
and maps each state to exactly one next action:

| State | Action |
|---|---|
| no `.mcp.json` connector entry | write it from `${METASPOT_HOST}`, reload |
| service MCP tools not visible | reload / run `/mcp` |
| server unreachable | wrong or missing `METASPOT_HOST` |
| 401 / not authenticated | run `/mcp` and authenticate |
| wrong identity (`whoami` mismatch) | log in with the correct Google account |
| `<svc>_whoami` returns the owner email | done — connected |

Every state has one instruction. That is the difference between "magic" and
"stuck."

## CRM domain skills (judgment over the real tool surface)

The CRM MCP surface is **contact identity data only**: `crm_contact_*`
(create/get/list/update/delete) + `crm_email_*` and `crm_phone_*`
(add/update/delete) + `crm_whoami`. (The legacy `lr_crm_*` prefix renames to
`crm_*`.) No notes/deals/activities entity exists — deferred. Skills encode
judgment over identity data; they do not invent unbacked workflows.

- **`crm-capture-contact`** — dedup before create. Search by email (exact) and
  name (fuzzy via list) first; if a strong match exists, augment it (add the new
  email/phone with a `work`/`personal` label) rather than duplicating; normalize
  the display name. The single most valuable behavior — the line between a CRM
  and a junk drawer.
- **`crm-enrich-from-context`** — extract name/emails/phones from unstructured
  text (email signature, meeting note, pasted vCard), find the matching contact
  or recognize it's new, and route to create-or-update with the same dedup
  discipline. This is what "your agent talks to your CRM" looks like in the demo.
- **`crm-reconcile-duplicates`** — scan for probable duplicates (shared email,
  near-identical names), present the cluster, consolidate emails/phones onto one
  canonical record, delete the redundant ones.

All three lean on a shared **find-the-right-contact** primitive (email-exact ->
name-fuzzy -> disambiguate on multiple matches) kept as internal guidance, not a
separately advertised skill.

Dedup is **convention enforced by skill behavior, not by the schema** — the same
ethos as per-app backup prefixes. The tools permit duplicates; the skills choose
not to make them.
