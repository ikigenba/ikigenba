# Phase 14 — Forward all four owner-identity headers through cron's nginx fragment

*Realizes design Decision 4 (nginx fragment: owner-identity forwarding
contract).*

This phase extends cron's nginx location fragment (`cron/etc/nginx.conf`) so that
**every** location that forwards identity today captures and forwards the four
owner headers the dashboard's introspection hooks now emit
(`X-Owner-Id`, `X-Owner-Email`, `X-Owner-Name`, `X-Owner-Picture`), not just
`X-Owner-Email`. The two identity-forwarding locations change; nothing else does.

- The **session-gated** exact landing location `location = /srv/cron/` gains
  `auth_request_set` + `proxy_set_header` for `X-Owner-Id`, `X-Owner-Name`, and
  `X-Owner-Picture` alongside its existing `X-Owner-Email`, using the
  service-prefixed variables `$cron_session_owner_id`, `$cron_session_owner_name`,
  `$cron_session_owner_picture` (beside the existing `$cron_session_owner`).
- The **bearer-gated** service prefix `location /srv/cron/` gains the same three
  owner headers, using `$cron_owner_id`, `$cron_owner_name`, `$cron_owner_picture`
  (beside the existing `$cron_owner`); its `X-Client-Id` (`$cron_client`) capture
  and its `@cron_authn_500` rate-limit re-emit are retained unchanged.
- The static asset tier (`/srv/cron/static/`), the PRM bootstrap, the
  `/srv/cron/feed` 404, and the 429 re-emit named location forward no identity
  and are left exactly as they are.

The identity-hygiene property extends to the new headers: one `proxy_set_header`
per header name, sourced from the auth-subrequest variable, so a client cannot
smuggle any owner header. The variable names stay `$cron_*`-prefixed and unique
across the apex-included fragments. Downstream consumption of `X-Owner-Id` by the
cron binary is out of scope here.

The change is proven by extending the existing content-assertion test that reads
`cron/etc/nginx.conf` from disk (`cmd/cron/main_test.go`); nginx is not run by the
suite.

**Done when** — cron's suite is green per the design Conventions
(`cd cron && go build ./...`, `go vet ./...`, `gofmt -l .` empty, and
`go test ./...` all pass with zero failures), and each id below is covered by a
clearly-named test tagged with the id:

- R-8ALX-VK6V — a test over `cron/etc/nginx.conf` asserts the session-gated exact
  landing location `= /srv/cron/` contains all four
  `auth_request_set $cron_session_owner{,_id,_name,_picture}
  $upstream_http_x_owner_{email,id,name,picture}` captures and a matching
  `proxy_set_header` for each of `X-Owner-Email`, `X-Owner-Id`, `X-Owner-Name`,
  `X-Owner-Picture` sourced from those variables.
- R-8BTU-9BXK — a test over `cron/etc/nginx.conf` asserts the bearer-gated prefix
  `location /srv/cron/` contains all four
  `auth_request_set $cron_owner{,_id,_name,_picture}
  $upstream_http_x_owner_{email,id,name,picture}` captures and a matching
  `proxy_set_header` for each of the four owner headers, with its existing
  `proxy_set_header X-Client-Id $cron_client` retained.
