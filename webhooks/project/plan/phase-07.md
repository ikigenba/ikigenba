# Phase 7 — nginx fragment, dev-harness wiring & e2e/onboarding

*Realizes design Decision 7 (nginx location fragment) and Decision 8 (test strategy, harness & dev-onboarding). Depends on Phase 6.*

Wire the finished binary into the running suite and prove it end to end through
real nginx. This phase is config + end-to-end tests rather than a Go package, but
it is one coherent context: the dev-harness edits are the shared precondition for
both D7's routing proofs and D8's onboarding proofs.

Land the service's path-routed location fragment `etc/nginx.conf` (design D7):
the `__PORT__`-templated three live tiers under `/srv/webhooks/` — the open exact
PRM bootstrap, the gated exact `/mcp` (`auth_request` + identity hygiene + 429
re-emit), the `/feed` 404 shield, the **public** `/in/` prefix (no `auth_request`,
no identity headers, `client_max_body_size 2m`, proxied with the mount stripped) —
plus the catch-all `return 404`.

Apply the **dev-harness edits** (design D8 — the only legitimate repo-root
touches): add `./webhooks` to `go.work`'s `use(…)`; add `webhooks` to `bin/start`
(build list, a `launch_webhooks` exporting `WEBHOOKS_DB_PATH` on `:3006`, the
PORTS map, the wait-for list); add `webhooks` to `bin/stop`; add `webhooks` to the
`nginx/run` fragment `__PORT__`-substitution loop.

End state: with the suite up via `bin/start`, the end-to-end tests run for real
against the dev front door on `:8080` and pass; `cd webhooks && go build ./... &&
go vet ./... && go test ./...` remains green. Per design's verification-gate
honesty rule, an all-skipped end-to-end layer (because `:8080` was unreachable) is
a **gap, not a pass** — the gate must bring the suite up and run the D7 ids for
real.

**Done when:** the design Verification ids for D7 and D8 are each covered by a
genuine test against the real harness and the suite is green —
- R-OD12-3CVG — through `:8080`, `POST /srv/webhooks/in/<name>` with a valid bearer
  and no OAuth token → `202`;
- R-OE8Y-H4M5 — through `:8080`, `/srv/webhooks/mcp` with no bearer token → `401`;
- R-OFGU-UWCU — through `:8080`, `GET
  /srv/webhooks/.well-known/oauth-protected-resource` → `200` with no token;
- R-OGOR-8O3J — through `:8080`, `GET /srv/webhooks/feed` → `404`;
- R-UELV-YLA4 — with the harness edits, `bin/start` builds and launches `webhooks`
  on `:3006` and the process answers `/health` with `200`; `bin/stop` stops it;
- R-UFTS-CD0T — with the suite up, the dashboard's service inventory / authorization-
  server resource list includes the webhooks MCP resource (`…/srv/webhooks/mcp`),
  discovered from the committed `etc/manifest.env` (`MCP=true`).
