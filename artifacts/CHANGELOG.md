# Changelog

## v0.2.0 — 2026-08-10

- Mount the MCP tool surface in the composition root: the running service now
  answers `POST /mcp` (identity-gated), fixing the v0.1.0 deploy where the
  tool table existed but was never wired to the served router.
- Prove reachability on the assembled binary with a composed smoke
  (R-P52Q-H8MG): initialize + tools/list through the real `serve` process,
  401 without identity headers.
