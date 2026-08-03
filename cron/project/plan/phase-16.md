# Phase 16 — Carry `X-Correlation-Id` across cron's nginx trust boundary

*Realizes design Decision 16 (correlation adoption and tick-root minting),
fragment slice only — no Verification ids: the fragment adds no cron behavior
of its own, so its acceptance bar is deterministic content assertions over the
shipped artifact. No dependency on any other pending phase, and none on the
appkit/eventplane rebuild — this phase touches one config file and no Go
source, so it can run before those land.*

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

The change is proven by extending the existing content-assertion test that
reads `cron/etc/nginx.conf` from disk (`cmd/cron`); nginx is not run by the
suite.

**Done when** (deterministic exit conditions):

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
- **A test asserts it, not just the grep.** The `cmd/cron` fragment test that
  reads `etc/nginx.conf` from disk is extended with the assertions above and
  passes.
- **No migration, no schema, no Go source outside the test.**
  `git status --porcelain internal/db/migrations/` prints nothing.
- The suite is green per design's *Conventions*: `cd cron && go build ./...`,
  `cd cron && go vet ./...`, `cd cron && gofmt -l .` (no output), and
  `cd cron && go test ./...` all succeed with zero failures.
