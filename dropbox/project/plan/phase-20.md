# Phase 20 — The `dropbox/docs/` filesystem-API reference + coverage guard

*Realizes design Decision 20. Depends on phase 18 (the loopback routes the
reference documents) and phase 19 (the MCP write tools, noted alongside their
loopback equivalents).*

Observable end state:

- A shipped `dropbox/docs/` tree carries the **filesystem-interaction API**
  reference: for each in-scope route — `GET /content`, `PUT /content`,
  `DELETE /content`, `POST /mkdir`, `POST /move`, `GET /list`, `GET /stat` — its
  method + path, query/body parameters, success-response shape, error taxonomy
  (`not_found`/`conflict`/`validation`/`too_large`), the streaming +
  local-commit-then-async-push contract, path confinement, and the `X-Client-Id`
  origin convention. It **excludes** `/feed`, `/health`, `/mcp`, the PRM
  well-known, and the landing page.
- A route-coverage test (in `cmd/dropbox`, over the shipped tree) enumerates the
  filesystem-API routes the composition root registers and fails if any is absent
  from the docs; the excluded plumbing routes are explicitly not required.

**Done when:** the suite is green (design Conventions commands, from `dropbox/`)
and:

- R-KVL9-O1M5 is covered by a test asserting `dropbox/docs/` documents each
  in-scope filesystem route with its method, path, parameters, and behavior.
- R-KWT6-1TCU is covered by a test asserting the coverage guard fails when a
  registered filesystem-API route is undocumented, and that the excluded plumbing
  routes are not required.
