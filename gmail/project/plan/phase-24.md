# Phase 24 — nginx fragment: forward the edge-minted correlation id

*Realizes design Decision 22 (correlation id in the nginx fragment).*

**External dependency.** The value this fragment forwards is minted and
returned by the dashboard's introspection endpoints, which the dashboard
workspace's own spec builds. The fragment is correct to ship before that lands
— an `auth_request_set` over a header the subrequest does not yet send
resolves to the empty string, which is exactly the behavior of a service that
mints its own id. No ordering constraint on this phase.

## What gets built

One shipped artifact, `gmail/etc/nginx.conf`, plus the assertions that pin it.

- The **bearer-gated prefix** `location /srv/gmail/` gains
  `auth_request_set $gmail_correlation $upstream_http_x_correlation_id;` and
  `proxy_set_header X-Correlation-Id $gmail_correlation;`.
- The **session-gated** `location = /srv/gmail/` and `location
  /srv/gmail/static/` each gain
  `auth_request_set $gmail_session_correlation $upstream_http_x_correlation_id;`
  and `proxy_set_header X-Correlation-Id $gmail_session_correlation;`.
- The **ungated PRM bootstrap** `location =
  /srv/gmail/.well-known/oauth-protected-resource` gains
  `proxy_set_header X-Correlation-Id "";`.
- The `= /srv/gmail/feed` and `= /srv/gmail/attachment` 404 stubs and
  `@gmail_authn_500` are untouched — they proxy nothing.
- The existing content assertions in `cmd/gmail/nginx_test.go` are extended to
  cover the new lines, in the style already used there (read the file from
  disk, assert on its content), each tagged with its D22 requirement id.

Observable end state: a gated request reaching gmail carries the chain id the
edge minted, and can never carry one a client supplied; an ungated request
carries none, so the chassis mints. No Go behavior changes.

## Done when

The suite is green — `cd gmail && go build ./...`, `go vet ./...`,
`gofmt -l .` (no output), and `go test ./...` all succeed with zero failures —
D22's ids are covered by clearly-named tests over the shipped artifact:

- `R-1M1T-299W` — the bearer-gated prefix carries both the
  `auth_request_set $gmail_correlation …` capture and the matching
  `proxy_set_header X-Correlation-Id $gmail_correlation;`.
- `R-1N9P-G10L` — both session-gated locations (the exact-match landing and the
  static tier) carry the `$gmail_session_correlation` capture and its matching
  forward; exactly two do.
- `R-1OHL-TSRA` — the ungated PRM bootstrap blanks the header
  (`proxy_set_header X-Correlation-Id "";`) with no correlation
  `auth_request_set` inside it, and exactly four
  `proxy_set_header X-Correlation-Id` directives exist in the whole file.

and these deterministic checks over the shipped artifact pass (all run against
`gmail/etc/nginx.conf`, which is not under `project/`):

- `grep -c 'auth_request_set \$gmail_correlation \$upstream_http_x_correlation_id;' gmail/etc/nginx.conf`
  prints **1**.
- `grep -c 'auth_request_set \$gmail_session_correlation \$upstream_http_x_correlation_id;' gmail/etc/nginx.conf`
  prints **2**.
- `grep -c 'proxy_set_header X-Correlation-Id \$gmail_correlation;' gmail/etc/nginx.conf`
  prints **1**.
- `grep -c 'proxy_set_header X-Correlation-Id \$gmail_session_correlation;' gmail/etc/nginx.conf`
  prints **2**.
- `grep -c 'proxy_set_header X-Correlation-Id "";' gmail/etc/nginx.conf`
  prints **1**.
- `grep -cE '^[[:space:]]*proxy_set_header X-Correlation-Id' gmail/etc/nginx.conf`
  prints **4** — the bearer location, the two session-gated locations, and the
  PRM bootstrap's empty set, and nothing else. A stray extra forwarding line,
  or one added to a location D22 leaves alone, fails this check.
