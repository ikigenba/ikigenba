# Phase 16 — Carry `X-Correlation-Id` across cron's nginx trust boundary

*Realizes design Decision 16 (correlation adoption and the tick root), fragment
slice — its ids R-BBON-OZTN and R-BE4G-GJB1. No dependency on any other pending
phase, and none on the appkit/eventplane rebuild — this phase touches one
config file and its test, so it can run before those land.*

This phase changes exactly one shipped artifact, `cron/etc/nginx.conf`, so the
introspection-minted correlation id reaches the loopback service and a public
caller can never inject one. It adds lines and removes none.

**Every gated location captures the id and forwards it**, with a
service-prefixed variable so the name stays unique inside the one apex
`server` block all fragments are included into:

- `location /srv/cron/` (the bearer prefix, `auth_request /_authn`) —
  `auth_request_set $cron_correlation $upstream_http_x_correlation_id;` and
  `proxy_set_header X-Correlation-Id $cron_correlation;`, beside its existing
  four `X-Owner-*` captures, its `X-Client-Id`, and its
  `$authn_status`/`@cron_authn_500` rate-limit re-emit — all retained
  unchanged.
- `location = /srv/cron/` (the session-gated landing root,
  `auth_request /_session-authn`) — the same pair using
  `$cron_session_correlation`, beside its four existing `X-Owner-*` captures.
- `location /srv/cron/static/` (the session-gated asset tier) — the same pair
  using `$cron_static_correlation`. This location forwards no owner identity
  and still does not; the correlation id is not identity and carries no
  authority, and an asset fetch is a real hop of the page load's chain.

**The one ungated proxying location strips the header**:
`location = /srv/cron/.well-known/oauth-protected-resource` gains
`proxy_set_header X-Correlation-Id "";`. nginx omits a header set to the empty
string entirely, so the chassis sees nothing inbound and mints for itself.

`location = /srv/cron/feed` (`return 404;`) and `location @cron_authn_500`
proxy nothing and are untouched — the event plane stays unreachable through
nginx exactly as before.

The change is proven by extending the existing id-tagged content-assertion
tests that read `cron/etc/nginx.conf` from disk (`cmd/cron/main_test.go`, where
D4's `R-NGNX-*` / `R-8ALX-VK6V` / `R-8BTU-9BXK` tags already live); nginx is not
run by the suite.

**Done when** — the suite is green per design's *Conventions*
(`cd cron && go build ./...`, `go vet ./...`, `gofmt -l .` empty, and
`go test ./...` all pass with zero failures), each id below is covered by a
clearly-named test tagged with the id, and the deterministic checks hold:

- R-BBON-OZTN — a test over `cron/etc/nginx.conf` asserts all three gated
  locations carry both halves: `location /srv/cron/` (bearer) has
  `auth_request_set $cron_correlation $upstream_http_x_correlation_id;` plus
  `proxy_set_header X-Correlation-Id $cron_correlation;`; `location = /srv/cron/`
  (session) the same pair on `$cron_session_correlation`; and
  `location /srv/cron/static/` the same pair on `$cron_static_correlation`.
- R-BE4G-GJB1 — a test asserts `location =
  /srv/cron/.well-known/oauth-protected-resource` carries
  `proxy_set_header X-Correlation-Id "";`, **and** that the file carries
  exactly four `proxy_set_header X-Correlation-Id` lines in total.

Deterministic checks:

- **Three gated captures present.** All three succeed from `cron/`:
  - `grep -c 'auth_request_set \$cron_correlation \$upstream_http_x_correlation_id;' etc/nginx.conf` prints `1`
  - `grep -c 'auth_request_set \$cron_session_correlation \$upstream_http_x_correlation_id;' etc/nginx.conf` prints `1`
  - `grep -c 'auth_request_set \$cron_static_correlation \$upstream_http_x_correlation_id;' etc/nginx.conf` prints `1`
- **Three matching forwards present.** `grep -c 'proxy_set_header X-Correlation-Id \$cron_' etc/nginx.conf` prints `3`.
- **The ungated PRM location strips.**
  `grep -c 'proxy_set_header X-Correlation-Id "";' etc/nginx.conf` prints `1`,
  and the whole file contains exactly **four** `X-Correlation-Id`
  `proxy_set_header` lines (`grep -c 'proxy_set_header X-Correlation-Id' etc/nginx.conf`
  prints `4`) — so no location under the mount was left forwarding a
  client-supplied value.
- **Nothing regressed.** The pre-existing locations all still appear:
  `grep -c 'location = /srv/cron/.well-known/oauth-protected-resource'`,
  `grep -c 'location = /srv/cron/feed { return 404; }'`,
  `grep -c 'location = /srv/cron/ {'`, `grep -c 'location /srv/cron/static/'`,
  `grep -c 'location /srv/cron/ {'`, and `grep -c 'location @cron_authn_500'`
  each print `1`, and `grep -c 'proxy_set_header X-Owner-' etc/nginx.conf`
  prints `8` (four owner headers on each of the two identity-forwarding
  locations, unchanged).
- **No migration, no schema, no Go source outside the test.**
  `git status --porcelain internal/db/migrations/` prints nothing.
