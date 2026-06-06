# Path-routed suite architecture (single box per customer)

Status: adopted 2026-05-29; amended 2026-05-31 to reserve the `/srv/` prefix for
services (see "Service mount prefix" below). First implementation:
`ai.metaspot.org` (dashboard + crm). This **amends** the Service-layer section of
the top-level `CLAUDE.md`: services no longer ship standalone nginx vhosts or own
subdomains — they mount under paths on the apex host. Where this doc and
`CLAUDE.md` differ, this doc wins for path-routed accounts.

## Model

One box per customer answers on the apex `<account>.metaspot.org`. The suite is
one **dashboard** plus N **services**, all on that box, all bound to loopback,
routed by **path** (not subdomain):

```
<account>.metaspot.org/                  -> dashboard   (UI, auth, push)
<account>.metaspot.org/srv/<svc>/...     -> service <svc>  (REST + MCP, no UI)
<account>.metaspot.org/srv/<svc>/mcp     -> service <svc> MCP endpoint
```

The customer sees one host, one login, one consent, one push authorization. The
operator still runs N systemd units; what collapses is the *user-facing* surface.

### Service mount prefix (`/srv/`)

All services mount under a single reserved namespace: `/srv/<svc>/`. The dashboard
owns every other top-level path (`/`, `/login`, `/oauth/*`, `/internal/*`,
`/agents/*`, `/.well-known/*`, `/static/*`) and is `DEFAULT=true`, so a flat
`/<svc>/` mount would put each service name one collision away from a current or
future dashboard route. Reserving `/srv/` makes the boundary unambiguous:
**anything under `/srv/` is a proxied service; everything else is the dashboard.**
Service names must still be unique among themselves, but never against the
dashboard's surface.

- The **dashboard** is the apex/`DEFAULT` app and the platform's privileged
  service. It is the OAuth **authorization server** for the suite, owns the
  management UI, owns push, and owns the apex nginx server block + the single
  apex TLS cert.
- **Services** are pure REST + MCP APIs with **no UI** and **no token logic**.
  This "no UI" rule is load-bearing: it is what makes path-prefix stripping
  safe. A service that wants a browser UI breaks the model — push that UI into
  the dashboard.
- Services bind **127.0.0.1 only**. nginx is the sole trust boundary. A service
  listening on a public interface would let anyone spoof identity headers and is
  a security defect.

## Identity: external IdP -> dashboard -> opaque tokens

Two distinct token layers, never conflated:

1. **Upstream (login):** the human authenticates to the dashboard via an
   external IdP (Google). This is a rare login event between browser and
   dashboard only. Services never see it.
2. **Downstream (hot path):** the dashboard mints its **own opaque tokens** for
   use against services. These are validated on every request by introspection
   against the dashboard. Opaque (not JWT) is deliberate: on one box the
   introspection call is a cached loopback hop costing ~nothing, and it buys
   **instant revocation** from the dashboard UI — the whole point of centralized
   management. Revisit only if a customer ever outgrows one box.

## The auth contract (unified — one pattern everywhere)

Every `/srv/<svc>/` location, **including `/srv/<svc>/mcp`**, goes through nginx
`auth_request`. Services have zero token logic.

### Dashboard endpoint (introspection)

```
POST /internal/authn          bound 127.0.0.1, never routed publicly
  in : Authorization: Bearer <opaque>      (forwarded by nginx subrequest)
       X-Original-URI: /srv/<svc>/...       (resource hint for binding check)
  out: 200  + X-Owner-Email, X-Client-Id, X-Chain-Id, X-Token-Id, X-Scopes
       401  + WWW-Authenticate: Bearer resource_metadata="https://<account>.metaspot.org/srv/<svc>/.well-known/oauth-protected-resource"
       429  (per-token rate limit exceeded)
```

This is the existing `requireBearer` logic (token hash + lookup, resource
binding, workspace check, per-token rate limit) lifted out of the request path
and exposed as an endpoint. On 401 the dashboard emits the MCP OAuth **discovery
challenge on behalf of the service** — nginx propagates the subrequest's
`WWW-Authenticate` header to the client, so MCP clients get a correct flow with
no token logic in the service.

