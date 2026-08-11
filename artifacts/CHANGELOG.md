# Changelog

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
