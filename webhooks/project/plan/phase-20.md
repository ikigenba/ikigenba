# Phase 20 — Correlation lines in the committed nginx fragment

*Realizes design Decision 18 (correlation at the front door). No dependency on
any other pending phase.*

`webhooks/etc/nginx.conf` gains the correlation-header plumbing D18 specifies,
and nothing else. The fragment is inert configuration, so this phase can be
built before the revised `appkit`/`eventplane` exist (its effect becomes
observable when they do, under Phase 21). Its proof extends the existing
per-location content assertions in `cmd/webhooks/nginx_test.go`, the way every
other tier in this fragment is already pinned.

End state of the fragment:

- The two **ungated, proxying** locations — the PRM bootstrap
  `= /srv/webhooks/.well-known/oauth-protected-resource` and the public ingress
  prefix `/srv/webhooks/in/` — each carry
  `proxy_set_header X-Correlation-Id "";`, so no caller-supplied id can reach
  the service and the chassis mints a fresh chain id per delivery.
- The three **gated** locations each capture the id minted by the dashboard's
  introspection subresponse into their own service-prefixed variable and
  forward it with a single `proxy_set_header`:
  `= /srv/webhooks/mcp` → `$wh_corr`, `= /srv/webhooks/` → `$wh_session_corr`,
  `/srv/webhooks/static/` → `$wh_static_corr`.
- `= /srv/webhooks/feed` and the `/srv/webhooks/` catch-all are untouched (both
  `return 404` without proxying), as are every tier's gate, upstream,
  precedence, owner-header plumbing, `client_max_body_size`, and the
  `@webhooks_authn_500` re-emit.

**Done when:** both ids below are covered by clearly-named per-location
assertions in `cmd/webhooks/nginx_test.go` (reading the repo-real
`etc/nginx.conf` from disk and asserting **inside each location block**, using
the existing block-extraction helper — a whole-file substring search does not
satisfy them), and the suite is green per design's *Conventions*
(`go build ./...`, `go vet ./...`, `go test ./...` all clean, with every
pre-existing fragment assertion in `cmd/webhooks/nginx_test.go` and
`internal/e2e` still passing, proving no tier regressed):

- **R-EL96-NKKT** — both ungated proxying blocks (the PRM exact match and the
  public ingress prefix) carry `proxy_set_header X-Correlation-Id "";` and
  neither carries an `auth_request` or a correlation `auth_request_set`.
- **R-EMH3-1CBI** — each of the three gated blocks (`= /srv/webhooks/mcp`,
  `= /srv/webhooks/`, `/srv/webhooks/static/`) captures
  `$upstream_http_x_correlation_id` into its own variable (`$wh_corr`,
  `$wh_session_corr`, `$wh_static_corr` — three distinct names) and forwards
  exactly that variable with one `proxy_set_header X-Correlation-Id` in the same
  block; no gated block carries the empty-string strip.

As a fast structural sanity check while building (not the acceptance bar):
`grep -c 'proxy_set_header X-Correlation-Id' etc/nginx.conf` should print `5`,
`grep -c 'proxy_set_header X-Correlation-Id "";'` should print `2`, and
`grep -c 'upstream_http_x_correlation_id'` should print `3`.
