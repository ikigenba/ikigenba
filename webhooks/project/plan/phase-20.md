# Phase 20 — Correlation lines in the committed nginx fragment

*Realizes design Decision 18 (correlation at the front door). No dependency on
any other pending phase.*

`webhooks/etc/nginx.conf` gains the correlation-header plumbing D18 specifies,
and nothing else. This is a **structural** phase: the fragment is inert
configuration, it carries no requirement ids, and it can be built before the
revised `appkit`/`eventplane` exist (its effect becomes observable when they do,
under Phase 21).

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

**Done when:** all four checks below pass from `webhooks/`, with the suite green
per design's *Conventions* (`go build ./...`, `go vet ./...`, `go test ./...`
all clean — the existing `cmd/webhooks/nginx_test.go` and `internal/e2e`
fragment assertions must still pass, proving no tier regressed):

1. `grep -c 'proxy_set_header X-Correlation-Id' etc/nginx.conf` prints exactly
   `5`.
2. `grep -c 'proxy_set_header X-Correlation-Id "";' etc/nginx.conf` prints
   exactly `2`.
3. `grep -cE 'auth_request_set \$wh_(corr|session_corr|static_corr) \$upstream_http_x_correlation_id;' etc/nginx.conf`
   prints exactly `3`.
4. `grep -c 'upstream_http_x_correlation_id' etc/nginx.conf` prints exactly `3`
   — the id is captured on the gated tiers only, never on an ungated one.