### What the service must do

- Trust `X-Owner-Email` / `X-Client-Id` injected by nginx. Build the audit
  identity from them. Nothing else.
- Serve one unauthenticated static doc: `/srv/<svc>/.well-known/oauth-protected-resource`
  (RFC 9728), composed from its resource id + the dashboard AS URL (both from
  env). This is the only unauthenticated route a service exposes.

### nginx (apex server block, owned by dashboard's bin/setup)

```nginx
location = /_authn {
    internal;
    proxy_pass http://127.0.0.1:<dashboard_port>/internal/authn;
    proxy_set_header X-Original-URI $request_uri;
    proxy_pass_request_body off;
    proxy_set_header Content-Length "";
}

# one fragment per service, dropped by the service's bin/setup into
# /etc/nginx/conf.d/locations/<svc>.conf and included into this server block
location /srv/<svc>/ {
    auth_request /_authn;
    auth_request_set $owner $upstream_http_x_owner_email;
    auth_request_set $cid   $upstream_http_x_client_id;
    proxy_set_header X-Owner-Email $owner;       # nginx sets identity authoritatively
    proxy_set_header X-Client-Id  $cid;
    proxy_set_header X-Owner-Email "";           # (clear any inbound spoof first; see note)
    proxy_pass http://127.0.0.1:<svc_port>/;     # trailing slash strips /srv/<svc>/
}
location = /srv/<svc>/.well-known/oauth-protected-resource {
    proxy_pass http://127.0.0.1:<svc_port>;      # static PRM doc, no auth_request
}
```

Header hygiene: nginx must overwrite identity headers so a client cannot inject
its own `X-Owner-Email`. Set them only from the `auth_request` result.

## nginx ownership (the change from CLAUDE.md)

- **dashboard `bin/setup`** owns: the apex `server` block (one per box), the
  single `<account>.metaspot.org` cert (HTTP-01), the ACME challenge location,
  the `/_authn` internal location, and an `include /etc/nginx/conf.d/locations/*.conf;`.
- **service `bin/setup`** owns: nothing global. It writes
  `/etc/nginx/conf.d/locations/<svc>.conf` (its two `location` blocks) and
  reloads nginx. It does **not** install a vhost and does **not** issue a cert.

DNS is unchanged — the apex A record already points at the box. The
`*.<account>.metaspot.org` wildcard becomes vestigial under path routing.

## Per-service conventions (unchanged from CLAUDE.md, with path additions)

- Six-script interface, `/opt/<svc>/bin/run` entrypoint, systemd unit via the
  platform launcher, secrets via `/metaspot/<env>/app-config`. All still apply.
- `etc/manifest.env` adds the service's mount path. Dashboard is `DEFAULT=true`.
- Audit is **per-service**: each repo keeps its own audit store. The dashboard
  audits auth/token/grant events; a service audits its own domain mutations
  using the header identity. No cross-service audit API.
- Every service MCP exposes a **no-side-effect identity tool** (`<svc>_whoami`)
  returning the authenticated owner email. It exists so the client-side connect
  skill can prove the full chain (plugin -> connector -> dashboard OAuth ->
  service) end to end. See `connector-and-install.md`.

## Client install layer

The dashboard, the service MCP, and the customer's Claude Code are tied together
by a client-side connector + plugin. That layer — the four-artifact split
(plugin/connector/service/dashboard), the minimal install flow, the
self-templating landing page, and the connector plugin's skills — is specified
in `connector-and-install.md`. The only server-side requirements it imposes are
recorded above: the `<svc>_whoami` identity tool and the dashboard's public
landing page that serves the install snippet.

## Push (dashboard-owned)

One VAPID keypair, one subscription store, one consent — all in the dashboard.
Services are publishers: they call the dashboard's internal send API. Every push
carries a `source` (the service name) and `category` from day one, so the
dashboard can apply per-source mute preferences. Do not let services manage their
own subscriptions or VAPID keys.
