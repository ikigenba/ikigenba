# Changelog

## v0.5.0 — 2026-08-13

- The landing page and its assets (styles, fonts, icons, and the inventory
  controller script) are now served from files on disk through the shared web
  mount, instead of being compiled into the service binary. The page looks and
  behaves exactly as before; this is an internal consistency change that brings
  artifacts in line with how the other services serve their web assets.

## v0.4.1 — 2026-08-12

- A browser's unprompted request for the site icon at the service root now
  returns the ikigenba mark instead of "not found". This is the request a client
  makes without reading any page markup, and it had never been answered.
- Nothing else changed. Tabs and bookmarks already showed the mark from the page
  markup and are unaffected; the icon is served without requiring a login.

## v0.4.0 — 2026-08-11

- The service's web pages now carry the suite's brand icon: browsers show the ikigenba mark on the tab, in bookmarks and history, and as the icon if the page is saved to an iOS home screen. Nothing else about the pages changed.

## v0.3.0 — 2026-08-11

- Compose every client-facing URL from the configured front-door base instead
  of the request or a hardcoded default, fixing the v0.2.0 deploy where
  `upload` returned `https://localhost/srv/artifacts/u/<token>` (unreachable
  from the caller's machine) and stored records returned
  `https://127.0.0.1:3009/srv/artifacts/...`.
- Take the base from the chassis, `strings.TrimSuffix(rt.ResourceID(), "mcp")`,
  as sites and webhooks already did; no new environment variable. `NewService`
  now requires it and panics on empty, and one shared `DownloadURL` renders the
  tier-correct URL for both the upload ingress and the MCP records so the two
  cannot drift. The loopback `content_url` is unchanged by design.
- Pin the failure mode with tagged tests (R-78BZ-IXSS, R-79JV-WPJH,
  R-7BZO-O90V, R-7D7L-20RK), including an ingress request arriving as
  `Host: 127.0.0.1:3009` whose returned `url` must ignore it.

## v0.2.0 — 2026-08-10

- Mount the MCP tool surface in the composition root: the running service now
  answers `POST /mcp` (identity-gated), fixing the v0.1.0 deploy where the
  tool table existed but was never wired to the served router.
- Prove reachability on the assembled binary with a composed smoke
  (R-P52Q-H8MG): initialize + tools/list through the real `serve` process,
  401 without identity headers.
