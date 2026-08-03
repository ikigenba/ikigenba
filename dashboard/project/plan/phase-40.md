# Phase 40 — Mint the suite's correlation id at the introspection edge, and fix the apex fragment

*Realizes design Decision 30 (edge minting) and 33 (apex nginx fragment).*

The dashboard is the last hop before any `/srv/<svc>/` request reaches a service,
so it is where that request's correlation id is born. This phase makes the three
introspection endpoints mint one per decision and return it on every allow, and
corrects the dashboard's own nginx fragment so nothing client-supplied can ride
in on that header and so the endpoints can learn the gated request's method.

**Cross-workspace dependency.** The shared minter lands in the `eventplane`
workspace (`eventplane/correlation`) before this phase builds — the suite's
execution order is root/registry, then appkit and eventplane, then the telemetry
service, then the dashboard. This phase consumes the minter through the
one-method `correlationMinter` interface the dashboard owns, satisfied at the
composition root, so the adapter is the only place a signature change lands.

The end state:

- `internal/server` owns a `correlationMinter` seam; `cmd/dashboard/main.go`
  satisfies it with the shared minter and threads it through `server.Options`.
- `handleAuthn`, `handleAuthnPAT`, and `handleSessionAuthn` each mint a fresh id
  at the start of the decision, hold it for the record phase 41 adds, and set
  `X-Correlation-Id` on the **allow** branch only — never on a 401, 403, 429, or
  500. None of the three reads an inbound `X-Correlation-Id`.
- `dashboard/etc/nginx.conf` sets `proxy_set_header X-Correlation-Id "";` on the
  apex `location /`, on `location = /_authn`, and on `location = /_session-authn`,
  and adds `proxy_set_header X-Original-Method $request_method;` to the two
  internal locations alongside the existing `X-Original-URI $request_uri;`.

**Done when:** these ids are covered by clearly-named tests and the suite is
green. The endpoint tests drive the real handlers through `(*app).routes()` with
`httptest` against a real temp database — the response headers are the seam under
test, so nothing in the decision path is stubbed. The fragment tests read
`etc/nginx.conf` from disk and assert its content, extending the id-tagged
precedent already in `cmd/dashboard/main_test.go` (`R-XJBT-7YIF`/`R-XKJP-LQ94`,
which extract a named location block and assert within it) — assert per block, in
the same style, so a line added to the wrong location fails.

- R-X7FZ-83J9 — a `handleAuthn` allow's `X-Correlation-Id` is exactly 26
  characters drawn only from the Crockford alphabet
  `0123456789ABCDEFGHJKMNPQRSTVWXYZ` (no `I`, `L`, `O`, `U`, no lowercase).
- R-X8NV-LV9Y — two successive allows on the same token return different
  `X-Correlation-Id` values.
- R-X9VR-ZN0N — an allow whose request carried an attacker-chosen
  `X-Correlation-Id` returns a different value, and the supplied value appears in
  no response header.
- R-XB3O-DERC — a `handleSessionAuthn` allow carries an `X-Correlation-Id` with
  the same format and freshness properties and never echoes a supplied value.
- R-XCBK-R6I1 — a `handleAuthn` 401, a `handleAuthn` 429, and a
  `handleSessionAuthn` 401 each carry no `X-Correlation-Id` header at all.
- R-XOIK-KVWZ — the apex `location /` block in `etc/nginx.conf` contains
  `proxy_set_header X-Correlation-Id "";`.
- R-XPQG-YNNO — the `= /_authn` and `= /_session-authn` blocks each contain
  `proxy_set_header X-Original-Method $request_method;` and
  `proxy_set_header X-Correlation-Id "";`, and each still contains
  `X-Original-URI $request_uri` (asserted per block, so adding the lines to only
  one location fails).

Green means, from `dashboard/`: `go build ./...`, `go vet ./...`, `gofmt -l .`
(no output), and `go test ./...` all succeed with zero failures.
