# Phase 27 — Correlation lines in the committed nginx fragment

*Realizes design Decision 31 (nginx fragment: capture on the gated locations,
strip on the ungated one). No dependency on any other pending phase.*

`scripts/etc/nginx.conf` gains the correlation-header plumbing D31 specifies,
and nothing else. This is a **structural** phase: the fragment is inert
configuration, it carries no requirement ids, and it can be built before the
revised `eventplane`/`appkit` exist (its effect becomes observable when they
do).

End state of the fragment:

- The three **gated** locations each capture the chain id the dashboard minted,
  from the auth subresponse, into their own service-prefixed variable and
  forward it with a single `proxy_set_header`: the bearer prefix
  `location /srv/scripts/` → `$scripts_corr`, the session landing
  `location = /srv/scripts/` → `$scripts_session_corr`, and the session assets
  `location /srv/scripts/static/` → `$scripts_static_corr`.
- The one **ungated proxying** location — the PRM bootstrap
  `= /srv/scripts/.well-known/oauth-protected-resource` — carries
  `proxy_set_header X-Correlation-Id "";`, so no client value survives and the
  chassis mints.
- `= /srv/scripts/feed` (a bare `return 404`) and the `@scripts_authn_500`
  re-emit are untouched, as are every tier's gate, precedence, upstream, and
  owner-header plumbing.

**Done when:** all four checks below pass from `scripts/`, with the suite green
per design's *Conventions* (`go build ./...`, `go vet ./...`, `gofmt -l .`
printing nothing, and `go test ./...` — the existing fragment content
assertions in `cmd/scripts/main_test.go` must still pass, proving no tier
regressed and that the string `prompts` still does not appear in the file):

1. `grep -c 'proxy_set_header X-Correlation-Id' etc/nginx.conf` prints exactly
   `4`.
2. `grep -c 'proxy_set_header X-Correlation-Id "";' etc/nginx.conf` prints
   exactly `1`.
3. `grep -cE 'auth_request_set \$scripts_(corr|session_corr|static_corr) \$upstream_http_x_correlation_id;' etc/nginx.conf`
   prints exactly `3`.
4. `grep -c 'upstream_http_x_correlation_id' etc/nginx.conf` prints exactly `3`
   — the id is captured on the gated tiers only, never on an ungated one.
