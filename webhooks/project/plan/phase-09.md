# Phase 9 — nginx landing + static tiers (session-gated)

*Realizes design Decision 7 (nginx location fragment — the landing + static tiers only; the D8 nginx content-assertion layer is the substrate, but adds no new D8 ids). Depends on Phase 8 (the landing handler the tiers proxy to).*

Make the landing page reachable through the front door by adding its two tiers to
the committed `etc/nginx.conf` fragment, beside the existing PRM / `/mcp` /
`/feed` / `/in/` tiers and the catch-all. Both new tiers are gated by the
dashboard **browser session** (`auth_request /_session-authn`), never the bearer
gate — a browser holds no token.

Add to `etc/nginx.conf` (design D7):

- an **exact-match** `location = /srv/webhooks/` — `auth_request
  /_session-authn`, capture + forward the session owner
  (`proxy_set_header X-Owner-Email $wh_session_owner`), `proxy_pass
  http://127.0.0.1:3006/` (trailing slash → upstream root `/`);
- a `location /srv/webhooks/static/` — `auth_request /_session-authn`,
  `proxy_pass http://127.0.0.1:3006/static/`.

Leave every existing tier byte-intact: the exact PRM bootstrap, the bearer-gated
exact `/mcp`, the `/feed` 404 shield, the public `/in/` prefix, and the catch-all
`location /srv/webhooks/ { return 404; }`. Precedence holds automatically —
exact `=` beats prefixes, and `/srv/webhooks/static/` is longer than the
catch-all.

Because the browser-session gate cannot be driven live through `:8080` (the e2e
harness mints no dashboard session cookie), these tiers are proven by a
**content-assertion** test over the committed fragment text
(`internal/web/nginx_test.go`, crm-`nginx_test.go`-style: `os.ReadFile` the
fragment + assert the location blocks), not by a request through the front door.
The dev front door still mirrors prod: `webhooks` is already in the `nginx/run`
fragment loop, so no harness edit is needed.

End state: the fragment carries the two new tiers; `internal/web/nginx_test.go`
asserts them; `cd webhooks && go build ./... && go vet ./... && go test ./...`
is green. (The Phase 7 end-to-end ids — ingress / `/mcp` / PRM / `/feed` — remain
owned and verified by Phase 7; this phase must not disturb them, but does not
re-cover them.)

**Done when:** this phase's three D7 Verification ids are each covered by a
genuine content-assertion test over the committed fragment and the suite is green
(`go build ./... && go vet ./... && go test ./...`) — no running suite required,
since these are static string assertions —
- R-TTUW-5O3V — the fragment defines the landing as an exact-match `location =
  /srv/webhooks/`, session-gated (`auth_request /_session-authn`, not `/_authn`),
  forwarding `X-Owner-Email $wh_session_owner`, proxying to
  `http://127.0.0.1:3006/`;
- R-TV2S-JFUK — the fragment defines `location /srv/webhooks/static/`,
  session-gated (`auth_request /_session-authn`), proxying to
  `http://127.0.0.1:3006/static/`;
- R-TWAO-X7L9 — the added tiers leave the prior tiers intact: the catch-all
  `location /srv/webhooks/` still `return 404`, `/mcp` still bearer-gated
  (`auth_request /_authn`), `/srv/webhooks/feed` still `return 404`.

Regression context (not this phase's coverage): the Phase 7 end-to-end tier ids
(the `:8080` ingress / `/mcp` / PRM / `/feed` checks) stay green because the added
tiers shadow none of those surfaces — but they remain Phase 7's to verify and are
deliberately not listed here by id, so this phase's covered-id set is exactly the
three above.
