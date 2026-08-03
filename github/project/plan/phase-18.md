# Phase 18 — nginx fragment: forward the edge-minted correlation id

*Realizes design Decision 12 (correlation id in the nginx fragment).*

**External dependency.** The value this fragment forwards is minted and
returned by the dashboard's introspection endpoints, which the dashboard
workspace's own spec builds. The fragment is correct to ship before that lands
— an `auth_request_set` over a header the subrequest does not yet send
resolves to the empty string, which is exactly the behavior of a service that
mints its own id. No ordering constraint on this phase.

## What gets built

One shipped artifact, `github/etc/nginx.conf`, plus the assertions that pin it.

- The **bearer-gated prefix** `location /srv/github/` gains
  `auth_request_set $github_correlation $upstream_http_x_correlation_id;` and
  `proxy_set_header X-Correlation-Id $github_correlation;`.
- The **session-gated** `location = /srv/github/` and `location
  /srv/github/static/` each gain
  `auth_request_set $github_session_correlation $upstream_http_x_correlation_id;`
  and `proxy_set_header X-Correlation-Id $github_session_correlation;`.
- The **ungated PRM bootstrap** `location =
  /srv/github/.well-known/oauth-protected-resource` gains
  `proxy_set_header X-Correlation-Id "";`.
- The `= /srv/github/pr` and `= /srv/github/token` 404 stubs and
  `@github_authn_500` are untouched — they proxy nothing.
- The existing content assertions in `internal/web/nginx_test.go` are extended
  to cover the new lines, in the style already used there (read the file from
  disk, assert on its content). They carry no requirement id, matching D12.

Observable end state: a gated request reaching `github` carries the chain id
the edge minted, and can never carry one a client supplied; an ungated request
carries none, so the chassis mints. No Go behavior changes.

## Done when

The suite is green — from `github/`: `GOWORK=off go build ./...` succeeds,
`GOWORK=off go test ./...` passes with no failures and no `SKIP`,
`gofmt -l .` is empty, and `go vet ./...` is clean — and these deterministic
checks over the shipped artifact pass (all run against `github/etc/nginx.conf`,
which is not under `project/`):

- `grep -c 'auth_request_set \$github_correlation \$upstream_http_x_correlation_id;' github/etc/nginx.conf`
  prints **1**.
- `grep -c 'auth_request_set \$github_session_correlation \$upstream_http_x_correlation_id;' github/etc/nginx.conf`
  prints **2**.
- `grep -c 'proxy_set_header X-Correlation-Id \$github_correlation;' github/etc/nginx.conf`
  prints **1**.
- `grep -c 'proxy_set_header X-Correlation-Id \$github_session_correlation;' github/etc/nginx.conf`
  prints **2**.
- `grep -c 'proxy_set_header X-Correlation-Id "";' github/etc/nginx.conf`
  prints **1**.
- `grep -cE '^[[:space:]]*proxy_set_header X-Correlation-Id' github/etc/nginx.conf`
  prints **4** — the bearer location, the two session-gated locations, and the
  PRM bootstrap's empty set, and nothing else. A stray extra forwarding line,
  or one added to a location D12 leaves alone, fails this check.
